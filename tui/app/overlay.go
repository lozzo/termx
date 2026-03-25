package app

import (
	"github.com/lozzow/termx/tui/state/pool"
)

type OverlayKind string
type ConnectTarget string

const (
	OverlayConnectDialog OverlayKind = "connect-dialog"
	OverlayHelp          OverlayKind = "help"

	ConnectTargetSplitRight ConnectTarget = "split-right"
	ConnectTargetNewTab     ConnectTarget = "new-tab"
	ConnectTargetNewFloat   ConnectTarget = "new-floating"
	ConnectTargetReconnect  ConnectTarget = "reconnect"
)

type ConnectDialogState struct {
	Target      ConnectTarget
	Destination string
	Query       string
	Items       []pool.ConnectItem
}

type HelpOverlayState struct {
	Sections []HelpSection
}

type HelpSection struct {
	Title string
	Items []string
}

type OverlayState struct {
	Kind    OverlayKind
	Connect *ConnectDialogState
	Help    *HelpOverlayState
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

func (s OverlayStack) Active() OverlayState {
	if len(s.stack) == 0 {
		return OverlayState{}
	}
	return s.stack[len(s.stack)-1]
}

func (s OverlayStack) Push(state OverlayState) OverlayStack {
	next := s.Clone()
	next.stack = append(next.stack, state)
	return next
}

func (s OverlayStack) Replace(state OverlayState) OverlayStack {
	return OverlayStack{stack: []OverlayState{state}}
}

func (s OverlayStack) Clear() OverlayStack {
	return OverlayStack{}
}

func (s OverlayStack) Clone() OverlayStack {
	if len(s.stack) == 0 {
		return OverlayStack{}
	}
	out := make([]OverlayState, len(s.stack))
	copy(out, s.stack)
	return OverlayStack{stack: out}
}

func DefaultHelpOverlay() *HelpOverlayState {
	return &HelpOverlayState{
		Sections: []HelpSection{
			{Title: "Most Used", Items: []string{"c-f connect pane", "c-o new float", "c-t tab actions"}},
			{Title: "Pane / Tab / Workspace", Items: []string{"split pane", "close pane", "switch tab", "switch workspace"}},
			{Title: "Shared Terminal", Items: []string{"connect existing", "become owner", "kill vs remove", "restart exited"}},
			{Title: "Floating", Items: []string{"move", "resize", "recall center", "close float"}},
			{Title: "Exit / Close", Items: []string{"close pane keeps terminal", "Esc closes overlay"}},
		},
	}
}
