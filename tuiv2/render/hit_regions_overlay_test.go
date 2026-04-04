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

	regions := OverlayHitRegions(state)
	itemRegions := collectRegionsByKind(regions, HitRegionPickerItem)
	if len(itemRegions) != 3 {
		t.Fatalf("expected 3 picker item regions, got %#v", itemRegions)
	}
	layout := buildPickerCardLayout(100, FrameBodyHeight(30), 3, true)
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

	regions := OverlayHitRegions(state)
	itemRegions := collectRegionsByKind(regions, HitRegionWorkspaceItem)
	if len(itemRegions) != 2 {
		t.Fatalf("expected 2 workspace item regions, got %#v", itemRegions)
	}
	_, overlayHeight := overlayViewport(TermSize{Width: 96, Height: FrameBodyHeight(26)})
	layout := buildPickerCardLayout(96, overlayHeight, 2, true)
	if itemRegions[0].Rect.Y != layout.firstItemY || itemRegions[1].Rect.Y != layout.firstItemY+1 {
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
				Hint:  "[Enter] continue  [Esc] cancel",
			},
		},
	}
	promptRegions := OverlayHitRegions(promptState)
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
	lines, inputLine := promptOverlayContent(promptState.Overlay.Prompt)
	footerSpecs := promptFooterActionSpecs(promptState.Overlay.Prompt)
	width, height := overlayViewport(TermSize{Width: 90, Height: FrameBodyHeight(24)})
	layout := buildPickerCardLayout(width, height, len(lines), len(footerSpecs) > 0)
	expectedInput := promptInputRect(layout, promptState.Overlay.Prompt, inputLine)
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
	helpRegions := OverlayHitRegions(helpState)
	if cards := collectRegionsByKind(helpRegions, HitRegionHelpCard); len(cards) != 1 {
		t.Fatalf("expected one help card region, got %#v", cards)
	}
	if dismiss := collectRegionsByKind(helpRegions, HitRegionOverlayDismiss); len(dismiss) == 0 {
		t.Fatalf("expected help dismiss regions, got %#v", helpRegions)
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
	regions := OverlayHitRegions(state)
	queryRegions := collectRegionsByKind(regions, HitRegionOverlayQueryInput)
	if len(queryRegions) != 1 {
		t.Fatalf("expected one workspace query region, got %#v", queryRegions)
	}
	layout := buildPickerCardLayout(100, FrameBodyHeight(30), 1, true)
	if got, want := queryRegions[0].Rect, pickerQueryRowRect(layout); got != want {
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
	regions := OverlayHitRegions(state)
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
	regions := OverlayHitRegions(state)
	itemRegions := collectRegionsByKind(regions, HitRegionPickerItem)
	layout := buildPickerCardLayout(100, FrameBodyHeight(28), len(items), true)
	if len(itemRegions) != layout.listHeight {
		t.Fatalf("expected picker rows clipped to list height %d, got %d", layout.listHeight, len(itemRegions))
	}
}

func TestOverlayHitRegionsPickerFooterActionsHaveStablePrefixOrder(t *testing.T) {
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
	regions := OverlayHitRegions(state)
	actionRegions := collectRegionsByKind(regions, HitRegionOverlayFooterAction)
	if len(actionRegions) != 3 {
		t.Fatalf("expected clipped picker footer actions prefix (3), got %#v", actionRegions)
	}
	wantActions := []input.ActionKind{
		input.ActionSubmitPrompt,
		input.ActionPickerAttachSplit,
		input.ActionEditTerminal,
	}
	for index, region := range actionRegions {
		if region.Action.Kind != wantActions[index] {
			t.Fatalf("picker footer action[%d]=%q, want %q", index, region.Action.Kind, wantActions[index])
		}
	}
}

func TestOverlayHitRegionsWorkspacePickerFooterActionsExposeManagementSemantics(t *testing.T) {
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
	regions := OverlayHitRegions(state)
	actionRegions := collectRegionsByKind(regions, HitRegionOverlayFooterAction)
	fullActions := []input.ActionKind{
		input.ActionSubmitPrompt,
		input.ActionCreateWorkspace,
		input.ActionRenameWorkspace,
		input.ActionDeleteWorkspace,
		input.ActionPrevWorkspace,
		input.ActionNextWorkspace,
		input.ActionCancelMode,
	}
	layout := buildPickerCardLayout(140, FrameBodyHeight(30), 2, true)
	_, expected := layoutOverlayFooterActions(workspacePickerFooterActionSpecs(), workbench.Rect{W: layout.innerWidth, H: 1})
	wantActions := fullActions[:len(expected)]
	if len(actionRegions) != len(wantActions) {
		t.Fatalf("expected clipped workspace footer prefix of %d actions, got %#v", len(wantActions), actionRegions)
	}
	for index, region := range actionRegions {
		if region.Action.Kind != wantActions[index] {
			t.Fatalf("workspace footer action[%d]=%q, want %q", index, region.Action.Kind, wantActions[index])
		}
	}
}

func TestOverlayHitRegionsTerminalManagerFooterActionsExposeActionOrder(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 140, Height: 30},
		Overlay: VisibleOverlay{
			Kind: VisibleOverlayTerminalManager,
			TerminalManager: &modal.TerminalManagerState{
				Items: []modal.PickerItem{
					{TerminalID: "term-1", State: "running"},
				},
			},
		},
	}
	regions := OverlayHitRegions(state)
	actionRegions := collectRegionsByKind(regions, HitRegionOverlayFooterAction)
	fullActions := []input.ActionKind{
		input.ActionSubmitPrompt,
		input.ActionAttachTab,
		input.ActionAttachFloating,
		input.ActionEditTerminal,
		input.ActionKillTerminal,
		input.ActionCancelMode,
	}
	layout := buildPickerCardLayout(140, FrameBodyHeight(30), 1, true)
	_, expected := layoutOverlayFooterActions(terminalManagerFooterActionSpecs(), workbench.Rect{W: layout.innerWidth, H: 1})
	wantActions := fullActions[:len(expected)]
	if len(actionRegions) != len(wantActions) {
		t.Fatalf("expected clipped terminal manager footer prefix of %d actions, got %#v", len(wantActions), actionRegions)
	}
	for index, region := range actionRegions {
		if region.Action.Kind != wantActions[index] {
			t.Fatalf("terminal manager footer action[%d]=%q, want %q", index, region.Action.Kind, wantActions[index])
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

	regions := TerminalPoolHitRegions(state)
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

	regions := TerminalPoolHitRegions(state)
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

func TestTerminalPoolHitRegionsClipsFooterActionsWhenWidthIsTight(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 30, Height: 20},
		Surface: VisibleSurface{
			Kind: VisibleSurfaceTerminalPool,
			TerminalPool: &modal.TerminalManagerState{
				Items: []modal.PickerItem{{TerminalID: "term-1", State: "running"}},
			},
		},
	}

	regions := TerminalPoolHitRegions(state)
	footerRegions := collectRegionsByKind(regions, HitRegionTerminalPoolAction)
	if len(footerRegions) != 2 {
		t.Fatalf("expected first 2 footer actions to fit, got %#v", footerRegions)
	}
	if footerRegions[0].Action.Kind != input.ActionSubmitPrompt || footerRegions[1].Action.Kind != input.ActionAttachTab {
		t.Fatalf("expected clipped footer actions to keep stable prefix order, got %#v", footerRegions)
	}
}
