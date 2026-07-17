package runtime

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/apipb"
)

// SessionOwner 是跨端 Go Client Engine 的 endpoint generation 与当前 ready session 真值。
// 它只接收外部已经选定的单个 route，不实现 planner/race；Android、Web、TUI 和 CLI 不得在 owner 外缓存可执行 protocol client。
type SessionOwner struct {
	mu          sync.Mutex
	generations map[endpoint.EndpointID]SessionGeneration
	current     map[endpoint.EndpointID]ApplicationReadySession
	closed      bool
}

// NewSessionOwner 创建空的客户端 session owner。
// generation 从每个 endpoint 的 1 开始单调递增，进程内溢出时 fail closed，不能回绕后复活旧 resource stamp。
func NewSessionOwner() *SessionOwner {
	return &SessionOwner{
		generations: make(map[endpoint.EndpointID]SessionGeneration),
		current:     make(map[endpoint.EndpointID]ApplicationReadySession),
	}
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
	endpointID := target.ID
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return SessionLease{}, runtimeError(ErrorUnavailable, "session owner is closed", nil)
	}
	previousGeneration := owner.generations[endpointID]
	if previousGeneration == SessionGeneration(math.MaxUint64) {
		owner.mu.Unlock()
		return SessionLease{}, runtimeError(ErrorUnavailable, "endpoint session generation is exhausted", nil)
	}
	generation := previousGeneration + 1
	owner.generations[endpointID] = generation
	previous := owner.current[endpointID]
	delete(owner.current, endpointID)
	owner.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}

	attempt, err := NewAttemptRequest(target, routeID, generation, intent)
	if err != nil {
		return SessionLease{}, err
	}
	ready, err := dialer.Dial(ctx, attempt)
	if err != nil {
		return SessionLease{}, err
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

	owner.mu.Lock()
	if owner.closed || owner.generations[endpointID] != generation {
		owner.mu.Unlock()
		_ = application.Close()
		return SessionLease{}, runtimeError(ErrorStaleSession, "route attempt completed after its endpoint generation was replaced", nil)
	}
	owner.current[endpointID] = application
	owner.mu.Unlock()
	go owner.removeWhenDone(endpointID, generation, application)
	return SessionLease{Stamp: attempt.Stamp()}, nil
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
	if session == nil || session.Stamp() != request.Stamp {
		owner.mu.Unlock()
		return runtimeError(ErrorStaleSession, "disconnect session stamp is not current", nil)
	}
	delete(owner.current, request.Stamp.EndpointID)
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
		delete(owner.current, endpointID)
	}
	owner.mu.Unlock()
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
	if session == nil || session.Stamp() != stamp {
		return nil, runtimeError(ErrorStaleSession, fmt.Sprintf("endpoint %q session generation is stale", stamp.EndpointID), nil)
	}
	return session, nil
}

func (owner *SessionOwner) removeWhenDone(endpointID endpoint.EndpointID, generation SessionGeneration, session ApplicationReadySession) {
	<-session.Done()
	owner.mu.Lock()
	if current := owner.current[endpointID]; owner.generations[endpointID] == generation && current == session {
		delete(owner.current, endpointID)
	}
	owner.mu.Unlock()
	_ = session.Close()
}
