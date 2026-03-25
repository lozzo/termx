package app

import (
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
	Screen      Screen
	Overlay     OverlayStack
	FocusTarget FocusTarget
	Workspace   *workspace.WorkspaceState
	Terminals   map[types.TerminalID]terminal.Metadata
	Sessions    map[types.TerminalID]TerminalSession
}

type TerminalSession struct {
	TerminalID types.TerminalID
	Channel    uint16
	Attached   bool
	Snapshot   *protocol.Snapshot
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

func (m Model) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) View() string {
	if viewRenderer != nil {
		return viewRenderer(m, 120, 20)
	}
	return ""
}
