package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	paneChromeActionGap      = 1
	paneChromeOwnerRegionMin = 1
)

const (
	HitRegionPaneDetach           HitRegionKind = "pane-detach"
	HitRegionPaneReconnect        HitRegionKind = "pane-reconnect"
	HitRegionPaneCloseKill        HitRegionKind = "pane-close-kill"
	HitRegionPaneOpenPicker       HitRegionKind = "pane-open-picker"
	HitRegionPaneCenterFloating   HitRegionKind = "pane-center-floating"
	HitRegionPaneToggleFloating   HitRegionKind = "pane-toggle-floating"
	HitRegionPaneCollapseFloating HitRegionKind = "pane-collapse-floating"
	HitRegionPaneBalancePanes     HitRegionKind = "pane-balance-panes"
	HitRegionPaneCycleLayout      HitRegionKind = "pane-cycle-layout"
)

type paneChromeActionToken struct {
	Kind   HitRegionKind
	Label  string
	Action input.ActionKind
}

type paneChromeActionSlot struct {
	Kind  HitRegionKind
	Label string
	X     int
}

type paneChromeContext struct {
	Floating         bool
	TerminalAttached bool
	LayoutCluster    bool
}

func paneZoomIcon() string             { return paneChromeGlyphs.Zoom }
func paneSplitVerticalIcon() string    { return paneChromeGlyphs.SplitVertical }
func paneSplitHorizontalIcon() string  { return paneChromeGlyphs.SplitHorizontal }
func paneCloseIcon() string            { return paneChromeGlyphs.Close }
func paneCenterFloatingIcon() string   { return paneChromeGlyphs.CenterFloating }
func paneCollapseFloatingIcon() string { return paneChromeGlyphs.CollapseFloating }
func paneRunningIcon() string          { return paneChromeGlyphs.Running }
func paneWaitingIcon() string          { return paneChromeGlyphs.Waiting }
func paneExitedIcon() string           { return paneChromeGlyphs.Exited }
func paneKilledIcon() string           { return paneChromeGlyphs.Killed }

func paneChromeTerminalAttached(title string, border paneBorderInfo) bool {
	if strings.TrimSpace(border.StateLabel) != "" || strings.TrimSpace(border.ShareLabel) != "" || strings.TrimSpace(border.RoleLabel) != "" {
		return true
	}
	return strings.TrimSpace(title) != "" && strings.TrimSpace(strings.ToLower(title)) != "unconnected"
}

func paneChromeContextForFrame(rect workbench.Rect, title string, border paneBorderInfo, floating bool) paneChromeContext {
	return paneChromeContext{
		Floating:         floating,
		TerminalAttached: paneChromeTerminalAttached(title, border),
		LayoutCluster:    !floating && rect.X == 0 && rect.Y == 0,
	}
}

func paneChromeContextForPane(pane workbench.VisiblePane, title string, border paneBorderInfo) paneChromeContext {
	return paneChromeContextForFrame(pane.Rect, title, border, pane.Floating)
}

func paneChromeActionTokensForContext(ctx paneChromeContext) []paneChromeActionToken {
	if ctx.Floating {
		return []paneChromeActionToken{
			{Kind: HitRegionPaneCenterFloating, Label: "[" + paneCenterFloatingIcon() + "]", Action: input.ActionCenterFloatingPane},
			{Kind: HitRegionPaneCollapseFloating, Label: "[" + paneCollapseFloatingIcon() + "]", Action: input.ActionCollapseFloatingPane},
			{Kind: HitRegionPaneZoom, Label: "[" + paneZoomIcon() + "]", Action: input.ActionZoomPane},
			{Kind: HitRegionPaneClose, Label: "[" + paneCloseIcon() + "]", Action: input.ActionClosePane},
		}
	}
	tokens := []paneChromeActionToken{
		{Kind: HitRegionPaneZoom, Label: "[" + paneZoomIcon() + "]", Action: input.ActionZoomPane},
		{Kind: HitRegionPaneSplitV, Label: "[" + paneSplitVerticalIcon() + "]", Action: input.ActionSplitPane},
		{Kind: HitRegionPaneSplitH, Label: "[" + paneSplitHorizontalIcon() + "]", Action: input.ActionSplitPaneHorizontal},
		{Kind: HitRegionPaneClose, Label: "[" + paneCloseIcon() + "]", Action: input.ActionClosePane},
	}
	return tokens
}

func paneChromeActionTokensForFrame(rect workbench.Rect, title string, border paneBorderInfo, floating bool) []paneChromeActionToken {
	return paneChromeActionTokensForContext(paneChromeContextForFrame(rect, title, border, floating))
}

func paneChromeActionTokensForPane(pane workbench.VisiblePane, title string, border paneBorderInfo) []paneChromeActionToken {
	return paneChromeActionTokensForContext(paneChromeContextForPane(pane, title, border))
}

func paneChromeActionSignatureForFrame(rect workbench.Rect, title string, border paneBorderInfo, floating bool) string {
	tokens := paneChromeActionTokensForFrame(rect, title, border, floating)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		parts = append(parts, string(token.Kind))
	}
	return strings.Join(parts, "|")
}

func paneChromeActionSlotsWidth(tokens []paneChromeActionToken, count int) int {
	if count <= 0 {
		return 0
	}
	total := 0
	if count > len(tokens) {
		count = len(tokens)
	}
	for i := 0; i < count; i++ {
		total += xansi.StringWidth(tokens[i].Label)
		if i > 0 {
			total += paneChromeActionGap
		}
	}
	return total
}

func paneChromeActionClusterWidth(tokens []paneChromeActionToken, count int) int {
	total := paneChromeActionSlotsWidth(tokens, count)
	if total > 0 {
		total += 1
	}
	return total
}

func paneChromeActionSlotForKind(kind HitRegionKind, paneID string) (input.SemanticAction, bool) {
	switch kind {
	case HitRegionPaneClose:
		return input.SemanticAction{Kind: input.ActionClosePane, PaneID: paneID}, true
	case HitRegionPaneZoom:
		return input.SemanticAction{Kind: input.ActionZoomPane, PaneID: paneID}, true
	case HitRegionPaneSplitV:
		return input.SemanticAction{Kind: input.ActionSplitPane, PaneID: paneID}, true
	case HitRegionPaneSplitH:
		return input.SemanticAction{Kind: input.ActionSplitPaneHorizontal, PaneID: paneID}, true
	case HitRegionPaneDetach:
		return input.SemanticAction{Kind: input.ActionDetachPane, PaneID: paneID}, true
	case HitRegionPaneReconnect:
		return input.SemanticAction{Kind: input.ActionReconnectPane, PaneID: paneID}, true
	case HitRegionPaneCloseKill:
		return input.SemanticAction{Kind: input.ActionClosePaneKill, PaneID: paneID}, true
	case HitRegionPaneOpenPicker:
		return input.SemanticAction{Kind: input.ActionOpenPicker, PaneID: paneID, TargetID: paneID}, true
	case HitRegionPaneCenterFloating:
		return input.SemanticAction{Kind: input.ActionCenterFloatingPane, PaneID: paneID}, true
	case HitRegionPaneCollapseFloating:
		return input.SemanticAction{Kind: input.ActionCollapseFloatingPane, PaneID: paneID}, true
	case HitRegionPaneToggleFloating:
		return input.SemanticAction{Kind: input.ActionToggleFloatingVisibility, PaneID: paneID}, true
	case HitRegionPaneBalancePanes:
		return input.SemanticAction{Kind: input.ActionBalancePanes, PaneID: paneID}, true
	case HitRegionPaneCycleLayout:
		return input.SemanticAction{Kind: input.ActionCycleLayout, PaneID: paneID}, true
	}
	return input.SemanticAction{}, false
}

func PaneChromeHitRegions(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy, confirmPaneID string) []HitRegion {
	if strings.TrimSpace(pane.ID) == "" || pane.Frameless {
		return nil
	}
	lookup := newRuntimeLookup(runtimeState)
	title := resolvePaneTitleWithLookup(pane, lookup)
	border := paneBorderInfoWithLookup(pane, lookup, confirmPaneID)
	if strings.TrimSpace(pane.TerminalID) != "" && !paneChromeTerminalAttached(title, border) {
		title = "terminal"
	}
	layout, ok := paneTopBorderLabelsLayout(
		pane.Rect,
		title,
		border,
		paneChromeActionTokensForPane(pane, title, border),
	)
	if !ok {
		return nil
	}

	regions := make([]HitRegion, 0, len(layout.actionSlots)+1)
	for _, slot := range layout.actionSlots {
		action, ok := paneChromeActionSlotForKind(slot.Kind, pane.ID)
		if !ok {
			continue
		}
		regions = append(regions, HitRegion{
			Kind:   slot.Kind,
			PaneID: pane.ID,
			Action: action,
			Rect: workbench.Rect{
				X: slot.X,
				Y: pane.Rect.Y,
				W: xansi.StringWidth(slot.Label),
				H: 1,
			},
		})
	}

	if layout.roleLabel != "" && paneOwnerActionLabel(pane, lookup, confirmPaneID) != "" && xansi.StringWidth(layout.roleLabel) >= paneChromeOwnerRegionMin {
		regions = append(regions, HitRegion{
			Kind:   HitRegionPaneOwner,
			PaneID: pane.ID,
			Action: input.SemanticAction{
				Kind:   input.ActionBecomeOwner,
				PaneID: pane.ID,
			},
			Rect: workbench.Rect{
				X: layout.roleX,
				Y: pane.Rect.Y,
				W: xansi.StringWidth(layout.roleLabel),
				H: 1,
			},
		})
	}
	return regions
}
