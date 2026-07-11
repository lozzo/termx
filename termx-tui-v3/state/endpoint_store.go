package state

import (
	"sort"
	"strings"

	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/connection"
)

const (
	// EndpointTransportLocal 表示通过本机 unix socket 访问 daemon。
	// 该值只用于 TUI reducer/view-model 展示，真实 dial 仍由 endpoint manager 和 transport 层负责。
	EndpointTransportLocal EndpointTransportKind = "local"
	// EndpointTransportSSH 表示通过 SSH 访问远端 daemon。
	// SSH host key 与认证结果不由 label 或 endpoint id 表达，必须在 transport 连接阶段处理。
	EndpointTransportSSH EndpointTransportKind = "ssh"
	// EndpointTransportHubP2P 是未来 hub/P2P transport 的 UI 占位。
	// 展示层只能显示 hub 连接配置与运行时状态，真实身份校验仍由 hub transport 完成。
	EndpointTransportHubP2P EndpointTransportKind = "hub-p2p"
)

const (
	// EndpointConnectAuto 表示 endpoint 会在启动或恢复需要时自动连接。
	// 它来自 connections.yaml 的 connect_mode，不表示当前已经 online。
	EndpointConnectAuto EndpointConnectMode = "auto"
	// EndpointConnectOnDemand 表示 endpoint 只在 picker 展开、搜索命中或可见 restore 需要时连接。
	EndpointConnectOnDemand EndpointConnectMode = "on_demand"
	// EndpointConnectManual 表示 endpoint 只响应显式 connect action。
	EndpointConnectManual EndpointConnectMode = "manual"
)

const (
	// EndpointStatusUnknown 表示 reducer 尚未收到 registry 或连接状态。
	// renderer 不应把它解释成 online，只能展示为未初始化状态。
	EndpointStatusUnknown EndpointStatusKind = ""
	// EndpointStatusConnected 表示该 endpoint 最近一次 list/connect 成功。
	// 这只是 TUI 已知的运行时投影，terminal lifecycle truth 仍在 owning daemon。
	EndpointStatusConnected EndpointStatusKind = "connected"
	// EndpointStatusConnecting 表示 endpoint manager 正在建立连接。
	// ME004 只提供展示状态，真实连接流程在后续 ME007/ME008 接入。
	EndpointStatusConnecting EndpointStatusKind = "connecting"
	// EndpointStatusAuto 表示 auto endpoint 已配置但当前没有已知连接结果。
	// 它和 connect mode 同名，是为了让 picker 能展示启动期的连接策略状态。
	EndpointStatusAuto EndpointStatusKind = "auto"
	// EndpointStatusOnDemand 表示 on_demand endpoint 当前处于待需连接状态。
	EndpointStatusOnDemand EndpointStatusKind = "on_demand"
	// EndpointStatusManual 表示 manual endpoint 当前等待显式 connect action。
	EndpointStatusManual EndpointStatusKind = "manual"
	// EndpointStatusDisabled 表示 registry 中该 endpoint 被禁用。
	// 禁用不会自动断开已有 session，但禁止未来自动连接、恢复和创建。
	EndpointStatusDisabled EndpointStatusKind = "disabled"
	// EndpointStatusOffline 表示该 endpoint 最近一次连接或 list 失败。
	// 该失败只能影响本 endpoint，不能清空其他 endpoint 的 terminal pool。
	EndpointStatusOffline EndpointStatusKind = "offline"
	// EndpointStatusReconnectRequired 表示 registry reload 后 dial identity 已变化。
	// 当前 session 不能热切换，必须等显式 reconnect/disconnect 后再使用新配置。
	EndpointStatusReconnectRequired EndpointStatusKind = "reconnect-required"
	// EndpointStatusUnregistered 表示运行时仍有该 endpoint 的 session 或 terminal，但 registry 已删除。
	// 已有展示可以继续存在，重启后 workbench binding 应进入 unresolved。
	EndpointStatusUnregistered EndpointStatusKind = "unregistered"
)

const (
	// EndpointErrorUnknown 表示当前 endpoint 没有可分类错误，或错误还未进入 reducer。
	EndpointErrorUnknown EndpointErrorKind = ""
	// EndpointErrorTransportClosed 表示已有 transport 主动关闭，例如 SSH 子进程退出。
	// 这是 endpoint 连接生命周期事件，不应作为全局 toast 独占展示。
	EndpointErrorTransportClosed EndpointErrorKind = "transport-closed"
	// EndpointErrorTransportDial 表示 transport 建连失败，通常发生在 lazy dial 或 auto connect 阶段。
	EndpointErrorTransportDial EndpointErrorKind = "transport-dial"
	// EndpointErrorAuth 表示认证失败；该分类只服务 UI 展示，不替代 SSH/transport 层的安全判断。
	EndpointErrorAuth EndpointErrorKind = "auth"
	// EndpointErrorHostKey 表示 host key 校验失败，必须作为高风险 endpoint 状态展示。
	EndpointErrorHostKey EndpointErrorKind = "host-key"
	// EndpointErrorRemoteDaemon 表示远端 termx daemon、stdio-proxy 或 socket 不可用。
	EndpointErrorRemoteDaemon EndpointErrorKind = "remote-daemon"
	// EndpointErrorProtocol 表示 transport 已连通但 protocol 层返回错误或断开。
	EndpointErrorProtocol EndpointErrorKind = "protocol"
	// EndpointErrorConfig 表示 registry、disabled endpoint 或 unsupported transport 这类本地配置错误。
	EndpointErrorConfig EndpointErrorKind = "config"
	// EndpointErrorUnavailable 表示无法进一步分类的连接不可达错误。
	EndpointErrorUnavailable EndpointErrorKind = "unavailable"
)

// EndpointTransportKind 是 TUI 展示层使用的 transport 枚举。
// 它只表达到达 daemon 的方式，不承担安全身份、认证状态或 host key 校验。
type EndpointTransportKind string

// EndpointConnectMode 是 TUI 展示层使用的连接策略枚举。
// 它来自 connection registry，只影响未来连接时机，不热切换已连接 session。
type EndpointConnectMode string

// EndpointStatusKind 是 endpoint manager 回投给 reducer 的运行时展示状态。
// renderer 只能消费该状态；真正的 terminal list、history 和 lifecycle 仍属于 owning daemon。
type EndpointStatusKind string

// EndpointErrorKind 是 endpoint-scoped 失败类型投影。
// 它来自 transport/protocol/service 错误分类，只用于 TUI 展示和局部状态标记；
// renderer 不得根据它重试、fallback 或改写 terminal lifecycle truth。
type EndpointErrorKind string

// EndpointItem 是 reducer-owned endpoint 展示投影。
// ID 是 workbench/路由主键；Label/Transport/ConnectMode 来自 registry；
// Status/LastError 来自 endpoint manager 的运行时消息；DefaultCommand/DefaultCWD
// 来自 owning daemon 的 path.defaults，不得由 TUI 本地环境推断。
type EndpointItem struct {
	ID                EndpointID
	Label             string
	Transport         EndpointTransportKind
	Address           string
	AuthRef           string
	ConnectMode       EndpointConnectMode
	Enabled           bool
	Socket            string
	RemoteSocket      string
	HubURL            string
	HubDeviceID       string
	DeviceFingerprint string
	GrantRef          string
	RelayMode         string
	// ObservedPath 是当前 managed WebRTC session 的 direct/single_relay/relay_mesh 投影。
	// registry reload 不提供该值；断线或新 session 会由 endpoint runtime event 更新。
	ObservedPath string
	// RouteSelectionReason 是当前 SmartRoute session 的稳定公开原因；非 SmartRoute endpoint 保持为空。
	RouteSelectionReason string
	Status               EndpointStatusKind
	LastError            string
	LastErrorKind        EndpointErrorKind
	TerminalCount        int
	ReconnectRequired    bool
	Unregistered         bool
	DefaultCommand       []string
	DefaultCWD           string
	DefaultsLoaded       bool
	DefaultsError        string
}

// EndpointStore 是 TUI/client 侧 endpoint registry 与运行时连接状态的 reducer-owned 投影。
// connections.yaml 是期望状态，已连接 session 是运行时事实；该 store 只保存两者 diff 后的 UI 可见结果。
type EndpointStore struct {
	Items []EndpointItem
}

// EndpointItemFromConnectionConfig 把共享 connection registry 配置转换成 TUI endpoint 投影。
// 该转换不做网络 IO，也不验证凭据；失败和 host key 只能由后续 transport 连接消息回投。
func EndpointItemFromConnectionConfig(cfg connection.Config) EndpointItem {
	endpointID := EndpointID(strings.TrimSpace(string(cfg.ID)))
	if endpointID == "" {
		endpointID = DefaultEndpointID
	}
	return EndpointItem{
		ID:                NormalizeEndpointID(endpointID),
		Label:             strings.TrimSpace(cfg.Label),
		Transport:         EndpointTransportKind(strings.TrimSpace(string(cfg.Transport))),
		Address:           strings.TrimSpace(cfg.Address),
		AuthRef:           strings.TrimSpace(cfg.AuthRef),
		ConnectMode:       EndpointConnectMode(strings.TrimSpace(string(cfg.ConnectMode))),
		Enabled:           cfg.Enabled,
		Socket:            strings.TrimSpace(cfg.Socket),
		RemoteSocket:      strings.TrimSpace(cfg.RemoteSocket),
		HubURL:            strings.TrimSpace(cfg.HubURL),
		HubDeviceID:       strings.TrimSpace(cfg.HubDeviceID),
		DeviceFingerprint: strings.TrimSpace(cfg.DeviceFingerprint),
		GrantRef:          strings.TrimSpace(cfg.GrantRef),
		RelayMode:         strings.TrimSpace(string(cfg.RelayMode)),
	}
}

// ApplyConnectionRegistry 用新的 connections.yaml 快照更新 endpoint 展示配置。
// 已存在的运行时状态会按 endpoint id 保留；被删除但仍有运行时状态的 endpoint 会标记为 unregistered。
func (store EndpointStore) ApplyConnectionRegistry(registry connection.Registry) EndpointStore {
	next := EndpointStore{}
	seen := map[EndpointID]struct{}{}
	for _, cfg := range registry.List() {
		item := EndpointItemFromConnectionConfig(cfg).withDefaults()
		if previous, ok := store.Endpoint(item.ID); ok {
			item.Status = previous.Status
			item.LastError = previous.LastError
			item.LastErrorKind = previous.LastErrorKind
			item.TerminalCount = previous.TerminalCount
			item.ReconnectRequired = previous.ReconnectRequired || previous.RequiresReconnect(item)
			item.ObservedPath = previous.ObservedPath
			item.RouteSelectionReason = previous.RouteSelectionReason
			item.DefaultCommand = append([]string(nil), previous.DefaultCommand...)
			item.DefaultCWD = previous.DefaultCWD
			item.DefaultsLoaded = previous.DefaultsLoaded
			item.DefaultsError = previous.DefaultsError
		}
		next.Items = append(next.Items, item)
		seen[item.ID] = struct{}{}
	}
	for _, item := range store.Items {
		item = item.withDefaults()
		if _, ok := seen[item.ID]; ok {
			continue
		}
		item.Unregistered = true
		item.Status = EndpointStatusUnregistered
		next.Items = append(next.Items, item)
	}
	return next.Normalize()
}

// Normalize 返回带有稳定排序和默认字段的 EndpointStore。
// 它不合并不同 endpoint，也不会把 label 当成 identity；排序只为了 UI 和测试稳定。
func (store EndpointStore) Normalize() EndpointStore {
	if len(store.Items) == 0 {
		return EndpointStore{}
	}
	items := cloneEndpointItems(store.Items)
	for index := range items {
		items[index] = items[index].withDefaults()
	}
	sort.SliceStable(items, func(i, j int) bool {
		return string(items[i].ID) < string(items[j].ID)
	})
	out := make([]EndpointItem, 0, len(items))
	seen := map[EndpointID]struct{}{}
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		out = append(out, item)
		seen[item.ID] = struct{}{}
	}
	return EndpointStore{Items: out}
}

// HasItems 表示 reducer 已经加载了 endpoint registry 或运行时 endpoint 状态。
// renderer 用它决定是否启用多 endpoint 分组展示，避免旧单本地测试在未接 registry 前抖动。
func (store EndpointStore) HasItems() bool {
	return len(store.Items) > 0
}

// Endpoint 返回指定 endpoint 的展示投影。
// 找不到时返回 false；调用方若需要展示 orphan terminal，应显式使用 UnregisteredEndpoint。
func (store EndpointStore) Endpoint(endpointID EndpointID) (EndpointItem, bool) {
	endpointID = NormalizeEndpointID(endpointID)
	for _, item := range store.Items {
		item = item.withDefaults()
		if item.ID == endpointID {
			return item, true
		}
	}
	return EndpointItem{}, false
}

// DisplayEndpoint 返回适合 row view-model 使用的 endpoint 投影。
// 已加载 registry 时，缺失 endpoint 会被标记为 unregistered；未加载 registry 时返回 false 保持旧本地紧凑展示。
func (store EndpointStore) DisplayEndpoint(endpointID EndpointID) (EndpointItem, bool) {
	endpointID = NormalizeEndpointID(endpointID)
	if item, ok := store.Endpoint(endpointID); ok {
		return item, true
	}
	if !store.HasItems() {
		return EndpointItem{}, false
	}
	return UnregisteredEndpoint(endpointID), true
}

// Upsert 插入或替换一个 endpoint 展示投影。
// 它用于测试 harness 和后续 endpoint manager 消息回投；重复 ID 只保留最后一次写入的状态。
func (store EndpointStore) Upsert(item EndpointItem) EndpointStore {
	item = item.withDefaults()
	items := cloneEndpointItems(store.Items)
	replaced := false
	for index := range items {
		if NormalizeEndpointID(items[index].ID) == item.ID {
			items[index] = item
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, item)
	}
	return (EndpointStore{Items: items}).Normalize()
}

// MarkTerminalListResult 记录某个 endpoint 的 terminal list 结果。
// 成功只更新该 endpoint 的数量和 connected 状态；失败只把该 endpoint 标为 offline，不影响其他 endpoint 的列表真值。
func (store EndpointStore) MarkTerminalListResult(endpointID EndpointID, terminalCount int, err string) EndpointStore {
	return store.MarkRuntimeStatus(endpointID, EndpointStatusUnknown, EndpointErrorUnknown, terminalCount, err)
}

// MarkRuntimeStatus 记录 endpoint manager 主动回投的连接状态。
// Status/ErrorKind 来自 endpoint-scoped service/transport 事件；该方法只更新对应 endpoint，
// 不删除 terminal pool、workbench binding 或其他 endpoint 的状态。
func (store EndpointStore) MarkRuntimeStatus(endpointID EndpointID, status EndpointStatusKind, errorKind EndpointErrorKind, terminalCount int, err string) EndpointStore {
	endpointID = NormalizeEndpointID(endpointID)
	item, ok := store.DisplayEndpoint(endpointID)
	if !ok {
		item = DefaultLocalEndpoint()
		item.ID = endpointID
	}
	item.TerminalCount = terminalCount
	item.LastError = strings.TrimSpace(err)
	item.LastErrorKind = NormalizeEndpointErrorKind(errorKind)
	if item.LastError != "" {
		if item.LastErrorKind == EndpointErrorUnknown {
			item.LastErrorKind = ClassifyEndpointErrorText(item.LastError)
		}
		if status == EndpointStatusUnknown {
			status = EndpointStatusOffline
		}
	} else if status == EndpointStatusUnknown {
		status = EndpointStatusConnected
	}
	if item.LastError == "" && status == EndpointStatusConnected {
		item.LastErrorKind = EndpointErrorUnknown
	}
	item.Status = status
	return store.Upsert(item)
}

// MarkManagedRoute 原子更新单个 managed endpoint 的实际 WebRTC 路径和 SmartRoute 原因。
// 空路径会同时清除旧原因；非法组合保持原状态，避免 renderer 看到新路径和旧诊断混合。
func (store EndpointStore) MarkManagedRoute(endpointID EndpointID, observedPath, selectionReason string) EndpointStore {
	endpointID = NormalizeEndpointID(endpointID)
	item, ok := store.DisplayEndpoint(endpointID)
	if !ok {
		return store
	}
	switch observedPath = strings.TrimSpace(observedPath); observedPath {
	case "", "direct", "single_relay", "relay_mesh":
	default:
		return store
	}
	selectionReason = strings.TrimSpace(selectionReason)
	if observedPath == "" {
		selectionReason = ""
	} else if selectionReason != "" && !cloudcompanion.IsKnownRouteSelectionReason(cloudcompanion.RouteSelectionReason(selectionReason)) {
		return store
	}
	item.ObservedPath = observedPath
	item.RouteSelectionReason = selectionReason
	return store.Upsert(item)
}

// ApplyDefaults 记录某个 endpoint daemon 返回的创建默认值。
// 成功结果成为后续 create prompt/submit 的 truth；失败只记录错误，不用本地环境补洞。
func (store EndpointStore) ApplyDefaults(endpointID EndpointID, command []string, cwd string, err string) EndpointStore {
	endpointID = NormalizeEndpointID(endpointID)
	item, ok := store.DisplayEndpoint(endpointID)
	if !ok {
		item = DefaultLocalEndpoint()
		item.ID = endpointID
	}
	item.DefaultsError = strings.TrimSpace(err)
	if item.DefaultsError == "" {
		item.DefaultCommand = append([]string(nil), command...)
		item.DefaultCWD = strings.TrimSpace(cwd)
		item.DefaultsLoaded = true
	}
	return store.Upsert(item)
}

// DefaultLocalEndpoint 返回缺省 local endpoint 的展示投影。
// 该默认值只用于 TUI state/view-model，不代表已经完成 local socket 连接。
func DefaultLocalEndpoint() EndpointItem {
	return EndpointItem{
		ID:          DefaultEndpointID,
		Label:       string(DefaultEndpointID),
		Transport:   EndpointTransportLocal,
		ConnectMode: EndpointConnectAuto,
		Enabled:     true,
		Status:      EndpointStatusAuto,
	}
}

// UnregisteredEndpoint 返回 registry 缺失但运行时仍可见的 endpoint 投影。
// 它用于删除 connection 后继续展示已有 session/terminal，避免 layout 或 terminal pool 被错误清空。
func UnregisteredEndpoint(endpointID EndpointID) EndpointItem {
	return EndpointItem{
		ID:           NormalizeEndpointID(endpointID),
		Label:        string(NormalizeEndpointID(endpointID)),
		Status:       EndpointStatusUnregistered,
		Enabled:      false,
		Unregistered: true,
	}
}

// DisplayLabel 返回 endpoint 在 picker/manager 中显示的机器名称。
// label 缺失时退回 endpoint id；该值只用于 UI，不参与安全身份判断。
func (item EndpointItem) DisplayLabel() string {
	item = item.withDefaults()
	if strings.TrimSpace(item.Label) != "" {
		return strings.TrimSpace(item.Label)
	}
	return string(item.ID)
}

// DisplayStatus 返回 endpoint 当前应展示的状态。
// reconnect-required、unregistered 和 disabled 具有最高优先级，避免连接策略掩盖运行时风险。
func (item EndpointItem) DisplayStatus() EndpointStatusKind {
	item = item.withDefaults()
	switch {
	case item.Unregistered:
		return EndpointStatusUnregistered
	case item.ReconnectRequired:
		return EndpointStatusReconnectRequired
	case !item.Enabled:
		return EndpointStatusDisabled
	case item.Status != EndpointStatusUnknown:
		return item.Status
	}
	switch item.ConnectMode {
	case EndpointConnectOnDemand:
		return EndpointStatusOnDemand
	case EndpointConnectManual:
		return EndpointStatusManual
	case EndpointConnectAuto:
		return EndpointStatusAuto
	default:
		return EndpointStatusUnknown
	}
}

// DisplayErrorLabel 返回 endpoint 错误类型和原始摘要的组合展示文本。
// 错误类型先显示，便于 picker/manager/workbench 在有限宽度内区分 auth、host-key、transport closed 等失败。
func (item EndpointItem) DisplayErrorLabel() string {
	item = item.withDefaults()
	errText := strings.TrimSpace(item.LastError)
	errKind := NormalizeEndpointErrorKind(item.LastErrorKind)
	if errKind == EndpointErrorUnknown && errText != "" {
		errKind = ClassifyEndpointErrorText(errText)
	}
	if errKind == EndpointErrorUnknown {
		return errText
	}
	if errText == "" {
		return string(errKind)
	}
	return string(errKind) + ": " + errText
}

// NormalizeEndpointErrorKind 把未知错误类型归入空值，避免 renderer 展示拼写错误的状态。
func NormalizeEndpointErrorKind(kind EndpointErrorKind) EndpointErrorKind {
	switch kind {
	case EndpointErrorTransportClosed,
		EndpointErrorTransportDial,
		EndpointErrorAuth,
		EndpointErrorHostKey,
		EndpointErrorRemoteDaemon,
		EndpointErrorProtocol,
		EndpointErrorConfig,
		EndpointErrorUnavailable:
		return kind
	default:
		return EndpointErrorUnknown
	}
}

// ClassifyEndpointErrorText 根据服务层错误摘要生成 endpoint 错误类型。
// 该分类只影响 UI 标志；真实认证、host key 和 transport 行为仍由 connection/transport/protocol 层负责。
func ClassifyEndpointErrorText(text string) EndpointErrorKind {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case lower == "":
		return EndpointErrorUnknown
	case strings.Contains(lower, "host key") ||
		strings.Contains(lower, "remote host identification") ||
		strings.Contains(lower, "known_hosts"):
		return EndpointErrorHostKey
	case strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "publickey") ||
		strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "auth"):
		return EndpointErrorAuth
	case strings.Contains(lower, "stdio-proxy") ||
		strings.Contains(lower, "remote socket") ||
		strings.Contains(lower, "daemon") ||
		strings.Contains(lower, "no such file or directory"):
		return EndpointErrorRemoteDaemon
	case strings.Contains(lower, "ssh transport closed") ||
		strings.Contains(lower, "transport closed") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused"):
		return EndpointErrorTransportClosed
	case strings.Contains(lower, "dial") ||
		strings.Contains(lower, "connect timeout") ||
		strings.Contains(lower, "no route to host") ||
		strings.Contains(lower, "network is unreachable"):
		return EndpointErrorTransportDial
	case strings.Contains(lower, "protocol error") ||
		strings.Contains(lower, "unexpected frame") ||
		strings.Contains(lower, "eof"):
		return EndpointErrorProtocol
	case strings.Contains(lower, "not registered") ||
		strings.Contains(lower, "disabled") ||
		strings.Contains(lower, "transport") && strings.Contains(lower, "not connected"):
		return EndpointErrorConfig
	default:
		return EndpointErrorUnavailable
	}
}

// RequiresReconnect 判断 registry reload 后是否改变了 endpoint dial identity。
// 该判断只覆盖 TUI 展示字段，完整 dial identity 仍由 termx-shared/connection.Config 负责。
func (item EndpointItem) RequiresReconnect(next EndpointItem) bool {
	item = item.withDefaults()
	next = next.withDefaults()
	return item.Transport != next.Transport ||
		strings.TrimSpace(item.Address) != strings.TrimSpace(next.Address) ||
		strings.TrimSpace(item.AuthRef) != strings.TrimSpace(next.AuthRef) ||
		strings.TrimSpace(item.Socket) != strings.TrimSpace(next.Socket) ||
		strings.TrimSpace(item.RemoteSocket) != strings.TrimSpace(next.RemoteSocket) ||
		strings.TrimSpace(item.HubURL) != strings.TrimSpace(next.HubURL) ||
		strings.TrimSpace(item.HubDeviceID) != strings.TrimSpace(next.HubDeviceID) ||
		strings.TrimSpace(item.DeviceFingerprint) != strings.TrimSpace(next.DeviceFingerprint) ||
		strings.TrimSpace(item.GrantRef) != strings.TrimSpace(next.GrantRef) ||
		strings.TrimSpace(item.RelayMode) != strings.TrimSpace(next.RelayMode)
}

func (item EndpointItem) withDefaults() EndpointItem {
	item.ID = NormalizeEndpointID(item.ID)
	item.DefaultCommand = append([]string(nil), item.DefaultCommand...)
	if strings.TrimSpace(item.Label) == "" {
		item.Label = string(item.ID)
	}
	if strings.TrimSpace(string(item.Transport)) == "" {
		if item.ID == DefaultEndpointID {
			item.Transport = EndpointTransportLocal
		}
	}
	if strings.TrimSpace(string(item.ConnectMode)) == "" {
		if item.ID == DefaultEndpointID {
			item.ConnectMode = EndpointConnectAuto
		} else {
			item.ConnectMode = EndpointConnectOnDemand
		}
	}
	return item
}

func cloneEndpointItems(items []EndpointItem) []EndpointItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]EndpointItem, len(items))
	copy(cloned, items)
	for index := range cloned {
		cloned[index].DefaultCommand = append([]string(nil), cloned[index].DefaultCommand...)
	}
	return cloned
}
