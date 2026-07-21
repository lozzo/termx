package managed

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	peeradapter "github.com/muxvia/muxvia/client/adapter/peer"
	"github.com/muxvia/muxvia/client/port"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/apipb"
)

// CloudSessionOpener 为单次 managed attempt 延迟打开 Cloud client 及其生命周期 owner。
// opener 只能返回 signaling/route client，不能携带 CapabilityGrant、DeviceIdentity private key 或 DataChannel payload。
type CloudSessionOpener func(context.Context) (CloudClient, io.Closer, error)

// LazyDialer 在 runtime 真正启动 managed attempt 时才装配 Cloud client，并在失败或 ReadyPeerSession Close 时释放它。
// local/SSH planner winner 不会触发 opener；实际 WebRTC/auth/Hello 仍由 Dialer 完成。
type LazyDialer struct {
	OpenCloud     CloudSessionOpener
	Peers         port.ManagedPeerFactory
	Authorization peeradapter.Authorizer
	ClientName    string
	Now           func() time.Time
}

// Dial 延迟创建 Cloud client，调用标准 managed Dialer，并把 Cloud owner 绑定到返回 ReadyPeerSession 的 exact-once Close。
func (dialer LazyDialer) Connect(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadyPeerSession, error) {
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
	}).Connect(ctx, request)
	if err != nil {
		_ = closer.Close()
		return nil, err
	}
	application, ok := ready.(clientruntime.ApplicationReadyPeerSession)
	if !ok {
		_ = ready.Close()
		_ = closer.Close()
		return nil, fmt.Errorf("managed route returned no application session")
	}
	return &lazyReadyPeerSession{ApplicationReadyPeerSession: application, closer: closer}, nil
}

type lazyReadyPeerSession struct {
	clientruntime.ApplicationReadyPeerSession
	closer    io.Closer
	closeOnce sync.Once
	closeErr  error
}

// Close 先关闭 generation-bound managed session，再释放本次 lazy Cloud client；重复调用返回首次结果。
func (session *lazyReadyPeerSession) Close() error {
	session.closeOnce.Do(func() {
		session.closeErr = session.ApplicationReadyPeerSession.Close()
		if err := session.closer.Close(); session.closeErr == nil {
			session.closeErr = err
		}
	})
	return session.closeErr
}

// ExecuteApplicationTerminal 保留 resource-producing command 的有界 terminal response 能力。
func (session *lazyReadyPeerSession) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	executor, ok := session.ApplicationReadyPeerSession.(clientruntime.TerminalResponseApplicationExecutor)
	if !ok {
		return nil, fmt.Errorf("managed session does not support terminal application responses")
	}
	return executor.ExecuteApplicationTerminal(ctx, command)
}

// OpenResourceStream 委托当前 managed protocol session 打开 resource handle 对应的 framing stream。
func (session *lazyReadyPeerSession) OpenResourceStream(resource *apipb.ResourceHandle) (clientruntime.ResourceStream, error) {
	provider, ok := session.ApplicationReadyPeerSession.(clientruntime.ResourceStreamSession)
	if !ok {
		return nil, fmt.Errorf("managed session does not support resource streams")
	}
	return provider.OpenResourceStream(resource)
}

// ApplicationAttachmentChannel 返回当前 managed generation 内 resource 对应的 attachment channel。
func (session *lazyReadyPeerSession) ApplicationAttachmentChannel(resource *apipb.ResourceHandle) (uint16, bool) {
	provider, ok := session.ApplicationReadyPeerSession.(clientruntime.ApplicationAttachmentSession)
	if !ok {
		return 0, false
	}
	return provider.ApplicationAttachmentChannel(resource)
}

// ApplicationAttachment 返回当前 managed generation 内 channel 对应的 attachment resource。
func (session *lazyReadyPeerSession) ApplicationAttachment(channel uint16) (*apipb.ResourceHandle, bool) {
	provider, ok := session.ApplicationReadyPeerSession.(clientruntime.ApplicationAttachmentSession)
	if !ok {
		return nil, false
	}
	return provider.ApplicationAttachment(channel)
}

var _ clientruntime.PeerConnector = LazyDialer{}
var _ clientruntime.ResourceStreamSession = (*lazyReadyPeerSession)(nil)
var _ clientruntime.ApplicationAttachmentSession = (*lazyReadyPeerSession)(nil)
var _ clientruntime.TerminalResponseApplicationExecutor = (*lazyReadyPeerSession)(nil)
