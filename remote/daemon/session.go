// Package daemon owns the authorized remote DataChannel-to-core-v2 boundary.
package daemon

import (
	"context"
	"fmt"
	"io"
	"time"

	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport"
)

// ScopedTransportServer 是 remote-v2 允许调用的 core-v2 transport 边界。
// 实现方只能按已经验证的 capability scope 建立 protocol session，不能把远程 DataChannel 当作本地无限权限 listener。
type ScopedTransportServer interface {
	// ServeScopedTransport 按 daemon 已验证的 scope 服务同一条 DataChannel 上的 termx protocol。
	ServeScopedTransport(context.Context, transport.Transport, core.TransportScope) error
}

// SessionAcceptor 是远程 DataChannel 进入 core-v2 的唯一授权入口。
// Identity、grant expiry/revoke 和 scope mapping 都属于 daemon；Cloud Companion、Hub admission 或 signaling 结果不能预授权 session。
type SessionAcceptor struct {
	Core        ScopedTransportServer
	Identity    remoteauth.Identity
	Revocations remoteauth.RevocationChecker
	Random      io.Reader
	Now         func() time.Time
}

// ServeDataChannel 在同一 transport 上完成 DeviceHello/CapabilityOpen，成功后才调用 core-v2。
// daemonDTLSFingerprint 必须来自当前 Pion 本端 DTLSTransport；失败会关闭当前 channel，且不创建 protocol session 或 fallback。
func (acceptor SessionAcceptor) ServeDataChannel(ctx context.Context, connection transport.Transport, daemonDTLSFingerprint string) error {
	if acceptor.Core == nil {
		return fmt.Errorf("remote daemon core transport server is not configured")
	}
	if connection == nil {
		return fmt.Errorf("remote daemon data channel transport is not configured")
	}
	defer connection.Close()
	claims, err := (remoteauth.ServerHandshake{
		Identity: acceptor.Identity, Revocations: acceptor.Revocations, Random: acceptor.Random, Now: acceptor.Now,
	}).Accept(ctx, connection, daemonDTLSFingerprint)
	if err != nil {
		return fmt.Errorf("authorize remote data channel: %w", err)
	}
	scope := core.TransportScope{
		AllowDaemon:       claims.Scope.AllowDaemon,
		TerminalID:        claims.Scope.TerminalID,
		MachineEventsOnly: claims.Scope.MachineEventsOnly,
	}
	return acceptor.Core.ServeScopedTransport(ctx, connection, scope)
}
