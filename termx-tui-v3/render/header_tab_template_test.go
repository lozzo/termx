package render

import (
	"strings"
	"testing"
)

func TestHeaderTabDefaultSegmentsKeepStableWidth(t *testing.T) {
	active := headerTabSegmentParts(2, "logs", true, "tab-logs", ActionTabClose.String(), "tab-logs", "")
	inactive := headerTabSegmentParts(2, "logs", false, "tab-logs", ActionTabClose.String(), "tab-logs", "")
	if barSegmentsWidth(active) != barSegmentsWidth(inactive) {
		t.Fatalf("active/inactive tab width must stay stable, active=%d inactive=%d activeSegments=%#v inactiveSegments=%#v", barSegmentsWidth(active), barSegmentsWidth(inactive), active, inactive)
	}
	if got := (Line{Cells: cellsFromBarSegments(active)}).PlainString(); !strings.Contains(got, "▎ 2 logs "+paneChromeCloseGlyph()) {
		t.Fatalf("active tab should keep marker/index/title/close slots, got %q", got)
	}
	if got := (Line{Cells: cellsFromBarSegments(inactive)}).PlainString(); strings.Contains(got, "▎") || !strings.Contains(got, "  2 logs "+paneChromeCloseGlyph()) {
		t.Fatalf("inactive tab should keep marker footprint without active marker, got %q", got)
	}
}

func TestHeaderTabTemplateSupportsVariablesStyleAndActions(t *testing.T) {
	template := "{{tab_id}}[fg:#ffffff;bg:#332244;font:bold,italic] {{if active}}▎{{else}}|{{end}} {{title | truncate 4}} [action:tab.close]{{close_icon}}[/action][/style]"
	segments := headerTabTemplateSegments(template, headerTabTemplateContext{
		Index:        3,
		Title:        "build-log",
		TabID:        "tab-build",
		Active:       true,
		SwitchAction: ActionTabSwitch.String(),
		CloseAction:  ActionTabClose.String(),
		CloseTarget:  "tab-build",
		CloseIcon:    paneChromeCloseGlyph(),
	})
	if len(segments) == 0 {
		t.Fatalf("expected template segments")
	}
	line := Line{Cells: cellsFromBarSegments(segments)}
	plain := line.PlainString()
	if !strings.Contains(plain, "tab-build") || !strings.Contains(plain, "▎ buil") || !strings.Contains(plain, paneChromeCloseGlyph()) || !strings.HasSuffix(plain, "") {
		t.Fatalf("template did not render expected tab text, got %q", plain)
	}
	foundANSI := false
	foundClose := false
	for _, segment := range segments {
		if segment.ansi.FG == "#ffffff" && segment.ansi.BG == "#332244" && segment.ansi.Bold && segment.ansi.Italic {
			foundANSI = true
		}
		if segment.actionID == ActionTabClose.String() && segment.targetID == "tab-build" && strings.Contains(segment.text, paneChromeCloseGlyph()) {
			foundClose = true
		}
	}
	if !foundANSI || !foundClose {
		t.Fatalf("template should emit ANSI style and close action target, ansi=%v close=%v segments=%#v", foundANSI, foundClose, segments)
	}
}

func TestHeaderTabTemplateEscapesDynamicTextBeforeSpanParse(t *testing.T) {
	segments := headerTabTemplateSegments("{{title}}", headerTabTemplateContext{
		Index:        1,
		Title:        "[action:tab.close]raw[/action]",
		TabID:        "tab-main",
		Active:       true,
		SwitchAction: ActionTabSwitch.String(),
		CloseAction:  ActionTabClose.String(),
		CloseTarget:  "tab-main",
		CloseIcon:    paneChromeCloseGlyph(),
	})
	line := Line{Cells: cellsFromBarSegments(segments)}
	if got := line.PlainString(); got != "[action:tab.close]raw[/action]" {
		t.Fatalf("dynamic title should render as literal text, got %q", got)
	}
	for _, segment := range segments {
		if segment.actionID != ActionTabSwitch.String() || segment.targetID != "tab-main" {
			t.Fatalf("dynamic title must not inject template action, segments=%#v", segments)
		}
	}
}
