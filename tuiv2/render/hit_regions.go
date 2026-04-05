package render

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type HitRegionKind string

const (
	HitRegionWorkspaceLabel     HitRegionKind = "workspace-label"
	HitRegionTabSwitch          HitRegionKind = "tab-switch"
	HitRegionTabClose           HitRegionKind = "tab-close"
	HitRegionTabCreate          HitRegionKind = "tab-create"
	HitRegionOverlayDismiss     HitRegionKind = "overlay-dismiss"
	HitRegionPickerItem         HitRegionKind = "picker-item"
	HitRegionWorkspaceItem      HitRegionKind = "workspace-item"
	HitRegionHelpCard           HitRegionKind = "help-card"
	HitRegionPromptCard         HitRegionKind = "prompt-card"
	HitRegionTerminalPoolItem   HitRegionKind = "terminal-pool-item"
	HitRegionTerminalPoolAction HitRegionKind = "terminal-pool-action"
	HitRegionEmptyPaneAttach    HitRegionKind = "empty-pane-attach"
	HitRegionEmptyPaneCreate    HitRegionKind = "empty-pane-create"
	HitRegionEmptyPaneManager   HitRegionKind = "empty-pane-manager"
	HitRegionEmptyPaneClose     HitRegionKind = "empty-pane-close"
	HitRegionExitedPaneRestart  HitRegionKind = "exited-pane-restart"
	HitRegionExitedPaneChoose   HitRegionKind = "exited-pane-choose"
	HitRegionPaneClose          HitRegionKind = "pane-close"
	HitRegionPaneZoom           HitRegionKind = "pane-zoom"
	HitRegionPaneSplitV         HitRegionKind = "pane-split-vertical"
	HitRegionPaneSplitH         HitRegionKind = "pane-split-horizontal"
	HitRegionPaneOwner          HitRegionKind = "pane-owner"
)

type HitRegion struct {
	Kind      HitRegionKind
	Rect      workbench.Rect
	Action    input.SemanticAction
	TabIndex  int
	ItemIndex int
	PaneID    string
}

func pointInRect(rect workbench.Rect, x, y int) bool {
	return rect.W > 0 && rect.H > 0 &&
		x >= rect.X && x < rect.X+rect.W &&
		y >= rect.Y && y < rect.Y+rect.H
}

func hitRegionAt(regions []HitRegion, x, y int) (HitRegion, bool) {
	for _, region := range regions {
		if pointInRect(region.Rect, x, y) {
			return region, true
		}
	}
	return HitRegion{}, false
}

func HitRegionAt(regions []HitRegion, x, y int) (HitRegion, bool) {
	return hitRegionAt(regions, x, y)
}
