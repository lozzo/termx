package intent

import "github.com/lozzow/termx/tui/domain/types"

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
