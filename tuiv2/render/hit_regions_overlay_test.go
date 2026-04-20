package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func collectRegionsByKind(regions []HitRegion, kind HitRegionKind) []HitRegion {
	out := make([]HitRegion, 0, len(regions))
	for _, region := range regions {
		if region.Kind == kind {
			out = append(out, region)
		}
	}
	return out
}

func vmFromState(state VisibleRenderState) RenderVM {
	return RenderVM{
		Workbench: state.Workbench,
		Runtime:   state.Runtime,
		Surface: RenderSurfaceVM{
			Kind:         state.Surface.Kind,
			TerminalPool: state.Surface.TerminalPool,
		},
		Overlay: RenderOverlayVM{
			Kind:             state.Overlay.Kind,
			Prompt:           state.Overlay.Prompt,
			Picker:           state.Overlay.Picker,
			WorkspacePicker:  state.Overlay.WorkspacePicker,
			Help:             state.Overlay.Help,
			FloatingOverview: state.Overlay.FloatingOverview,
		},
		TermSize: state.TermSize,
		Status: RenderStatusVM{
			Notice:      state.Notice,
			Error:       state.Error,
			InputMode:   state.InputMode,
			Hints:       append([]string(nil), state.StatusHints...),
			RightTokens: append([]RenderStatusToken(nil), statusBarRightTokens(state)...),
		},
		Body: RenderBodyVM{
			OwnerConfirmPaneID: state.OwnerConfirmPaneID,
			EmptySelection: RenderPaneSelectionVM{
				PaneID: state.EmptyPaneSelectionPaneID,
				Index:  state.EmptyPaneSelectionIndex,
			},
			ExitedSelection: RenderPaneSelectionVM{
				PaneID: state.ExitedPaneSelectionPaneID,
				Index:  state.ExitedPaneSelectionIndex,
			},
			SnapshotOverride: RenderSnapshotOverrideVM{
				PaneID:   state.PaneSnapshotOverridePaneID,
				Snapshot: state.PaneSnapshotOverride,
			},
			CopyMode: RenderCopyModeVM{
				PaneID:     state.CopyModePaneID,
				CursorRow:  state.CopyModeCursorRow,
				CursorCol:  state.CopyModeCursorCol,
				ViewTopRow: state.CopyModeViewTopRow,
				MarkSet:    state.CopyModeMarkSet,
				MarkRow:    state.CopyModeMarkRow,
				MarkCol:    state.CopyModeMarkCol,
				Snapshot:   state.CopyModeSnapshot,
			},
		},
	}
}

func TestOverlayHitRegionsPickerRowsAndDismissUseCardLayout(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 100, Height: 30},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayPicker,
			Picker: &modal.PickerState{
				Items: []modal.PickerItem{
					{TerminalID: "term-1", Name: "shell-1"},
					{TerminalID: "term-2", Name: "shell-2"},
					{TerminalID: "term-3", Name: "shell-3"},
				},
			},
		},
	}

	regions := OverlayHitRegions(vmFromState(state))
	itemRegions := collectRegionsByKind(regions, HitRegionPickerItem)
	if len(itemRegions) != 3 {
		t.Fatalf("expected 3 picker item regions, got %#v", itemRegions)
	}
	layout := buildPickerCardLayout(100, FrameBodyHeight(30), 3, false)
	for i, region := range itemRegions {
		if region.ItemIndex != i {
			t.Fatalf("expected item index %d, got %#v", i, region)
		}
		want := pickerCardRect(layout)
		want.X++
		want.Y = layout.firstItemY + i
		want.W = layout.innerWidth
		want.H = 1
		if region.Rect != want {
			t.Fatalf("expected picker row rect %#v, got %#v", want, region.Rect)
		}
	}
	dismissRegions := collectRegionsByKind(regions, HitRegionOverlayDismiss)
	if len(dismissRegions) == 0 {
		t.Fatalf("expected overlay dismiss regions, got %#v", regions)
	}
	for _, region := range dismissRegions {
		if region.Action.Kind != input.ActionCancelMode {
			t.Fatalf("expected dismiss action cancel-mode, got %#v", region.Action)
		}
	}
	queryRegions := collectRegionsByKind(regions, HitRegionOverlayQueryInput)
	if len(queryRegions) != 1 {
		t.Fatalf("expected one overlay query region, got %#v", queryRegions)
	}
	if got, want := queryRegions[0].Rect, pickerQueryRowRect(layout); got != want {
		t.Fatalf("expected picker query rect %#v, got %#v", want, got)
	}
}

func TestOverlayHitRegionsWorkspacePickerItemRows(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 96, Height: 26},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayWorkspacePicker,
			WorkspacePicker: &modal.WorkspacePickerState{
				Items: []modal.WorkspacePickerItem{
					{Name: "main"},
					{Name: "dev"},
				},
			},
		},
	}

	regions := OverlayHitRegions(vmFromState(state))
	itemRegions := collectRegionsByKind(regions, HitRegionWorkspaceItem)
	if len(itemRegions) != 3 {
		t.Fatalf("expected 3 workspace item regions including create row, got %#v", itemRegions)
	}
	_, overlayHeight := overlayViewport(TermSize{Width: 96, Height: FrameBodyHeight(26)})
	layout := buildWorkbenchTreeCardLayout(96, overlayHeight, 3, 0)
	if itemRegions[0].Rect.Y != layout.leftRect.Y || itemRegions[1].Rect.Y != layout.leftRect.Y+1 || itemRegions[2].Rect.Y != layout.leftRect.Y+2 {
		t.Fatalf("workspace picker row placement mismatch: %#v", itemRegions)
	}
}

func TestOverlayHitRegionsPromptAndHelpExposeCardAndDismiss(t *testing.T) {
	promptState := VisibleRenderState{
		TermSize: TermSize{Width: 90, Height: 24},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayPrompt,
			Prompt: &modal.PromptState{
				Kind:  "create-terminal-name",
				Value: "shell",
				Hint:  "update the terminal name before continuing",
			},
		},
	}
	promptRegions := OverlayHitRegions(vmFromState(promptState))
	if cards := collectRegionsByKind(promptRegions, HitRegionPromptCard); len(cards) != 1 {
		t.Fatalf("expected one prompt card region, got %#v", cards)
	}
	if dismiss := collectRegionsByKind(promptRegions, HitRegionOverlayDismiss); len(dismiss) == 0 {
		t.Fatalf("expected prompt dismiss regions, got %#v", promptRegions)
	}
	if inputRegions := collectRegionsByKind(promptRegions, HitRegionPromptInput); len(inputRegions) != 1 {
		t.Fatalf("expected one prompt input region, got %#v", inputRegions)
	}
	if submitRegions := collectRegionsByKind(promptRegions, HitRegionPromptSubmit); len(submitRegions) != 1 {
		t.Fatalf("expected one prompt submit region, got %#v", submitRegions)
	}
	if cancelRegions := collectRegionsByKind(promptRegions, HitRegionPromptCancel); len(cancelRegions) != 1 {
		t.Fatalf("expected one prompt cancel region, got %#v", cancelRegions)
	}
	lines, inputLines := promptOverlayContent(promptState.Overlay.Prompt)
	footerSpecs := promptFooterActionSpecs(promptState.Overlay.Prompt)
	width, height := overlayViewport(TermSize{Width: 90, Height: FrameBodyHeight(24)})
	layout := buildPickerCardLayout(width, height, len(lines), len(footerSpecs) > 0)
	expectedInput := promptInputRect(layout, promptState.Overlay.Prompt, inputLines[0])
	if got := collectRegionsByKind(promptRegions, HitRegionPromptInput)[0].Rect; got != expectedInput {
		t.Fatalf("expected prompt input rect %#v, got %#v", expectedInput, got)
	}

	helpState := VisibleRenderState{
		TermSize: TermSize{Width: 90, Height: 24},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayHelp,
			Help: &modal.HelpState{
				Sections: []modal.HelpSection{
					{
						Title:    "Most Used",
						Bindings: []modal.HelpBinding{{Key: "Ctrl-P", Action: "pane mode"}},
					},
				},
			},
		},
	}
	helpRegions := OverlayHitRegions(vmFromState(helpState))
	if cards := collectRegionsByKind(helpRegions, HitRegionHelpCard); len(cards) != 1 {
		t.Fatalf("expected one help card region, got %#v", cards)
	}
	if dismiss := collectRegionsByKind(helpRegions, HitRegionOverlayDismiss); len(dismiss) == 0 {
		t.Fatalf("expected help dismiss regions, got %#v", helpRegions)
	}
}

func TestOverlayHitRegionsPromptSuggestionPopupOverflowsWithoutGrowingCard(t *testing.T) {
	prompt := &modal.PromptState{
		Kind:        "create-terminal-form",
		Title:       "Create Terminal",
		Hint:        "name is required; command, workdir, tags are optional",
		ActiveField: 2,
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{
				Key:             "workdir",
				Label:           "workdir",
				Value:           "/tmp/de",
				SuggestionTitle: "path: /tmp",
				SuggestionItems: []string{"/tmp/demo/", "/tmp/dev/", "/tmp/deploy/", "/tmp/design/", "/tmp/debug/", "/tmp/delta/"},
			},
			{Key: "tags", Label: "tags", Value: "role=dev"},
		},
	}
	state := VisibleRenderState{
		TermSize: TermSize{Width: 100, Height: 30},
		Overlay: VisibleOverlay{
			Kind:   VisibleOverlayPrompt,
			Prompt: prompt,
		},
	}

	regions := OverlayHitRegions(vmFromState(state))
	cardRegions := collectRegionsByKind(regions, HitRegionPromptCard)
	if len(cardRegions) != 1 {
		t.Fatalf("expected one prompt card region, got %#v", cardRegions)
	}
	suggestionRegions := collectRegionsByKind(regions, HitRegionPromptSuggestionItem)
	if len(suggestionRegions) != len(prompt.Fields[2].SuggestionItems) {
		t.Fatalf("expected %d suggestion regions, got %#v", len(prompt.Fields[2].SuggestionItems), suggestionRegions)
	}

	lines, inputLines := promptOverlayContent(prompt)
	width, height := overlayViewport(TermSize{Width: 100, Height: FrameBodyHeight(30)})
	hasFooter := len(promptFooterActionSpecs(prompt)) > 0
	layout := buildPickerCardLayout(width, height, len(lines), hasFooter)
	if got, want := cardRegions[0].Rect, pickerCardRect(layout); got != want {
		t.Fatalf("expected prompt card rect %#v, got %#v", want, got)
	}

	popup := buildPromptSuggestionPopupLayout(defaultUITheme(), prompt.Fields[prompt.ActiveField], prompt.PromptSuggestionSelected, inputLines[prompt.ActiveField], pickerInnerWidth(width))
	last := suggestionRegions[len(suggestionRegions)-1]
	popupTopY := promptSuggestionPopupTopY(layout, popup)
	expectedLastY := popupTopY + popup.itemStart + len(prompt.Fields[2].SuggestionItems) - 1
	if got := last.Rect.Y; got != expectedLastY {
		t.Fatalf("expected last suggestion row y=%d, got %#v", expectedLastY, last.Rect)
	}
	if cardBottom := cardRegions[0].Rect.Y + cardRegions[0].Rect.H - 1; last.Rect.Y <= cardBottom {
		t.Fatalf("expected last suggestion row to overflow card bottom %d, got %#v", cardBottom, last.Rect)
	}
	if region, ok := HitRegionAt(regions, last.Rect.X+1, last.Rect.Y); !ok || region.Kind != HitRegionPromptSuggestionItem || region.ItemIndex != len(suggestionRegions)-1 {
		t.Fatalf("expected overflow point to hit last suggestion region, got ok=%v region=%#v", ok, region)
	}
}

func TestOverlayHitRegionsWorkspacePickerQueryInputUsesEditableFieldRect(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 100, Height: 30},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayWorkspacePicker,
			WorkspacePicker: &modal.WorkspacePickerState{
				Items: []modal.WorkspacePickerItem{{Name: "main"}},
			},
		},
	}
	regions := OverlayHitRegions(vmFromState(state))
	queryRegions := collectRegionsByKind(regions, HitRegionOverlayQueryInput)
	if len(queryRegions) != 1 {
		t.Fatalf("expected one workspace query region, got %#v", queryRegions)
	}
	layout := buildWorkbenchTreeCardLayout(100, FrameBodyHeight(30), 2, 0)
	if got, want := queryRegions[0].Rect, layout.queryRect; got != want {
		t.Fatalf("expected workspace query rect %#v, got %#v", want, got)
	}
}

func TestOverlayHitRegionsPromptFooterKindMappingTracksClippedPrefix(t *testing.T) {
	prompt := &modal.PromptState{
		Kind:  "rename-tab",
		Value: "tab-1",
	}
	state := VisibleRenderState{
		TermSize: TermSize{Width: 60, Height: 24},
		Overlay: VisibleOverlay{
			Kind:   VisibleOverlayPrompt,
			Prompt: prompt,
		},
	}
	regions := OverlayHitRegions(vmFromState(state))
	lines, _ := promptOverlayContent(prompt)
	footerSpecs := promptFooterActionSpecs(prompt)
	width, height := overlayViewport(TermSize{Width: 60, Height: FrameBodyHeight(24)})
	layout := buildPickerCardLayout(width, height, len(lines), true)
	_, expected := layoutOverlayFooterActions(footerSpecs, workbench.Rect{
		X: layout.cardX + 1,
		Y: pickerFooterRowY(layout),
		W: layout.innerWidth,
		H: 1,
	})
	var gotFooter []HitRegion
	for _, region := range regions {
		if region.Kind == HitRegionPromptSubmit || region.Kind == HitRegionPromptCancel || region.Kind == HitRegionOverlayFooterAction {
			gotFooter = append(gotFooter, region)
		}
	}
	if len(gotFooter) != len(expected) {
		t.Fatalf("expected %d prompt footer regions, got %#v", len(expected), gotFooter)
	}
	for i, want := range expected {
		if gotFooter[i].Action.Kind != want.Action.Kind {
			t.Fatalf("expected prompt footer action[%d]=%q, got %q", i, want.Action.Kind, gotFooter[i].Action.Kind)
		}
		if gotFooter[i].Rect != want.Rect {
			t.Fatalf("expected prompt footer rect[%d]=%#v, got %#v", i, want.Rect, gotFooter[i].Rect)
		}
		switch want.Action.Kind {
		case input.ActionSubmitPrompt:
			if gotFooter[i].Kind != HitRegionPromptSubmit {
				t.Fatalf("expected submit region kind %q, got %q", HitRegionPromptSubmit, gotFooter[i].Kind)
			}
		case input.ActionCancelMode:
			if gotFooter[i].Kind != HitRegionPromptCancel {
				t.Fatalf("expected cancel region kind %q, got %q", HitRegionPromptCancel, gotFooter[i].Kind)
			}
		default:
			if gotFooter[i].Kind != HitRegionOverlayFooterAction {
				t.Fatalf("expected generic footer region kind %q, got %q", HitRegionOverlayFooterAction, gotFooter[i].Kind)
			}
		}
	}
}

func TestOverlayHitRegionsPickerRowLimitMatchesRenderedListHeight(t *testing.T) {
	items := make([]modal.PickerItem, 0, 20)
	for i := 0; i < 20; i++ {
		items = append(items, modal.PickerItem{TerminalID: "term"})
	}
	state := VisibleRenderState{
		TermSize: TermSize{Width: 100, Height: 28},
		Overlay: VisibleOverlay{
			Kind:   VisibleOverlayPicker,
			Picker: &modal.PickerState{Items: items},
		},
	}
	regions := OverlayHitRegions(vmFromState(state))
	itemRegions := collectRegionsByKind(regions, HitRegionPickerItem)
	layout := buildPickerCardLayout(100, FrameBodyHeight(28), len(items), false)
	if len(itemRegions) != layout.listHeight {
		t.Fatalf("expected picker rows clipped to list height %d, got %d", layout.listHeight, len(itemRegions))
	}
}

func TestOverlayHitRegionsPickerHasNoFooterActionRegions(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 100, Height: 28},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayPicker,
			Picker: &modal.PickerState{
				Items: []modal.PickerItem{
					{TerminalID: "term-1", Name: "shell-1"},
				},
			},
		},
	}
	regions := OverlayHitRegions(vmFromState(state))
	actionRegions := collectRegionsByKind(regions, HitRegionOverlayFooterAction)
	if len(actionRegions) != 0 {
		t.Fatalf("expected picker overlay footer actions to be hidden, got %#v", actionRegions)
	}
}

func TestOverlayHitRegionsWorkspacePickerFooterActionsExposeActionOrder(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 140, Height: 30},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayWorkspacePicker,
			WorkspacePicker: &modal.WorkspacePickerState{
				Items: []modal.WorkspacePickerItem{
					{Name: "main"},
					{Name: "dev"},
				},
			},
		},
	}
	regions := OverlayHitRegions(vmFromState(state))
	actionRegions := collectRegionsByKind(regions, HitRegionOverlayFooterAction)
	layout := buildWorkbenchTreeCardLayout(140, FrameBodyHeight(30), 3, 0)
	_, expected := layoutOverlayFooterActions(workbenchTreeActionSpecs(state.Overlay.WorkspacePicker.SelectedItem()), workbench.Rect{
		X: layout.actionRowRect.X,
		Y: layout.actionRowRect.Y,
		W: layout.actionRowRect.W,
		H: 1,
	})
	if len(actionRegions) != len(expected) {
		t.Fatalf("expected %d workspace footer regions, got %#v", len(expected), actionRegions)
	}
	for i, want := range expected {
		if actionRegions[i].Action.Kind != want.Action.Kind {
			t.Fatalf("expected workspace footer action[%d]=%q, got %q", i, want.Action.Kind, actionRegions[i].Action.Kind)
		}
		if actionRegions[i].Rect != want.Rect {
			t.Fatalf("expected workspace footer rect[%d]=%#v, got %#v", i, want.Rect, actionRegions[i].Rect)
		}
	}
}

func TestTerminalPoolHitRegionsRowsFollowGroupedListLayout(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 80, Height: 20},
		Surface: VisibleSurface{
			Kind: VisibleSurfaceTerminalPool,
			TerminalPool: &modal.TerminalManagerState{
				Items: []modal.PickerItem{
					{TerminalID: "term-1", State: "running"},
					{TerminalID: "term-2", State: "running"},
					{TerminalID: "term-3", State: "exited"},
				},
			},
		},
	}

	regions := TerminalPoolHitRegions(vmFromState(state))
	itemRegions := collectRegionsByKind(regions, HitRegionTerminalPoolItem)
	if len(itemRegions) != 3 {
		t.Fatalf("expected 3 terminal pool item regions, got %#v", itemRegions)
	}
	if itemRegions[0].Rect.Y != 4 || itemRegions[1].Rect.Y != 5 || itemRegions[2].Rect.Y != 7 {
		t.Fatalf("unexpected terminal pool row y positions: %#v", itemRegions)
	}
	for index, region := range itemRegions {
		if region.ItemIndex != index {
			t.Fatalf("expected item index %d, got %#v", index, region)
		}
	}
}

func TestTerminalPoolHitRegionsIncludeFooterActions(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 120, Height: 24},
		Surface: VisibleSurface{
			Kind: VisibleSurfaceTerminalPool,
			TerminalPool: &modal.TerminalManagerState{
				Items: []modal.PickerItem{
					{TerminalID: "term-1", State: "running"},
				},
			},
		},
	}

	regions := TerminalPoolHitRegions(vmFromState(state))
	footerRegions := collectRegionsByKind(regions, HitRegionTerminalPoolAction)
	wantActions := []input.ActionKind{
		input.ActionSubmitPrompt,
		input.ActionAttachTab,
		input.ActionAttachFloating,
		input.ActionEditTerminal,
		input.ActionKillTerminal,
	}
	if len(footerRegions) != len(wantActions) {
		t.Fatalf("expected %d footer regions, got %#v", len(wantActions), footerRegions)
	}

	layout := buildTerminalPoolPageLayout(state.Surface.TerminalPool, state.TermSize.Width, FrameBodyHeight(state.TermSize.Height))
	if len(layout.footerActions) == 0 {
		t.Fatalf("expected terminal pool footer actions in layout")
	}
	footerY := layout.footerActions[0].rect.Y
	lastX := -1
	for index, region := range footerRegions {
		if region.Action.Kind != wantActions[index] {
			t.Fatalf("footer region[%d] action=%q, want %q", index, region.Action.Kind, wantActions[index])
		}
		if region.Rect.Y != footerY || region.Rect.H != 1 {
			t.Fatalf("footer region[%d] rect=%+v, want y=%d h=1", index, region.Rect, footerY)
		}
		if region.Rect.X <= lastX {
			t.Fatalf("footer regions must be ordered left-to-right, got %#v", footerRegions)
		}
		lastX = region.Rect.X
	}
}

func TestTerminalPoolHitRegionsIncludeQueryInput(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 80, Height: 20},
		Surface: VisibleSurface{
			Kind: VisibleSurfaceTerminalPool,
			TerminalPool: &modal.TerminalManagerState{
				Items: []modal.PickerItem{{TerminalID: "term-1", State: "running"}},
			},
		},
	}

	regions := TerminalPoolHitRegions(vmFromState(state))
	queryRegions := collectRegionsByKind(regions, HitRegionOverlayQueryInput)
	if len(queryRegions) != 1 {
		t.Fatalf("expected one terminal pool query region, got %#v", queryRegions)
	}

	layout := buildTerminalPoolPageLayout(state.Surface.TerminalPool, state.TermSize.Width, FrameBodyHeight(state.TermSize.Height))
	if got, want := queryRegions[0].Rect, layout.queryRect; got != want {
		t.Fatalf("terminal pool query rect=%+v, want %+v", got, want)
	}
}

func TestTerminalPoolHitRegionsClipsFooterActionsWhenWidthIsTight(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 15, Height: 20},
		Surface: VisibleSurface{
			Kind: VisibleSurfaceTerminalPool,
			TerminalPool: &modal.TerminalManagerState{
				Items: []modal.PickerItem{{TerminalID: "term-1", State: "running"}},
			},
		},
	}

	regions := TerminalPoolHitRegions(vmFromState(state))
	footerRegions := collectRegionsByKind(regions, HitRegionTerminalPoolAction)
	if len(footerRegions) != 2 {
		t.Fatalf("expected first 2 footer actions to fit, got %#v", footerRegions)
	}
	if footerRegions[0].Action.Kind != input.ActionSubmitPrompt || footerRegions[1].Action.Kind != input.ActionAttachTab {
		t.Fatalf("expected clipped footer actions to keep stable prefix order, got %#v", footerRegions)
	}
}
