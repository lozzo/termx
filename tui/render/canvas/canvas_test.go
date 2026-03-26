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
