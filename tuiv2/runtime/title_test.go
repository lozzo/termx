package runtime

import (
	"testing"

	localvterm "github.com/lozzow/termx/termx-core/vterm"
)

func TestRuntimeTitleUpdateFromOSC2(t *testing.T) {
	runtime := New(nil)

	terminalID := "term-1"
	terminal := runtime.registry.GetOrCreate(terminalID)
	terminal.Channel = 1

	// Create a real VTerm instance
	vt := localvterm.New(80, 24, 1000, nil)
	terminal.VTerm = vt

	// Set up title callback manually (simulating what ensureVTerm does)
	vt.SetTitleHandler(func(title string) {
		terminal.Title = title
	})

	// Send OSC 2 sequence
	_, err := vt.Write([]byte("\x1b]2;My Terminal Title\x1b\\"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Check that terminal.Title was updated
	if terminal.Title != "My Terminal Title" {
		t.Errorf("Expected terminal.Title to be 'My Terminal Title', got '%s'", terminal.Title)
	}

	// Check that Visible() includes the title
	visible := runtime.Visible()
	if len(visible.Terminals) != 1 {
		t.Fatalf("Expected 1 terminal in visible state, got %d", len(visible.Terminals))
	}
	if visible.Terminals[0].Title != "My Terminal Title" {
		t.Errorf("Expected visible terminal title to be 'My Terminal Title', got '%s'", visible.Terminals[0].Title)
	}
}

func TestRuntimeTitleChangeCallbackFromOSC2(t *testing.T) {
	runtime := New(nil)

	terminal := runtime.registry.GetOrCreate("term-1")
	terminal.Channel = 1
	vt := localvterm.New(80, 24, 1000, nil)
	terminal.VTerm = vt

	var gotTerminalID string
	var gotTitle string
	runtime.SetTitleChange(func(terminalID, title string) {
		gotTerminalID = terminalID
		gotTitle = title
	})

	runtime.ensureVTerm(terminal)
	if _, err := terminal.VTerm.Write([]byte("\x1b]2;Callback Title\x1b\\")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if gotTerminalID != "term-1" {
		t.Fatalf("expected callback terminal ID term-1, got %q", gotTerminalID)
	}
	if gotTitle != "Callback Title" {
		t.Fatalf("expected callback title Callback Title, got %q", gotTitle)
	}
}
