package runtime

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/apipb"
)

// SessionOwner 是跨端 Go Client Engine 的 endpoint generation、planner race 与当前 ready session 真值。
// 单 route adapter 只能通过 AttemptRequest 参与 owner 启动的 race；Android、Web、TUI 和 CLI 不得在 owner 外缓存可执行 protocol client。
type SessionOwner struct {
	mu           sync.Mutex
	authority    *SessionGenerationAuthority
	current      map[endpoint.EndpointID]ApplicationReadySession
	configs      map[endpoint.EndpointID]string
	stickyRoutes map[endpoint.EndpointID]endpoint.RouteID
	acquireLocks map[endpoint.EndpointID]*sync.Mutex
	sharedLeases map[endpoint.EndpointID]map[*sharedApplicationLease]struct{}
	watchers     map[endpoint.EndpointID]map[chan EndpointEvent]struct{}
	closed       bool
}

// SessionGenerationAuthority 是 Go Client Engine 进程级的 endpoint generation 真值。
// engine/host 可以重建，但同一进程内后创建的 owner 必须继续递增；平台层只能持有该对象引用，不能读取或生成数值。
type SessionGenerationAuthority struct {
	mu          sync.Mutex
	generations map[endpoint.EndpointID]SessionGeneration
	current     map[endpoint.EndpointID]ApplicationReadySession
}

// NewSessionGenerationAuthority 创建空的进程级 generation authority。
func NewSessionGenerationAuthority() *SessionGenerationAuthority {
	return &SessionGenerationAuthority{generations: make(map[endpoint.EndpointID]SessionGeneration), current: make(map[endpoint.EndpointID]ApplicationReadySession)}
}

// NewSessionOwner 创建空的客户端 session owner。
// generation 从每个 endpoint 的 1 开始单调递增，进程内溢出时 fail closed，不能回绕后复活旧 resource stamp。
func NewSessionOwner() *SessionOwner {
	return NewSessionOwnerWithAuthority(NewSessionGenerationAuthority())
}

// NewSessionOwnerWithAuthority 创建共享进程级 generation 真值的 session owner。
// authority 只跨 host 重建复用，不改变每个 owner 对其 ready session 生命周期的独占责任。
func NewSessionOwnerWithAuthority(authority *SessionGenerationAuthority) *SessionOwner {
	if authority == nil {
		authority = NewSessionGenerationAuthority()
	}
	return &SessionOwner{
		authority:    authority,
		current:      make(map[endpoint.EndpointID]ApplicationReadySession),
		configs:      make(map[endpoint.EndpointID]string),
		stickyRoutes: make(map[endpoint.EndpointID]endpoint.RouteID),
		acquireLocks: make(map[endpoint.EndpointID]*sync.Mutex),
		sharedLeases: make(map[endpoint.EndpointID]map[*sharedApplicationLease]struct{}),
		watchers:     make(map[endpoint.EndpointID]map[chan EndpointEvent]struct{}),
	}
}

// AcquireRoute 复用同 endpoint、同 config key 的当前 ready session，并返回独立 consumer lease。
// config key 由 adapter 对已验证连接配置生成，runtime 只比较 opaque key；lease Close 只释放 consumer，配置变化或 owner teardown 才提升 generation。
func (owner *SessionOwner) AcquireRoute(ctx context.Context, target endpoint.Endpoint, routeID endpoint.RouteID, intent ConnectIntent, configKey string, dialer RouteAttemptDialer) (ApplicationReadySession, error) {
	if owner == nil || dialer == nil || configKey == "" {
		return nil, runtimeError(ErrorInvalidRequest, "session owner, route dialer, and config key are required", nil)
	}
	acquireLock := owner.endpointAcquireLock(target.ID)
	acquireLock.Lock()
	defer acquireLock.Unlock()
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return nil, runtimeError(ErrorUnavailable, "session owner is closed", nil)
	}
	current := owner.current[target.ID]
	if current != nil && owner.configs[target.ID] == configKey && owner.authority.isCurrent(target.ID, current.Stamp().Generation, current) {
		select {
		case <-current.Done():
		default:
			lease := owner.newSharedLeaseLocked(target.ID, current)
			owner.mu.Unlock()
			return lease, nil
		}
	}
	owner.mu.Unlock()
	lease, err := owner.ConnectRoute(ctx, target, routeID, intent, dialer)
	if err != nil {
		return nil, err
	}
	ready, err := owner.ApplicationSession(lease)
	if err != nil {
		return nil, err
	}
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		_ = ready.Close()
		return nil, runtimeError(ErrorUnavailable, "session owner is closed", nil)
	}
	owner.configs[target.ID] = configKey
	shared := owner.newSharedLeaseLocked(target.ID, ready)
	owner.mu.Unlock()
	return shared, nil
}

func (owner *SessionOwner) endpointAcquireLock(endpointID endpoint.EndpointID) *sync.Mutex {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	lock := owner.acquireLocks[endpointID]
	if lock == nil {
		lock = &sync.Mutex{}
		owner.acquireLocks[endpointID] = lock
	}
	return lock
}

// ConnectRoute 为调用方已经选定的唯一 route 建立新 generation。
// 新 generation 在拨号前即使旧 session 失效；并发连接只有最后分配的 generation 可以发布，迟到成功必须关闭并返回 stale。
func (owner *SessionOwner) ConnectRoute(
	ctx context.Context,
	target endpoint.Endpoint,
	routeID endpoint.RouteID,
	intent ConnectIntent,
	dialer RouteAttemptDialer,
) (SessionLease, error) {
	if owner == nil || dialer == nil {
		return SessionLease{}, runtimeError(ErrorUnavailable, "session owner or route dialer is unavailable", nil)
	}
	attempt, err := owner.BeginRouteAttempt(target, routeID, intent)
	if err != nil {
		return SessionLease{}, err
	}
	ready, err := dialer.Dial(ctx, attempt)
	if err != nil {
		return SessionLease{}, err
	}
	return owner.AdoptReadySession(attempt, ready)
}

// AdoptReadySession 把指定 attempt 已完成 Hello/auth 的 ready session 发布为当前 winner。
// 该入口只供 composition 迁移已经建立的 transport；attempt generation 必须先由 BeginRouteAttempt 分配，调用方不能伪造 stamp。
func (owner *SessionOwner) AdoptReadySession(attempt AttemptRequest, ready ReadySession) (SessionLease, error) {
	if owner == nil {
		if ready != nil {
			_ = ready.Close()
		}
		return SessionLease{}, runtimeError(ErrorUnavailable, "session owner is unavailable", nil)
	}
	if err := ValidateReadySession(attempt, ready); err != nil {
		_ = ready.Close()
		return SessionLease{}, err
	}
	application, ok := ready.(ApplicationReadySession)
	if !ok {
		_ = ready.Close()
		return SessionLease{}, runtimeError(ErrorUnavailable, "route attempt returned no Proto application session", nil)
	}
	stamp := attempt.Stamp()
	endpointID := stamp.EndpointID
	generation := stamp.Generation

	owner.mu.Lock()
	owner.authority.mu.Lock()
	if owner.closed || owner.authority.generations[endpointID] != generation || owner.authority.current[endpointID] != nil {
		owner.authority.mu.Unlock()
		owner.mu.Unlock()
		_ = application.Close()
		return SessionLease{}, runtimeError(ErrorStaleSession, "route attempt completed after its endpoint generation was replaced", nil)
	}
	owner.authority.current[endpointID] = application
	owner.authority.mu.Unlock()
	owner.current[endpointID] = application
	owner.mu.Unlock()
	go owner.removeWhenDone(endpointID, generation, application)
	return SessionLease{Stamp: attempt.Stamp()}, nil
}

// BeginRouteAttempt 为非 session 路由操作分配 endpoint generation，并立即撤销旧 session。
// Pairing 等认证握手必须通过该入口取得 AttemptRequest，平台 binding 不得自行生成 generation；调用方若不发布 ready session，
// 本次 generation 仍然有效地使旧 lease 失效，后续正式连接会再分配下一代。
func (owner *SessionOwner) BeginRouteAttempt(target endpoint.Endpoint, routeID endpoint.RouteID, intent ConnectIntent) (AttemptRequest, error) {
	if owner == nil {
		return AttemptRequest{}, runtimeError(ErrorUnavailable, "session owner is unavailable", nil)
	}
	// 静态 route/identity/intent 必须在推进 generation 或关闭健康 session 前完成验证。
	if _, err := NewAttemptRequest(target, routeID, 1, intent); err != nil {
		return AttemptRequest{}, err
	}
	generation, err := owner.beginEndpointGeneration(target.ID)
	if err != nil {
		return AttemptRequest{}, err
	}
	return NewAttemptRequest(target, routeID, generation, intent)
}

func (owner *SessionOwner) beginEndpointGeneration(endpointID endpoint.EndpointID) (SessionGeneration, error) {
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return 0, runtimeError(ErrorUnavailable, "session owner is closed", nil)
	}
	owner.authority.mu.Lock()
	previousGeneration := owner.authority.generations[endpointID]
	if previousGeneration == SessionGeneration(math.MaxUint64) {
		owner.authority.mu.Unlock()
		owner.mu.Unlock()
		return 0, runtimeError(ErrorUnavailable, "endpoint session generation is exhausted", nil)
	}
	generation := previousGeneration + 1
	owner.authority.generations[endpointID] = generation
	previousGlobal := owner.authority.current[endpointID]
	delete(owner.authority.current, endpointID)
	owner.authority.mu.Unlock()
	previous := owner.current[endpointID]
	delete(owner.current, endpointID)
	delete(owner.configs, endpointID)
	shared := owner.takeSharedLeasesLocked(endpointID)
	owner.mu.Unlock()
	for _, lease := range shared {
		lease.finish(runtimeError(ErrorStaleSession, "endpoint session generation was replaced", nil))
	}
	if previousGlobal != nil {
		_ = previousGlobal.Close()
	} else if previous != nil {
		_ = previous.Close()
	}
	return generation, nil
}

// ApplicationSession 把 runtime 返回的 lease 投影为 generation-fenced application ready session。
// wrapper 不缓存可执行 protocol client；每次 command/event/resource 操作都回到 owner 校验 current winner，Close 也只断开精确 generation。
func (owner *SessionOwner) ApplicationSession(lease SessionLease) (ApplicationReadySession, error) {
	if err := lease.Validate(); err != nil {
		return nil, err
	}
	current, err := owner.session(lease.Stamp)
	if err != nil {
		return nil, err
	}
	return &ownedApplicationSession{
		owner: owner, stamp: lease.Stamp, observedPath: current.ObservedPath(),
		done: current.Done(), terminal: current,
	}, nil
}

// ExecuteApplication 把 generated Proto command 路由到 stamp 指定的当前 ready session。
// generation 不匹配时在进入 protocol adapter 前失败，非幂等 command 不会被自动重放到新 session。
func (owner *SessionOwner) ExecuteApplication(ctx context.Context, stamp EndpointSessionStamp, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	session, err := owner.session(stamp)
	if err != nil {
		return nil, err
	}
	return session.ExecuteApplication(ctx, command)
}

// ApplicationEvents 返回 stamp 指定 session 的 generated Proto event stream。
// session 被替换或关闭后底层 stream 必须结束；调用方必须用新 lease 显式重新订阅，不能跨 generation 拼接事件。
func (owner *SessionOwner) ApplicationEvents(ctx context.Context, stamp EndpointSessionStamp) (<-chan *apipb.EventEnvelope, error) {
	session, err := owner.session(stamp)
	if err != nil {
		return nil, err
	}
	return session.ApplicationEvents(ctx)
}

// Disconnect 只关闭 stamp 精确匹配的当前 generation。
// stale disconnect 不能误关后来建立的 session；关闭错误只属于被释放 session，不触发其他 route fallback。
func (owner *SessionOwner) Disconnect(_ context.Context, request DisconnectRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	owner.mu.Lock()
	session := owner.current[request.Stamp.EndpointID]
	if session == nil || session.Stamp() != request.Stamp || !owner.authority.isCurrent(request.Stamp.EndpointID, request.Stamp.Generation, session) {
		owner.mu.Unlock()
		return runtimeError(ErrorStaleSession, "disconnect session stamp is not current", nil)
	}
	delete(owner.current, request.Stamp.EndpointID)
	owner.authority.removeCurrent(request.Stamp.EndpointID, session)
	owner.mu.Unlock()
	return session.Close()
}

// Close 关闭 owner 持有的全部当前 session 并禁止新连接。
// 方法可重复调用；它不修改 endpoint registry、credential store 或 daemon terminal lifecycle。
func (owner *SessionOwner) Close() error {
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return nil
	}
	owner.closed = true
	sessions := make([]ApplicationReadySession, 0, len(owner.current))
	for endpointID, session := range owner.current {
		sessions = append(sessions, session)
		owner.authority.removeCurrent(endpointID, session)
		delete(owner.current, endpointID)
		delete(owner.configs, endpointID)
	}
	shared := make([]*sharedApplicationLease, 0)
	for endpointID := range owner.sharedLeases {
		shared = append(shared, owner.takeSharedLeasesLocked(endpointID)...)
	}
	for endpointID, watchers := range owner.watchers {
		for watcher := range watchers {
			close(watcher)
		}
		delete(owner.watchers, endpointID)
	}
	owner.mu.Unlock()
	for _, lease := range shared {
		lease.finish(runtimeError(ErrorUnavailable, "session owner is closed", nil))
	}
	var first error
	for _, session := range sessions {
		if err := session.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (owner *SessionOwner) session(stamp EndpointSessionStamp) (ApplicationReadySession, error) {
	if owner == nil {
		return nil, runtimeError(ErrorUnavailable, "session owner is unavailable", nil)
	}
	if err := stamp.Validate(); err != nil {
		return nil, err
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed {
		return nil, runtimeError(ErrorUnavailable, "session owner is closed", nil)
	}
	session := owner.current[stamp.EndpointID]
	if session == nil || session.Stamp() != stamp || !owner.authority.isCurrent(stamp.EndpointID, stamp.Generation, session) {
		return nil, runtimeError(ErrorStaleSession, fmt.Sprintf("endpoint %q session generation is stale", stamp.EndpointID), nil)
	}
	return session, nil
}

func (owner *SessionOwner) removeWhenDone(endpointID endpoint.EndpointID, generation SessionGeneration, session ApplicationReadySession) {
	<-session.Done()
	stamp := session.Stamp()
	err := session.Err()
	owner.mu.Lock()
	if current := owner.current[endpointID]; current == session {
		delete(owner.current, endpointID)
		delete(owner.configs, endpointID)
	}
	// 旧 session 的 Done 可能晚于新 generation 发布；这里只能回收精确绑定
	// 当前 ready session 的 consumer lease，不能触碰后来 generation。
	shared := owner.takeSharedLeasesForSessionLocked(endpointID, session)
	owner.authority.removeCurrent(endpointID, session)
	owner.mu.Unlock()
	for _, lease := range shared {
		lease.finish(err)
	}
	_ = session.Close()
	owner.publishEndpointEvent(EndpointEvent{EndpointID: endpointID, Stamp: stamp, Phase: EndpointPhaseOffline, ErrorCode: CodeOf(err), Message: errorMessage(err)})
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (owner *SessionOwner) newSharedLeaseLocked(endpointID endpoint.EndpointID, ready ApplicationReadySession) *sharedApplicationLease {
	lease := &sharedApplicationLease{owner: owner, endpointID: endpointID, ready: ready, done: make(chan struct{})}
	if owner.sharedLeases[endpointID] == nil {
		owner.sharedLeases[endpointID] = make(map[*sharedApplicationLease]struct{})
	}
	owner.sharedLeases[endpointID][lease] = struct{}{}
	go func() {
		select {
		case <-ready.Done():
			lease.finish(ready.Err())
		case <-lease.done:
		}
	}()
	return lease
}

func (owner *SessionOwner) releaseSharedLease(lease *sharedApplicationLease) {
	owner.mu.Lock()
	leases := owner.sharedLeases[lease.endpointID]
	delete(leases, lease)
	if len(leases) == 0 {
		delete(owner.sharedLeases, lease.endpointID)
	}
	owner.mu.Unlock()
}

func (owner *SessionOwner) takeSharedLeasesLocked(endpointID endpoint.EndpointID) []*sharedApplicationLease {
	leasing := owner.sharedLeases[endpointID]
	result := make([]*sharedApplicationLease, 0, len(leasing))
	for lease := range leasing {
		result = append(result, lease)
	}
	delete(owner.sharedLeases, endpointID)
	return result
}

func (owner *SessionOwner) takeSharedLeasesForSessionLocked(endpointID endpoint.EndpointID, ready ApplicationReadySession) []*sharedApplicationLease {
	leasing := owner.sharedLeases[endpointID]
	result := make([]*sharedApplicationLease, 0, len(leasing))
	for lease := range leasing {
		if lease.ready != ready {
			continue
		}
		result = append(result, lease)
		delete(leasing, lease)
	}
	if len(leasing) == 0 {
		delete(owner.sharedLeases, endpointID)
	}
	return result
}

func (authority *SessionGenerationAuthority) isCurrent(endpointID endpoint.EndpointID, generation SessionGeneration, session ApplicationReadySession) bool {
	if authority == nil {
		return false
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	return authority.generations[endpointID] == generation && authority.current[endpointID] == session
}

func (authority *SessionGenerationAuthority) removeCurrent(endpointID endpoint.EndpointID, session ApplicationReadySession) {
	if authority == nil {
		return
	}
	authority.mu.Lock()
	if authority.current[endpointID] == session {
		delete(authority.current, endpointID)
	}
	authority.mu.Unlock()
}

type ownedApplicationSession struct {
	owner        *SessionOwner
	stamp        EndpointSessionStamp
	observedPath string
	done         <-chan struct{}
	terminal     ApplicationReadySession
}

func (session *ownedApplicationSession) Stamp() EndpointSessionStamp { return session.stamp }
func (session *ownedApplicationSession) ObservedPath() string        { return session.observedPath }
func (session *ownedApplicationSession) Readiness() ReadySessionEvidence {
	return session.terminal.Readiness()
}
func (session *ownedApplicationSession) Done() <-chan struct{} { return session.done }
func (session *ownedApplicationSession) Err() error            { return session.terminal.Err() }

func (session *ownedApplicationSession) Close() error {
	return session.owner.Disconnect(context.Background(), DisconnectRequest{Stamp: session.stamp})
}

func (session *ownedApplicationSession) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.owner.ExecuteApplication(ctx, session.stamp, command)
}

// ExecuteApplicationTerminal 把 terminal-response 选择路由到同一 generation 的底层 ready session。
func (session *ownedApplicationSession) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	current, err := session.owner.session(session.stamp)
	if err != nil {
		return nil, err
	}
	executor, ok := current.(TerminalResponseApplicationExecutor)
	if !ok {
		return nil, runtimeError(ErrorUnavailable, "current session does not support terminal application responses", nil)
	}
	return executor.ExecuteApplicationTerminal(ctx, command)
}

func (session *ownedApplicationSession) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	return session.owner.ApplicationEvents(ctx, session.stamp)
}

func (session *ownedApplicationSession) OpenResourceStream(resource *apipb.ResourceHandle) (ResourceStream, error) {
	current, err := session.owner.session(session.stamp)
	if err != nil {
		return nil, err
	}
	provider, ok := current.(ResourceStreamSession)
	if !ok {
		return nil, runtimeError(ErrorUnavailable, "current session does not support resource streams", nil)
	}
	return provider.OpenResourceStream(resource)
}

var _ ApplicationReadySession = (*ownedApplicationSession)(nil)
var _ ResourceStreamSession = (*ownedApplicationSession)(nil)

type sharedApplicationLease struct {
	owner      *SessionOwner
	endpointID endpoint.EndpointID
	ready      ApplicationReadySession
	done       chan struct{}
	closeOnce  sync.Once
	errMu      sync.Mutex
	err        error
}

func (lease *sharedApplicationLease) Stamp() EndpointSessionStamp { return lease.ready.Stamp() }
func (lease *sharedApplicationLease) ObservedPath() string        { return lease.ready.ObservedPath() }
func (lease *sharedApplicationLease) Readiness() ReadySessionEvidence {
	return lease.ready.Readiness()
}
func (lease *sharedApplicationLease) Done() <-chan struct{} { return lease.done }
func (lease *sharedApplicationLease) Err() error {
	lease.errMu.Lock()
	defer lease.errMu.Unlock()
	return lease.err
}
func (lease *sharedApplicationLease) Close() error {
	lease.closeOnce.Do(func() {
		lease.owner.releaseSharedLease(lease)
		close(lease.done)
	})
	return nil
}
func (lease *sharedApplicationLease) finish(err error) {
	lease.errMu.Lock()
	lease.err = err
	lease.errMu.Unlock()
	lease.closeOnce.Do(func() {
		lease.owner.releaseSharedLease(lease)
		close(lease.done)
	})
}
func (lease *sharedApplicationLease) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	if err := lease.active(); err != nil {
		return nil, err
	}
	return lease.ready.ExecuteApplication(ctx, command)
}

// ExecuteApplicationTerminal 保持 shared lease generation fence，并把 terminal response 交给同一 ready session。
func (lease *sharedApplicationLease) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	if err := lease.active(); err != nil {
		return nil, err
	}
	executor, ok := lease.ready.(TerminalResponseApplicationExecutor)
	if !ok {
		return nil, runtimeError(ErrorUnavailable, "shared session does not support terminal application responses", nil)
	}
	return executor.ExecuteApplicationTerminal(ctx, command)
}
func (lease *sharedApplicationLease) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	if err := lease.active(); err != nil {
		return nil, err
	}
	eventCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-lease.done:
			cancel()
		case <-eventCtx.Done():
		}
	}()
	return lease.ready.ApplicationEvents(eventCtx)
}
func (lease *sharedApplicationLease) OpenResourceStream(resource *apipb.ResourceHandle) (ResourceStream, error) {
	if err := lease.active(); err != nil {
		return nil, err
	}
	provider, ok := lease.ready.(ResourceStreamSession)
	if !ok {
		return nil, runtimeError(ErrorUnavailable, "shared session does not support resource streams", nil)
	}
	return provider.OpenResourceStream(resource)
}

func (lease *sharedApplicationLease) active() error {
	select {
	case <-lease.done:
		return runtimeError(ErrorStaleSession, "shared session lease is closed", lease.Err())
	default:
		return nil
	}
}

var _ ApplicationReadySession = (*sharedApplicationLease)(nil)
var _ ResourceStreamSession = (*sharedApplicationLease)(nil)
