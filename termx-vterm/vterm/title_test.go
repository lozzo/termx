package vterm

import (
	"strings"
	"testing"
)

func TestVTermTitleCallback(t *testing.T) {
	var capturedTitle string
	titleHandler := func(title string) {
		capturedTitle = title
	}

	vt := New(80, 24, 1000, nil)
	vt.SetTitleHandler(titleHandler)

	// OSC 2 ; title ST (where ST is ESC \)
	_, err := vt.Write([]byte("\x1b]2;Test Title\x1b\\"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if capturedTitle != "Test Title" {
		t.Errorf("Expected title 'Test Title', got '%s'", capturedTitle)
	}
}

func TestVTermTitleCallbackNotSetDoesNotPanic(t *testing.T) {
	vt := New(80, 24, 1000, nil)
	// Don't set title handler

	// Should not panic
	_, err := vt.Write([]byte("\x1b]2;Test Title\x1b\\"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func TestVTermTitleBELPreservesFollowingPromptText(t *testing.T) {
	var capturedTitle string
	vt := New(80, 24, 1000, nil)
	vt.SetTitleHandler(func(title string) {
		capturedTitle = title
	})

	if _, err := vt.Write([]byte("\x1b]2;termx-prompt\x07termx$ ")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if capturedTitle != "termx-prompt" {
		t.Fatalf("expected title termx-prompt, got %q", capturedTitle)
	}
	rendered := strings.Join(vt.RenderLines(), "\n")
	if !strings.Contains(rendered, "termx$") {
		t.Fatalf("expected prompt text to remain visible, got %q", rendered)
	}
}

func TestVTermLongTitleWithinParserBuffer(t *testing.T) {
	title := strings.Repeat("x", 32*1024)
	var capturedTitle string
	vt := New(80, 24, 1000, nil)
	vt.SetTitleHandler(func(title string) {
		capturedTitle = title
	})

	if _, err := vt.Write([]byte("\x1b]2;" + title + "\x07prompt")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if capturedTitle != title {
		t.Fatalf("expected long title to survive parser buffer, got len=%d want=%d", len(capturedTitle), len(title))
	}
	rendered := strings.Join(vt.RenderLines(), "\n")
	if !strings.Contains(rendered, "prompt") {
		t.Fatalf("expected prompt text after long title, got %q", rendered)
	}
}

func TestVTermWorkingDirectoryCallback(t *testing.T) {
	var captured string
	vt := New(80, 24, 1000, nil)
	vt.SetWorkingDirectoryHandler(func(path string) {
		captured = path
	})

	if _, err := vt.Write([]byte("\x1b]7;file://host/srv/app\x1b\\")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if captured != "file://host/srv/app" {
		t.Fatalf("expected working directory callback, got %q", captured)
	}
}

func TestVTermOSC8HyperlinkDoesNotLeakControlBytes(t *testing.T) {
	vt := New(80, 24, 1000, nil)

	if _, err := vt.Write([]byte("\x1b]8;id=termx;https://example.test\x1b\\linked\x1b]8;;\x1b\\ tail")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	row := vt.ScreenRowView(0)
	if got := rowToString(row); !strings.Contains(got, "linked tail") {
		t.Fatalf("expected hyperlink text to remain visible, got %q", got)
	}
	for i := 0; i < len("linked"); i++ {
		if row[i].LinkURL != "https://example.test" || row[i].LinkParams != "id=termx" {
			t.Fatalf("expected linked cell %d to keep hyperlink, got %#v", i, row[i])
		}
	}
	if row[len("linked")+1].LinkURL != "" || row[len("linked")+1].LinkParams != "" {
		t.Fatalf("expected trailing text to reset hyperlink, got %#v", row[len("linked")+1])
	}
}

func TestVTermUnsupportedPrivateModeDoesNotLeakControlBytes(t *testing.T) {
	vt := New(80, 24, 1000, nil)

	if _, err := vt.Write([]byte("\x1b[?9999hprompt\x1b[?9999l")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	rendered := strings.Join(vt.RenderLines(), "\n")
	if !strings.Contains(rendered, "prompt") {
		t.Fatalf("expected text after unsupported private mode to remain visible, got %q", rendered)
	}
	for _, leaked := range []string{"[?9999h", "[?9999l"} {
		if strings.Contains(rendered, leaked) {
			t.Fatalf("expected unsupported private mode bytes not to render, got %q", rendered)
		}
	}
}
