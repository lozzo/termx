package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

// Model 只承载根应用壳层状态。
// Screen 表示页面级互斥路由，Overlay 表示可跨页面复用的临时浮层栈，
// 两者拆开建模，后续页面切换就不需要把“弹层是否打开”硬塞进 screen 枚举里。
type Model struct {
	Screen         Screen
	Overlay        OverlayStack
	FocusTarget    FocusTarget
	Pool           TerminalPoolState
	Workspace      *workspace.WorkspaceState
	Terminals      map[types.TerminalID]terminal.Metadata
	Sessions       map[types.TerminalID]TerminalSession
	Notice         *NoticeState
	PendingEffects []Effect
	IntentExecutor IntentExecutor
}

type TerminalSession struct {
	TerminalID types.TerminalID
	Channel    uint16
	Attached   bool
	ReadOnly   bool
	Preview    bool
	Snapshot   *protocol.Snapshot
}

// TerminalPoolState 保存独立 Terminal Pool 页面的一期状态。
// 这里不把 preview 当成 workbench 焦点的一部分，避免只读观察抢走日常输入焦点。
type TerminalPoolState struct {
	Query                       string
	SelectedTerminalID          types.TerminalID
	PreviewTerminalID           types.TerminalID
	PreviewReadonly             bool
	PreviewSubscriptionRevision int
}

type IntentMessage struct {
	Intent Intent
}

type IntentExecutor interface {
	ExecuteIntent(context.Context, Model, Intent) (Model, error)
}

var viewRenderer func(Model, int, int) string

func SetViewRenderer(renderer func(Model, int, int) string) {
	viewRenderer = renderer
}

func NewModel() Model {
	return Model{
		Screen:      ScreenWorkbench,
		Overlay:     EmptyOverlayStack(),
		FocusTarget: FocusWorkbench,
		Terminals:   make(map[types.TerminalID]terminal.Metadata),
		Sessions:    make(map[types.TerminalID]TerminalSession),
	}
}

// SwitchScreen 只处理顶层页面路由切换。
// 焦点目标跟随顶层页面变化，避免根壳层把页面焦点规则散落到调用方手里。
func (m Model) SwitchScreen(screen Screen) Model {
	m.Screen = normalizeScreen(screen)
	m.FocusTarget = defaultFocusTarget(m.Screen)
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case IntentMessage:
		if m.IntentExecutor != nil {
			next, _ := m.IntentExecutor.ExecuteIntent(context.Background(), m, typed.Intent)
			return next, nil
		}
		return m.Apply(typed.Intent), nil
	default:
		return m, nil
	}
}

func (m Model) View() string {
	if viewRenderer != nil {
		return viewRenderer(m, 120, 20)
	}
	return ""
}

type NoticeState struct {
	Message string
}

func (m Model) clone() Model {
	next := m
	next.Overlay = m.Overlay.Clone()
	next.Workspace = m.Workspace.Clone()
	next.Terminals = cloneTerminalMap(m.Terminals)
	next.Sessions = cloneSessionMap(m.Sessions)
	next.PendingEffects = append([]Effect(nil), m.PendingEffects...)
	if m.Notice != nil {
		notice := *m.Notice
		next.Notice = &notice
	}
	return next
}

func (m *Model) ensureWorkspace() {
	if m.Workspace == nil {
		m.Workspace = workspace.NewTemporary("main")
	}
	if m.Terminals == nil {
		m.Terminals = make(map[types.TerminalID]terminal.Metadata)
	}
	if m.Sessions == nil {
		m.Sessions = make(map[types.TerminalID]TerminalSession)
	}
}

func cloneTerminalMap(input map[types.TerminalID]terminal.Metadata) map[types.TerminalID]terminal.Metadata {
	if len(input) == 0 {
		return make(map[types.TerminalID]terminal.Metadata)
	}
	out := make(map[types.TerminalID]terminal.Metadata, len(input))
	for key, meta := range input {
		out[key] = meta.Clone()
	}
	return out
}

func cloneSessionMap(input map[types.TerminalID]TerminalSession) map[types.TerminalID]TerminalSession {
	if len(input) == 0 {
		return make(map[types.TerminalID]TerminalSession)
	}
	out := make(map[types.TerminalID]TerminalSession, len(input))
	for key, session := range input {
		out[key] = session
	}
	return out
}

type Effect interface {
	effectName() string
}

// CreateTerminalEffect 只描述“要创建并绑定一个 terminal”，不假装 runtime 已经成功。
type CreateTerminalEffect struct {
	PaneID  types.PaneID
	Command []string
	Name    string
	Size    protocol.Size
}

func (CreateTerminalEffect) effectName() string { return "create_terminal" }

// KillTerminalEffect 只描述 kill 请求；真正的 exited 状态要等 runtime 执行成功后再回填。
type KillTerminalEffect struct {
	TerminalID types.TerminalID
}

func (KillTerminalEffect) effectName() string { return "kill_terminal" }

// RefreshPreviewEffect 请求 runtime 以只读模式重新订阅 preview terminal。
type RefreshPreviewEffect struct {
	TerminalID types.TerminalID
}

func (RefreshPreviewEffect) effectName() string { return "refresh_preview" }

type UpdateTerminalMetadataEffect struct {
	TerminalID types.TerminalID
	Name       string
	Tags       map[string]string
}

func (UpdateTerminalMetadataEffect) effectName() string { return "update_terminal_metadata" }

type RemoveTerminalEffect struct {
	TerminalID types.TerminalID
	Visible    bool
	Name       string
}

func (RemoveTerminalEffect) effectName() string { return "remove_terminal" }
