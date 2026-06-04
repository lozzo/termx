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
	required := []string{"workbench-live", "split-hidden-toast", "terminal-picker", "copy-empty", "copy-history"}
	for _, name := range required {
		if len(cases[name].Lines) == 0 {
			t.Fatalf("missing smoke case %s in %#v", name, result.Cases)
		}
	}
	if !frameContains(cases["workbench-live"].Lines, "termx live 🚀") ||
		!frameContains(cases["workbench-live"].Lines, "你好 output") ||
		!frameContains(cases["workbench-live"].Lines, "╭─ shell active") {
		t.Fatalf("workbench live smoke missing shell/live content: %#v", cases["workbench-live"].Lines)
	}
	assertNoASCIIChrome(t, "workbench-live", cases["workbench-live"])
	if frameContains(cases["split-hidden-toast"].Lines, " main ") ||
		frameContains(cases["split-hidden-toast"].Lines, " live ") ||
		!frameContains(cases["split-hidden-toast"].Lines, "[warning] warn 🚀 ... 世界") {
		t.Fatalf("split hidden toast smoke invalid: %#v", cases["split-hidden-toast"].Lines)
	}
	assertNoASCIIChrome(t, "split-hidden-toast", cases["split-hidden-toast"])
	if !frameContains(cases["terminal-picker"].Lines, "terminal picker pending") {
		t.Fatalf("terminal picker smoke missing placeholder: %#v", cases["terminal-picker"].Lines)
	}
	if !frameContains(cases["copy-empty"].Lines, "copy history empty") {
		t.Fatalf("copy empty smoke missing pending/empty content: %#v", cases["copy-empty"].Lines)
	}
	if !frameContains(cases["copy-history"].Lines, "termx-tui-v3") {
		t.Fatalf("copy history smoke missing authoritative row: %#v", cases["copy-history"].Lines)
	}
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

func frameContains(lines []string, value string) bool {
	for _, line := range lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}
