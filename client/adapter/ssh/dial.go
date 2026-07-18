package ssh

import (
	"context"
	"fmt"
	"strings"
	"time"

	protocoladapter "github.com/lozzow/termx/client/adapter/protocol"
	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	sshtransport "github.com/lozzow/termx/shared/transport/ssh"
)

const defaultClientName = "termx-ssh-client"

// Options 是单次 SSH route adapter 的 process/Hello 配置。
// SSH binary 与额外参数只由 composition root 注入；route host、credential ref 和 remote socket 始终来自 AttemptRequest。
type Options struct {
	ClientName     string
	SSHBinary      string
	ExtraArgs      []string
	ConnectTimeout time.Duration
}

// Dialer 只执行 planner 已选定的 ssh-stdio attempt。
// OpenSSH 拥有 host-key 与用户认证，远端 stdio-proxy 只桥接 daemon-local 0600 socket；adapter 随后验证 daemon identity 并完成 protocol Hello。
type Dialer struct {
	options Options
}

// NewDialer 创建不拥有 route selection、fallback 或 session cache 的 SSH dialer。
func NewDialer(options Options) *Dialer {
	options.ClientName = strings.TrimSpace(options.ClientName)
	if options.ClientName == "" {
		options.ClientName = defaultClientName
	}
	options.ExtraArgs = append([]string(nil), options.ExtraArgs...)
	return &Dialer{options: options}
}

// Dial 完成 OpenSSH transport、远端 daemon identity 校验和 protocol Hello 后返回 ReadySession。
// 任一步失败都会关闭 SSH process/transport，禁止把仅启动成功的子进程发布为 winner candidate。
func (dialer *Dialer) Dial(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadySession, error) {
	if dialer == nil {
		return nil, fmt.Errorf("SSH dialer is required")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	route := request.Route()
	if route.Kind != endpoint.RouteSSHStdio {
		return nil, fmt.Errorf("SSH adapter cannot dial route kind %s", route.Kind)
	}
	transportConnection, err := sshtransport.Dial(ctx, sshtransport.DialOptions{
		Address: sshAddress(route),
		AuthRef: route.CredentialRef, RemoteSocket: route.RemoteSocket,
		SSHBinary: dialer.options.SSHBinary, ConnectTimeout: dialer.options.ConnectTimeout,
		ExtraArgs: sshArgs(route, dialer.options.ExtraArgs),
	})
	if err != nil {
		return nil, err
	}
	client := internalprotocol.NewClient(transportConnection)
	if err := client.Hello(ctx, internalprotocol.Hello{Version: wire.Version, Client: dialer.options.ClientName}); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("SSH endpoint protocol Hello: %w", err)
	}
	ready, err := protocoladapter.NewApplicationClientWithObservedPath(client, request.Stamp(), "ssh:"+route.Host)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	identity, err := protocoladapter.VerifyDaemonIdentity(ctx, ready.ApplicationSession, request.DaemonIdentity())
	if err != nil {
		_ = ready.Close()
		return nil, err
	}
	if err := ready.MarkReady(clientruntime.ReadySessionEvidence{
		Identity: identity, IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: wire.Version,
	}); err != nil {
		_ = ready.Close()
		return nil, err
	}
	// ReadySession 已经完成 OpenSSH auth、fresh daemon proof 与 Hello；从这里开始
	// SSH 子进程由 ready.Close 拥有，planner race context 只允许取消 loser。
	transportConnection.CommitReady()
	return ready, nil
}

func sshArgs(route endpoint.AccessRoute, extra []string) []string {
	args := append([]string(nil), extra...)
	if route.Port != 0 && route.Port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", route.Port))
	}
	if route.ProxyJump != "" {
		args = append(args, "-J", route.ProxyJump)
	}
	return args
}

func sshAddress(route endpoint.AccessRoute) string {
	target := route.Host
	if route.User != "" && !strings.Contains(target, "@") {
		target = route.User + "@" + target
	}
	return target
}

var _ clientruntime.RouteAttemptDialer = (*Dialer)(nil)
