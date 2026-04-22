package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/shared"
)

func renderEmptyWorkbenchBodyVM(vm RenderVM, width, height int, kind emptyWorkbenchKind) renderedBody {
	canvas := newComposedCanvas(width, height)
	canvas.hostEmojiVS16Mode = emojiVariationSelectorModeForRuntime(vm.Runtime)
	theme := uiThemeForVM(vm)

	headline := "No tabs in this workspace"
	details := []string{
		"Ctrl-F open terminal picker",
		"Ctrl-T then c create a new tab",
	}
	if kind == emptyWorkbenchNoPanes {
		headline = "No panes in this tab"
		details = []string{
			"Ctrl-F create the first pane via terminal picker",
			"Ctrl-T then c create a fresh tab",
		}
	}

	lines := append([]string{headline}, details...)
	startY := maxInt(0, (height-len(lines))/2)
	for i, line := range lines {
		y := startY + i
		if y >= height {
			break
		}
		text := centerText(xansi.Truncate(line, width, ""), width)
		style := drawStyle{FG: theme.panelMuted}
		if i == 0 {
			style = drawStyle{FG: theme.panelText, Bold: true}
		}
		canvas.drawText(0, y, text, style)
	}

	return renderedBody{
		lines:  canvas.cachedContentLines(),
		cursor: hideCursorANSI(),
		meta:   solidPresentMetadata(width, height, renderOwnerEmptyWorkbench),
	}
}

func emojiVariationSelectorModeForRuntime(runtimeState *VisibleRuntimeStateProxy) shared.AmbiguousEmojiVariationSelectorMode {
	if runtimeState == nil {
		return shared.AmbiguousEmojiVariationSelectorStrip
	}
	switch runtimeState.HostEmojiVS16Mode {
	case shared.AmbiguousEmojiVariationSelectorRaw:
		return shared.AmbiguousEmojiVariationSelectorRaw
	case shared.AmbiguousEmojiVariationSelectorAdvance, shared.AmbiguousEmojiVariationSelectorStrip:
		// 中文说明：即便探测分类里有 advance，真正进入 Bubble Tea 行渲染时
		// 也要合并到 strip，因为这里不能安全依赖行内光标移动来补齐宽度。
		return shared.AmbiguousEmojiVariationSelectorStrip
	default:
		return shared.AmbiguousEmojiVariationSelectorStrip
	}
}

func renderPageLinesWithPinnedFooter(headerLines, contentLines []string, footerLine string, width, height int) []string {
	if height <= 0 {
		return nil
	}
	if height == 1 {
		return []string{forceWidthANSIOverlay(footerLine, width)}
	}

	lines := make([]string, 0, height)
	lines = append(lines, headerLines...)
	lines = append(lines, contentLines...)
	if len(lines) > height-1 {
		lines = lines[:height-1]
	}
	for len(lines) < height-1 {
		lines = append(lines, forceWidthANSIOverlay("", width))
	}
	lines = append(lines, forceWidthANSIOverlay(footerLine, width))
	return lines
}

func immersiveZoomActive(state VisibleRenderState) bool {
	return immersiveZoomActiveVM(RenderVMFromVisibleState(state))
}

func immersiveZoomActiveVM(vm RenderVM) bool {
	if vm.Surface.Kind != VisibleSurfaceWorkbench || vm.Workbench == nil {
		return false
	}
	activeTab := vm.Workbench.ActiveTab
	if activeTab < 0 || activeTab >= len(vm.Workbench.Tabs) {
		return false
	}
	return strings.TrimSpace(vm.Workbench.Tabs[activeTab].ZoomedPaneID) != ""
}
