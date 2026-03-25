package app

import (
	"context"
	"strings"

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
	Screen            Screen
	Overlay           OverlayStack
	FocusTarget       FocusTarget
	Pool              TerminalPoolState
	Workspace         *workspace.WorkspaceState
	Terminals         map[types.TerminalID]terminal.Metadata
	Sessions          map[types.TerminalID]TerminalSession
	Notice            *NoticeState
	PendingEffects    []Effect
	PreviewStreamNext func() tea.Cmd
	IntentExecutor    IntentExecutor
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
	SearchInputActive           bool
}

type PreviewStreamMessage struct {
	TerminalID types.TerminalID
	Revision   int
	Frame      protocol.StreamFrame
}

type PreviewStreamClosedMessage struct {
	TerminalID types.TerminalID
	Revision   int
}

type IntentMessage struct {
	Intent Intent
}

type IntentExecutor interface {
	ExecuteIntent(context.Context, Model, Intent) (Model, tea.Cmd, error)
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
			next, cmd, _ := m.IntentExecutor.ExecuteIntent(context.Background(), m, typed.Intent)
			return next, cmd
		}
		return m.Apply(typed.Intent), nil
	case tea.KeyMsg:
		if intent, handled := m.keyIntent(typed); handled {
			if m.IntentExecutor != nil {
				next, cmd, _ := m.IntentExecutor.ExecuteIntent(context.Background(), m, intent)
				return next, cmd
			}
			return m.Apply(intent), nil
		}
		return m, nil
	case PreviewStreamMessage:
		next := m.applyPreviewStreamMessage(typed)
		if next.PreviewStreamNext != nil {
			return next, next.PreviewStreamNext()
		}
		return next, nil
	case PreviewStreamClosedMessage:
		next := m.clone()
		if next.Pool.PreviewTerminalID == typed.TerminalID && next.Pool.PreviewSubscriptionRevision == typed.Revision {
			next.PreviewStreamNext = nil
		}
		return next, nil
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
	next.PreviewStreamNext = m.PreviewStreamNext
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

func (m Model) keyIntent(msg tea.KeyMsg) (Intent, bool) {
	switch {
	case msg.Type == tea.KeyEsc && m.Overlay.HasActive():
		return CancelOverlayIntent{}, true
	case m.Screen == ScreenTerminalPool:
		return m.poolKeyIntent(msg)
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'p' && m.Screen == ScreenWorkbench && !m.Overlay.HasActive():
		return OpenTerminalPoolIntent{}, true
	default:
		return nil, false
	}
}

// poolKeyIntent 把 Terminal Pool 页内真实键盘路径统一翻译成 intent。
// 这里保持“按键 -> intent -> reducer/runtime”单一路径，避免页面动作出现测试能调、生产键盘调不到的分叉。
func (m Model) poolKeyIntent(msg tea.KeyMsg) (Intent, bool) {
	if m.Pool.SearchInputActive {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyEnter:
			return SetTerminalPoolSearchInputIntent{Active: false}, true
		case tea.KeyBackspace:
			return SearchTerminalPoolIntent{Query: truncateLastRune(m.Pool.Query)}, true
		case tea.KeyRunes:
			return SearchTerminalPoolIntent{Query: m.Pool.Query + string(msg.Runes)}, true
		default:
			return nil, false
		}
	}

	switch msg.Type {
	case tea.KeyEsc:
		return CloseTerminalPoolIntent{}, true
	case tea.KeyUp:
		return MoveTerminalPoolSelectionIntent{Delta: -1}, true
	case tea.KeyDown:
		return MoveTerminalPoolSelectionIntent{Delta: 1}, true
	case tea.KeyEnter:
		return OpenSelectedTerminalHereIntent{}, true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return nil, false
		}
		switch msg.Runes[0] {
		case '/':
			return SetTerminalPoolSearchInputIntent{Active: true}, true
		case 't':
			return OpenSelectedTerminalInNewTabIntent{}, true
		case 'o':
			return OpenSelectedTerminalInFloatingIntent{}, true
		case 'e':
			return OpenTerminalMetadataEditorIntent{}, true
		case 'k':
			return KillSelectedTerminalIntent{}, true
		case 'd':
			return RemoveSelectedTerminalIntent{}, true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func (m Model) applyPreviewStreamMessage(msg PreviewStreamMessage) Model {
	next := m.clone()
	if next.Pool.PreviewTerminalID != msg.TerminalID || next.Pool.PreviewSubscriptionRevision != msg.Revision {
		return next
	}
	session, ok := next.Sessions[msg.TerminalID]
	if !ok {
		return next
	}
	session.Snapshot = appendStreamFrame(session.Snapshot, msg.Frame)
	next.Sessions[msg.TerminalID] = session
	return next
}

func appendStreamFrame(snapshot *protocol.Snapshot, frame protocol.StreamFrame) *protocol.Snapshot {
	if snapshot == nil {
		snapshot = &protocol.Snapshot{}
	}
	if frame.Type != protocol.TypeOutput || len(frame.Payload) == 0 {
		return snapshot
	}
	lines := strings.Split(strings.ReplaceAll(string(frame.Payload), "\r\n", "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		row := make([]protocol.Cell, 0, len(line))
		for _, r := range line {
			row = append(row, protocol.Cell{Content: string(r), Width: 1})
		}
		snapshot.Screen.Cells = append(snapshot.Screen.Cells, row)
	}
	return snapshot
}

func truncateLastRune(input string) string {
	if input == "" {
		return ""
	}
	runes := []rune(input)
	return string(runes[:len(runes)-1])
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

type AttachTerminalEffect struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
	ReadOnly   bool
	ForPreview bool
}

func (AttachTerminalEffect) effectName() string { return "attach_terminal" }
