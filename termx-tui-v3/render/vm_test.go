package render

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestRenderVMBuilderUsesCopyModeOnlyWhenBoundToHistory(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       80,
			Rows: []state.HistoryRow{
				{Text: "old", LineID: 10},
				{Text: "new", LineID: 11},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  80,
			Cursor:     state.CopyPosition{Row: 1, Col: 2},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory {
		t.Fatalf("expected copy-history content, got %#v", content)
	}
	if len(content.Lines) < 4 || content.Lines[1].PlainString() != "● old" || content.Lines[2].PlainString() != "● new" {
		t.Fatalf("unexpected copy-history content lines %#v", content.Lines)
	}
	if len(content.HitRegions) != 2 || content.HitRegions[0].LineID != 10 || content.HitRegions[0].Rect.Y != 1 || content.HitRegions[1].Rect.Y != 2 {
		t.Fatalf("unexpected hit regions %#v", content.HitRegions)
	}
	if !vm.Shell.Cursor.Visible || vm.Shell.Cursor.Row != 2 || vm.Shell.Cursor.Col != 4 {
		t.Fatalf("expected copy cursor VM, got %#v", vm.Shell.Cursor)
	}
}

func TestRenderVMBuilderProjectsCopyHistoryContentRendererState(t *testing.T) {
	root := state.Root{
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       12,
			Rows: []state.HistoryRow{
				{Text: "alpha", LineID: 10, RowInLine: 0, ClippedStart: true},
				{Text: "beta好", LineID: 10, RowInLine: 1, ClippedEnd: true},
				{Text: "gamma", LineID: 11, RowInLine: 0},
			},
			Lines: []state.HistoryLineSpan{
				{LineID: 10, StartRow: 0, EndRow: 1, ClippedBefore: true, ClippedAfter: true},
				{LineID: 11, StartRow: 2, EndRow: 2},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  12,
			Cursor:     state.CopyPosition{Row: 1, Col: 4},
			Selection: &state.CopySelection{
				Anchor: state.CopyPosition{Row: 0, Col: 2},
				Focus:  state.CopyPosition{Row: 1, Col: 4},
			},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if got := content.Lines[0].PlainString(); !strings.Contains(got, "search") {
		t.Fatalf("expected copy search row, got %#v", content.Lines)
	}
	if got := content.Lines[1].PlainString(); !strings.Contains(got, "⇡ alpha") {
		t.Fatalf("expected clipped-before marker, got %#v", content.Lines)
	}
	if got := content.Lines[2].PlainString(); !strings.Contains(got, "╎ beta好 ⇣") {
		t.Fatalf("expected continuation and clipped-end markers, got %#v", content.Lines)
	}
	if !lineHasStyledCell(content.Lines[1], "pha", StyleAccent) || !lineHasStyledCell(content.Lines[2], "beta", StyleAccent) {
		t.Fatalf("expected selection rendered as styled cells, got %#v", content.Lines)
	}
	if !content.Cursor.Visible || content.Cursor.Row != 2 || content.Cursor.Col != 6 {
		t.Fatalf("expected cursor offset by copy marker, got %#v", content.Cursor)
	}
	if !strings.Contains(content.Status, "row 2/3 line:10 part:2 cols:12") || !strings.Contains(content.Status, "span:1-2") {
		t.Fatalf("expected position status, got %q", content.Status)
	}
	if len(content.HitRegions) != 3 || content.HitRegions[1].LineID != 10 || content.HitRegions[1].Rect.Y != 2 || content.HitRegions[1].Rect.W <= 12 {
		t.Fatalf("expected history hit regions with marker width, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderShowsPendingWithoutAuthoritativeHistory(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-history",
			Cols:       80,
			Rows:       []state.HistoryRow{{Text: "old", LineID: 10}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-copy",
			BoundCols:  80,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Pending {
		t.Fatalf("expected pending copy-history content, got %#v", content)
	}
	if len(content.Lines) != 1 || !strings.Contains(content.Lines[0].PlainString(), "stale history token") {
		t.Fatalf("copy mode must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderShowsPendingAfterCopyResizeInvalidation(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundCols:  98,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Pending {
		t.Fatalf("expected pending copy-history after resize invalidation, got %#v", content)
	}
	if len(content.Lines) != 1 || !strings.Contains(content.Lines[0].PlainString(), "authoritative history window pending") {
		t.Fatalf("copy mode resize pending must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderShowsCopyHistoryEmptyWithoutLiveFallback(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       80,
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  80,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Empty || content.Pending {
		t.Fatalf("expected empty copy-history content, got %#v", content)
	}
	if len(content.Lines) != 1 || content.Lines[0].PlainString() != "copy history empty" {
		t.Fatalf("copy mode empty must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderBuildsProductHeaderFooterSummaries(t *testing.T) {
	shell := state.DefaultShell().
		SetInteractionMode(state.InteractionModePane).
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志 🚀", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical)
	shell, _ = shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     state.PaneState{ID: "float-pane", Title: "浮窗", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 4, Y: 3, W: 30, H: 8},
	})
	root := state.Root{
		Shell: shell,
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Title:      "termx",
			Lines:      []string{"live"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	header := vm.Shell.Header
	if !header.Visible || header.Workspace != "main" || header.Tab != "[main]" || header.ActivePane != "pane-2" || header.TerminalSummary != "term:1" || header.FloatingSummary != "float:1" {
		t.Fatalf("unexpected product header %#v", header)
	}
	footer := vm.Shell.Footer
	if !footer.Visible || footer.Mode != "pane" || footer.ActiveTarget != "pane:日志 🚀 attached" || !containsString(footer.Actions, "v split") || !strings.Contains(footer.GlobalSummary, "panes:2") || !strings.Contains(footer.GlobalSummary, "float:1") {
		t.Fatalf("unexpected product footer %#v", footer)
	}
	if len(vm.Shell.Layout.Floating) != 1 || vm.Shell.Layout.Floating[0].Title != "浮窗" || !vm.Shell.Layout.Floating[0].Active {
		t.Fatalf("expected floating VM projection, got %#v", vm.Shell.Layout.Floating)
	}
}

func TestRenderVMBuilderAnchorsEmptyPaneCursorForIME(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "浮窗", Kind: state.PaneEmpty, Active: true}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentEmptyPane || !content.Cursor.Visible {
		t.Fatalf("empty pane should expose a cursor anchor for IME, got %#v", content)
	}
	if content.Cursor.Col != DisplayWidth("empty pane ")+DisplayWidth("浮窗") {
		t.Fatalf("empty pane cursor should follow title text, got %#v", content.Cursor)
	}
}

func TestRenderVMBuilderDimsTiledPaneWhenFloatingOwnsFocus(t *testing.T) {
	shell := state.DefaultShell()
	shell, result := shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     state.PaneState{ID: "float-pane", Title: "float", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 4, Y: 3, W: 30, H: 8},
	})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", result)
	}

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || vm.Shell.Layout.Panels[0].Active {
		t.Fatalf("active floating should dim tiled pane chrome, panels=%#v floating=%#v", vm.Shell.Layout.Panels, vm.Shell.Layout.Floating)
	}
	if len(vm.Shell.Layout.Floating) != 1 || !vm.Shell.Layout.Floating[0].Active {
		t.Fatalf("expected active floating VM, got %#v", vm.Shell.Layout.Floating)
	}

	shell, result = shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: "float-1"})
	if result.Status != state.FloatingCommandOK {
		t.Fatalf("close floating: %#v", result)
	}
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || !vm.Shell.Layout.Panels[0].Active {
		t.Fatalf("tiled pane active visual should restore after floating closes, got %#v", vm.Shell.Layout.Panels)
	}
}

func TestRenderVMBuilderProjectsTabStripAndWorkspaceSummary(t *testing.T) {
	shell := state.DefaultShell()
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"})
	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: "remote"})
	shell = shell.SetInteractionMode(state.InteractionModeWorkspace)

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Header.Workspace != "remote" || vm.Shell.Header.Tab != "[main]" {
		t.Fatalf("expected active workspace header, got %#v", vm.Shell.Header)
	}
	if vm.Shell.Footer.Mode != "workspace" || !containsString(vm.Shell.Footer.Actions, "t tree") || !strings.Contains(vm.Shell.Footer.GlobalSummary, "tabs:1") {
		t.Fatalf("expected workspace footer summary, got %#v", vm.Shell.Footer)
	}

	shell, _ = shell.ApplyWorkbenchCommand(state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspacePrevious})
	vm = NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Header.Workspace != "main" || vm.Shell.Header.Tab != "main [logs]" {
		t.Fatalf("expected original workspace tab strip, got %#v", vm.Shell.Header)
	}
}

func TestRenderVMBuilderDerivesFooterModePrecedence(t *testing.T) {
	builder := NewRenderVMBuilder()
	if got := builder.Build(state.Root{
		CopyMode: state.CopyModeStore{Active: true, TerminalID: "term-copy", BoundCols: 80},
	}).Shell.Footer.Mode; got != "copy" {
		t.Fatalf("expected copy footer mode, got %q", got)
	}
	if got := builder.Build(state.Root{
		Shell:    state.DefaultShell().OpenTerminalPicker().SetInteractionMode(state.InteractionModeGlobal),
		CopyMode: state.CopyModeStore{Active: true, TerminalID: "term-copy", BoundCols: 80},
	}).Shell.Footer.Mode; got != "terminal-picker" {
		t.Fatalf("overlay should win footer mode, got %q", got)
	}
	if got := builder.Build(state.Root{
		Shell: state.DefaultShell().SetInteractionMode(state.InteractionModeResize),
	}).Shell.Footer.Mode; got != "resize" {
		t.Fatalf("expected interaction footer mode, got %q", got)
	}
}

func TestRenderVMBuilderShowsCopyHistoryBindingErrorWithoutLiveFallback(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Lines:      []string{"live-row"},
		},
		History: state.HistoryStore{
			TerminalID: "term-history",
			Token:      "tok-1",
			Cols:       80,
			Rows:       []state.HistoryRow{{Text: "old", LineID: 10}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-copy",
			BoundToken: "tok-1",
			BoundCols:  80,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || content.Error == "" {
		t.Fatalf("expected copy-history binding error, got %#v", content)
	}
	if len(content.Lines) != 1 || !strings.Contains(content.Lines[0].PlainString(), "terminal mismatch") {
		t.Fatalf("copy mode error must not fallback to live rows, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderBuildsShellPanelToastAndOverlayVM(t *testing.T) {
	shell := state.DefaultShell().
		SetHeaderVisible(false).
		SetFooterVisible(false).
		SetPanelPresentation(state.PanelPresentationSplitLine).
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal).
		AddToast(state.ToastSpec{ID: "toast-1", Severity: state.ToastWarning, Title: "warn", Body: "body", Pending: true}).
		OpenTerminalPicker()
	root := state.Root{
		Shell: shell,
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Lines:      []string{"prompt"},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if vm.Shell.Header.Visible || vm.Shell.Footer.Visible {
		t.Fatalf("expected hidden header/footer, got header=%#v footer=%#v", vm.Shell.Header, vm.Shell.Footer)
	}
	if len(vm.Shell.Layout.Panels) != 2 {
		t.Fatalf("expected two panel VMs, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Layout.Panels[0].Presentation != PanelPresentationSplitLine || vm.Shell.Layout.Panels[1].Presentation != PanelPresentationSplitLine {
		t.Fatalf("expected split line presentation, got %#v", vm.Shell.Layout.Panels)
	}
	if !vm.Shell.Layout.Panels[1].Active || activeContent(vm.Shell).Kind != ContentTerminalLive {
		t.Fatalf("expected active live content in active panel, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Overlay.Kind != OverlayTerminalPicker || vm.Shell.Overlay.Content.Kind != ContentTerminalPicker {
		t.Fatalf("expected terminal picker overlay VM, got %#v", vm.Shell.Overlay)
	}
	if len(vm.Shell.Toasts) != 1 || vm.Shell.Toasts[0].Severity != ToastWarning || !vm.Shell.Toasts[0].Pending {
		t.Fatalf("expected warning toast VM, got %#v", vm.Shell.Toasts)
	}
}

func TestRenderVMBuilderProjectsZoomedPaneOnly(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal).
		ZoomPane(state.PaneCommandTarget{PaneID: "pane-2"})

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell, Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Lines: []string{"prompt"}}})
	if len(vm.Shell.Layout.Panels) != 1 {
		t.Fatalf("expected one zoomed panel, got %#v", vm.Shell.Layout.Panels)
	}
	if panel := vm.Shell.Layout.Panels[0]; panel.ID != "pane-2" || !panel.Active || panel.Content.Kind != ContentTerminalLive {
		t.Fatalf("unexpected zoomed panel projection %#v", panel)
	}
	if vm.Shell.Layout.Split.PaneID != "pane-2" || len(vm.Shell.Layout.Split.Children) != 0 {
		t.Fatalf("expected zoom split root to target pane, got %#v", vm.Shell.Layout.Split)
	}
}

func TestRenderVMBuilderDoesNotProjectClosedPane(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionHorizontal).
		ClosePane(state.PaneCommandTarget{PaneID: "pane-2"})

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if len(vm.Shell.Layout.Panels) != 1 || vm.Shell.Layout.Panels[0].ID != state.DefaultPaneID {
		t.Fatalf("expected closed pane removed from render VM, got %#v", vm.Shell.Layout.Panels)
	}
	if vm.Shell.Layout.Split.PaneID != state.DefaultPaneID {
		t.Fatalf("expected split tree pruned to default pane, got %#v", vm.Shell.Layout.Split)
	}
}

func TestRenderVMBuilderProjectsPaneGeometryHints(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical).
		SetPaneSize(state.PaneCommand{Target: state.PaneCommandTarget{PaneID: "pane-2"}, SizeMode: state.PaneSizeCells, Cols: 12})

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	if vm.Shell.Layout.Split.FixedPaneID != "pane-2" || vm.Shell.Layout.Split.FixedCols != 12 {
		t.Fatalf("expected fixed pane size hint in render VM, got %#v", vm.Shell.Layout.Split)
	}
}

func TestRenderVMBuilderUsesRootViewportAsLayoutTruth(t *testing.T) {
	root := state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 37, Rows: 13},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"prompt"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
			Cols:       120,
			Rows:       40,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if got, want := vm.Shell.Layout.Viewport, (Rect{W: 37, H: 13}); got != want {
		t.Fatalf("expected external viewport as layout truth got=%#v want=%#v", got, want)
	}
}

func TestRenderVMBuilderUsesLiveSurface(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Cols:       80,
			Rows:       24,
			Lines:      []string{"prompt", "output"},
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			Attached:   true,
			Cols:       80,
			Rows:       24,
			State:      state.TerminalLiveAttached,
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentTerminalLive || len(content.Lines) != 2 || content.Lines[0].PlainString() != "prompt" || content.Status != "live: term-live attached 80x24" {
		t.Fatalf("unexpected live content %#v", content)
	}
	if vm.Shell.Footer.ActiveTarget != "pane:shell attached" {
		t.Fatalf("expected attached footer active target, got %#v", vm.Shell.Footer)
	}
}

func TestRenderVMBuilderProjectsTerminalLiveANSIStyleCursorAndState(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Cols:       20,
			Rows:       5,
			Ready:      true,
			Lines:      []string{"\x1b[31mERR\x1b[0m ok \x1b[32m好🚀\x1b[0m"},
			Cursor:     state.LiveCursor{Visible: true, Row: 0, Col: 10, Shape: "bar"},
		},
		Session: state.TerminalSessionStore{TerminalID: "term-live", Attached: true, Cols: 20, Rows: 5},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentTerminalLive || len(content.Lines) != 1 {
		t.Fatalf("expected terminal-live content, got %#v", content)
	}
	if got := content.Lines[0].PlainString(); got != "ERR ok 好🚀" {
		t.Fatalf("expected sanitized live text, got %q", got)
	}
	if !lineHasStyledCell(content.Lines[0], "ERR", StyleDanger) || !lineHasStyledCell(content.Lines[0], "好🚀", StyleSuccess) {
		t.Fatalf("expected basic SGR mapped to styled cells, got %#v", content.Lines[0])
	}
	if !content.Cursor.Visible || content.Cursor.Row != 0 || content.Cursor.Col != 10 || content.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected live cursor projection, got %#v", content.Cursor)
	}

	pending := NewRenderVMBuilder().Build(state.Root{})
	if content := activeContent(pending.Shell); !content.Pending || content.Empty || content.Lines[0].PlainString() != "live surface pending" {
		t.Fatalf("expected pending live surface content, got %#v", content)
	}

	empty := NewRenderVMBuilder().Build(state.Root{Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true}})
	if content := activeContent(empty.Shell); !content.Empty || content.Pending || content.Lines[0].PlainString() != "live surface empty" {
		t.Fatalf("expected empty live surface content, got %#v", content)
	}
}

func TestRenderVMBuilderProjectsLiveExitLifecycle(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			State:      state.TerminalLiveExited,
			ExitCode:   0,
			ExitReason: "done",
		},
		Session: state.TerminalSessionStore{
			TerminalID: "term-live",
			State:      state.TerminalLiveExited,
			ExitCode:   0,
			ExitReason: "done",
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Kind != ContentTerminalLive || !content.Empty || content.Pending || content.Cursor.Visible {
		t.Fatalf("expected exited live content lifecycle, got %#v", content)
	}
	if content.Lines[0].PlainString() != "terminal exited: term-live code:0 done" || content.Status != "exited: term-live code:0 done" {
		t.Fatalf("unexpected exited live projection content=%#v status=%q", content.Lines, content.Status)
	}
	if vm.Shell.Footer.ActiveTarget != "pane:shell exited" {
		t.Fatalf("expected exited footer active target, got %#v", vm.Shell.Footer)
	}
}

func TestRenderVMBuilderRespectsActiveEmptyAndExitedPaneContent(t *testing.T) {
	emptyShell := state.DefaultShell()
	emptyShell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "slot", Kind: state.PaneEmpty, Active: true}
	emptyVM := NewRenderVMBuilder().Build(state.Root{
		Shell:   emptyShell,
		Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true, Lines: []string{"live must not replace empty"}},
	})
	if content := activeContent(emptyVM.Shell); content.Kind != ContentEmptyPane || !content.Empty || !strings.Contains(content.Lines[0].PlainString(), "empty pane slot") {
		t.Fatalf("expected active empty pane placeholder, got %#v", content)
	} else if !contentHasAction(content, "empty.attach") || !contentHasAction(content, "empty.create") || !contentHasAction(content, "empty.manager") || !contentHasAction(content, "empty.close") {
		t.Fatalf("expected empty pane CTA action regions, got %#v", content.HitRegions)
	}

	exitedShell := state.DefaultShell()
	exitedShell.Workspace.Tabs[0].Panes[0] = state.PaneState{ID: state.DefaultPaneID, Title: "old shell", Kind: state.PaneExited, TerminalID: "term-old", Active: true}
	exitedVM := NewRenderVMBuilder().Build(state.Root{
		Shell:   exitedShell,
		Surface: state.TerminalSurfaceStore{TerminalID: "term-live", Ready: true, Lines: []string{"live must not replace exited"}},
	})
	if content := activeContent(exitedVM.Shell); content.Kind != ContentExitedPane || content.Status != "exited: restart/reconnect/close" || !strings.Contains(content.Lines[0].PlainString(), "exited pane old shell") || !strings.Contains(content.Lines[1].PlainString(), "term-old") {
		t.Fatalf("expected active exited pane placeholder, got %#v", content)
	} else if !contentHasAction(content, "exited.restart") || !contentHasAction(content, "exited.reconnect") || !contentHasAction(content, "exited.close") {
		t.Fatalf("expected exited pane CTA action regions, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderProjectsTerminalPickerContentRenderer(t *testing.T) {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenTerminalPicker()
	shell.Overlay.Query = "term"

	vm := NewRenderVMBuilder().Build(state.Root{Shell: shell})
	content := vm.Shell.Overlay.Content
	if vm.Shell.Overlay.Kind != OverlayTerminalPicker || content.Kind != ContentTerminalPicker {
		t.Fatalf("expected terminal picker content, got %#v", vm.Shell.Overlay)
	}
	if !strings.Contains(content.Lines[0].PlainString(), "Search term") ||
		!strings.Contains(content.Lines[1].PlainString(), "Select terminal source state target") ||
		!strings.Contains(content.Lines[2].PlainString(), "▌ shell") ||
		!strings.Contains(content.Lines[3].PlainString(), "日志🚀") ||
		!strings.Contains(content.Lines[4].PlainString(), "DETAIL shell") ||
		!strings.Contains(content.Lines[len(content.Lines)-2].PlainString(), "[attach]  Attach Selected") ||
		!strings.Contains(content.Lines[len(content.Lines)-1].PlainString(), "[new]  New Shell") {
		t.Fatalf("expected picker search/list/detail/action rows, got %#v", content.Lines)
	}
	if strings.Contains(content.Lines[0].PlainString(), "filter terminals") || strings.Contains(content.Lines[4].PlainString(), "PREVIEW pane:") {
		t.Fatalf("picker must not regress to old engineering labels, got %#v", content.Lines)
	}
	if content.Status != "terminal picker: 2 items query:term" {
		t.Fatalf("expected picker item count status, got %q", content.Status)
	}
	if !content.Cursor.Visible || content.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected picker search cursor, got %#v", content.Cursor)
	}
	if !contentHasAction(content, "picker.attach") || !contentHasAction(content, "picker.new") {
		t.Fatalf("expected picker action hit regions, got %#v", content.HitRegions)
	}
}

func TestRenderVMBuilderFiltersTerminalPickerAndHighlightsSelectedRow(t *testing.T) {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenTerminalPicker().
		SetTerminalPickerQuery("日志")

	content := NewRenderVMBuilder().Build(state.Root{Shell: shell}).Shell.Overlay.Content
	if !strings.Contains(content.Lines[0].PlainString(), "Search 日志") ||
		!strings.Contains(content.Lines[2].PlainString(), "▌ 日志🚀") ||
		!strings.Contains(content.Lines[3].PlainString(), "DETAIL 日志🚀") ||
		strings.Contains(content.Lines[2].PlainString(), "shell") {
		t.Fatalf("expected filtered selected picker row, got %#v", content.Lines)
	}
	if !lineHasStyledCell(content.Lines[2], "日志🚀", StyleAccent) {
		t.Fatalf("expected selected picker row to use accent style, got %#v", content.Lines[2])
	}
	if content.Cursor.Col != DisplayWidth("Search 日志") {
		t.Fatalf("expected cursor after query text, got %#v", content.Cursor)
	}
	if content.Status != "terminal picker: 1 items query:日志" {
		t.Fatalf("expected query status, got %q", content.Status)
	}
}

func TestRenderVMBuilderProjectsTerminalPickerPoolStateAndRows(t *testing.T) {
	shell := state.DefaultShell().OpenTerminalPicker()
	root := state.Root{
		Shell: shell,
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{{
				TerminalID: "term-pool",
				Title:      "远程🚀",
				State:      "running",
			}},
		},
	}

	content := NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(content.Lines[2].PlainString(), "shell") ||
		!strings.Contains(content.Lines[3].PlainString(), "远程🚀") ||
		!strings.Contains(content.Lines[3].PlainString(), "pool") ||
		!strings.Contains(content.Lines[4].PlainString(), "DETAIL shell") {
		t.Fatalf("expected pane and pool rows, got %#v", content.Lines)
	}
	if len(content.HitRegions) < 4 || content.HitRegions[1].Row != 1 || content.HitRegions[1].PaneID != "" {
		t.Fatalf("expected pool row action region without pane id, got %#v", content.HitRegions)
	}

	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolLoading}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(content.Lines[2].PlainString(), "pool loading terminals") {
		t.Fatalf("expected loading row, got %#v", content.Lines)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolReady}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(content.Lines[2].PlainString(), "pool empty") {
		t.Fatalf("expected empty row, got %#v", content.Lines)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolError, LastError: "boom"}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !strings.Contains(content.Lines[2].PlainString(), "pool error boom") {
		t.Fatalf("expected error row, got %#v", content.Lines)
	}
}

func TestRenderVMBuilderProjectsTerminalPoolPage(t *testing.T) {
	shell := state.DefaultShell().OpenTerminalPool().SetTerminalPoolQuery("日志")
	root := state.Root{
		Shell: shell,
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{{
				TerminalID: "term-pool",
				Title:      "日志🚀",
				State:      "running",
				CWD:        "/tmp/日志",
				Cols:       120,
				Rows:       36,
				Tags:       map[string]string{"role": "shell"},
			}, {
				TerminalID: "term-other",
				Title:      "worker",
				State:      "exited",
			}},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := vm.Shell.Overlay.Content
	if vm.Shell.Overlay.Kind != OverlayTerminalPool || content.Kind != ContentTerminalPool {
		t.Fatalf("expected terminal pool content, got %#v", vm.Shell.Overlay)
	}
	if !strings.Contains(content.Lines[0].PlainString(), "Terminal Pool") ||
		!strings.Contains(content.Lines[1].PlainString(), "⌕ search 日志") ||
		!strings.Contains(content.Lines[2].PlainString(), "▌ 日志🚀") ||
		!strings.Contains(content.Lines[3].PlainString(), "DETAIL 日志🚀") ||
		!strings.Contains(content.Lines[4].PlainString(), "120x36") ||
		!strings.Contains(content.Lines[6].PlainString(), "role=shell") ||
		!strings.Contains(content.Lines[len(content.Lines)-1].PlainString(), "[kill]  Kill Terminal") {
		t.Fatalf("expected pool list/detail/preview/actions, got %#v", content.Lines)
	}
	if strings.Contains(content.Lines[2].PlainString(), "worker") {
		t.Fatalf("query should filter non-matching row, got %#v", content.Lines)
	}
	if !contentHasAction(content, "pool.select") || !contentHasAction(content, "pool.attach") || !contentHasAction(content, "pool.edit") || !contentHasAction(content, "pool.kill") {
		t.Fatalf("expected pool action hit regions, got %#v", content.HitRegions)
	}
	if !content.Cursor.Visible || content.Cursor.Col != DisplayWidth("⌕ search 日志") {
		t.Fatalf("expected pool search cursor, got %#v", content.Cursor)
	}

	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolLoading}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !content.Pending || !strings.Contains(content.Lines[2].PlainString(), "loading terminals") {
		t.Fatalf("expected pool loading page, got %#v", content)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolError, LastError: "boom"}
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if content.Error != "boom" || !strings.Contains(content.Lines[2].PlainString(), "list error boom") {
		t.Fatalf("expected pool error page, got %#v", content)
	}
	root.TerminalPool = state.TerminalPoolStore{Status: state.TerminalPoolReady}
	root.Shell = state.DefaultShell().OpenTerminalPool()
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !content.Empty || !strings.Contains(content.Lines[2].PlainString(), "list empty") {
		t.Fatalf("expected pool empty page, got %#v", content)
	}
}

func TestRenderVMBuilderProjectsWorkbenchTreeOverlay(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "日志🚀", Kind: state.PaneTerminalLive, TerminalID: "term-2"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}).
		OpenWorkbenchTree().
		SetWorkbenchTreeQuery("日志")
	root := state.Root{Shell: shell}

	vm := NewRenderVMBuilder().Build(root)
	content := vm.Shell.Overlay.Content
	if vm.Shell.Overlay.Kind != OverlayWorkbenchTree || !vm.Shell.Overlay.Opaque || content.Kind != ContentWorkbenchTree {
		t.Fatalf("expected workbench tree opaque overlay, got %#v", vm.Shell.Overlay)
	}
	if !strings.Contains(content.Lines[0].PlainString(), "Workbench Tree") ||
		!strings.Contains(content.Lines[1].PlainString(), "⌕ search 日志") ||
		!strings.Contains(content.Lines[2].PlainString(), "▌      pane  日志🚀") ||
		!strings.Contains(content.Lines[3].PlainString(), "DETAIL 日志🚀") ||
		!strings.Contains(content.Lines[len(content.Lines)-1].PlainString(), "[open]  Open / Focus") {
		t.Fatalf("expected tree header/search/row/detail/action, got %#v", content.Lines)
	}
	if !contentHasAction(content, "workbench.select") || !contentHasAction(content, "workbench.open") {
		t.Fatalf("expected workbench hit regions, got %#v", content.HitRegions)
	}
	if !content.Cursor.Visible || content.Cursor.Col != DisplayWidth("⌕ search 日志") {
		t.Fatalf("expected workbench search cursor, got %#v", content.Cursor)
	}

	root.Shell = state.DefaultShell().OpenWorkbenchTree().SetWorkbenchTreeQuery("missing")
	content = NewRenderVMBuilder().Build(root).Shell.Overlay.Content
	if !content.Empty || !strings.Contains(content.Lines[2].PlainString(), "no workbench node selected") {
		t.Fatalf("expected empty tree page, got %#v", content)
	}
}

func TestRenderVMBuilderProjectsPromptAndHelpOverlay(t *testing.T) {
	shell := state.DefaultShell().OpenPrompt(state.PromptState{
		Title:       "Rename Pane",
		Context:     "输入新名称",
		Value:       "日志🚀",
		Placeholder: "name",
	})
	content := NewRenderVMBuilder().Build(state.Root{Shell: shell}).Shell.Overlay.Content
	if content.Kind != ContentPrompt ||
		!strings.Contains(content.Lines[0].PlainString(), "Rename Pane") ||
		!strings.Contains(content.Lines[1].PlainString(), "输入新名称") ||
		!strings.Contains(content.Lines[2].PlainString(), "INPUT 日志🚀") ||
		!contentHasAction(content, "prompt.submit") ||
		!contentHasAction(content, "prompt.cancel") {
		t.Fatalf("expected prompt content, got %#v", content)
	}
	if !content.Cursor.Visible || content.Cursor.Col != DisplayWidth("INPUT 日志🚀") {
		t.Fatalf("expected prompt cursor after input, got %#v", content.Cursor)
	}

	content = NewRenderVMBuilder().Build(state.Root{Shell: state.DefaultShell().OpenHelp("most-used")}).Shell.Overlay.Content
	if content.Kind != ContentHelp ||
		!strings.Contains(content.Lines[0].PlainString(), "Help") ||
		!strings.Contains(content.Lines[1].PlainString(), "Most used") ||
		!strings.Contains(content.Lines[5].PlainString(), "Floating") ||
		!contentHasAction(content, "help.close") {
		t.Fatalf("expected help content, got %#v", content)
	}
	if content.Cursor.Visible {
		t.Fatalf("help overlay should not show input cursor, got %#v", content.Cursor)
	}
}

func TestRenderVMBuilderShowsLiveError(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{Err: "boom"},
	}

	vm := NewRenderVMBuilder().Build(root)
	content := activeContent(vm.Shell)
	if content.Status != "error: boom" || content.Error != "boom" {
		t.Fatalf("expected error content, got %#v", content)
	}
}

func TestRendererConsumesShellVMAndSanitizesContent(t *testing.T) {
	renderer := NewRenderer(DefaultTheme())
	result := renderer.RenderResult(RenderVM{
		Shell: ShellVM{
			Cursor: Cursor{Visible: true, Row: 2, Col: 3, Shape: CursorShapeBlock},
			Footer: FooterVM{Visible: true, Mode: "copy", Hint: "copy"},
			Layout: LayoutVM{Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "copy",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content: ContentVM{
					Kind:  ContentCopyHistory,
					Lines: []Line{NewLine("hello\nworld")},
				},
			}}},
		},
	})
	frame := result.Frame()

	if len(frame.Lines) != defaultHeight {
		t.Fatalf("expected default frame height %d, got %d", defaultHeight, len(frame.Lines))
	}
	if !frameContains(frame, "hello world") {
		t.Fatalf("expected sanitized content inside panel, got %#v", frame.Lines)
	}
	if !frameContains(frame, "copy") {
		t.Fatalf("expected styled status line to contain text, got %q", frame.Lines[1])
	}
	if result.Metadata.Height != defaultHeight || result.Metadata.Width != defaultWidth {
		t.Fatalf("expected render metadata, got %#v", result.Metadata)
	}
	if !result.Cursor.Visible || result.Cursor.Row != 2 || result.Cursor.Col != 3 {
		t.Fatalf("expected cursor passthrough, got %#v", result.Cursor)
	}
}

func frameContains(frame Frame, value string) bool {
	for _, line := range frame.Lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func lineHasStyledCell(line Line, text string, style StyleToken) bool {
	for _, cell := range line.Cells {
		if cell.Text == text && cell.Style == style {
			return true
		}
	}
	return false
}

func contentHasAction(content ContentVM, actionID string) bool {
	for _, region := range content.HitRegions {
		if region.Kind == HitRegionContentAction && region.ActionID == actionID {
			return true
		}
	}
	return false
}

func TestStyleHelpersWidthAndTruncateANSI(t *testing.T) {
	styled := StatusStyle(DefaultTheme()).Render("abcdef")
	if Width(styled) != 6 {
		t.Fatalf("expected ANSI-safe width 6, got %d", Width(styled))
	}
	if got := Truncate(styled, 3); Width(got) != 3 {
		t.Fatalf("expected truncated width 3, got width=%d value=%q", Width(got), got)
	}
	if got := SafeLine("a\nb"); got != "a b" {
		t.Fatalf("unexpected safe line %q", got)
	}
}

func TestWidthSafeHelpersKeepRowsAtTargetDisplayWidth(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
	}{
		{name: "cjk", value: "世界", width: 6},
		{name: "emoji", value: "ok 🚀", width: 8},
		{name: "combining", value: "e\u0301cho", width: 7},
		{name: "ansi", value: StatusStyle(DefaultTheme()).Render("warn 🚀"), width: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fitted := FitText(tc.value, tc.width)
			if got := DisplayWidth(fitted); got != tc.width {
				t.Fatalf("expected fitted width %d, got %d for %q", tc.width, got, fitted)
			}
			line := LineFromText(tc.value, tc.width)
			if got := line.Width(); got != tc.width {
				t.Fatalf("expected line width %d, got %d for %#v", tc.width, got, line)
			}
		})
	}
}

func TestWidthSafeTruncateDoesNotExceedTargetWidth(t *testing.T) {
	value := StatusStyle(DefaultTheme()).Render("title 🚀 世界")
	for width := 1; width <= 8; width++ {
		truncated := TruncateCells(value, width)
		if got := DisplayWidth(truncated); got > width {
			t.Fatalf("truncated width exceeds target width=%d got=%d value=%q", width, got, truncated)
		}
	}
}
