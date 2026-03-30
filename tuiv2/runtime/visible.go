package runtime

type VisibleRuntime struct {
	Terminals []VisibleTerminal
}

type VisibleTerminal struct {
	TerminalID   string
	Name         string
	State        string
	AttachMode   string
	OwnerPaneID  string
	BoundPaneIDs []string
}
