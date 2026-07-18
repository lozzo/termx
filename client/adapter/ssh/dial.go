package ssh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
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

// Dialer 是 planner 已选定的 ssh-webrtc-tcp attempt 边界。
// RTC006 会在这里接入 Go SSH direct-tcpip 与 ICE-TCP；旧 OpenSSH stdio proxy 不得作为新 Route 的 fallback。
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

// Dial 在 RTC006 实现 Go SSH backed ICE-TCP 前显式拒绝新 Route。
// 该失败保证新配置不会被错误解释为旧 stdio proxy 参数。
func (dialer *Dialer) Connect(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadyPeerSession, error) {
	if dialer == nil {
		return nil, fmt.Errorf("SSH dialer is required")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	route := request.Route()
	if route.Kind != endpoint.RouteSSHWebRTCTCP {
		return nil, fmt.Errorf("SSH adapter cannot dial route kind %s", route.Kind)
	}
	return nil, fmt.Errorf("ssh-webrtc-tcp connector is not available before RTC006")
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

var _ clientruntime.PeerConnector = (*Dialer)(nil)
