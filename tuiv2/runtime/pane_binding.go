package runtime

type PaneBinding struct {
	PaneID    string
	Role      BindingRole
	Connected bool
	Channel   uint16
}
