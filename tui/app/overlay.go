package app

type OverlayKind string

type OverlayState struct {
	Kind OverlayKind
}

type OverlayStack struct {
	stack []OverlayState
}

func EmptyOverlayStack() OverlayStack {
	return OverlayStack{}
}

func (s OverlayStack) HasActive() bool {
	return len(s.stack) > 0
}
