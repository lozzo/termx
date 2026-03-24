package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

type staticProgramRenderer struct {
	view string
}

func (r staticProgramRenderer) Render(_ types.AppState, _ []btui.Notice) string {
	return r.view
}

func TestBubbleteaProgramRunnerEntersAltScreenAndRendersView(t *testing.T) {
	model := btui.NewModel(btui.ModelConfig{
		InitialState: buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty),
		InitCmd:      tea.Quit,
		Renderer: staticProgramRenderer{
			view: "termx\nchrome_header:\nheader_bar: ws=main",
		},
	})

	var output bytes.Buffer
	err := (bubbleteaProgramRunner{}).Run(model, bytes.NewBuffer(nil), &output)
	if err != nil {
		t.Fatalf("expected program runner to succeed, got %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "\x1b[?1049h") {
		t.Fatalf("expected program runner to enter alt screen, got %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[?1049l") {
		t.Fatalf("expected program runner to leave alt screen, got %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[?1002h") || !strings.Contains(rendered, "\x1b[?1006h") {
		t.Fatalf("expected program runner to enable mouse cell motion, got %q", rendered)
	}
	if !strings.Contains(rendered, "header_bar: ws=main") {
		t.Fatalf("expected program runner to render model view, got %q", rendered)
	}
}
