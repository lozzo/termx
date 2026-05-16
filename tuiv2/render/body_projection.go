package render

type bodyProjectionOptions struct {
	ConfirmPaneID        string
	Chrome               UIChromeConfig
	EmptySelection       RenderPaneSelectionVM
	ExitedSelection      RenderPaneSelectionVM
	ExitedSelectionPulse bool
	SnapshotOverride     RenderSnapshotOverrideVM
	CopyMode             RenderCopyModeVM
	CopyModes            map[string]RenderCopyModeVM
	FloatingDragPreview  RenderFloatingDragPreviewVM
	ImmersiveZoom        bool
}

func bodyProjectionOptionsForVM(vm RenderVM, exitedSelectionPulse bool) bodyProjectionOptions {
	copyModes := make(map[string]RenderCopyModeVM)
	for _, copyMode := range vm.Body.CopyModes {
		if copyMode.PaneID != "" {
			copyModes[copyMode.PaneID] = copyMode
		}
	}
	if vm.Body.CopyMode.PaneID != "" {
		copyModes[vm.Body.CopyMode.PaneID] = vm.Body.CopyMode
	}
	return bodyProjectionOptions{
		ConfirmPaneID:        vm.Body.OwnerConfirmPaneID,
		Chrome:               normalizeUIChromeConfig(vm.Chrome),
		EmptySelection:       vm.Body.EmptySelection,
		ExitedSelection:      vm.Body.ExitedSelection,
		ExitedSelectionPulse: exitedSelectionPulse,
		SnapshotOverride:     vm.Body.SnapshotOverride,
		CopyMode:             vm.Body.CopyMode,
		CopyModes:            copyModes,
		FloatingDragPreview:  vm.Body.FloatingDragPreview,
		ImmersiveZoom:        immersiveZoomActiveVM(vm),
	}
}

func (o bodyProjectionOptions) copyModeForPane(paneID string) (RenderCopyModeVM, bool) {
	if paneID == "" {
		return RenderCopyModeVM{}, false
	}
	if o.CopyModes != nil {
		if copyMode, ok := o.CopyModes[paneID]; ok && copyMode.PaneID != "" {
			return copyMode, true
		}
	}
	if o.CopyMode.PaneID == paneID {
		return o.CopyMode, true
	}
	return RenderCopyModeVM{}, false
}
