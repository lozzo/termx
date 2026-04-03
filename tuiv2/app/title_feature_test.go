package app

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

func TestFeatureTerminalTitleSyncEndToEnd(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	model.SetSendFunc(func(msg tea.Msg) {
		_, cmd := model.Update(msg)
		drainCmd(t, model, cmd, 10)
	})

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatal("expected runtime vterm after snapshot load")
	}

	if _, err := terminal.VTerm.Write([]byte("\x1b]2;OSC Title\x1b\\")); err != nil {
		t.Fatalf("write osc2 failed: %v", err)
	}

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.Title != "OSC Title" {
		t.Fatalf("expected pane title to update, got %q", pane.Title)
	}
	if terminal.Title != "OSC Title" {
		t.Fatalf("expected runtime title to update, got %q", terminal.Title)
	}
	assertViewContains(t, model, "OSC Title")
}
