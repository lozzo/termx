// Package daemon owns the authorized remote DataChannel-to-core-v2 boundary.
package daemon

import (
	"context"
	"fmt"
	"time"

	termxcorev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/lozzow/termx/termx-shared/transport/datachannel"
)

// ScopedTransportServer 是 remote-v2 允许调用的 core-v2 transport 边界。
// 实现方只能按已经验证的 capability scope 建立 protocol session，不能把远程 DataChannel 当作本地无限权限 listener。
type ScopedTransportServer interface {
	ServeScopedTransport(context.Context, transport.Transport, termxcorev2.TransportScope) error
}

// SessionRequest 描述一个已经完成 WebRTC 协商、等待 daemon 授权的 DataChannel。
// Grant 是 remote-issued bearer capability；request 不携带 trust anchor，避免客户端或 Hub 自报 fingerprint 改写 daemon 身份真值。
type SessionRequest struct {
	Channel datachannel.Channel
	Grant   string
	Now     time.Time
}

// SessionAcceptor 是远程 DataChannel 进入 core-v2 的唯一授权入口。
// grant 的签名、fingerprint、expiry 和 revoke 全部通过后才创建 protocol transport；验证失败不会调用 core，也不会 fallback 到其他 transport。
type SessionAcceptor struct {
	Core              ScopedTransportServer
	DeviceFingerprint string
	Revocations       remoteauth.RevocationChecker
}

// Authorize 验证已经从端到端 DataChannel 握手中收到的 capability grant。
// 调用方不得从 signaling、Companion 或 Hub 提取 grant；验证必须发生在创建 core-v2 session 之前。
func (acceptor SessionAcceptor) Authorize(grant string, now time.Time) (remoteauth.Claims, error) {
	claims, err := remoteauth.Verify(grant, acceptor.DeviceFingerprint, now, acceptor.Revocations)
	if err != nil {
		return remoteauth.Claims{}, fmt.Errorf("authorize remote data channel: %w", err)
	}
	return claims, nil
}

// Serve 验证 remote-issued capability 并按 scope 把 DataChannel 交给 core-v2。
// 调用会持续到 protocol session 结束；DataChannel 和 core transport 的关闭只影响当前远程 session。
func (acceptor SessionAcceptor) Serve(ctx context.Context, request SessionRequest) error {
	if acceptor.Core == nil {
		return fmt.Errorf("remote daemon core transport server is not configured")
	}
	if request.Channel == nil {
		return fmt.Errorf("remote daemon data channel is not configured")
	}
	claims, err := acceptor.Authorize(request.Grant, request.Now)
	if err != nil {
		return err
	}
	scope := termxcorev2.TransportScope{
		AllowDaemon:       claims.Scope.AllowDaemon,
		TerminalID:        claims.Scope.TerminalID,
		MachineEventsOnly: claims.Scope.MachineEventsOnly,
	}
	protocolTransport := datachannel.New(request.Channel)
	defer protocolTransport.Close()
	return acceptor.Core.ServeScopedTransport(ctx, protocolTransport, scope)
}
