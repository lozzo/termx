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
	if vm.Mode != ModeCopy {
		t.Fatalf("expected copy mode vm, got %q", vm.Mode)
	}
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory {
		t.Fatalf("expected copy-history content, got %#v", content)
	}
	if len(vm.Lines) != 2 || vm.Lines[0] != "old" || vm.Lines[1] != "new" {
		t.Fatalf("unexpected vm lines %v", vm.Lines)
	}
	if len(content.HitRegions) != 2 || content.HitRegions[0].LineID != 10 || content.HitRegions[1].Rect.Y != 1 {
		t.Fatalf("unexpected hit regions %#v", content.HitRegions)
	}
	if !vm.Shell.Cursor.Visible || vm.Shell.Cursor.Row != 1 || vm.Shell.Cursor.Col != 2 {
		t.Fatalf("expected copy cursor VM, got %#v", vm.Shell.Cursor)
	}
}

func TestRenderVMBuilderDoesNotFallbackWithoutAuthoritativeHistory(t *testing.T) {
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
	if vm.Mode != ModeCopy {
		t.Fatalf("expected copy pending vm, got %q", vm.Mode)
	}
	content := activeContent(vm.Shell)
	if content.Kind != ContentCopyHistory || !content.Pending {
		t.Fatalf("expected pending copy-history content, got %#v", content)
	}
	if len(vm.Lines) != 1 || !strings.Contains(vm.Lines[0], "stale history token") {
		t.Fatalf("copy mode must not fallback to live rows, got %v", vm.Lines)
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
	if len(vm.Lines) != 1 || vm.Lines[0] != "copy history empty" {
		t.Fatalf("copy mode empty must not fallback to live rows, got %v", vm.Lines)
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
	if len(vm.Lines) != 1 || !strings.Contains(vm.Lines[0], "terminal mismatch") {
		t.Fatalf("copy mode error must not fallback to live rows, got %v", vm.Lines)
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

func TestRenderVMBuilderUsesLiveSurface(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-live",
			Lines:      []string{"prompt", "output"},
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if vm.Mode != ModeLive {
		t.Fatalf("expected live vm, got %q", vm.Mode)
	}
	if len(vm.Lines) != 2 || vm.Lines[0] != "prompt" || vm.Status != "live: term-live" {
		t.Fatalf("unexpected live vm %#v", vm)
	}
}

func TestRenderVMBuilderShowsLiveError(t *testing.T) {
	root := state.Root{
		Surface: state.TerminalSurfaceStore{Err: "boom"},
	}

	vm := NewRenderVMBuilder().Build(root)
	if vm.Status != "error: boom" {
		t.Fatalf("expected error status, got %q", vm.Status)
	}
}

func TestRendererConsumesVMAndSanitizesLines(t *testing.T) {
	renderer := NewRenderer(DefaultTheme())
	result := renderer.RenderResult(RenderVM{
		Shell:  ShellVM{Cursor: Cursor{Visible: true, Row: 2, Col: 3, Shape: CursorShapeBlock}},
		Mode:   ModeCopy,
		Lines:  []string{"hello\nworld"},
		Status: "copy",
	})
	frame := result.Frame()

	if len(frame.Lines) != minFrameHeight {
		t.Fatalf("expected framework minimum frame height %d, got %d", minFrameHeight, len(frame.Lines))
	}
	if !frameContains(frame, "hello world") {
		t.Fatalf("expected sanitized content inside panel, got %#v", frame.Lines)
	}
	if !frameContains(frame, "copy") {
		t.Fatalf("expected styled status line to contain text, got %q", frame.Lines[1])
	}
	if result.Metadata.Height != minFrameHeight || result.Metadata.Width != defaultWidth {
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
