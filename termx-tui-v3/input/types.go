package input

// EventKind classifies host input without depending on Bubble Tea key or mouse
// types.
type EventKind string

const (
	EventKindKey   EventKind = "key"
	EventKindMouse EventKind = "mouse"
)

// InputEvent is the host input boundary owned by TerminalHost.
type InputEvent struct {
	Kind EventKind
}
