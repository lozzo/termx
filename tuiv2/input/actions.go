package input

type ActionKind string

const (
	ActionFocusPane           ActionKind = "focus-pane"
	ActionSplitPane           ActionKind = "split-pane"
	ActionClosePane           ActionKind = "close-pane"
	ActionDetachPane          ActionKind = "detach-pane"
	ActionOpenPicker          ActionKind = "open-picker"
	ActionOpenTerminalManager ActionKind = "open-terminal-manager"
	ActionOpenPrompt          ActionKind = "open-prompt"
	ActionSubmitPrompt        ActionKind = "submit-prompt"
	ActionCancelMode          ActionKind = "cancel-mode"
	ActionKillTerminal        ActionKind = "kill-terminal"
	ActionRemoveTerminal      ActionKind = "remove-terminal"
	ActionBecomeOwner         ActionKind = "become-owner"
)

type SemanticAction struct {
	Kind      ActionKind
	Workspace string
	TabID     string
	PaneID    string
	TargetID  string
	Text      string
}
