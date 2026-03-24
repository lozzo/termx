package terminalmanager

type ActionID string

const (
	ActionJumpToConnectedPane ActionID = "jump"
	ActionConnectHere  ActionID = "connect_here"
	ActionNewTab       ActionID = "new_tab"
	ActionFloatingPane ActionID = "floating"
	ActionEditMetadata ActionID = "edit"
	ActionAcquireOwner ActionID = "acquire_owner"
	ActionStop         ActionID = "stop"
)

type ActionRow struct {
	ID    ActionID
	Label string
}

func ActionRows() []ActionRow {
	return []ActionRow{
		{ID: ActionJumpToConnectedPane, Label: "jump to connected pane"},
		{ID: ActionConnectHere, Label: "connect here"},
		{ID: ActionNewTab, Label: "open in new tab"},
		{ID: ActionFloatingPane, Label: "open in floating pane"},
		{ID: ActionEditMetadata, Label: "edit metadata"},
		{ID: ActionAcquireOwner, Label: "acquire owner"},
		{ID: ActionStop, Label: "stop terminal"},
	}
}
