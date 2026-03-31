package input

type ActionKind string

const (
	ActionFocusPane           ActionKind = "focus-pane"
	ActionFocusPaneLeft       ActionKind = "focus-pane-left"
	ActionFocusPaneRight      ActionKind = "focus-pane-right"
	ActionFocusPaneUp         ActionKind = "focus-pane-up"
	ActionFocusPaneDown       ActionKind = "focus-pane-down"
	ActionSplitPane           ActionKind = "split-pane"
	ActionSplitPaneHorizontal ActionKind = "split-pane-horizontal"
	ActionClosePane           ActionKind = "close-pane"
	ActionDetachPane          ActionKind = "detach-pane"
	ActionEnterPaneMode       ActionKind = "enter-pane-mode"
	ActionEnterResizeMode     ActionKind = "enter-resize-mode"
	ActionEnterTabMode        ActionKind = "enter-tab-mode"
	ActionEnterWorkspaceMode  ActionKind = "enter-workspace-mode"
	ActionEnterFloatingMode   ActionKind = "enter-floating-mode"
	ActionEnterDisplayMode    ActionKind = "enter-display-mode"
	ActionEnterGlobalMode     ActionKind = "enter-global-mode"
	ActionOpenPicker          ActionKind = "open-picker"
	ActionOpenWorkspacePicker ActionKind = "open-workspace-picker"
	ActionOpenHelp            ActionKind = "open-help"
	ActionOpenTerminalManager ActionKind = "open-terminal-manager"
	ActionOpenPrompt          ActionKind = "open-prompt"
	ActionSubmitPrompt        ActionKind = "submit-prompt"
	ActionCancelMode          ActionKind = "cancel-mode"
	ActionPickerUp            ActionKind = "picker-up"
	ActionPickerDown          ActionKind = "picker-down"
	ActionScrollUp            ActionKind = "scroll-up"
	ActionScrollDown          ActionKind = "scroll-down"
	ActionZoomPane           ActionKind = "zoom-pane"
	ActionQuit                ActionKind = "quit"
	ActionCreateWorkspace     ActionKind = "create-workspace"
	ActionSwitchWorkspace     ActionKind = "switch-workspace"
	ActionDeleteWorkspace     ActionKind = "delete-workspace"
	ActionCreateTab           ActionKind = "create-tab"
	ActionCloseTab            ActionKind = "close-tab"
	ActionNextTab             ActionKind = "next-tab"
	ActionPrevTab             ActionKind = "prev-tab"
	ActionKillTerminal        ActionKind = "kill-terminal"
	ActionRemoveTerminal      ActionKind = "remove-terminal"
	ActionBecomeOwner         ActionKind = "become-owner"
	ActionResizePaneLeft      ActionKind = "resize-pane-left"
	ActionResizePaneRight     ActionKind = "resize-pane-right"
	ActionResizePaneUp        ActionKind = "resize-pane-up"
	ActionResizePaneDown      ActionKind = "resize-pane-down"
	ActionCreateFloatingPane  ActionKind = "create-floating-pane"
)

type SemanticAction struct {
	Kind      ActionKind
	Workspace string
	TabID     string
	PaneID    string
	TargetID  string
	Text      string
}
