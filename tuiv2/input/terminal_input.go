package input

type TerminalInputKind string

const (
	TerminalInputBytes      TerminalInputKind = "bytes"
	TerminalInputPaste      TerminalInputKind = "paste"
	TerminalInputEncodedKey TerminalInputKind = "encoded-key"
	TerminalInputWheel      TerminalInputKind = "wheel"
)

type TerminalInput struct {
	Kind           TerminalInputKind
	PaneID         string
	Data           []byte
	Text           string
	Repeat         int
	WheelDirection int
}
