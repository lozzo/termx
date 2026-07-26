package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/muxvia/muxvia/cloud/runtimesnapshot"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrStateClosed 表示 Edge runtime actor 已停止，调用方必须结束当前连接生命周期。
	ErrStateClosed = errors.New("Edge runtime state is closed")
	// ErrStaleGeneration 表示迟到事件不能修改较新的实时对象。
	ErrStaleGeneration = errors.New("runtime object generation is stale")
)

// StateConfig 约束 Edge runtime actor 的 mailbox 和每条 Controller 流的增量缓冲。
type StateConfig struct {
	MailboxSize int
	DeltaBuffer int
}

// State 是 Edge 在线 agent/session 的唯一内存 owner。
// R2 的写入来自测试 harness；R4/R5 将由 gateway 把认证后的事件提交到同一 actor。
type State struct {
	mailbox   chan stateRequest
	done      chan struct{}
	closing   atomic.Bool
	closeOnce sync.Once
}

// Feed 在同一个 actor 事务中取得 revision R 的快照并订阅 R 之后的增量。
// 调用方必须 Close，网络发送不得在 actor goroutine 中执行。
type Feed struct {
	Snapshot *cloudv1.RuntimeSnapshot
	Deltas   <-chan *cloudv1.RuntimeDelta
	close    func()
}

// Close 取消当前控制流的增量订阅，不影响 Edge runtime 真值。
func (feed *Feed) Close() {
	if feed != nil && feed.close != nil {
		feed.close()
		feed.close = nil
	}
}

type stateRequest struct {
	run func(*stateData)
}

type stateData struct {
	revision       uint64
	agents         map[string]*cloudv1.AgentPresence
	sessions       map[string]*cloudv1.ClientSessionSummary
	subscribers    map[uint64]chan *cloudv1.RuntimeDelta
	nextSubscriber uint64
	deltaBuffer    int
}

// NewState 启动单写者 actor；容量必须有界且大于零。
func NewState(config StateConfig) (*State, error) {
	if config.MailboxSize <= 0 || config.DeltaBuffer <= 0 {
		return nil, errors.New("runtime mailbox and delta buffer must be positive")
	}
	state := &State{mailbox: make(chan stateRequest, config.MailboxSize), done: make(chan struct{})}
	go state.run(config.DeltaBuffer)
	return state, nil
}

// UpsertAgent 原子写入较新或同 generation 的 daemon Presence，并产生一个 revision。
func (state *State) UpsertAgent(ctx context.Context, agent *cloudv1.AgentPresence) error {
	if _, err := runtimesnapshot.NormalizeClone(&cloudv1.RuntimeSnapshot{Agents: []*cloudv1.AgentPresence{agent}}); err != nil {
		return err
	}
	clone := proto.Clone(agent).(*cloudv1.AgentPresence)
	return state.mutate(ctx, func(data *stateData) error {
		if current := data.agents[clone.GetDaemonId()]; current != nil && clone.GetGeneration() < current.GetGeneration() {
			return ErrStaleGeneration
		}
		data.agents[clone.GetDaemonId()] = clone
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentUpserted{AgentUpserted: proto.Clone(clone).(*cloudv1.AgentPresence)}})
		return nil
	})
}

// RemoveAgent 仅删除精确 generation，避免旧连接关闭事件误删新 Presence。
func (state *State) RemoveAgent(ctx context.Context, daemonID string, generation uint64) error {
	return state.mutate(ctx, func(data *stateData) error {
		current := data.agents[daemonID]
		if current == nil || current.GetGeneration() != generation {
			return ErrStaleGeneration
		}
		delete(data.agents, daemonID)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentRemoved{AgentRemoved: &cloudv1.AgentRemoved{DaemonId: daemonID, Generation: generation}}})
		return nil
	})
}

// UpsertSession 原子写入较新或同 generation 的客户端信令会话。
func (state *State) UpsertSession(ctx context.Context, session *cloudv1.ClientSessionSummary) error {
	if _, err := runtimesnapshot.NormalizeClone(&cloudv1.RuntimeSnapshot{Sessions: []*cloudv1.ClientSessionSummary{session}}); err != nil {
		return err
	}
	clone := proto.Clone(session).(*cloudv1.ClientSessionSummary)
	return state.mutate(ctx, func(data *stateData) error {
		if current := data.sessions[clone.GetSessionId()]; current != nil && clone.GetGeneration() < current.GetGeneration() {
			return ErrStaleGeneration
		}
		data.sessions[clone.GetSessionId()] = clone
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_SessionUpserted{SessionUpserted: proto.Clone(clone).(*cloudv1.ClientSessionSummary)}})
		return nil
	})
}

// RemoveSession 仅删除精确 generation 的客户端信令会话。
func (state *State) RemoveSession(ctx context.Context, sessionID string, generation uint64) error {
	return state.mutate(ctx, func(data *stateData) error {
		current := data.sessions[sessionID]
		if current == nil || current.GetGeneration() != generation {
			return ErrStaleGeneration
		}
		delete(data.sessions, sessionID)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_SessionRemoved{SessionRemoved: &cloudv1.ClientSessionRemoved{SessionId: sessionID, Generation: generation}}})
		return nil
	})
}

// OpenFeed 返回原子快照和后续增量；增量缓冲溢出时 channel 会关闭，ControllerLink 必须重连重建。
func (state *State) OpenFeed(ctx context.Context) (*Feed, error) {
	type result struct {
		feed *Feed
		err  error
	}
	reply := make(chan result, 1)
	err := state.submit(ctx, func(data *stateData) {
		snapshot, snapshotErr := data.snapshot()
		if snapshotErr != nil {
			reply <- result{err: snapshotErr}
			return
		}
		data.nextSubscriber++
		id := data.nextSubscriber
		channel := make(chan *cloudv1.RuntimeDelta, data.deltaBuffer)
		data.subscribers[id] = channel
		reply <- result{feed: &Feed{Snapshot: snapshot, Deltas: channel, close: func() { state.unsubscribe(id) }}}
	})
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-state.done:
		return nil, ErrStateClosed
	case result := <-reply:
		return result.feed, result.err
	}
}

// Snapshot 返回当前不可变投影，供状态查询和测试使用。
func (state *State) Snapshot(ctx context.Context) (*cloudv1.RuntimeSnapshot, error) {
	reply := make(chan struct {
		snapshot *cloudv1.RuntimeSnapshot
		err      error
	}, 1)
	if err := state.submit(ctx, func(data *stateData) {
		snapshot, err := data.snapshot()
		reply <- struct {
			snapshot *cloudv1.RuntimeSnapshot
			err      error
		}{snapshot, err}
	}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-state.done:
		return nil, ErrStateClosed
	case result := <-reply:
		return result.snapshot, result.err
	}
}

// Close 停止 actor 并关闭全部 feed；重复调用安全。
func (state *State) Close() {
	state.closeOnce.Do(func() {
		state.closing.Store(true)
		ack := make(chan struct{})
		select {
		case state.mailbox <- stateRequest{run: func(data *stateData) {
			for id, subscriber := range data.subscribers {
				close(subscriber)
				delete(data.subscribers, id)
			}
			close(ack)
		}}:
			<-ack
		case <-state.done:
		}
		close(state.done)
	})
}

func (state *State) run(deltaBuffer int) {
	data := &stateData{
		agents: make(map[string]*cloudv1.AgentPresence), sessions: make(map[string]*cloudv1.ClientSessionSummary),
		subscribers: make(map[uint64]chan *cloudv1.RuntimeDelta), deltaBuffer: deltaBuffer,
	}
	for {
		select {
		case <-state.done:
			return
		case request := <-state.mailbox:
			request.run(data)
		}
	}
}

func (state *State) mutate(ctx context.Context, mutation func(*stateData) error) error {
	reply := make(chan error, 1)
	if err := state.submit(ctx, func(data *stateData) { reply <- mutation(data) }); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-state.done:
		return ErrStateClosed
	case err := <-reply:
		return err
	}
}

func (state *State) submit(ctx context.Context, run func(*stateData)) error {
	if state.closing.Load() {
		return ErrStateClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-state.done:
		return ErrStateClosed
	case state.mailbox <- stateRequest{run: run}:
		return nil
	}
}

func (state *State) unsubscribe(id uint64) {
	_ = state.submit(context.Background(), func(data *stateData) {
		if subscriber := data.subscribers[id]; subscriber != nil {
			close(subscriber)
			delete(data.subscribers, id)
		}
	})
}

func (data *stateData) snapshot() (*cloudv1.RuntimeSnapshot, error) {
	snapshot := &cloudv1.RuntimeSnapshot{Revision: data.revision, Agents: make([]*cloudv1.AgentPresence, 0, len(data.agents)), Sessions: make([]*cloudv1.ClientSessionSummary, 0, len(data.sessions))}
	for _, agent := range data.agents {
		snapshot.Agents = append(snapshot.Agents, proto.Clone(agent).(*cloudv1.AgentPresence))
	}
	for _, session := range data.sessions {
		snapshot.Sessions = append(snapshot.Sessions, proto.Clone(session).(*cloudv1.ClientSessionSummary))
	}
	normalized, err := runtimesnapshot.NormalizeClone(snapshot)
	if err != nil {
		return nil, fmt.Errorf("normalize Edge runtime snapshot: %w", err)
	}
	return normalized, nil
}

func (data *stateData) publish(delta *cloudv1.RuntimeDelta) {
	for id, subscriber := range data.subscribers {
		select {
		case subscriber <- proto.Clone(delta).(*cloudv1.RuntimeDelta):
		default:
			close(subscriber)
			delete(data.subscribers, id)
		}
	}
}
