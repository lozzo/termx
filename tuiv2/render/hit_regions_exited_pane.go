package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type exitedPaneActionSpec struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
}

type exitedPaneActionLayout struct {
	spec    exitedPaneActionSpec
	rowRect workbench.Rect
}

func exitedPaneActionSpecs(paneID string) []exitedPaneActionSpec {
	return []exitedPaneActionSpec{
		{
			Kind:   HitRegionExitedPaneRestart,
			Label:  "R restart current terminal",
			Action: input.SemanticAction{Kind: input.ActionRestartTerminal, PaneID: paneID},
		},
		{
			Kind:   HitRegionExitedPaneChoose,
			Label:  "Ctrl-F choose another terminal",
			Action: input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: paneID, PaneID: paneID},
		},
	}
}

func wrapExitedPaneActionLabel(spec exitedPaneActionSpec, selected bool, pulse bool) string {
	label := strings.TrimSpace(spec.Label)
	if label == "" {
		return ""
	}
	if selected {
		if pulse {
			return "► " + label + " ◄"
		}
		return "◆ " + label + " ◆"
	}
	return "[ " + label + " ]"
}

func ExitedPaneRecoveryRegions(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) []HitRegion {
	if pane.ID == "" || strings.TrimSpace(pane.TerminalID) == "" {
		return nil
	}
	terminal := findVisibleTerminal(runtimeState, pane.TerminalID)
	if terminal == nil || terminal.State != "exited" {
		return nil
	}
	layout := layoutExitedPaneRecoveryActions(contentRectForPane(pane.Rect), pane.ID)
	regions := make([]HitRegion, 0, len(layout))
	for _, item := range layout {
		regions = append(regions, HitRegion{
			Kind:   item.spec.Kind,
			Rect:   item.rowRect,
			Action: item.spec.Action,
			PaneID: pane.ID,
		})
	}
	return regions
}

func layoutExitedPaneRecoveryActions(contentRect workbench.Rect, paneID string) []exitedPaneActionLayout {
	if contentRect.W <= 0 || contentRect.H <= 0 || strings.TrimSpace(paneID) == "" {
		return nil
	}
	specs := exitedPaneActionSpecs(paneID)
	out := make([]exitedPaneActionLayout, 0, len(specs))
	startY := contentRect.Y + contentRect.H - len(specs)
	if contentRect.H >= len(specs)+1 {
		startY--
	}
	if startY < contentRect.Y {
		startY = contentRect.Y
	}
	for index, spec := range specs {
		y := startY + index
		if y < contentRect.Y || y >= contentRect.Y+contentRect.H {
			continue
		}
		lineText := centerText(xansi.Truncate(wrapExitedPaneActionLabel(spec, false, true), contentRect.W, ""), contentRect.W)
		out = append(out, exitedPaneActionLayout{
			spec:    spec,
			rowRect: workbench.Rect{X: contentRect.X, Y: y, W: xansi.StringWidth(lineText), H: 1},
		})
	}
	return out
}
