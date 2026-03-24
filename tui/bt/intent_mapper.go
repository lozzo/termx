package bt

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/domain/types"
)

const defaultPrefixTimeout = 3 * time.Second

type Clock interface {
	Now() time.Time
}

type IntentMapper interface {
	MapKey(state types.AppState, msg tea.KeyMsg) []intent.Intent
	MapMouse(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent
}

type Config struct {
	Clock         Clock
	PrefixTimeout time.Duration
}

type DefaultIntentMapper struct {
	clock         Clock
	prefixTimeout time.Duration
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func NewIntentMapper(cfg Config) IntentMapper {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	timeout := cfg.PrefixTimeout
	if timeout <= 0 {
		timeout = defaultPrefixTimeout
	}
	return DefaultIntentMapper{
		clock:         clock,
		prefixTimeout: timeout,
	}
}

// MapKey 只负责把 bubbletea 的按键消息翻译成 intent。
// 这里不直接改状态，后续无论接 shell、鼠标还是 server 事件，都统一走 reducer。
func (m DefaultIntentMapper) MapKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	switch state.UI.Overlay.Kind {
	case types.OverlayLayoutResolve:
		return mapLayoutResolveKey(msg)
	case types.OverlayTerminalPicker:
		return mapTerminalPickerKey(msg)
	case types.OverlayWorkspacePicker:
		return mapWorkspacePickerKey(msg)
	case types.OverlayTerminalManager:
		return mapTerminalManagerKey(msg)
	case types.OverlayPrompt:
		return mapPromptKey(msg)
	}
	if intents := m.mapPaneModeKey(state, msg); len(intents) > 0 {
		return intents
	}
	if intents := m.mapTabModeKey(state, msg); len(intents) > 0 {
		return intents
	}
	if intents := m.mapFloatingModeKey(state, msg); len(intents) > 0 {
		return intents
	}
	if intents := m.mapGlobalModeKey(state, msg); len(intents) > 0 {
		return intents
	}
	return m.mapRootKey(state, msg)
}

// MapMouse 统一处理 overlay 的滚轮和最小点击命中。
// 这里仍然只产出 intent，不直接改状态，让鼠标、键盘继续共享同一套 reducer 路径。
func (m DefaultIntentMapper) MapMouse(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	switch state.UI.Overlay.Kind {
	case types.OverlayLayoutResolve:
		if intents := mapLayoutResolveMouse(msg); len(intents) > 0 {
			return intents
		}
		return mapLayoutResolveMouseClick(state, msg, view)
	case types.OverlayTerminalPicker:
		if intents := mapTerminalPickerMouse(msg); len(intents) > 0 {
			return intents
		}
		return mapTerminalPickerMouseClick(state, msg, view)
	case types.OverlayWorkspacePicker:
		if intents := mapWorkspacePickerMouse(msg); len(intents) > 0 {
			return intents
		}
		return mapWorkspacePickerMouseClick(state, msg, view)
	case types.OverlayTerminalManager:
		if intents := mapTerminalManagerMouse(msg); len(intents) > 0 {
			return intents
		}
		return mapTerminalManagerMouseClick(state, msg, view)
	case types.OverlayPrompt:
		return mapPromptMouseClick(state, msg, view)
	default:
		return nil
	}
}

func (m DefaultIntentMapper) mapRootKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	if intents := mapPaneBodyKey(state, msg); len(intents) > 0 {
		return intents
	}
	switch msg.String() {
	case "ctrl+f":
		return []intent.Intent{intent.OpenTerminalPickerIntent{}}
	case "ctrl+w":
		return []intent.Intent{intent.OpenWorkspacePickerIntent{}}
	case "ctrl+g":
		deadline := m.clock.Now().Add(m.prefixTimeout)
		return []intent.Intent{intent.ActivateModeIntent{
			Mode:       types.ModeGlobal,
			Sticky:     false,
			DeadlineAt: &deadline,
		}}
	case "ctrl+p":
		deadline := m.clock.Now().Add(m.prefixTimeout)
		return []intent.Intent{intent.ActivateModeIntent{
			Mode:       types.ModePane,
			Sticky:     false,
			DeadlineAt: &deadline,
		}}
	case "ctrl+t":
		deadline := m.clock.Now().Add(m.prefixTimeout)
		return []intent.Intent{intent.ActivateModeIntent{
			Mode:       types.ModeTab,
			Sticky:     false,
			DeadlineAt: &deadline,
		}}
	case "ctrl+o":
		deadline := m.clock.Now().Add(m.prefixTimeout)
		return []intent.Intent{intent.ActivateModeIntent{
			Mode:       types.ModeFloating,
			Sticky:     false,
			DeadlineAt: &deadline,
		}}
	case "esc":
		if state.UI.Mode.Active != types.ModeNone {
			return []intent.Intent{intent.ActivateModeIntent{Mode: types.ModeNone}}
		}
	}
	return nil
}

func mapPaneBodyKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	pane, ok := focusedPane(state)
	if !ok {
		return nil
	}
	switch pane.SlotState {
	case types.PaneSlotEmpty, types.PaneSlotWaiting:
		switch msg.String() {
		case "n":
			return []intent.Intent{intent.CreateTerminalInActivePaneIntent{}}
		case "a":
			return []intent.Intent{intent.OpenTerminalPickerIntent{}}
		case "m":
			return []intent.Intent{intent.OpenTerminalManagerIntent{}}
		case "x":
			return []intent.Intent{intent.ClosePaneIntent{PaneID: state.UI.Focus.PaneID}}
		}
	case types.PaneSlotExited:
		switch msg.String() {
		case "r":
			return []intent.Intent{intent.RestartProgramExitedTerminalIntent{PaneID: state.UI.Focus.PaneID}}
		case "a":
			return []intent.Intent{intent.OpenTerminalPickerIntent{}}
		case "x":
			return []intent.Intent{intent.ClosePaneIntent{PaneID: state.UI.Focus.PaneID}}
		}
	}
	return nil
}

func (m DefaultIntentMapper) mapGlobalModeKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	if state.UI.Mode.Active != types.ModeGlobal {
		return nil
	}
	switch msg.String() {
	case "t":
		return []intent.Intent{intent.OpenTerminalManagerIntent{}}
	case "s":
		return []intent.Intent{intent.SplitActivePaneIntent{}}
	case "esc":
		return []intent.Intent{intent.ActivateModeIntent{Mode: types.ModeNone}}
	default:
		return nil
	}
}

func (m DefaultIntentMapper) mapPaneModeKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	if state.UI.Mode.Active != types.ModePane {
		return nil
	}
	switch msg.String() {
	case "h", "left":
		return []intent.Intent{intent.PaneFocusMoveIntent{Direction: types.DirectionLeft}}
	case "j", "down":
		return []intent.Intent{intent.PaneFocusMoveIntent{Direction: types.DirectionDown}}
	case "k", "up":
		return []intent.Intent{intent.PaneFocusMoveIntent{Direction: types.DirectionUp}}
	case "l", "right":
		return []intent.Intent{intent.PaneFocusMoveIntent{Direction: types.DirectionRight}}
	case "esc":
		return []intent.Intent{intent.ActivateModeIntent{Mode: types.ModeNone}}
	default:
		return nil
	}
}

func (m DefaultIntentMapper) mapTabModeKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	if state.UI.Mode.Active != types.ModeTab {
		return nil
	}
	switch msg.String() {
	case "n":
		return []intent.Intent{intent.CreateTabIntent{}}
	case "h", "left":
		return []intent.Intent{intent.TabFocusMoveIntent{Delta: -1}}
	case "l", "right":
		return []intent.Intent{intent.TabFocusMoveIntent{Delta: 1}}
	case "esc":
		return []intent.Intent{intent.ActivateModeIntent{Mode: types.ModeNone}}
	default:
		return nil
	}
}

func (m DefaultIntentMapper) mapFloatingModeKey(state types.AppState, msg tea.KeyMsg) []intent.Intent {
	if state.UI.Mode.Active != types.ModeFloating {
		return nil
	}
	switch msg.String() {
	case "n":
		return []intent.Intent{intent.CreateFloatingPaneIntent{}}
	case "j", "down":
		return []intent.Intent{intent.MoveFloatingPaneIntent{DeltaY: 2}}
	case "k", "up":
		return []intent.Intent{intent.MoveFloatingPaneIntent{DeltaY: -2}}
	case "H":
		return []intent.Intent{intent.MoveFloatingPaneIntent{DeltaX: -2}}
	case "L":
		return []intent.Intent{intent.MoveFloatingPaneIntent{DeltaX: 2}}
	case "c":
		return []intent.Intent{intent.CenterFloatingPaneIntent{}}
	case "h", "left":
		return []intent.Intent{intent.FloatingFocusMoveIntent{Delta: -1}}
	case "l", "right":
		return []intent.Intent{intent.FloatingFocusMoveIntent{Delta: 1}}
	case "esc":
		return []intent.Intent{intent.ActivateModeIntent{Mode: types.ModeNone}}
	default:
		return nil
	}
}

func mapTerminalPickerKey(msg tea.KeyMsg) []intent.Intent {
	switch msg.String() {
	case "up", "k":
		return []intent.Intent{intent.TerminalPickerMoveIntent{Delta: -1}}
	case "down", "j":
		return []intent.Intent{intent.TerminalPickerMoveIntent{Delta: 1}}
	case "enter":
		return []intent.Intent{intent.TerminalPickerSubmitIntent{}}
	case "esc":
		return []intent.Intent{intent.CloseOverlayIntent{}}
	case "backspace", "delete":
		return []intent.Intent{intent.TerminalPickerBackspaceIntent{}}
	default:
		if text := inputText(msg); text != "" {
			return []intent.Intent{intent.TerminalPickerAppendQueryIntent{Text: text}}
		}
		return nil
	}
}

func mapTerminalPickerMouse(msg tea.MouseMsg) []intent.Intent {
	switch tea.MouseEvent(msg).Button {
	case tea.MouseButtonWheelUp:
		return []intent.Intent{intent.TerminalPickerMoveIntent{Delta: -1}}
	case tea.MouseButtonWheelDown:
		return []intent.Intent{intent.TerminalPickerMoveIntent{Delta: 1}}
	default:
		return nil
	}
}

func mapLayoutResolveKey(msg tea.KeyMsg) []intent.Intent {
	switch msg.String() {
	case "up", "k":
		return []intent.Intent{intent.LayoutResolveMoveIntent{Delta: -1}}
	case "down", "j":
		return []intent.Intent{intent.LayoutResolveMoveIntent{Delta: 1}}
	case "enter":
		return []intent.Intent{intent.LayoutResolveSubmitIntent{}}
	case "esc":
		return []intent.Intent{intent.CloseOverlayIntent{}}
	default:
		return nil
	}
}

func mapLayoutResolveMouse(msg tea.MouseMsg) []intent.Intent {
	switch tea.MouseEvent(msg).Button {
	case tea.MouseButtonWheelUp:
		return []intent.Intent{intent.LayoutResolveMoveIntent{Delta: -1}}
	case tea.MouseButtonWheelDown:
		return []intent.Intent{intent.LayoutResolveMoveIntent{Delta: 1}}
	default:
		return nil
	}
}

func focusedPane(state types.AppState) (types.PaneState, bool) {
	workspace, ok := state.Domain.Workspaces[state.UI.Focus.WorkspaceID]
	if !ok {
		return types.PaneState{}, false
	}
	tab, ok := workspace.Tabs[state.UI.Focus.TabID]
	if !ok {
		return types.PaneState{}, false
	}
	pane, ok := tab.Panes[state.UI.Focus.PaneID]
	if !ok {
		return types.PaneState{}, false
	}
	return pane, true
}

func mapWorkspacePickerKey(msg tea.KeyMsg) []intent.Intent {
	switch msg.String() {
	case "up", "k":
		return []intent.Intent{intent.WorkspacePickerMoveIntent{Delta: -1}}
	case "down", "j":
		return []intent.Intent{intent.WorkspacePickerMoveIntent{Delta: 1}}
	case "left", "h":
		return []intent.Intent{intent.WorkspacePickerCollapseIntent{}}
	case "right", "l":
		return []intent.Intent{intent.WorkspacePickerExpandIntent{}}
	case "enter":
		return []intent.Intent{intent.WorkspacePickerSubmitIntent{}}
	case "esc":
		return []intent.Intent{intent.CloseOverlayIntent{}}
	case "backspace", "delete":
		return []intent.Intent{intent.WorkspacePickerBackspaceIntent{}}
	default:
		if text := inputText(msg); text != "" {
			return []intent.Intent{intent.WorkspacePickerAppendQueryIntent{Text: text}}
		}
		return nil
	}
}

func mapWorkspacePickerMouse(msg tea.MouseMsg) []intent.Intent {
	switch tea.MouseEvent(msg).Button {
	case tea.MouseButtonWheelUp:
		return []intent.Intent{intent.WorkspacePickerMoveIntent{Delta: -1}}
	case tea.MouseButtonWheelDown:
		return []intent.Intent{intent.WorkspacePickerMoveIntent{Delta: 1}}
	default:
		return nil
	}
}

func mapTerminalManagerKey(msg tea.KeyMsg) []intent.Intent {
	switch msg.String() {
	case "up":
		return []intent.Intent{intent.TerminalManagerMoveIntent{Delta: -1}}
	case "down":
		return []intent.Intent{intent.TerminalManagerMoveIntent{Delta: 1}}
	case "enter":
		return []intent.Intent{intent.TerminalManagerConnectHereIntent{}}
	case "j":
		return []intent.Intent{intent.TerminalManagerJumpToConnectedPaneIntent{}}
	case "t":
		return []intent.Intent{intent.TerminalManagerConnectInNewTabIntent{}}
	case "o":
		return []intent.Intent{intent.TerminalManagerConnectInFloatingPaneIntent{}}
	case "e":
		return []intent.Intent{intent.TerminalManagerEditMetadataIntent{}}
	case "a":
		return []intent.Intent{intent.TerminalManagerAcquireOwnerIntent{}}
	case "k":
		return []intent.Intent{intent.TerminalManagerStopIntent{}}
	case "esc":
		return []intent.Intent{intent.CloseOverlayIntent{}}
	case "backspace", "delete":
		return []intent.Intent{intent.TerminalManagerBackspaceIntent{}}
	default:
		if text := inputText(msg); text != "" {
			return []intent.Intent{intent.TerminalManagerAppendQueryIntent{Text: text}}
		}
		return nil
	}
}

func mapTerminalManagerMouse(msg tea.MouseMsg) []intent.Intent {
	switch tea.MouseEvent(msg).Button {
	case tea.MouseButtonWheelUp:
		return []intent.Intent{intent.TerminalManagerMoveIntent{Delta: -1}}
	case tea.MouseButtonWheelDown:
		return []intent.Intent{intent.TerminalManagerMoveIntent{Delta: 1}}
	default:
		return nil
	}
}

func mapPromptKey(msg tea.KeyMsg) []intent.Intent {
	switch msg.String() {
	case "tab":
		return []intent.Intent{intent.PromptNextFieldIntent{}}
	case "shift+tab":
		return []intent.Intent{intent.PromptPreviousFieldIntent{}}
	case "enter":
		return []intent.Intent{intent.SubmitPromptIntent{}}
	case "esc":
		return []intent.Intent{intent.CancelPromptIntent{}}
	case "backspace", "delete":
		return []intent.Intent{intent.PromptBackspaceIntent{}}
	default:
		if text := inputText(msg); text != "" {
			return []intent.Intent{intent.PromptAppendInputIntent{Text: text}}
		}
		return nil
	}
}

func inputText(msg tea.KeyMsg) string {
	switch {
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0:
		return string(msg.Runes)
	case msg.String() == " ":
		return " "
	default:
		return ""
	}
}
