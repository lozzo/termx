package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
)

func TestFeatureTerminalTitleSyncEndToEnd(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 1, Mode: "collaborator"},
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-1",
			Name:  "shell",
			State: "running",
		}}},
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

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		pane := model.workbench.ActivePane()
		if pane != nil && pane.Title == "shell" && terminal.Title == "OSC Title" {
			assertViewContains(t, model, "shell")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.Title != "shell" {
		t.Fatalf("expected pane title to stay on terminal name, got %q", pane.Title)
	}
	if terminal.Title != "OSC Title" {
		t.Fatalf("expected runtime title to update, got %q", terminal.Title)
	}
	if strings.Contains(xansi.Strip(model.View()), "OSC Title") {
		t.Fatalf("expected pane chrome to ignore OSC title, got:\n%s", xansi.Strip(model.View()))
	}
}

func TestFeatureTerminalTitleCallbackDoesNotBlockTerminalWrite(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	release := make(chan struct{})
	model.SetSendFunc(func(msg tea.Msg) {
		<-release
	})

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 10); err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatal("expected runtime vterm after snapshot load")
	}

	done := make(chan error, 1)
	go func() {
		_, err := terminal.VTerm.Write([]byte("\x1b]2;Unblocked Title\x1b\\"))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write osc2 failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("terminal write blocked on title callback send")
	}

	close(release)
	if terminal.Title != "Unblocked Title" {
		t.Fatalf("expected runtime title to update, got %q", terminal.Title)
	}
}
