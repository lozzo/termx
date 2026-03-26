package canvas

import (
	"strings"
	"testing"
)

func TestCanvasWriteLineAndString(t *testing.T) {
	c := New(12, 3)
	c.WriteLine(0, "termx")
	c.WriteLine(1, "live")

	out := c.String()
	if !strings.Contains(out, "termx") {
		t.Fatalf("expected output to contain termx, got %q", out)
	}
	if !strings.Contains(out, "live") {
		t.Fatalf("expected output to contain live, got %q", out)
	}
}

func TestCanvasDrawBoxAndWriteBlock(t *testing.T) {
	c := New(20, 6)
	c.DrawBox(0, 0, 20, 6, "demo")
	c.WriteBlock(2, 2, 10, 2, []string{"hello", "world"})

	out := c.String()
	for _, want := range []string{"┌", "┐", "└", "┘", "demo", "hello", "world"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
}
