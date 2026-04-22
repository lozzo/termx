package render

type bodyProjectionOptions struct {
	ConfirmPaneID        string
	Chrome               UIChromeConfig
	EmptySelection       RenderPaneSelectionVM
	ExitedSelection      RenderPaneSelectionVM
	ExitedSelectionPulse bool
	SnapshotOverride     RenderSnapshotOverrideVM
	CopyMode             RenderCopyModeVM
	ImmersiveZoom        bool
}

func bodyProjectionOptionsForVM(vm RenderVM, exitedSelectionPulse bool) bodyProjectionOptions {
	return bodyProjectionOptions{
		ConfirmPaneID:        vm.Body.OwnerConfirmPaneID,
		Chrome:               normalizeUIChromeConfig(vm.Chrome),
		EmptySelection:       vm.Body.EmptySelection,
		ExitedSelection:      vm.Body.ExitedSelection,
		ExitedSelectionPulse: exitedSelectionPulse,
		SnapshotOverride:     vm.Body.SnapshotOverride,
		CopyMode:             vm.Body.CopyMode,
		ImmersiveZoom:        immersiveZoomActiveVM(vm),
	}
}
