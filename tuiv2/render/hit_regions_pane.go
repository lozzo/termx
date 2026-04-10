package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type emptyPaneActionSpec struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
}

type emptyPaneActionLayout struct {
	spec     emptyPaneActionSpec
	rowRect  workbench.Rect
	lineText string
}

func emptyPaneActionSpecs(paneID string) []emptyPaneActionSpec {
	return []emptyPaneActionSpec{
		{
			Kind:   HitRegionEmptyPaneAttach,
			Label:  "Attach existing terminal",
			Action: input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: paneID, PaneID: paneID},
		},
		{
			Kind:   HitRegionEmptyPaneCreate,
			Label:  "Create new terminal",
			Action: input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: paneID, PaneID: paneID},
		},
		{
			Kind:   HitRegionEmptyPaneManager,
			Label:  "Open terminal manager",
			Action: input.SemanticAction{Kind: input.ActionOpenTerminalManager, PaneID: paneID},
		},
		{
			Kind:   HitRegionEmptyPaneClose,
			Label:  "Close pane",
			Action: input.SemanticAction{Kind: input.ActionClosePane, PaneID: paneID},
		},
	}
}

func wrapEmptyPaneActionLabel(spec emptyPaneActionSpec, selected bool) string {
	label := strings.TrimSpace(spec.Label)
	if label == "" {
		return ""
	}
	if selected {
		return "► " + label + " ◄"
	}
	return "[ " + label + " ]"
}

func EmptyPaneActionRegions(pane workbench.VisiblePane) []HitRegion {
	if pane.ID == "" || strings.TrimSpace(pane.TerminalID) != "" {
		return nil
	}
	layout := layoutEmptyPaneActions(interactiveContentRectForPane(pane), pane.ID)
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

func interactiveContentRectForPane(pane workbench.VisiblePane) workbench.Rect {
	if pane.Frameless {
		return pane.Rect
	}
	return contentRectForPaneEdges(pane.Rect, pane.SharedLeft, pane.SharedTop)
}

func layoutEmptyPaneActions(contentRect workbench.Rect, paneID string) []emptyPaneActionLayout {
	if contentRect.W <= 0 || contentRect.H <= 0 || strings.TrimSpace(paneID) == "" {
		return nil
	}
	specs := emptyPaneActionSpecs(paneID)
	out := make([]emptyPaneActionLayout, 0, len(specs))
	startY := contentRect.Y
	if contentRect.H >= len(specs)+2 {
		startY++
	}
	for index, spec := range specs {
		y := startY + index
		if y >= contentRect.Y+contentRect.H {
			break
		}
		lineText := centerText(xansi.Truncate(wrapEmptyPaneActionLabel(spec, false), contentRect.W, ""), contentRect.W)
		out = append(out, emptyPaneActionLayout{
			spec:     spec,
			rowRect:  workbench.Rect{X: contentRect.X, Y: y, W: contentRect.W, H: 1},
			lineText: lineText,
		})
	}
	return out
}
