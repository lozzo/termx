package termxtuiv3

import (
	"context"
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
	if len(frame.Lines) == 0 || frame.Lines[0] != "termx-tui-v3" {
		t.Fatalf("unexpected smoke frame %#v", frame)
	}
}
