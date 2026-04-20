package uiinput

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestRenderShowsCompletionImmediatelyAfterCursor(t *testing.T) {
	state := FromLegacy("/tmp/al", len([]rune("/tmp/al")), true, "")
	state.SetCompletion("pha/")
	rendered := state.Render(RenderConfig{
		Prompt: "  workdir: ",
		Width:  32,
	})
	if !strings.Contains(rendered, "/tmp/alpha/") {
		t.Fatalf("expected inline completion in rendered input, got %q", rendered)
	}
}

func TestRenderClipsCompletionToAvailableWidth(t *testing.T) {
	state := FromLegacy("/tmp/al", len([]rune("/tmp/al")), true, "")
	state.SetCompletion("phabetical/")
	rendered := state.Render(RenderConfig{
		Prompt: "  workdir: ",
		Width:  10,
	})
	if got := strings.TrimPrefix(rendered, "  workdir: "); len([]rune(got)) != 10 {
		t.Fatalf("expected clipped visible width 10, got %q", got)
	}
}

func TestHandleKeyAcceptsSpace(t *testing.T) {
	state := FromLegacy("foo", len([]rune("foo")), true, "")
	if !state.HandleKey(tea.KeyMsg{Type: tea.KeySpace}) {
		t.Fatal("expected space key to mutate input")
	}
	if got, want := state.Value(), "foo "; got != want {
		t.Fatalf("value after space = %q, want %q", got, want)
	}
}
