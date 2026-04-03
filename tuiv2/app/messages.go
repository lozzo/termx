package app

import (
	"image/color"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

type RenderTickMsg struct{}

// InvalidateMsg is sent by the runtime stream goroutine to trigger a View() redraw.
type InvalidateMsg struct{}

type SemanticActionMsg struct {
	Action input.SemanticAction
}

type TerminalInputMsg struct {
	Input input.TerminalInput
}

type EffectAppliedMsg struct {
	Effect orchestrator.Effect
}

type clearErrorMsg struct {
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

type hostDefaultColorsMsg struct {
	FG color.Color
	BG color.Color
}

type hostPaletteColorMsg struct {
	Index int
	Color color.Color
}

// reattachFailedMsg is sent when a pane's terminal could not be re-attached on
// startup. The handler opens the terminal picker for that pane if it is still
// the active pane and has no terminal bound.
type reattachFailedMsg struct {
	tabID  string
	paneID string
}

type prefixTimeoutMsg struct{ seq int }
