package overlay

type Kind string

const (
	KindConnectPicker Kind = "connect-picker"
	KindHelp          Kind = "help"
	KindPrompt        Kind = "prompt"
)

type ActiveState struct {
	Kind  Kind
	Title string
}

type State struct {
	Active ActiveState
}

func (s State) OpenConnectPicker() State {
	s.Active = ActiveState{Kind: KindConnectPicker, Title: "connect"}
	return s
}

func (s State) OpenHelp() State {
	s.Active = ActiveState{Kind: KindHelp, Title: "help"}
	return s
}

func (s State) OpenPrompt(title string) State {
	s.Active = ActiveState{Kind: KindPrompt, Title: title}
	return s
}

func (s State) Clear() State {
	return State{}
}
