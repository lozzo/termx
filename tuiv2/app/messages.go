package app

import (
	"image/color"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

type RenderTickMsg struct{}

// InvalidateMsg is sent by the runtime stream goroutine to trigger a View() redraw.
type InvalidateMsg struct{}

type renderRefreshMsg struct{}

type SemanticActionMsg struct {
	Action input.SemanticAction
}

type TerminalInputMsg struct {
	Input input.TerminalInput
}

type interactionBatchMsg struct {
	Messages []tea.Msg
}

type keyBurstMsg struct {
	Msg    tea.KeyMsg
	Repeat int
}

type mouseWheelBurstMsg struct {
	Msg    tea.MouseMsg
	Repeat int
}

type queuedMouseMsg struct {
	Seq      uint64
	Kind     string
	QueuedAt time.Time
	Msg      tea.MouseMsg
}

type mouseMotionFlushMsg struct {
	epoch uint64
}

type terminalInputSentMsg struct {
	err        error
	paneID     string
	terminalID string
}

type terminalWheelDispatchMsg struct {
	seq uint64
}

type sharedTerminalSnapshotResyncMsg struct {
	seq        uint64
	paneID     string
	terminalID string
}

type terminalAttachReadyMsg struct {
	paneID     string
	terminalID string
}

type paneAttachFailedMsg struct {
	PaneID     string
	TerminalID string
	Err        error
}

type EffectAppliedMsg struct {
	Effect orchestrator.Effect
}

type clearErrorMsg struct {
	seq uint64
}

type clearOwnerConfirmMsg struct {
	seq uint64
}

type clearNoticeMsg struct {
	seq uint64
}

type copyModeAutoScrollMsg struct {
	seq uint64
}

type terminalTitleMsg struct {
	TerminalID string
	Title      string
}

type pickerItemsLoadedMsg struct {
	Items []modal.PickerItem
}

type terminalManagerItemsLoadedMsg struct {
	Items []modal.PickerItem
}

type terminalSizeLockToggledMsg struct {
	Notice string
}

type hostDefaultColorsMsg struct {
	FG color.Color
	BG color.Color
}

type hostPaletteColorMsg struct {
	Index int
	Color color.Color
}

type hostThemeFlushMsg struct{}

type hostCursorPositionMsg struct {
	X int
	Y int
}

type hostEmojiProbeMsg struct {
	Attempt int
}

type hostEmojiProbeGiveUpMsg struct{}

// reattachFailedMsg is sent when a pane's terminal could not be re-attached on
// startup. The handler opens the terminal picker for that pane if it is still
// the active pane and has no terminal bound.
type reattachFailedMsg struct {
	tabID  string
	paneID string
}

type prefixTimeoutMsg struct{ seq int }

type sessionSnapshotMsg struct {
	Snapshot *protocol.SessionSnapshot
	Err      error
}

type sessionViewUpdatedMsg struct {
	View *protocol.ViewInfo
	Err  error
}

type sessionEventMsg struct {
	Event protocol.Event
}

type terminalEventMsg struct {
	Event protocol.Event
}
