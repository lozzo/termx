package termxtuiv3

import (
	"context"
	"strings"
	"testing"
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

func frameContains(lines []string, value string) bool {
	for _, line := range lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}
