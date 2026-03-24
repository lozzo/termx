package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type screenShellViewRenderer interface {
	RenderShell(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, notices []btui.Notice, metrics wireframeMetrics) string
}

type modernScreenShellRenderer struct {
	Screens RuntimeTerminalStore
}

type modernShellTheme struct {
	app               lipgloss.Style
	topBar            lipgloss.Style
	subBar            lipgloss.Style
	tab               lipgloss.Style
	activeTab         lipgloss.Style
	chip              lipgloss.Style
	activeChip        lipgloss.Style
	panel             lipgloss.Style
	activePanel       lipgloss.Style
	mutedPanel        lipgloss.Style
	panelTitle        lipgloss.Style
	panelMeta         lipgloss.Style
	terminalBody      lipgloss.Style
	panelHeader       lipgloss.Style
	activePanelHeader lipgloss.Style
	panelFooter       lipgloss.Style
	activePanelFooter lipgloss.Style
	backdropPanel     lipgloss.Style
	noticeInfo        lipgloss.Style
	noticeWarn        lipgloss.Style
	noticeError       lipgloss.Style
	footer            lipgloss.Style
	modalBackdrop     lipgloss.Style
	modalPanel        lipgloss.Style
	modalTitle        lipgloss.Style
	modalMeta         lipgloss.Style
	modalBody         lipgloss.Style
	selectedListItem  lipgloss.Style
	listItem          lipgloss.Style
}

func defaultModernShellTheme() modernShellTheme {
	return modernShellTheme{
		app: lipgloss.NewStyle().
			Background(lipgloss.Color("#08111d")).
			Foreground(lipgloss.Color("#dbe7f3")),
		topBar: lipgloss.NewStyle().
			Background(lipgloss.Color("#0f172a")).
			Foreground(lipgloss.Color("#e2e8f0")).
			Bold(true).
			Padding(0, 1),
		subBar: lipgloss.NewStyle().
			Background(lipgloss.Color("#101c2f")).
			Foreground(lipgloss.Color("#bfd1e5")).
			Padding(0, 1),
		tab: lipgloss.NewStyle().
			Background(lipgloss.Color("#0b1220")).
			Foreground(lipgloss.Color("#8aa1bb")).
			Padding(0, 1),
		activeTab: lipgloss.NewStyle().
			Background(lipgloss.Color("#1d4ed8")).
			Foreground(lipgloss.Color("#eff6ff")).
			Bold(true).
			Padding(0, 1),
		chip: lipgloss.NewStyle().
			Background(lipgloss.Color("#162338")).
			Foreground(lipgloss.Color("#cbd5e1")).
			Padding(0, 1),
		activeChip: lipgloss.NewStyle().
			Background(lipgloss.Color("#0f766e")).
			Foreground(lipgloss.Color("#ecfeff")).
			Bold(true).
			Padding(0, 1),
		panel: lipgloss.NewStyle().
			Background(lipgloss.Color("#0b1220")).
			Foreground(lipgloss.Color("#dbe7f3")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#334155")).
			Padding(0, 1),
		activePanel: lipgloss.NewStyle().
			Background(lipgloss.Color("#0b1220")).
			Foreground(lipgloss.Color("#eff6ff")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#38bdf8")).
			Padding(0, 1),
		mutedPanel: lipgloss.NewStyle().
			Background(lipgloss.Color("#0a1424")).
			Foreground(lipgloss.Color("#94a3b8")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#233247")).
			Padding(0, 1),
		panelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f8fafc")),
		panelMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")),
		terminalBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e2e8f0")),
		panelHeader: lipgloss.NewStyle().
			Background(lipgloss.Color("#172033")).
			Foreground(lipgloss.Color("#dbe7f3")).
			Bold(true).
			Padding(0, 1),
		activePanelHeader: lipgloss.NewStyle().
			Background(lipgloss.Color("#1d4ed8")).
			Foreground(lipgloss.Color("#eff6ff")).
			Bold(true).
			Padding(0, 1),
		panelFooter: lipgloss.NewStyle().
			Background(lipgloss.Color("#111a2b")).
			Foreground(lipgloss.Color("#a5b4c8")).
			Padding(0, 1),
		activePanelFooter: lipgloss.NewStyle().
			Background(lipgloss.Color("#0f766e")).
			Foreground(lipgloss.Color("#ecfeff")).
			Bold(true).
			Padding(0, 1),
		backdropPanel: lipgloss.NewStyle().
			Background(lipgloss.Color("#09101d")).
			Foreground(lipgloss.Color("#cbd5e1")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#223047")).
			Padding(0, 1),
		noticeInfo: lipgloss.NewStyle().
			Background(lipgloss.Color("#0f3d69")).
			Foreground(lipgloss.Color("#e0f2fe")).
			Bold(true).
			Padding(0, 1),
		noticeWarn: lipgloss.NewStyle().
			Background(lipgloss.Color("#5b3700")).
			Foreground(lipgloss.Color("#fef3c7")).
			Bold(true).
			Padding(0, 1),
		noticeError: lipgloss.NewStyle().
			Background(lipgloss.Color("#6b1425")).
			Foreground(lipgloss.Color("#fee2e2")).
			Bold(true).
			Padding(0, 1),
		footer: lipgloss.NewStyle().
			Background(lipgloss.Color("#0f172a")).
			Foreground(lipgloss.Color("#cbd5e1")).
			Padding(0, 1),
		modalBackdrop: lipgloss.NewStyle().
			Background(lipgloss.Color("#08111d")).
			Foreground(lipgloss.Color("#dbe7f3")),
		modalPanel: lipgloss.NewStyle().
			Background(lipgloss.Color("#111827")).
			Foreground(lipgloss.Color("#eff6ff")).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#60a5fa")).
			Padding(0, 1),
		modalTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#f8fafc")),
		modalMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93c5fd")),
		modalBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dbeafe")),
		selectedListItem: lipgloss.NewStyle().
			Background(lipgloss.Color("#e2e8f0")).
			Foreground(lipgloss.Color("#0f172a")).
			Bold(true),
		listItem: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cbd5e1")),
	}
}

// RenderShell 是默认第一视觉 renderer。
// 它只负责产品态主界面，不再输出 debug section；详细调试信息继续走 --debug-ui 的旧 renderer。
func (r modernScreenShellRenderer) RenderShell(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, notices []btui.Notice, metrics wireframeMetrics) string {
	theme := defaultModernShellTheme()
	width := max(64, metrics.ViewportWidth)
	height := max(18, metrics.ViewportHeight)

	header := r.renderTopBar(theme, state, workspace, tab, pane, width)
	tabs := r.renderTabBar(theme, workspace, tab, width)
	context := r.renderContextBar(theme, state, pane, width)
	footer := r.renderFooter(theme, state, pane, notices, width)

	bodyHeight := height - 3
	if bodyHeight < 8 {
		bodyHeight = 8
	}

	body := r.renderWorkbench(theme, state, tab, pane, width, bodyHeight)
	if state.UI.Overlay.Kind != types.OverlayNone {
		body = r.renderOverlayViewport(theme, state, pane, width, bodyHeight)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, tabs, context, body, footer)
	return theme.app.Render(view)
}

func (r modernScreenShellRenderer) renderTopBar(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, width int) string {
	items := []string{
		theme.activeChip.Render("termx"),
		theme.chip.Render("workspace " + safeWorkspaceLabel(workspace)),
		theme.chip.Render("tab " + safeTabLabel(tab)),
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		items = append(items, theme.activeChip.Render("overlay "+string(state.UI.Overlay.Kind)))
	}
	left := lipgloss.JoinHorizontal(lipgloss.Left, items...)
	rightParts := []string{"pane " + string(pane.ID)}
	if state.UI.Mode.Active != "" && state.UI.Mode.Active != types.ModeNone {
		rightParts = append(rightParts, "mode "+string(state.UI.Mode.Active))
	}
	right := theme.panelMeta.Render(strings.Join(rightParts, "  •  "))
	return theme.topBar.Render(fillANSIHorizontal(left, right, width))
}

func (r modernScreenShellRenderer) renderTabBar(theme modernShellTheme, workspace types.WorkspaceState, tab types.TabState, width int) string {
	items := make([]string, 0, len(workspace.TabOrder)+1)
	items = append(items, theme.panelMeta.Render("tabs"))
	for _, tabID := range workspace.TabOrder {
		tabState, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := safeTabLabel(tabState)
		if tabID == workspace.ActiveTabID {
			items = append(items, theme.activeTab.Render(label))
			continue
		}
		items = append(items, theme.tab.Render(label))
	}
	right := theme.panelMeta.Render(r.renderWorkspaceSummaryText(workspace))
	return theme.subBar.Render(fillANSIHorizontal(strings.Join(items, " "), right, width))
}

func (r modernScreenShellRenderer) renderWorkspaceSummaryText(workspace types.WorkspaceState) string {
	tabs := len(workspace.TabOrder)
	panes := 0
	terminals := map[types.TerminalID]struct{}{}
	floating := 0
	for _, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		for _, pane := range tab.Panes {
			panes++
			if pane.Kind == types.PaneKindFloating {
				floating++
			}
			if pane.TerminalID != "" {
				terminals[pane.TerminalID] = struct{}{}
			}
		}
	}
	return fmt.Sprintf("%d tabs  %d panes  %d terminals  %d floating", tabs, panes, len(terminals), floating)
}

func (r modernScreenShellRenderer) renderContextBar(theme modernShellTheme, state types.AppState, pane types.PaneState, width int) string {
	layer := state.UI.Focus.Layer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	role := renderScreenShellPaneCardRole(state, pane)
	if role == "" {
		role = "unassigned"
	}
	leftItems := []string{
		theme.chip.Render(renderPaneTitle(state, pane)),
		theme.activeChip.Render(role),
		theme.chip.Render(string(pane.SlotState)),
	}
	if pane.TerminalID != "" {
		leftItems = append(leftItems, theme.chip.Render("terminal "+string(pane.TerminalID)))
	}
	leftItems = append(leftItems, theme.chip.Render("focus "+string(layer)))
	left := lipgloss.JoinHorizontal(lipgloss.Left, leftItems...)
	rightParts := []string{"mode " + string(mode)}
	if state.UI.Overlay.Kind != types.OverlayNone {
		rightParts = append(rightParts, "overlay "+string(state.UI.Overlay.Kind))
	}
	right := theme.panelMeta.Render(strings.Join(rightParts, "  •  "))
	return theme.subBar.Render(fillANSIHorizontal(left, right, width))
}

func (r modernScreenShellRenderer) renderWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, width, height int) string {
	tiledPaneIDs := orderedTiledPaneIDs(tab)
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	switch {
	case len(tiledPaneIDs) > 1:
		return r.renderSplitWorkbench(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, width, height)
	case len(floatingPaneIDs) > 0 && len(tiledPaneIDs) == 0:
		return r.renderFloatingWorkbench(theme, state, tab, pane, floatingPaneIDs, width, height)
	default:
		return r.renderSingleWorkbench(theme, state, pane, width, height, true)
	}
}

func (r modernScreenShellRenderer) renderSingleWorkbench(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int, active bool) string {
	panelStyle := theme.panel
	if active {
		panelStyle = theme.activePanel
	}
	bodyWidth := max(20, width-4)
	lines := r.renderPanePanelLines(theme, state, pane, bodyWidth, max(6, height-4), true, active, 0, 0)
	return panelStyle.Width(width - 2).Render(strings.Join(lines, "\n"))
}

func (r modernScreenShellRenderer) renderPanePreview(terminalID types.TerminalID) string {
	if terminalID == "" || r.Screens == nil {
		return ""
	}
	snapshot, ok := r.Screens.Snapshot(terminalID)
	if !ok || snapshot == nil {
		return ""
	}
	rows, _, _ := renderSnapshotRows(snapshot)
	for i := len(rows) - 1; i >= 0; i-- {
		line := strings.TrimSpace(rows[i])
		if line == "" || line == "<empty>" {
			continue
		}
		return truncateModernLine(line, 32)
	}
	return ""
}

func (r modernScreenShellRenderer) renderSplitWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, width, height int) string {
	header := theme.panelMeta.Render(fmt.Sprintf("Split view  •  %d panes  •  active %s", len(tiledPaneIDs), renderPaneTitle(state, pane)))
	layoutSummary := theme.panelMeta.Render(renderModernSplitLayoutSummary(tab, len(tiledPaneIDs)))
	canvasHeight := max(8, height-1)
	if len(floatingPaneIDs) > 0 {
		canvasHeight = max(8, height-5)
	}
	var canvas string
	if tab.RootSplit != nil {
		canvas = r.renderSplitCanvasNode(theme, state, tab, tab.RootSplit, width, canvasHeight)
	} else {
		canvas = r.renderImplicitSplitCanvas(theme, state, tab, tiledPaneIDs, width, canvasHeight)
	}
	lines := []string{header, layoutSummary, canvas}
	if len(floatingPaneIDs) > 0 {
		lines = append(lines, r.renderDetachedFloatingStrip(theme, state, tab, floatingPaneIDs, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (r modernScreenShellRenderer) renderFloatingWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, width, height int) string {
	header := theme.panelMeta.Render(fmt.Sprintf("Floating workbench  •  %d windows  •  focus %s", len(floatingPaneIDs), renderPaneTitle(state, pane)))
	summary := theme.panelMeta.Render(renderModernFloatingWorkbenchSummary(state, tab, floatingPaneIDs))
	lines := []string{header, summary}
	if hint := renderModernFloatingModeHint(state); hint != "" {
		lines = append(lines, theme.panelMeta.Render(hint))
	}
	deckWidth := min(34, max(28, width/3))
	mainWidth := max(30, width-deckWidth-1)
	bodyHeight := max(8, height-1)
	main := r.renderPaneWorkbenchCard(theme, state, pane, mainWidth, bodyHeight, true)
	deck := r.renderFloatingDeck(theme, state, tab, floatingPaneIDs, deckWidth, bodyHeight)
	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, main, " ", deck))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderSplitCanvasNode 递归落地 split 树，让默认产品态直接显示多 pane 盒模型。
func (r modernScreenShellRenderer) renderSplitCanvasNode(theme modernShellTheme, state types.AppState, tab types.TabState, node *types.SplitNode, width, height int) string {
	if node == nil {
		return r.renderPaneWorkbenchCard(theme, state, types.PaneState{SlotState: types.PaneSlotEmpty}, width, height, false)
	}
	if node.PaneID != "" {
		targetPane, ok := tab.Panes[node.PaneID]
		if !ok {
			return r.renderMissingPaneCard(theme, width, height, string(node.PaneID))
		}
		return r.renderPaneWorkbenchCard(theme, state, targetPane, width, height, node.PaneID == tab.ActivePaneID)
	}
	switch node.Direction {
	case types.SplitDirectionHorizontal:
		firstHeight := int(float64(max(1, height-1)) * node.Ratio)
		firstHeight = max(6, min(height-7, firstHeight))
		secondHeight := max(6, height-firstHeight-1)
		top := r.renderSplitCanvasNode(theme, state, tab, node.First, width, firstHeight)
		bottom := r.renderSplitCanvasNode(theme, state, tab, node.Second, width, secondHeight)
		return lipgloss.JoinVertical(lipgloss.Left, top, bottom)
	default:
		firstWidth := int(float64(max(1, width-1)) * node.Ratio)
		firstWidth = max(18, min(width-19, firstWidth))
		secondWidth := max(18, width-firstWidth-1)
		left := r.renderSplitCanvasNode(theme, state, tab, node.First, firstWidth, height)
		right := r.renderSplitCanvasNode(theme, state, tab, node.Second, secondWidth, height)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
}

func (r modernScreenShellRenderer) renderImplicitSplitCanvas(theme modernShellTheme, state types.AppState, tab types.TabState, tiledPaneIDs []types.PaneID, width, height int) string {
	if len(tiledPaneIDs) == 0 {
		return r.renderPaneWorkbenchCard(theme, state, types.PaneState{SlotState: types.PaneSlotEmpty}, width, height, false)
	}
	if len(tiledPaneIDs) == 1 {
		targetPane, ok := tab.Panes[tiledPaneIDs[0]]
		if !ok {
			return r.renderMissingPaneCard(theme, width, height, string(tiledPaneIDs[0]))
		}
		return r.renderPaneWorkbenchCard(theme, state, targetPane, width, height, true)
	}
	firstWidth := max(18, width/2)
	secondWidth := max(18, width-firstWidth-1)
	left := r.renderMissingPaneCard(theme, firstWidth, height, string(tiledPaneIDs[0]))
	right := r.renderMissingPaneCard(theme, secondWidth, height, string(tiledPaneIDs[1]))
	if targetPane, ok := tab.Panes[tiledPaneIDs[0]]; ok {
		left = r.renderPaneWorkbenchCard(theme, state, targetPane, firstWidth, height, tiledPaneIDs[0] == tab.ActivePaneID)
	}
	if targetPane, ok := tab.Panes[tiledPaneIDs[1]]; ok {
		right = r.renderPaneWorkbenchCard(theme, state, targetPane, secondWidth, height, tiledPaneIDs[1] == tab.ActivePaneID)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (r modernScreenShellRenderer) renderDetachedFloatingStrip(theme modernShellTheme, state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID, width int) string {
	items := []string{theme.panelMeta.Render("Detached windows")}
	for _, paneID := range floatingPaneIDs {
		targetPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		label := fmt.Sprintf("%s  %s", paneID, renderPaneTitle(state, targetPane))
		if paneID == tab.ActivePaneID && tab.ActiveLayer == types.FocusLayerFloating {
			items = append(items, theme.activeChip.Render(truncateModernLine(label, 20)))
			continue
		}
		items = append(items, theme.chip.Render(truncateModernLine(label, 20)))
	}
	return theme.subBar.Render(fillANSIHorizontal(strings.Join(items, " "), theme.panelMeta.Render(fmt.Sprintf("%d floating", len(floatingPaneIDs))), width))
}

func (r modernScreenShellRenderer) renderFloatingDeck(theme modernShellTheme, state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID, width, height int) string {
	header := theme.panelTitle.Render(fmt.Sprintf("Window deck  •  %d windows", len(floatingPaneIDs)))
	if len(floatingPaneIDs) == 0 {
		return theme.mutedPanel.Width(width - 2).Height(height - 2).Render(strings.Join([]string{header, theme.panelMeta.Render("No floating windows")}, "\n"))
	}
	cardHeight := 7
	cards := make([]string, 0, len(floatingPaneIDs))
	remainingHeight := max(0, height-1)
	for index, paneID := range floatingPaneIDs {
		if remainingHeight < 4 {
			break
		}
		targetPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		cards = append(cards, r.renderFloatingDeckCard(theme, state, targetPane, width, min(cardHeight, remainingHeight), paneID == tab.ActivePaneID, index, len(floatingPaneIDs)))
		remainingHeight -= cardHeight
	}
	if len(cards) == 0 {
		cards = append(cards, theme.panelMeta.Render("No floating windows"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{header}, cards...)...)
}

func (r modernScreenShellRenderer) renderFloatingDeckCard(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int, active bool, index int, total int) string {
	panelStyle := theme.mutedPanel
	if active {
		panelStyle = theme.activePanel
	}
	rectText := "rect auto"
	if pane.Rect.W > 0 || pane.Rect.H > 0 {
		rectText = fmt.Sprintf("rect %d,%d  %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H)
	}
	preview := r.renderPanePreview(pane.TerminalID)
	if preview == "" {
		preview = string(pane.SlotState)
	}
	lines := normalizeModernPanelLines([]string{
		theme.panelTitle.Render(fmt.Sprintf("z %d/%d • %s", index+1, max(1, total), renderPaneTitle(state, pane))),
		theme.panelMeta.Render(rectText),
		theme.panelMeta.Render(fmt.Sprintf("slot %s  •  %s", pane.SlotState, renderScreenShellPaneCardRole(state, pane))),
		theme.terminalBody.Render(preview),
		theme.panelMeta.Render(truncateModernLine(renderModernPaneFooterLine(state, pane, active), max(10, width-4))),
	}, max(14, width-4), max(4, height-2))
	return panelStyle.Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
}

func (r modernScreenShellRenderer) renderPaneWorkbenchCard(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int, active bool) string {
	panelStyle := theme.panel
	if active {
		panelStyle = theme.activePanel
	} else if pane.Kind == types.PaneKindFloating {
		panelStyle = theme.mutedPanel
	}
	contentWidth := max(12, width-4)
	contentHeight := max(4, height-2)
	zIndex, zTotal := 0, 0
	if pane.Kind == types.PaneKindFloating {
		zIndex, zTotal = renderModernFloatingZ(state, pane)
	}
	lines := r.renderPanePanelLines(theme, state, pane, contentWidth, contentHeight, true, active, zIndex, zTotal)
	lines = normalizeModernPanelLines(lines, contentWidth, contentHeight)
	return panelStyle.Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
}

func (r modernScreenShellRenderer) renderMissingPaneCard(theme modernShellTheme, width, height int, paneID string) string {
	lines := normalizeModernPanelLines([]string{
		theme.panelTitle.Render("Missing pane"),
		theme.panelMeta.Render("pane " + paneID),
		theme.terminalBody.Render("Layout references a pane that is not loaded."),
	}, max(12, width-4), max(4, height-2))
	return theme.mutedPanel.Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
}

func normalizeModernPanelLines(lines []string, width, height int) []string {
	out := make([]string, 0, height)
	for _, line := range lines {
		if len(out) >= height {
			break
		}
		out = append(out, truncateModernLine(line, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// renderPanePanelLines 统一负责产品态 pane 卡片的正文。
// 这里把 connected / empty / waiting / exited 四态折叠成同一种视觉骨架，避免再回到旧版分叉渲染。
func (r modernScreenShellRenderer) renderPanePanelLines(theme modernShellTheme, state types.AppState, pane types.PaneState, width, maxRows int, includeTitle bool, active bool, zIndex int, zTotal int) []string {
	lines := make([]string, 0, maxRows)
	if includeTitle {
		lines = append(lines, renderModernPaneHeader(theme, renderModernPaneTitleBar(state, pane, active, zIndex, zTotal), width, active))
	}
	lines = append(lines, theme.panelTitle.Render("Status"))
	lines = append(lines, theme.panelMeta.Render(renderModernPaneStatusLine(state, pane)))
	lines = append(lines, theme.panelMeta.Render(renderModernPaneIdentityLine(pane)))
	if pane.Kind == types.PaneKindFloating && (pane.Rect.W > 0 || pane.Rect.H > 0) {
		lines = append(lines, theme.panelTitle.Render("Geometry"))
		lines = append(lines, theme.panelMeta.Render(fmt.Sprintf("rect %d,%d  %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H)))
	}

	switch pane.SlotState {
	case types.PaneSlotEmpty:
		lines = append(lines, theme.panelTitle.Render("Details"))
		lines = append(lines,
			theme.terminalBody.Render("No terminal connected yet."),
		)
		lines = append(lines, theme.panelTitle.Render("Actions"))
		lines = append(lines, theme.panelMeta.Render("Press n to start one, or a to connect an existing terminal."))
	case types.PaneSlotWaiting:
		lines = append(lines, theme.panelTitle.Render("Details"))
		lines = append(lines,
			theme.terminalBody.Render("Waiting for a terminal connection."),
		)
		lines = append(lines, theme.panelTitle.Render("Actions"))
		lines = append(lines, theme.panelMeta.Render("This pane is reserved by layout or restore flow."))
	case types.PaneSlotExited:
		exitText := "history retained"
		if pane.LastExitCode != nil {
			exitText = fmt.Sprintf("history retained  exit %d", *pane.LastExitCode)
		}
		lines = append(lines, theme.panelTitle.Render("Details"))
		lines = append(lines,
			theme.terminalBody.Render("Terminal program exited."),
			theme.panelMeta.Render(exitText),
		)
		lines = append(lines, theme.panelTitle.Render("Actions"))
		lines = append(lines, theme.panelMeta.Render("Press r to restart, or a to connect another terminal."))
	default:
		lines = append(lines, theme.panelTitle.Render("Terminal"))
		lines = append(lines, r.renderTerminalMetaLines(theme, state, pane, width)...)
		lines = append(lines, theme.panelTitle.Render("Actions"))
		lines = append(lines, theme.panelMeta.Render(truncateModernLine(renderModernPaneActionLine(state, pane), width)))
	}
	lines = append(lines, theme.panelTitle.Render("Footer"))
	lines = append(lines, renderModernPaneFooter(theme, renderModernPaneFooterLine(state, pane, active), width, active))
	if pane.SlotState == types.PaneSlotConnected {
		lines = append(lines, theme.panelTitle.Render("Preview"))
		lines = append(lines, r.renderTerminalPreviewLines(theme, pane, width, maxRows-len(lines)-1)...)
	}

	return lines
}

func renderModernPaneTitleBar(state types.AppState, pane types.PaneState, active bool, zIndex int, zTotal int) string {
	parts := []string{renderPaneTitle(state, pane)}
	role := renderScreenShellPaneCardRole(state, pane)
	if role != "" {
		parts = append(parts, role)
	}
	if pane.Kind != "" {
		parts = append(parts, string(pane.Kind))
	}
	if active {
		parts = append(parts, "active")
	}
	if zIndex > 0 && zTotal > 0 {
		parts = append(parts, fmt.Sprintf("z %d/%d", zIndex, zTotal))
	}
	return strings.Join(parts, " • ")
}

func renderModernPaneHeader(theme modernShellTheme, text string, width int, active bool) string {
	style := theme.panelHeader
	if active {
		style = theme.activePanelHeader
	}
	return style.Render(truncateModernLine(text, max(8, width-2)))
}

func renderModernPaneFooter(theme modernShellTheme, text string, width int, active bool) string {
	style := theme.panelFooter
	if active {
		style = theme.activePanelFooter
	}
	return style.Render(truncateModernLine(text, max(8, width-2)))
}

func renderModernPaneFooterLine(state types.AppState, pane types.PaneState, active bool) string {
	switch pane.Kind {
	case types.PaneKindFloating:
		if active && state.UI.Mode.Active == types.ModeFloating {
			return "live window  •  move/size"
		}
		if active {
			return "live window  •  deck  •  Ctrl-o"
		}
		return "standby window  •  deck"
	}
	switch pane.SlotState {
	case types.PaneSlotEmpty:
		if active {
			return "empty slot  •  n create  •  a connect"
		}
		return "empty pane  •  Ctrl-p pane"
	case types.PaneSlotWaiting:
		return "waiting slot  •  layout/restore reserved"
	case types.PaneSlotExited:
		if active {
			return "restart ready  •  r restart  •  a connect"
		}
		return "exited pane  •  Ctrl-p pane"
	default:
		if active {
			return "live input  •  Ctrl-p  •  pick"
		}
		return "standby pane  •  Ctrl-p pane"
	}
}

func renderModernFloatingZ(state types.AppState, pane types.PaneState) (int, int) {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return 0, 0
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok || len(tab.FloatingOrder) == 0 {
		return 0, 0
	}
	for idx, paneID := range tab.FloatingOrder {
		if paneID == pane.ID {
			return idx + 1, len(tab.FloatingOrder)
		}
	}
	return 0, len(tab.FloatingOrder)
}

func renderModernPaneStatusLine(state types.AppState, pane types.PaneState) string {
	role := renderScreenShellPaneCardRole(state, pane)
	if role == "" {
		role = string(pane.SlotState)
	}
	parts := []string{role, string(pane.SlotState)}
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			stateLabel := string(terminal.State)
			if stateLabel == "" {
				stateLabel = "running"
			}
			parts = append(parts, stateLabel)
		}
	}
	return strings.Join(parts, "  •  ")
}

func renderModernPaneIdentityLine(pane types.PaneState) string {
	parts := []string{string(pane.Kind) + " " + string(pane.ID)}
	if pane.TerminalID != "" {
		parts = append(parts, "terminal "+string(pane.TerminalID))
	}
	return strings.Join(parts, "  •  ")
}

func renderModernPaneActionLine(state types.AppState, pane types.PaneState) string {
	if state.UI.Mode.Active == types.ModeFloating && pane.Kind == types.PaneKindFloating {
		return "move j/k  •  size H/J/K/L  •  c center"
	}
	switch pane.SlotState {
	case types.PaneSlotExited:
		return "r restart  •  a connect"
	case types.PaneSlotWaiting, types.PaneSlotEmpty:
		return "n create  •  a connect  •  m manager"
	default:
		return "type  •  Ctrl-f picker  •  Ctrl-g global"
	}
}

func (r modernScreenShellRenderer) renderTerminalMetaLines(theme modernShellTheme, state types.AppState, pane types.PaneState, width int) []string {
	if pane.TerminalID == "" {
		return nil
	}
	terminal, ok := state.Domain.Terminals[pane.TerminalID]
	if !ok {
		return nil
	}
	stateLabel := string(terminal.State)
	if stateLabel == "" {
		stateLabel = "running"
	}
	meta := []string{fmt.Sprintf("state %s", stateLabel)}
	if len(terminal.Command) > 0 {
		meta = append(meta, "cmd "+strings.Join(terminal.Command, " "))
	}
	if tags := renderTerminalTags(terminal.Tags); tags != "" {
		meta = append(meta, "tags "+tags)
	}
	return []string{theme.panelMeta.Render(truncateModernLine(strings.Join(meta, "  •  "), width))}
}

func (r modernScreenShellRenderer) renderTerminalPreviewLines(theme modernShellTheme, pane types.PaneState, width, maxRows int) []string {
	if maxRows <= 0 {
		maxRows = 1
	}
	if pane.TerminalID == "" || r.Screens == nil {
		return []string{theme.terminalBody.Render("<screen unavailable>")}
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return []string{theme.terminalBody.Render("<screen unavailable>")}
	}
	rows, _, _ := renderSnapshotRows(snapshot)
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, theme.terminalBody.Render(truncateModernLine(row, width)))
	}
	return lines
}

func (r modernScreenShellRenderer) renderOverlayViewport(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int) string {
	backdropHeight := min(7, max(5, height/4))
	panelWidth := min(width-4, max(56, width*3/4))
	if panelWidth <= 0 {
		panelWidth = width
	}
	panel := r.renderOverlayPanel(theme, state, panelWidth)
	modalHeight := max(8, height-backdropHeight-1)
	backdrop := r.renderOverlayBackdrop(theme, state, pane, width, backdropHeight)
	modal := lipgloss.Place(
		width,
		modalHeight,
		lipgloss.Center,
		lipgloss.Center,
		panel,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#08111d")),
	)
	return lipgloss.JoinVertical(lipgloss.Left, backdrop, modal)
}

func (r modernScreenShellRenderer) renderOverlayBackdrop(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int) string {
	preview := r.renderPanePreview(pane.TerminalID)
	if preview == "" {
		preview = string(pane.SlotState)
	}
	lines := normalizeModernPanelLines([]string{
		theme.panelTitle.Render("Backdrop workbench"),
		theme.panelMeta.Render(fmt.Sprintf("overlay active • %s", state.UI.Overlay.Kind)),
		theme.panelMeta.Render("background " + renderModernPaneTitleBar(state, pane, false, 0, 0)),
		theme.panelMeta.Render(renderModernPaneStatusLine(state, pane)),
		theme.terminalBody.Render(preview),
	}, max(16, width-4), max(3, height-2))
	return theme.backdropPanel.Width(width - 2).Height(height - 1).Render(strings.Join(lines, "\n"))
}

func (r modernScreenShellRenderer) renderOverlayPanel(theme modernShellTheme, state types.AppState, width int) string {
	title := overlayTitle(state.UI.Overlay.Kind)
	lines := []string{theme.modalTitle.Render(title)}
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		lines = append(lines, theme.modalMeta.Render("return to "+returnFocus))
	}
	lines = append(lines, r.renderOverlayPanelBody(theme, state.UI.Overlay, width-6)...)
	if footer := renderModernOverlayFooter(theme, state.UI.Overlay.Kind, width-6); footer != "" {
		lines = append(lines, "", footer)
	}
	return theme.modalPanel.Width(width - 2).Render(strings.Join(lines, "\n"))
}

func overlayTitle(kind types.OverlayKind) string {
	switch kind {
	case types.OverlayHelp:
		return "Help"
	case types.OverlayTerminalManager:
		return "Terminal Manager"
	case types.OverlayWorkspacePicker:
		return "Workspace Picker"
	case types.OverlayTerminalPicker:
		return "Terminal Picker"
	case types.OverlayLayoutResolve:
		return "Layout Resolve"
	case types.OverlayPrompt:
		return "Prompt"
	default:
		return string(kind)
	}
}

func (r modernScreenShellRenderer) renderOverlayPanelBody(theme modernShellTheme, overlay types.OverlayState, width int) []string {
	switch overlay.Kind {
	case types.OverlayHelp:
		return []string{
			theme.modalMeta.Render("Keysets"),
			theme.modalBody.Render(truncateModernLine("Ctrl-p pane  •  Ctrl-t tab  •  Ctrl-w workspace  •  Ctrl-f picker", width)),
			theme.modalBody.Render(truncateModernLine("Ctrl-o floating  •  Ctrl-g global  •  Esc close", width)),
			"",
			theme.modalMeta.Render("Model"),
			theme.modalBody.Render(truncateModernLine("pane is the view slot, terminal is the running entity", width)),
		}
	case types.OverlayTerminalManager:
		manager, _ := overlay.Data.(*terminalmanagerdomain.State)
		return renderModernTerminalManagerOverlay(theme, manager, width)
	case types.OverlayWorkspacePicker:
		picker, _ := overlay.Data.(*workspacedomain.PickerState)
		return renderModernWorkspacePickerOverlay(theme, picker, width)
	case types.OverlayTerminalPicker:
		picker, _ := overlay.Data.(*terminalpickerdomain.State)
		return renderModernTerminalPickerOverlay(theme, picker, width)
	case types.OverlayLayoutResolve:
		resolve, _ := overlay.Data.(*layoutresolvedomain.State)
		return renderModernLayoutResolveOverlay(theme, resolve, width)
	case types.OverlayPrompt:
		prompt, _ := overlay.Data.(*promptdomain.State)
		return renderModernPromptOverlay(theme, prompt, width)
	default:
		return []string{theme.modalBody.Render("Overlay active")}
	}
}

func renderModernTerminalManagerOverlay(theme modernShellTheme, manager *terminalmanagerdomain.State, width int) []string {
	if manager == nil {
		return []string{theme.modalBody.Render("No terminal data loaded yet.")}
	}
	rows := manager.VisibleRows()
	selected, ok := manager.SelectedRow()
	contentWidth := max(18, width/2-4)
	leftLines := []string{
		theme.modalMeta.Render(fmt.Sprintf("search %q", manager.Query())),
		theme.modalMeta.Render(fmt.Sprintf("%d rows", len(rows))),
		"",
		theme.modalMeta.Render("Selection"),
	}
	if ok {
		leftLines = append(leftLines, theme.modalBody.Render(truncateModernLine("selected "+activeRowLabel(selected, true), contentWidth)))
	}
	leftLines = append(leftLines, "", theme.modalMeta.Render("Rows"))
	for _, line := range modernTerminalManagerRowPreview(theme, rows, selected, ok, width) {
		leftLines = append(leftLines, line)
	}
	rightLines := trimModernOverlayLines(renderModernTerminalManagerDetail(theme, manager, contentWidth))
	actionLines := []string{
		theme.modalBody.Render(truncateModernLine("Enter connect here  •  t new tab  •  o floating  •  e edit  •  s stop", width)),
	}
	return renderModernOverlayPanels(theme, width, "Visible terminals", leftLines, "Detail panel", rightLines, "Action bar", actionLines)
}

func modernTerminalManagerRowPreview(theme modernShellTheme, rows []terminalmanagerdomain.Row, selected terminalmanagerdomain.Row, hasSelected bool, width int) []string {
	preview := make([]string, 0, min(4, len(rows)))
	selectedIndex := 0
	if hasSelected {
		for idx, row := range rows {
			if row.Kind == selected.Kind && row.Label == selected.Label && row.TerminalID == selected.TerminalID {
				selectedIndex = idx
				break
			}
		}
	}
	slice, _ := overlayPreviewRowsAround(rows, 4, selectedIndex)
	for _, row := range slice {
		text := truncateModernLine(renderModernTerminalManagerRowText(row), width)
		switch row.Kind {
		case terminalmanagerdomain.RowKindHeader:
			preview = append(preview, theme.modalMeta.Render(text))
			continue
		case terminalmanagerdomain.RowKindCreate:
			if hasSelected && selected.Kind == row.Kind && selected.Label == row.Label {
				preview = append(preview, theme.selectedListItem.Render("> "+text))
				continue
			}
			preview = append(preview, theme.listItem.Render("  "+text))
			continue
		}
		if hasSelected && row.Kind == selected.Kind && row.Label == selected.Label && row.TerminalID == selected.TerminalID {
			preview = append(preview, theme.selectedListItem.Render("> "+text))
			continue
		}
		preview = append(preview, theme.listItem.Render("  "+text))
	}
	return preview
}

func renderModernTerminalManagerRowText(row terminalmanagerdomain.Row) string {
	switch row.Kind {
	case terminalmanagerdomain.RowKindHeader:
		return "[" + string(row.Section) + "]"
	case terminalmanagerdomain.RowKindCreate:
		return "[create] + new terminal  •  current workbench"
	default:
		parts := []string{"[terminal] " + row.Label}
		if row.State != "" {
			parts = append(parts, string(row.State))
		}
		if row.VisibilityLabel != "" {
			parts = append(parts, row.VisibilityLabel)
		}
		if row.OwnerSlotLabel != "" {
			parts = append(parts, "owner "+row.OwnerSlotLabel)
		} else {
			parts = append(parts, fmt.Sprintf("%d panes", row.ConnectedPaneCount))
		}
		return strings.Join(parts, "  •  ")
	}
}

func renderModernTerminalManagerDetail(theme modernShellTheme, manager *terminalmanagerdomain.State, width int) []string {
	row, ok := manager.SelectedRow()
	if !ok {
		return nil
	}
	lines := []string{"", theme.modalMeta.Render("Detail")}
	if row.Kind == terminalmanagerdomain.RowKindCreate {
		lines = append(lines, theme.modalBody.Render(truncateModernLine("Create a new terminal in the current workbench.", width)))
		return lines
	}
	detail, ok := manager.SelectedDetail()
	if !ok {
		lines = append(lines, theme.modalBody.Render("No terminal detail loaded yet."))
		return lines
	}
	summaryParts := []string{string(detail.State), detail.VisibilityLabel}
	lines = append(lines, theme.modalBody.Render(truncateModernLine(strings.Join(summaryParts, "  •  "), width)))
	if detail.OwnerSlotLabel != "" {
		lines = append(lines, theme.modalMeta.Render(truncateModernLine("owner "+detail.OwnerSlotLabel, width)))
	}
	if detail.ConnectedPaneCount > 0 {
		lines = append(lines, theme.modalMeta.Render(fmt.Sprintf("%d panes connected", detail.ConnectedPaneCount)))
	}
	if detail.Command != "" {
		lines = append(lines, theme.modalBody.Render(truncateModernLine("cmd "+detail.Command, width)))
	}
	if len(detail.Tags) > 0 {
		lines = append(lines, theme.modalMeta.Render(truncateModernLine("tags "+renderModernTags(detail.Tags), width)))
	}
	if len(detail.Locations) > 0 {
		lines = append(lines, theme.modalMeta.Render(truncateModernLine("path "+renderModernLocation(detail.Locations[0]), width)))
		if len(detail.Locations) > 1 {
			lines = append(lines, theme.modalMeta.Render(fmt.Sprintf("%d locations", len(detail.Locations))))
		}
	}
	return lines
}

func renderModernWorkspacePickerOverlay(theme modernShellTheme, picker *workspacedomain.PickerState, width int) []string {
	if picker == nil {
		return []string{theme.modalBody.Render("No workspace tree loaded yet.")}
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	contentWidth := max(18, width/2-4)
	leftLines := []string{
		theme.modalMeta.Render(fmt.Sprintf("query %q  •  %d rows", picker.Query(), len(rows))),
	}
	if hasSelected {
		leftLines = append(leftLines, "", theme.modalMeta.Render("Selection"))
		leftLines = append(leftLines, theme.modalBody.Render(truncateModernLine(fmt.Sprintf("selected %s  •  %s", selectedRow.Node.Label, selectedRow.Node.Kind), contentWidth)))
	}
	selectedIndex := 0
	if hasSelected {
		for idx, row := range rows {
			if row.Node.Key == selectedRow.Node.Key {
				selectedIndex = idx
				break
			}
		}
	}
	leftLines = append(leftLines, "", theme.modalMeta.Render("Tree"))
	slice, _ := overlayPreviewRowsAround(rows, 6, selectedIndex)
	for _, row := range slice {
		label := renderModernWorkspaceTreeRowText(row)
		label = truncateModernLine(label, width)
		if hasSelected && row.Node.Key == selectedRow.Node.Key {
			leftLines = append(leftLines, theme.selectedListItem.Render("> "+label))
			continue
		}
		leftLines = append(leftLines, theme.listItem.Render("  "+label))
	}
	rightLines := trimModernOverlayLines(renderModernWorkspacePickerDetail(theme, picker, contentWidth))
	actionLines := []string{
		theme.modalBody.Render(truncateModernLine("Enter jump  •  / filter  •  Esc close", width)),
	}
	return renderModernOverlayPanels(theme, width, "Tree panel", leftLines, "Target panel", rightLines, "Action bar", actionLines)
}

func renderModernWorkspaceTreeRowText(row workspacedomain.TreeRow) string {
	indent := strings.Repeat("  ", row.Depth)
	prefix := ""
	switch {
	case row.Node.Kind == workspacedomain.TreeNodeKindCreate:
		prefix = "[create]"
	case row.Node.Kind == workspacedomain.TreeNodeKindPane:
		prefix = "[pane]"
	case row.Expanded:
		prefix = "[-] [" + string(row.Node.Kind) + "]"
	default:
		prefix = "[+] [" + string(row.Node.Kind) + "]"
	}
	if row.Node.Kind == workspacedomain.TreeNodeKindPane || row.Node.Kind == workspacedomain.TreeNodeKindCreate {
		return indent + prefix + " " + row.Node.Label
	}
	return indent + prefix + " " + row.Node.Label
}

func renderModernWorkspacePickerDetail(theme modernShellTheme, picker *workspacedomain.PickerState, width int) []string {
	row, ok := picker.SelectedRow()
	if !ok {
		return nil
	}
	lines := []string{"", theme.modalMeta.Render("Target")}
	switch row.Node.Kind {
	case workspacedomain.TreeNodeKindCreate:
		lines = append(lines, theme.modalBody.Render(truncateModernLine("Create a new workspace and switch focus into it.", width)))
	case workspacedomain.TreeNodeKindWorkspace:
		lines = append(lines, theme.modalBody.Render(truncateModernLine(fmt.Sprintf("workspace %s  (%s)", row.Node.Label, row.Node.WorkspaceID), width)))
	case workspacedomain.TreeNodeKindTab:
		lines = append(lines, theme.modalBody.Render(truncateModernLine(fmt.Sprintf("workspace %s  •  tab %s", row.Node.WorkspaceID, row.Node.Label), width)))
	case workspacedomain.TreeNodeKindPane:
		lines = append(lines, theme.modalBody.Render(truncateModernLine(fmt.Sprintf("workspace %s  •  tab %s  •  pane %s", row.Node.WorkspaceID, row.Node.TabID, row.Node.PaneID), width)))
		lines = append(lines, theme.modalMeta.Render(truncateModernLine("direct jump target inside the workbench tree", width)))
	}
	return lines
}

func renderModernTerminalPickerOverlay(theme modernShellTheme, picker *terminalpickerdomain.State, width int) []string {
	if picker == nil {
		return []string{theme.modalBody.Render("No terminal options loaded yet.")}
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	contentWidth := max(18, width/2-4)
	leftLines := []string{
		theme.modalMeta.Render(fmt.Sprintf("query %q  •  %d rows", picker.Query(), len(rows))),
	}
	if hasSelected {
		leftLines = append(leftLines, "", theme.modalMeta.Render("Selection"))
		leftLines = append(leftLines, theme.modalBody.Render("selected "+truncateModernLine(selectedRow.Label, contentWidth)))
	}
	selectedIndex := 0
	if hasSelected {
		for idx, row := range rows {
			if row.Kind == selectedRow.Kind && row.Label == selectedRow.Label && row.TerminalID == selectedRow.TerminalID {
				selectedIndex = idx
				break
			}
		}
	}
	leftLines = append(leftLines, "", theme.modalMeta.Render("Results"))
	slice, _ := overlayPreviewRowsAround(rows, 4, selectedIndex)
	for _, row := range slice {
		text := truncateModernLine(renderModernTerminalPickerRowText(row), width)
		if hasSelected && row.Kind == selectedRow.Kind && row.Label == selectedRow.Label && row.TerminalID == selectedRow.TerminalID {
			leftLines = append(leftLines, theme.selectedListItem.Render("> "+text))
			continue
		}
		leftLines = append(leftLines, theme.listItem.Render("  "+text))
	}
	rightLines := trimModernOverlayLines(renderModernTerminalPickerDetail(theme, picker, contentWidth))
	actionLines := []string{
		theme.modalBody.Render(truncateModernLine("Enter connect  •  n create new  •  Esc close", width)),
	}
	return renderModernOverlayPanels(theme, width, "Results panel", leftLines, "Detail panel", rightLines, "Action bar", actionLines)
}

func renderModernTerminalPickerRowText(row terminalpickerdomain.Row) string {
	if row.Kind == terminalpickerdomain.RowKindCreate {
		return "[create] + new terminal"
	}
	parts := []string{"[terminal] " + row.Label}
	if row.State != "" {
		parts = append(parts, string(row.State))
	}
	if row.Visible {
		parts = append(parts, "visible")
	} else {
		parts = append(parts, "hidden")
	}
	parts = append(parts, fmt.Sprintf("%d panes", row.ConnectedPaneCount))
	return strings.Join(parts, "  •  ")
}

func renderModernTerminalPickerDetail(theme modernShellTheme, picker *terminalpickerdomain.State, width int) []string {
	row, ok := picker.SelectedRow()
	if !ok {
		return nil
	}
	lines := []string{"", theme.modalMeta.Render("Detail")}
	if row.Kind == terminalpickerdomain.RowKindCreate {
		lines = append(lines, theme.modalBody.Render(truncateModernLine("Create a new terminal using current shell defaults.", width)))
		return lines
	}
	summaryParts := []string{string(row.State)}
	if row.Visible {
		summaryParts = append(summaryParts, "visible")
	} else {
		summaryParts = append(summaryParts, "hidden")
	}
	if row.ConnectedPaneCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d panes", row.ConnectedPaneCount))
	}
	lines = append(lines, theme.modalBody.Render(truncateModernLine(strings.Join(summaryParts, "  •  "), width)))
	if row.Command != "" {
		lines = append(lines, theme.modalBody.Render(truncateModernLine("cmd "+row.Command, width)))
	}
	if len(row.Tags) > 0 {
		lines = append(lines, theme.modalMeta.Render(truncateModernLine("tags "+renderModernStringTags(row.Tags), width)))
	}
	return lines
}

func renderModernLayoutResolveOverlay(theme modernShellTheme, resolve *layoutresolvedomain.State, width int) []string {
	if resolve == nil {
		return []string{theme.modalBody.Render("No layout action required.")}
	}
	lines := []string{
		theme.modalMeta.Render(fmt.Sprintf("pane %s  •  role %s", resolve.PaneID, resolve.Role)),
	}
	if resolve.Hint != "" {
		lines = append(lines, "", theme.modalMeta.Render("Target"))
		lines = append(lines, theme.modalBody.Render(truncateModernLine(resolve.Hint, width)))
	}
	rows := resolve.Rows()
	selectedRow, hasSelected := resolve.SelectedRow()
	lines = append(lines, renderModernLayoutResolveDetail(theme, selectedRow, hasSelected, width)...)
	selectedIndex := 0
	if hasSelected {
		for idx, row := range rows {
			if row.Action == selectedRow.Action && row.Label == selectedRow.Label {
				selectedIndex = idx
				break
			}
		}
	}
	lines = append(lines, "", theme.modalMeta.Render("Choices"))
	slice, _ := overlayPreviewRowsAround(rows, 4, selectedIndex)
	for _, row := range slice {
		text := truncateModernLine(string(row.Action)+"  "+row.Label, width)
		if hasSelected && row.Action == selectedRow.Action && row.Label == selectedRow.Label {
			lines = append(lines, theme.selectedListItem.Render("> "+text))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+text))
	}
	lines = append(lines, "", theme.modalMeta.Render("Actions"))
	lines = append(lines, theme.modalBody.Render(truncateModernLine("Enter confirm  •  Esc close", width)))
	return lines
}

func renderModernLayoutResolveDetail(theme modernShellTheme, row layoutresolvedomain.Row, hasSelected bool, width int) []string {
	if !hasSelected {
		return nil
	}
	return []string{
		"",
		theme.modalMeta.Render("Selection"),
		theme.modalBody.Render(truncateModernLine(fmt.Sprintf("%s  •  %s", row.Action, row.Label), width)),
	}
}

func renderModernPromptOverlay(theme modernShellTheme, prompt *promptdomain.State, width int) []string {
	if prompt == nil {
		return []string{theme.modalBody.Render("Prompt not ready.")}
	}
	if len(prompt.Fields) == 0 {
		return []string{
			theme.modalMeta.Render("draft mode"),
			"",
			theme.modalMeta.Render("Context"),
			theme.modalBody.Render(truncateModernLine(renderModernPromptContext(prompt), width)),
			theme.modalMeta.Render("Fields"),
			theme.modalBody.Render(truncateModernLine(prompt.Draft, width)),
			"",
			theme.modalMeta.Render("Actions"),
			theme.modalBody.Render(truncateModernLine("Enter submit  •  Esc cancel", width)),
		}
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	lines := []string{
		theme.modalMeta.Render(fmt.Sprintf("%d fields  •  active %s", len(prompt.Fields), prompt.Fields[active].Key)),
		"",
		theme.modalMeta.Render("Context"),
		theme.modalBody.Render(truncateModernLine(renderModernPromptContext(prompt), width)),
		"",
		theme.modalMeta.Render("Fields"),
	}
	for idx, field := range prompt.Fields {
		text := truncateModernLine(fmt.Sprintf("%s: %s", field.Label, field.Value), width)
		if idx == active {
			lines = append(lines, theme.selectedListItem.Render("> "+text))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+text))
	}
	lines = append(lines, "", theme.modalMeta.Render("Actions"))
	lines = append(lines, theme.modalBody.Render(truncateModernLine("Enter submit  •  Tab next field  •  Esc cancel", width)))
	return lines
}

func renderModernPromptContext(prompt *promptdomain.State) string {
	parts := []string{string(prompt.Kind)}
	if prompt.TerminalID != "" {
		parts = append(parts, "terminal "+string(prompt.TerminalID))
	}
	if prompt.Title != "" {
		parts = append(parts, prompt.Title)
	}
	return strings.Join(parts, "  •  ")
}

func renderModernOverlayPanels(theme modernShellTheme, width int, leftTitle string, leftLines []string, rightTitle string, rightLines []string, actionTitle string, actionLines []string) []string {
	leftLines = trimModernOverlayLines(leftLines)
	rightLines = trimModernOverlayLines(rightLines)
	actionLines = trimModernOverlayLines(actionLines)
	if len(rightLines) == 0 {
		rightLines = []string{theme.modalBody.Render("No detail loaded yet.")}
	}
	if len(actionLines) == 0 {
		actionLines = []string{theme.modalBody.Render("No actions available.")}
	}
	if width < 56 {
		return []string{
			renderModernOverlaySectionPanel(theme, leftTitle, leftLines, width),
			"",
			renderModernOverlaySectionPanel(theme, rightTitle, rightLines, width),
			"",
			renderModernOverlaySectionPanel(theme, actionTitle, actionLines, width),
		}
	}
	leftWidth := max(24, (width-1)/2)
	rightWidth := max(24, width-leftWidth-1)
	top := lipgloss.JoinHorizontal(
		lipgloss.Top,
		renderModernOverlaySectionPanel(theme, leftTitle, leftLines, leftWidth),
		" ",
		renderModernOverlaySectionPanel(theme, rightTitle, rightLines, rightWidth),
	)
	return []string{
		top,
		"",
		renderModernOverlaySectionPanel(theme, actionTitle, actionLines, width),
	}
}

func renderModernOverlaySectionPanel(theme modernShellTheme, title string, lines []string, width int) string {
	if width < 18 {
		width = 18
	}
	contentWidth := max(12, width-4)
	out := []string{theme.panelTitle.Render(title)}
	for _, line := range trimModernOverlayLines(lines) {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, truncateModernLine(line, contentWidth))
	}
	return theme.mutedPanel.Width(width - 2).Render(strings.Join(out, "\n"))
}

func trimModernOverlayLines(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(xansi.Strip(lines[start])) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(xansi.Strip(lines[end-1])) == "" {
		end--
	}
	if start >= end {
		return nil
	}
	return append([]string(nil), lines[start:end]...)
}

func renderModernSplitLayoutSummary(tab types.TabState, tiledPanes int) string {
	summary := summarizeTiledLayout(tab.RootSplit, tiledPanes)
	ratio := "auto"
	if summary.HasRatio {
		ratio = fmt.Sprintf("%02.0f/%02.0f", summary.Ratio*100, (1-summary.Ratio)*100)
	}
	return fmt.Sprintf("layout %s %s  •  depth %d  •  switch Ctrl-p pane", summary.Root, ratio, summary.Depth)
}

func renderModernFloatingWorkbenchSummary(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	activePane := string(tab.ActivePaneID)
	if activePane == "" && len(floatingPaneIDs) > 0 {
		activePane = string(floatingPaneIDs[0])
	}
	topPane := "<none>"
	if len(floatingPaneIDs) > 0 {
		topPane = string(floatingPaneIDs[len(floatingPaneIDs)-1])
	}
	layer := state.UI.Focus.Layer
	if layer == "" {
		layer = types.FocusLayerFloating
	}
	return fmt.Sprintf("focus %s  •  top %s  •  layer %s  •  deck %d", activePane, topPane, layer, len(floatingPaneIDs))
}

func renderModernFloatingModeHint(state types.AppState) string {
	if state.UI.Mode.Active != types.ModeFloating {
		return ""
	}
	return "move j/k  •  size H/J/K/L  •  c center  •  x close  •  Esc exit"
}

func renderModernOverlayFooter(theme modernShellTheme, kind types.OverlayKind, width int) string {
	var text string
	switch kind {
	case types.OverlayHelp:
		text = "Esc close"
	case types.OverlayTerminalManager:
		text = "Enter connect  •  t new tab  •  o floating  •  Esc close"
	case types.OverlayWorkspacePicker:
		text = "Enter jump  •  / filter  •  Esc close"
	case types.OverlayTerminalPicker:
		text = "Enter connect  •  n create  •  Esc close"
	case types.OverlayLayoutResolve:
		text = "Enter confirm  •  Esc close"
	case types.OverlayPrompt:
		text = "Enter submit  •  Tab next  •  Esc cancel"
	default:
		return ""
	}
	return theme.modalMeta.Render(truncateModernLine(text, width))
}

func renderModernTags(tags []terminalmanagerdomain.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for _, tag := range tags {
		parts = append(parts, tag.Key+"="+tag.Value)
	}
	return strings.Join(parts, ",")
}

func renderModernStringTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+tags[key])
	}
	return strings.Join(parts, ",")
}

func renderModernLocation(location terminalmanagerdomain.Location) string {
	return fmt.Sprintf("%s / %s / %s", location.WorkspaceName, location.TabName, location.SlotLabel)
}

func (r modernScreenShellRenderer) renderFooter(theme modernShellTheme, state types.AppState, pane types.PaneState, notices []btui.Notice, width int) string {
	notice := renderModernNotice(theme, notices)
	parts := renderScreenShellFooterParts(state)
	left := notice
	right := theme.panelMeta.Render(strings.Join(parts, "  •  "))
	return theme.footer.Render(fillANSIHorizontal(left, right, width))
}

func renderModernNotice(theme modernShellTheme, notices []btui.Notice) string {
	total := countVisibleNotices(notices)
	if total == 0 {
		return theme.noticeInfo.Render("ready")
	}
	last, ok := lastVisibleNotice(notices)
	if !ok {
		return theme.noticeInfo.Render(fmt.Sprintf("%d notices", total))
	}
	label := last.Text
	if last.Count > 1 {
		label = fmt.Sprintf("%s (x%d)", label, last.Count)
	}
	switch last.Level {
	case btui.NoticeLevelError:
		return theme.noticeError.Render(label)
	default:
		return theme.noticeInfo.Render(label)
	}
}

func fillANSIHorizontal(left, right string, width int) string {
	if width <= 0 {
		return left + right
	}
	leftW := xansi.StringWidth(left)
	rightW := xansi.StringWidth(right)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncateModernLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	return xansi.Truncate(line, width, "…")
}

func sortedNoticeLevels(notices []btui.Notice) []btui.NoticeLevel {
	levels := map[btui.NoticeLevel]struct{}{}
	for _, notice := range notices {
		levels[notice.Level] = struct{}{}
	}
	out := make([]btui.NoticeLevel, 0, len(levels))
	for level := range levels {
		out = append(out, level)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func modernSnapshotSize(snapshot *protocol.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	if snapshot.Size.Cols == 0 && snapshot.Size.Rows == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", snapshot.Size.Cols, snapshot.Size.Rows)
}
