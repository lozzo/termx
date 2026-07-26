// Package directory 实现 Controller 的纯内存实时目录 actor。
// PostgreSQL 不参与 Presence、session、Edge generation 或快照恢复。
package directory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/runtimesnapshot"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrClosed 表示 Directory actor 已关闭。
	ErrClosed = errors.New("Controller Directory is closed")
	// ErrStaleConnection 表示消息不属于当前已登记的 Edge connection generation。
	ErrStaleConnection = errors.New("Edge connection generation is stale")
)

// SyncError 表示控制流必须发送 ResyncRequired，不能继续应用已知有缺口的增量。
type SyncError struct {
	ExpectedRevision uint64
	Reason           string
}

func (err *SyncError) Error() string {
	return fmt.Sprintf("runtime resync required at revision %d: %s", err.ExpectedRevision, err.Reason)
}

// Config 定义 actor mailbox 与 Edge 断线后的 generation 整体清理宽限期。
type Config struct {
	MailboxSize        int
	GracePeriod        time.Duration
	WatcherMailboxSize int
}

// Attachment 是通过 mTLS 和 EdgeHello 验证后的连接身份，不包含持久 Edge 配置。
type Attachment struct {
	EdgeID          string
	BootID          string
	ConnectionID    string
	SoftwareVersion string
	ConnectedAt     time.Time
}

// EdgeProjection 是管理/API 层可读取的不可变实时投影。
type EdgeProjection struct {
	Attachment
	RuntimeRevision      uint64
	AgentCount           int
	SessionCount         int
	RelayAllocationCount int
	LastHeartbeat        time.Time
}

// ObjectLocation 表示实时对象当前由哪个 Edge generation 持有。
type ObjectLocation struct {
	EdgeID       string
	BootID       string
	ConnectionID string
	Generation   uint64
}

// SessionProjection 是运营 API 可读取的实时 session 与精确 Edge generation 投影。
type SessionProjection struct {
	Session     *cloudv1.ClientSessionSummary
	Location    ObjectLocation
	ConnectedAt time.Time
	Relay       bool
}

// Directory 是 Controller 在线 Edge、daemon 和 session 的唯一 owner。
type Directory struct {
	mailbox            chan request
	done               chan struct{}
	closing            atomic.Bool
	closeOnce          sync.Once
	grace              time.Duration
	instanceID         string
	watcherMailboxSize int
}

type request struct{ run func(*directoryState) }

type directoryState struct {
	connections map[string]*connectionState
	current     map[string]string
	daemons     map[string]ObjectLocation
	sessions    map[string]ObjectLocation
	pending     map[string]pendingCommand
	watchers    map[uint64]chan *cloudv1.OperatorRuntimeEvent
	nextWatcher uint64
	eventSeq    uint64
	instanceID  string
}

type pendingCommand struct {
	location ObjectLocation
	result   chan *cloudv1.EdgeCommandResult
}

type connectionState struct {
	attachment    Attachment
	snapshot      *cloudv1.RuntimeSnapshot
	staging       *snapshotBuilder
	lastHeartbeat time.Time
}

type snapshotBuilder struct {
	id          string
	revision    uint64
	nextChunk   uint32
	agents      []*cloudv1.AgentPresence
	sessions    []*cloudv1.ClientSessionSummary
	allocations []*cloudv1.RelayAllocationSummary
}

// New 创建空 Directory；Controller 重启时不得从数据库恢复任何在线对象。
func New(config Config) (*Directory, error) {
	if config.MailboxSize <= 0 || config.GracePeriod < 0 {
		return nil, errors.New("Directory mailbox must be positive and grace period cannot be negative")
	}
	if config.WatcherMailboxSize == 0 {
		config.WatcherMailboxSize = 128
	}
	if config.WatcherMailboxSize < 0 {
		return nil, errors.New("Directory watcher mailbox cannot be negative")
	}
	directory := &Directory{mailbox: make(chan request, config.MailboxSize), done: make(chan struct{}), grace: config.GracePeriod, instanceID: uuid.NewString(), watcherMailboxSize: config.WatcherMailboxSize}
	go directory.run()
	return directory, nil
}

// Attach 登记已认证控制流，但在合法快照提交前不把 Edge 发布为在线。
func (directory *Directory) Attach(ctx context.Context, attachment Attachment) error {
	attachment.EdgeID = strings.TrimSpace(attachment.EdgeID)
	attachment.BootID = strings.TrimSpace(attachment.BootID)
	attachment.ConnectionID = strings.TrimSpace(attachment.ConnectionID)
	attachment.SoftwareVersion = strings.TrimSpace(attachment.SoftwareVersion)
	if attachment.EdgeID == "" || attachment.BootID == "" || attachment.ConnectionID == "" || attachment.SoftwareVersion == "" {
		return errors.New("Edge attachment identity and software version are required")
	}
	if attachment.ConnectedAt.IsZero() {
		attachment.ConnectedAt = time.Now().UTC()
	}
	return directory.mutate(ctx, func(state *directoryState) error {
		if _, exists := state.connections[attachment.ConnectionID]; exists {
			return errors.New("Edge connection already attached")
		}
		state.connections[attachment.ConnectionID] = &connectionState{attachment: attachment}
		return nil
	})
}

// BeginSnapshot 创建当前 connection 的临时命名空间；旧投影在提交前保持不变。
func (directory *Directory) BeginSnapshot(ctx context.Context, connectionID string, begin *cloudv1.SnapshotBegin) error {
	return directory.mutate(ctx, func(state *directoryState) error {
		connection, err := requireConnection(state, connectionID)
		if err != nil {
			return err
		}
		if begin == nil || strings.TrimSpace(begin.GetSnapshotId()) == "" {
			return &SyncError{ExpectedRevision: connection.currentRevision(), Reason: "snapshot identity is required"}
		}
		connection.staging = &snapshotBuilder{id: begin.GetSnapshotId(), revision: begin.GetRevision()}
		return nil
	})
}

// AppendSnapshot 验证 chunk identity 和严格递增索引后追加到临时命名空间。
func (directory *Directory) AppendSnapshot(ctx context.Context, connectionID string, chunk *cloudv1.SnapshotChunk) error {
	return directory.mutate(ctx, func(state *directoryState) error {
		connection, err := requireConnection(state, connectionID)
		if err != nil {
			return err
		}
		if chunk == nil || connection.staging == nil || chunk.GetSnapshotId() != connection.staging.id || chunk.GetChunkIndex() != connection.staging.nextChunk {
			return &SyncError{ExpectedRevision: connection.currentRevision(), Reason: "snapshot chunk identity or sequence mismatch"}
		}
		connection.staging.nextChunk++
		for _, agent := range chunk.GetAgents() {
			connection.staging.agents = append(connection.staging.agents, proto.Clone(agent).(*cloudv1.AgentPresence))
		}
		for _, session := range chunk.GetSessions() {
			connection.staging.sessions = append(connection.staging.sessions, proto.Clone(session).(*cloudv1.ClientSessionSummary))
		}
		for _, allocation := range chunk.GetAllocations() {
			connection.staging.allocations = append(connection.staging.allocations, proto.Clone(allocation).(*cloudv1.RelayAllocationSummary))
		}
		return nil
	})
}

// CommitSnapshot 校验 digest 后原子替换 Edge generation 及其全部反向索引。
func (directory *Directory) CommitSnapshot(ctx context.Context, connectionID string, end *cloudv1.SnapshotEnd) error {
	return directory.mutate(ctx, func(state *directoryState) error {
		connection, err := requireConnection(state, connectionID)
		if err != nil {
			return err
		}
		builder := connection.staging
		if end == nil || builder == nil || end.GetSnapshotId() != builder.id || end.GetRevision() != builder.revision || end.GetChunkCount() != builder.nextChunk {
			connection.staging = nil
			return &SyncError{ExpectedRevision: connection.currentRevision(), Reason: "snapshot end does not match begin/chunks"}
		}
		snapshot, err := runtimesnapshot.NormalizeClone(&cloudv1.RuntimeSnapshot{Revision: builder.revision, Agents: builder.agents, Sessions: builder.sessions, Allocations: builder.allocations})
		if err != nil {
			connection.staging = nil
			return &SyncError{ExpectedRevision: connection.currentRevision(), Reason: err.Error()}
		}
		digest, err := runtimesnapshot.Digest(snapshot)
		if err != nil || !bytes.Equal(digest, end.GetDigest()) {
			connection.staging = nil
			return &SyncError{ExpectedRevision: connection.currentRevision(), Reason: "snapshot digest mismatch"}
		}
		if oldID := state.current[connection.attachment.EdgeID]; oldID != "" && oldID != connectionID {
			state.removeIndexes(oldID)
		}
		state.removeIndexes(connectionID)
		connection.snapshot = snapshot
		connection.staging = nil
		connection.lastHeartbeat = time.Now().UTC()
		state.current[connection.attachment.EdgeID] = connectionID
		state.addIndexes(connectionID)
		state.publish("edge", connection.attachment.EdgeID, cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_RESET)
		return nil
	})
}

// ApplyDelta 只接受当前已发布 generation 的下一个严格连续 revision。
func (directory *Directory) ApplyDelta(ctx context.Context, connectionID string, delta *cloudv1.RuntimeDelta) error {
	if err := runtimesnapshot.ValidateDelta(delta); err != nil {
		return err
	}
	return directory.mutate(ctx, func(state *directoryState) error {
		connection, err := requireCurrent(state, connectionID)
		if err != nil {
			return err
		}
		expected := connection.snapshot.GetRevision() + 1
		if delta.GetRevision() != expected {
			return &SyncError{ExpectedRevision: expected, Reason: "runtime delta revision is not contiguous"}
		}
		if err := state.applyDelta(connectionID, connection, delta); err != nil {
			return &SyncError{ExpectedRevision: expected, Reason: err.Error()}
		}
		connection.snapshot.Revision = delta.GetRevision()
		kind, id, operation := runtimeDeltaEvent(delta)
		state.publish(kind, id, operation)
		return nil
	})
}

// Heartbeat 更新当前 generation 的观测时间，不把瞬时指标写入数据库。
func (directory *Directory) Heartbeat(ctx context.Context, connectionID string, heartbeat *cloudv1.EdgeHeartbeat) error {
	return directory.mutate(ctx, func(state *directoryState) error {
		connection, err := requireCurrent(state, connectionID)
		if err != nil {
			return err
		}
		if heartbeat == nil || heartbeat.GetRuntimeRevision() != connection.currentRevision() {
			return &SyncError{ExpectedRevision: connection.currentRevision() + 1, Reason: "heartbeat runtime revision mismatch"}
		}
		connection.lastHeartbeat = time.Now().UTC()
		return nil
	})
}

// Detach 在 grace 后整体删除当前 connection generation；新 generation 提交后旧定时器无权删除它。
func (directory *Directory) Detach(connectionID string) {
	time.AfterFunc(directory.grace, func() {
		_ = directory.submit(context.Background(), func(state *directoryState) {
			connection := state.connections[connectionID]
			if connection == nil {
				return
			}
			if state.current[connection.attachment.EdgeID] == connectionID {
				state.removeIndexes(connectionID)
				delete(state.current, connection.attachment.EdgeID)
				state.publish("edge", connection.attachment.EdgeID, cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_DELETE)
			}
			delete(state.connections, connectionID)
		})
	})
}

// Edge 返回指定 Edge 的当前不可变实时投影。
func (directory *Directory) Edge(ctx context.Context, edgeID string) (EdgeProjection, bool, error) {
	type result struct {
		projection EdgeProjection
		found      bool
	}
	reply := make(chan result, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		connection := state.connections[state.current[edgeID]]
		if connection == nil || connection.snapshot == nil {
			reply <- result{}
			return
		}
		reply <- result{projection: connection.projection(), found: true}
	}); err != nil {
		return EdgeProjection{}, false, err
	}
	select {
	case <-ctx.Done():
		return EdgeProjection{}, false, ctx.Err()
	case <-directory.done:
		return EdgeProjection{}, false, ErrClosed
	case value := <-reply:
		return value.projection, value.found, nil
	}
}

// ListEdges 返回按 Edge ID 排序的当前在线投影，供 R3 管理 API 使用。
func (directory *Directory) ListEdges(ctx context.Context) ([]EdgeProjection, error) {
	reply := make(chan []EdgeProjection, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		result := make([]EdgeProjection, 0, len(state.current))
		for _, connectionID := range state.current {
			if connection := state.connections[connectionID]; connection != nil && connection.snapshot != nil {
				result = append(result, connection.projection())
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].EdgeID < result[j].EdgeID })
		reply <- result
	}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-directory.done:
		return nil, ErrClosed
	case result := <-reply:
		return result, nil
	}
}

// LocateDaemon 查询 daemon 当前 Edge generation；不存在时不回退数据库。
func (directory *Directory) LocateDaemon(ctx context.Context, daemonID string) (ObjectLocation, bool, error) {
	return directory.locate(ctx, daemonID, true)
}

// LocateSession 查询客户端信令 session 当前 Edge generation；不存在时不从数据库恢复。
func (directory *Directory) LocateSession(ctx context.Context, sessionID string) (ObjectLocation, bool, error) {
	return directory.locate(ctx, sessionID, false)
}

// Session 返回当前信令 session 的不可变摘要和所属 Edge generation。
// Controller 签发 RelayLease 时使用该摘要校验 Edge 请求，不能信任请求里重复携带的账号字段。
func (directory *Directory) Session(ctx context.Context, sessionID string) (*cloudv1.ClientSessionSummary, ObjectLocation, bool, error) {
	type result struct {
		session  *cloudv1.ClientSessionSummary
		location ObjectLocation
		found    bool
	}
	reply := make(chan result, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		location, found := state.sessions[strings.TrimSpace(sessionID)]
		if !found {
			reply <- result{}
			return
		}
		connection := state.connections[location.ConnectionID]
		if connection == nil || connection.snapshot == nil || state.current[location.EdgeID] != location.ConnectionID {
			reply <- result{}
			return
		}
		for _, session := range connection.snapshot.GetSessions() {
			if session.GetSessionId() == strings.TrimSpace(sessionID) && session.GetGeneration() == location.Generation {
				reply <- result{session: proto.Clone(session).(*cloudv1.ClientSessionSummary), location: location, found: true}
				return
			}
		}
		reply <- result{}
	}); err != nil {
		return nil, ObjectLocation{}, false, err
	}
	select {
	case <-ctx.Done():
		return nil, ObjectLocation{}, false, ctx.Err()
	case <-directory.done:
		return nil, ObjectLocation{}, false, ErrClosed
	case value := <-reply:
		return value.session, value.location, value.found, nil
	}
}

// ListSessions 返回全部当前 session 的不可变快照；对象离线后不会从数据库补回。
func (directory *Directory) ListSessions(ctx context.Context) ([]SessionProjection, error) {
	reply := make(chan []SessionProjection, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		result := make([]SessionProjection, 0, len(state.sessions))
		for _, connectionID := range state.current {
			connection := state.connections[connectionID]
			if connection == nil || connection.snapshot == nil {
				continue
			}
			relaySessions := make(map[string]bool, len(connection.snapshot.GetAllocations()))
			for _, allocation := range connection.snapshot.GetAllocations() {
				relaySessions[allocation.GetSessionId()] = true
			}
			for _, session := range connection.snapshot.GetSessions() {
				location := state.sessions[session.GetSessionId()]
				result = append(result, SessionProjection{Session: proto.Clone(session).(*cloudv1.ClientSessionSummary), Location: location, ConnectedAt: connection.attachment.ConnectedAt, Relay: relaySessions[session.GetSessionId()]})
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Session.GetSessionId() < result[j].Session.GetSessionId() })
		reply <- result
	}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-directory.done:
		return nil, ErrClosed
	case result := <-reply:
		return result, nil
	}
}

// BeginCommand 在当前对象 generation 上登记一次有界 waiter；命令本身不持久化。
func (directory *Directory) BeginCommand(ctx context.Context, correlationID, objectID string, generation uint64, daemon bool) (ObjectLocation, <-chan *cloudv1.EdgeCommandResult, error) {
	type result struct {
		location ObjectLocation
		waiter   chan *cloudv1.EdgeCommandResult
		err      error
	}
	reply := make(chan result, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		if correlationID == "" || objectID == "" || generation == 0 {
			reply <- result{err: errors.New("command correlation, object, and generation are required")}
			return
		}
		if _, exists := state.pending[correlationID]; exists {
			reply <- result{err: errors.New("command correlation already exists")}
			return
		}
		location, found := state.sessions[objectID]
		if daemon {
			location, found = state.daemons[objectID]
		}
		if !found || location.Generation != generation {
			reply <- result{err: ErrStaleConnection}
			return
		}
		waiter := make(chan *cloudv1.EdgeCommandResult, 1)
		state.pending[correlationID] = pendingCommand{location: location, result: waiter}
		reply <- result{location: location, waiter: waiter}
	}); err != nil {
		return ObjectLocation{}, nil, err
	}
	select {
	case <-ctx.Done():
		return ObjectLocation{}, nil, ctx.Err()
	case <-directory.done:
		return ObjectLocation{}, nil, ErrClosed
	case value := <-reply:
		return value.location, value.waiter, value.err
	}
}

// CompleteCommand 只允许 owning Edge connection 完成当前 correlation。
func (directory *Directory) CompleteCommand(ctx context.Context, connectionID string, result *cloudv1.EdgeCommandResult) error {
	return directory.mutate(ctx, func(state *directoryState) error {
		if result == nil || result.GetCorrelationId() == "" {
			return errors.New("command result is required")
		}
		pending, ok := state.pending[result.GetCorrelationId()]
		if !ok || pending.location.ConnectionID != connectionID {
			return ErrStaleConnection
		}
		delete(state.pending, result.GetCorrelationId())
		pending.result <- proto.Clone(result).(*cloudv1.EdgeCommandResult)
		close(pending.result)
		return nil
	})
}

// CancelCommand 删除 HTTP 取消或超时的 waiter；迟到结果不能命中新请求。
func (directory *Directory) CancelCommand(correlationID string) {
	_ = directory.submit(context.Background(), func(state *directoryState) {
		if pending, ok := state.pending[correlationID]; ok {
			delete(state.pending, correlationID)
			close(pending.result)
		}
	})
}

// Watch 订阅有界 SSE 失效提示；消费者过慢时 channel 关闭并要求重新拉 snapshot。
func (directory *Directory) Watch(ctx context.Context) (<-chan *cloudv1.OperatorRuntimeEvent, func(), error) {
	type result struct {
		id     uint64
		events chan *cloudv1.OperatorRuntimeEvent
	}
	reply := make(chan result, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		state.nextWatcher++
		events := make(chan *cloudv1.OperatorRuntimeEvent, directory.watcherMailboxSize)
		state.watchers[state.nextWatcher] = events
		reply <- result{id: state.nextWatcher, events: events}
	}); err != nil {
		return nil, nil, err
	}
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-directory.done:
		return nil, nil, ErrClosed
	case value := <-reply:
		var once sync.Once
		cancel := func() {
			once.Do(func() {
				_ = directory.submit(context.Background(), func(state *directoryState) {
					if events, ok := state.watchers[value.id]; ok {
						delete(state.watchers, value.id)
						close(events)
					}
				})
			})
		}
		return value.events, cancel, nil
	}
}

// InstanceID 返回当前 Controller 进程的 SSE generation。
func (directory *Directory) InstanceID() string { return directory.instanceID }

// Close 停止 actor，Directory 内容随 Controller 进程一起消失。
func (directory *Directory) Close() {
	directory.closeOnce.Do(func() {
		directory.closing.Store(true)
		ack := make(chan struct{})
		select {
		case directory.mailbox <- request{run: func(*directoryState) { close(ack) }}:
			<-ack
		case <-directory.done:
		}
		close(directory.done)
	})
}

func (directory *Directory) run() {
	state := &directoryState{connections: make(map[string]*connectionState), current: make(map[string]string), daemons: make(map[string]ObjectLocation), sessions: make(map[string]ObjectLocation), pending: make(map[string]pendingCommand), watchers: make(map[uint64]chan *cloudv1.OperatorRuntimeEvent), instanceID: directory.instanceID}
	for {
		select {
		case <-directory.done:
			return
		case request := <-directory.mailbox:
			request.run(state)
		}
	}
}

func (state *directoryState) publish(kind, id string, operation cloudv1.OperatorEventOperation) {
	if kind == "" || id == "" {
		return
	}
	state.eventSeq++
	event := &cloudv1.OperatorRuntimeEvent{ControllerInstanceId: state.instanceID, EventSeq: state.eventSeq, ResourceKind: kind, ResourceId: id, Operation: operation, OccurredAt: timestamppb.Now()}
	for watcher, mailbox := range state.watchers {
		clone := proto.Clone(event).(*cloudv1.OperatorRuntimeEvent)
		select {
		case mailbox <- clone:
		default:
			close(mailbox)
			delete(state.watchers, watcher)
		}
	}
}

func runtimeDeltaEvent(delta *cloudv1.RuntimeDelta) (string, string, cloudv1.OperatorEventOperation) {
	switch value := delta.GetChange().(type) {
	case *cloudv1.RuntimeDelta_AgentUpserted:
		return "daemon", value.AgentUpserted.GetDaemonId(), cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_UPSERT
	case *cloudv1.RuntimeDelta_AgentRemoved:
		return "daemon", value.AgentRemoved.GetDaemonId(), cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_DELETE
	case *cloudv1.RuntimeDelta_SessionUpserted:
		return "session", value.SessionUpserted.GetSessionId(), cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_UPSERT
	case *cloudv1.RuntimeDelta_SessionRemoved:
		return "session", value.SessionRemoved.GetSessionId(), cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_DELETE
	case *cloudv1.RuntimeDelta_AllocationUpserted:
		return "allocation", value.AllocationUpserted.GetAllocationId(), cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_UPSERT
	case *cloudv1.RuntimeDelta_AllocationRemoved:
		return "allocation", value.AllocationRemoved.GetAllocationId(), cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_DELETE
	default:
		return "", "", cloudv1.OperatorEventOperation_OPERATOR_EVENT_OPERATION_UNSPECIFIED
	}
}

func (directory *Directory) mutate(ctx context.Context, mutation func(*directoryState) error) error {
	reply := make(chan error, 1)
	if err := directory.submit(ctx, func(state *directoryState) { reply <- mutation(state) }); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-directory.done:
		return ErrClosed
	case err := <-reply:
		return err
	}
}

func (directory *Directory) submit(ctx context.Context, run func(*directoryState)) error {
	if directory.closing.Load() {
		return ErrClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-directory.done:
		return ErrClosed
	case directory.mailbox <- request{run: run}:
		return nil
	}
}

func (directory *Directory) locate(ctx context.Context, id string, daemon bool) (ObjectLocation, bool, error) {
	type result struct {
		location ObjectLocation
		found    bool
	}
	reply := make(chan result, 1)
	if err := directory.submit(ctx, func(state *directoryState) {
		var location ObjectLocation
		var found bool
		if daemon {
			location, found = state.daemons[id]
		} else {
			location, found = state.sessions[id]
		}
		reply <- result{location, found}
	}); err != nil {
		return ObjectLocation{}, false, err
	}
	select {
	case <-ctx.Done():
		return ObjectLocation{}, false, ctx.Err()
	case <-directory.done:
		return ObjectLocation{}, false, ErrClosed
	case value := <-reply:
		return value.location, value.found, nil
	}
}

func requireConnection(state *directoryState, connectionID string) (*connectionState, error) {
	connection := state.connections[connectionID]
	if connection == nil {
		return nil, ErrStaleConnection
	}
	return connection, nil
}

func requireCurrent(state *directoryState, connectionID string) (*connectionState, error) {
	connection, err := requireConnection(state, connectionID)
	if err != nil || state.current[connection.attachment.EdgeID] != connectionID || connection.snapshot == nil {
		return nil, ErrStaleConnection
	}
	return connection, nil
}

func (connection *connectionState) currentRevision() uint64 {
	if connection.snapshot == nil {
		return 0
	}
	return connection.snapshot.GetRevision()
}

func (connection *connectionState) projection() EdgeProjection {
	return EdgeProjection{Attachment: connection.attachment, RuntimeRevision: connection.snapshot.GetRevision(), AgentCount: len(connection.snapshot.GetAgents()), SessionCount: len(connection.snapshot.GetSessions()), RelayAllocationCount: len(connection.snapshot.GetAllocations()), LastHeartbeat: connection.lastHeartbeat}
}

func (state *directoryState) removeIndexes(connectionID string) {
	connection := state.connections[connectionID]
	if connection == nil || connection.snapshot == nil {
		return
	}
	for _, agent := range connection.snapshot.GetAgents() {
		if state.daemons[agent.GetDaemonId()].ConnectionID == connectionID {
			delete(state.daemons, agent.GetDaemonId())
		}
	}
	for _, session := range connection.snapshot.GetSessions() {
		if state.sessions[session.GetSessionId()].ConnectionID == connectionID {
			delete(state.sessions, session.GetSessionId())
		}
	}
}

func (state *directoryState) addIndexes(connectionID string) {
	connection := state.connections[connectionID]
	for _, agent := range connection.snapshot.GetAgents() {
		state.daemons[agent.GetDaemonId()] = ObjectLocation{EdgeID: connection.attachment.EdgeID, BootID: connection.attachment.BootID, ConnectionID: connectionID, Generation: agent.GetGeneration()}
	}
	for _, session := range connection.snapshot.GetSessions() {
		state.sessions[session.GetSessionId()] = ObjectLocation{EdgeID: connection.attachment.EdgeID, BootID: connection.attachment.BootID, ConnectionID: connectionID, Generation: session.GetGeneration()}
	}
}

func (state *directoryState) applyDelta(connectionID string, connection *connectionState, delta *cloudv1.RuntimeDelta) error {
	switch change := delta.GetChange().(type) {
	case *cloudv1.RuntimeDelta_AgentUpserted:
		agent := change.AgentUpserted
		for index, current := range connection.snapshot.Agents {
			if current.GetDaemonId() == agent.GetDaemonId() {
				if agent.GetGeneration() < current.GetGeneration() {
					return errors.New("agent generation regressed")
				}
				connection.snapshot.Agents[index] = proto.Clone(agent).(*cloudv1.AgentPresence)
				state.daemons[agent.GetDaemonId()] = ObjectLocation{EdgeID: connection.attachment.EdgeID, BootID: connection.attachment.BootID, ConnectionID: connectionID, Generation: agent.GetGeneration()}
				return nil
			}
		}
		connection.snapshot.Agents = append(connection.snapshot.Agents, proto.Clone(agent).(*cloudv1.AgentPresence))
		state.daemons[agent.GetDaemonId()] = ObjectLocation{EdgeID: connection.attachment.EdgeID, BootID: connection.attachment.BootID, ConnectionID: connectionID, Generation: agent.GetGeneration()}
	case *cloudv1.RuntimeDelta_AgentRemoved:
		for index, current := range connection.snapshot.Agents {
			if current.GetDaemonId() == change.AgentRemoved.GetDaemonId() && current.GetGeneration() == change.AgentRemoved.GetGeneration() {
				connection.snapshot.Agents = append(connection.snapshot.Agents[:index], connection.snapshot.Agents[index+1:]...)
				if state.daemons[current.GetDaemonId()].ConnectionID == connectionID {
					delete(state.daemons, current.GetDaemonId())
				}
				return nil
			}
		}
		return errors.New("removed agent generation is not current")
	case *cloudv1.RuntimeDelta_SessionUpserted:
		session := change.SessionUpserted
		for index, current := range connection.snapshot.Sessions {
			if current.GetSessionId() == session.GetSessionId() {
				if session.GetGeneration() < current.GetGeneration() {
					return errors.New("session generation regressed")
				}
				connection.snapshot.Sessions[index] = proto.Clone(session).(*cloudv1.ClientSessionSummary)
				state.sessions[session.GetSessionId()] = ObjectLocation{EdgeID: connection.attachment.EdgeID, BootID: connection.attachment.BootID, ConnectionID: connectionID, Generation: session.GetGeneration()}
				return nil
			}
		}
		connection.snapshot.Sessions = append(connection.snapshot.Sessions, proto.Clone(session).(*cloudv1.ClientSessionSummary))
		state.sessions[session.GetSessionId()] = ObjectLocation{EdgeID: connection.attachment.EdgeID, BootID: connection.attachment.BootID, ConnectionID: connectionID, Generation: session.GetGeneration()}
	case *cloudv1.RuntimeDelta_SessionRemoved:
		for index, current := range connection.snapshot.Sessions {
			if current.GetSessionId() == change.SessionRemoved.GetSessionId() && current.GetGeneration() == change.SessionRemoved.GetGeneration() {
				connection.snapshot.Sessions = append(connection.snapshot.Sessions[:index], connection.snapshot.Sessions[index+1:]...)
				if state.sessions[current.GetSessionId()].ConnectionID == connectionID {
					delete(state.sessions, current.GetSessionId())
				}
				return nil
			}
		}
		return errors.New("removed session generation is not current")
	case *cloudv1.RuntimeDelta_AllocationUpserted:
		allocation := change.AllocationUpserted
		for index, current := range connection.snapshot.Allocations {
			if current.GetAllocationId() == allocation.GetAllocationId() {
				if allocation.GetGeneration() < current.GetGeneration() {
					return errors.New("Relay allocation generation regressed")
				}
				connection.snapshot.Allocations[index] = proto.Clone(allocation).(*cloudv1.RelayAllocationSummary)
				return nil
			}
		}
		connection.snapshot.Allocations = append(connection.snapshot.Allocations, proto.Clone(allocation).(*cloudv1.RelayAllocationSummary))
	case *cloudv1.RuntimeDelta_AllocationRemoved:
		for index, current := range connection.snapshot.Allocations {
			if current.GetAllocationId() == change.AllocationRemoved.GetAllocationId() && current.GetGeneration() == change.AllocationRemoved.GetGeneration() {
				connection.snapshot.Allocations = append(connection.snapshot.Allocations[:index], connection.snapshot.Allocations[index+1:]...)
				return nil
			}
		}
		return errors.New("removed Relay allocation generation is not current")
	default:
		return errors.New("runtime delta change is required")
	}
	return nil
}
