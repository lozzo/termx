package terminalpool

import (
	"testing"

	corepool "github.com/lozzow/termx/tui/core/pool"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

func TestApplyGroupsBuildsVisibleParkedExitedAndSelection(t *testing.T) {
	state := State{}
	state.ApplyGroups(corepool.Groups{
		Visible: []coreterminal.Metadata{{ID: types.TerminalID("term-visible"), Name: "visible-shell", State: coreterminal.StateRunning}},
		Parked:  []coreterminal.Metadata{{ID: types.TerminalID("term-parked"), Name: "parked-shell", State: coreterminal.StateRunning}},
		Exited:  []coreterminal.Metadata{{ID: types.TerminalID("term-exited"), Name: "exited-shell", State: coreterminal.StateExited}},
	})

	if len(state.Visible) != 1 || state.Visible[0].ID != types.TerminalID("term-visible") {
		t.Fatalf("expected visible item, got %#v", state.Visible)
	}
	if len(state.Parked) != 1 || state.Parked[0].ID != types.TerminalID("term-parked") {
		t.Fatalf("expected parked item, got %#v", state.Parked)
	}
	if len(state.Exited) != 1 || state.Exited[0].ID != types.TerminalID("term-exited") {
		t.Fatalf("expected exited item, got %#v", state.Exited)
	}
	if state.SelectedTerminalID != types.TerminalID("term-visible") {
		t.Fatalf("expected first visible item selected, got %q", state.SelectedTerminalID)
	}
}
