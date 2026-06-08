package render

import "testing"

func TestActionIDCatalogIsUniqueAndCoversRenderedActions(t *testing.T) {
	seen := map[ActionID]struct{}{}
	for _, id := range ActionIDCatalog() {
		if id == "" {
			t.Fatal("action id catalog must not contain empty id")
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate action id %q", id)
		}
		seen[id] = struct{}{}
	}
	for _, id := range []ActionID{
		ActionPaneFocus,
		ActionPaneResize,
		ActionPaneSplitDown,
		ActionPaneSplitRight,
		ActionPaneClose,
		ActionTabCreate,
		ActionTabClose,
		ActionFooterPaneMode,
		ActionFooterResizeMode,
		ActionFooterGlobalMode,
		ActionFooterPicker,
		ActionFooterToggleHeader,
		ActionFooterToggleFooter,
		ActionFooterOpenPool,
		ActionFooterOpenTree,
		ActionFooterCloseToast,
		ActionFooterClearToasts,
		ActionFloatingRaise,
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
}
