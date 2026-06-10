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
	required := []string{"workbench-live", "split-hidden-toast", "terminal-picker", "terminal-pool-page", "workbench-tree-page", "copy-empty", "copy-history", "prompt-overlay", "help-overlay", "tab-workspace", "pane-command-flow", "visual-audit-current"}
	for _, name := range required {
		if len(cases[name].Lines) == 0 {
			t.Fatalf("missing smoke case %s in %#v", name, result.Cases)
		}
	}
	if !frameContains(cases["workbench-live"].Lines, "termx live 🚀") ||
		!frameContains(cases["workbench-live"].Lines, "你好 output") ||
		!frameContains(cases["workbench-live"].Lines, "┌─ shell") {
		t.Fatalf("workbench live smoke missing shell/live content: %#v", cases["workbench-live"].Lines)
	}
	if !frameContains(cases["workbench-live"].Lines, "active") {
		t.Fatalf("workbench live smoke missing pane state slot: %#v", cases["workbench-live"].Lines)
	}
	if frameContains(cases["workbench-live"].Lines, "◆ owner") ||
		frameContains(cases["workbench-live"].Lines, "⇄2") ||
		frameContains(cases["workbench-live"].Lines, "1/31") {
		t.Fatalf("workbench live smoke should not render premature pane chrome tokens: %#v", cases["workbench-live"].Lines)
	}
	if !frameContains(cases["workbench-live"].ANSILines, "\x1b[1;38;2;169;112;255m") {
		t.Fatalf("workbench live smoke missing active pane accent ANSI: %#v", cases["workbench-live"].ANSILines)
	}
	if !frameContains(cases["workbench-live"].Lines, "  main") || !frameContains(cases["workbench-live"].Lines, "▎ 1 main ") || !frameContains(cases["workbench-live"].Lines, " ") || !frameContains(cases["workbench-live"].Lines, "[Ctrl] • [P] PANE") || !frameContains(cases["workbench-live"].Lines, "[G] GLOBAL") || !frameContains(cases["workbench-live"].Lines, "ws:main") {
		t.Fatalf("workbench live smoke missing styled shell bar tokens: %#v", cases["workbench-live"].Lines)
	}
	if !frameContains(cases["workbench-live"].ANSILines, "\x1b[48;2;8;8;13m") {
		t.Fatalf("workbench live smoke missing shell bar background ANSI: %#v", cases["workbench-live"].ANSILines)
	}
	assertNoASCIIChrome(t, "workbench-live", cases["workbench-live"])
	assertContinuousCardPaneBorder(t, "workbench-live", cases["workbench-live"])
	if frameContains(cases["split-hidden-toast"].Lines, " ws:") ||
		frameContains(cases["split-hidden-toast"].Lines, " mode:") ||
		!frameContains(cases["split-hidden-toast"].Lines, "│") ||
		!frameContains(cases["split-hidden-toast"].Lines, "warn 🚀 ...") ||
		frameContains(cases["split-hidden-toast"].Lines, "warning · pending") ||
		frameContains(cases["split-hidden-toast"].Lines, "世界") {
		t.Fatalf("split hidden toast smoke invalid: %#v", cases["split-hidden-toast"].Lines)
	}
	if !frameContains(cases["split-hidden-toast"].Lines, "┌─ shell") ||
		!frameContains(cases["split-hidden-toast"].Lines, "┬─ logs") ||
		render.SliceCells(cases["split-hidden-toast"].Lines[0], render.DisplayWidth(cases["split-hidden-toast"].Lines[0])-1, render.DisplayWidth(cases["split-hidden-toast"].Lines[0])) != "┐" {
		t.Fatalf("split hidden toast smoke should keep a complete split-line top boundary, got %#v", cases["split-hidden-toast"].Lines)
	}
	if frameContains(cases["split-hidden-toast"].Lines, " "+render.DefaultPaneChromeGlyphs().Running) ||
		frameContains(cases["split-hidden-toast"].Lines, "⇄2") ||
		frameContains(cases["split-hidden-toast"].Lines, "◆ owner") ||
		frameContains(cases["split-hidden-toast"].Lines, "1/31") {
		t.Fatalf("split hidden toast should not render premature pane chrome tokens: %#v", cases["split-hidden-toast"].Lines)
	}
	assertNoASCIIChrome(t, "split-hidden-toast", cases["split-hidden-toast"])
	if !frameContains(cases["terminal-picker"].Lines, "search:") ||
		!frameContains(cases["terminal-picker"].Lines, "termx-picker shell") ||
		!frameContains(cases["terminal-picker"].Lines, "running") ||
		!frameContains(cases["terminal-picker"].Lines, "80x24") ||
		!frameContains(cases["terminal-picker"].Lines, "+ new terminal") ||
		!frameContains(cases["terminal-picker"].Lines, "Create terminal") ||
		!frameContains(cases["terminal-picker"].Lines, "Attach here") ||
		frameContains(cases["terminal-picker"].Lines, "filter terminals") ||
		frameContains(cases["terminal-picker"].Lines, "Select terminal source state target") ||
		frameContains(cases["terminal-picker"].Lines, "DETAIL") ||
		frameContains(cases["terminal-picker"].Lines, "[attach]") ||
		frameContains(cases["terminal-picker"].Lines, "[new]") {
		t.Fatalf("terminal picker smoke missing product content: %#v", cases["terminal-picker"].Lines)
	}
	if !frameContains(cases["terminal-pool-page"].Lines, "Terminal Pool") ||
		!frameContains(cases["terminal-pool-page"].Lines, "⌕ search 日志") ||
		!frameContains(cases["terminal-pool-page"].Lines, "▌ 日志🚀") ||
		!frameContains(cases["terminal-pool-page"].Lines, "DETAIL 日志🚀") ||
		!frameContains(cases["terminal-pool-page"].Lines, "[attach]  Attach Here") ||
		!frameContains(cases["terminal-pool-page"].Lines, "[kill]  Kill") {
		t.Fatalf("terminal pool smoke missing page visual contract: %#v", cases["terminal-pool-page"].Lines)
	}
	if !frameContains(cases["workbench-tree-page"].Lines, "Workbench Tree") ||
		!frameContains(cases["workbench-tree-page"].Lines, "TUI storage projection") ||
		!frameContains(cases["workbench-tree-page"].Lines, "⌕ search 日志") ||
		!frameContains(cases["workbench-tree-page"].Lines, "▌      pane  日志🚀") ||
		!frameContains(cases["workbench-tree-page"].Lines, "DETAIL 日志🚀") ||
		!frameContains(cases["workbench-tree-page"].Lines, "[open]  Open") {
		t.Fatalf("workbench tree smoke missing page visual contract: %#v", cases["workbench-tree-page"].Lines)
	}
	if !frameContains(cases["copy-empty"].Lines, "copy history empty") {
		t.Fatalf("copy empty smoke missing pending/empty content: %#v", cases["copy-empty"].Lines)
	}
	if !frameContains(cases["copy-history"].Lines, "⌕ search [/ query]") ||
		!frameContains(cases["copy-history"].Lines, "termx-tui-v3") ||
		!frameContains(cases["copy-history"].Lines, "SCROLL") {
		t.Fatalf("copy history smoke missing authoritative row: %#v", cases["copy-history"].Lines)
	}
	if !frameContains(cases["prompt-overlay"].Lines, "Command Prompt") ||
		!frameContains(cases["prompt-overlay"].Lines, "重命名") ||
		frameContains(cases["prompt-overlay"].Lines, "[submit]  Submit") ||
		frameContains(cases["prompt-overlay"].Lines, "[cancel]  Cancel") {
		t.Fatalf("prompt overlay smoke missing prompt content: %#v", cases["prompt-overlay"].Lines)
	}
	if !frameContains(cases["help-overlay"].Lines, "Help") ||
		!frameContains(cases["help-overlay"].Lines, "Most used") ||
		!frameContains(cases["help-overlay"].Lines, "Floating") ||
		!frameContains(cases["help-overlay"].Lines, "Terminal Pool") {
		t.Fatalf("help overlay smoke missing help content: %#v", cases["help-overlay"].Lines)
	}
	if !frameContains(cases["tab-workspace"].Lines, " remote ") ||
		!frameContains(cases["tab-workspace"].Lines, "1 main ") ||
		!frameContains(cases["tab-workspace"].Lines, "WORKSPACE") ||
		!frameContains(cases["tab-workspace"].Lines, "workspace live") {
		t.Fatalf("tab/workspace smoke missing product entry content: %#v", cases["tab-workspace"].Lines)
	}
	if !frameContains(cases["pane-command-flow"].Lines, "pane.close") ||
		!frameContains(cases["pane-command-flow"].Lines, "pane command live") {
		t.Fatalf("pane command smoke missing close feedback or live content: %#v", cases["pane-command-flow"].Lines)
	}
	if !frameContains(cases["pane-command-flow"].ANSILines, "\x1b[1;38;2;169;112;255m") {
		t.Fatalf("pane command smoke missing styled active pane ANSI: %#v", cases["pane-command-flow"].ANSILines)
	}
	assertNoASCIIChrome(t, "pane-command-flow", cases["pane-command-flow"])
	if len(cases["visual-audit-current"].Lines) != 40 || render.DisplayWidth(cases["visual-audit-current"].Lines[0]) != 140 {
		t.Fatalf("visual audit smoke must use fixed 140x40 viewport, got lines=%d width=%d", len(cases["visual-audit-current"].Lines), render.DisplayWidth(cases["visual-audit-current"].Lines[0]))
	}
	if !frameContains(cases["visual-audit-current"].Lines, "visual review") ||
		!frameContains(cases["visual-audit-current"].Lines, "visual review") ||
		!frameContains(cases["visual-audit-current"].Lines, "[]─[]") {
		t.Fatalf("visual review smoke missing fixed visual markers: %#v", cases["visual-audit-current"].Lines)
	}
	if frameContains(cases["visual-audit-current"].Lines, "⇄2") ||
		frameContains(cases["visual-audit-current"].Lines, "◆ owner") {
		t.Fatalf("visual review smoke should not render premature pane chrome tokens: %#v", cases["visual-audit-current"].Lines)
	}
	if frameContains(cases["visual-audit-current"].Lines, "visual acceptance") {
		t.Fatalf("visual review smoke must not claim acceptance: %#v", cases["visual-audit-current"].Lines)
	}
	if !frameContains(cases["visual-audit-current"].ANSILines, "\x1b[1;38;2;169;112;255m") {
		t.Fatalf("visual audit smoke missing active accent ANSI: %#v", cases["visual-audit-current"].ANSILines)
	}
	assertDefaultVisualReviewChrome(t, cases)
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
		if strings.Contains(line, "|") || strings.Contains(line, "---") {
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
		if !isPaneBorderGlyph(left) || !isRightPaneBorderOrOverflowGlyph(right) {
			t.Fatalf("smoke case %s pane border discontinuity row=%d left=%q right=%q frame=%#v", name, row, left, right, frame.Lines)
		}
	}
}

func assertDefaultVisualReviewChrome(t *testing.T, cases map[string]render.Frame) {
	t.Helper()
	review := cases["visual-audit-current"]
	requiredReview := []string{"  main", "1 main  ▎ 2 logs  ", "visual review", "┌───────────[]─[]─[]─[]─┐", "unconnected", "└──────────────────────────v┘", "[Ctrl] • [P] PANE", "[W] WORKSPACE", "[V] COPY", "[G] GLOBAL", "ws:main float:1 terminals:1"}
	for _, marker := range requiredReview {
		if !frameContains(review.Lines, marker) {
			t.Fatalf("visual review smoke missing chrome marker %q: %#v", marker, review.Lines)
		}
	}
	requiredOverlays := map[string][]string{
		"terminal-picker":     {"┌─ terminal picker", "search:", "termx-picker shell", "running", "80x24"},
		"terminal-pool-page":  {"┌─ terminal pool", "● open", "esc", "Terminal Pool", "⌕ search 日志", "DETAIL 日志🚀", "[kill]  Kill"},
		"workbench-tree-page": {"┌─ workbench tree", "● open", "esc", "Workbench Tree", "TUI storage projection", "⌕ search 日志", "DETAIL 日志🚀", "[open]  Open"},
		"prompt-overlay":      {"┌─ prompt", "Command Prompt", "重命名"},
		"help-overlay":        {"┌─ help", "● open", "esc", "Most used", "Terminal Pool"},
	}
	for name, markers := range requiredOverlays {
		frame := cases[name]
		for _, marker := range markers {
			if !frameContains(frame.Lines, marker) {
				t.Fatalf("smoke case %s missing overlay visual marker %q: %#v", name, marker, frame.Lines)
			}
		}
	}
	picker := cases["terminal-picker"]
	for _, stale := range []string{"filter terminals", "PREVIEW pane:", "Select terminal source state target", "DETAIL", "[attach]", "[new]", "pane:", "selected ", "● open"} {
		if frameContains(picker.Lines, stale) {
			t.Fatalf("terminal picker regressed to engineering label %q: %#v", stale, picker.Lines)
		}
	}
	prompt := cases["prompt-overlay"]
	for _, stale := range []string{"● open", "esc"} {
		if frameContains(prompt.Lines, stale) {
			t.Fatalf("prompt overlay regressed to engineering label %q: %#v", stale, prompt.Lines)
		}
	}
	split := cases["split-hidden-toast"]
	for _, marker := range []string{"┌─ shell", "┬─ logs", "┴", "warn 🚀"} {
		if !frameContains(split.Lines, marker) {
			t.Fatalf("split hidden toast missing styled split marker %q: %#v", marker, split.Lines)
		}
	}
	for _, stale := range []string{"⇄2", "◆ owner", "1/31", " " + render.DefaultPaneChromeGlyphs().Running} {
		if frameContains(split.Lines, stale) {
			t.Fatalf("split hidden toast rendered premature pane chrome marker %q: %#v", stale, split.Lines)
		}
	}
}

func paneChromeFullActionMarker() string {
	glyphs := render.DefaultPaneChromeGlyphs()
	return "[" + glyphs.SplitHorizontal + "]─[" + glyphs.SplitVertical + "]─[" + glyphs.Zoom + "]─[" + glyphs.Close + "]"
}

func paneChromeCompactActionMarker() string {
	glyphs := render.DefaultPaneChromeGlyphs()
	return "[" + glyphs.Zoom + "]─[" + glyphs.Close + "]"
}

func paneChromeCloseActionMarker() string {
	return "[" + render.DefaultPaneChromeGlyphs().Close + "]"
}

func isPaneBorderGlyph(value string) bool {
	switch value {
	case "│", "┌", "┐", "└", "┘", "├", "┤", "┼":
		return true
	default:
		return false
	}
}

func isRightPaneBorderOrOverflowGlyph(value string) bool {
	return isPaneBorderGlyph(value) || value == ">"
}

func frameContains(lines []string, value string) bool {
	for _, line := range lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}
