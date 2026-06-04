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
		},
	}

	vm := NewRenderVMBuilder().Build(root)
	if vm.Mode != ModeCopy {
		t.Fatalf("expected copy mode vm, got %q", vm.Mode)
	}
	if len(vm.Lines) != 2 || vm.Lines[0] != "old" || vm.Lines[1] != "new" {
		t.Fatalf("unexpected vm lines %v", vm.Lines)
	}
	if len(vm.HitRegions) != 2 || vm.HitRegions[0].LineID != 10 || vm.HitRegions[1].Rect.Y != 1 {
		t.Fatalf("unexpected hit regions %#v", vm.HitRegions)
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
	if vm.Mode != ModeLive {
		t.Fatalf("expected live fallback placeholder, got %q", vm.Mode)
	}
	if len(vm.Lines) != 1 || vm.Lines[0] != "live-row" {
		t.Fatalf("unexpected fallback lines %v", vm.Lines)
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
		Mode:   ModeCopy,
		Lines:  []string{"hello\nworld"},
		Status: "copy",
	})
	frame := result.Frame()

	if len(frame.Lines) != 2 {
		t.Fatalf("expected content and status line, got %v", frame.Lines)
	}
	if frame.Lines[0] != "hello world" {
		t.Fatalf("expected sanitized line, got %q", frame.Lines[0])
	}
	if !strings.Contains(frame.Lines[1], "copy") {
		t.Fatalf("expected styled status line to contain text, got %q", frame.Lines[1])
	}
	if result.Metadata.Height != 2 || result.Metadata.Width == 0 {
		t.Fatalf("expected render metadata, got %#v", result.Metadata)
	}
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
