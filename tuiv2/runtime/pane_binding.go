package runtime

type PaneBinding struct {
	PaneID     string
	TerminalID string
	Role       BindingRole
	Connected  bool
	Channel    uint16
}
