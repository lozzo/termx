package render

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	HitRegionOverlayFooterAction  HitRegionKind = "overlay-footer-action"
	HitRegionOverlayQueryInput    HitRegionKind = "overlay-query-input"
	HitRegionPromptInput          HitRegionKind = "prompt-input"
	HitRegionPromptSubmit         HitRegionKind = "prompt-submit"
	HitRegionPromptCancel         HitRegionKind = "prompt-cancel"
	HitRegionFloatingOverviewItem HitRegionKind = "floating-overview-item"
	HitRegionFloatingOverviewCard HitRegionKind = "floating-overview-card"
)

func OverlayHitRegions(vm RenderVM) []HitRegion {
	if vm.TermSize.Width <= 0 || vm.TermSize.Height <= 0 {
		return nil
	}
	overlaySize := TermSize{
		Width:  vm.TermSize.Width,
		Height: FrameBodyHeight(vm.TermSize.Height),
	}
	width, height := overlayViewport(overlaySize)
	switch vm.Overlay.Kind {
	case VisibleOverlayPicker:
		return pickerOverlayHitRegions(vm.Overlay.Picker, width, height)
	case VisibleOverlayWorkspacePicker:
		return workspacePickerOverlayHitRegions(vm.Overlay.WorkspacePicker, width, height)
	case VisibleOverlayHelp:
		return helpOverlayHitRegions(vm.Overlay.Help, width, height)
	case VisibleOverlayPrompt:
		return promptOverlayHitRegions(vm.Overlay.Prompt, width, height)
	case VisibleOverlayFloatingOverview:
		return floatingOverviewOverlayHitRegions(vm.Overlay.FloatingOverview, width, height)
	default:
		return nil
	}
}

func TerminalPoolHitRegions(vm RenderVM) []HitRegion {
	if vm.Surface.Kind != VisibleSurfaceTerminalPool || vm.Surface.TerminalPool == nil {
		return nil
	}
	if vm.TermSize.Width <= 0 || vm.TermSize.Height <= 0 {
		return nil
	}
	width := maxInt(1, vm.TermSize.Width)
	height := FrameBodyHeight(vm.TermSize.Height)
	layout := buildTerminalPoolPageLayout(vm.Surface.TerminalPool, width, height)
	if len(layout.itemRows) == 0 && len(layout.footerActions) == 0 {
		return nil
	}
	regions := make([]HitRegion, 0, len(layout.itemRows)+len(layout.footerActions))
	regions = append(regions, HitRegion{
		Kind: HitRegionOverlayQueryInput,
		Rect: layout.queryRect,
	})
	for _, row := range layout.itemRows {
		regions = append(regions, HitRegion{
			Kind:      HitRegionTerminalPoolItem,
			ItemIndex: row.itemIndex,
			Rect:      row.rect,
		})
	}
	for _, action := range layout.footerActions {
		regions = append(regions, HitRegion{
			Kind:   HitRegionTerminalPoolAction,
			Rect:   action.rect,
			Action: action.action,
		})
	}
	return regions
}

func pickerOverlayHitRegions(picker *modal.PickerState, width, height int) []HitRegion {
	if picker == nil {
		return nil
	}
	items := picker.VisibleItems()
	layout := buildPickerCardLayout(width, height, len(items), false)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, len(items)+5)
	regions = append(regions, dismissRegions(card, width, layout.contentHeight)...)
	regions = append(regions, HitRegion{
		Kind: HitRegionOverlayQueryInput,
		Rect: pickerQueryRowRect(layout),
	})
	rows := minInt(layout.listHeight, len(items))
	for i := 0; i < rows; i++ {
		regions = append(regions, HitRegion{
			Kind:      HitRegionPickerItem,
			ItemIndex: i,
			Rect: workbench.Rect{
				X: card.X + 1,
				Y: layout.firstItemY + i,
				W: layout.innerWidth,
				H: 1,
			},
		})
	}
	return regions
}

func workspacePickerOverlayHitRegions(picker *modal.WorkspacePickerState, width, height int) []HitRegion {
	if picker == nil {
		return nil
	}
	items := picker.VisibleItems()
	layout := buildWorkbenchTreeCardLayout(width, height, len(items), picker.Selected)
	card := workbench.Rect{X: layout.cardX, Y: layout.cardY, W: layout.cardWidth, H: layout.cardHeight}
	regions := make([]HitRegion, 0, minInt(layout.treeRows, len(items))+8)
	regions = append(regions, dismissRegions(card, width, layout.contentHeight)...)
	regions = append(regions, HitRegion{
		Kind: HitRegionOverlayQueryInput,
		Rect: layout.queryRect,
	})
	start, end := workbenchTreeWindow(len(items), picker.Selected, layout.treeRows)
	for i := start; i < end; i++ {
		regions = append(regions, HitRegion{
			Kind:      HitRegionWorkspaceItem,
			ItemIndex: i,
			Rect: workbench.Rect{
				X: layout.leftRect.X,
				Y: layout.leftRect.Y + (i - start),
				W: layout.leftRect.W,
				H: 1,
			},
		})
	}
	if selected := picker.SelectedItem(); selected != nil {
		_, actions := layoutOverlayFooterActions(workbenchTreeActionSpecs(selected), layout.actionRowRect)
		for _, action := range actions {
			regions = append(regions, HitRegion{
				Kind:   HitRegionOverlayFooterAction,
				Rect:   action.Rect,
				Action: action.Action,
			})
		}
	}
	return regions
}

func helpOverlayHitRegions(help *modal.HelpState, width, height int) []HitRegion {
	if help == nil {
		return nil
	}
	lineCount := len(helpOverlayLines(defaultUITheme(), help, pickerInnerWidth(width)))
	layout := buildPickerCardLayout(width, height, lineCount, false)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, 5)
	regions = append(regions, dismissRegions(card, width, layout.contentHeight)...)
	regions = append(regions, HitRegion{
		Kind: HitRegionHelpCard,
		Rect: card,
	})
	return regions
}

func promptOverlayHitRegions(prompt *modal.PromptState, width, height int) []HitRegion {
	if prompt == nil {
		return nil
	}
	lines, inputLines := promptOverlayContent(prompt)
	footerSpecs := promptFooterActionSpecs(prompt)
	footer := ""
	if len(footerSpecs) == 0 && prompt != nil {
		footer = prompt.Hint
	}
	lineCount := len(lines)
	hasFooter := len(footerSpecs) > 0 || strings.TrimSpace(footer) != ""
	layout := buildPickerCardLayout(width, height, lineCount, hasFooter)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, 8)
	regions = append(regions, dismissRegions(card, width, layout.contentHeight)...)
	for fieldIndex, inputLine := range inputLines {
		if inputLine < 0 || inputLine >= layout.listHeight {
			continue
		}
		rect := promptInputRect(layout, prompt, inputLine)
		if prompt.IsForm() && fieldIndex < len(prompt.Fields) {
			rect = promptFormInputRect(layout, prompt, inputLine, fieldIndex)
		}
		regions = append(regions, HitRegion{
			Kind:      HitRegionPromptInput,
			ItemIndex: fieldIndex,
			Rect:      rect,
		})
	}
	if len(footerSpecs) > 0 {
		_, actions := layoutOverlayFooterActions(footerSpecs, workbench.Rect{
			X: card.X + 1,
			Y: pickerFooterRowY(layout),
			W: layout.innerWidth,
			H: 1,
		})
		for _, action := range actions {
			kind := HitRegionOverlayFooterAction
			switch action.Action.Kind {
			case input.ActionSubmitPrompt:
				kind = HitRegionPromptSubmit
			case input.ActionCancelMode:
				kind = HitRegionPromptCancel
			}
			regions = append(regions, HitRegion{
				Kind:   kind,
				Rect:   action.Rect,
				Action: action.Action,
			})
		}
	}
	regions = append(regions, HitRegion{
		Kind: HitRegionPromptCard,
		Rect: card,
	})
	return regions
}

func floatingOverviewOverlayHitRegions(overview *modal.FloatingOverviewState, width, height int) []HitRegion {
	if overview == nil {
		return nil
	}
	footerSpecs := floatingOverviewFooterActionSpecs()
	layout := buildPickerCardLayout(width, height, len(overview.Items), len(footerSpecs) > 0)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, len(overview.Items)+len(footerSpecs)+4)
	regions = append(regions, dismissRegions(card, width, layout.contentHeight)...)
	rows := minInt(layout.listHeight, len(overview.Items))
	for i := 0; i < rows; i++ {
		regions = append(regions, HitRegion{
			Kind:      HitRegionFloatingOverviewItem,
			ItemIndex: i,
			Rect: workbench.Rect{
				X: card.X + 1,
				Y: layout.firstItemY + i,
				W: layout.innerWidth,
				H: 1,
			},
		})
	}
	if len(footerSpecs) > 0 {
		_, actions := layoutOverlayFooterActions(footerSpecs, workbench.Rect{
			X: card.X + 1,
			Y: pickerFooterRowY(layout),
			W: layout.innerWidth,
			H: 1,
		})
		for _, action := range actions {
			regions = append(regions, HitRegion{
				Kind:   HitRegionOverlayFooterAction,
				Rect:   action.Rect,
				Action: action.Action,
			})
		}
	}
	regions = append(regions, HitRegion{Kind: HitRegionFloatingOverviewCard, Rect: card})
	return regions
}

func pickerCardRect(layout pickerCardLayout) workbench.Rect {
	return workbench.Rect{
		X: layout.cardX,
		Y: layout.cardY,
		W: layout.cardWidth,
		H: layout.cardHeight,
	}
}

func dismissRegions(card workbench.Rect, width, height int) []HitRegion {
	cancel := input.SemanticAction{Kind: input.ActionCancelMode}
	regions := make([]HitRegion, 0, 4)
	if card.Y > 0 {
		regions = append(regions, HitRegion{
			Kind:   HitRegionOverlayDismiss,
			Action: cancel,
			Rect:   workbench.Rect{X: 0, Y: 0, W: width, H: card.Y},
		})
	}
	if card.X > 0 {
		regions = append(regions, HitRegion{
			Kind:   HitRegionOverlayDismiss,
			Action: cancel,
			Rect:   workbench.Rect{X: 0, Y: card.Y, W: card.X, H: card.H},
		})
	}
	if rightX := card.X + card.W; rightX < width {
		regions = append(regions, HitRegion{
			Kind:   HitRegionOverlayDismiss,
			Action: cancel,
			Rect:   workbench.Rect{X: rightX, Y: card.Y, W: width - rightX, H: card.H},
		})
	}
	if bottomY := card.Y + card.H; bottomY < height {
		regions = append(regions, HitRegion{
			Kind:   HitRegionOverlayDismiss,
			Action: cancel,
			Rect:   workbench.Rect{X: 0, Y: bottomY, W: width, H: height - bottomY},
		})
	}
	return regions
}
