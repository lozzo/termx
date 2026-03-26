package overlay

type Kind string

const (
	KindConnectPicker Kind = "connect-picker"
)

type ActiveState struct {
	Kind Kind
}

type State struct {
	Active ActiveState
}

func (s State) OpenConnectPicker() State {
	s.Active = ActiveState{Kind: KindConnectPicker}
	return s
}

func (s State) Clear() State {
	return State{}
}
