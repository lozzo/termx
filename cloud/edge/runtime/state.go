package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/runtimesnapshot"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
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
	MailboxSize       int
	DeltaBuffer       int
	MaxSessions       int
	MaxPendingSignals int
	Now               func() time.Time
}

// State 是 Edge 在线 agent/session 的唯一内存 owner。
// R2 的写入来自测试 harness；R4/R5 将由 gateway 把认证后的事件提交到同一 actor。
type State struct {
	mailbox   chan *stateRequest
	done      chan struct{}
	closing   atomic.Bool
	gate      sync.RWMutex
	closeOnce sync.Once
	now       func() time.Time
	limits    stateLimits
}

type stateLimits struct {
	maxSessions       int
	maxPendingSignals int
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
	lifecycle atomic.Uint32
	run       func(*stateData) error
	result    chan error
	stop      bool
}

const (
	requestQueued uint32 = iota
	requestExecuting
	requestCanceled
)

type stateData struct {
	revision           uint64
	agents             map[string]*cloudv1.AgentPresence
	agentClaims        map[string]*cloudv1.DaemonBindingClaims
	agentWriters       map[string]agentWriter
	agentNextGen       map[string]uint64
	sessions           map[string]*cloudv1.ClientSessionSummary
	sessionClosers     map[string]sessionCloser
	pendingSignals     map[string]pendingSignal
	relayLeases        map[string]relayLease
	relayReservations  map[string]relayReservation
	allocations        map[string]relayAllocation
	allocationNextGen  map[string]uint64
	accountAllocations map[string]uint32
	leaseAllocations   map[string]uint32
	sessionAllocations map[string]uint32
	accountRates       map[string]*policy.RateLimiter
	sessionRates       map[string]*policy.RateLimiter
	subscribers        map[uint64]chan *cloudv1.RuntimeDelta
	nextSubscriber     uint64
	deltaBuffer        int
}

type pendingSignal struct {
	daemonID, sessionID string
	agentGeneration     uint64
	result              chan *cloudv1.AgentEvent
}

type agentWriter struct {
	generation uint64
	send       func(*cloudv1.EdgeCommand) bool
	close      func()
}

type sessionCloser struct {
	generation uint64
	close      func()
}

// NewState 启动单写者 actor；容量必须有界且大于零。
func NewState(config StateConfig) (*State, error) {
	if config.MailboxSize <= 0 || config.DeltaBuffer <= 0 {
		return nil, errors.New("runtime mailbox and delta buffer must be positive")
	}
	if config.MaxSessions <= 0 {
		config.MaxSessions = 4096
	}
	if config.MaxPendingSignals <= 0 {
		config.MaxPendingSignals = 4096
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	state := &State{
		mailbox: make(chan *stateRequest, config.MailboxSize), done: make(chan struct{}), now: config.Now,
		limits: stateLimits{maxSessions: config.MaxSessions, maxPendingSignals: config.MaxPendingSignals},
	}
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
		if clone.GetGeneration() > data.agentNextGen[clone.GetDaemonId()] {
			data.agentNextGen[clone.GetDaemonId()] = clone.GetGeneration()
		}
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentUpserted{AgentUpserted: proto.Clone(clone).(*cloudv1.AgentPresence)}})
		return nil
	})
}

// AttachAuthenticatedAgent 为已认证 AgentGateway 分配单调 generation，并保留 daemon 身份供 ClientGateway 离线验签。
func (state *State) AttachAuthenticatedAgent(ctx context.Context, agent *cloudv1.AgentPresence, claims *cloudv1.DaemonBindingClaims, send func(*cloudv1.EdgeCommand) bool, closeWriter func()) (uint64, error) {
	if agent == nil || claims == nil || claims.GetDaemonId() != agent.GetDaemonId() || claims.GetAccountId() != agent.GetAccountId() || len(claims.GetDevicePublicKey()) == 0 {
		return 0, errors.New("authenticated Agent claims do not match Presence")
	}
	claims = proto.Clone(claims).(*cloudv1.DaemonBindingClaims)
	if agent == nil || send == nil || closeWriter == nil {
		return 0, errors.New("authenticated agent and writer are required")
	}
	clone := proto.Clone(agent).(*cloudv1.AgentPresence)
	var generation uint64
	var oldClose func()
	if err := state.call(ctx, func(data *stateData) error {
		data.agentNextGen[clone.GetDaemonId()]++
		clone.Generation = data.agentNextGen[clone.GetDaemonId()]
		if _, err := runtimesnapshot.NormalizeClone(&cloudv1.RuntimeSnapshot{Agents: []*cloudv1.AgentPresence{clone}}); err != nil {
			return err
		}
		if old := data.agentWriters[clone.GetDaemonId()]; old.close != nil {
			oldClose = old.close
			data.cancelAgentSignals(clone.GetDaemonId(), old.generation)
		}
		data.agentWriters[clone.GetDaemonId()] = agentWriter{generation: clone.GetGeneration(), send: send, close: closeWriter}
		data.agents[clone.GetDaemonId()] = clone
		data.agentClaims[clone.GetDaemonId()] = proto.Clone(claims).(*cloudv1.DaemonBindingClaims)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentUpserted{AgentUpserted: proto.Clone(clone).(*cloudv1.AgentPresence)}})
		generation = clone.GetGeneration()
		return nil
	}); err != nil {
		return 0, err
	}
	if oldClose != nil {
		oldClose()
	}
	return generation, nil
}

// AuthenticatedAgentClaims 返回当前 AgentGateway generation 已验证的 daemon 身份，不访问 Controller。
func (state *State) AuthenticatedAgentClaims(ctx context.Context, daemonID string) (*cloudv1.DaemonBindingClaims, error) {
	daemonID = strings.TrimSpace(daemonID)
	if daemonID == "" {
		return nil, errors.New("daemon id is required")
	}
	var claims *cloudv1.DaemonBindingClaims
	if err := state.call(ctx, func(data *stateData) error {
		currentClaims := data.agentClaims[daemonID]
		agent := data.agents[daemonID]
		writer := data.agentWriters[daemonID]
		if currentClaims == nil || agent == nil || writer.send == nil || writer.generation != agent.GetGeneration() || currentClaims.GetExpiresAt() == nil || !currentClaims.GetExpiresAt().AsTime().After(state.now().UTC()) {
			return ErrStaleGeneration
		}
		claims = proto.Clone(currentClaims).(*cloudv1.DaemonBindingClaims)
		return nil
	}); err != nil {
		return nil, err
	}
	return claims, nil
}

// DetachAgent 删除精确 AgentGateway generation；迟到连接不能删除替换后的 Presence。
func (state *State) DetachAgent(ctx context.Context, daemonID string, generation uint64) error {
	return state.mutate(ctx, func(data *stateData) error {
		writer := data.agentWriters[daemonID]
		current := data.agents[daemonID]
		if current == nil || current.GetGeneration() != generation || writer.generation != generation {
			return ErrStaleGeneration
		}
		delete(data.agentWriters, daemonID)
		delete(data.agents, daemonID)
		delete(data.agentClaims, daemonID)
		data.cancelAgentSignals(daemonID, generation)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AgentRemoved{AgentRemoved: &cloudv1.AgentRemoved{DaemonId: daemonID, Generation: generation}}})
		return nil
	})
}

// SendAgentCommand 在 actor 内解析精确 generation，随后在 actor 外执行有界非阻塞入队。
func (state *State) SendAgentCommand(ctx context.Context, daemonID string, generation uint64, command *cloudv1.EdgeCommand) error {
	if command == nil {
		return errors.New("Edge command is required")
	}
	var send func(*cloudv1.EdgeCommand) bool
	if err := state.call(ctx, func(data *stateData) error {
		writer := data.agentWriters[daemonID]
		if writer.generation != generation || writer.send == nil {
			return ErrStaleGeneration
		}
		send = writer.send
		return nil
	}); err != nil {
		return err
	}
	if !send(proto.Clone(command).(*cloudv1.EdgeCommand)) {
		return errors.New("agent writer queue is unavailable")
	}
	return nil
}

// CloseAgentConnection 解析精确 generation 后在 actor 外关闭 AgentGateway writer。
func (state *State) CloseAgentConnection(ctx context.Context, daemonID string, generation uint64) error {
	var closeWriter func()
	if err := state.call(ctx, func(data *stateData) error {
		writer := data.agentWriters[daemonID]
		if writer.generation != generation || writer.close == nil {
			return ErrStaleGeneration
		}
		closeWriter = writer.close
		return nil
	}); err != nil {
		return err
	}
	closeWriter()
	return nil
}

// BeginAgentSignal 在 actor 内锁定 daemon 当前 generation 并登记有界一次性 correlation。
// 返回后网络发送与等待都必须在 actor 外执行，避免阻塞在线状态 owner。
func (state *State) BeginAgentSignal(ctx context.Context, correlationID, daemonID, sessionID string) (uint64, <-chan *cloudv1.AgentEvent, error) {
	correlationID, daemonID, sessionID = strings.TrimSpace(correlationID), strings.TrimSpace(daemonID), strings.TrimSpace(sessionID)
	if correlationID == "" || daemonID == "" || sessionID == "" {
		return 0, nil, errors.New("agent signal correlation, daemon, and session are required")
	}
	var generation uint64
	var response chan *cloudv1.AgentEvent
	if err := state.call(ctx, func(data *stateData) error {
		if _, exists := data.pendingSignals[correlationID]; exists {
			return errors.New("agent signal correlation already exists")
		}
		if len(data.pendingSignals) >= state.limits.maxPendingSignals {
			return errors.New("runtime pending signal capacity is exhausted")
		}
		agent := data.agents[daemonID]
		writer := data.agentWriters[daemonID]
		if agent == nil || writer.send == nil || writer.generation != agent.GetGeneration() {
			return ErrStaleGeneration
		}
		response = make(chan *cloudv1.AgentEvent, 1)
		data.pendingSignals[correlationID] = pendingSignal{daemonID: daemonID, sessionID: sessionID, agentGeneration: agent.GetGeneration(), result: response}
		generation = agent.GetGeneration()
		return nil
	}); err != nil {
		return 0, nil, err
	}
	return generation, response, nil
}

// ResolveAgentSignal 只允许当前 AgentGateway generation 完成其登记的 correlation。
func (state *State) ResolveAgentSignal(ctx context.Context, daemonID string, generation uint64, event *cloudv1.AgentEvent) error {
	if event == nil || boolCount(event.GetAnswer() != nil, event.GetRejected() != nil, event.GetAuthorization() != nil) != 1 {
		return errors.New("agent signal result is invalid")
	}
	correlationID, sessionID := "", ""
	if answer := event.GetAnswer(); answer != nil {
		correlationID, sessionID = answer.GetCorrelationId(), answer.GetSessionId()
	} else if rejected := event.GetRejected(); rejected != nil {
		correlationID, sessionID = event.GetRejected().GetCorrelationId(), event.GetRejected().GetSessionId()
	} else {
		correlationID, sessionID = event.GetAuthorization().GetCorrelationId(), event.GetAuthorization().GetSessionId()
	}
	return state.mutate(ctx, func(data *stateData) error {
		pending, ok := data.pendingSignals[correlationID]
		if !ok || pending.daemonID != daemonID || pending.sessionID != sessionID || pending.agentGeneration != generation {
			return ErrStaleGeneration
		}
		delete(data.pendingSignals, correlationID)
		pending.result <- proto.Clone(event).(*cloudv1.AgentEvent)
		close(pending.result)
		return nil
	})
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

// CancelAgentSignal 删除超时或客户端断开的精确 correlation；迟到 answer 会被 generation fence 拒绝。
func (state *State) CancelAgentSignal(ctx context.Context, correlationID string) error {
	return state.mutate(ctx, func(data *stateData) error {
		pending, ok := data.pendingSignals[strings.TrimSpace(correlationID)]
		if !ok {
			return nil
		}
		delete(data.pendingSignals, strings.TrimSpace(correlationID))
		close(pending.result)
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
		if data.sessions[clone.GetSessionId()] == nil && len(data.sessions) >= state.limits.maxSessions {
			return errors.New("runtime client session capacity is exhausted")
		}
		data.sessions[clone.GetSessionId()] = clone
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_SessionUpserted{SessionUpserted: proto.Clone(clone).(*cloudv1.ClientSessionSummary)}})
		return nil
	})
}

// RegisterSessionCloser 把 ClientGateway generation 的取消函数登记到同一 actor。
func (state *State) RegisterSessionCloser(ctx context.Context, sessionID string, generation uint64, closeSession func()) error {
	if closeSession == nil {
		return errors.New("session closer is required")
	}
	return state.mutate(ctx, func(data *stateData) error {
		current := data.sessions[sessionID]
		if current == nil || current.GetGeneration() != generation {
			return ErrStaleGeneration
		}
		data.sessionClosers[sessionID] = sessionCloser{generation: generation, close: closeSession}
		return nil
	})
}

// CloseSession 解析精确 generation 后在 actor 外取消 ClientGateway stream。
func (state *State) CloseSession(ctx context.Context, sessionID string, generation uint64) error {
	var closeSession func()
	if err := state.call(ctx, func(data *stateData) error {
		current := data.sessions[sessionID]
		closer := data.sessionClosers[sessionID]
		if current == nil || current.GetGeneration() != generation || closer.generation != generation || closer.close == nil {
			return ErrStaleGeneration
		}
		closeSession = closer.close
		return nil
	}); err != nil {
		return err
	}
	closeSession()
	return nil
}

// RemoveSession 仅删除精确 generation 的客户端信令会话。
func (state *State) RemoveSession(ctx context.Context, sessionID string, generation uint64) error {
	return state.mutate(ctx, func(data *stateData) error {
		current := data.sessions[sessionID]
		if current == nil || current.GetGeneration() != generation {
			return ErrStaleGeneration
		}
		delete(data.sessions, sessionID)
		delete(data.sessionClosers, sessionID)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_SessionRemoved{SessionRemoved: &cloudv1.ClientSessionRemoved{SessionId: sessionID, Generation: generation}}})
		return nil
	})
}

// OpenFeed 返回原子快照和后续增量；增量缓冲溢出时 channel 会关闭，ControllerLink 必须重连重建。
func (state *State) OpenFeed(ctx context.Context) (*Feed, error) {
	var feed *Feed
	err := state.call(ctx, func(data *stateData) error {
		snapshot, snapshotErr := data.snapshot()
		if snapshotErr != nil {
			return snapshotErr
		}
		data.nextSubscriber++
		id := data.nextSubscriber
		channel := make(chan *cloudv1.RuntimeDelta, data.deltaBuffer)
		data.subscribers[id] = channel
		feed = &Feed{Snapshot: snapshot, Deltas: channel, close: func() { state.unsubscribe(id) }}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return feed, nil
}

// Snapshot 返回当前不可变投影，供状态查询和测试使用。
func (state *State) Snapshot(ctx context.Context) (*cloudv1.RuntimeSnapshot, error) {
	var snapshot *cloudv1.RuntimeSnapshot
	if err := state.call(ctx, func(data *stateData) error {
		var err error
		snapshot, err = data.snapshot()
		return err
	}); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// Close 停止 actor 并关闭全部 feed；重复调用安全。
func (state *State) Close() {
	state.closeOnce.Do(func() {
		state.gate.Lock()
		state.closing.Store(true)
		state.gate.Unlock()

		var closers []func()
		request := &stateRequest{result: make(chan error, 1), stop: true, run: func(data *stateData) error {
			for id, writer := range data.agentWriters {
				if writer.close != nil {
					closers = append(closers, writer.close)
				}
				delete(data.agentWriters, id)
			}
			for id, session := range data.sessionClosers {
				if session.close != nil {
					closers = append(closers, session.close)
				}
				delete(data.sessionClosers, id)
			}
			for correlationID, pending := range data.pendingSignals {
				close(pending.result)
				delete(data.pendingSignals, correlationID)
			}
			for id, subscriber := range data.subscribers {
				close(subscriber)
				delete(data.subscribers, id)
			}
			return nil
		}}
		state.mailbox <- request
		<-request.result
		<-state.done

		var wait sync.WaitGroup
		wait.Add(len(closers))
		for _, closeOwned := range closers {
			go func() {
				defer wait.Done()
				closeOwned()
			}()
		}
		wait.Wait()
	})
}

func (state *State) run(deltaBuffer int) {
	defer close(state.done)
	data := &stateData{
		agents: make(map[string]*cloudv1.AgentPresence), agentClaims: make(map[string]*cloudv1.DaemonBindingClaims), agentWriters: make(map[string]agentWriter), agentNextGen: make(map[string]uint64), sessions: make(map[string]*cloudv1.ClientSessionSummary), sessionClosers: make(map[string]sessionCloser), pendingSignals: make(map[string]pendingSignal),
		relayLeases: make(map[string]relayLease), relayReservations: make(map[string]relayReservation), allocations: make(map[string]relayAllocation), allocationNextGen: make(map[string]uint64),
		accountAllocations: make(map[string]uint32), leaseAllocations: make(map[string]uint32), sessionAllocations: make(map[string]uint32),
		accountRates: make(map[string]*policy.RateLimiter), sessionRates: make(map[string]*policy.RateLimiter),
		subscribers: make(map[uint64]chan *cloudv1.RuntimeDelta), deltaBuffer: deltaBuffer,
	}
	for request := range state.mailbox {
		if !request.lifecycle.CompareAndSwap(requestQueued, requestExecuting) {
			continue
		}
		request.result <- request.run(data)
		if request.stop {
			return
		}
	}
}

func (data *stateData) cancelAgentSignals(daemonID string, generation uint64) {
	for correlationID, pending := range data.pendingSignals {
		if pending.daemonID == daemonID && pending.agentGeneration == generation {
			delete(data.pendingSignals, correlationID)
			close(pending.result)
		}
	}
}

func (state *State) mutate(ctx context.Context, mutation func(*stateData) error) error {
	return state.call(ctx, mutation)
}

func (state *State) call(ctx context.Context, run func(*stateData) error) error {
	request := &stateRequest{run: run, result: make(chan error, 1)}
	state.gate.RLock()
	if state.closing.Load() {
		state.gate.RUnlock()
		return ErrStateClosed
	}
	select {
	case <-ctx.Done():
		state.gate.RUnlock()
		return ctx.Err()
	case <-state.done:
		state.gate.RUnlock()
		return ErrStateClosed
	case state.mailbox <- request:
		state.gate.RUnlock()
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		if request.lifecycle.CompareAndSwap(requestQueued, requestCanceled) {
			err := ctx.Err()
			request.result <- err
			return err
		}
		return <-request.result
	}
}

func (state *State) unsubscribe(id uint64) {
	_ = state.call(context.Background(), func(data *stateData) error {
		if subscriber := data.subscribers[id]; subscriber != nil {
			close(subscriber)
			delete(data.subscribers, id)
		}
		return nil
	})
}

func (data *stateData) snapshot() (*cloudv1.RuntimeSnapshot, error) {
	snapshot := &cloudv1.RuntimeSnapshot{Revision: data.revision, Agents: make([]*cloudv1.AgentPresence, 0, len(data.agents)), Sessions: make([]*cloudv1.ClientSessionSummary, 0, len(data.sessions)), Allocations: make([]*cloudv1.RelayAllocationSummary, 0, len(data.allocations))}
	for _, agent := range data.agents {
		snapshot.Agents = append(snapshot.Agents, proto.Clone(agent).(*cloudv1.AgentPresence))
	}
	for _, session := range data.sessions {
		snapshot.Sessions = append(snapshot.Sessions, proto.Clone(session).(*cloudv1.ClientSessionSummary))
	}
	for _, allocation := range data.allocations {
		snapshot.Allocations = append(snapshot.Allocations, proto.Clone(allocation.summary).(*cloudv1.RelayAllocationSummary))
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
