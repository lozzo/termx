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
	if len(vm.Lines) != 1 || vm.Lines[0] != "live surface pending" {
		t.Fatalf("unexpected fallback lines %v", vm.Lines)
	}
}

func TestRendererConsumesVMAndSanitizesLines(t *testing.T) {
	frame := NewRenderer(DefaultTheme()).Render(RenderVM{
		Mode:   ModeCopy,
		Lines:  []string{"hello\nworld"},
		Status: "copy",
	})

	if len(frame.Lines) != 2 {
		t.Fatalf("expected content and status line, got %v", frame.Lines)
	}
	if frame.Lines[0] != "hello world" {
		t.Fatalf("expected sanitized line, got %q", frame.Lines[0])
	}
	if !strings.Contains(frame.Lines[1], "copy") {
		t.Fatalf("expected styled status line to contain text, got %q", frame.Lines[1])
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
