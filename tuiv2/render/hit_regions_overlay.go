package render

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	HitRegionOverlayFooterAction HitRegionKind = "overlay-footer-action"
	HitRegionOverlayQueryInput   HitRegionKind = "overlay-query-input"
	HitRegionPromptInput         HitRegionKind = "prompt-input"
	HitRegionPromptSubmit        HitRegionKind = "prompt-submit"
	HitRegionPromptCancel        HitRegionKind = "prompt-cancel"
)

func OverlayHitRegions(state VisibleRenderState) []HitRegion {
	if state.TermSize.Width <= 0 || state.TermSize.Height <= 0 {
		return nil
	}
	overlaySize := TermSize{
		Width:  state.TermSize.Width,
		Height: FrameBodyHeight(state.TermSize.Height),
	}
	width, height := overlayViewport(overlaySize)
	switch state.Overlay.Kind {
	case VisibleOverlayPicker:
		return pickerOverlayHitRegions(state.Overlay.Picker, width, height)
	case VisibleOverlayWorkspacePicker:
		return workspacePickerOverlayHitRegions(state.Overlay.WorkspacePicker, width, height)
	case VisibleOverlayHelp:
		return helpOverlayHitRegions(state.Overlay.Help, width, height)
	case VisibleOverlayPrompt:
		return promptOverlayHitRegions(state.Overlay.Prompt, width, height)
	case VisibleOverlayTerminalManager:
		return terminalManagerOverlayHitRegions(state.Overlay.TerminalManager, width, height)
	default:
		return nil
	}
}

func TerminalPoolHitRegions(state VisibleRenderState) []HitRegion {
	if state.Surface.Kind != VisibleSurfaceTerminalPool || state.Surface.TerminalPool == nil {
		return nil
	}
	if state.TermSize.Width <= 0 || state.TermSize.Height <= 0 {
		return nil
	}
	width := maxInt(1, state.TermSize.Width)
	height := FrameBodyHeight(state.TermSize.Height)
	layout := buildTerminalPoolPageLayout(state.Surface.TerminalPool, width, height)
	if len(layout.itemRows) == 0 && len(layout.footerActions) == 0 {
		return nil
	}
	regions := make([]HitRegion, 0, len(layout.itemRows)+len(layout.footerActions))
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
	footerSpecs := pickerFooterActionSpecs()
	layout := buildPickerCardLayout(width, height, len(items), len(footerSpecs) > 0)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, len(items)+len(footerSpecs)+5)
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
	return regions
}

func workspacePickerOverlayHitRegions(picker *modal.WorkspacePickerState, width, height int) []HitRegion {
	if picker == nil {
		return nil
	}
	items := picker.VisibleItems()
	footerSpecs := workspacePickerFooterActionSpecs()
	layout := buildPickerCardLayout(width, height, len(items), len(footerSpecs) > 0)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, len(items)+len(footerSpecs)+5)
	regions = append(regions, dismissRegions(card, width, layout.contentHeight)...)
	regions = append(regions, HitRegion{
		Kind: HitRegionOverlayQueryInput,
		Rect: pickerQueryRowRect(layout),
	})
	rows := minInt(layout.listHeight, len(items))
	for i := 0; i < rows; i++ {
		regions = append(regions, HitRegion{
			Kind:      HitRegionWorkspaceItem,
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
	return regions
}

func terminalManagerOverlayHitRegions(manager *modal.TerminalManagerState, width, height int) []HitRegion {
	if manager == nil {
		return nil
	}
	items := manager.VisibleItems()
	footerSpecs := terminalManagerFooterActionSpecs()
	layout := buildPickerCardLayout(width, height, len(items), len(footerSpecs) > 0)
	card := pickerCardRect(layout)
	regions := make([]HitRegion, 0, len(items)+len(footerSpecs)+5)
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
	lines, inputLine := promptOverlayContent(prompt)
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
	if inputLine >= 0 && inputLine < layout.listHeight {
		regions = append(regions, HitRegion{
			Kind: HitRegionPromptInput,
			Rect: promptInputRect(layout, prompt, inputLine),
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
