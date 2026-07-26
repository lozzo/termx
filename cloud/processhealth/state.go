// Package processhealth 实现 Controller 与 Edge 共用的固定健康检查语义。
package processhealth

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

// State 分开表达进程存活与 Cloud 可接受新工作两个状态。
// /healthz 只表示进程可回应；/readyz 只在依赖和控制链就绪时返回 200。
type State struct {
	alive atomic.Bool
	ready atomic.Bool
}

// SetAlive 由进程 lifecycle owner 在 listener 启动和关闭时调用。
func (state *State) SetAlive(value bool) {
	state.alive.Store(value)
}

// SetReady 由组装层在必需依赖或 ControllerLink 状态变化时调用。
func (state *State) SetReady(value bool) {
	state.ready.Store(value)
}

// Alive 返回当前进程健康真值。
func (state *State) Alive() bool {
	return state.alive.Load()
}

// Ready 返回当前 Cloud 就绪真值。
func (state *State) Ready() bool {
	return state.ready.Load()
}

// ServeHTTP 只暴露固定 /healthz 与 /readyz，不接受用户自定义路径。
func (state *State) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var healthy bool
	var status string
	switch request.URL.Path {
	case "/healthz":
		healthy, status = state.Alive(), "alive"
	case "/readyz":
		healthy, status = state.Ready(), "ready"
	default:
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}
	writer.WriteHeader(code)
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": healthy, "status": status})
}

// IsLoopbackAddress 校验 Controller 健康 listener 不会被配置到公网网卡。
func IsLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
