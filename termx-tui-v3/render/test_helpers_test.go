package render

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func bindTestPaneTerminal(root state.Root, paneID string, terminalID string) state.Root {
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: paneID}, terminalID)
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(
		paneID,
		terminalID,
		7,
		80,
		24,
		state.TerminalResizeRoleOwner,
		"surface",
		state.TerminalPaneViewID(paneID),
		true,
	))
	return root
}

func hitRegionByAction(t *testing.T, regions []HitRegion, actionID string) HitRegion {
	t.Helper()
	for _, region := range regions {
		if region.ActionID == actionID {
			return region
		}
	}
	t.Fatalf("missing action hit region %q in %#v", actionID, regions)
	return HitRegion{}
}

func frameContains(frame Frame, value string) bool {
	for _, line := range frame.Lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func plainLines(lines []Line) string {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line.PlainString())
		builder.WriteByte('\n')
	}
	return builder.String()
}
