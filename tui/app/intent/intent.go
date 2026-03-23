package intent

import (
	"time"

	"github.com/lozzow/termx/tui/domain/types"
)

type Intent interface {
	intentName() string
}

type ConnectSource string

const (
	ConnectSourcePicker          ConnectSource = "picker"
	ConnectSourceManagerHere     ConnectSource = "manager_here"
	ConnectSourceManagerNewTab   ConnectSource = "manager_new_tab"
	ConnectSourceManagerFloating ConnectSource = "manager_floating"
	ConnectSourceRestore         ConnectSource = "restore"
	ConnectSourceLayoutResolve   ConnectSource = "layout_resolve"
)

type ConnectTerminalIntent struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
	Source     ConnectSource
}

func (ConnectTerminalIntent) intentName() string { return "connect_terminal" }

type StopTerminalIntent struct {
	TerminalID types.TerminalID
}

func (StopTerminalIntent) intentName() string { return "stop_terminal" }

type TerminalProgramExitedIntent struct {
	TerminalID types.TerminalID
	ExitCode   int
}

func (TerminalProgramExitedIntent) intentName() string { return "terminal_program_exited" }

type WorkspaceTreeJumpIntent struct {
	WorkspaceID types.WorkspaceID
	TabID       types.TabID
	PaneID      types.PaneID
}

func (WorkspaceTreeJumpIntent) intentName() string { return "workspace_tree_jump" }

type ClosePaneIntent struct {
	PaneID types.PaneID
}

func (ClosePaneIntent) intentName() string { return "close_pane" }

type OpenWorkspacePickerIntent struct{}

func (OpenWorkspacePickerIntent) intentName() string { return "open_workspace_picker" }

type OpenTerminalManagerIntent struct{}

func (OpenTerminalManagerIntent) intentName() string { return "open_terminal_manager" }

type OpenPromptIntent struct {
	PromptKind string
	TerminalID types.TerminalID
}

func (OpenPromptIntent) intentName() string { return "open_prompt" }

type CloseOverlayIntent struct{}

func (CloseOverlayIntent) intentName() string { return "close_overlay" }

type WorkspacePickerMoveIntent struct {
	Delta int
}

func (WorkspacePickerMoveIntent) intentName() string { return "workspace_picker_move" }

type WorkspacePickerAppendQueryIntent struct {
	Text string
}

func (WorkspacePickerAppendQueryIntent) intentName() string { return "workspace_picker_append_query" }

type WorkspacePickerBackspaceIntent struct{}

func (WorkspacePickerBackspaceIntent) intentName() string { return "workspace_picker_backspace" }

type WorkspacePickerExpandIntent struct{}

func (WorkspacePickerExpandIntent) intentName() string { return "workspace_picker_expand" }

type WorkspacePickerCollapseIntent struct{}

func (WorkspacePickerCollapseIntent) intentName() string { return "workspace_picker_collapse" }

type WorkspacePickerSubmitIntent struct{}

func (WorkspacePickerSubmitIntent) intentName() string { return "workspace_picker_submit" }

type TerminalManagerMoveIntent struct {
	Delta int
}

func (TerminalManagerMoveIntent) intentName() string { return "terminal_manager_move" }

type TerminalManagerAppendQueryIntent struct {
	Text string
}

func (TerminalManagerAppendQueryIntent) intentName() string { return "terminal_manager_append_query" }

type TerminalManagerBackspaceIntent struct{}

func (TerminalManagerBackspaceIntent) intentName() string { return "terminal_manager_backspace" }

type TerminalManagerConnectHereIntent struct{}

func (TerminalManagerConnectHereIntent) intentName() string { return "terminal_manager_connect_here" }

type TerminalManagerConnectInNewTabIntent struct{}

func (TerminalManagerConnectInNewTabIntent) intentName() string {
	return "terminal_manager_connect_in_new_tab"
}

type TerminalManagerConnectInFloatingPaneIntent struct{}

func (TerminalManagerConnectInFloatingPaneIntent) intentName() string {
	return "terminal_manager_connect_in_floating_pane"
}

type TerminalManagerEditMetadataIntent struct{}

func (TerminalManagerEditMetadataIntent) intentName() string { return "terminal_manager_edit_metadata" }

type TerminalManagerStopIntent struct{}

func (TerminalManagerStopIntent) intentName() string { return "terminal_manager_stop" }

type SubmitPromptIntent struct {
	Value string
}

func (SubmitPromptIntent) intentName() string { return "submit_prompt" }

type CancelPromptIntent struct{}

func (CancelPromptIntent) intentName() string { return "cancel_prompt" }

type ActivateModeIntent struct {
	Mode       types.ModeKind
	Sticky     bool
	DeadlineAt *time.Time
}

func (ActivateModeIntent) intentName() string { return "activate_mode" }

type ModeTimedOutIntent struct {
	Now time.Time
}

func (ModeTimedOutIntent) intentName() string { return "mode_timed_out" }
