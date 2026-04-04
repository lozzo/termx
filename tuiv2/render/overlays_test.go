package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
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
	overlay := xansi.Strip(renderPickerOverlay(picker, TermSize{Width: 100, Height: 30}))
	for _, want := range []string{"Terminal Picker", "search:", "> ○ term-1 shell  running"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("overlay missing %q:\n%s", want, overlay)
		}
	}
	for _, want := range []string{"[Enter] attach", "[Tab] split+attach"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("expected picker footer action %q:\n%s", want, overlay)
		}
	}
}

func TestRenderWorkspacePickerOverlayDoesNotAddTerminalIndicator(t *testing.T) {
	picker := &modal.WorkspacePickerState{
		Title:    "Workspaces",
		Selected: 0,
		Items:    []modal.WorkspacePickerItem{{Name: "main", Description: "1 tab(s), 1 pane(s)"}},
	}
	overlay := xansi.Strip(renderWorkspacePickerOverlay(picker, TermSize{Width: 100, Height: 30}))
	if strings.Contains(overlay, "> main") {
		t.Fatalf("workspace picker should not reuse terminal indicator:\n%s", overlay)
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
	state = WithStatus(state, "", "", string(input.ModePicker))
	state = AttachPicker(state, &modal.PickerState{Title: "Terminal Picker", Items: []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}}})
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	for _, want := range []string{"main", "tab 1", "Terminal Picker"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
	for _, want := range []string{"PICKER", "UP/DOWN MOVE", "Enter HERE"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected picker shortcuts in unified status bar; missing %q:\n%s", want, frame)
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
	state = WithStatus(state, "", "", string(input.ModeWorkspacePicker))
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
	for _, want := range []string{"WORKSPACE-PICKER", "UP/DOWN MOVE", "TYPE FILTER", "Enter OPEN", "Esc BACK"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expected workspace picker shortcuts in unified status bar; missing %q:\n%s", want, frame)
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
	state = WithStatus(state, "", "", string(input.ModeHelp))
	state = AttachHelp(state, modal.DefaultHelp())
	frame := NewCoordinator(func() VisibleRenderState { return state }).RenderFrame()
	for _, want := range []string{"main", "tab 1", "Help", "Ctrl-P", "Ctrl-F", "Most Used"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
	if !strings.Contains(frame, "Esc BACK") {
		t.Fatalf("expected help shortcut in unified status bar:\n%s", frame)
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
	for _, want := range []string{"Terminal Manager", "term-1", "bash -lc htop", "main/tab 1/pane-1"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("terminal manager overlay missing %q:\n%s", want, overlay)
		}
	}
	for _, want := range []string{"[Enter] here", "[Ctrl-T] tab", "[Ctrl-O] float"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("expected terminal manager footer action %q:\n%s", want, overlay)
		}
	}
}

func TestPromptValueWithCursorRespectsExplicitPosition(t *testing.T) {
	prompt := &modal.PromptState{Value: "shell"}
	prompt.Cursor = 0
	if got, want := promptValueWithCursor(prompt), "_shell"; got != want {
		t.Fatalf("cursor at start got %q, want %q", got, want)
	}
	prompt.Cursor = 2
	if got, want := promptValueWithCursor(prompt), "sh_ell"; got != want {
		t.Fatalf("cursor in middle got %q, want %q", got, want)
	}
	prompt.Cursor = -1
	if got, want := promptValueWithCursor(prompt), "shell_"; got != want {
		t.Fatalf("negative cursor should fallback to end, got %q, want %q", got, want)
	}
}

func TestLayoutOverlayFooterActionsClipKeepsStablePrefixOrder(t *testing.T) {
	specs := pickerFooterActionSpecs()
	if len(specs) < 3 {
		t.Fatalf("expected at least three picker footer specs, got %#v", specs)
	}
	baseRect := workbench.Rect{X: 0, Y: 0, W: 200, H: 1}
	prefixLine, prefixLayouts := layoutOverlayFooterActions(specs[:2], baseRect)
	if len(prefixLayouts) != 2 {
		t.Fatalf("expected two prefix layouts, got %#v", prefixLayouts)
	}
	clipWidth := prefixLayouts[1].Rect.X + prefixLayouts[1].Rect.W
	line, layouts := layoutOverlayFooterActions(specs, workbench.Rect{X: 0, Y: 0, W: clipWidth, H: 1})
	if len(layouts) != 2 {
		t.Fatalf("expected clipped prefix of two actions, got %#v", layouts)
	}
	if line != prefixLine {
		t.Fatalf("expected clipped line %q, got %q", prefixLine, line)
	}
	for index, layout := range layouts {
		if layout.Action.Kind != specs[index].Action.Kind {
			t.Fatalf("clipped action[%d]=%q, want %q", index, layout.Action.Kind, specs[index].Action.Kind)
		}
	}
}

func TestWorkspacePickerFooterActionSpecsOrderStable(t *testing.T) {
	specs := workspacePickerFooterActionSpecs()
	want := []input.ActionKind{
		input.ActionSubmitPrompt,
		input.ActionCreateWorkspace,
		input.ActionRenameWorkspace,
		input.ActionDeleteWorkspace,
		input.ActionPrevWorkspace,
		input.ActionNextWorkspace,
		input.ActionCancelMode,
	}
	if len(specs) != len(want) {
		t.Fatalf("workspace footer action count=%d, want %d", len(specs), len(want))
	}
	for index, spec := range specs {
		if spec.Action.Kind != want[index] {
			t.Fatalf("workspace footer action[%d]=%q, want %q", index, spec.Action.Kind, want[index])
		}
	}
}

func TestPickerQueryRowRectTracksEditableFieldAfterSearchPrefix(t *testing.T) {
	layout := buildPickerCardLayout(100, 28, 4, true)
	rect := pickerQueryRowRect(layout)
	prefixW := xansi.StringWidth("search: ")
	want := workbench.Rect{
		X: layout.cardX + 1 + prefixW,
		Y: layout.cardY + 2,
		W: maxInt(1, layout.innerWidth-prefixW),
		H: 1,
	}
	if rect != want {
		t.Fatalf("picker query rect=%#v, want %#v", rect, want)
	}
}

func TestPromptInputRectTracksEditableFieldAfterPromptPrefix(t *testing.T) {
	layout := buildPickerCardLayout(100, 28, 5, true)
	prompt := &modal.PromptState{Kind: "create-terminal-tags"}
	inputLine := 3
	rect := promptInputRect(layout, prompt, inputLine)
	prefixW := xansi.StringWidth(promptFieldLabel(prompt.Kind) + ": ")
	want := workbench.Rect{
		X: layout.cardX + 1 + prefixW,
		Y: layout.firstItemY + inputLine,
		W: maxInt(1, layout.innerWidth-prefixW),
		H: 1,
	}
	if rect != want {
		t.Fatalf("prompt input rect=%#v, want %#v", rect, want)
	}
}
