// Package connection owns the shared client-side endpoint registry model.
package connection

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// DefaultFileName 是 CLI/TUI 共享连接注册表的默认文件名。
	DefaultFileName = "connections.yaml"

	// DefaultEndpointID 是缺省本地 daemon endpoint 的稳定 ID。
	DefaultEndpointID EndpointID = "local"
)

const (
	// TransportLocal 表示通过本机 unix socket 连接 termx daemon。
	TransportLocal TransportKind = "local"
	// TransportSSH 表示通过 SSH 到远端主机后连接该主机上的 termx daemon。
	TransportSSH TransportKind = "ssh"
	// TransportHubP2P 是未来 hub/P2P 连接模式的保留 transport 名称。
	TransportHubP2P TransportKind = "hub-p2p"
)

const (
	// ConnectAuto 表示 TUI/client 启动时应主动连接 endpoint。
	ConnectAuto ConnectMode = "auto"
	// ConnectOnDemand 表示只在用户或可见 restore 需要该 endpoint 时连接。
	ConnectOnDemand ConnectMode = "on_demand"
	// ConnectManual 表示只有显式 connect action 才能连接 endpoint。
	ConnectManual ConnectMode = "manual"
)

// EndpointID 是客户端本地 connection registry 中 endpoint 的稳定主键。
// 它用于 workbench ref、路由和配置 diff，不是展示名，也不能替代 SSH host key 或 hub device identity。
type EndpointID string

// TransportKind 描述连接 endpoint 的 transport 类型。
// transport 只表达连接方式；daemon identity、安全校验和展示 label 必须由独立字段处理。
type TransportKind string

// ConnectMode 描述 endpoint 的自动连接策略。
// 它只影响未来连接时机，不允许热切换已经建立的 protocol session。
type ConnectMode string

// Registry 是 CLI/TUI 共享的 endpoint 连接注册表。
// 它表达用户配置期望状态；已经建立的 endpoint session 是运行时事实，不能被 registry reload 直接原地改写。
type Registry struct {
	Version     int
	Default     EndpointID
	Connections map[EndpointID]Config
}

// Config 描述一个 endpoint 的连接配置。
// ID 是持久化和路由主键；Label 只用于 UI 展示；Transport/Address/AuthRef/Socket/RemoteSocket 共同组成 dial identity。
type Config struct {
	ID           EndpointID
	Label        string
	Transport    TransportKind
	Address      string
	AuthRef      string
	ConnectMode  ConnectMode
	Enabled      bool
	Socket       string
	RemoteSocket string
}

// DialIdentity 是决定已连接 session 是否需要 reconnect 的连接身份。
// 修改这些字段不能热切换运行中 session；只能标记 reconnect required。
type DialIdentity struct {
	Transport    TransportKind
	Address      string
	AuthRef      string
	Socket       string
	RemoteSocket string
}

// DefaultPath 返回 connection registry 默认读取路径。
// 该路径归 CLI/TUI 共享连接目标所有，不属于 TUI-only 的 tui-v3.yaml。
func DefaultPath() string {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "termx", DefaultFileName)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "termx", DefaultFileName)
	}
	return filepath.Join(os.TempDir(), "termx-config", DefaultFileName)
}

// DefaultRegistry 返回缺少 connections.yaml 时的稳定本地 registry。
// 这个默认值只包含 local endpoint，保证本地单 daemon 行为不会因为多 endpoint 基础设施存在而改变。
func DefaultRegistry() Registry {
	label := "local"
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		label = strings.TrimSpace(hostname)
	}
	cfg := defaultLocalConfig(label)
	return Registry{
		Version: 1,
		Default: DefaultEndpointID,
		Connections: map[EndpointID]Config{
			DefaultEndpointID: cfg,
		},
	}
}

// Load 读取 connection registry。
// path 为空时读取 DefaultPath；默认路径不存在会返回 DefaultRegistry，显式 path 不存在则返回错误。
func Load(path string) (Registry, error) {
	explicit := strings.TrimSpace(path) != ""
	if !explicit {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return DefaultRegistry(), nil
		}
		return Registry{}, err
	}
	registry, err := Parse(data)
	if err != nil {
		return Registry{}, fmt.Errorf("parse connection registry %q: %w", path, err)
	}
	return registry, nil
}

// Parse 解析 connections.yaml 的严格 YAML 子集。
// 当前 schema 只支持两空格缩进的 map + scalar；未知字段、list 和非法缩进都会失败，避免配置静默无效。
func Parse(data []byte) (Registry, error) {
	return parseRegistry(data)
}

// Normalize 返回规范化后的 registry。
// 它会补齐默认值、校验 endpoint id 和 transport/connect mode，并选择稳定 default endpoint。
func (registry Registry) Normalize() (Registry, error) {
	if registry.Version == 0 {
		registry.Version = 1
	}
	if registry.Version != 1 {
		return Registry{}, fmt.Errorf("unsupported version %d", registry.Version)
	}
	if len(registry.Connections) == 0 {
		return DefaultRegistry(), nil
	}
	normalized := Registry{
		Version:     registry.Version,
		Default:     normalizeEndpointID(registry.Default),
		Connections: make(map[EndpointID]Config, len(registry.Connections)),
	}
	for id, cfg := range registry.Connections {
		if cfg.ID == "" {
			cfg.ID = id
		}
		cfg.ID = normalizeEndpointID(cfg.ID)
		if err := validateEndpointID(cfg.ID); err != nil {
			return Registry{}, err
		}
		if id = normalizeEndpointID(id); id != "" && id != cfg.ID {
			return Registry{}, fmt.Errorf("connection key %q does not match id %q", id, cfg.ID)
		}
		cfg = cfg.withDefaults()
		if err := cfg.Validate(); err != nil {
			return Registry{}, err
		}
		normalized.Connections[cfg.ID] = cfg
	}
	if normalized.Default == "" {
		normalized.Default = chooseDefaultEndpoint(normalized.Connections)
	}
	if normalized.Default == "" {
		return Registry{}, fmt.Errorf("no enabled connections")
	}
	defaultConnection, ok := normalized.Connections[normalized.Default]
	if !ok {
		return Registry{}, fmt.Errorf("default connection %q not found", normalized.Default)
	}
	if !defaultConnection.Enabled {
		return Registry{}, fmt.Errorf("default connection %q is disabled", normalized.Default)
	}
	return normalized, nil
}

// List 返回按 endpoint id 稳定排序的 connection 配置。
// UI 和 CLI 列表必须使用稳定顺序，避免 map 顺序造成 picker/diagnostic 抖动。
func (registry Registry) List() []Config {
	ids := make([]string, 0, len(registry.Connections))
	for id := range registry.Connections {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	out := make([]Config, 0, len(ids))
	for _, id := range ids {
		out = append(out, registry.Connections[EndpointID(id)])
	}
	return out
}

// DefaultConnection 返回 registry 当前 default endpoint 配置。
// 返回 false 表示 registry 未规范化或 default 指向缺失 endpoint。
func (registry Registry) DefaultConnection() (Config, bool) {
	cfg, ok := registry.Connections[registry.Default]
	return cfg, ok
}

// Validate 校验单个 connection 配置的字段语义。
// 它不做网络 IO，也不验证 SSH host key；安全身份校验属于具体 transport 连接阶段。
func (cfg Config) Validate() error {
	if err := validateEndpointID(cfg.ID); err != nil {
		return err
	}
	switch cfg.Transport {
	case TransportLocal:
		if strings.TrimSpace(cfg.Address) != "" {
			return fmt.Errorf("local connection %q must not set address", cfg.ID)
		}
	case TransportSSH:
		if strings.TrimSpace(cfg.Address) == "" {
			return fmt.Errorf("ssh connection %q requires address", cfg.ID)
		}
	case TransportHubP2P:
		return fmt.Errorf("hub-p2p connection %q is not enabled in this workflow slice", cfg.ID)
	default:
		return fmt.Errorf("connection %q has unknown transport %q", cfg.ID, cfg.Transport)
	}
	switch cfg.ConnectMode {
	case ConnectAuto, ConnectOnDemand, ConnectManual:
	default:
		return fmt.Errorf("connection %q has unknown connect_mode %q", cfg.ID, cfg.ConnectMode)
	}
	return nil
}

// DialIdentity 返回该 connection 的运行时连接身份。
// registry reload 后只要该值变化，已连接 session 就不能热切换，必须标记 reconnect required。
func (cfg Config) DialIdentity() DialIdentity {
	cfg = cfg.withDefaults()
	return DialIdentity{
		Transport:    cfg.Transport,
		Address:      strings.TrimSpace(cfg.Address),
		AuthRef:      strings.TrimSpace(cfg.AuthRef),
		Socket:       strings.TrimSpace(cfg.Socket),
		RemoteSocket: strings.TrimSpace(cfg.RemoteSocket),
	}
}

// RequiresReconnect 判断配置变化是否会改变已连接 session 的 dial identity。
// label、enabled 和 connect_mode 不会触发 reconnect；transport/address/auth/socket 变化必须由用户显式重连或下次连接生效。
func (cfg Config) RequiresReconnect(next Config) bool {
	return cfg.DialIdentity() != next.DialIdentity()
}

// DisplayChanged 判断配置变化是否只影响 UI 展示名称。
// 调用方可以把 label 变化热更新到 picker/pane chrome，但不能据此修改 terminal lifecycle truth。
func (cfg Config) DisplayChanged(next Config) bool {
	return strings.TrimSpace(cfg.Label) != strings.TrimSpace(next.Label)
}

func defaultLocalConfig(label string) Config {
	label = strings.TrimSpace(label)
	if label == "" {
		label = string(DefaultEndpointID)
	}
	return Config{
		ID:          DefaultEndpointID,
		Label:       label,
		Transport:   TransportLocal,
		ConnectMode: ConnectAuto,
		Enabled:     true,
		Socket:      "auto",
	}
}

func (cfg Config) withDefaults() Config {
	cfg.ID = normalizeEndpointID(cfg.ID)
	if strings.TrimSpace(cfg.Label) == "" {
		cfg.Label = string(cfg.ID)
	}
	if cfg.Transport == "" {
		if cfg.ID == DefaultEndpointID {
			cfg.Transport = TransportLocal
		}
	}
	if cfg.ConnectMode == "" {
		if cfg.ID == DefaultEndpointID {
			cfg.ConnectMode = ConnectAuto
		} else {
			cfg.ConnectMode = ConnectOnDemand
		}
	}
	if cfg.Transport == TransportLocal && strings.TrimSpace(cfg.Socket) == "" {
		cfg.Socket = "auto"
	}
	if cfg.Transport == TransportSSH && strings.TrimSpace(cfg.RemoteSocket) == "" {
		cfg.RemoteSocket = "auto"
	}
	return cfg
}

func normalizeEndpointID(id EndpointID) EndpointID {
	return EndpointID(strings.TrimSpace(string(id)))
}

func validateEndpointID(id EndpointID) error {
	value := strings.TrimSpace(string(id))
	if value == "" {
		return fmt.Errorf("connection id is required")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("connection id %q contains invalid character %q", value, r)
		}
	}
	return nil
}

func chooseDefaultEndpoint(connections map[EndpointID]Config) EndpointID {
	if cfg, ok := connections[DefaultEndpointID]; ok && cfg.Enabled {
		return DefaultEndpointID
	}
	for _, cfg := range sortedConfigs(connections) {
		if cfg.Enabled {
			return cfg.ID
		}
	}
	return ""
}

func sortedConfigs(connections map[EndpointID]Config) []Config {
	registry := Registry{Connections: connections}
	return registry.List()
}
