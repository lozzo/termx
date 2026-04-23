package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

type interactionDecisionKind string

const (
	interactionDecisionIgnore interactionDecisionKind = "ignore"

	interactionDecisionMouseClick            interactionDecisionKind = "mouse_click"
	interactionDecisionMouseWheel            interactionDecisionKind = "mouse_wheel"
	interactionDecisionForwardTerminalMouse  interactionDecisionKind = "forward_terminal_mouse"
	interactionDecisionUpdateCopySelection   interactionDecisionKind = "update_copy_selection"
	interactionDecisionStopCopySelection     interactionDecisionKind = "stop_copy_selection"
	interactionDecisionMouseDrag             interactionDecisionKind = "mouse_drag"
	interactionDecisionMouseRelease          interactionDecisionKind = "mouse_release"
	interactionDecisionFinalizeCopySelection interactionDecisionKind = "finalize_copy_selection"
	interactionDecisionFinalizeMouseDrag     interactionDecisionKind = "finalize_mouse_drag"
)

type interactionDecision struct {
	Kind interactionDecisionKind
	Msg  tea.MouseMsg
	X    int
	Y    int
}

type scrollPolicyKind string

const (
	scrollPolicyNone            scrollPolicyKind = "none"
	scrollPolicyHandled         scrollPolicyKind = "handled"
	scrollPolicyCopyModeMove    scrollPolicyKind = "copy_mode_move"
	scrollPolicySwitchTab       scrollPolicyKind = "switch_tab"
	scrollPolicyForwardTerminal scrollPolicyKind = "forward_terminal"
	scrollPolicyLocalScrollback scrollPolicyKind = "local_scrollback"
)

type scrollPolicyDecision struct {
	Kind           scrollPolicyKind
	Cmd            tea.Cmd
	Delta          int
	LocalRepeat    int
	ForwardedInput input.TerminalInput
	TargetPaneID   string
}
