package managed

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/apipb"
)

// CloudSessionOpener 为单次 managed attempt 延迟打开 Cloud client 及其生命周期 owner。
// opener 只能返回 signaling/route client，不能携带 CapabilityGrant、DeviceIdentity private key 或 DataChannel payload。
type CloudSessionOpener func(context.Context) (CloudClient, io.Closer, error)

// LazyDialer 在 runtime 真正启动 managed attempt 时才装配 Cloud client，并在失败或 ReadySession Close 时释放它。
// local/SSH planner winner 不会触发 opener；实际 WebRTC/auth/Hello 仍由 Dialer 完成。
type LazyDialer struct {
	OpenCloud     CloudSessionOpener
	Peers         port.ManagedPeerFactory
	Authorization Authorizer
	ClientName    string
	Now           func() time.Time
}

// Dial 延迟创建 Cloud client，调用标准 managed Dialer，并把 Cloud owner 绑定到返回 ReadySession 的 exact-once Close。
func (dialer LazyDialer) Dial(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadySession, error) {
	if dialer.OpenCloud == nil {
		return nil, fmt.Errorf("managed Cloud opener is required")
	}
	cloud, closer, err := dialer.OpenCloud(ctx)
	if err != nil {
		return nil, err
	}
	if cloud == nil || closer == nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, fmt.Errorf("managed Cloud opener returned incomplete lifecycle")
	}
	ready, err := (&Dialer{
		Cloud: cloud, Peers: dialer.Peers, Authorization: dialer.Authorization, ClientName: dialer.ClientName, Now: dialer.Now,
	}).Dial(ctx, request)
	if err != nil {
		_ = closer.Close()
		return nil, err
	}
	application, ok := ready.(clientruntime.ApplicationReadySession)
	if !ok {
		_ = ready.Close()
		_ = closer.Close()
		return nil, fmt.Errorf("managed route returned no application session")
	}
	return &lazyReadySession{ApplicationReadySession: application, closer: closer}, nil
}

type lazyReadySession struct {
	clientruntime.ApplicationReadySession
	closer    io.Closer
	closeOnce sync.Once
	closeErr  error
}

// Close 先关闭 generation-bound managed session，再释放本次 lazy Cloud client；重复调用返回首次结果。
func (session *lazyReadySession) Close() error {
	session.closeOnce.Do(func() {
		session.closeErr = session.ApplicationReadySession.Close()
		if err := session.closer.Close(); session.closeErr == nil {
			session.closeErr = err
		}
	})
	return session.closeErr
}

// ExecuteApplicationTerminal 保留 resource-producing command 的有界 terminal response 能力。
func (session *lazyReadySession) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	executor, ok := session.ApplicationReadySession.(clientruntime.TerminalResponseApplicationExecutor)
	if !ok {
		return nil, fmt.Errorf("managed session does not support terminal application responses")
	}
	return executor.ExecuteApplicationTerminal(ctx, command)
}

// OpenResourceStream 委托当前 managed protocol session 打开 resource handle 对应的 framing stream。
func (session *lazyReadySession) OpenResourceStream(resource *apipb.ResourceHandle) (clientruntime.ResourceStream, error) {
	provider, ok := session.ApplicationReadySession.(clientruntime.ResourceStreamSession)
	if !ok {
		return nil, fmt.Errorf("managed session does not support resource streams")
	}
	return provider.OpenResourceStream(resource)
}

// ApplicationAttachmentChannel 返回当前 managed generation 内 resource 对应的 attachment channel。
func (session *lazyReadySession) ApplicationAttachmentChannel(resource *apipb.ResourceHandle) (uint16, bool) {
	provider, ok := session.ApplicationReadySession.(clientruntime.ApplicationAttachmentSession)
	if !ok {
		return 0, false
	}
	return provider.ApplicationAttachmentChannel(resource)
}

// ApplicationAttachment 返回当前 managed generation 内 channel 对应的 attachment resource。
func (session *lazyReadySession) ApplicationAttachment(channel uint16) (*apipb.ResourceHandle, bool) {
	provider, ok := session.ApplicationReadySession.(clientruntime.ApplicationAttachmentSession)
	if !ok {
		return nil, false
	}
	return provider.ApplicationAttachment(channel)
}

var _ clientruntime.RouteAttemptDialer = LazyDialer{}
var _ clientruntime.ResourceStreamSession = (*lazyReadySession)(nil)
var _ clientruntime.ApplicationAttachmentSession = (*lazyReadySession)(nil)
var _ clientruntime.TerminalResponseApplicationExecutor = (*lazyReadySession)(nil)
