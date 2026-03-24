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

type StopTerminalSucceededIntent struct {
	TerminalID types.TerminalID
}

func (StopTerminalSucceededIntent) intentName() string { return "stop_terminal_succeeded" }

type CreateTerminalSucceededIntent struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
	Name       string
	Command    []string
	State      types.TerminalRunState
}

func (CreateTerminalSucceededIntent) intentName() string { return "create_terminal_succeeded" }

type ConnectTerminalInNewTabSucceededIntent struct {
	WorkspaceID types.WorkspaceID
	TerminalID  types.TerminalID
}

func (ConnectTerminalInNewTabSucceededIntent) intentName() string {
	return "connect_terminal_in_new_tab_succeeded"
}

type ConnectTerminalInFloatingPaneSucceededIntent struct {
	WorkspaceID types.WorkspaceID
	TabID       types.TabID
	TerminalID  types.TerminalID
}

func (ConnectTerminalInFloatingPaneSucceededIntent) intentName() string {
	return "connect_terminal_in_floating_pane_succeeded"
}

type TerminalProgramExitedIntent struct {
	TerminalID types.TerminalID
	ExitCode   int
}

func (TerminalProgramExitedIntent) intentName() string { return "terminal_program_exited" }

type TerminalRemovedIntent struct {
	TerminalID types.TerminalID
}

func (TerminalRemovedIntent) intentName() string { return "terminal_removed" }

type RegisterTerminalIntent struct {
	TerminalID types.TerminalID
	Name       string
	Command    []string
	State      types.TerminalRunState
}

func (RegisterTerminalIntent) intentName() string { return "register_terminal" }

type SyncTerminalStateIntent struct {
	TerminalID types.TerminalID
	State      types.TerminalRunState
	ExitCode   *int
}

func (SyncTerminalStateIntent) intentName() string { return "sync_terminal_state" }

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

type OpenTerminalPickerIntent struct{}

func (OpenTerminalPickerIntent) intentName() string { return "open_terminal_picker" }

type OpenLayoutResolveIntent struct {
	PaneID types.PaneID
	Role   string
	Hint   string
}

func (OpenLayoutResolveIntent) intentName() string { return "open_layout_resolve" }

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

type TerminalPickerMoveIntent struct {
	Delta int
}

func (TerminalPickerMoveIntent) intentName() string { return "terminal_picker_move" }

type TerminalPickerAppendQueryIntent struct {
	Text string
}

func (TerminalPickerAppendQueryIntent) intentName() string { return "terminal_picker_append_query" }

type TerminalPickerBackspaceIntent struct{}

func (TerminalPickerBackspaceIntent) intentName() string { return "terminal_picker_backspace" }

type TerminalPickerSubmitIntent struct{}

func (TerminalPickerSubmitIntent) intentName() string { return "terminal_picker_submit" }

type LayoutResolveMoveIntent struct {
	Delta int
}

func (LayoutResolveMoveIntent) intentName() string { return "layout_resolve_move" }

type LayoutResolveSubmitIntent struct{}

func (LayoutResolveSubmitIntent) intentName() string { return "layout_resolve_submit" }

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

type TerminalManagerAcquireOwnerIntent struct{}

func (TerminalManagerAcquireOwnerIntent) intentName() string { return "terminal_manager_acquire_owner" }

type TerminalManagerStopIntent struct{}

func (TerminalManagerStopIntent) intentName() string { return "terminal_manager_stop" }

type TerminalManagerCreateTerminalIntent struct{}

func (TerminalManagerCreateTerminalIntent) intentName() string {
	return "terminal_manager_create_terminal"
}

type SplitActivePaneIntent struct{}

func (SplitActivePaneIntent) intentName() string { return "split_active_pane" }

type SubmitPromptIntent struct {
	Value string
}

func (SubmitPromptIntent) intentName() string { return "submit_prompt" }

type UpdateTerminalMetadataSucceededIntent struct {
	TerminalID types.TerminalID
	Name       string
	Tags       map[string]string
}

func (UpdateTerminalMetadataSucceededIntent) intentName() string {
	return "update_terminal_metadata_succeeded"
}

type CancelPromptIntent struct{}

func (CancelPromptIntent) intentName() string { return "cancel_prompt" }

type PromptAppendInputIntent struct {
	Text string
}

func (PromptAppendInputIntent) intentName() string { return "prompt_append_input" }

type PromptBackspaceIntent struct{}

func (PromptBackspaceIntent) intentName() string { return "prompt_backspace" }

type PromptNextFieldIntent struct{}

func (PromptNextFieldIntent) intentName() string { return "prompt_next_field" }

type PromptPreviousFieldIntent struct{}

func (PromptPreviousFieldIntent) intentName() string { return "prompt_previous_field" }

type PromptSelectFieldIntent struct {
	Index int
}

func (PromptSelectFieldIntent) intentName() string { return "prompt_select_field" }

type PaneFocusMoveIntent struct {
	Direction types.Direction
}

func (PaneFocusMoveIntent) intentName() string { return "pane_focus_move" }

type TabFocusMoveIntent struct {
	Delta int
}

func (TabFocusMoveIntent) intentName() string { return "tab_focus_move" }

type CreateTabIntent struct{}

func (CreateTabIntent) intentName() string { return "create_tab" }

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
