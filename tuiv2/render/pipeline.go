package render

import (
	"strings"

	"github.com/lozzow/termx/perftrace"
)

func renderResultWithCoordinator(coordinator *Coordinator, vm RenderVM) RenderResult {
	if vm.Workbench == nil {
		return RenderResult{
			Lines:  []string{"tuiv2"},
			Cursor: hideCursorANSI(),
		}
	}

	immersiveZoom := immersiveZoomActiveVM(vm)
	bodyCursorOffsetY := TopChromeRows
	if immersiveZoom {
		bodyCursorOffsetY = 0
	}

	bodyHeight := FrameBodyHeight(vm.TermSize.Height)
	if immersiveZoom {
		bodyHeight = maxInt(1, vm.TermSize.Height)
	}

	tabBar := ""
	statusBar := ""
	if !immersiveZoom {
		if coordinator != nil {
			tabBar = coordinator.renderTabBarCached(vm)
			statusBar = coordinator.renderStatusBarCached(vm)
		} else {
			tabBar = renderTabBarVM(vm)
			statusBar = renderStatusBarVM(vm)
		}
	}

	overlaySize := TermSize{Width: vm.TermSize.Width, Height: bodyHeight}
	overlayCursorVisible := true
	if coordinator != nil {
		coordinator.mu.Lock()
		overlayCursorVisible = coordinator.cursorBlinkVisible
		coordinator.mu.Unlock()
	}

	body := renderedBody{cursor: hideCursorANSI()}
	if overlay := renderActiveOverlayVMWithCursor(vm, overlaySize, bodyCursorOffsetY, overlayCursorVisible); overlay.Content() != "" && overlayIsOpaque(vm.Overlay) {
		perftrace.Count("render.body.path.overlay_opaque", 0)
		body = overlay
	} else {
		body = renderBodyFrameWithCoordinatorVM(coordinator, vm, vm.TermSize.Width, bodyHeight)
		if overlay.Content() != "" {
			body.content = compositeOverlay(body.Content(), overlay.Content(), overlaySize)
			body.lines = nil
			body.cursor = overlay.cursor
			body.blink = overlay.blink
		}
	}

	bodyLines := body.lines
	if len(bodyLines) == 0 {
		bodyLines = body.Lines()
	}
	lines := make([]string, 0, len(bodyLines)+2)
	if !immersiveZoom {
		lines = append(lines, tabBar)
	}
	lines = append(lines, bodyLines...)
	if !immersiveZoom {
		lines = append(lines, statusBar)
	}
	if coordinator != nil {
		coordinator.mu.Lock()
		if !body.blink {
			coordinator.cursorBlinkVisible = true
		}
		coordinator.mu.Unlock()
	}
	return RenderResult{
		Lines:  lines,
		Cursor: body.cursor,
		Blink:  body.blink,
		Meta:   composeRenderMetadata(vm.TermSize.Width, len(lines), immersiveZoom, body.meta),
	}
}

func renderBodyFrameWithCoordinatorVM(coordinator *Coordinator, vm RenderVM, width, height int) renderedBody {
	finish := perftrace.Measure("render.body")
	defer finish(0)
	if width <= 0 || height <= 0 {
		return renderedBody{}
	}
	cursorOffsetY := TopChromeRows
	if immersiveZoomActiveVM(vm) {
		cursorOffsetY = 0
	}
	if vm.Surface.Kind == VisibleSurfaceTerminalPool && vm.Surface.TerminalPool != nil {
		perftrace.Count("render.body.path.terminal_pool", 0)
		cursorVisible := true
		if coordinator != nil {
			coordinator.mu.Lock()
			cursorVisible = coordinator.cursorBlinkVisible
			coordinator.mu.Unlock()
		}
		return renderTerminalPoolPageWithCursor(vm.Surface.TerminalPool, vm.Runtime, TermSize{Width: width, Height: height}, cursorOffsetY, cursorVisible)
	}
	if vm.Workbench == nil {
		perftrace.Count("render.body.path.empty_workbench", 0)
		return renderedBody{content: strings.Repeat("\n", maxInt(0, height-1))}
	}
	activeTabIdx := vm.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(vm.Workbench.Tabs) {
		perftrace.Count("render.body.path.empty_no_tabs", 0)
		return renderEmptyWorkbenchBodyVM(vm, width, height, emptyWorkbenchNoTabs)
	}
	tab := vm.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		perftrace.Count("render.body.path.empty_no_panes", 0)
		return renderEmptyWorkbenchBodyVM(vm, width, height, emptyWorkbenchNoPanes)
	}
	lookup := newRuntimeLookup(vm.Runtime)
	exitedSelectionPulse := true
	if coordinator != nil {
		coordinator.mu.Lock()
		exitedSelectionPulse = coordinator.cursorBlinkVisible
		coordinator.mu.Unlock()
	}
	entries := paneEntriesForTab(tab, vm.Workbench.FloatingPanes, width, height, lookup, bodyProjectionOptionsForVM(vm, exitedSelectionPulse), uiThemeForRuntime(vm.Runtime))
	if body, ok := renderAltScreenFastPathVM(vm, entries, cursorOffsetY); ok {
		perftrace.Count("render.body.path.alt_screen_fast_path", 0)
		return body
	}
	perftrace.Count("render.body.path.canvas", 0)
	canvas := renderBodyCanvas(coordinator, vm.Runtime, immersiveZoomActiveVM(vm), entries, width, height)
	return renderedBody{
		lines:  canvas.cachedContentLines(),
		cursor: canvas.cursorANSI(),
		blink:  canvas.syntheticCursorBlink,
		meta:   &PresentMetadata{OwnerMap: canvas.ownerMap()},
	}
}

const (
	renderOwnerTopChrome    uint32 = 1
	renderOwnerBottomChrome uint32 = 2
)

func composeRenderMetadata(width, height int, immersiveZoom bool, bodyMeta *PresentMetadata) *PresentMetadata {
	if bodyMeta == nil || len(bodyMeta.OwnerMap) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	meta := &PresentMetadata{
		OwnerMap: make([][]uint32, height),
	}
	for y := 0; y < height; y++ {
		meta.OwnerMap[y] = make([]uint32, width)
	}
	offsetY := 0
	if !immersiveZoom {
		offsetY = 1
		for x := 0; x < width; x++ {
			meta.OwnerMap[0][x] = renderOwnerTopChrome
			meta.OwnerMap[height-1][x] = renderOwnerBottomChrome
		}
	}
	for y := range bodyMeta.OwnerMap {
		targetY := offsetY + y
		if targetY < 0 || targetY >= height {
			continue
		}
		copy(meta.OwnerMap[targetY], bodyMeta.OwnerMap[y])
	}
	return meta
}

func renderVMNeedsCursorBlink(vm RenderVM) bool {
	if overlayVMNeedsCursorBlink(vm.Overlay) || terminalPoolVMNeedsCursorBlink(vm.Surface) {
		return true
	}
	if vm.Overlay.Kind != VisibleOverlayNone || vm.Surface.Kind != VisibleSurfaceWorkbench {
		return false
	}
	if vm.Workbench == nil || vm.Runtime == nil {
		return false
	}
	activeTabIdx := vm.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(vm.Workbench.Tabs) {
		return false
	}
	tab := vm.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		return false
	}
	width := vm.TermSize.Width
	height := FrameBodyHeight(vm.TermSize.Height)
	if immersiveZoomActiveVM(vm) {
		height = maxInt(1, vm.TermSize.Height)
	}
	if width <= 0 || height <= 0 {
		return false
	}
	if activeExitedPaneHasRecoverySelectionVM(vm) {
		return true
	}
	return false
}

func overlayVMNeedsCursorBlink(overlay RenderOverlayVM) bool {
	switch overlay.Kind {
	case VisibleOverlayPrompt, VisibleOverlayPicker, VisibleOverlayWorkspacePicker, VisibleOverlayTerminalManager:
		return true
	default:
		return false
	}
}

func terminalPoolVMNeedsCursorBlink(surface RenderSurfaceVM) bool {
	return surface.Kind == VisibleSurfaceTerminalPool && surface.TerminalPool != nil
}

func activeExitedPaneHasRecoverySelectionVM(vm RenderVM) bool {
	if vm.Workbench == nil || vm.Runtime == nil || vm.Body.ExitedSelection.PaneID == "" {
		return false
	}
	activeTabIdx := vm.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(vm.Workbench.Tabs) {
		return false
	}
	tab := vm.Workbench.Tabs[activeTabIdx]
	if tab.ActivePaneID == "" || tab.ActivePaneID != vm.Body.ExitedSelection.PaneID {
		return false
	}
	lookup := newRuntimeLookup(vm.Runtime)
	for i := range tab.Panes {
		pane := &tab.Panes[i]
		if pane.ID != tab.ActivePaneID || pane.TerminalID == "" {
			continue
		}
		terminal := findVisibleTerminalWithLookup(lookup, pane.TerminalID)
		return terminal != nil && terminal.State == "exited"
	}
	return false
}

func overlayIsOpaque(overlay RenderOverlayVM) bool {
	switch overlay.Kind {
	case VisibleOverlayPrompt, VisibleOverlayPicker, VisibleOverlayWorkspacePicker, VisibleOverlayTerminalManager, VisibleOverlayHelp, VisibleOverlayFloatingOverview:
		return true
	default:
		return false
	}
}

func (b renderedBody) Lines() []string {
	if len(b.lines) > 0 {
		out := make([]string, len(b.lines))
		copy(out, b.lines)
		return out
	}
	if b.content == "" {
		return []string{""}
	}
	return strings.Split(b.content, "\n")
}
