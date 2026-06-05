package termxtuiv3

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/render"
)

func TestModuleName(t *testing.T) {
	if ModuleName != "termx-tui-v3" {
		t.Fatalf("unexpected module name %q", ModuleName)
	}
}

func TestSmokeRun(t *testing.T) {
	frame, err := SmokeRun(context.Background())
	if err != nil {
		t.Fatalf("smoke run: %v", err)
	}
	if len(frame.Lines) == 0 || !frameContains(frame.Lines, "termx-tui-v3") {
		t.Fatalf("unexpected smoke frame %#v", frame)
	}
}

func TestSmokeRunDetailedCoversUIFramework(t *testing.T) {
	result, err := SmokeRunDetailed(context.Background())
	if err != nil {
		t.Fatalf("smoke run detailed: %v", err)
	}
	cases := smokeCasesByName(result)
	required := []string{"workbench-live", "split-hidden-toast", "terminal-picker", "copy-empty", "copy-history", "prompt-overlay", "help-overlay", "tab-workspace", "pane-command-flow", "visual-audit-current"}
	for _, name := range required {
		if len(cases[name].Lines) == 0 {
			t.Fatalf("missing smoke case %s in %#v", name, result.Cases)
		}
	}
	if !frameContains(cases["workbench-live"].Lines, "termx live 🚀") ||
		!frameContains(cases["workbench-live"].Lines, "你好 output") ||
		!frameContains(cases["workbench-live"].Lines, "┌─ shell") ||
		!frameContains(cases["workbench-live"].Lines, "[x]") {
		t.Fatalf("workbench live smoke missing shell/live content: %#v", cases["workbench-live"].Lines)
	}
	if !frameContains(cases["workbench-live"].ANSILines, "\x1b[1;38;2;88;213;201m") {
		t.Fatalf("workbench live smoke missing active pane accent ANSI: %#v", cases["workbench-live"].ANSILines)
	}
	if !frameContains(cases["workbench-live"].Lines, "ws:main") || !frameContains(cases["workbench-live"].Lines, "tab:[main]") || !frameContains(cases["workbench-live"].Lines, "mode:live") {
		t.Fatalf("workbench live smoke missing styled shell bar tokens: %#v", cases["workbench-live"].Lines)
	}
	if !frameContains(cases["workbench-live"].ANSILines, "\x1b[48;2;24;50;74m") {
		t.Fatalf("workbench live smoke missing shell bar background ANSI: %#v", cases["workbench-live"].ANSILines)
	}
	assertNoASCIIChrome(t, "workbench-live", cases["workbench-live"])
	assertContinuousCardPaneBorder(t, "workbench-live", cases["workbench-live"])
	if frameContains(cases["split-hidden-toast"].Lines, " ws:") ||
		frameContains(cases["split-hidden-toast"].Lines, " mode:") ||
		!frameContains(cases["split-hidden-toast"].Lines, "▌ warn 🚀 ...") ||
		!frameContains(cases["split-hidden-toast"].Lines, "warning  世界") {
		t.Fatalf("split hidden toast smoke invalid: %#v", cases["split-hidden-toast"].Lines)
	}
	if !frameContains(cases["split-hidden-toast"].Lines, "┌─ shell") ||
		!frameContains(cases["split-hidden-toast"].Lines, "… pending") ||
		!frameContains(cases["split-hidden-toast"].Lines, "┬─ logs") ||
		!frameContains(cases["split-hidden-toast"].Lines, "● active") ||
		render.SliceCells(cases["split-hidden-toast"].Lines[0], render.DisplayWidth(cases["split-hidden-toast"].Lines[0])-1, render.DisplayWidth(cases["split-hidden-toast"].Lines[0])) != "┐" {
		t.Fatalf("split hidden toast smoke should keep a complete split-line top boundary, got %#v", cases["split-hidden-toast"].Lines)
	}
	assertNoASCIIChrome(t, "split-hidden-toast", cases["split-hidden-toast"])
	if !frameContains(cases["terminal-picker"].Lines, "search [type to filter]") ||
		!frameContains(cases["terminal-picker"].Lines, "termx-picker") ||
		!frameContains(cases["terminal-picker"].Lines, "[new] new terminal") {
		t.Fatalf("terminal picker smoke missing product content: %#v", cases["terminal-picker"].Lines)
	}
	if !frameContains(cases["copy-empty"].Lines, "copy history empty") {
		t.Fatalf("copy empty smoke missing pending/empty content: %#v", cases["copy-empty"].Lines)
	}
	if !frameContains(cases["copy-history"].Lines, "termx-tui-v3") {
		t.Fatalf("copy history smoke missing authoritative row: %#v", cases["copy-history"].Lines)
	}
	if !frameContains(cases["prompt-overlay"].Lines, "Command Prompt") ||
		!frameContains(cases["prompt-overlay"].Lines, "重命名") ||
		!frameContains(cases["prompt-overlay"].Lines, "Submit") {
		t.Fatalf("prompt overlay smoke missing prompt content: %#v", cases["prompt-overlay"].Lines)
	}
	if !frameContains(cases["help-overlay"].Lines, "Help") ||
		!frameContains(cases["help-overlay"].Lines, "Most used") ||
		!frameContains(cases["help-overlay"].Lines, "Floating") ||
		!frameContains(cases["help-overlay"].Lines, "Terminal Pool") {
		t.Fatalf("help overlay smoke missing help content: %#v", cases["help-overlay"].Lines)
	}
	if !frameContains(cases["tab-workspace"].Lines, "ws:remote") ||
		!frameContains(cases["tab-workspace"].Lines, "tab:[main]") ||
		!frameContains(cases["tab-workspace"].Lines, "mode:workspace") ||
		!frameContains(cases["tab-workspace"].Lines, "workspace live") {
		t.Fatalf("tab/workspace smoke missing product entry content: %#v", cases["tab-workspace"].Lines)
	}
	if !frameContains(cases["pane-command-flow"].Lines, "pane.close") ||
		!frameContains(cases["pane-command-flow"].Lines, "pane command live") {
		t.Fatalf("pane command smoke missing close feedback or live content: %#v", cases["pane-command-flow"].Lines)
	}
	if !frameContains(cases["pane-command-flow"].ANSILines, "\x1b[1;38;2;88;213;201m") {
		t.Fatalf("pane command smoke missing styled active pane ANSI: %#v", cases["pane-command-flow"].ANSILines)
	}
	assertNoASCIIChrome(t, "pane-command-flow", cases["pane-command-flow"])
	if len(cases["visual-audit-current"].Lines) != 40 || render.DisplayWidth(cases["visual-audit-current"].Lines[0]) != 120 {
		t.Fatalf("visual audit smoke must use fixed 120x40 viewport, got lines=%d width=%d", len(cases["visual-audit-current"].Lines), render.DisplayWidth(cases["visual-audit-current"].Lines[0]))
	}
	if !frameContains(cases["visual-audit-current"].Lines, "visual audit baselin") ||
		!frameContains(cases["visual-audit-current"].Lines, "visual gap") ||
		!frameContains(cases["visual-audit-current"].Lines, "quick actions") ||
		!frameContains(cases["visual-audit-current"].Lines, "not tuiv2") {
		t.Fatalf("visual audit smoke missing fixed visual baseline markers: %#v", cases["visual-audit-current"].Lines)
	}
	if !frameContains(cases["visual-audit-current"].ANSILines, "\x1b[1;38;2;88;213;201m") {
		t.Fatalf("visual audit smoke missing active accent ANSI: %#v", cases["visual-audit-current"].ANSILines)
	}
	assertNoASCIIChrome(t, "visual-audit-current", cases["visual-audit-current"])
	for name, frame := range cases {
		assertSmokeWidth(t, name, frame)
	}
}

func smokeCasesByName(result SmokeResult) map[string]render.Frame {
	cases := make(map[string]render.Frame, len(result.Cases))
	for _, item := range result.Cases {
		cases[item.Name] = item.Frame
	}
	return cases
}

func assertSmokeWidth(t *testing.T, name string, frame render.Frame) {
	t.Helper()
	if len(frame.Lines) == 0 {
		t.Fatalf("smoke case %s produced empty frame", name)
	}
	width := render.DisplayWidth(frame.Lines[0])
	for row, line := range frame.Lines {
		if got := render.DisplayWidth(line); got != width {
			t.Fatalf("smoke case %s row %d width=%d want=%d line=%q", name, row, got, width, line)
		}
	}
}

func assertNoASCIIChrome(t *testing.T, name string, frame render.Frame) {
	t.Helper()
	for row, line := range frame.Lines {
		if strings.ContainsAny(line, "+|") {
			t.Fatalf("smoke case %s row %d contains ASCII chrome: %q", name, row, line)
		}
	}
}

func assertContinuousCardPaneBorder(t *testing.T, name string, frame render.Frame) {
	t.Helper()
	if len(frame.Lines) < 3 {
		t.Fatalf("smoke case %s too small for pane border: %#v", name, frame.Lines)
	}
	width := render.DisplayWidth(frame.Lines[0])
	for row := 1; row < len(frame.Lines)-1; row++ {
		left := render.SliceCells(frame.Lines[row], 0, 1)
		right := render.SliceCells(frame.Lines[row], width-1, width)
		if !isVerticalPaneBorderGlyph(left) || !isVerticalPaneBorderGlyph(right) {
			t.Fatalf("smoke case %s pane border discontinuity row=%d left=%q right=%q frame=%#v", name, row, left, right, frame.Lines)
		}
	}
}

func isVerticalPaneBorderGlyph(value string) bool {
	switch value {
	case "│", "┌", "┐", "└", "┘", "├", "┤", "┼":
		return true
	default:
		return false
	}
}

func frameContains(lines []string, value string) bool {
	for _, line := range lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}
