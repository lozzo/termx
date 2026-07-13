package render

import (
	"testing"

	actiondomain "github.com/lozzow/termx/tui/action"
	"github.com/lozzow/termx/tui/state"
)

func TestProjectionCatalogIsSingleSourceForRenderedActions(t *testing.T) {
	specs := ProjectionCatalog()
	if len(specs) == 0 {
		t.Fatal("action spec catalog must not be empty")
	}
	seen := map[ProjectionID]struct{}{}
	for _, spec := range specs {
		if spec.ID == "" {
			t.Fatal("action id catalog must not contain empty id")
		}
		if _, ok := seen[spec.ID]; ok {
			t.Fatalf("duplicate action id %q", spec.ID)
		}
		if len(spec.Surfaces) == 0 {
			t.Fatalf("action spec %q must declare at least one surface", spec.ID)
		}
		if spec.CanonicalActionID != "" {
			if canonical, ok := actiondomain.SpecByID(spec.CanonicalActionID); !ok || canonical.ID != spec.CanonicalActionID {
				t.Fatalf("projection %q references non-canonical action %q", spec.ID, spec.CanonicalActionID)
			}
		}
		seen[spec.ID] = struct{}{}
	}
	for _, id := range []ProjectionID{
		ActionPaneFocus,
		ActionPaneResize,
		ActionPaneSplitDown,
		ActionPaneSplitRight,
		ActionPaneZoom,
		ActionPaneClose,
		ActionPaneFooterClose,
		ActionPaneFooterFocus,
		ActionPaneFooterZoom,
		ActionPaneFooterBalance,
		ActionPaneFooterCard,
		ActionPaneFooterSplitLine,
		ActionResizeLeft,
		ActionResizeRight,
		ActionResizeUp,
		ActionResizeDown,
		ActionResizeBalance,
		ActionResizeLayoutLock,
		ActionResizeLayoutToggle,
		ActionResizeLayoutPan,
		ActionResizeLayoutAlign,
		ActionResizeLayoutCenter,
		ActionResizeLayoutReset,
		ActionCopyOlder,
		ActionTabCreate,
		ActionTabSwitch,
		ActionTabClose,
		ActionTabRename,
		ActionTabPrevious,
		ActionTabNext,
		ActionFooterPaneMode,
		ActionFooterResizeMode,
		ActionFooterTabMode,
		ActionFooterWorkspaceMode,
		ActionFooterFloatingMode,
		ActionFooterCopyMode,
		ActionFooterGlobalMode,
		ActionFooterPicker,
		ActionFooterToggleHeader,
		ActionFooterToggleFooter,
		ActionFooterOpenPool,
		ActionFooterOpenTree,
		ActionFooterCloseToast,
		ActionFooterClearToasts,
		ActionFooterNewWorkspace,
		ActionFooterRenameWorkspace,
		ActionFooterPreviousWorkspace,
		ActionFooterNextWorkspace,
		ActionFooterDeleteWorkspace,
		ActionFloatingRaise,
		ActionFloatingNew,
		ActionFloatingClose,
		ActionFloatingPick,
		ActionFloatingTakeOwner,
		ActionFloatingCenter,
		ActionFloatingCollapse,
		ActionFloatingMoveDrag,
		ActionFloatingResizeDrag,
		ActionEmptyAttach,
		ActionEmptyCreate,
		ActionEmptyManager,
		ActionEmptyClose,
		ActionExitedRestart,
		ActionExitedReconnect,
		ActionExitedClose,
		ActionDisconnectedReconnect,
		ActionDisconnectedDisconnect,
		ActionPickerAttach,
		ActionPickerNew,
		ActionPoolSelect,
		ActionPoolAttach,
		ActionPoolAttachTab,
		ActionPoolAttachFloat,
		ActionPoolRestart,
		ActionPoolEdit,
		ActionPoolKill,
		ActionPoolDelete,
		ActionWorkbenchSelect,
		ActionWorkbenchOpen,
		ActionWorkbenchRename,
		ActionWorkbenchNew,
		ActionWorkbenchDelete,
		ActionClipboardHistoryOpen,
		ActionClipboardHistorySelect,
		ActionClipboardHistoryPaste,
		ActionClipboardHistoryNew,
		ActionClipboardHistoryEdit,
		ActionClipboardHistoryDelete,
		ActionClipboardHistoryDividerDrag,
		ActionPromptSubmit,
		ActionPromptCancel,
		ActionHelpClose,
	} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("action id %q must be registered in catalog", id)
		}
	}
	if got := ProjectionActionIDs(); len(got) != len(specs) {
		t.Fatalf("action id catalog must be derived from specs got=%d want=%d", len(got), len(specs))
	}
}

func TestProjectionCatalogClassifiesVisibleActions(t *testing.T) {
	assertSpec := func(id ProjectionID, surface ActionSurface) ProjectionSpec {
		t.Helper()
		spec, ok := ProjectionByID(id)
		if !ok {
			t.Fatalf("missing action spec %q", id)
		}
		if !spec.HasSurface(surface) {
			t.Fatalf("action %q must include surface %q, got %#v", id, surface, spec.Surfaces)
		}
		return spec
	}

	footer := assertSpec(ActionFooterPaneMode, ActionSurfaceFooter)
	if footer.FooterKey != "^P" || footer.FooterLabel != "PANE" || footer.FooterStyle != StyleFooterKeyPane {
		t.Fatalf("footer action should carry default token metadata, got %#v", footer)
	}

	pane := assertSpec(ActionPaneClose, ActionSurfacePaneChrome)
	if pane.ChromeGlyph == "" || !pane.Danger {
		t.Fatalf("pane close action should carry chrome glyph and danger metadata, got %#v", pane)
	}

	floating := assertSpec(ActionFloatingClose, ActionSurfaceFloatingChrome)
	if floating.ChromeGlyph == "" || !floating.Danger {
		t.Fatalf("floating close action should carry chrome glyph and danger metadata, got %#v", floating)
	}

	copyOlder := assertSpec(ActionCopyOlder, ActionSurfaceFooter)
	if copyOlder.FooterKey != "PgUp" || copyOlder.HelpLabel == "" {
		t.Fatalf("copy older action should carry footer and help metadata, got %#v", copyOlder)
	}

	helpOpen := assertSpec(ActionHelpOpen, ActionSurfaceFooter)
	if helpOpen.FooterKey != "?" || helpOpen.FooterLabel != "HELP" {
		t.Fatalf("help open action should carry global footer metadata, got %#v", helpOpen)
	}
	assertSpec(ActionHelpClose, ActionSurfaceHelp)
	assertSpec(ActionPoolAttach, ActionSurfaceContent)
	assertSpec(ActionPoolAttachTab, ActionSurfaceContent)
	assertSpec(ActionPoolAttachFloat, ActionSurfaceContent)
	assertSpec(ActionPoolRestart, ActionSurfaceInput)
	assertSpec(ActionPoolEdit, ActionSurfaceHelp)
	assertSpec(ActionPoolDelete, ActionSurfaceContent)
	assertSpec(ActionClipboardHistoryOpen, ActionSurfaceFooter)
	assertSpec(ActionClipboardHistoryPaste, ActionSurfaceContent)
	assertSpec(ActionClipboardHistoryNew, ActionSurfaceContent)
	assertSpec(ActionClipboardHistoryDividerDrag, ActionSurfaceContent)
	assertSpec(ActionPromptSubmit, ActionSurfaceContent)
}

func TestLegacyProjectionIDsDoNotBecomeCanonicalActionIdentities(t *testing.T) {
	chrome, _ := ProjectionByID(ActionPaneClose)
	footer, _ := ProjectionByID(ActionPaneFooterClose)
	if chrome.CanonicalActionID != "panel.close" || footer.CanonicalActionID != "panel.close" {
		t.Fatalf("pane close projections must share one canonical action: chrome=%#v footer=%#v", chrome, footer)
	}
	if _, ok := actiondomain.SpecByID(actiondomain.ID(ActionPaneFooterClose)); ok {
		t.Fatalf("visual projection %q must not be registered as canonical action", ActionPaneFooterClose)
	}
}

func TestProjectionByIDKeepsDynamicChromeGlyphsCurrent(t *testing.T) {
	t.Cleanup(ResetPaneChromeGlyphs)
	SetPaneChromeGlyphs(PaneChromeGlyphs{Close: "❌", Zoom: "🔎", SplitVertical: "↕", SplitHorizontal: "↔"})

	for _, tt := range []struct {
		id   ProjectionID
		want string
	}{
		{id: ActionPaneClose, want: paneChromeCloseActionText()},
		{id: ActionFloatingClose, want: paneChromeCloseGlyph()},
		{id: ActionPaneZoom, want: paneChromeZoomGlyph()},
		{id: ActionFloatingRaise, want: paneChromeZoomGlyph()},
		{id: ActionPaneSplitRight, want: paneChromeSplitVerticalActionText()},
		{id: ActionPaneSplitDown, want: paneChromeSplitHorizontalActionText()},
	} {
		spec, ok := ProjectionByID(tt.id)
		if !ok {
			t.Fatalf("missing action spec %q", tt.id)
		}
		if spec.ChromeGlyph != tt.want {
			t.Fatalf("action %q chrome glyph got=%q want=%q", tt.id, spec.ChromeGlyph, tt.want)
		}
	}
}

func TestProjectionCatalogKeepsInputOnlyActionsSeparate(t *testing.T) {
	for _, id := range []ProjectionID{
		ActionFloatingMoveLeft,
		ActionFloatingMoveRight,
		ActionFloatingMoveUp,
		ActionFloatingMoveDown,
		ActionFloatingNarrow,
		ActionFloatingWide,
		ActionFloatingShort,
		ActionFloatingTall,
	} {
		spec, ok := ProjectionByID(id)
		if !ok {
			t.Fatalf("missing input-only action spec %q", id)
		}
		if !spec.HasSurface(ActionSurfaceInput) {
			t.Fatalf("input-only action %q must declare input surface, got %#v", id, spec.Surfaces)
		}
		for _, surface := range []ActionSurface{ActionSurfaceFooter, ActionSurfacePaneChrome, ActionSurfaceFloatingChrome, ActionSurfaceContent} {
			if spec.HasSurface(surface) {
				t.Fatalf("input-only action %q must not declare visible surface %q", id, surface)
			}
		}
	}
}

func TestProjectionCatalogCoversVisibleVMAndHitRegionActions(t *testing.T) {
	root := actionCatalogRoot()
	vm := NewRenderVMBuilder().Build(root)
	plan := MeasureLayout(vm.Shell, Rect{W: 80, H: 24})

	for _, action := range vm.Shell.Footer.ActionTokens {
		assertRegisteredAction(t, action.ActionID, ActionSurfaceFooter)
	}
	for _, panel := range vm.Shell.Layout.Panels {
		for _, action := range panel.Chrome.Actions {
			assertRegisteredAction(t, action.ActionID, ActionSurfacePaneChrome)
		}
	}
	for _, floating := range vm.Shell.Layout.Floating {
		for _, action := range floatingChromeActionItems(floating.Rect.W) {
			assertRegisteredAction(t, action.ActionID, ActionSurfaceFloatingChrome)
		}
	}
	for _, region := range plan.HitRegions {
		if region.ActionID == "" {
			continue
		}
		assertRegisteredAction(t, region.ActionID, "")
	}
}

func actionCatalogRoot() state.Root {
	shell, _ := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-2", Title: "logs", Kind: state.PaneEmpty}, state.SplitDirectionVertical).
		ApplyFloatingCommand(state.FloatingCommand{
			Action:   state.FloatingCommandCreate,
			TargetID: "float-1",
			Pane:     state.PaneState{ID: "float-pane-1", Title: "floating", Kind: state.PaneEmpty},
			Title:    "floating",
			BoundsW:  80,
			BoundsH:  24,
		})
	return state.Root{
		Viewport: state.ViewportStore{Cols: 80, Rows: 24, Valid: true},
		Shell:    shell,
	}
}

func assertRegisteredAction(t *testing.T, id string, surface ActionSurface) ProjectionSpec {
	t.Helper()
	if id == "" {
		return ProjectionSpec{}
	}
	spec, ok := ProjectionByIDString(id)
	if !ok {
		t.Fatalf("action %q must be registered in ProjectionCatalog", id)
	}
	if surface != "" && !spec.HasSurface(surface) {
		t.Fatalf("action %q must include surface %q, got %#v", id, surface, spec.Surfaces)
	}
	return spec
}
