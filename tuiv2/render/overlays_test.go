package render

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestRenderPickerOverlayUsesCenteredCard(t *testing.T) {
	picker := &modal.PickerState{
		Title:    "Terminal Picker",
		Footer:   "[Enter] attach  [Esc] close",
		Selected: 0,
		Items:    []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}},
	}
	overlay := renderPickerOverlay(picker, TermSize{Width: 100, Height: 30})
	for _, want := range []string{"Terminal Picker", "search:", "term-1", "attach"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("overlay missing %q:\n%s", want, overlay)
		}
	}
}

func TestCompositeOverlayOverlaysCenteredCard(t *testing.T) {
	body := strings.Join([]string{"aaaa", "aaaa", "aaaa", "aaaa"}, "\n")
	overlay := strings.Join([]string{"    xx", "    yy"}, "\n")
	composited := compositeOverlay(body, overlay, TermSize{Width: 8, Height: 4})
	if !strings.Contains(composited, "xx") || !strings.Contains(composited, "yy") {
		t.Fatalf("composited body missing overlay:\n%s", composited)
	}
}

func TestRenderFrameWithPickerOverlay(t *testing.T) {
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
	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 100, 28), 100, 30)
	state = AttachPicker(state, &modal.PickerState{Title: "Terminal Picker", Items: []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}}})
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	for _, want := range []string{"main", "tab 1", "Terminal Picker"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
}

func TestRenderFrameWithWorkspacePickerOverlay(t *testing.T) {
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
	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 100, 28), 100, 30)
	state = AttachWorkspacePicker(state, &modal.WorkspacePickerState{
		Title: "Workspaces",
		Items: []modal.WorkspacePickerItem{{Name: "main"}, {Name: "dev"}},
	})
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	for _, want := range []string{"main", "tab 1", "Workspaces", "dev"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
}

func TestRenderFrameWithHelpOverlay(t *testing.T) {
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
	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 100, 28), 100, 30)
	state = AttachHelp(state, modal.DefaultHelp())
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	for _, want := range []string{"main", "tab 1", "Help", "Ctrl-P", "Ctrl-F", "Most Used"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
}

func TestRenderTerminalManagerOverlayShowsSelectedTerminalDetails(t *testing.T) {
	manager := &modal.TerminalManagerState{
		Title:    "Terminal Manager",
		Footer:   "[Enter] here  [Ctrl-T] tab  [Ctrl-O] float  [Ctrl-E] edit  [Ctrl-K] kill  [Esc] close",
		Selected: 0,
		Items: []modal.PickerItem{{
			TerminalID:  "term-1",
			Name:        "shell",
			State:       "visible",
			Command:     "bash -lc htop",
			Location:    "main/tab 1/pane-1",
			Observed:    true,
			Description: "running · 1 pane bound",
		}},
	}
	overlay := renderTerminalManagerOverlay(manager, TermSize{Width: 100, Height: 30})
	for _, want := range []string{"Terminal Manager", "term-1", "bash -lc htop", "main/tab 1/pane-1", "Ctrl-T", "Ctrl-O", "Ctrl-E"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("terminal manager overlay missing %q:\n%s", want, overlay)
		}
	}
}
