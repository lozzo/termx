package chrome

import (
	"strings"
	"testing"
)

func TestFrameIncludesTitleAndMeta(t *testing.T) {
	view := Frame("shell-dev", "running  owner", 30, []string{"body"})
	if !strings.Contains(view, "shell-dev") || !strings.Contains(view, "owner") {
		t.Fatalf("expected title/meta in frame, got %q", view)
	}
}
