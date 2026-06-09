package state

import (
	"os"
	"strings"
	"testing"
)

func TestShellSourceFilesStaySplitByResponsibility(t *testing.T) {
	limits := map[string]int{
		"shell.go":            540,
		"shell_workbench.go":  660,
		"shell_pane_tree.go":  860,
		"shell_overlay.go":    420,
		"shell_floating.go":   340,
		"shell_projection.go": 340,
		"shell_workspace.go":  140,
		"shell_clone.go":      120,
	}
	for path, limit := range limits {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if lines := sourceLineCount(string(data)); lines > limit {
			t.Fatalf("%s grew to %d lines; split responsibilities before adding more shell logic", path, lines)
		}
	}
}

func sourceLineCount(source string) int {
	source = strings.TrimSuffix(source, "\n")
	if source == "" {
		return 0
	}
	return strings.Count(source, "\n") + 1
}
