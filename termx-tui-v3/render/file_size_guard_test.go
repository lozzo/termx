package render

import (
	"os"
	"strings"
	"testing"
)

func TestRendererSourceFilesStaySplitByResponsibility(t *testing.T) {
	limits := map[string]int{
		"framework.go":          140,
		"layout_plan.go":        340,
		"layout_hit_regions.go": 430,
		"canvas.go":             520,
		"shell_bar.go":          1220,
		"panel_chrome.go":       680,
		"overlay_chrome.go":     240,
	}
	for path, limit := range limits {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if lines := sourceLineCount(string(data)); lines > limit {
			t.Fatalf("%s grew to %d lines; split responsibilities before adding more renderer logic", path, lines)
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
