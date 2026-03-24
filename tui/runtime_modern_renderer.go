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
	app              lipgloss.Style
	topBar           lipgloss.Style
	subBar           lipgloss.Style
	tab              lipgloss.Style
	activeTab        lipgloss.Style
	chip             lipgloss.Style
	activeChip       lipgloss.Style
	panel            lipgloss.Style
	activePanel      lipgloss.Style
	mutedPanel       lipgloss.Style
	panelTitle       lipgloss.Style
	panelMeta        lipgloss.Style
	terminalBody     lipgloss.Style
	noticeInfo       lipgloss.Style
	noticeWarn       lipgloss.Style
	noticeError      lipgloss.Style
	footer           lipgloss.Style
	modalBackdrop    lipgloss.Style
	modalPanel       lipgloss.Style
	modalTitle       lipgloss.Style
	modalMeta        lipgloss.Style
	modalBody        lipgloss.Style
	selectedListItem lipgloss.Style
	listItem         lipgloss.Style
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

	header := r.renderTopBar(theme, workspace, tab, pane, width)
	tabs := r.renderTabBar(theme, workspace, tab, width)
	context := r.renderContextBar(theme, state, pane, width)
	footer := r.renderFooter(theme, state, pane, notices, width)

	bodyHeight := height - 3
	if bodyHeight < 8 {
		bodyHeight = 8
	}

	body := r.renderWorkbench(theme, state, tab, pane, width, bodyHeight)
	if state.UI.Overlay.Kind != types.OverlayNone {
		body = r.renderOverlayViewport(theme, state, width, bodyHeight)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, tabs, context, body, footer)
	return theme.app.Render(view)
}

func (r modernScreenShellRenderer) renderTopBar(theme modernShellTheme, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, width int) string {
	left := lipgloss.JoinHorizontal(lipgloss.Left,
		theme.activeChip.Render("termx"),
		theme.chip.Render("workspace "+safeWorkspaceLabel(workspace)),
		theme.chip.Render("tab "+safeTabLabel(tab)),
	)
	right := theme.panelMeta.Render("pane " + string(pane.ID))
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
	left := lipgloss.JoinHorizontal(lipgloss.Left,
		theme.chip.Render(renderPaneTitle(state, pane)),
		theme.chip.Render(string(pane.SlotState)),
		theme.chip.Render("focus "+string(layer)),
	)
	right := theme.panelMeta.Render("mode " + string(mode))
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
	lines := r.renderPanePanelLines(theme, state, pane, bodyWidth, max(6, height-4), false)
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
	sidebarWidth := min(32, max(26, width/3))
	mainWidth := max(30, width-sidebarWidth-1)
	main := r.renderSingleWorkbench(theme, state, pane, mainWidth, height, true)

	sidebarLines := []string{theme.panelTitle.Render("Pane map")}
	for _, paneID := range tiledPaneIDs {
		targetPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		item := fmt.Sprintf("%s  %s  %s", paneID, renderPaneTitle(state, targetPane), targetPane.SlotState)
		if paneID == tab.ActivePaneID {
			sidebarLines = append(sidebarLines, theme.selectedListItem.Render("> "+truncateModernLine(item, sidebarWidth-8)))
			continue
		}
		sidebarLines = append(sidebarLines, theme.listItem.Render("  "+truncateModernLine(item, sidebarWidth-8)))
	}
	if len(floatingPaneIDs) > 0 {
		sidebarLines = append(sidebarLines, "", theme.panelTitle.Render("Floating"))
		for _, paneID := range floatingPaneIDs {
			targetPane, ok := tab.Panes[paneID]
			if !ok {
				continue
			}
			sidebarLines = append(sidebarLines, theme.listItem.Render("  "+truncateModernLine(renderPaneTitle(state, targetPane), sidebarWidth-8)))
		}
	}
	sidebar := theme.mutedPanel.Width(sidebarWidth - 2).Height(height - 2).Render(strings.Join(sidebarLines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, main, " ", sidebar)
}

func (r modernScreenShellRenderer) renderFloatingWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, width, height int) string {
	sidebarWidth := min(30, max(24, width/3))
	mainWidth := max(30, width-sidebarWidth-1)
	main := r.renderSingleWorkbench(theme, state, pane, mainWidth, height, true)

	lines := []string{theme.panelTitle.Render("Window stack")}
	for _, paneID := range floatingPaneIDs {
		targetPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		preview := r.renderPanePreview(targetPane.TerminalID)
		if preview == "" {
			switch targetPane.SlotState {
			case types.PaneSlotWaiting:
				preview = "waiting"
			case types.PaneSlotExited:
				preview = "exited"
			case types.PaneSlotEmpty:
				preview = "empty"
			default:
				preview = "live"
			}
		}
		label := fmt.Sprintf("%s  %s  %s", paneID, renderPaneTitle(state, targetPane), preview)
		if paneID == tab.ActivePaneID {
			lines = append(lines, theme.selectedListItem.Render("> "+truncateModernLine(label, sidebarWidth-8)))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+truncateModernLine(label, sidebarWidth-8)))
	}
	sidebar := theme.mutedPanel.Width(sidebarWidth - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, main, " ", sidebar)
}

// renderPanePanelLines 统一负责产品态 pane 卡片的正文。
// 这里把 connected / empty / waiting / exited 四态折叠成同一种视觉骨架，避免再回到旧版分叉渲染。
func (r modernScreenShellRenderer) renderPanePanelLines(theme modernShellTheme, state types.AppState, pane types.PaneState, width, maxRows int, includeTitle bool) []string {
	lines := make([]string, 0, maxRows)
	role := renderScreenShellPaneCardRole(state, pane)
	if role == "" {
		role = string(pane.SlotState)
	}
	if includeTitle {
		lines = append(lines, theme.panelTitle.Render(renderPaneTitle(state, pane)))
	}
	statusParts := []string{strings.ToUpper(string(pane.Kind))}
	if pane.TerminalID != "" {
		statusParts = append(statusParts, string(pane.TerminalID))
	}
	statusParts = append(statusParts, role)
	lines = append(lines, theme.panelMeta.Render(strings.Join(statusParts, "  ")))

	switch pane.SlotState {
	case types.PaneSlotEmpty:
		lines = append(lines,
			theme.terminalBody.Render("No terminal connected yet."),
			theme.panelMeta.Render("Press n to start one, or a to connect an existing terminal."),
		)
	case types.PaneSlotWaiting:
		lines = append(lines,
			theme.terminalBody.Render("Waiting for a terminal connection."),
			theme.panelMeta.Render("This pane is reserved by layout or restore flow."),
		)
	case types.PaneSlotExited:
		exitText := "history retained"
		if pane.LastExitCode != nil {
			exitText = fmt.Sprintf("history retained  exit %d", *pane.LastExitCode)
		}
		lines = append(lines,
			theme.terminalBody.Render("Terminal program exited."),
			theme.panelMeta.Render(exitText),
			theme.panelMeta.Render("Press r to restart, or a to connect another terminal."),
		)
	default:
		lines = append(lines, r.renderTerminalMetaLines(theme, state, pane, width)...)
		lines = append(lines, r.renderTerminalPreviewLines(theme, pane, width, maxRows-len(lines)-1)...)
	}

	return lines
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

func (r modernScreenShellRenderer) renderOverlayViewport(theme modernShellTheme, state types.AppState, width, height int) string {
	panelWidth := min(width-4, max(44, width*2/3))
	if panelWidth <= 0 {
		panelWidth = width
	}
	panel := r.renderOverlayPanel(theme, state, panelWidth)
	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		panel,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#08111d")),
	)
}

func (r modernScreenShellRenderer) renderOverlayPanel(theme modernShellTheme, state types.AppState, width int) string {
	title := overlayTitle(state.UI.Overlay.Kind)
	lines := []string{theme.modalTitle.Render(title)}
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		lines = append(lines, theme.modalMeta.Render("return to "+returnFocus))
	}
	lines = append(lines, r.renderOverlayPanelBody(theme, state.UI.Overlay, width-6)...)
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
			theme.modalBody.Render(truncateModernLine("Ctrl-p pane  •  Ctrl-t tab  •  Ctrl-w workspace  •  Ctrl-f picker", width)),
			theme.modalBody.Render(truncateModernLine("Ctrl-o floating  •  Ctrl-g global  •  Esc close", width)),
			theme.modalMeta.Render("pane is the view slot, terminal is the running entity"),
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
	lines := []string{
		theme.modalMeta.Render(fmt.Sprintf("search %q", manager.Query())),
	}
	rows := manager.VisibleRows()
	selected, ok := manager.SelectedRow()
	if ok {
		lines = append(lines, theme.modalBody.Render(truncateModernLine("selected "+activeRowLabel(selected, true), width)))
	}
	lines = append(lines, theme.modalMeta.Render(fmt.Sprintf("%d rows  •  Enter here  •  t new tab  •  o floating", len(rows))))
	for _, line := range modernTerminalManagerRowPreview(theme, rows, selected, ok, width) {
		lines = append(lines, line)
	}
	return lines
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
		label := row.Label
		if row.TerminalID != "" {
			label = fmt.Sprintf("%s  (%s)", label, row.TerminalID)
		}
		text := truncateModernLine(label, width)
		if hasSelected && row.Kind == selected.Kind && row.Label == selected.Label && row.TerminalID == selected.TerminalID {
			preview = append(preview, theme.selectedListItem.Render("> "+text))
			continue
		}
		preview = append(preview, theme.listItem.Render("  "+text))
	}
	return preview
}

func renderModernWorkspacePickerOverlay(theme modernShellTheme, picker *workspacedomain.PickerState, width int) []string {
	if picker == nil {
		return []string{theme.modalBody.Render("No workspace tree loaded yet.")}
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	lines := []string{theme.modalMeta.Render(fmt.Sprintf("query %q  •  %d rows", picker.Query(), len(rows)))}
	if hasSelected {
		lines = append(lines, theme.modalBody.Render(fmt.Sprintf("selected %s  •  %s", selectedRow.Node.Label, selectedRow.Node.Kind)))
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
	slice, _ := overlayPreviewRowsAround(rows, 5, selectedIndex)
	for _, row := range slice {
		label := strings.Repeat("  ", row.Depth) + row.Node.Label
		label = truncateModernLine(label, width)
		if hasSelected && row.Node.Key == selectedRow.Node.Key {
			lines = append(lines, theme.selectedListItem.Render("> "+label))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+label))
	}
	return lines
}

func renderModernTerminalPickerOverlay(theme modernShellTheme, picker *terminalpickerdomain.State, width int) []string {
	if picker == nil {
		return []string{theme.modalBody.Render("No terminal options loaded yet.")}
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	lines := []string{theme.modalMeta.Render(fmt.Sprintf("query %q  •  %d rows", picker.Query(), len(rows)))}
	if hasSelected {
		lines = append(lines, theme.modalBody.Render("selected " + truncateModernLine(selectedRow.Label, width)))
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
	slice, _ := overlayPreviewRowsAround(rows, 4, selectedIndex)
	for _, row := range slice {
		text := row.Label
		if row.TerminalID != "" {
			text += "  (" + string(row.TerminalID) + ")"
		}
		text = truncateModernLine(text, width)
		if hasSelected && row.Kind == selectedRow.Kind && row.Label == selectedRow.Label && row.TerminalID == selectedRow.TerminalID {
			lines = append(lines, theme.selectedListItem.Render("> "+text))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+text))
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
		lines = append(lines, theme.modalBody.Render(truncateModernLine(resolve.Hint, width)))
	}
	rows := resolve.Rows()
	selectedRow, hasSelected := resolve.SelectedRow()
	selectedIndex := 0
	if hasSelected {
		for idx, row := range rows {
			if row.Action == selectedRow.Action && row.Label == selectedRow.Label {
				selectedIndex = idx
				break
			}
		}
	}
	slice, _ := overlayPreviewRowsAround(rows, 4, selectedIndex)
	for _, row := range slice {
		text := truncateModernLine(string(row.Action)+"  "+row.Label, width)
		if hasSelected && row.Action == selectedRow.Action && row.Label == selectedRow.Label {
			lines = append(lines, theme.selectedListItem.Render("> "+text))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+text))
	}
	return lines
}

func renderModernPromptOverlay(theme modernShellTheme, prompt *promptdomain.State, width int) []string {
	if prompt == nil {
		return []string{theme.modalBody.Render("Prompt not ready.")}
	}
	if len(prompt.Fields) == 0 {
		return []string{
			theme.modalMeta.Render("draft mode"),
			theme.modalBody.Render(truncateModernLine(prompt.Draft, width)),
		}
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	lines := []string{
		theme.modalMeta.Render(fmt.Sprintf("%d fields  •  active %s", len(prompt.Fields), prompt.Fields[active].Key)),
	}
	for idx, field := range prompt.Fields {
		text := truncateModernLine(fmt.Sprintf("%s: %s", field.Label, field.Value), width)
		if idx == active {
			lines = append(lines, theme.selectedListItem.Render("> "+text))
			continue
		}
		lines = append(lines, theme.listItem.Render("  "+text))
	}
	return lines
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
