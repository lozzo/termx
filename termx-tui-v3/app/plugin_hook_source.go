package app

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-shared/plugin"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// ClientHookObjectKind 描述 client-owned UI hook 的对象类型。
// 它只覆盖当前 client session 的 reducer-owned UI 投影，不表达 daemon terminal lifecycle。
type ClientHookObjectKind string

const (
	ClientHookObjectPanel ClientHookObjectKind = plugin.ObjectKindPanel
	ClientHookObjectFloat ClientHookObjectKind = plugin.ObjectKindFloat
	ClientHookObjectTab   ClientHookObjectKind = plugin.ObjectKindTab
)

// ClientHookVerb 描述 client UI after-event 的动作语义。
// 这些动词只能在 reducer mutation 成功后使用，不能作为 before/veto hook。
type ClientHookVerb string

const (
	ClientHookVerbCreated   ClientHookVerb = "created"
	ClientHookVerbClosed    ClientHookVerb = "closed"
	ClientHookVerbBound     ClientHookVerb = "bound"
	ClientHookVerbResized   ClientHookVerb = "resized"
	ClientHookVerbFocused   ClientHookVerb = "focused"
	ClientHookVerbActivated ClientHookVerb = "activated"
)

// ClientHookSourceConfig 描述一个 TUI/App/Web/GUI client session 的 hook source 配置。
// SourceSession 是 daemon mailbox 看到的 client session id；terminal lifecycle 仍由 owning daemon 发布。
type ClientHookSourceConfig struct {
	SourceSession string
	ClientKind    plugin.ClientKind
	WorkspaceID   string
	Now           func() time.Time
}

// ClientHookSource 把 client reducer after-event 封装成 plugin.HookEvent。
// 它只发布 panel/float/tab 等 client-owned UI 事实，不解释 terminal running/exited，也不执行插件 handler。
type ClientHookSource struct {
	mu            sync.Mutex
	sourceSession string
	clientKind    plugin.ClientKind
	workspaceID   string
	now           func() time.Time
	sequence      uint64
}

// NewClientHookSource 创建 client session hook source adapter。
// 调用方应按 client session 生命周期持有它，确保同一 session 的 Sequence 单调递增。
func NewClientHookSource(config ClientHookSourceConfig) *ClientHookSource {
	sessionID := config.SourceSession
	if sessionID == "" {
		sessionID = "client"
	}
	clientKind := config.ClientKind
	if clientKind == "" {
		clientKind = plugin.ClientKindTUI
	}
	workspaceID := config.WorkspaceID
	if workspaceID == "" {
		workspaceID = state.DefaultWorkspaceID
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ClientHookSource{
		sourceSession: sessionID,
		clientKind:    clientKind,
		workspaceID:   workspaceID,
		now:           now,
	}
}

// ClientHookRect 是 floating/panel geometry hook payload 中的 cell 矩形。
// 该值来自 client render/reducer 投影，只用于 UI 元数据，不参与 terminal resize truth。
type ClientHookRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// ClientHookAfterEvent 是 client reducer mutation 成功后的 hook 输入。
// 它是 source adapter 的边界对象：调用方必须先完成 reducer state mutation，再把 after-event 转成 hook envelope。
type ClientHookAfterEvent struct {
	ObjectKind  ClientHookObjectKind
	Verb        ClientHookVerb
	ObjectID    string
	WorkspaceID string
	TabID       string
	TerminalRef *state.TerminalRef
	Rect        *ClientHookRect
	Action      string
	Source      string
	Time        time.Time
	Lossy       bool
	Trace       plugin.MessageTrace
}

// ClientHookAfterEventEffect 把 reducer 成功 mutation 后的 UI 事实交给插件 hook host。
// reducer 只发布 after-event，不执行插件、不直接生成持久订阅，也不允许 hook handler 反向修改 state。
type ClientHookAfterEventEffect struct {
	After ClientHookAfterEvent
}

func (ClientHookAfterEventEffect) isEffect() {}

// ClientHookPayload 是 client UI hook 的结构化 payload。
// 它只携带 client-owned UI 元数据和可选 TerminalRef，不携带 daemon lifecycle 判断。
type ClientHookPayload struct {
	WorkspaceID string            `json:"workspace_id,omitempty"`
	TabID       string            `json:"tab_id,omitempty"`
	ObjectKind  string            `json:"object_kind"`
	ObjectID    string            `json:"object_id"`
	Verb        string            `json:"verb"`
	EndpointID  plugin.EndpointID `json:"endpoint_id,omitempty"`
	TerminalID  plugin.TerminalID `json:"terminal_id,omitempty"`
	Rect        *ClientHookRect   `json:"rect,omitempty"`
	Action      string            `json:"action,omitempty"`
	Source      string            `json:"source,omitempty"`
}

// AfterEvent 把 client UI after-event 转换成系统 hook envelope。
// 只有预定义的 panel/float/tab event 会成功；daemon-local identity 字段始终留空。
func (source *ClientHookSource) AfterEvent(after ClientHookAfterEvent) (plugin.HookEvent, bool) {
	eventType, ok := clientHookEventType(after.ObjectKind, after.Verb)
	if !ok || after.ObjectID == "" {
		return plugin.HookEvent{}, false
	}
	workspaceID := after.WorkspaceID
	if workspaceID == "" {
		workspaceID = source.workspaceID
	}
	at := after.Time
	if at.IsZero() {
		at = source.nowTime()
	}
	ref, hasRef := clientHookTerminalRef(after.TerminalRef)
	payload := ClientHookPayload{
		WorkspaceID: workspaceID,
		TabID:       after.TabID,
		ObjectKind:  string(after.ObjectKind),
		ObjectID:    after.ObjectID,
		Verb:        string(after.Verb),
		Rect:        cloneClientHookRect(after.Rect),
		Action:      after.Action,
		Source:      after.Source,
	}
	if hasRef {
		payload.EndpointID = ref.EndpointID
		payload.TerminalID = ref.TerminalID
	}
	seq := source.nextSequence()
	at = at.UTC()
	eventID := fmt.Sprintf("client:%s:%d", source.sourceSession, seq)
	hook := plugin.HookEvent{
		EventID:       eventID,
		Type:          eventType,
		SourceHost:    plugin.HostClient,
		SourceSession: source.sourceSession,
		ClientKind:    source.clientKind,
		WorkspaceID:   workspaceID,
		ObjectKind:    string(after.ObjectKind),
		ObjectID:      after.ObjectID,
		Sequence:      seq,
		Time:          at,
		Trace:         hookEventTrace(eventID, after.Trace),
		Payload:       mustClientHookJSON(payload),
		Lossy:         after.Lossy || clientHookDefaultLossy(after.Verb),
	}
	if hasRef {
		hook.EndpointID = ref.EndpointID
		refCopy := ref
		hook.TerminalRef = &refCopy
	}
	return hook, true
}

// AfterPaneCommand 从成功的 pane reducer command result 生成 panel hook。
// result 必须已经是 OK；CloseAndKill 只发布 panel.closed，terminal kill 事实仍由 daemon terminal hook 发布。
func (source *ClientHookSource) AfterPaneCommand(command state.PaneCommand, result state.PaneCommandResult, terminalRef *state.TerminalRef) (plugin.HookEvent, bool) {
	after, ok := clientHookAfterEventFromPaneCommand(command, result, terminalRef)
	if !ok {
		return plugin.HookEvent{}, false
	}
	return source.AfterEvent(after)
}

// AfterFloatingCommand 从成功的 floating reducer command result 生成 floating hook。
// 它只发布 created/closed/focused/resized 这组第一阶段事件，不把移动或折叠解释成 terminal 生命周期。
func (source *ClientHookSource) AfterFloatingCommand(command state.FloatingCommand, result state.FloatingCommandResult, terminalRef *state.TerminalRef) (plugin.HookEvent, bool) {
	after, ok := clientHookAfterEventFromFloatingCommand(command, result, terminalRef)
	if !ok {
		return plugin.HookEvent{}, false
	}
	return source.AfterEvent(after)
}

// AfterWorkbenchCommand 从成功的 workbench command result 生成 tab hook。
// 第一阶段只覆盖 tab created/activated；workspace 切换和 tab close 可在后续事件目录扩展。
func (source *ClientHookSource) AfterWorkbenchCommand(command state.WorkbenchCommand, result state.WorkbenchCommandResult) (plugin.HookEvent, bool) {
	after, ok := clientHookAfterEventFromWorkbenchCommand(command, result)
	if !ok {
		return plugin.HookEvent{}, false
	}
	return source.AfterEvent(after)
}

func clientHookAfterEventFromPaneCommand(command state.PaneCommand, result state.PaneCommandResult, terminalRef *state.TerminalRef) (ClientHookAfterEvent, bool) {
	if result.Status != state.PaneCommandOK {
		return ClientHookAfterEvent{}, false
	}
	after := ClientHookAfterEvent{
		ObjectKind:  ClientHookObjectPanel,
		WorkspaceID: command.Target.WorkspaceID,
		TabID:       command.Target.TabID,
		TerminalRef: terminalRef,
		Action:      string(command.Action),
		Source:      string(command.Source),
	}
	switch command.Action {
	case state.PaneCommandSplit:
		after.Verb = ClientHookVerbCreated
		after.ObjectID = command.NewPane.ID
	case state.PaneCommandClose, state.PaneCommandCloseAndKill:
		after.Verb = ClientHookVerbClosed
		after.ObjectID = command.Target.PaneID
	case state.PaneCommandFocus:
		after.Verb = ClientHookVerbFocused
		after.ObjectID = command.Target.PaneID
	case state.PaneCommandResize, state.PaneCommandSetSize:
		after.Verb = ClientHookVerbResized
		after.ObjectID = command.Target.PaneID
	default:
		return ClientHookAfterEvent{}, false
	}
	return after, true
}

func clientHookAfterEventFromFloatingCommand(command state.FloatingCommand, result state.FloatingCommandResult, terminalRef *state.TerminalRef) (ClientHookAfterEvent, bool) {
	if result.Status != state.FloatingCommandOK {
		return ClientHookAfterEvent{}, false
	}
	objectID := result.ID
	if objectID == "" {
		objectID = command.TargetID
	}
	after := ClientHookAfterEvent{
		ObjectKind:  ClientHookObjectFloat,
		ObjectID:    objectID,
		TerminalRef: terminalRef,
		Action:      string(command.Action),
		Source:      string(command.Source),
	}
	if command.Rect.W > 0 || command.Rect.H > 0 {
		after.Rect = &ClientHookRect{X: command.Rect.X, Y: command.Rect.Y, W: command.Rect.W, H: command.Rect.H}
	}
	switch command.Action {
	case state.FloatingCommandCreate:
		after.Verb = ClientHookVerbCreated
	case state.FloatingCommandClose:
		after.Verb = ClientHookVerbClosed
	case state.FloatingCommandFocusRaise, state.FloatingCommandSummon:
		after.Verb = ClientHookVerbFocused
	case state.FloatingCommandResize:
		after.Verb = ClientHookVerbResized
	default:
		return ClientHookAfterEvent{}, false
	}
	return after, true
}

func clientHookAfterEventFromWorkbenchCommand(command state.WorkbenchCommand, result state.WorkbenchCommandResult) (ClientHookAfterEvent, bool) {
	if result.Status != state.WorkbenchCommandOK {
		return ClientHookAfterEvent{}, false
	}
	after := ClientHookAfterEvent{
		ObjectKind:  ClientHookObjectTab,
		ObjectID:    result.ID,
		WorkspaceID: command.Target.WorkspaceID,
		Action:      string(command.Action),
		Source:      string(command.Source),
	}
	if after.ObjectID == "" {
		after.ObjectID = command.TargetID
	}
	switch command.Action {
	case state.WorkbenchCommandTabCreate:
		after.Verb = ClientHookVerbCreated
	case state.WorkbenchCommandTabSwitch, state.WorkbenchCommandTabNext, state.WorkbenchCommandTabPrevious:
		after.Verb = ClientHookVerbActivated
	default:
		return ClientHookAfterEvent{}, false
	}
	return after, true
}

func (source *ClientHookSource) nextSequence() uint64 {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.sequence++
	return source.sequence
}

func (source *ClientHookSource) nowTime() time.Time {
	return source.now().UTC()
}

func clientHookEventType(kind ClientHookObjectKind, verb ClientHookVerb) (plugin.EventType, bool) {
	switch kind {
	case ClientHookObjectPanel:
		switch verb {
		case ClientHookVerbCreated:
			return plugin.SystemEventClientPanelCreated, true
		case ClientHookVerbClosed:
			return plugin.SystemEventClientPanelClosed, true
		case ClientHookVerbBound:
			return plugin.SystemEventClientPanelBound, true
		case ClientHookVerbResized:
			return plugin.SystemEventClientPanelResized, true
		case ClientHookVerbFocused:
			return plugin.SystemEventClientPanelFocused, true
		}
	case ClientHookObjectFloat:
		switch verb {
		case ClientHookVerbCreated:
			return plugin.SystemEventClientFloatCreated, true
		case ClientHookVerbClosed:
			return plugin.SystemEventClientFloatClosed, true
		case ClientHookVerbResized:
			return plugin.SystemEventClientFloatResized, true
		case ClientHookVerbFocused:
			return plugin.SystemEventClientFloatFocused, true
		}
	case ClientHookObjectTab:
		switch verb {
		case ClientHookVerbCreated:
			return plugin.SystemEventClientTabCreated, true
		case ClientHookVerbActivated:
			return plugin.SystemEventClientTabActivated, true
		}
	}
	return "", false
}

func clientHookDefaultLossy(verb ClientHookVerb) bool {
	return verb == ClientHookVerbResized || verb == ClientHookVerbFocused
}

func clientHookTerminalRef(ref *state.TerminalRef) (plugin.TerminalRef, bool) {
	if ref == nil {
		return plugin.TerminalRef{}, false
	}
	normalized := ref.Normalize()
	if normalized.Empty() {
		return plugin.TerminalRef{}, false
	}
	return plugin.TerminalRef{
		EndpointID: plugin.EndpointID(normalized.EndpointID),
		TerminalID: plugin.TerminalID(normalized.TerminalID),
	}, true
}

func clientHookAfterEventEffect(root state.Root, after ClientHookAfterEvent) []Effect {
	if !root.ClientHooks.Enabled {
		return nil
	}
	after = completeClientHookAfterEvent(root, after)
	if _, ok := clientHookEventType(after.ObjectKind, after.Verb); !ok || after.ObjectID == "" {
		return nil
	}
	return []Effect{ClientHookAfterEventEffect{After: after}}
}

func completeClientHookAfterEvent(root state.Root, after ClientHookAfterEvent) ClientHookAfterEvent {
	shell := root.Shell.EnsureDefaults()
	if after.WorkspaceID == "" {
		after.WorkspaceID = shell.Workspace.ID
	}
	if after.TabID == "" {
		after.TabID = shell.Workspace.ActiveTabID
	}
	if !after.Lossy {
		after.Lossy = clientHookDefaultLossy(after.Verb)
	}
	return after
}

func clientHookPaneCommandEffects(previous state.Root, next state.Root, command state.PaneCommand, result state.PaneCommandResult) []Effect {
	refRoot := next
	if command.Action == state.PaneCommandClose || command.Action == state.PaneCommandCloseAndKill {
		refRoot = previous
	}
	ref := clientHookTerminalRefForPane(refRoot, clientHookPaneObjectID(command))
	after, ok := clientHookAfterEventFromPaneCommand(command, result, ref)
	if !ok {
		return nil
	}
	return clientHookAfterEventEffect(next, after)
}

func clientHookFloatingCommandEffects(previous state.Root, next state.Root, command state.FloatingCommand, result state.FloatingCommandResult) []Effect {
	refRoot := next
	if command.Action == state.FloatingCommandClose {
		refRoot = previous
	}
	floatingID := result.ID
	if floatingID == "" {
		floatingID = command.TargetID
	}
	ref := clientHookTerminalRefForFloating(refRoot, floatingID)
	after, ok := clientHookAfterEventFromFloatingCommand(command, result, ref)
	if !ok {
		return nil
	}
	return clientHookAfterEventEffect(next, after)
}

func clientHookWorkbenchCommandEffects(next state.Root, command state.WorkbenchCommand, result state.WorkbenchCommandResult) []Effect {
	after, ok := clientHookAfterEventFromWorkbenchCommand(command, result)
	if !ok {
		return nil
	}
	return clientHookAfterEventEffect(next, after)
}

func clientHookPanelBoundEffect(root state.Root, paneID string, ref state.TerminalRef, action string) []Effect {
	if paneID == "" || ref.Empty() {
		return nil
	}
	ref = ref.Normalize()
	after := ClientHookAfterEvent{
		ObjectKind:  ClientHookObjectPanel,
		Verb:        ClientHookVerbBound,
		ObjectID:    paneID,
		TerminalRef: &ref,
		Action:      action,
	}
	return clientHookAfterEventEffect(root, after)
}

func clientHookPaneObjectID(command state.PaneCommand) string {
	if command.Action == state.PaneCommandSplit {
		return command.NewPane.ID
	}
	return command.Target.PaneID
}

func clientHookTerminalRefForPane(root state.Root, paneID string) *state.TerminalRef {
	if paneID == "" {
		return nil
	}
	if binding, ok := root.TerminalViews.PaneBinding(paneID); ok && binding.TerminalID != "" {
		ref := binding.TerminalRef().Normalize()
		return &ref
	}
	return nil
}

func clientHookTerminalRefForFloating(root state.Root, floatingID string) *state.TerminalRef {
	if floatingID == "" {
		return nil
	}
	if binding, ok := root.TerminalViews.FloatingBinding(floatingID); ok && binding.TerminalID != "" {
		ref := binding.TerminalRef().Normalize()
		return &ref
	}
	return nil
}

func hookEventTrace(eventID string, cause plugin.MessageTrace) plugin.MessageTrace {
	if cause.TraceID == "" {
		return plugin.MessageTrace{TraceID: eventID}
	}
	return cause.Clone()
}

func cloneClientHookRect(rect *ClientHookRect) *ClientHookRect {
	if rect == nil {
		return nil
	}
	out := *rect
	return &out
}

func mustClientHookJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return json.RawMessage(payload)
}
