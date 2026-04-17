package runtime

type PaneViewportState struct {
	Offset int
}

type PaneBinding struct {
	PaneID    string
	Role      BindingRole
	Connected bool
	Channel   uint16
	Viewport  PaneViewportState
}
