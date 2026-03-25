package overlay

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/pool"
	"github.com/lozzow/termx/tui/state/types"
)

func TestRenderConnectDialogShowsTargetAndSelection(t *testing.T) {
	model := app.NewModel()
	model.Overlay = app.EmptyOverlayStack().Push(app.OverlayState{
		Kind: app.OverlayConnectDialog,
		Connect: &app.ConnectDialogState{
			Target:      app.ConnectTargetSplitRight,
			Destination: "ws-1 / tab-1 / pane-2",
			Items: []pool.ConnectItem{
				{TerminalID: types.TerminalID(""), Name: "+ new terminal"},
				{TerminalID: types.TerminalID("term-1"), Name: "api-dev", StateSummary: "running", OwnerSummary: "owner elsewhere"},
			},
		},
	})

	view := Render(model, 80, 20)
	if !strings.Contains(view, "Connect Pane") {
		t.Fatal("expected connect dialog title")
	}
	if !strings.Contains(view, "split-right") {
		t.Fatal("expected target summary")
	}
	if !strings.Contains(view, "api-dev") || !strings.Contains(view, "owner elsewhere") {
		t.Fatal("expected existing terminal entry")
	}
}

func TestRenderHelpOverlayShowsGroupedHelp(t *testing.T) {
	model := app.NewModel()
	model = model.Apply(app.IntentOpenHelp)

	view := Render(model, 80, 20)
	if !strings.Contains(view, "Most Used") {
		t.Fatal("expected Most Used section")
	}
	if !strings.Contains(view, "Shared Terminal") {
		t.Fatal("expected Shared Terminal section")
	}
	if !strings.Contains(view, "kill vs remove") {
		t.Fatal("expected semantic help text")
	}
}
