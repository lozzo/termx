package plugin

import (
	"fmt"
	"strings"
)

// RunnerType 描述插件代码的运行适配器类型。
// 它只说明 host 如何启动代码，不赋予插件任何 action 或 hook 权限。
type RunnerType string

const (
	// RunnerBuiltin 表示插件逻辑由 host 内建实现提供。
	RunnerBuiltin RunnerType = "builtin"
	// RunnerStdioJSON 表示插件通过 stdio JSON 协议和 host 交换消息。
	RunnerStdioJSON RunnerType = "stdio_json"
)

// CapabilityHookReceiveSelfCaused 是允许 hook 接收自身因果链事件的显式权限。
// 默认 hook 不接收 self-caused 事件，避免 hook/action 循环成为插件可绕过的默认行为。
const CapabilityHookReceiveSelfCaused Capability = "hook.receive_self_caused"

// PluginManifest 是插件安装包提供的声明式元数据。
// host 必须把它和安装来源、grant、event catalog 一起解析，不能把 manifest 当作最终权限事实。
type PluginManifest struct {
	ID    PluginID
	Name  string
	API   int
	Hosts []PluginHostManifest
}

// PluginHostManifest 描述插件在一个 host placement 下的声明。
// Capabilities 是插件申请的最小权限声明，最终是否可用必须由 host grant 决定。
type PluginHostManifest struct {
	Placement     HostPlacement
	ClientKinds   []ClientKind
	Runner        RunnerSpec
	Capabilities  []Capability
	Contributions PluginContributions
}

// RunnerSpec 描述 host 启动插件代码所需的纯配置。
// PL002 只建模 runner 类型和命令，不执行外部进程，也不绑定具体脚本语言。
type RunnerSpec struct {
	Type    RunnerType
	Command []string
}

// PluginContributions 是插件向宿主声明的 action/keybinding/hook 入口。
// 这些贡献点只有在 namespace、event catalog 和 grant 都通过后才会进入 host catalog。
type PluginContributions struct {
	Actions     []ActionContribution
	Keybindings []KeybindingContribution
	Hooks       []HookContribution
}

// ActionContribution 是 manifest 中的 action 声明。
// 它会被 host 解析成 ActionSpec；OwnerPluginID 和最终 capability 不能由 runner 自报。
type ActionContribution struct {
	ID                   ActionID
	Title                string
	Scope                ActionScope
	SupportedClientKinds []ClientKind
	RequiredCaps         []Capability
	ClientRequiredCaps   []Capability
	DaemonRequiredCaps   []Capability
	Danger               DangerLevel
	ParamsSchema         string
	Idempotent           bool
}

// KeybindingContribution 声明一组按键序列到 action 的绑定。
// 快捷键只指向 ActionID；同一个 action 可以拥有多组 key sequence。
type KeybindingContribution struct {
	Keys        []string
	ActionID    ActionID
	Args        []byte
	ClientKinds []ClientKind
}

// HookContribution 是 manifest 中的 hook 订阅声明。
// host 会根据 EventSpec 推导 ResolvedCaps，runner 不能在订阅请求里自报权限。
type HookContribution struct {
	EventType         EventType
	Handler           string
	Scope             HookScope
	Filters           []HookFilter
	Delivery          HookDelivery
	ReceiveSelfCaused bool
}

// EventSpec 是 host 预定义 hook event catalog 中的事实。
// termx.* 系统事件只能由拥有 truth 的 host 注册，第三方 manifest 只能订阅。
type EventSpec struct {
	Type            EventType
	SourceHost      HostPlacement
	DefaultDelivery HookDelivery
	RequiredCaps    []Capability
	Lossy           bool
	ObjectKind      string
}

// CapabilityGrant 是 host 安装或信任流程产生的授权事实。
// grant 按 plugin+host placement 生效；Host 为空表示该授权可用于该插件的所有 placement。
type CapabilityGrant struct {
	GrantID      string
	PluginID     PluginID
	Host         HostPlacement
	Capabilities []Capability
	Trusted      bool
}

// CatalogBuildConfig 描述 host 解析 manifest 所需的外部事实。
// TrustedTermXPlugins 是 host 派生的 first-party 白名单，不能由 manifest 自己声明。
type CatalogBuildConfig struct {
	EventCatalog        []EventSpec
	Grants              []CapabilityGrant
	TrustedTermXPlugins []PluginID
}

// CatalogDiagnosticReason 是贡献点未进入 catalog 的稳定原因。
// 它用于测试、安装诊断和后续 UI 展示，不携带动态错误文本。
type CatalogDiagnosticReason string

const (
	// DiagnosticMissingCapability 表示 manifest 贡献点缺少对应 host grant。
	DiagnosticMissingCapability CatalogDiagnosticReason = "missing_capability"
	// DiagnosticUnknownAction 表示 keybinding 指向的 action 没有进入 catalog。
	DiagnosticUnknownAction CatalogDiagnosticReason = "unknown_action"
	// DiagnosticUnsupportedContribution 表示贡献点存在但不适合当前 host/action authority。
	DiagnosticUnsupportedContribution CatalogDiagnosticReason = "unsupported_contribution"
)

// CatalogDiagnostic 描述 manifest 贡献点被 host 跳过的原因。
// 结构错误会直接返回 error；授权不足这类可恢复问题进入 diagnostics。
type CatalogDiagnostic struct {
	PluginID PluginID
	Host     HostPlacement
	Kind     string
	ID       string
	Reason   CatalogDiagnosticReason
}

// ResolvedKeybinding 是 host 解析后的快捷键绑定。
// 它只引用已经进入 ActionSpec catalog 的 action，避免快捷键绕过 action/grant 层。
type ResolvedKeybinding struct {
	PluginID    PluginID
	Keys        []string
	ActionID    ActionID
	Args        []byte
	ClientKinds []ClientKind
}

// PluginCatalog 是 host 从 manifest、event catalog 和 grant 推导出的运行期事实。
// 后续 TUI/daemon/runner 只能消费这个 catalog，不应直接信任原始 manifest。
type PluginCatalog struct {
	Actions       map[ActionID]ActionSpec
	Keybindings   []ResolvedKeybinding
	Subscriptions []HookSubscription
	Events        map[EventType]EventSpec
	Diagnostics   []CatalogDiagnostic
}

// ValidatePluginManifest 执行 manifest 的结构与命名空间基础校验。
// 它不校验 grant，因为权限属于 host 安装状态，不属于插件包自身事实。
func ValidatePluginManifest(manifest PluginManifest, config CatalogBuildConfig) error {
	if err := validatePluginID(manifest.ID); err != nil {
		return err
	}
	trustedTermX := trustedPluginSet(config.TrustedTermXPlugins)
	if strings.HasPrefix(string(manifest.ID), "termx.") && !trustedTermX[manifest.ID] {
		return fmt.Errorf("plugin %s cannot use termx namespace without host trust", manifest.ID)
	}
	if manifest.API <= 0 {
		return fmt.Errorf("plugin %s api version is required", manifest.ID)
	}
	if len(manifest.Hosts) == 0 {
		return fmt.Errorf("plugin %s must declare at least one host", manifest.ID)
	}
	seenHosts := make(map[HostPlacement]struct{}, len(manifest.Hosts))
	for _, host := range manifest.Hosts {
		if _, ok := seenHosts[host.Placement]; ok {
			return fmt.Errorf("plugin %s declares duplicate host %s", manifest.ID, host.Placement)
		}
		seenHosts[host.Placement] = struct{}{}
		if err := validateHostManifest(manifest.ID, host, trustedTermX[manifest.ID]); err != nil {
			return err
		}
	}
	return nil
}

// BuildPluginCatalog 解析 manifest、event catalog 和 grant，生成 host 运行期 catalog。
// 它只做纯模型计算：不启动 runner、不注册 protocol、不修改 TUI 或 daemon state。
func BuildPluginCatalog(manifests []PluginManifest, config CatalogBuildConfig) (PluginCatalog, error) {
	catalog := PluginCatalog{
		Actions: make(map[ActionID]ActionSpec),
		Events:  make(map[EventType]EventSpec),
	}
	for _, event := range config.EventCatalog {
		if event.Type == "" {
			return PluginCatalog{}, fmt.Errorf("event type is required")
		}
		if !strings.HasPrefix(string(event.Type), "termx.") {
			return PluginCatalog{}, fmt.Errorf("system event %s must use termx namespace", event.Type)
		}
		if !validEventSourceHost(event.SourceHost) {
			return PluginCatalog{}, fmt.Errorf("event %s source host is invalid", event.Type)
		}
		if !eventTypeMatchesSourceHost(event.Type, event.SourceHost) {
			return PluginCatalog{}, fmt.Errorf("event %s source host %s mismatches owner namespace", event.Type, event.SourceHost)
		}
		if _, err := resolveHookDelivery(event, HookDelivery{}); err != nil {
			return PluginCatalog{}, err
		}
		if _, exists := catalog.Events[event.Type]; exists {
			return PluginCatalog{}, fmt.Errorf("duplicate event %s", event.Type)
		}
		catalog.Events[event.Type] = cloneEventSpec(event)
	}

	for _, manifest := range manifests {
		if err := ValidatePluginManifest(manifest, config); err != nil {
			return PluginCatalog{}, err
		}
	}
	for _, manifest := range manifests {
		if err := appendManifestToCatalog(&catalog, manifest, config); err != nil {
			return PluginCatalog{}, err
		}
	}
	for _, manifest := range manifests {
		appendManifestKeybindings(&catalog, manifest, config)
	}
	return catalog, nil
}

func appendManifestToCatalog(catalog *PluginCatalog, manifest PluginManifest, config CatalogBuildConfig) error {
	trustedTermX := trustedPluginSet(config.TrustedTermXPlugins)[manifest.ID]
	grants := grantsForPlugin(manifest.ID, config.Grants)
	for _, host := range manifest.Hosts {
		declaredCaps := capabilitySet(host.Capabilities)
		grantedCaps := grantedCapsForHost(grants, host.Placement)
		trustedHost := trustedTermX || trustedGrantForHost(grants, host.Placement)
		for _, action := range host.Contributions.Actions {
			if err := appendActionContribution(catalog, manifest, host, action, declaredCaps, grantedCaps, trustedTermX); err != nil {
				return err
			}
		}
		for _, hook := range host.Contributions.Hooks {
			if err := appendHookContribution(catalog, manifest, host, hook, declaredCaps, grantedCaps, trustedHost); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendManifestKeybindings(catalog *PluginCatalog, manifest PluginManifest, config CatalogBuildConfig) {
	trustedTermX := trustedPluginSet(config.TrustedTermXPlugins)[manifest.ID]
	grants := grantsForPlugin(manifest.ID, config.Grants)
	for _, host := range manifest.Hosts {
		trustedHost := trustedTermX || trustedGrantForHost(grants, host.Placement)
		for _, keybinding := range host.Contributions.Keybindings {
			appendKeybindingContribution(catalog, manifest, host, keybinding, trustedHost)
		}
	}
}

func appendActionContribution(catalog *PluginCatalog, manifest PluginManifest, host PluginHostManifest, action ActionContribution, declaredCaps, grantedCaps map[Capability]struct{}, trustedTermX bool) error {
	if err := validateActionContribution(manifest.ID, action, trustedTermX); err != nil {
		return err
	}
	if _, exists := catalog.Actions[action.ID]; exists {
		return fmt.Errorf("duplicate action %s", action.ID)
	}
	required := requiredActionCaps(action)
	if !allCapsDeclared(declaredCaps, required) {
		return fmt.Errorf("action %s requires undeclared capability", action.ID)
	}
	if !allCapsGranted(grantedCaps, required) {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "action",
			ID:       string(action.ID),
			Reason:   DiagnosticMissingCapability,
		})
		return nil
	}
	supportedKinds := append([]ClientKind(nil), action.SupportedClientKinds...)
	if len(supportedKinds) == 0 && host.Placement == HostClient {
		supportedKinds = append([]ClientKind(nil), host.ClientKinds...)
	}
	catalog.Actions[action.ID] = ActionSpec{
		ID:                   action.ID,
		OwnerPluginID:        manifest.ID,
		Scope:                action.Scope,
		SupportedClientKinds: supportedKinds,
		RequiredCaps:         cloneCaps(action.RequiredCaps),
		ClientRequiredCaps:   cloneCaps(action.ClientRequiredCaps),
		DaemonRequiredCaps:   cloneCaps(action.DaemonRequiredCaps),
		Danger:               action.Danger,
		ParamsSchema:         action.ParamsSchema,
		Idempotent:           action.Idempotent,
	}
	return nil
}

func appendHookContribution(catalog *PluginCatalog, manifest PluginManifest, host PluginHostManifest, hook HookContribution, declaredCaps, grantedCaps map[Capability]struct{}, trustedHost bool) error {
	if hook.EventType == "" {
		return fmt.Errorf("plugin %s hook event type is required", manifest.ID)
	}
	if hook.Handler == "" {
		return fmt.Errorf("plugin %s hook %s handler is required", manifest.ID, hook.EventType)
	}
	event, ok := catalog.Events[hook.EventType]
	if !ok {
		return fmt.Errorf("plugin %s subscribes unknown system event %s", manifest.ID, hook.EventType)
	}
	required := cloneCaps(event.RequiredCaps)
	if hook.ReceiveSelfCaused {
		required = append(required, CapabilityHookReceiveSelfCaused)
	}
	if !allCapsDeclared(declaredCaps, required) {
		return fmt.Errorf("hook %s requires undeclared capability", hook.EventType)
	}
	if !allCapsGranted(grantedCaps, required) {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "hook",
			ID:       string(hook.EventType),
			Reason:   DiagnosticMissingCapability,
		})
		return nil
	}
	if hookScopeEmpty(hook.Scope) && !trustedHost {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "hook",
			ID:       string(hook.EventType),
			Reason:   DiagnosticUnsupportedContribution,
		})
		return nil
	}
	delivery, err := resolveHookDelivery(event, hook.Delivery)
	if err != nil {
		return err
	}
	sub := HookSubscription{
		PluginID:          manifest.ID,
		Host:              host.Placement,
		EventTypes:        []EventType{hook.EventType},
		Scope:             cloneHookScope(hook.Scope),
		Filters:           cloneHookFilters(hook.Filters),
		Delivery:          delivery,
		ResolvedCaps:      required,
		ReceiveSelfCaused: hook.ReceiveSelfCaused,
	}
	if err := ValidateHookSubscription(sub); err != nil {
		return err
	}
	catalog.Subscriptions = append(catalog.Subscriptions, sub)
	return nil
}

func appendKeybindingContribution(catalog *PluginCatalog, manifest PluginManifest, host PluginHostManifest, keybinding KeybindingContribution, trustedHost bool) {
	if len(keybinding.Keys) == 0 || keybinding.ActionID == "" {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "keybinding",
			ID:       string(keybinding.ActionID),
			Reason:   DiagnosticUnknownAction,
		})
		return
	}
	action, ok := catalog.Actions[keybinding.ActionID]
	if !ok {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "keybinding",
			ID:       string(keybinding.ActionID),
			Reason:   DiagnosticUnknownAction,
		})
		return
	}
	if action.Scope != ActionScopeClient {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "keybinding",
			ID:       string(keybinding.ActionID),
			Reason:   DiagnosticUnsupportedContribution,
		})
		return
	}
	if action.OwnerPluginID != manifest.ID && !trustedHost {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "keybinding",
			ID:       string(keybinding.ActionID),
			Reason:   DiagnosticUnsupportedContribution,
		})
		return
	}
	clientKinds := append([]ClientKind(nil), keybinding.ClientKinds...)
	if len(clientKinds) == 0 {
		clientKinds = append([]ClientKind(nil), host.ClientKinds...)
	}
	if len(action.SupportedClientKinds) > 0 && !clientKindsWithin(clientKinds, action.SupportedClientKinds) {
		catalog.Diagnostics = append(catalog.Diagnostics, CatalogDiagnostic{
			PluginID: manifest.ID,
			Host:     host.Placement,
			Kind:     "keybinding",
			ID:       string(keybinding.ActionID),
			Reason:   DiagnosticUnsupportedContribution,
		})
		return
	}
	catalog.Keybindings = append(catalog.Keybindings, ResolvedKeybinding{
		PluginID:    manifest.ID,
		Keys:        append([]string(nil), keybinding.Keys...),
		ActionID:    keybinding.ActionID,
		Args:        append([]byte(nil), keybinding.Args...),
		ClientKinds: clientKinds,
	})
}

func validateHostManifest(pluginID PluginID, host PluginHostManifest, trustedTermX bool) error {
	if !validHostPlacement(host.Placement) {
		return fmt.Errorf("plugin %s host placement is invalid", pluginID)
	}
	if host.Placement == HostClient && len(host.ClientKinds) == 0 {
		return fmt.Errorf("plugin %s client host must declare client kinds", pluginID)
	}
	if len(host.ClientKinds) > 0 && !validClientKinds(host.ClientKinds) {
		return fmt.Errorf("plugin %s host %s client kinds are invalid", pluginID, host.Placement)
	}
	if host.Placement == HostOneShot && (len(host.Contributions.Hooks) > 0 || len(host.Contributions.Keybindings) > 0) {
		return fmt.Errorf("plugin %s one_shot host can only contribute actions", pluginID)
	}
	if host.Placement != HostClient && len(host.Contributions.Keybindings) > 0 {
		return fmt.Errorf("plugin %s host %s cannot contribute keybindings", pluginID, host.Placement)
	}
	if host.Runner.Type == "" {
		return fmt.Errorf("plugin %s host %s runner type is required", pluginID, host.Placement)
	}
	if !validRunnerType(host.Runner.Type) {
		return fmt.Errorf("plugin %s host %s runner type is invalid", pluginID, host.Placement)
	}
	if host.Runner.Type == RunnerBuiltin && !trustedTermX {
		return fmt.Errorf("plugin %s host %s cannot self-declare builtin runner", pluginID, host.Placement)
	}
	if host.Runner.Type == RunnerStdioJSON && len(host.Runner.Command) == 0 {
		return fmt.Errorf("plugin %s host %s stdio_json command is required", pluginID, host.Placement)
	}
	for _, action := range host.Contributions.Actions {
		if err := validateActionContribution(pluginID, action, trustedTermX); err != nil {
			return err
		}
		if !actionScopeCompatibleWithHost(action.Scope, host.Placement) {
			return fmt.Errorf("plugin %s host %s cannot contribute %s action %s", pluginID, host.Placement, action.Scope, action.ID)
		}
		if len(action.SupportedClientKinds) > 0 && !validClientKinds(action.SupportedClientKinds) {
			return fmt.Errorf("plugin %s action %s client kinds are invalid", pluginID, action.ID)
		}
		if host.Placement == HostClient && len(action.SupportedClientKinds) > 0 && !clientKindsWithin(action.SupportedClientKinds, host.ClientKinds) {
			return fmt.Errorf("plugin %s action %s client kinds exceed host client kinds", pluginID, action.ID)
		}
	}
	for _, keybinding := range host.Contributions.Keybindings {
		if len(keybinding.ClientKinds) > 0 && !validClientKinds(keybinding.ClientKinds) {
			return fmt.Errorf("plugin %s keybinding %s client kinds are invalid", pluginID, keybinding.ActionID)
		}
		if len(keybinding.ClientKinds) > 0 && !clientKindsWithin(keybinding.ClientKinds, host.ClientKinds) {
			return fmt.Errorf("plugin %s keybinding %s client kinds exceed host client kinds", pluginID, keybinding.ActionID)
		}
	}
	for _, hook := range host.Contributions.Hooks {
		if hook.EventType == "" {
			return fmt.Errorf("plugin %s hook event type is required", pluginID)
		}
		if !strings.HasPrefix(string(hook.EventType), "termx.") {
			return fmt.Errorf("plugin %s hook %s must subscribe system event catalog", pluginID, hook.EventType)
		}
		if hook.Delivery.Mode != "" && !validDeliveryMode(hook.Delivery.Mode) {
			return fmt.Errorf("plugin %s hook %s delivery mode is invalid", pluginID, hook.EventType)
		}
	}
	return nil
}

func validateActionContribution(pluginID PluginID, action ActionContribution, trustedTermX bool) error {
	if action.ID == "" {
		return fmt.Errorf("plugin %s action id is required", pluginID)
	}
	if !validActionScope(action.Scope) {
		return fmt.Errorf("action %s scope is invalid", action.ID)
	}
	if !validDangerLevel(action.Danger) {
		return fmt.Errorf("action %s danger level is invalid", action.ID)
	}
	if action.Danger == DangerDestructive && len(requiredActionCaps(action)) == 0 {
		return fmt.Errorf("destructive action %s must require explicit capability", action.ID)
	}
	if strings.HasPrefix(string(action.ID), "termx.") {
		if !trustedTermX {
			return fmt.Errorf("plugin %s cannot contribute termx namespace action %s", pluginID, action.ID)
		}
		return nil
	}
	prefix := string(pluginID) + "."
	if !strings.HasPrefix(string(action.ID), prefix) {
		return fmt.Errorf("plugin %s action %s must be under plugin namespace", pluginID, action.ID)
	}
	return nil
}

func validatePluginID(id PluginID) error {
	if id == "" {
		return fmt.Errorf("plugin id is required")
	}
	parts := strings.Split(string(id), ".")
	if len(parts) < 2 {
		return fmt.Errorf("plugin id %s must include publisher namespace", id)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("plugin id %s contains empty namespace part", id)
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				continue
			}
			return fmt.Errorf("plugin id %s contains invalid character", id)
		}
	}
	return nil
}

func validHostPlacement(host HostPlacement) bool {
	return host == HostDaemon || host == HostClient || host == HostWorkspace || host == HostOneShot
}

func validEventSourceHost(host HostPlacement) bool {
	return host == HostDaemon || host == HostClient || host == HostWorkspace
}

func validRunnerType(runner RunnerType) bool {
	return runner == RunnerBuiltin || runner == RunnerStdioJSON
}

func validActionScope(scope ActionScope) bool {
	return scope == ActionScopeClient || scope == ActionScopeDaemon || scope == ActionScopeWorkspace
}

func validDangerLevel(danger DangerLevel) bool {
	return danger == DangerNone || danger == DangerDestructive
}

func validDeliveryMode(mode HookDeliveryMode) bool {
	return mode == DeliveryStrictQueued || mode == DeliveryQueued || mode == DeliveryLatest || mode == DeliveryCoalesced
}

func validClientKinds(kinds []ClientKind) bool {
	for _, kind := range kinds {
		if kind != ClientKindTUI && kind != ClientKindApp && kind != ClientKindWeb && kind != ClientKindGUI {
			return false
		}
	}
	return true
}

func clientKindsWithin(candidates []ClientKind, allowed []ClientKind) bool {
	allowedSet := make(map[ClientKind]struct{}, len(allowed))
	for _, kind := range allowed {
		allowedSet[kind] = struct{}{}
	}
	for _, kind := range candidates {
		if _, ok := allowedSet[kind]; !ok {
			return false
		}
	}
	return true
}

func actionScopeCompatibleWithHost(scope ActionScope, host HostPlacement) bool {
	switch host {
	case HostClient:
		return scope == ActionScopeClient
	case HostDaemon:
		return scope == ActionScopeDaemon
	case HostWorkspace:
		return scope == ActionScopeWorkspace
	case HostOneShot:
		return true
	default:
		return false
	}
}

func eventTypeMatchesSourceHost(eventType EventType, host HostPlacement) bool {
	switch {
	case strings.HasPrefix(string(eventType), "termx.client."):
		return host == HostClient
	case strings.HasPrefix(string(eventType), "termx.daemon."):
		return host == HostDaemon
	case strings.HasPrefix(string(eventType), "termx.workspace."):
		return host == HostWorkspace
	default:
		return false
	}
}

func trustedPluginSet(ids []PluginID) map[PluginID]bool {
	out := make(map[PluginID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func grantsForPlugin(pluginID PluginID, grants []CapabilityGrant) []CapabilityGrant {
	var out []CapabilityGrant
	for _, grant := range grants {
		if grant.PluginID == pluginID {
			out = append(out, grant)
		}
	}
	return out
}

func trustedGrantForHost(grants []CapabilityGrant, host HostPlacement) bool {
	for _, grant := range grants {
		if grant.Host != "" && grant.Host != host {
			continue
		}
		if grant.Trusted {
			return true
		}
	}
	return false
}

func grantedCapsForHost(grants []CapabilityGrant, host HostPlacement) map[Capability]struct{} {
	out := make(map[Capability]struct{})
	for _, grant := range grants {
		if grant.Host != "" && grant.Host != host {
			continue
		}
		for _, cap := range grant.Capabilities {
			out[cap] = struct{}{}
		}
	}
	return out
}

func capabilitySet(caps []Capability) map[Capability]struct{} {
	out := make(map[Capability]struct{}, len(caps))
	for _, cap := range caps {
		out[cap] = struct{}{}
	}
	return out
}

func requiredActionCaps(action ActionContribution) []Capability {
	out := cloneCaps(action.RequiredCaps)
	out = append(out, action.ClientRequiredCaps...)
	out = append(out, action.DaemonRequiredCaps...)
	return uniqueCaps(out)
}

func allCapsDeclared(declared map[Capability]struct{}, required []Capability) bool {
	for _, cap := range uniqueCaps(required) {
		if _, ok := declared[cap]; !ok {
			return false
		}
	}
	return true
}

func allCapsGranted(granted map[Capability]struct{}, required []Capability) bool {
	for _, cap := range uniqueCaps(required) {
		if _, ok := granted[cap]; !ok {
			return false
		}
	}
	return true
}

func uniqueCaps(caps []Capability) []Capability {
	seen := make(map[Capability]struct{}, len(caps))
	var out []Capability
	for _, cap := range caps {
		if cap == "" {
			continue
		}
		if _, ok := seen[cap]; ok {
			continue
		}
		seen[cap] = struct{}{}
		out = append(out, cap)
	}
	return out
}

func cloneCaps(caps []Capability) []Capability {
	return append([]Capability(nil), caps...)
}

func cloneEventSpec(event EventSpec) EventSpec {
	out := event
	out.RequiredCaps = cloneCaps(event.RequiredCaps)
	return out
}

func cloneHookScope(scope HookScope) HookScope {
	out := scope
	if scope.TerminalRef != nil {
		ref := *scope.TerminalRef
		out.TerminalRef = &ref
	}
	return out
}

func cloneHookFilters(filters []HookFilter) []HookFilter {
	return append([]HookFilter(nil), filters...)
}

func hookScopeEmpty(scope HookScope) bool {
	return scope.WorkspaceID == "" &&
		scope.ClientSessionID == "" &&
		scope.EndpointID == "" &&
		scope.TerminalRef == nil &&
		scope.DaemonID == "" &&
		scope.DaemonTerminalID == ""
}

func resolveHookDelivery(event EventSpec, requested HookDelivery) (HookDelivery, error) {
	if event.DefaultDelivery.Mode == "" {
		return HookDelivery{}, fmt.Errorf("event %s default delivery mode is required", event.Type)
	}
	if !validDeliveryMode(event.DefaultDelivery.Mode) {
		return HookDelivery{}, fmt.Errorf("event %s default delivery mode is invalid", event.Type)
	}
	if event.Lossy && !event.DefaultDelivery.Mode.Lossy() {
		return HookDelivery{}, fmt.Errorf("event %s lossy flag requires lossy default delivery", event.Type)
	}
	if requested.Mode == "" {
		return event.DefaultDelivery, nil
	}
	if !validDeliveryMode(requested.Mode) {
		return HookDelivery{}, fmt.Errorf("hook %s delivery mode is invalid", event.Type)
	}
	if requested.Mode != event.DefaultDelivery.Mode {
		return HookDelivery{}, fmt.Errorf("hook %s delivery mode cannot override event policy", event.Type)
	}
	if event.DefaultDelivery.QueueLimit > 0 && requested.QueueLimit > event.DefaultDelivery.QueueLimit {
		return HookDelivery{}, fmt.Errorf("hook %s queue limit exceeds event policy", event.Type)
	}
	if event.DefaultDelivery.Throttle > 0 && requested.Throttle < event.DefaultDelivery.Throttle {
		return HookDelivery{}, fmt.Errorf("hook %s throttle is below event policy", event.Type)
	}
	if event.DefaultDelivery.Debounce > 0 && requested.Debounce < event.DefaultDelivery.Debounce {
		return HookDelivery{}, fmt.Errorf("hook %s debounce is below event policy", event.Type)
	}
	resolved := event.DefaultDelivery
	if requested.QueueLimit > 0 {
		resolved.QueueLimit = requested.QueueLimit
	}
	if requested.Throttle > 0 {
		resolved.Throttle = requested.Throttle
	}
	if requested.Debounce > 0 {
		resolved.Debounce = requested.Debounce
	}
	return resolved, nil
}
