package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestModelViewShowsProjectedState(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Name = "demo"
	rt.Registry().Get("term-1").State = "running"

	model := New(shared.Config{}, wb, rt)
	view := model.View()
	for _, want := range []string{"workspace: main", "tab tab-1 (tab 1)", "pane pane-1 title=shell terminal=term-1", "terminal term-1 name=demo state=running"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestModelUpdateOpenPickerSetsMode(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	updated, cmd := model.Update(input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: "req-1"})
	if updated != model {
		t.Fatal("expected model pointer to remain stable")
	}
	if cmd == nil {
		t.Fatal("expected command from open picker action")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %#v", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 batched commands, got %d", len(batch))
	}
	first := batch[0]()
	if _, ok := first.(EffectAppliedMsg); !ok {
		t.Fatalf("expected first batched msg to be EffectAppliedMsg, got %#v", first)
	}
	if model.input.Mode().Kind != input.ModePicker {
		t.Fatalf("expected picker mode, got %q", model.input.Mode().Kind)
	}
}

func TestModelUpdateWindowSizeAndError(t *testing.T) {
	model := New(shared.Config{}, workbench.NewWorkbench(), runtime.New(nil))
	_, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if model.width != 120 || model.height != 40 {
		t.Fatalf("unexpected size: %dx%d", model.width, model.height)
	}
	boom := errors.New("boom")
	_, _ = model.Update(boom)
	if !errors.Is(model.err, boom) {
		t.Fatalf("expected stored error, got %v", model.err)
	}
}
