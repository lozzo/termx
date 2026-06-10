package render

import (
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestActionSpecCatalogIsSingleSourceForRenderedActions(t *testing.T) {
	specs := ActionSpecCatalog()
	if len(specs) == 0 {
		t.Fatal("action spec catalog must not be empty")
	}
	seen := map[ActionID]struct{}{}
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
		seen[spec.ID] = struct{}{}
	}
	for _, id := range []ActionID{
		ActionPaneFocus,
		ActionPaneResize,
		ActionPaneSplitDown,
		ActionPaneSplitRight,
		ActionPaneZoom,
		ActionPaneClose,
		ActionPaneFooterSplit,
		ActionPaneFooterClose,
		ActionPaneFooterFocus,
		ActionPaneFooterZoom,
		ActionResizeLeft,
		ActionResizeRight,
		ActionResizeUp,
		ActionResizeDown,
		ActionResizeBalance,
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
		ActionFloatingMoveDrag,
		ActionFloatingResizeDrag,
		ActionEmptyAttach,
		ActionEmptyCreate,
		ActionEmptyManager,
		ActionEmptyClose,
		ActionExitedRestart,
		ActionExitedReconnect,
		ActionExitedClose,
		ActionPickerAttach,
		ActionPickerNew,
		ActionPoolSelect,
		ActionPoolAttach,
		ActionPoolEdit,
		ActionPoolKill,
		ActionWorkbenchSelect,
		ActionWorkbenchOpen,
		ActionWorkbenchRename,
		ActionWorkbenchNew,
		ActionWorkbenchDelete,
		ActionPromptSubmit,
		ActionPromptCancel,
		ActionHelpClose,
	} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("action id %q must be registered in catalog", id)
		}
	}
	if got := ActionIDCatalog(); len(got) != len(specs) {
		t.Fatalf("action id catalog must be derived from specs got=%d want=%d", len(got), len(specs))
	}
}

func TestActionSpecCatalogClassifiesVisibleClickableAndDispatchActions(t *testing.T) {
	assertSpec := func(id ActionID, surface ActionSurface, dispatch ActionDispatch) ActionSpec {
		t.Helper()
		spec, ok := ActionSpecByID(id)
		if !ok {
			t.Fatalf("missing action spec %q", id)
		}
		if !spec.HasSurface(surface) {
			t.Fatalf("action %q must include surface %q, got %#v", id, surface, spec.Surfaces)
		}
		if spec.Dispatch != dispatch {
			t.Fatalf("action %q dispatch got=%q want=%q", id, spec.Dispatch, dispatch)
		}
		return spec
	}

	footer := assertSpec(ActionFooterPaneMode, ActionSurfaceFooter, ActionDispatchApp)
	if footer.FooterKey != "^P" || footer.FooterLabel != "pane" || footer.FooterStyle != StyleFooterKeyPane {
		t.Fatalf("footer action should carry default token metadata, got %#v", footer)
	}

	pane := assertSpec(ActionPaneClose, ActionSurfacePaneChrome, ActionDispatchPaneCommand)
	if pane.ChromeGlyph == "" || !pane.Danger {
		t.Fatalf("pane close action should carry chrome glyph and danger metadata, got %#v", pane)
	}

	floating := assertSpec(ActionFloatingClose, ActionSurfaceFloatingChrome, ActionDispatchApp)
	if floating.ChromeGlyph == "" || !floating.Danger {
		t.Fatalf("floating close action should carry chrome glyph and danger metadata, got %#v", floating)
	}

	copyOlder := assertSpec(ActionCopyOlder, ActionSurfaceFooter, ActionDispatchApp)
	if copyOlder.FooterKey != "pgup" || copyOlder.HelpLabel == "" {
		t.Fatalf("copy older action should carry footer and help metadata, got %#v", copyOlder)
	}

	assertSpec(ActionHelpClose, ActionSurfaceHelp, ActionDispatchApp)
	assertSpec(ActionPoolAttach, ActionSurfaceContent, ActionDispatchApp)
	assertSpec(ActionPromptSubmit, ActionSurfaceContent, ActionDispatchApp)
}

func TestActionSpecCatalogKeepsInputOnlyActionsSeparate(t *testing.T) {
	for _, id := range []ActionID{
		ActionPromptOpen,
		ActionHelpOpen,
		ActionFloatingMoveLeft,
		ActionFloatingMoveRight,
		ActionFloatingMoveUp,
		ActionFloatingMoveDown,
		ActionFloatingNarrow,
		ActionFloatingWide,
		ActionFloatingShort,
		ActionFloatingTall,
	} {
		spec, ok := ActionSpecByID(id)
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

func TestActionSpecCatalogCoversVisibleVMAndHitRegionActions(t *testing.T) {
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
		spec := assertRegisteredAction(t, region.ActionID, "")
		if spec.Dispatch == ActionDispatchNone {
			t.Fatalf("clickable action %q must declare dispatch semantics", region.ActionID)
		}
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

func assertRegisteredAction(t *testing.T, id string, surface ActionSurface) ActionSpec {
	t.Helper()
	if id == "" {
		return ActionSpec{}
	}
	spec, ok := ActionSpecByIDString(id)
	if !ok {
		t.Fatalf("action %q must be registered in ActionSpecCatalog", id)
	}
	if surface != "" && !spec.HasSurface(surface) {
		t.Fatalf("action %q must include surface %q, got %#v", id, surface, spec.Surfaces)
	}
	return spec
}
