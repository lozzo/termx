package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	layoutdomain "github.com/lozzow/termx/tui/domain/layout"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type runtimeRenderer struct {
	Screens      RuntimeTerminalStore
	DebugVisible *bool
}

type wireframeMetrics struct {
	ViewportWidth    int
	ViewportHeight   int
	OverlayWidth     int
	SplitColumnWidth int
	MainPaneWidth    int
	SidebarWidth     int
}

type shellBorderStyle struct {
	Corner     byte
	Horizontal byte
	Vertical   byte
}

const runtimeScreenPreviewRows = 8
const runtimeOverlayPreviewRows = 8
const runtimeOverlayDetailPreviewRows = 4
const runtimeTerminalManagerPreviewRows = 4
const runtimeBarMaxWidth = 96
const runtimeSummaryMaxWidth = 240
const runtimeDetailMaxWidth = 240
const runtimeOutlinePreviewMaxWidth = 48
const runtimeWireframeWidth = 78
const runtimeWireframeOverlayWidth = 58
const runtimeWireframeSplitColumnWidth = 38
const runtimeWireframeMainPaneWidth = 52
const runtimeWireframeSidebarWidth = 24
const runtimeWireframePreviewRows = 4

var (
	defaultShellBorderStyle = shellBorderStyle{
		Corner:     '+',
		Horizontal: '-',
		Vertical:   '|',
	}
	emphasisShellBorderStyle = shellBorderStyle{
		Corner:     '#',
		Horizontal: '#',
		Vertical:   '#',
	}
)

// Render 先提供一个稳定、可测试的文本视图，优先把生命周期打通。
// 这里不追求视觉完成度，只把当前 workspace / tab / pane / overlay 这些主语义明确展示出来。
func (r runtimeRenderer) Render(state types.AppState, notices []btui.Notice) string {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return "termx\nno workspace"
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return fmt.Sprintf("termx\nworkspace: %s\nno tab", workspace.Name)
	}
	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		return fmt.Sprintf("termx\nworkspace: %s\ntab: %s\nno pane", workspace.Name, tab.Name)
	}
	if !r.debugVisible() {
		return modernScreenShellRenderer{Screens: r.Screens}.RenderShell(state, workspace, tab, pane, notices, r.wireframeMetrics(pane))
	}

	statusLines := renderStatusSection(workspace, tab, pane, state.UI)
	overlayActive := state.UI.Overlay.Kind != types.OverlayNone

	lines := []string{"termx"}
	lines = append(lines, r.renderScreenShell(state, workspace, tab, pane, notices)...)
	lines = append(lines, r.renderWireframeView(state, workspace, tab, pane)...)
	lines = appendChrome(lines, "header", []string{
		renderHeaderBar(workspace, tab, pane, state.UI),
		renderWorkspaceBar(workspace),
		renderWorkspaceSummary(workspace),
		renderTabStrip(workspace),
		renderTabSummary(tab),
		renderTabPathBar(state, workspace, tab, pane),
		renderTabLayerBar(tab),
	}, func(lines []string) []string {
		return appendSection(lines, "status", statusLines)
	})
	lines = appendChrome(lines, "body", []string{r.renderBodyBar(state, pane, overlayActive)}, func(lines []string) []string {
		lines = append(lines, renderPaneBar(state, pane))
		lines = append(lines, renderTiledOutlineBar(tab)...)
		lines = append(lines, renderTiledLayout(tab)...)
		lines = append(lines, r.renderTiledTree(state, tab)...)
		lines = append(lines, r.renderTiledOutline(state, tab)...)
		lines = append(lines, renderFloatingOutlineBar(tab)...)
		lines = append(lines, r.renderFloatingOutline(state, tab)...)
		lines = appendSection(lines, "terminal", r.renderTerminalSection(state, pane, overlayActive))
		lines = appendSection(lines, "screen", r.renderScreenSection(pane, overlayActive))
		return appendSection(lines, "overlay", renderOverlayLines(state.UI.Overlay, state.UI.Focus))
	})
	lines = appendChrome(lines, "footer", []string{
		renderFooterBar(notices, state.UI.Overlay.Kind),
		renderFocusBar(state, pane),
		renderShortcutBar(state, pane),
	}, func(lines []string) []string {
		return appendSection(lines, "notices", renderNoticeLines(notices))
	})
	return strings.Join(lines, "\n")
}

func (r runtimeRenderer) debugVisible() bool {
	if r.DebugVisible == nil {
		return true
	}
	return *r.DebugVisible
}

func appendSection(lines []string, name string, body []string) []string {
	if len(body) == 0 {
		return lines
	}
	lines = append(lines, fmt.Sprintf("section_%s:", name))
	return append(lines, body...)
}

func appendChrome(lines []string, name string, body []string, fn func([]string) []string) []string {
	lines = append(lines, fmt.Sprintf("chrome_%s:", name))
	lines = append(lines, body...)
	if fn != nil {
		return fn(lines)
	}
	return lines
}

// renderScreenShell 提供一个真正优先展示给用户的稳定外壳，
// 让启动后的第一屏先有 workspace/tab/pane/frame/footer，而不是只看到调试型语义字段。
func (r runtimeRenderer) renderScreenShell(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, notices []btui.Notice) []string {
	metrics := r.wireframeMetrics(pane)
	lines := []string{"screen_shell:"}
	overlayActive := state.UI.Overlay.Kind != types.OverlayNone
	body := []string{renderScreenShellHeader(workspace, tab, pane)}
	if overlayActive {
		body = append(body, renderScreenShellWorkspaceTabsLine(workspace))
	} else {
		body = append(body,
			renderScreenShellWorkspaceSummaryLine(workspace),
			renderScreenShellTabStripLine(workspace),
		)
	}
	body = append(body, renderScreenShellStateLine(state, tab, pane))
	body = append(body,
		renderScreenShellTargetLine(state, workspace, tab, pane),
		renderScreenShellPathLine(state, workspace, tab, pane),
	)
	if metaLine := r.renderScreenShellTerminalMetaLine(state, pane); metaLine != "" {
		body = append(body, metaLine)
	}
	if statusLine := renderScreenShellPaneStatusLine(state, pane); statusLine != "" {
		body = append(body, statusLine)
	}
	if actionLine := renderScreenShellActionLine(state, pane); actionLine != "" {
		body = append(body, actionLine)
	}
	body = append(body, r.renderScreenShellWorkbench(state, tab, pane, metrics, overlayActive)...)
	if overlayActive {
		body = append(body, renderScreenShellMask(state, metrics))
		body = append(body, r.renderScreenShellDialog(state, metrics)...)
	}
	if noticeLine := renderScreenShellNoticeLine(notices, overlayActive); noticeLine != "" {
		body = append(body, noticeLine)
	}
	body = append(body, renderScreenShellFooter(state, pane))
	lines = append(lines, renderShellBox(metrics.ViewportWidth+2, renderScreenShellFrameTitle(state, metrics), body)...)
	return lines
}

func renderScreenShellMask(state types.AppState, metrics wireframeMetrics) string {
	returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus)
	if returnFocus == "" {
		returnFocus = "none"
	}
	return fmt.Sprintf("MASK[dimmed %dx%d] OVERLAY[%s] RETURN[%s]", metrics.ViewportWidth, metrics.ViewportHeight, state.UI.Overlay.Kind, returnFocus)
}

func renderScreenShellHeader(workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) string {
	return compactSummaryLine(
		fmt.Sprintf("Workspace %s", safeWorkspaceLabel(workspace)),
		fmt.Sprintf("Tab %s", safeTabLabel(tab)),
	)
}

func renderScreenShellWorkspaceSummaryLine(workspace types.WorkspaceState) string {
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
	return compactSummaryLine(
		fmt.Sprintf("Overview tabs %d", tabs),
		fmt.Sprintf("panes %d", panes),
		fmt.Sprintf("terminals %d", len(terminals)),
		fmt.Sprintf("floating %d", floating),
	)
}

func renderScreenShellTabStripLine(workspace types.WorkspaceState) string {
	if len(workspace.TabOrder) == 0 {
		return "Tabs none"
	}
	return compactSummaryLine(fmt.Sprintf("Tabs %s", renderScreenShellTabStripValue(workspace)))
}

func renderScreenShellTabStripValue(workspace types.WorkspaceState) string {
	parts := make([]string, 0, len(workspace.TabOrder))
	for _, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := safeTabLabel(tab)
		if tabID == workspace.ActiveTabID {
			parts = append(parts, fmt.Sprintf("[%s]", label))
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

func renderScreenShellWorkspaceTabsLine(workspace types.WorkspaceState) string {
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
	return compactSummaryLine(
		fmt.Sprintf("Workspace %s", safeWorkspaceLabel(workspace)),
		fmt.Sprintf("tabs %d", tabs),
		fmt.Sprintf("panes %d", panes),
		fmt.Sprintf("terminals %d", len(terminals)),
		fmt.Sprintf("floating %d", floating),
		fmt.Sprintf("Tabs %s", renderScreenShellTabStripValue(workspace)),
	)
}

func renderScreenShellFrameTitle(state types.AppState, metrics wireframeMetrics) string {
	return compactSummaryLine(
		"termx workbench",
		fmt.Sprintf("%dx%d", metrics.ViewportWidth, metrics.ViewportHeight),
		fmt.Sprintf("overlay %s", state.UI.Overlay.Kind),
	)
}

func renderScreenShellStateLine(state types.AppState, tab types.TabState, pane types.PaneState) string {
	layer := tab.ActiveLayer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	focus := state.UI.Focus.Layer
	if focus == "" {
		focus = types.FocusLayerTiled
	}
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	return compactSummaryLine(
		fmt.Sprintf("Workbench %s", layer),
		fmt.Sprintf("focus %s", focus),
		fmt.Sprintf("mode %s", mode),
		fmt.Sprintf("overlay %s", state.UI.Overlay.Kind),
	)
}

func renderScreenShellTargetLine(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) string {
	terminalID := "<none>"
	if pane.TerminalID != "" {
		terminalID = string(pane.TerminalID)
	}
	return compactSummaryLine(
		fmt.Sprintf("Active pane %s", renderPaneTitle(state, pane)),
		fmt.Sprintf("pane %s", pane.ID),
		fmt.Sprintf("terminal %s", terminalID),
		fmt.Sprintf("slot %s", pane.SlotState),
	)
}

func renderScreenShellPathLine(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) string {
	focus := state.UI.Focus.Layer
	if focus == "" {
		focus = types.FocusLayerTiled
	}
	return compactSummaryLine(
		fmt.Sprintf("Location %s / %s / %s / %s", safeWorkspaceLabel(workspace), safeTabLabel(tab), safePaneKind(pane.Kind), pane.ID),
		fmt.Sprintf("focus %s", focus),
		fmt.Sprintf("active %s", renderPaneTitle(state, pane)),
	)
}

func renderScreenShellFooter(state types.AppState, pane types.PaneState) string {
	parts := renderScreenShellFooterParts(state)
	return compactSummaryLine(fmt.Sprintf("Keys %s", strings.Join(parts, " | ")))
}

func renderScreenShellFooterParts(state types.AppState) []string {
	if state.UI.Overlay.Kind != types.OverlayNone {
		return []string{"Esc close", "? help"}
	}
	if state.UI.Mode.Active == types.ModeFloating {
		return []string{"h/l focus", "j/k move", "H/J/K/L size", "Esc exit", "? help"}
	}
	return []string{"Ctrl-p pane", "Ctrl-t tab", "Ctrl-w ws", "Ctrl-o float", "? help"}
}

// renderScreenShellTerminalMetaLine 把默认运行态下最关键的 terminal 元信息上收进主壳，
// 避免关闭 debug 区后，用户还得靠调试 renderer 才能知道 command / size / rows。
func (r runtimeRenderer) renderScreenShellTerminalMetaLine(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" || pane.SlotState != types.PaneSlotConnected {
		return ""
	}
	terminalState := types.TerminalRunStateRunning
	command := ""
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
		if terminal.State != "" {
			terminalState = terminal.State
		}
		if len(terminal.Command) > 0 {
			command = strings.Join(terminal.Command, " ")
		}
	}
	role := renderScreenShellPaneCardRole(state, pane)
	parts := []string{
		fmt.Sprintf("Terminal %s", pane.TerminalID),
		string(terminalState),
		role,
	}
	if conn := state.Domain.Connections[pane.TerminalID]; conn.TerminalID != "" && len(conn.ConnectedPaneIDs) > 0 {
		parts = append(parts, fmt.Sprintf("peers %d", len(conn.ConnectedPaneIDs)))
	}
	if r.Screens != nil {
		if status, ok := r.Screens.Status(pane.TerminalID); ok && (status.Size.Cols != 0 || status.Size.Rows != 0) {
			parts = append(parts, fmt.Sprintf("size %dx%d", status.Size.Cols, status.Size.Rows))
		}
		if snapshot, ok := r.Screens.Snapshot(pane.TerminalID); ok && snapshot != nil {
			rows, totalRows, _ := renderSnapshotRows(snapshot)
			parts = append(parts, fmt.Sprintf("preview %d/%d", len(rows), totalRows))
		}
	}
	if command != "" {
		parts = append(parts, fmt.Sprintf("cmd %s", command))
	}
	return compactSummaryLine(parts...)
}

func renderScreenShellPaneStatusLine(state types.AppState, pane types.PaneState) string {
	switch pane.SlotState {
	case types.PaneSlotEmpty:
		return "Status empty pane | no terminal connected"
	case types.PaneSlotWaiting:
		return "Status waiting pane | connect pending"
	case types.PaneSlotExited:
		if pane.LastExitCode != nil {
			return fmt.Sprintf("Status exited pane | history retained | exit %d", *pane.LastExitCode)
		}
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.ExitCode != nil {
			return fmt.Sprintf("Status exited pane | history retained | exit %d", *terminal.ExitCode)
		}
		return "Status exited pane | history retained"
	default:
		return ""
	}
}

func renderScreenShellActionLine(state types.AppState, pane types.PaneState) string {
	switch state.UI.Overlay.Kind {
	case types.OverlayHelp:
		return "Actions Esc close | ? help"
	case types.OverlayTerminalPicker, types.OverlayWorkspacePicker, types.OverlayLayoutResolve:
		return "Actions Enter confirm | Esc close | ? help"
	case types.OverlayTerminalManager:
		return "Actions Enter here | t new tab | o float | e edit | k stop | Esc close | ? help"
	case types.OverlayPrompt:
		return "Actions Enter submit | Esc close | ? help"
	}
	if state.UI.Mode.Active == types.ModeFloating {
		return "Actions h/l focus | j/k move | H/J/K/L size | [/] z | c center | x close | Esc exit | ? help"
	}
	switch pane.SlotState {
	case types.PaneSlotConnected:
		return "Actions type in terminal | Ctrl-g global | Ctrl-f picker | ? help"
	case types.PaneSlotEmpty, types.PaneSlotWaiting:
		return "Actions n new | a connect | m manager | x close | ? help"
	case types.PaneSlotExited:
		return "Actions r restart | a connect | x close | ? help"
	default:
		return ""
	}
}

// renderScreenShellNoticeLine 把 notice 汇总搬进第一视觉 shell，
// 让用户不需要滚到调试 footer 才知道当前是否有错误或提示。
func renderScreenShellNoticeLine(notices []btui.Notice, overlayActive bool) string {
	total := countVisibleNotices(notices)
	if total == 0 {
		if overlayActive {
			return ""
		}
		return "Notice none"
	}
	last, ok := lastVisibleNotice(notices)
	if !ok {
		return fmt.Sprintf("Notice %d", total)
	}
	text := last.Text
	if last.Count > 1 {
		text = fmt.Sprintf("%s (x%d)", text, last.Count)
	}
	return compactSummaryLine(
		fmt.Sprintf("Notice %d %s", total, last.Level),
		text,
	)
}

func (r runtimeRenderer) renderScreenShellWorkbench(state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, overlayActive bool) []string {
	tiledPaneIDs := orderedTiledPaneIDs(tab)
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	switch {
	case len(tiledPaneIDs) > 1:
		return r.renderScreenShellSplit(state, tab, tiledPaneIDs, floatingPaneIDs, metrics, overlayActive)
	case len(floatingPaneIDs) > 0:
		return r.renderScreenShellFloating(state, tab, pane, floatingPaneIDs, metrics, overlayActive)
	default:
		bodyRows := renderScreenShellSinglePaneBodyRows(metrics, overlayActive)
		return renderScreenShellPaneBox(metrics.ViewportWidth, renderScreenShellPaneTitle(state, pane, true), r.renderScreenShellPaneLines(state, pane, overlayActive, bodyRows), true)
	}
}

func renderScreenShellSinglePaneBodyRows(metrics wireframeMetrics, overlayActive bool) int {
	if overlayActive {
		return 2
	}
	switch {
	case metrics.ViewportHeight >= 60:
		return 8
	case metrics.ViewportHeight >= 36:
		return 6
	default:
		return 4
	}
}

func (r runtimeRenderer) renderScreenShellSplit(state types.AppState, tab types.TabState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, overlayActive bool) []string {
	summary := summarizeTiledLayout(tab.RootSplit, len(tiledPaneIDs))
	ratio := "n/a"
	if summary.HasRatio {
		ratio = fmt.Sprintf("%02.0f/%02.0f", summary.Ratio*100, (1-summary.Ratio)*100)
	}
	lines := []string{
		fmt.Sprintf("SPLIT SHELL[%s %s]", summary.Root, ratio),
		fmt.Sprintf("LAYOUT[split] root=%s ratio=%s leaves=%d", summary.Root, ratio, len(tiledPaneIDs)),
	}
	lines = append(lines, r.renderScreenShellTiledCanvas(state, tab, tiledPaneIDs, metrics, overlayActive)...)
	if len(floatingPaneIDs) > 0 {
		lines = append(lines, fmt.Sprintf("FLOATING WINDOWS[%d]", len(floatingPaneIDs)))
		cardWidth := metrics.SplitColumnWidth
		if len(floatingPaneIDs) == 1 {
			cardWidth = metrics.ViewportWidth
		}
		lines = append(lines, r.renderScreenShellWindowCards(state, tab, floatingPaneIDs, cardWidth, overlayActive)...)
	}
	return lines
}

func (r runtimeRenderer) renderScreenShellFloating(state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, overlayActive bool) []string {
	lines := []string{fmt.Sprintf("FLOAT SHELL[%d]", len(floatingPaneIDs))}
	lines = append(lines, r.renderScreenShellFloatingCanvas(state, tab, pane, floatingPaneIDs, metrics, overlayActive)...)
	sidebarBox := renderShellBox(metrics.ViewportWidth, "STACK[windows]", r.renderScreenShellFloatingSidebar(state, tab, floatingPaneIDs))
	lines = append(lines, sidebarBox...)
	lines = append(lines, fmt.Sprintf("WINDOWS[%d]", len(floatingPaneIDs)))
	lines = append(lines, r.renderScreenShellWindowCards(state, tab, floatingPaneIDs, metrics.SplitColumnWidth, overlayActive)...)
	return lines
}

func (r runtimeRenderer) renderScreenShellFloatingSidebar(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) []string {
	lines := []string{fmt.Sprintf("STACK[windows] total=%d", len(floatingPaneIDs))}
	if len(floatingPaneIDs) > 0 {
		if activePane, ok := tab.Panes[tab.ActivePaneID]; ok {
			lines = append(lines, fmt.Sprintf("FOCUS[%s] %s", tab.ActivePaneID, renderPaneTitle(state, activePane)))
		}
	}
	for _, paneID := range floatingPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		prefix := "  "
		if paneID == tab.ActivePaneID {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, paneID, renderPaneTitle(state, pane)))
	}
	return lines
}

func (r runtimeRenderer) renderScreenShellExtraPaneCards(state types.AppState, tab types.TabState, paneIDs []types.PaneID, width int, overlayActive bool) []string {
	boxes := make([][]string, 0, len(paneIDs))
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		body := []string{fmt.Sprintf("SLOT[%s]", pane.SlotState)}
		body = append(body, r.renderScreenShellPaneLines(state, pane, overlayActive, 4)...)
		boxes = append(boxes, renderScreenShellPaneBox(width, fmt.Sprintf("CARD[%s] %s [%s]", pane.ID, renderPaneTitle(state, pane), renderScreenShellPaneCardRole(state, pane)), body, pane.ID == tab.ActivePaneID))
	}
	return renderShellBoxGrid(boxes, 2, 2)
}

func (r runtimeRenderer) renderScreenShellWindowCards(state types.AppState, tab types.TabState, paneIDs []types.PaneID, width int, overlayActive bool) []string {
	lines := []string{fmt.Sprintf("WINDOW LIST[%d]", len(paneIDs))}
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		lines = append(lines, r.renderScreenShellWindowSummaryLine(state, tab, paneID, pane, overlayActive))
	}
	return lines
}

func (r runtimeRenderer) renderScreenShellDialog(state types.AppState, metrics wireframeMetrics) []string {
	if state.UI.Overlay.Kind == types.OverlayNone {
		return nil
	}
	body := []string{fmt.Sprintf("TITLE[%s]", state.UI.Overlay.Kind)}
	if !usesStructuredScreenShellDialog(state.UI.Overlay.Kind) {
		body = append([]string{fmt.Sprintf("overlay active: %s", state.UI.Overlay.Kind)}, body...)
	}
	body = append(body, r.renderScreenShellDialogBody(state)...)
	box := renderShellBoxWithStyle(metrics.OverlayWidth, fmt.Sprintf("DIALOG[%s]", state.UI.Overlay.Kind), body, emphasisShellBorderStyle)
	padding := (metrics.ViewportWidth - metrics.OverlayWidth) / 2
	if padding < 0 {
		padding = 0
	}
	return indentLines(box, padding)
}

func (r runtimeRenderer) renderScreenShellDialogBody(state types.AppState) []string {
	lines := make([]string, 0, 5)
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		lines = append(lines, fmt.Sprintf("RETURN TO[%s]", returnFocus))
	}
	lines = append(lines, renderScreenShellDialogSections(state.UI.Overlay)...)
	if footer, actions := renderScreenShellDialogFooter(state.UI.Overlay.Kind); footer != "" || actions != "" {
		if footer != "" {
			lines = append(lines, footer)
		}
		if actions != "" {
			lines = append(lines, actions)
		}
	}
	return lines
}

// renderScreenShellDialogSections 把 overlay 壳层正文投影成稳定的 list/detail/fields 分区。
// 这样 screen_shell 可以先给用户一个“像真实对话框”的最小 UI，而不是只复读 wireframe 摘要。
func renderScreenShellDialogSections(overlay types.OverlayState) []string {
	switch overlay.Kind {
	case types.OverlayTerminalManager:
		return renderScreenShellTerminalManagerDialogBody(overlay)
	case types.OverlayWorkspacePicker:
		return renderScreenShellWorkspacePickerDialogBody(overlay)
	case types.OverlayTerminalPicker:
		return renderScreenShellTerminalPickerDialogBody(overlay)
	case types.OverlayLayoutResolve:
		return renderScreenShellLayoutResolveDialogBody(overlay)
	case types.OverlayPrompt:
		return renderScreenShellPromptDialogBody(overlay)
	default:
		return renderWireframeOverlayBody(overlay)
	}
}

func usesStructuredScreenShellDialog(kind types.OverlayKind) bool {
	switch kind {
	case types.OverlayTerminalManager, types.OverlayWorkspacePicker, types.OverlayTerminalPicker, types.OverlayLayoutResolve, types.OverlayPrompt:
		return true
	default:
		return false
	}
}

func renderScreenShellTerminalManagerDialogBody(overlay types.OverlayState) []string {
	manager, ok := overlay.Data.(*terminalmanagerdomain.State)
	if !ok || manager == nil {
		return renderShellBox(runtimeWireframeOverlayWidth-2, "LIST[terminals]", []string{"BODY[list] rows=0 selected=none"})
	}
	rows := manager.VisibleRows()
	selectedRow, hasSelected := manager.SelectedRow()
	selectedID := "none"
	selectedIndex := 0
	if hasSelected {
		if selectedRow.TerminalID != "" {
			selectedID = string(selectedRow.TerminalID)
		} else {
			selectedID = selectedRow.Label
		}
		for idx, candidate := range rows {
			if candidate.Kind == selectedRow.Kind && candidate.TerminalID == selectedRow.TerminalID && candidate.Label == selectedRow.Label {
				selectedIndex = idx
				break
			}
		}
	}
	selectedSection := "none"
	if hasSelected {
		selectedSection = string(selectedRow.Section)
	}
	listBody := []string{
		fmt.Sprintf("F:%s %s", selectedSection, activeRowLabel(selectedRow, hasSelected)),
		fmt.Sprintf("rows=%d sel=%s q=%s", len(rows), selectedID, manager.Query()),
	}
	previewRows, _ := overlayPreviewRowsAround(rows, 1, selectedIndex)
	for _, row := range previewRows {
		prefix := "  "
		if hasSelected && selectedRow.Kind == row.Kind && selectedRow.TerminalID == row.TerminalID && selectedRow.Label == row.Label {
			prefix = ">> "
		}
		listBody = append(listBody, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	detailBody := []string{"D:none"}
	if detail, ok := manager.SelectedDetail(); ok {
		owner := detail.OwnerSlotLabel
		if owner == "" {
			owner = "-"
		}
		detailBody = []string{
			fmt.Sprintf("D:%s %s", detail.Name, detail.TerminalID),
			fmt.Sprintf("state=%s vis=%s", detail.State, detail.VisibilityLabel),
			fmt.Sprintf("owner=%s", owner),
			fmt.Sprintf("conn=%d loc=%d", detail.ConnectedPaneCount, len(detail.Locations)),
		}
		if detail.Command != "" {
			detailBody = append(detailBody, "BODY[command]", detail.Command)
		}
	}
	return joinASCIIBoxes([][]string{
		renderShellBox(27, "LIST[terminals]", listBody),
		renderShellBox(27, "DETAIL[terminal]", detailBody),
	}, 2)
}

func renderScreenShellWorkspacePickerDialogBody(overlay types.OverlayState) []string {
	picker, ok := overlay.Data.(*workspacedomain.PickerState)
	if !ok || picker == nil {
		return renderShellBox(runtimeWireframeOverlayWidth-2, "TREE[workspace]", []string{"BODY[tree] rows=0 selected=none"})
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	selectedKey := "none"
	selectedIndex := 0
	if hasSelected {
		selectedKey = selectedRow.Node.Key
		for idx, row := range rows {
			if row.Node.Key == selectedRow.Node.Key {
				selectedIndex = idx
				break
			}
		}
	}
	treeBody := []string{
		fmt.Sprintf("F:q=%s sel=%s", picker.Query(), selectedKey),
		fmt.Sprintf("rows=%d", len(rows)),
	}
	targetBody := []string{"D:none", "kind=none depth=0", "label=none"}
	if hasSelected {
		targetBody = []string{
			fmt.Sprintf("D:%s", selectedRow.Node.Key),
			fmt.Sprintf("kind=%s depth=%d", selectedRow.Node.Kind, selectedRow.Depth),
			fmt.Sprintf("label=%s", selectedRow.Node.Label),
		}
	}
	previewRows, _ := overlayPreviewRowsAround(rows, 5, selectedIndex)
	for _, row := range previewRows {
		prefix := "  "
		if hasSelected && row.Node.Key == selectedRow.Node.Key {
			prefix = ">> "
		}
		treeBody = append(treeBody, fmt.Sprintf("%s%s%s", prefix, strings.Repeat("  ", row.Depth), renderWorkspacePickerRowLabel(row)))
	}
	return joinASCIIBoxes([][]string{
		renderShellBox(30, "TREE[workspace]", treeBody),
		renderShellBox(24, "TARGET[node]", targetBody),
	}, 2)
}

func renderWorkspacePickerRowLabel(row workspacedomain.TreeRow) string {
	if row.Node.Kind == workspacedomain.TreeNodeKindCreate {
		return "+ workspace"
	}
	switch row.Node.Kind {
	case workspacedomain.TreeNodeKindWorkspace:
		return "ws " + row.Node.Label
	case workspacedomain.TreeNodeKindTab:
		return "tab " + row.Node.Label
	case workspacedomain.TreeNodeKindPane:
		return "pane " + row.Node.Label
	default:
		return fmt.Sprintf("[%s] %s", row.Node.Kind, row.Node.Label)
	}
}

func renderScreenShellTerminalPickerDialogBody(overlay types.OverlayState) []string {
	picker, ok := overlay.Data.(*terminalpickerdomain.State)
	if !ok || picker == nil {
		return joinASCIIBoxes([][]string{
			renderShellBox(30, "LIST[picker]", []string{"F:q= sel=none", "rows=0"}),
			renderShellBox(24, "DETAIL[target]", []string{"D:none"}),
		}, 2)
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	selectedID := "none"
	selectedIndex := 0
	if hasSelected {
		if selectedRow.TerminalID != "" {
			selectedID = string(selectedRow.TerminalID)
		} else {
			selectedID = "create"
		}
		for idx, row := range rows {
			if row.Kind == selectedRow.Kind && row.TerminalID == selectedRow.TerminalID && row.Label == selectedRow.Label {
				selectedIndex = idx
				break
			}
		}
	}
	listBody := []string{
		fmt.Sprintf("F:q=%s %s", picker.Query(), selectedID),
		fmt.Sprintf("rows=%d", len(rows)),
	}
	previewRows, _ := overlayPreviewRowsAround(rows, 5, selectedIndex)
	for _, row := range previewRows {
		prefix := "  "
		if hasSelected && row.Kind == selectedRow.Kind && row.TerminalID == selectedRow.TerminalID && row.Label == selectedRow.Label {
			prefix = ">> "
		}
		listBody = append(listBody, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	detailBody := []string{"D:none"}
	if hasSelected {
		switch selectedRow.Kind {
		case terminalpickerdomain.RowKindCreate:
			detailBody = []string{
				"D:create",
				"kind=create",
				"BODY[action]",
				"create and connect",
			}
		case terminalpickerdomain.RowKindTerminal:
			tags := renderTerminalTags(selectedRow.Tags)
			if tags == "" {
				tags = "-"
			}
			visibility := "hidden"
			if selectedRow.Visible {
				visibility = "visible"
			}
			detailBody = []string{
				fmt.Sprintf("D:%s %s", selectedRow.Label, selectedRow.TerminalID),
				fmt.Sprintf("state=%s vis=%s", selectedRow.State, visibility),
				fmt.Sprintf("conn=%d", selectedRow.ConnectedPaneCount),
				fmt.Sprintf("tags=%s", tags),
			}
			if selectedRow.Command != "" {
				detailBody = append(detailBody, "BODY[command]", selectedRow.Command)
			}
		}
	}
	return joinASCIIBoxes([][]string{
		renderShellBox(27, "LIST[picker]", listBody),
		renderShellBox(27, "DETAIL[target]", detailBody),
	}, 2)
}

func renderScreenShellLayoutResolveDialogBody(overlay types.OverlayState) []string {
	resolve, ok := overlay.Data.(*layoutresolvedomain.State)
	if !ok || resolve == nil {
		return joinASCIIBoxes([][]string{
			renderShellBox(30, "LIST[resolve]", []string{"F:pane=none sel=none", "rows=0"}),
			renderShellBox(24, "DETAIL[target]", []string{"D:none"}),
		}, 2)
	}
	rows := resolve.Rows()
	selectedRow, hasSelected := resolve.SelectedRow()
	selectedAction := "none"
	selectedIndex := 0
	if hasSelected {
		selectedAction = string(selectedRow.Action)
		for idx, row := range rows {
			if row.Action == selectedRow.Action && row.Label == selectedRow.Label {
				selectedIndex = idx
				break
			}
		}
	}
	listBody := []string{
		fmt.Sprintf("F:%s %s", resolve.PaneID, selectedAction),
		fmt.Sprintf("rows=%d", len(rows)),
	}
	previewRows, _ := overlayPreviewRowsAround(rows, 5, selectedIndex)
	for _, row := range previewRows {
		prefix := "  "
		if hasSelected && row.Action == selectedRow.Action && row.Label == selectedRow.Label {
			prefix = ">> "
		}
		listBody = append(listBody, fmt.Sprintf("%s%s", prefix, renderLayoutResolveRowLabel(row)))
	}
	actionSummary := "none"
	if hasSelected {
		switch selectedRow.Action {
		case layoutresolvedomain.ActionConnectExisting:
			actionSummary = "connect selected terminal"
		case layoutresolvedomain.ActionCreateNew:
			actionSummary = "create and connect"
		case layoutresolvedomain.ActionSkip:
			actionSummary = "keep pane waiting"
		}
	}
	detailBody := []string{
		fmt.Sprintf("D:%s", resolve.PaneID),
		fmt.Sprintf("role=%s", resolve.Role),
		fmt.Sprintf("hint=%s", resolve.Hint),
		"BODY[action]",
		actionSummary,
	}
	return joinASCIIBoxes([][]string{
		renderShellBox(27, "LIST[resolve]", listBody),
		renderShellBox(27, "DETAIL[target]", detailBody),
	}, 2)
}

func renderLayoutResolveRowLabel(row layoutresolvedomain.Row) string {
	switch row.Action {
	case layoutresolvedomain.ActionConnectExisting:
		return "connect existing"
	case layoutresolvedomain.ActionCreateNew:
		return "create terminal"
	case layoutresolvedomain.ActionSkip:
		return "keep waiting"
	default:
		return fmt.Sprintf("[%s] %s", row.Action, row.Label)
	}
}

func renderScreenShellPromptDialogBody(overlay types.OverlayState) []string {
	prompt, ok := overlay.Data.(*promptdomain.State)
	if !ok || prompt == nil {
		return joinASCIIBoxes([][]string{
			renderShellBox(30, "FIELDS[prompt]", []string{"count=0 f=draft"}),
			renderShellBox(24, "ACTIVE[field]", []string{"D:draft", "BODY[actions]", "submit | cancel"}),
		}, 2)
	}
	if len(prompt.Fields) == 0 {
		return joinASCIIBoxes([][]string{
			renderShellBox(30, "FIELDS[prompt]", []string{
				"count=0 f=draft",
				fmt.Sprintf(">> [draft] %s", prompt.Draft),
			}),
			renderShellBox(24, "ACTIVE[field]", []string{
				"D:draft",
				"label=draft",
				fmt.Sprintf("terminal=%s", prompt.TerminalID),
				"BODY[actions]",
				"submit | cancel",
			}),
		}, 2)
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	fieldsBody := []string{
		fmt.Sprintf("count=%d f=%s", len(prompt.Fields), prompt.Fields[active].Key),
		fmt.Sprintf("active=%s", prompt.Fields[active].Key),
	}
	previewFields, _ := overlayPreviewRowsAround(prompt.Fields, 4, active)
	for _, field := range previewFields {
		prefix := "  "
		if field.Key == prompt.Fields[active].Key && field.Label == prompt.Fields[active].Label {
			prefix = ">> "
		}
		fieldsBody = append(fieldsBody, fmt.Sprintf("%s[%s] %s: %s", prefix, field.Key, field.Label, field.Value))
	}
	activeBody := []string{
		fmt.Sprintf("D:%s", prompt.Fields[active].Key),
		fmt.Sprintf("label=%s", prompt.Fields[active].Label),
		fmt.Sprintf("terminal=%s", prompt.TerminalID),
		"BODY[actions]",
		"submit | cancel",
	}
	return joinASCIIBoxes([][]string{
		renderShellBox(30, "FIELDS[prompt]", fieldsBody),
		renderShellBox(24, "ACTIVE[field]", activeBody),
	}, 2)
}

func (r runtimeRenderer) renderScreenShellWindowSummaryLine(state types.AppState, tab types.TabState, paneID types.PaneID, pane types.PaneState, overlayActive bool) string {
	prefix := "  "
	if paneID == tab.ActivePaneID {
		prefix = "> "
	}
	parts := []string{fmt.Sprintf("%s[%s] %s", prefix, paneID, renderPaneTitle(state, pane))}
	if pane.TerminalID != "" && pane.SlotState == types.PaneSlotConnected {
		parts = append(parts, renderScreenShellPaneCardRole(state, pane))
	} else {
		parts = append(parts, string(pane.SlotState))
	}
	if pane.Rect.W > 0 || pane.Rect.H > 0 {
		parts = append(parts, fmt.Sprintf("%d,%d %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
	}
	if pane.TerminalID != "" && pane.SlotState == types.PaneSlotConnected {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			parts = append(parts, string(terminal.State))
		}
	}
	if preview := r.renderScreenShellWindowPreview(state, pane, overlayActive); preview != "" {
		parts = append(parts, preview)
	}
	return compactSummaryLine(parts...)
}

func (r runtimeRenderer) renderScreenShellWindowPreview(state types.AppState, pane types.PaneState, overlayActive bool) string {
	if overlayActive {
		return fmt.Sprintf("overlay active: %s", state.UI.Overlay.Kind)
	}
	if pane.TerminalID != "" && pane.SlotState == types.PaneSlotConnected {
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			return preview
		}
		return "<screen unavailable>"
	}
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return "waiting for connect"
	case types.PaneSlotExited:
		return "process exited"
	case types.PaneSlotEmpty:
		return "no terminal connected"
	default:
		return ""
	}
}

func activeRowLabel(row terminalmanagerdomain.Row, ok bool) string {
	if !ok {
		return "none"
	}
	switch row.Kind {
	case terminalmanagerdomain.RowKindTerminal:
		return row.Label
	case terminalmanagerdomain.RowKindCreate:
		return "create"
	case terminalmanagerdomain.RowKindHeader:
		return string(row.Section)
	default:
		return row.Label
	}
}

func renderScreenShellDialogFooter(kind types.OverlayKind) (string, string) {
	switch kind {
	case types.OverlayTerminalManager:
		return "FOOTER[enter here esc close]", "ACTIONS[enter here esc close]"
	case types.OverlayWorkspacePicker, types.OverlayTerminalPicker, types.OverlayLayoutResolve:
		return "FOOTER[enter confirm esc close]", "ACTIONS[enter confirm esc close]"
	case types.OverlayPrompt:
		return "FOOTER[enter submit esc close]", "ACTIONS[enter submit esc close]"
	case types.OverlayHelp:
		return "FOOTER[esc close]", "ACTIONS[esc close]"
	default:
		return "", ""
	}
}

func renderScreenShellPaneTitle(state types.AppState, pane types.PaneState, includeKind bool) string {
	title := renderPaneTitle(state, pane)
	paneRole := string(pane.SlotState)
	if pane.TerminalID != "" && pane.SlotState == types.PaneSlotConnected {
		paneRole = renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
		if paneRole == "" {
			paneRole = "connected"
		}
	}
	if includeKind {
		return fmt.Sprintf("%s [%s] [%s]", title, paneRole, safePaneKind(pane.Kind))
	}
	return fmt.Sprintf("%s [%s]", title, paneRole)
}

func (r runtimeRenderer) renderScreenShellPaneLines(state types.AppState, pane types.PaneState, overlayActive bool, maxRows int) []string {
	if overlayActive {
		return []string{fmt.Sprintf("overlay active: %s", state.UI.Overlay.Kind)}
	}
	if maxRows <= 0 {
		maxRows = 1
	}
	if pane.TerminalID != "" && pane.SlotState == types.PaneSlotConnected {
		terminalState := types.TerminalRunStateRunning
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			terminalState = terminal.State
		}
		footer := fmt.Sprintf("%s %s %s", pane.TerminalID, terminalState, renderScreenShellPaneCardRole(state, pane))
		contentBudget := maxRows - 1
		if contentBudget <= 0 {
			return []string{footer}
		}
		lines := make([]string, 0, maxRows)
		if r.Screens != nil {
			if snapshot, ok := r.Screens.Snapshot(pane.TerminalID); ok && snapshot != nil {
				rows, _, _ := renderSnapshotRows(snapshot)
				trimmed := make([]string, 0, contentBudget)
				for _, row := range rows {
					row = strings.TrimRight(row, " ")
					trimmed = append(trimmed, row)
				}
				if len(trimmed) > 0 {
					if len(trimmed) > contentBudget {
						trimmed = trimmed[len(trimmed)-contentBudget:]
					}
					lines = append(lines, trimmed...)
					for len(lines) < contentBudget {
						lines = append(lines, "")
					}
					lines = append(lines, footer)
					return lines
				}
			}
		}
		lines = append(lines, "<screen unavailable>")
		for len(lines) < contentBudget {
			lines = append(lines, "")
		}
		lines = append(lines, footer)
		return lines
	}
	contentBudget := maxRows - 1
	if contentBudget <= 0 {
		contentBudget = 1
	}
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		lines := []string{"waiting for connect"}
		for len(lines) < contentBudget {
			lines = append(lines, "")
		}
		lines = append(lines, "n new | a connect")
		return lines
	case types.PaneSlotExited:
		lines := []string{
			"process exited",
			"history retained",
		}
		if len(lines) > contentBudget {
			lines = lines[:contentBudget]
		}
		for len(lines) < contentBudget {
			lines = append(lines, "")
		}
		lines = append(lines, "r restart | a connect")
		return lines
	case types.PaneSlotEmpty:
		lines := []string{"no terminal connected"}
		for len(lines) < contentBudget {
			lines = append(lines, "")
		}
		lines = append(lines, "n new | a connect | m manager")
		return lines
	default:
		return []string{"<empty>"}
	}
}

func renderShellBox(width int, title string, body []string) []string {
	return renderShellBoxWithStyle(width, title, body, defaultShellBorderStyle)
}

// renderShellBoxWithStyle 统一收敛 screen shell 下的盒模型边框风格，
// 让 active pane / modal dialog 可以共享同一套稳定的字符边框规则。
func renderShellBoxWithStyle(width int, title string, body []string, style shellBorderStyle) []string {
	if width < 8 {
		width = 8
	}
	innerWidth := width - 2
	title = truncateLine(title, innerWidth)
	top := string(style.Corner) + " " + title
	if len(top) < width-1 {
		top += strings.Repeat(string(style.Horizontal), width-1-len(top))
	}
	top = truncateLine(top, width-1)
	lines := []string{top + string(style.Corner)}
	for _, line := range body {
		lines = append(lines, string(style.Vertical)+padRight(truncateLine(line, innerWidth), innerWidth)+string(style.Vertical))
	}
	lines = append(lines, string(style.Corner)+strings.Repeat(string(style.Horizontal), innerWidth)+string(style.Corner))
	return lines
}

func renderScreenShellPaneBox(width int, title string, body []string, active bool) []string {
	if width < 8 {
		width = 8
	}
	innerWidth := width - 2
	style := defaultShellBorderStyle
	if active {
		style = emphasisShellBorderStyle
	}
	lines := []string{string(style.Corner) + strings.Repeat(string(style.Horizontal), innerWidth) + string(style.Corner)}
	lines = append(lines, string(style.Vertical)+padRight(truncateLine(renderScreenShellPaneTitleLine(title, active), innerWidth), innerWidth)+string(style.Vertical))
	// overlay 或极短正文时不再额外插入分隔线，避免 screen shell 在对话框场景里继续膨胀高度。
	if len(body) > 1 {
		lines = append(lines, string(style.Vertical)+strings.Repeat(string(style.Horizontal), innerWidth)+string(style.Vertical))
	}
	for _, line := range body {
		lines = append(lines, string(style.Vertical)+padRight(truncateLine(line, innerWidth), innerWidth)+string(style.Vertical))
	}
	lines = append(lines, string(style.Corner)+strings.Repeat(string(style.Horizontal), innerWidth)+string(style.Corner))
	return lines
}

// screen shell 现在开始从“顺排 box 列表”进入真正的文本 canvas，
// 所以这里提供一个最小 compositor，把 pane box 按矩形位置拼进同一块工作台区域。
type screenShellCanvas struct {
	width  int
	height int
	rows   [][]rune
}

func newScreenShellCanvas(width int, height int) *screenShellCanvas {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	rows := make([][]rune, height)
	for y := range rows {
		rows[y] = []rune(strings.Repeat(" ", width))
	}
	return &screenShellCanvas{
		width:  width,
		height: height,
		rows:   rows,
	}
}

func (c *screenShellCanvas) stampLines(x int, y int, lines []string) {
	if c == nil {
		return
	}
	for rowIndex, line := range lines {
		targetY := y + rowIndex
		if targetY < 0 || targetY >= c.height {
			continue
		}
		runes := []rune(line)
		for columnIndex := 0; columnIndex < len(runes); columnIndex++ {
			targetX := x + columnIndex
			if targetX < 0 || targetX >= c.width {
				continue
			}
			c.rows[targetY][targetX] = runes[columnIndex]
		}
	}
}

func (c *screenShellCanvas) clearRect(x int, y int, width int, height int) {
	if c == nil {
		return
	}
	for row := 0; row < height; row++ {
		targetY := y + row
		if targetY < 0 || targetY >= c.height {
			continue
		}
		for col := 0; col < width; col++ {
			targetX := x + col
			if targetX < 0 || targetX >= c.width {
				continue
			}
			c.rows[targetY][targetX] = ' '
		}
	}
}

func (c *screenShellCanvas) lines() []string {
	if c == nil {
		return nil
	}
	lines := make([]string, 0, c.height)
	for _, row := range c.rows {
		lines = append(lines, string(row))
	}
	return lines
}

func renderScreenShellWorkbenchCanvasHeight(metrics wireframeMetrics, overlayActive bool) int {
	if overlayActive {
		return 8
	}
	return 12
}

func renderScreenShellPaneCanvasBox(width int, height int, title string, body []string, active bool) []string {
	if width < 8 {
		width = 8
	}
	if height < 4 {
		height = 4
	}
	innerWidth := width - 2
	useDivider := height >= 7
	bodyRows := height - 3
	if useDivider {
		bodyRows = height - 4
	}
	if bodyRows < 1 {
		bodyRows = 1
	}
	style := defaultShellBorderStyle
	if active {
		style = emphasisShellBorderStyle
	}
	lines := []string{string(style.Corner) + strings.Repeat(string(style.Horizontal), innerWidth) + string(style.Corner)}
	lines = append(lines, string(style.Vertical)+padRight(truncateLine(renderScreenShellPaneTitleLine(title, active), innerWidth), innerWidth)+string(style.Vertical))
	if useDivider {
		lines = append(lines, string(style.Vertical)+strings.Repeat(string(style.Horizontal), innerWidth)+string(style.Vertical))
	}
	for _, line := range clampPaddedLines(body, bodyRows) {
		lines = append(lines, string(style.Vertical)+padRight(truncateLine(line, innerWidth), innerWidth)+string(style.Vertical))
	}
	lines = append(lines, string(style.Corner)+strings.Repeat(string(style.Horizontal), innerWidth)+string(style.Corner))
	return lines
}

func clampPaddedLines(lines []string, target int) []string {
	if target <= 0 {
		return nil
	}
	if len(lines) > target {
		lines = lines[:target]
	}
	out := make([]string, 0, target)
	out = append(out, lines...)
	for len(out) < target {
		out = append(out, "")
	}
	return out
}

func (r runtimeRenderer) renderScreenShellTiledCanvas(state types.AppState, tab types.TabState, paneIDs []types.PaneID, metrics wireframeMetrics, overlayActive bool) []string {
	canvasHeight := renderScreenShellWorkbenchCanvasHeight(metrics, overlayActive)
	lines := []string{fmt.Sprintf("TILED CANVAS[%dx%d panes=%d]", metrics.ViewportWidth, canvasHeight, len(paneIDs))}
	canvas := newScreenShellCanvas(metrics.ViewportWidth, canvasHeight)
	rects := renderScreenShellTiledRects(tab, metrics.ViewportWidth, canvasHeight, paneIDs)
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		rect, ok := rects[paneID]
		if !ok {
			continue
		}
		box := renderScreenShellPaneCanvasBox(
			rect.W,
			rect.H,
			renderScreenShellPaneTitle(state, pane, true),
			r.renderScreenShellPaneLines(state, pane, overlayActive, renderScreenShellPaneCanvasBodyRows(rect.H)),
			paneID == tab.ActivePaneID,
		)
		canvas.stampLines(rect.X, rect.Y, box)
	}
	return append(lines, canvas.lines()...)
}

func renderScreenShellPaneCanvasBodyRows(height int) int {
	if height <= 4 {
		return 1
	}
	if height >= 7 {
		return height - 4
	}
	return height - 3
}

func renderScreenShellTiledRects(tab types.TabState, width int, height int, paneIDs []types.PaneID) map[types.PaneID]types.Rect {
	rects := map[types.PaneID]types.Rect{}
	root := splitNodeToLayoutNode(tab.RootSplit)
	if root != nil {
		rects = root.Rects(types.Rect{W: width, H: height})
	}
	if len(rects) >= len(paneIDs) {
		return rects
	}
	// fallback：即使 split tree 暂时不完整，也保证 screen shell 至少能给出稳定几何块。
	switch len(paneIDs) {
	case 0:
		return rects
	case 1:
		rects[paneIDs[0]] = types.Rect{W: width, H: height}
	default:
		columnWidth := width / 2
		if columnWidth < 8 {
			columnWidth = 8
		}
		leftWidth := columnWidth
		rightWidth := width - leftWidth
		rects[paneIDs[0]] = types.Rect{X: 0, Y: 0, W: leftWidth, H: height}
		rects[paneIDs[1]] = types.Rect{X: leftWidth, Y: 0, W: rightWidth, H: height}
		for index, paneID := range paneIDs[2:] {
			rects[paneID] = types.Rect{X: 0, Y: index, W: width, H: minInt(height-index, 4)}
		}
	}
	return rects
}

func (r runtimeRenderer) renderScreenShellFloatingCanvas(state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, overlayActive bool) []string {
	canvasHeight := renderScreenShellWorkbenchCanvasHeight(metrics, overlayActive)
	lines := []string{fmt.Sprintf("FLOAT CANVAS[%dx%d windows=%d]", metrics.ViewportWidth, canvasHeight, len(floatingPaneIDs))}
	canvas := newScreenShellCanvas(metrics.ViewportWidth, canvasHeight)
	if pane.Kind == types.PaneKindTiled {
		baseRect := types.Rect{W: metrics.ViewportWidth, H: canvasHeight}
		baseBox := renderScreenShellPaneCanvasBox(
			baseRect.W,
			baseRect.H,
			renderScreenShellPaneTitle(state, pane, true),
			r.renderScreenShellPaneLines(state, pane, overlayActive, renderScreenShellPaneCanvasBodyRows(baseRect.H)),
			pane.ID == tab.ActivePaneID,
		)
		canvas.stampLines(0, 0, baseBox)
	}
	for _, paneID := range floatingPaneIDs {
		floatingPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		rect := normalizeFloatingCanvasRect(floatingPane.Rect, metrics.ViewportWidth, metrics.ViewportHeight, canvasHeight)
		box := renderScreenShellPaneCanvasBox(
			rect.W,
			rect.H,
			renderScreenShellPaneTitle(state, floatingPane, true),
			r.renderScreenShellPaneLines(state, floatingPane, overlayActive, renderScreenShellPaneCanvasBodyRows(rect.H)),
			paneID == tab.ActivePaneID,
		)
		canvas.stampLines(rect.X, rect.Y, box)
	}
	return append(lines, canvas.lines()...)
}

func normalizeFloatingCanvasRect(rect types.Rect, viewportWidth int, viewportHeight int, canvasHeight int) types.Rect {
	if viewportWidth <= 0 {
		viewportWidth = runtimeWireframeWidth
	}
	if viewportHeight <= 0 {
		viewportHeight = 24
	}
	x := clampInt(scaleCoord(rect.X, viewportWidth, viewportWidth), 0, maxInt(viewportWidth-8, 0))
	w := scaleSize(rect.W, viewportWidth, viewportWidth)
	if w < 8 {
		w = 8
	}
	if x+w > viewportWidth {
		w = viewportWidth - x
	}
	if w < 8 {
		w = minInt(viewportWidth, 8)
		x = maxInt(0, viewportWidth-w)
	}
	y := clampInt(scaleCoord(rect.Y, viewportHeight, canvasHeight), 0, maxInt(canvasHeight-4, 0))
	h := scaleSize(rect.H, viewportHeight, canvasHeight)
	if h < 4 {
		h = 4
	}
	if y+h > canvasHeight {
		h = canvasHeight - y
	}
	if h < 4 {
		h = minInt(canvasHeight, 4)
		y = maxInt(0, canvasHeight-h)
	}
	return types.Rect{X: x, Y: y, W: w, H: h}
}

func scaleCoord(value int, source int, target int) int {
	if source <= 0 || target <= 0 || value <= 0 {
		return 0
	}
	return (value * target) / source
}

func scaleSize(value int, source int, target int) int {
	if source <= 0 || target <= 0 || value <= 0 {
		return 0
	}
	return (value*target + source - 1) / source
}

func clampInt(value int, low int, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func splitNodeToLayoutNode(node *types.SplitNode) *layoutdomain.Node {
	if node == nil {
		return nil
	}
	return &layoutdomain.Node{
		PaneID:    node.PaneID,
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     splitNodeToLayoutNode(node.First),
		Second:    splitNodeToLayoutNode(node.Second),
	}
}

// renderWireframeView 在语义 renderer 之上补一层稳定的 ASCII 工作台，
// 现在它降级成调试摘要层，只保留 topology / overlay / preview 的关键语义，
// 不再重复把整个 workbench 再画成一整片 box。
func (r runtimeRenderer) renderWireframeView(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) []string {
	lines := []string{"wireframe_view:"}
	layer := tab.ActiveLayer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	focus := state.UI.Focus.Layer
	if focus == "" {
		focus = types.FocusLayerTiled
	}
	metrics := r.wireframeMetrics(pane)
	lines = append(lines, compactSummaryLine(
		fmt.Sprintf("DEBUG[%s/%s]", safeWorkspaceLabel(workspace), safeTabLabel(tab)),
		fmt.Sprintf("viewport=%dx%d", metrics.ViewportWidth, metrics.ViewportHeight),
		fmt.Sprintf("layer=%s", layer),
		fmt.Sprintf("focus=%s", focus),
		fmt.Sprintf("overlay=%s", state.UI.Overlay.Kind),
	))
	lines = append(lines, r.renderWireframeWorkbench(state, tab, pane, metrics)...)
	if overlayLines := r.renderWireframeOverlayDialog(state, metrics); len(overlayLines) > 0 {
		lines = append(lines, overlayLines...)
	}
	return lines
}

func (r runtimeRenderer) renderWireframeWorkbench(state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics) []string {
	tiledPaneIDs := orderedTiledPaneIDs(tab)
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	switch {
	case len(tiledPaneIDs) > 1:
		return r.renderWireframeSplitWorkbench(state, tab, tiledPaneIDs, floatingPaneIDs, metrics)
	case len(floatingPaneIDs) > 0:
		return r.renderWireframeFloatingWorkbench(state, tab, pane, floatingPaneIDs, metrics)
	default:
		return r.renderWireframeSingleWorkbench(state, pane)
	}
}

func (r runtimeRenderer) renderWireframeSingleWorkbench(state types.AppState, pane types.PaneState) []string {
	lines := []string{
		compactSummaryLine("WORKBENCH[single]", renderWireframePaneStateSummary(state, pane, true)),
	}
	if pane.TerminalID != "" {
		terminalState := "unknown"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			terminalState = string(terminal.State)
		}
		lines = append(lines, fmt.Sprintf("TERM[%s] STATE[%s]", pane.TerminalID, terminalState))
	}
	if preview := r.renderWireframeDebugPreview(state, pane); preview != "" {
		lines = append(lines, preview)
	}
	return lines
}

func (r runtimeRenderer) renderWireframeSplitWorkbench(state types.AppState, tab types.TabState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, metrics wireframeMetrics) []string {
	summary := summarizeTiledLayout(tab.RootSplit, len(tiledPaneIDs))
	lines := []string{
		compactSummaryLine("WORKBENCH[split]", renderWireframeSplitSummary(summary)),
	}
	for _, paneID := range tiledPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		cardKind := "PANE"
		if paneID == tab.ActivePaneID {
			cardKind = "ACTIVE"
		}
		lines = append(lines, renderWireframePaneStateSummary(state, pane, cardKind == "ACTIVE"))
	}
	if len(floatingPaneIDs) > 0 {
		lines = append(lines, compactSummaryLine(fmt.Sprintf("FLOATING[%d]", len(floatingPaneIDs))))
		for _, line := range r.renderWireframeFloatingStackSummary(state, tab, floatingPaneIDs) {
			if line == "FLOATING STACK" {
				continue
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func (r runtimeRenderer) renderWireframeLayoutTree(state types.AppState, tab types.TabState, metrics wireframeMetrics) []string {
	if tab.RootSplit == nil {
		return nil
	}
	lines := []string{"LAYOUT TREE"}
	lines = append(lines, r.renderWireframeLayoutTreeNode(state, tab, tab.RootSplit, metrics.ViewportWidth-2, 0)...)
	return lines
}

func (r runtimeRenderer) renderWireframeLayoutTreeNode(state types.AppState, tab types.TabState, node *types.SplitNode, width int, depth int) []string {
	if node == nil {
		return nil
	}
	if node.First == nil && node.Second == nil {
		pane, ok := tab.Panes[node.PaneID]
		if !ok {
			return []string{strings.Repeat("  ", depth) + "pane[missing]"}
		}
		return []string{renderWireframeLayoutTreePaneLine(state, tab, pane, depth)}
	}

	lines := []string{renderWireframeLayoutTreeSplitLine(node, width, depth)}
	firstWidth, secondWidth := splitNodeWireframeWidths(node, width)
	childDepth := depth
	if node.Direction == types.SplitDirectionVertical {
		childDepth = depth + 1
	}
	lines = append(lines, r.renderWireframeLayoutTreeNode(state, tab, node.First, firstWidth, childDepth)...)
	lines = append(lines, r.renderWireframeLayoutTreeNode(state, tab, node.Second, secondWidth, childDepth)...)
	return lines
}

func renderWireframeLayoutTreeSplitLine(node *types.SplitNode, width int, depth int) string {
	displayWidth := width
	if node.Direction == types.SplitDirectionHorizontal {
		displayWidth, _ = splitNodeWireframeWidths(node, width)
	}
	return fmt.Sprintf("%ssplit[%s] ratio[%.2f] width[%d]", strings.Repeat("  ", depth), node.Direction, node.Ratio, displayWidth)
}

func renderWireframeLayoutTreePaneLine(state types.AppState, tab types.TabState, pane types.PaneState, depth int) string {
	role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
	if role == "" {
		role = string(pane.SlotState)
	}
	terminalState := string(types.TerminalRunStateRunning)
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
		terminalState = string(terminal.State)
	}
	prefix := strings.Repeat("  ", depth)
	if pane.ID == tab.ActivePaneID {
		return fmt.Sprintf("%s> pane[%s] role[%s] state[%s]", prefix, renderPaneTitle(state, pane), role, terminalState)
	}
	return fmt.Sprintf("%spane[%s] role[%s] state[%s]", prefix, renderPaneTitle(state, pane), role, terminalState)
}

func splitNodeWireframeWidths(node *types.SplitNode, width int) (int, int) {
	if width <= 1 {
		return width, 0
	}
	ratio := node.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	first := int(float64(width)*ratio + 0.5)
	if first <= 0 {
		first = 1
	}
	if first >= width {
		first = width - 1
	}
	return first, width - first
}

func renderWireframeSplitSummary(summary tiledLayoutSummary) string {
	parts := []string{
		fmt.Sprintf("SPLIT[%s]", summary.Root),
		fmt.Sprintf("LEAVES[%d]", summary.Leaves),
	}
	if summary.Root == "" {
		parts[0] = "SPLIT[implicit]"
	}
	if summary.HasRatio {
		parts = append([]string{fmt.Sprintf("SPLIT[%s]", summary.Root), fmt.Sprintf("RATIO[%.2f]", summary.Ratio)}, parts[1:]...)
	}
	if !summary.HasRatio {
		parts = append([]string{fmt.Sprintf("SPLIT[%s]", summary.Root), "RATIO[n/a]"}, parts[1:]...)
	}
	return strings.Join(parts, " ")
}

func renderWireframeSplitRatioBar(summary tiledLayoutSummary) string {
	totalSegments := 30
	if !summary.HasRatio {
		return "BAR[n/a]"
	}
	ratio := summary.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	left := int(float64(totalSegments)*ratio + 0.5)
	if left <= 0 {
		left = 1
	}
	if left >= totalSegments {
		left = totalSegments - 1
	}
	right := totalSegments - left
	return fmt.Sprintf("BAR[%s|%s]", strings.Repeat("=", left), strings.Repeat("=", right))
}

func (r runtimeRenderer) renderWireframeFloatingWorkbench(state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, metrics wireframeMetrics) []string {
	lines := []string{
		compactSummaryLine("WORKBENCH[floating]", fmt.Sprintf("FLOATING[%d]", len(floatingPaneIDs)), fmt.Sprintf("FOCUS[%s]", tab.ActivePaneID)),
		renderWireframePaneStateSummary(state, pane, true),
	}
	for _, line := range r.renderWireframeFloatingStackSummary(state, tab, floatingPaneIDs) {
		if line == "FLOATING STACK" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func (r runtimeRenderer) renderWireframeDebugPreview(state types.AppState, pane types.PaneState) string {
	previewLines := r.renderWireframePanePreviewLines(state, pane)
	if len(previewLines) == 0 {
		return ""
	}
	preview := previewLines[0]
	preview = strings.TrimPrefix(preview, "PREVIEW ")
	return fmt.Sprintf("PREVIEW[%s]", preview)
}

// renderWireframePaneCard 用统一卡片表达 pane 的控制关系、terminal 状态和最近屏幕内容，
// 这样 single/split/floating 三种工作台都能复用同一套最小可读块。
func (r runtimeRenderer) renderWireframePaneCard(state types.AppState, pane types.PaneState, active bool) []string {
	cardKind := "PANE"
	if active {
		cardKind = "ACTIVE"
	}
	role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
	if role == "" {
		role = string(pane.SlotState)
	}
	lines := []string{
		fmt.Sprintf("%s[%s] ROLE[%s] KIND[%s] SLOT[%s]", cardKind, renderPaneTitle(state, pane), role, safePaneKind(pane.Kind), pane.SlotState),
	}
	if pane.TerminalID != "" {
		terminalState := "unknown"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			terminalState = string(terminal.State)
		}
		lines = append(lines,
			fmt.Sprintf("%s[%s] ROLE[%s] STATE[%s]", cardKind, renderPaneTitle(state, pane), role, terminalState),
			fmt.Sprintf("TERM[%s] STATE[%s]", pane.TerminalID, terminalState),
		)
	} else {
		lines = append(lines, fmt.Sprintf("%s[%s] SLOT[%s]", cardKind, renderPaneTitle(state, pane), pane.SlotState))
	}
	if pane.Kind == types.PaneKindFloating && (pane.Rect.W > 0 || pane.Rect.H > 0) {
		lines = append(lines, fmt.Sprintf("RECT[%d,%d %dx%d]", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
	}
	lines = append(lines, r.renderWireframePanePreviewLines(state, pane)...)
	return lines
}

func (r runtimeRenderer) renderWireframePanePreviewLines(state types.AppState, pane types.PaneState) []string {
	if state.UI.Overlay.Kind != types.OverlayNone {
		return []string{"PREVIEW suppressed by overlay"}
	}
	if pane.TerminalID == "" || r.Screens == nil {
		return renderWireframePaneSlotPreview(state, pane)
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return renderWireframePaneSlotPreview(state, pane)
	}
	rows, _, _ := renderSnapshotRows(snapshot)
	if len(rows) > runtimeWireframePreviewRows {
		rows = rows[len(rows)-runtimeWireframePreviewRows:]
	}
	if len(rows) == 0 {
		return []string{"PREVIEW[empty]"}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("PREVIEW %s", row))
	}
	return lines
}

func renderWireframePaneStateSummary(state types.AppState, pane types.PaneState, active bool) string {
	cardKind := "PANE"
	if active {
		cardKind = "ACTIVE"
	}
	role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
	if role == "" {
		role = string(pane.SlotState)
	}
	if pane.TerminalID != "" {
		terminalState := "unknown"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			terminalState = string(terminal.State)
		}
		return fmt.Sprintf("%s[%s] ROLE[%s] STATE[%s]", cardKind, renderPaneTitle(state, pane), role, terminalState)
	}
	return fmt.Sprintf("%s[%s] SLOT[%s]", cardKind, renderPaneTitle(state, pane), pane.SlotState)
}

func renderWireframePaneSlotPreview(state types.AppState, pane types.PaneState) []string {
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return []string{"PREVIEW waiting for connect", "ACTION[n] new | ACTION[a] connect"}
	case types.PaneSlotExited:
		exitCode := ""
		if pane.LastExitCode != nil {
			exitCode = fmt.Sprintf(" EXIT[%d]", *pane.LastExitCode)
		} else if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.ExitCode != nil {
			exitCode = fmt.Sprintf(" EXIT[%d]", *terminal.ExitCode)
		}
		return []string{fmt.Sprintf("PREVIEW exited history retained%s", exitCode), "ACTION[r] restart | ACTION[a] connect"}
	case types.PaneSlotEmpty:
		return []string{"PREVIEW unconnected pane", "ACTION[n] new | ACTION[m] manager"}
	default:
		return []string{"PREVIEW unavailable"}
	}
}

func (r runtimeRenderer) renderWireframeFloatingStackBody(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) []string {
	lines := []string{"FLOATING STACK"}
	for _, paneID := range floatingPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
		if role == "" {
			role = string(pane.SlotState)
		}
		lines = append(lines, fmt.Sprintf("FLOAT[%s] %s %s %d,%d %dx%d", paneID, renderPaneTitle(state, pane), role, pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			lines = append(lines, fmt.Sprintf("PREVIEW %s", preview))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "FLOAT[none]")
	}
	return lines
}

func (r runtimeRenderer) renderWireframeFloatingStackSummary(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) []string {
	if len(floatingPaneIDs) == 0 {
		return nil
	}
	lines := []string{"FLOATING STACK"}
	for _, paneID := range floatingPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
		if role == "" {
			role = string(pane.SlotState)
		}
		lines = append(lines, fmt.Sprintf("FLOAT[%s] %s %s %d,%d %dx%d", paneID, renderPaneTitle(state, pane), role, pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
	}
	return lines
}

func (r runtimeRenderer) renderWireframeFloatingGeometryMap(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) []string {
	lines := []string{"FLOATING MAP"}
	if len(floatingPaneIDs) == 0 {
		return append(lines, "MAP[empty]")
	}
	rows := map[int][]string{}
	ys := make([]int, 0, len(floatingPaneIDs))
	for _, paneID := range floatingPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		if _, ok := rows[pane.Rect.Y]; !ok {
			ys = append(ys, pane.Rect.Y)
		}
		rows[pane.Rect.Y] = append(rows[pane.Rect.Y], fmt.Sprintf("x%02d w%02d %s(%s)", pane.Rect.X, pane.Rect.W, renderPaneTitle(state, pane), paneID))
	}
	sort.Ints(ys)
	for _, y := range ys {
		sort.Strings(rows[y])
		lines = append(lines, fmt.Sprintf("MAP[y%02d] %s", y, strings.Join(rows[y], " | ")))
	}
	return lines
}

func (r runtimeRenderer) renderWireframeOverlayDialog(state types.AppState, metrics wireframeMetrics) []string {
	if state.UI.Overlay.Kind == types.OverlayNone {
		return nil
	}
	body := []string{
		compactSummaryLine(
			fmt.Sprintf("DEBUG_OVERLAY[%s]", state.UI.Overlay.Kind),
			fmt.Sprintf("focus=%s", state.UI.Focus.Layer),
			fmt.Sprintf("center=%d/%d", (metrics.ViewportWidth-metrics.OverlayWidth)/2, metrics.OverlayWidth),
		),
	}
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		body = append(body, fmt.Sprintf("RETURN[%s]", returnFocus))
	}
	body = append(body, renderWireframeOverlayBody(state.UI.Overlay)...)
	return body
}

func (r runtimeRenderer) wireframeMetrics(pane types.PaneState) wireframeMetrics {
	width, height := r.wireframeViewport(pane)
	sidebarWidth := width / 4
	if sidebarWidth < runtimeWireframeSidebarWidth {
		sidebarWidth = runtimeWireframeSidebarWidth
	}
	if sidebarWidth > 28 {
		sidebarWidth = 28
	}
	mainWidth := width - sidebarWidth - 2
	if mainWidth < runtimeWireframeMainPaneWidth {
		mainWidth = runtimeWireframeMainPaneWidth
		sidebarWidth = width - mainWidth - 2
		if sidebarWidth < runtimeWireframeSidebarWidth {
			sidebarWidth = runtimeWireframeSidebarWidth
		}
	}
	splitColumnWidth := (width - 2) / 2
	if splitColumnWidth < runtimeWireframeSplitColumnWidth {
		splitColumnWidth = runtimeWireframeSplitColumnWidth
	}
	overlayWidth := width - 20
	if overlayWidth < runtimeWireframeOverlayWidth {
		overlayWidth = runtimeWireframeOverlayWidth
	}
	if overlayWidth > width {
		overlayWidth = width
	}
	return wireframeMetrics{
		ViewportWidth:    width,
		ViewportHeight:   height,
		OverlayWidth:     overlayWidth,
		SplitColumnWidth: splitColumnWidth,
		MainPaneWidth:    mainWidth,
		SidebarWidth:     sidebarWidth,
	}
}

// wireframeViewport 优先使用运行时 terminal status 的尺寸，保证工作台摘要跟随真实会话；
// 如果当前 terminal 还没有 status，再回退到 snapshot，最后才退回稳定默认值。
func (r runtimeRenderer) wireframeViewport(pane types.PaneState) (int, int) {
	width := runtimeWireframeWidth
	height := 24
	if pane.TerminalID == "" || r.Screens == nil {
		return width, height
	}
	if status, ok := r.Screens.Status(pane.TerminalID); ok {
		if status.Size.Cols > 0 {
			width = int(status.Size.Cols)
		}
		if status.Size.Rows > 0 {
			height = int(status.Size.Rows)
		}
	}
	if snapshot, ok := r.Screens.Snapshot(pane.TerminalID); ok && snapshot != nil {
		if width == runtimeWireframeWidth && snapshot.Size.Cols > 0 {
			width = int(snapshot.Size.Cols)
		}
		if height == 24 && snapshot.Size.Rows > 0 {
			height = int(snapshot.Size.Rows)
		}
	}
	if width < runtimeWireframeWidth {
		width = runtimeWireframeWidth
	}
	if width > runtimeBarMaxWidth {
		width = runtimeBarMaxWidth
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

func renderWireframeReturnFocus(focus types.FocusState) string {
	layer := focus.Layer
	if layer == "" && focus.WorkspaceID == "" && focus.TabID == "" && focus.PaneID == "" {
		return ""
	}
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	workspaceID := focus.WorkspaceID
	if workspaceID == "" {
		workspaceID = types.WorkspaceID("none")
	}
	tabID := focus.TabID
	if tabID == "" {
		tabID = types.TabID("none")
	}
	paneID := focus.PaneID
	if paneID == "" {
		paneID = types.PaneID("none")
	}
	return fmt.Sprintf("%s:%s/%s/%s", layer, workspaceID, tabID, paneID)
}

func renderWireframeOverlayBody(overlay types.OverlayState) []string {
	switch overlay.Kind {
	case types.OverlayTerminalManager:
		manager, ok := overlay.Data.(*terminalmanagerdomain.State)
		if !ok || manager == nil {
			return []string{"ROWS[0]"}
		}
		rows := manager.VisibleRows()
		selectedID := "none"
		selectedIndex := 0
		if row, ok := manager.SelectedRow(); ok {
			if row.TerminalID != "" {
				selectedID = string(row.TerminalID)
			} else {
				selectedID = row.Label
			}
			for idx, candidate := range rows {
				if candidate.Kind == row.Kind && candidate.TerminalID == row.TerminalID && candidate.Label == row.Label {
					selectedIndex = idx
					break
				}
			}
		}
		lines := []string{fmt.Sprintf("ROWS[%d] SELECTED[%s]", len(rows), selectedID)}
		previewRows, _ := overlayPreviewRowsAround(rows, runtimeOverlayDetailPreviewRows, selectedIndex)
		for _, row := range previewRows {
			prefix := "  "
			if selected, ok := manager.SelectedRow(); ok && selected.Kind == row.Kind && selected.TerminalID == row.TerminalID && selected.Label == row.Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
		}
		if detail, ok := manager.SelectedDetail(); ok {
			lines = append(lines,
				fmt.Sprintf("DETAIL[%s] STATE[%s] VIS[%s]", detail.Name, detail.State, detail.VisibilityLabel),
				fmt.Sprintf("LOCATIONS[%d] OWNER[%s]", len(detail.Locations), detail.OwnerSlotLabel),
			)
		}
		lines = append(lines, "ACTIONS[jump connect_here new_tab floating]")
		return lines
	case types.OverlayWorkspacePicker:
		picker, ok := overlay.Data.(*workspacedomain.PickerState)
		if !ok || picker == nil {
			return []string{"ROWS[0]"}
		}
		rows := picker.VisibleRows()
		selectedIndex := 0
		selectedKey := "none"
		if selected, ok := picker.SelectedRow(); ok {
			selectedKey = selected.Node.Key
			for idx, row := range rows {
				if row.Node.Key == selected.Node.Key {
					selectedIndex = idx
					break
				}
			}
		}
		lines := []string{fmt.Sprintf("ROWS[%d] QUERY[%s] SELECTED[%s]", len(rows), picker.Query(), selectedKey)}
		if selected, ok := picker.SelectedRow(); ok {
			lines = append(lines, fmt.Sprintf("TARGET[%s] LABEL[%s] DEPTH[%d]", selected.Node.Kind, selected.Node.Label, selected.Depth))
		}
		previewRows, _ := overlayPreviewRowsAround(rows, 6, selectedIndex)
		for _, row := range previewRows {
			prefix := "  "
			if selected, ok := picker.SelectedRow(); ok && selected.Node.Key == row.Node.Key {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
		}
		return lines
	case types.OverlayTerminalPicker:
		picker, ok := overlay.Data.(*terminalpickerdomain.State)
		if !ok || picker == nil {
			return []string{"ROWS[0]"}
		}
		rows := picker.VisibleRows()
		lines := []string{fmt.Sprintf("ROWS[%d] QUERY[%s]", len(rows), picker.Query())}
		for _, row := range rows {
			prefix := "  "
			if selected, ok := picker.SelectedRow(); ok && selected.Kind == row.Kind && selected.TerminalID == row.TerminalID && selected.Label == row.Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
			if len(lines) >= runtimeOverlayDetailPreviewRows+1 {
				break
			}
		}
		return lines
	case types.OverlayLayoutResolve:
		resolve, ok := overlay.Data.(*layoutresolvedomain.State)
		if !ok || resolve == nil {
			return []string{"ROWS[0]"}
		}
		rows := resolve.Rows()
		lines := []string{
			fmt.Sprintf("ROWS[%d] PANE[%s] ROLE[%s]", len(rows), resolve.PaneID, resolve.Role),
			fmt.Sprintf("HINT[%s]", resolve.Hint),
		}
		for _, row := range rows {
			prefix := "  "
			if selected, ok := resolve.SelectedRow(); ok && selected.Action == row.Action && selected.Label == row.Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Action, row.Label))
			if len(lines) >= runtimeOverlayDetailPreviewRows+1 {
				break
			}
		}
		return lines
	case types.OverlayPrompt:
		prompt, ok := overlay.Data.(*promptdomain.State)
		if !ok || prompt == nil {
			return []string{"PROMPT[draft]"}
		}
		lines := []string{
			fmt.Sprintf("PROMPT[%s]", prompt.Kind),
			fmt.Sprintf("TITLE[%s]", prompt.Title),
		}
		if len(prompt.Fields) == 0 {
			lines = append(lines, fmt.Sprintf("> [draft] %s", prompt.Draft))
			lines = append(lines, "ACTIONS[submit cancel]")
			return lines
		}
		active := prompt.Active
		if active < 0 || active >= len(prompt.Fields) {
			active = 0
		}
		lines = append(lines, fmt.Sprintf("ACTIVE[%s] VALUE[%s]", prompt.Fields[active].Key, prompt.Fields[active].Value))
		previewFields, _ := overlayPreviewRowsAround(prompt.Fields, runtimeOverlayDetailPreviewRows, active)
		for _, field := range previewFields {
			prefix := "  "
			if field.Key == prompt.Fields[active].Key && field.Label == prompt.Fields[active].Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s: %s", prefix, field.Key, field.Label, field.Value))
		}
		lines = append(lines, "ACTIONS[submit cancel]")
		return lines
	case types.OverlayHelp:
		return []string{
			"HELP[shortcuts]",
			"  Ctrl-p pane | Ctrl-t tab",
			"  Ctrl-w workspace | Ctrl-f picker",
		}
	default:
		return []string{fmt.Sprintf("OVERLAY[%s]", overlay.Kind)}
	}
}

func renderASCIIBox(width int, body []string) []string {
	if width < 4 {
		width = 4
	}
	innerWidth := width - 2
	lines := []string{"+" + strings.Repeat("-", innerWidth) + "+"}
	for _, line := range body {
		lines = append(lines, "|"+padRight(truncateLine(line, innerWidth), innerWidth)+"|")
	}
	lines = append(lines, "+"+strings.Repeat("-", innerWidth)+"+")
	return lines
}

func joinASCIIBoxes(boxes [][]string, gap int) []string {
	if len(boxes) == 0 {
		return nil
	}
	if len(boxes) == 1 {
		return boxes[0]
	}
	height := 0
	widths := make([]int, len(boxes))
	for index, box := range boxes {
		if len(box) > height {
			height = len(box)
		}
		if len(box) > 0 {
			widths[index] = len(box[0])
		}
	}
	spacer := strings.Repeat(" ", gap)
	lines := make([]string, 0, height)
	for row := 0; row < height; row++ {
		var builder strings.Builder
		for index, box := range boxes {
			if index > 0 {
				builder.WriteString(spacer)
			}
			if row < len(box) {
				builder.WriteString(box[row])
				continue
			}
			builder.WriteString(strings.Repeat(" ", widths[index]))
		}
		lines = append(lines, builder.String())
	}
	return lines
}

func renderShellBoxGrid(boxes [][]string, perRow int, gap int) []string {
	if len(boxes) == 0 {
		return nil
	}
	if perRow <= 0 {
		perRow = 1
	}
	lines := make([]string, 0, len(boxes)*4)
	for start := 0; start < len(boxes); start += perRow {
		end := start + perRow
		if end > len(boxes) {
			end = len(boxes)
		}
		lines = append(lines, joinASCIIBoxes(boxes[start:end], gap)...)
	}
	return lines
}

func indentLines(lines []string, padding int) []string {
	if padding <= 0 {
		return lines
	}
	prefix := strings.Repeat(" ", padding)
	indented := make([]string, 0, len(lines))
	for _, line := range lines {
		indented = append(indented, prefix+line)
	}
	return indented
}

func padRight(line string, width int) string {
	if len(line) >= width {
		return line
	}
	return line + strings.Repeat(" ", width-len(line))
}

func renderScreenShellPaneTitleLine(title string, active bool) string {
	if active {
		return "> " + title
	}
	return "  " + title
}

func renderScreenShellCardFocusLabel(active bool) string {
	if active {
		return "active"
	}
	return "background"
}

func renderScreenShellPaneCardRole(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID != "" {
		if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
			return role
		}
	}
	return string(pane.SlotState)
}

func safeWorkspaceLabel(workspace types.WorkspaceState) string {
	if workspace.Name != "" {
		return workspace.Name
	}
	return string(workspace.ID)
}

func safeTabLabel(tab types.TabState) string {
	if tab.Name != "" {
		return tab.Name
	}
	return string(tab.ID)
}

func safePaneKind(kind types.PaneKind) types.PaneKind {
	if kind != "" {
		return kind
	}
	return types.PaneKindTiled
}

// renderHeaderBar 把主工作区、pane、overlay、focus、mode 汇总成顶栏语义，
// 这样主视图即使正文被压缩，顶栏仍然能稳定表达当前上下文。
func renderHeaderBar(workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, ui types.UIState) string {
	parts := []string{
		fmt.Sprintf("header_bar: ws=%s", workspace.Name),
		fmt.Sprintf("tab=%s", tab.Name),
		fmt.Sprintf("pane=%s", pane.ID),
		fmt.Sprintf("slot=%s", pane.SlotState),
		fmt.Sprintf("overlay=%s", ui.Overlay.Kind),
		fmt.Sprintf("focus=%s", ui.Focus.Layer),
	}
	if ui.Mode.Active != types.ModeNone {
		parts = append(parts, fmt.Sprintf("mode=%s", ui.Mode.Active))
	}
	return compactBarLine(parts...)
}

// renderFooterBar 把 notice 聚合状态和当前 overlay 汇总到底栏，
// 底栏优先回答“现在有没有问题、当前是不是处在特殊交互里”。
func renderFooterBar(notices []btui.Notice, overlay types.OverlayKind) string {
	parts := []string{fmt.Sprintf("footer_bar: notices=%d", countVisibleNotices(notices))}
	if last, ok := lastVisibleNotice(notices); ok {
		parts = append(parts, fmt.Sprintf("last=%s", last.Level))
	}
	parts = append(parts, fmt.Sprintf("overlay=%s", overlay))
	return compactBarLine(parts...)
}

func renderWorkspaceBar(workspace types.WorkspaceState) string {
	label := workspace.Name
	if label == "" {
		label = string(workspace.ID)
	}
	return compactSummaryLine(fmt.Sprintf("workspace_bar: [%s]", label))
}

func renderWorkspaceSummary(workspace types.WorkspaceState) string {
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
	return compactSummaryLine(
		fmt.Sprintf("workspace_summary: tabs=%d", tabs),
		fmt.Sprintf("panes=%d", panes),
		fmt.Sprintf("terminals=%d", len(terminals)),
		fmt.Sprintf("floating=%d", floating),
	)
}

func renderTabStrip(workspace types.WorkspaceState) string {
	parts := make([]string, 0, len(workspace.TabOrder))
	for _, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := tab.Name
		if label == "" {
			label = string(tab.ID)
		}
		if tabID == workspace.ActiveTabID {
			parts = append(parts, fmt.Sprintf("[%s]", label))
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "tab_strip: <none>"
	}
	return compactSummaryLine(fmt.Sprintf("tab_strip: %s", strings.Join(parts, " | ")))
}

func renderTabSummary(tab types.TabState) string {
	tiled := 0
	floating := 0
	connected := 0
	waiting := 0
	exited := 0
	empty := 0
	for _, pane := range tab.Panes {
		switch pane.Kind {
		case types.PaneKindFloating:
			floating++
		default:
			tiled++
		}
		switch pane.SlotState {
		case types.PaneSlotConnected:
			connected++
		case types.PaneSlotWaiting:
			waiting++
		case types.PaneSlotExited:
			exited++
		case types.PaneSlotEmpty:
			empty++
		}
	}
	return compactSummaryLine(
		fmt.Sprintf("tab_summary: tiled=%d", tiled),
		fmt.Sprintf("floating=%d", floating),
		fmt.Sprintf("connected=%d", connected),
		fmt.Sprintf("waiting=%d", waiting),
		fmt.Sprintf("exited=%d", exited),
		fmt.Sprintf("empty=%d", empty),
		fmt.Sprintf("active_layer=%s", tab.ActiveLayer),
	)
}

func renderTabPathBar(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) string {
	workspaceLabel := workspace.Name
	if workspaceLabel == "" {
		workspaceLabel = string(workspace.ID)
	}
	tabLabel := tab.Name
	if tabLabel == "" {
		tabLabel = string(tab.ID)
	}
	layer := pane.Kind
	if layer == "" {
		layer = types.PaneKindTiled
	}
	return compactSummaryLine(
		fmt.Sprintf("tab_path_bar: path=%s/%s/%s:%s", workspaceLabel, tabLabel, layer, pane.ID),
		fmt.Sprintf("target=%s", renderPaneTitle(state, pane)),
	)
}

func renderTabLayerBar(tab types.TabState) string {
	tiledRoot := "<none>"
	if paneIDs := orderedTiledPaneIDs(tab); len(paneIDs) > 0 {
		tiledRoot = string(paneIDs[0])
	}
	floatingTop := "<none>"
	if paneIDs := orderedFloatingPaneIDs(tab); len(paneIDs) > 0 {
		floatingTop = string(paneIDs[len(paneIDs)-1])
	}
	return compactSummaryLine(
		fmt.Sprintf("tab_layer_bar: tiled_root=%s", tiledRoot),
		fmt.Sprintf("floating_top=%s", floatingTop),
		fmt.Sprintf("floating_total=%d", len(orderedFloatingPaneIDs(tab))),
	)
}

func renderPaneBar(state types.AppState, pane types.PaneState) string {
	parts := []string{fmt.Sprintf("pane_bar: title=%s", renderPaneTitle(state, pane))}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		parts = append(parts, fmt.Sprintf("role=%s", role))
	} else if pane.TerminalID == "" {
		parts = append(parts, fmt.Sprintf("slot=%s", pane.SlotState))
	}
	parts = append(parts, fmt.Sprintf("kind=%s", pane.Kind))
	return compactSummaryLine(parts...)
}

func renderPaneTitle(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			if terminal.Name != "" {
				return terminal.Name
			}
		}
		return string(pane.TerminalID)
	}
	switch pane.SlotState {
	case types.PaneSlotExited:
		return "exited pane"
	case types.PaneSlotWaiting:
		return "waiting pane"
	default:
		return "unconnected pane"
	}
}

func renderShortcutBar(state types.AppState, pane types.PaneState) string {
	parts := renderShortcutParts(state, pane)
	return compactSummaryLine(fmt.Sprintf("shortcut_bar: %s", strings.Join(parts, " | ")))
}

func renderFocusBar(state types.AppState, pane types.PaneState) string {
	target := renderFocusTarget(state, pane)
	layer := state.UI.Focus.Layer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	parts := []string{
		fmt.Sprintf("focus_bar: target=%s", target),
		fmt.Sprintf("layer=%s", layer),
	}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		parts = append(parts, fmt.Sprintf("role=%s", role))
	}
	return compactSummaryLine(parts...)
}

func renderFocusTarget(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			if terminal.Name != "" {
				return terminal.Name
			}
		}
		return string(pane.TerminalID)
	}
	switch pane.SlotState {
	case types.PaneSlotExited:
		return "exited-pane"
	case types.PaneSlotWaiting:
		return "waiting-pane"
	case types.PaneSlotEmpty:
		return "empty-pane"
	default:
		return string(pane.ID)
	}
}

func renderShortcutParts(state types.AppState, pane types.PaneState) []string {
	switch state.UI.Overlay.Kind {
	case types.OverlayHelp:
		return []string{"Esc close", "? help"}
	case types.OverlayTerminalPicker, types.OverlayWorkspacePicker, types.OverlayLayoutResolve:
		return []string{"Enter confirm", "Esc close", "? help"}
	case types.OverlayTerminalManager:
		return []string{"Enter here", "t new-tab", "o float", "e edit", "k stop", "Esc close", "? help"}
	case types.OverlayPrompt:
		return []string{"Enter submit", "Esc close", "? help"}
	}
	if state.UI.Mode.Active == types.ModeFloating {
		return []string{"h/l focus", "j/k move", "H/J/K/L size", "[/] z", "c center", "x close", "Esc exit", "? help"}
	}
	switch pane.SlotState {
	case types.PaneSlotEmpty, types.PaneSlotWaiting:
		return []string{"n new", "a connect", "m manager", "x close", "? help"}
	case types.PaneSlotExited:
		return []string{"r restart", "a connect", "x close", "? help"}
	default:
		return []string{"Ctrl-p pane", "Ctrl-t tab", "Ctrl-w ws", "Ctrl-o float", "Ctrl-f pick", "Ctrl-g global", "? help"}
	}
}

// renderBodyBar 汇总 body 主体的 terminal/screen/overlay 状态，
// 让用户先看到“主体现在展示的是什么”，再往下看具体 section 细节。
func (r runtimeRenderer) renderBodyBar(state types.AppState, pane types.PaneState, overlayActive bool) string {
	return compactBarLine(
		fmt.Sprintf("body_bar: terminal=%s", r.renderBodyTerminalSummary(state, pane)),
		fmt.Sprintf("screen=%s", r.renderBodyScreenSummary(pane, overlayActive)),
		fmt.Sprintf("overlay=%s", state.UI.Overlay.Kind),
	)
}

func (r runtimeRenderer) renderBodyTerminalSummary(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return "disconnected"
	}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
		return fmt.Sprintf("%s:%s", pane.TerminalID, terminal.State)
	}
	return string(pane.TerminalID)
}

func (r runtimeRenderer) renderBodyScreenSummary(pane types.PaneState, overlayActive bool) string {
	if pane.TerminalID == "" || r.Screens == nil {
		return "unavailable"
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return "unavailable"
	}
	rows, totalRows, _ := renderSnapshotRows(snapshot)
	if overlayActive {
		return "suppressed"
	}
	return fmt.Sprintf("preview:%d/%d", len(rows), totalRows)
}

func renderStatusSection(workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, ui types.UIState) []string {
	lines := []string{
		compactSummaryLine(
			fmt.Sprintf("workspace: %s", workspace.Name),
			fmt.Sprintf("tab: %s", tab.Name),
			fmt.Sprintf("pane: %s", pane.ID),
			fmt.Sprintf("slot: %s", pane.SlotState),
		),
		compactSummaryLine(
			fmt.Sprintf("tab_layer: %s", tab.ActiveLayer),
			fmt.Sprintf("overlay: %s", ui.Overlay.Kind),
			fmt.Sprintf("focus_layer: %s", ui.Focus.Layer),
			fmt.Sprintf("pane_kind: %s", pane.Kind),
		),
	}
	if ui.Focus.OverlayTarget != "" {
		lines = append(lines, fmt.Sprintf("focus_overlay_target: %s", ui.Focus.OverlayTarget))
	}
	if ui.Mode.Active != types.ModeNone {
		lines = append(lines, compactLine(
			fmt.Sprintf("mode: %s", ui.Mode.Active),
			fmt.Sprintf("mode_sticky: %t", ui.Mode.Sticky),
		))
	}
	if stack := renderFloatingStack(tab); stack != "" {
		lines = append(lines, fmt.Sprintf("floating_stack: %s", stack))
	}
	if pane.LastExitCode != nil {
		lines = append(lines, fmt.Sprintf("pane_exit_code: %d", *pane.LastExitCode))
	}
	if pane.Kind == types.PaneKindFloating {
		lines = append(lines, compactLine(
			fmt.Sprintf("pane_rect: x=%d", pane.Rect.X),
			fmt.Sprintf("y=%d", pane.Rect.Y),
			fmt.Sprintf("w=%d", pane.Rect.W),
			fmt.Sprintf("h=%d", pane.Rect.H),
		))
	}
	return lines
}

func renderFloatingStack(tab types.TabState) string {
	parts := make([]string, 0, len(tab.FloatingOrder))
	for _, paneID := range tab.FloatingOrder {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		parts = append(parts, string(paneID))
	}
	return strings.Join(parts, " > ")
}

func renderTiledOutlineBar(tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	return []string{compactSummaryLine(
		fmt.Sprintf("tiled_outline_bar: active=%s", tab.ActivePaneID),
		fmt.Sprintf("total=%d", len(paneIDs)),
	)}
}

func renderTiledLayout(tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	summary := summarizeTiledLayout(tab.RootSplit, len(paneIDs))
	parts := []string{
		fmt.Sprintf("tiled_layout: root=%s", summary.Root),
		fmt.Sprintf("depth=%d", summary.Depth),
		fmt.Sprintf("leaves=%d", summary.Leaves),
	}
	if summary.HasRatio {
		parts = append(parts, fmt.Sprintf("ratio=%.2f", summary.Ratio))
	}
	return []string{compactSummaryLine(parts...)}
}

func renderFloatingOutlineBar(tab types.TabState) []string {
	paneIDs := orderedFloatingPaneIDs(tab)
	if len(paneIDs) == 0 {
		return nil
	}
	return []string{compactSummaryLine(
		fmt.Sprintf("floating_outline_bar: active=%s", tab.ActivePaneID),
		fmt.Sprintf("total=%d", len(paneIDs)),
		fmt.Sprintf("top=%s", paneIDs[len(paneIDs)-1]),
	)}
}

type tiledLayoutSummary struct {
	Root     string
	Depth    int
	Leaves   int
	Ratio    float64
	HasRatio bool
}

func (r runtimeRenderer) renderTiledTree(state types.AppState, tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	lines := []string{"tiled_tree:"}
	if tab.RootSplit == nil {
		for i, paneID := range paneIDs {
			prefix := "|- "
			if i == len(paneIDs)-1 {
				prefix = "\\- "
			}
			lines = append(lines, prefix+r.renderTiledTreePaneLine(state, tab, paneID))
		}
		return lines
	}
	return append(lines, r.renderTiledTreeNode(state, tab, tab.RootSplit, "", true)...)
}

// renderTiledOutline 把当前 tab 下的 tiled pane 顺序稳定地投影成概览，
// 并补上每个 pane 的最小运行摘要，让 split 工作台在文本视图里也能读出结构和状态。
func (r runtimeRenderer) renderTiledOutline(state types.AppState, tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	lines := []string{"tiled_outline:"}
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindTiled {
			continue
		}
		prefix := "  "
		if paneID == tab.ActivePaneID {
			prefix = "> "
		}
		parts := []string{fmt.Sprintf("%s[tiled] %s", prefix, renderPaneTitle(state, pane))}
		parts = append(parts, renderPaneOverviewStateParts(state, pane)...)
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
		}
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			parts = append(parts, fmt.Sprintf("preview=%s", preview))
		}
		lines = append(lines, compactSummaryLine(parts...))
	}
	return lines
}

func (r runtimeRenderer) renderFloatingOutline(state types.AppState, tab types.TabState) []string {
	paneIDs := orderedFloatingPaneIDs(tab)
	if len(paneIDs) == 0 {
		return nil
	}
	lines := []string{"floating_outline:"}
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		prefix := "  "
		if paneID == tab.ActivePaneID {
			prefix = "> "
		}
		parts := []string{fmt.Sprintf("%s[floating] %s", prefix, renderPaneTitle(state, pane))}
		parts = append(parts, renderPaneOverviewStateParts(state, pane)...)
		if pane.Rect.W > 0 || pane.Rect.H > 0 {
			parts = append(parts, fmt.Sprintf("rect=%d,%d %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
		}
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
		}
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			parts = append(parts, fmt.Sprintf("preview=%s", preview))
		}
		lines = append(lines, compactSummaryLine(parts...))
	}
	return lines
}

// renderTiledTreeNode 递归投影 split 树，优先把结构关系表达清楚，
// 这样文本视图也能读出“哪个 pane 在哪一支、当前 active 在哪里”。
func (r runtimeRenderer) renderTiledTreeNode(state types.AppState, tab types.TabState, node *types.SplitNode, prefix string, isLast bool) []string {
	if node == nil {
		return nil
	}
	branch := "|- "
	nextPrefix := prefix + "|  "
	if isLast {
		branch = "\\- "
		nextPrefix = prefix + "   "
	}
	if node.First == nil && node.Second == nil {
		return []string{prefix + branch + r.renderTiledTreePaneLine(state, tab, node.PaneID)}
	}

	line := fmt.Sprintf("split %s", node.Direction)
	if node.Ratio > 0 {
		line = fmt.Sprintf("%s ratio=%.2f", line, node.Ratio)
	}
	lines := []string{prefix + branch + line}
	children := []*types.SplitNode{node.First, node.Second}
	filtered := make([]*types.SplitNode, 0, len(children))
	for _, child := range children {
		if child != nil {
			filtered = append(filtered, child)
		}
	}
	for i, child := range filtered {
		lines = append(lines, r.renderTiledTreeNode(state, tab, child, nextPrefix, i == len(filtered)-1)...)
	}
	return lines
}

func (r runtimeRenderer) renderTiledTreePaneLine(state types.AppState, tab types.TabState, paneID types.PaneID) string {
	pane, ok := tab.Panes[paneID]
	if !ok {
		return fmt.Sprintf("[missing] %s", paneID)
	}
	parts := []string{fmt.Sprintf("[tiled] %s", renderPaneTitle(state, pane))}
	if paneID == tab.ActivePaneID {
		parts[0] = "> " + parts[0]
	}
	parts = append(parts, renderPaneOverviewStateParts(state, pane)...)
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
		parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
	}
	if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
		parts = append(parts, fmt.Sprintf("preview=%s", preview))
	}
	return compactSummaryLine(parts...)
}

// renderPaneOverviewStateParts 把 pane 在概览中的最小生命周期语义统一起来，
// 让 tiled/floating/tree 三种投影都能稳定表达 waiting/exited/empty 差异。
func renderPaneOverviewStateParts(state types.AppState, pane types.PaneState) []string {
	if pane.SlotState != "" && pane.SlotState != types.PaneSlotConnected {
		parts := []string{fmt.Sprintf("slot=%s", pane.SlotState)}
		switch pane.SlotState {
		case types.PaneSlotWaiting:
			parts = append(parts, "detail=layout pending")
		case types.PaneSlotEmpty:
			parts = append(parts, "detail=terminal missing")
		case types.PaneSlotExited:
			if pane.LastExitCode != nil {
				parts = append(parts, fmt.Sprintf("exit=%d", *pane.LastExitCode))
			} else if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.ExitCode != nil {
				parts = append(parts, fmt.Sprintf("exit=%d", *terminal.ExitCode))
			}
			parts = append(parts, "detail=history retained")
		}
		return parts
	}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		return []string{fmt.Sprintf("role=%s", role)}
	}
	parts := []string{fmt.Sprintf("slot=%s", pane.SlotState)}
	return parts
}

func summarizeTiledLayout(root *types.SplitNode, fallbackLeaves int) tiledLayoutSummary {
	if root == nil {
		return tiledLayoutSummary{
			Root:   "<implicit>",
			Depth:  1,
			Leaves: fallbackLeaves,
		}
	}
	depth, leaves := splitTreeMetrics(root)
	summary := tiledLayoutSummary{
		Root:   string(root.Direction),
		Depth:  depth,
		Leaves: leaves,
	}
	if summary.Root == "" {
		summary.Root = "<implicit>"
	}
	if root.First != nil || root.Second != nil {
		summary.Ratio = root.Ratio
		summary.HasRatio = true
	}
	if summary.Leaves < fallbackLeaves {
		summary.Root = "<implicit>"
		summary.Depth = 1
		summary.Leaves = fallbackLeaves
		summary.Ratio = 0
		summary.HasRatio = false
	}
	return summary
}

func splitTreeMetrics(node *types.SplitNode) (depth int, leaves int) {
	if node == nil {
		return 0, 0
	}
	if node.First == nil && node.Second == nil {
		return 1, 1
	}
	firstDepth, firstLeaves := splitTreeMetrics(node.First)
	secondDepth, secondLeaves := splitTreeMetrics(node.Second)
	maxDepth := firstDepth
	if secondDepth > maxDepth {
		maxDepth = secondDepth
	}
	return maxDepth + 1, firstLeaves + secondLeaves
}

func orderedTiledPaneIDs(tab types.TabState) []types.PaneID {
	if len(tab.Panes) == 0 {
		return nil
	}
	ordered := make([]types.PaneID, 0, len(tab.Panes))
	seen := map[types.PaneID]struct{}{}
	collectSplitPaneIDs(tab.RootSplit, tab, seen, &ordered)

	remaining := make([]types.PaneID, 0, len(tab.Panes))
	for paneID, pane := range tab.Panes {
		if pane.Kind != types.PaneKindTiled {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		remaining = append(remaining, paneID)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	return append(ordered, remaining...)
}

func orderedFloatingPaneIDs(tab types.TabState) []types.PaneID {
	ordered := make([]types.PaneID, 0, len(tab.FloatingOrder))
	seen := map[types.PaneID]struct{}{}
	for _, paneID := range tab.FloatingOrder {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		seen[paneID] = struct{}{}
		ordered = append(ordered, paneID)
	}
	remaining := make([]types.PaneID, 0, len(tab.Panes))
	for paneID, pane := range tab.Panes {
		if pane.Kind != types.PaneKindFloating {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		remaining = append(remaining, paneID)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	return append(ordered, remaining...)
}

func collectSplitPaneIDs(node *types.SplitNode, tab types.TabState, seen map[types.PaneID]struct{}, ordered *[]types.PaneID) {
	if node == nil {
		return
	}
	if node.First == nil && node.Second == nil {
		pane, ok := tab.Panes[node.PaneID]
		if !ok || pane.Kind != types.PaneKindTiled {
			return
		}
		if _, ok := seen[node.PaneID]; ok {
			return
		}
		seen[node.PaneID] = struct{}{}
		*ordered = append(*ordered, node.PaneID)
		return
	}
	collectSplitPaneIDs(node.First, tab, seen, ordered)
	collectSplitPaneIDs(node.Second, tab, seen, ordered)
}

func (r runtimeRenderer) renderPanePreview(terminalID types.TerminalID) string {
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
		return truncateLine(line, runtimeOutlinePreviewMaxWidth)
	}
	if len(rows) == 0 {
		return ""
	}
	return truncateLine(rows[len(rows)-1], runtimeOutlinePreviewMaxWidth)
}

func (r runtimeRenderer) renderTerminalSection(state types.AppState, pane types.PaneState, compact bool) []string {
	if pane.TerminalID == "" {
		lines := []string{compactSummaryLine("terminal_bar: disconnected", "terminal: <disconnected>")}
		lines = append(lines, renderPaneSlotLines(pane)...)
		return lines
	}

	// section 首行需要同时保留 bar 和正文主语义，这里只做普通拼接，
	// 避免 bar 的裁剪策略误伤正文字段可见性。
	lines := []string{compactSummaryLine(renderTerminalBar(state, pane), fmt.Sprintf("terminal: %s", pane.TerminalID))}
	terminal, ok := state.Domain.Terminals[pane.TerminalID]
	if ok {
		label := terminal.Name
		if label == "" {
			label = string(terminal.ID)
		}
		lines[0] = compactSummaryLine(lines[0], fmt.Sprintf("title: %s", label))
		stateParts := make([]string, 0, 6)
		if terminal.State != "" {
			stateParts = append(stateParts, fmt.Sprintf("terminal_state: %s", terminal.State))
		}
		if terminal.ExitCode != nil {
			stateParts = append(stateParts, fmt.Sprintf("terminal_exit_code: %d", *terminal.ExitCode))
		}
		stateParts = append(stateParts, fmt.Sprintf("terminal_visibility: %t", terminal.Visible))
		stateParts = append(stateParts, renderConnectionLines(state.Domain.Connections[pane.TerminalID], pane.ID)...)
		if len(terminal.Command) > 0 {
			stateParts = append(stateParts, fmt.Sprintf("terminal_command: %s", strings.Join(terminal.Command, " ")))
		}
		perLine := 4
		if compact {
			perLine = len(stateParts)
			if perLine == 0 {
				perLine = 1
			}
		}
		lines = appendCompactParts(lines, perLine, stateParts)
		if !compact {
			if tags := renderTerminalTags(terminal.Tags); tags != "" {
				lines = append(lines, fmt.Sprintf("terminal_tags: %s", tags))
			}
		}
	} else {
		connectionParts := renderConnectionLines(state.Domain.Connections[pane.TerminalID], pane.ID)
		if len(connectionParts) > 0 {
			lines = append(lines, compactLine(connectionParts...))
		}
	}

	if r.Screens != nil {
		if status, ok := r.Screens.Status(pane.TerminalID); ok {
			lines = appendRuntimeStatusLines(lines, status)
		}
	}
	lines = append(lines, renderPaneSlotLines(pane)...)
	return lines
}

func renderPaneSlotLines(pane types.PaneState) []string {
	switch pane.SlotState {
	case types.PaneSlotEmpty:
		return []string{
			"pane_slot_detail: terminal removed or not connected",
			"pane_actions:",
			"  [n] start new terminal",
			"  [a] connect existing terminal",
			"  [m] open terminal manager",
			"  [x] close pane",
		}
	case types.PaneSlotWaiting:
		return []string{
			"pane_slot_detail: waiting for layout or restore resolution",
			"pane_actions:",
			"  [n] start new terminal",
			"  [a] connect existing terminal",
			"  [m] open terminal manager",
			"  [x] close pane",
		}
	case types.PaneSlotExited:
		lines := []string{
			"pane_slot_detail: terminal program exited",
			"pane_history: retained",
			"pane_actions:",
			"  [r] restart terminal",
			"  [a] connect another terminal",
			"  [x] close pane",
		}
		return lines
	default:
		return nil
	}
}

func (r runtimeRenderer) renderScreenSection(pane types.PaneState, compact bool) []string {
	if pane.TerminalID == "" || r.Screens == nil {
		return []string{compactSummaryLine("screen_bar: state=unavailable", "screen: <unavailable>")}
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return []string{compactSummaryLine("screen_bar: state=unavailable", "screen: <unavailable>")}
	}
	rows, totalRows, truncated := renderSnapshotRows(snapshot)
	meta := []string{fmt.Sprintf("screen_rows: %d/%d", len(rows), totalRows)}
	if truncated {
		meta = append(meta, "screen_truncated: true")
	}
	if compact {
		return []string{
			compactSummaryLine(
				fmt.Sprintf("screen_bar: state=suppressed | rows=%d/%d", len(rows), totalRows),
				compactLine(append([]string{"screen: <suppressed by overlay>"}, meta...)...),
			),
		}
	}
	lines := []string{
		compactSummaryLine(
			fmt.Sprintf("screen_bar: state=preview | rows=%d/%d", len(rows), totalRows),
			compactLine(append([]string{"screen:"}, meta...)...),
		),
	}
	return append(lines, rows...)
}

func renderTerminalBar(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return "terminal_bar: disconnected"
	}
	parts := []string{fmt.Sprintf("terminal_bar: id=%s", pane.TerminalID)}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
		label := terminal.Name
		if label == "" {
			label = string(terminal.ID)
		}
		parts = append(parts, fmt.Sprintf("title=%s", label))
		if terminal.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
		}
	}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		parts = append(parts, fmt.Sprintf("role=%s", role))
	}
	return compactBarLine(parts...)
}

func renderTerminalRole(conn types.ConnectionState, paneID types.PaneID) string {
	if conn.TerminalID == "" || !containsPaneID(conn.ConnectedPaneIDs, paneID) {
		return ""
	}
	if conn.OwnerPaneID == paneID {
		return "owner"
	}
	return "follower"
}

func compactLine(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, " | ")
}

func compactBarLine(parts ...string) string {
	return truncateLine(compactLine(parts...), runtimeBarMaxWidth)
}

// compactSummaryLine 用于 section/status 这类“摘要 + 正文首行”。
// 这里给比 bar 更宽的预算，既避免长字段把视图横向撑爆，也尽量保留更多正文语义。
func compactSummaryLine(parts ...string) string {
	return truncateLine(compactLine(parts...), runtimeSummaryMaxWidth)
}

// compactDetailLine 用于 overlay/detail 元数据行。
// 这些行需要比 bar 更宽的预算，但也不能让超长 command/tag/hint 把主视图横向撑爆。
func compactDetailLine(parts ...string) string {
	return truncateLine(compactLine(parts...), runtimeDetailMaxWidth)
}

func truncateLine(line string, maxWidth int) string {
	if maxWidth <= 0 || len(line) <= maxWidth {
		return line
	}
	if maxWidth <= 3 {
		return line[:maxWidth]
	}
	// 语义栏位通常前后都带关键上下文，中间裁剪能同时保留头部主语与尾部状态。
	visible := maxWidth - 3
	head := visible / 2
	tail := visible - head
	return line[:head] + "..." + line[len(line)-tail:]
}

func appendCompactParts(lines []string, perLine int, parts []string) []string {
	if perLine <= 0 {
		perLine = len(parts)
	}
	current := make([]string, 0, perLine)
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		current = append(current, part)
		if len(current) == perLine {
			lines = append(lines, compactLine(current...))
			current = current[:0]
		}
	}
	if len(current) > 0 {
		lines = append(lines, compactLine(current...))
	}
	return lines
}

func renderConnectionLines(conn types.ConnectionState, paneID types.PaneID) []string {
	if conn.TerminalID == "" || !containsPaneID(conn.ConnectedPaneIDs, paneID) {
		return nil
	}
	role := "follower"
	if conn.OwnerPaneID == paneID {
		role = "owner"
	}
	return []string{
		fmt.Sprintf("connection_role: %s", role),
		fmt.Sprintf("connected_panes: %d", len(conn.ConnectedPaneIDs)),
	}
}

func containsPaneID(ids []types.PaneID, target types.PaneID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func renderTerminalTags(tags map[string]string) string {
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
		parts = append(parts, fmt.Sprintf("%s=%s", key, tags[key]))
	}
	return strings.Join(parts, ",")
}

func appendRuntimeStatusLines(lines []string, status RuntimeTerminalStatus) []string {
	lines = appendCompactParts(lines, 3, []string{
		runtimeStatePart(status),
		runtimeExitCodePart(status),
		runtimeSizePart(status),
	})
	lines = appendCompactParts(lines, 2, []string{
		runtimeAccessPart(status),
		runtimeSyncLostPart(status),
	})
	lines = appendCompactParts(lines, 1, []string{
		runtimeRemovedPart(status),
		runtimeReadErrorPart(status),
	})
	return lines
}

func runtimeStatePart(status RuntimeTerminalStatus) string {
	if status.State == "" {
		return ""
	}
	return fmt.Sprintf("runtime_state: %s", status.State)
}

func runtimeExitCodePart(status RuntimeTerminalStatus) string {
	if !status.Closed || status.ExitCode == nil {
		return ""
	}
	return fmt.Sprintf("runtime_exit_code: %d", *status.ExitCode)
}

func runtimeSizePart(status RuntimeTerminalStatus) string {
	if status.Size.Cols == 0 && status.Size.Rows == 0 {
		return ""
	}
	return fmt.Sprintf("runtime_size: %dx%d", status.Size.Cols, status.Size.Rows)
}

func runtimeAccessPart(status RuntimeTerminalStatus) string {
	if !status.ObserverOnly {
		return ""
	}
	return "runtime_access: observer_only"
}

func runtimeSyncLostPart(status RuntimeTerminalStatus) string {
	if !status.SyncLost {
		return ""
	}
	return fmt.Sprintf("runtime_sync_lost: %d", status.SyncLostDroppedBytes)
}

func runtimeRemovedPart(status RuntimeTerminalStatus) string {
	if status.RemovedReason == "" {
		return ""
	}
	return fmt.Sprintf("runtime_removed: %s", status.RemovedReason)
}

func runtimeReadErrorPart(status RuntimeTerminalStatus) string {
	if status.ReadError == "" {
		return ""
	}
	return fmt.Sprintf("runtime_read_error: %s", status.ReadError)
}

func renderSnapshotRows(snapshot *protocol.Snapshot) ([]string, int, bool) {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return []string{"<empty>"}, 0, false
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		lines = append(lines, renderSnapshotRow(row))
	}
	return snapshotPreviewWindow(lines, runtimeScreenPreviewRows)
}

// snapshotPreviewWindow 优先展示最后一段真正有内容的终端输出，
// 避免 terminal 尾部残留大量空行时，screen shell 和 screen section 只看到一大片空白。
func snapshotPreviewWindow(lines []string, limit int) ([]string, int, bool) {
	totalRows := len(lines)
	if totalRows == 0 {
		return []string{"<empty>"}, 0, false
	}
	if limit <= 0 {
		limit = runtimeScreenPreviewRows
	}
	lastMeaningful := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		lastMeaningful = i
		break
	}
	if lastMeaningful < 0 {
		if totalRows > limit {
			lines = lines[:limit]
		}
		return lines, totalRows, totalRows > len(lines)
	}
	start := 0
	if lastMeaningful+1 > limit {
		start = lastMeaningful + 1 - limit
	}
	window := append([]string(nil), lines[start:lastMeaningful+1]...)
	truncated := start > 0 || lastMeaningful+1 < totalRows
	return window, totalRows, truncated
}

func renderSnapshotRow(row []protocol.Cell) string {
	if len(row) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return strings.TrimRight(builder.String(), " ")
}

func renderOverlayLines(overlay types.OverlayState, focus types.FocusState) []string {
	bar := renderOverlayBar(overlay.Kind, focus.Layer)
	switch overlay.Kind {
	case types.OverlayWorkspacePicker:
		picker, ok := overlay.Data.(*workspacedomain.PickerState)
		if !ok || picker == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderWorkspacePickerLines(picker))
	case types.OverlayHelp:
		return mergeSectionBar(bar, renderHelpLines(overlay.ReturnFocus))
	case types.OverlayTerminalManager:
		manager, ok := overlay.Data.(*terminalmanagerdomain.State)
		if !ok || manager == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderTerminalManagerLines(manager))
	case types.OverlayTerminalPicker:
		picker, ok := overlay.Data.(*terminalpickerdomain.State)
		if !ok || picker == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderTerminalPickerLines(picker))
	case types.OverlayLayoutResolve:
		resolve, ok := overlay.Data.(*layoutresolvedomain.State)
		if !ok || resolve == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderLayoutResolveLines(resolve))
	case types.OverlayPrompt:
		prompt, ok := overlay.Data.(*promptdomain.State)
		if !ok || prompt == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderPromptLines(prompt))
	default:
		return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
	}
}

func renderHelpLines(returnFocus types.FocusState) []string {
	layer := returnFocus.Layer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	paneID := returnFocus.PaneID
	if paneID == "" {
		paneID = types.PaneID("none")
	}
	return []string{
		compactLine(
			fmt.Sprintf("help_bar: layer=%s", layer),
			fmt.Sprintf("pane=%s", paneID),
		),
		"help_most_used: Ctrl-p pane | Ctrl-t tab | Ctrl-w workspace | Ctrl-f picker | Ctrl-o floating | Ctrl-g global",
		"help_concepts: pane = work slot | terminal = running entity",
		"help_shared: owner controls terminal-level operations | follower observes without control",
		"help_exit: close pane != stop terminal != detach TUI",
	}
}

func renderOverlayBar(kind types.OverlayKind, focusLayer types.FocusLayer) string {
	return compactBarLine(
		fmt.Sprintf("overlay_bar: kind=%s", kind),
		fmt.Sprintf("focus=%s", focusLayer),
	)
}

func mergeSectionBar(bar string, body []string) []string {
	if len(body) == 0 {
		return []string{bar}
	}
	lines := append([]string{}, body...)
	lines[0] = compactSummaryLine(bar, lines[0])
	return lines
}

func renderWorkspacePickerLines(picker *workspacedomain.PickerState) []string {
	rows := picker.VisibleRows()
	lines := []string{compactLine(
		renderWorkspacePickerBar(picker),
		compactLine(
			fmt.Sprintf("workspace_picker_query: %s", picker.Query()),
			fmt.Sprintf("workspace_picker_row_count: %d", len(rows)),
		),
	)}
	if node, ok := picker.SelectedNode(); ok {
		lines = append(lines, compactLine(
			fmt.Sprintf("workspace_picker_selected: %s", node.Key),
			fmt.Sprintf("workspace_picker_selected_kind: %s", node.Kind),
			fmt.Sprintf("workspace_picker_selected_label: %s", node.Label),
		))
	}
	if row, ok := picker.SelectedRow(); ok {
		lines = append(lines, compactLine(
			fmt.Sprintf("workspace_picker_selected_depth: %d", row.Depth),
			fmt.Sprintf("workspace_picker_selected_expanded: %t", row.Expanded),
			fmt.Sprintf("workspace_picker_selected_match: %t", row.Match),
		))
	}
	selected, hasSelection := picker.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Node.Key == selected.Node.Key {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeOverlayPreviewRows, selectedIndex)
	rowMeta := []string{"workspace_picker_rows:", fmt.Sprintf("workspace_picker_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "workspace_picker_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Node.Key == selected.Node.Key {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
	}
	return lines
}

func renderWorkspacePickerBar(picker *workspacedomain.PickerState) string {
	if node, ok := picker.SelectedNode(); ok {
		parts := []string{
			fmt.Sprintf("workspace_picker_bar: selected=%s", node.Key),
			fmt.Sprintf("kind=%s", node.Kind),
		}
		if row, ok := picker.SelectedRow(); ok {
			parts = append(parts, fmt.Sprintf("depth=%d", row.Depth))
		}
		return compactBarLine(parts...)
	}
	return "workspace_picker_bar: none"
}

func renderTerminalManagerLines(manager *terminalmanagerdomain.State) []string {
	rows := manager.VisibleRows()
	lines := []string{compactLine(
		renderTerminalManagerBar(manager),
		compactLine(
			fmt.Sprintf("terminal_manager_query: %s", manager.Query()),
			fmt.Sprintf("terminal_manager_row_count: %d", len(rows)),
		),
	)}
	if row, ok := manager.SelectedRow(); ok && row.Kind == terminalmanagerdomain.RowKindTerminal {
		selectedTags := ""
		if tags := renderTerminalTags(row.Tags); tags != "" {
			selectedTags = fmt.Sprintf("terminal_manager_selected_tags: %s", tags)
		}
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_manager_selected: %s", row.TerminalID),
				fmt.Sprintf("terminal_manager_selected_label: %s", row.Label),
				fmt.Sprintf("terminal_manager_selected_kind: %s", row.Kind),
				fmt.Sprintf("terminal_manager_selected_section: %s", row.Section),
			),
			compactLine(
				fmt.Sprintf("terminal_manager_selected_state: %s", row.State),
				fmt.Sprintf("terminal_manager_selected_visible: %t", row.Visible),
				fmt.Sprintf("terminal_manager_selected_visibility: %s", row.VisibilityLabel),
				fmt.Sprintf("terminal_manager_selected_connected_panes: %d", row.ConnectedPaneCount),
				fmt.Sprintf("terminal_manager_selected_location_count: %d", row.LocationCount),
			),
			compactDetailLine(
				fmt.Sprintf("terminal_manager_selected_command: %s", row.Command),
				fmt.Sprintf("terminal_manager_selected_owner: %s", row.OwnerSlotLabel),
				selectedTags,
			),
		)
	} else if terminalID, ok := manager.SelectedTerminalID(); ok {
		lines = append(lines, fmt.Sprintf("terminal_manager_selected: %s", terminalID))
	}
	selected, hasSelection := manager.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeTerminalManagerPreviewRows, selectedIndex)
	rowMeta := []string{"terminal_manager_rows:", fmt.Sprintf("terminal_manager_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "terminal_manager_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Kind != terminalmanagerdomain.RowKindHeader && row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	if detail, ok := manager.SelectedDetail(); ok {
		detailTags := ""
		if tags := renderDetailTags(detail.Tags); tags != "" {
			detailTags = fmt.Sprintf("detail_tags: %s", tags)
		}
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_manager_detail: %s", detail.Name),
				fmt.Sprintf("detail_terminal: %s", detail.TerminalID),
				fmt.Sprintf("detail_state: %s", detail.State),
				fmt.Sprintf("detail_visible: %t", detail.Visible),
				fmt.Sprintf("detail_visibility: %s", detail.VisibilityLabel),
			),
			compactDetailLine(
				fmt.Sprintf("detail_connected_panes: %d", detail.ConnectedPaneCount),
				fmt.Sprintf("detail_location_count: %d", len(detail.Locations)),
				fmt.Sprintf("detail_command: %s", detail.Command),
			),
			compactDetailLine(fmt.Sprintf("detail_owner: %s", detail.OwnerSlotLabel), detailTags),
		)
		if locations := renderDetailLocations(detail.Locations); len(locations) > 0 {
			previewLocations, truncated := overlayPreviewStrings(locations, runtimeOverlayDetailPreviewRows)
			meta := []string{"detail_locations:", fmt.Sprintf("detail_locations_rendered: %d", len(previewLocations))}
			if truncated {
				meta = append(meta, "detail_locations_truncated: true")
			}
			lines = append(lines, compactDetailLine(meta...))
			lines = append(lines, previewLocations...)
		}
		actionRows := terminalmanagerdomain.ActionRows()
		lines = append(lines, compactLine(
			"terminal_manager_actions:",
			fmt.Sprintf("terminal_manager_actions_rendered: %d", len(actionRows)),
		))
		for _, action := range actionRows {
			lines = append(lines, fmt.Sprintf("  [%s] %s", action.ID, action.Label))
		}
	}
	return lines
}

func renderTerminalManagerBar(manager *terminalmanagerdomain.State) string {
	if row, ok := manager.SelectedRow(); ok {
		selected := row.Label
		if row.TerminalID != "" {
			selected = string(row.TerminalID)
		}
		parts := []string{fmt.Sprintf("terminal_manager_bar: selected=%s", selected)}
		if row.Section != "" {
			parts = append(parts, fmt.Sprintf("section=%s", row.Section))
		}
		parts = append(parts, fmt.Sprintf("kind=%s", row.Kind))
		return compactBarLine(parts...)
	} else if terminalID, ok := manager.SelectedTerminalID(); ok {
		return compactBarLine(fmt.Sprintf("terminal_manager_bar: selected=%s", terminalID))
	}
	return "terminal_manager_bar: none"
}

func renderDetailTags(tags []terminalmanagerdomain.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for _, tag := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", tag.Key, tag.Value))
	}
	return strings.Join(parts, ",")
}

func renderDetailLocations(locations []terminalmanagerdomain.Location) []string {
	if len(locations) == 0 {
		return nil
	}
	lines := make([]string, 0, len(locations))
	for _, location := range locations {
		lines = append(lines, fmt.Sprintf("  [location] %s/%s/%s", location.WorkspaceName, location.TabName, location.SlotLabel))
	}
	return lines
}

func renderPromptLines(prompt *promptdomain.State) []string {
	lines := []string{compactLine(
		renderPromptBar(prompt),
		compactLine(
			fmt.Sprintf("prompt_title: %s", prompt.Title),
			fmt.Sprintf("prompt_kind: %s", prompt.Kind),
		),
	)}
	if prompt.TerminalID != "" {
		lines = append(lines, fmt.Sprintf("prompt_terminal: %s", prompt.TerminalID))
	}
	if len(prompt.Fields) == 0 {
		lines = append(lines,
			compactDetailLine("prompt_active_field: draft", "prompt_active_label: draft", fmt.Sprintf("prompt_active_value: %s", prompt.Draft)),
			compactLine("prompt_active_index: 0", "prompt_field_count: 0"),
			"prompt_fields: | prompt_fields_rendered: 1",
			fmt.Sprintf("> [draft] %s", prompt.Draft),
		)
		return append(lines, renderPromptActionLines()...)
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	lines = append(lines,
		compactDetailLine(
			fmt.Sprintf("prompt_active_field: %s", prompt.Fields[active].Key),
			fmt.Sprintf("prompt_active_label: %s", prompt.Fields[active].Label),
			fmt.Sprintf("prompt_active_value: %s", prompt.Fields[active].Value),
		),
		compactLine(
			fmt.Sprintf("prompt_active_index: %d", active),
			fmt.Sprintf("prompt_field_count: %d", len(prompt.Fields)),
		),
	)
	previewFields, truncated := overlayPreviewRowsAround(prompt.Fields, runtimeOverlayDetailPreviewRows, active)
	fieldMeta := []string{"prompt_fields:", fmt.Sprintf("prompt_fields_rendered: %d", len(previewFields))}
	if truncated {
		fieldMeta = append(fieldMeta, "prompt_fields_truncated: true")
	}
	lines = append(lines, compactLine(fieldMeta...))
	for _, field := range previewFields {
		prefix := "  "
		if field.Key == prompt.Fields[active].Key && field.Label == prompt.Fields[active].Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s: %s", prefix, field.Key, field.Label, field.Value))
	}
	return append(lines, renderPromptActionLines()...)
}

func renderPromptActionLines() []string {
	actionRows := promptdomain.ActionRows()
	lines := []string{compactLine(
		"prompt_actions:",
		fmt.Sprintf("prompt_actions_rendered: %d", len(actionRows)),
	)}
	for _, action := range actionRows {
		lines = append(lines, fmt.Sprintf("  [%s] %s", action.ID, action.Label))
	}
	return lines
}

func renderPromptBar(prompt *promptdomain.State) string {
	activeField := "draft"
	if len(prompt.Fields) > 0 {
		active := prompt.Active
		if active < 0 || active >= len(prompt.Fields) {
			active = 0
		}
		activeField = prompt.Fields[active].Key
	}
	parts := []string{fmt.Sprintf("prompt_bar: kind=%s", prompt.Kind)}
	if prompt.TerminalID != "" {
		parts = append(parts, fmt.Sprintf("terminal=%s", prompt.TerminalID))
	}
	parts = append(parts, fmt.Sprintf("active=%s", activeField))
	return compactBarLine(parts...)
}

func renderTerminalPickerLines(picker *terminalpickerdomain.State) []string {
	rows := picker.VisibleRows()
	lines := []string{compactLine(
		renderTerminalPickerBar(picker),
		compactLine(
			fmt.Sprintf("terminal_picker_query: %s", picker.Query()),
			fmt.Sprintf("terminal_picker_row_count: %d", len(rows)),
		),
	)}
	if row, ok := picker.SelectedRow(); ok && row.Kind == terminalpickerdomain.RowKindTerminal {
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_picker_selected: %s", row.TerminalID),
				fmt.Sprintf("terminal_picker_selected_label: %s", row.Label),
				fmt.Sprintf("terminal_picker_selected_kind: %s", row.Kind),
			),
			compactDetailLine(
				fmt.Sprintf("terminal_picker_selected_state: %s", row.State),
				fmt.Sprintf("terminal_picker_selected_command: %s", row.Command),
			),
			compactLine(
				fmt.Sprintf("terminal_picker_selected_visible: %t", row.Visible),
				fmt.Sprintf("terminal_picker_selected_connected_panes: %d", row.ConnectedPaneCount),
			),
		)
		if tags := renderTerminalTags(row.Tags); tags != "" {
			lines = append(lines, fmt.Sprintf("terminal_picker_selected_tags: %s", tags))
		}
	} else if terminalID, ok := picker.SelectedTerminalID(); ok {
		lines = append(lines, fmt.Sprintf("terminal_picker_selected: %s", terminalID))
	}
	selected, hasSelection := picker.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeOverlayPreviewRows, selectedIndex)
	rowMeta := []string{"terminal_picker_rows:", fmt.Sprintf("terminal_picker_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "terminal_picker_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	return lines
}

func renderTerminalPickerBar(picker *terminalpickerdomain.State) string {
	parts := []string{fmt.Sprintf("terminal_picker_bar: query=%s", picker.Query())}
	if row, ok := picker.SelectedRow(); ok {
		if row.TerminalID != "" {
			parts = append(parts, fmt.Sprintf("selected=%s", row.TerminalID))
		} else {
			parts = append(parts, fmt.Sprintf("selected=%s", row.Label))
		}
		parts = append(parts, fmt.Sprintf("kind=%s", row.Kind))
	} else if terminalID, ok := picker.SelectedTerminalID(); ok {
		parts = append(parts, fmt.Sprintf("selected=%s", terminalID))
	}
	return compactBarLine(parts...)
}

func renderLayoutResolveLines(resolve *layoutresolvedomain.State) []string {
	rows := resolve.Rows()
	lines := []string{
		compactLine(
			renderLayoutResolveBar(resolve),
			fmt.Sprintf("layout_resolve_pane: %s", resolve.PaneID),
			fmt.Sprintf("layout_resolve_role: %s", resolve.Role),
		),
		compactDetailLine(
			fmt.Sprintf("layout_resolve_hint: %s", resolve.Hint),
			fmt.Sprintf("layout_resolve_row_count: %d", len(rows)),
		),
	}
	if row, ok := resolve.SelectedRow(); ok {
		lines = append(lines, compactLine(
			fmt.Sprintf("layout_resolve_selected: %s", row.Action),
			fmt.Sprintf("layout_resolve_selected_label: %s", row.Label),
		))
	}
	selected, hasSelection := resolve.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Action == selected.Action && row.Label == selected.Label {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeOverlayPreviewRows, selectedIndex)
	rowMeta := []string{"layout_resolve_rows:", fmt.Sprintf("layout_resolve_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "layout_resolve_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Action == selected.Action && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Action, row.Label))
	}
	return lines
}

func renderLayoutResolveBar(resolve *layoutresolvedomain.State) string {
	parts := []string{
		fmt.Sprintf("layout_resolve_bar: pane=%s", resolve.PaneID),
		fmt.Sprintf("role=%s", resolve.Role),
	}
	if row, ok := resolve.SelectedRow(); ok {
		parts = append(parts, fmt.Sprintf("selected=%s", row.Action))
	}
	return compactBarLine(parts...)
}

func renderNoticeLines(notices []btui.Notice) []string {
	total := countVisibleNotices(notices)
	if total == 0 {
		return []string{"notice_bar: total=0 | showing=0 | notices: 0"}
	}
	visible := visibleNotices(notices)
	start := 0
	if len(visible) > runtimeOverlayDetailPreviewRows {
		start = len(visible) - runtimeOverlayDetailPreviewRows
	}
	previewNotices := visible[start:]
	truncated := start > 0
	lines := []string{renderNoticeBar(visible, previewNotices)}
	if groupBar := renderNoticeGroupBar(visible); groupBar != "" {
		lines = append(lines, groupBar)
	}
	meta := []string{fmt.Sprintf("notices_rendered: %d", len(previewNotices))}
	if truncated {
		meta = append(meta, "notices_truncated: true")
	}
	lines = append(lines, compactLine(meta...))
	for _, notice := range previewNotices {
		line := fmt.Sprintf("[%s] %s", notice.Level, notice.Text)
		if notice.Count > 1 {
			line = fmt.Sprintf("%s (x%d)", line, notice.Count)
		}
		lines = append(lines, line)
	}
	return lines
}

func countVisibleNotices(notices []btui.Notice) int {
	return len(visibleNotices(notices))
}

func lastVisibleNotice(notices []btui.Notice) (btui.Notice, bool) {
	visible := visibleNotices(notices)
	if len(visible) == 0 {
		return btui.Notice{}, false
	}
	return visible[len(visible)-1], true
}

func visibleNotices(notices []btui.Notice) []btui.Notice {
	visible := make([]btui.Notice, 0, len(notices))
	for _, notice := range notices {
		if strings.TrimSpace(notice.Text) == "" {
			continue
		}
		visible = append(visible, notice)
	}
	return visible
}

func renderNoticeBar(visible []btui.Notice, preview []btui.Notice) string {
	parts := []string{
		fmt.Sprintf("notice_bar: total=%d", len(visible)),
		fmt.Sprintf("showing=%d", len(preview)),
	}
	if len(visible) > 0 {
		parts = append(parts, fmt.Sprintf("last=%s", visible[len(visible)-1].Level))
	}
	parts = append(parts, "notices:")
	return compactBarLine(parts...)
}

func renderNoticeGroupBar(visible []btui.Notice) string {
	errorCount := 0
	infoCount := 0
	for _, notice := range visible {
		switch notice.Level {
		case btui.NoticeLevelError:
			errorCount++
		case btui.NoticeLevelInfo:
			infoCount++
		}
	}
	parts := make([]string, 0, 2)
	if errorCount > 0 {
		parts = append(parts, fmt.Sprintf("error=%d", errorCount))
	}
	if infoCount > 0 {
		parts = append(parts, fmt.Sprintf("info=%d", infoCount))
	}
	if len(parts) == 0 {
		return ""
	}
	parts[0] = fmt.Sprintf("notice_group_bar: %s", parts[0])
	return compactBarLine(parts...)
}

func overlayPreviewRowsAround[T any](rows []T, limit int, focusIndex int) ([]T, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	if focusIndex < 0 {
		focusIndex = 0
	}
	if focusIndex >= len(rows) {
		focusIndex = len(rows) - 1
	}
	start := focusIndex - limit + 1
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
		start = end - limit
	}
	return rows[start:end], true
}

func overlayPreviewStrings(rows []string, limit int) ([]string, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}
