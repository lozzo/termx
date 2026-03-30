package orchestrator

import "github.com/lozzow/termx/tuiv2/input"

type Effect interface {
	effectTag()
}

type OpenPickerEffect struct {
	RequestID string
}

func (OpenPickerEffect) effectTag() {}

type AttachTerminalEffect struct {
	PaneID     string
	TerminalID string
	Mode       string
}

func (AttachTerminalEffect) effectTag() {}

type LoadSnapshotEffect struct {
	TerminalID string
	Offset     int
	Limit      int
}

func (LoadSnapshotEffect) effectTag() {}

type SetInputModeEffect struct {
	Mode input.ModeState
}

func (SetInputModeEffect) effectTag() {}
