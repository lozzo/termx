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
	"github.com/muesli/termenv"
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
	panelLabel        lipgloss.Style
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
	activeTiledBorder lipgloss.Style
	idleTiledBorder   lipgloss.Style
	activeFloatBorder lipgloss.Style
	idleFloatBorder   lipgloss.Style
	runToken          lipgloss.Style
	stopToken         lipgloss.Style
	exitToken         lipgloss.Style
	waitToken         lipgloss.Style
	emptyToken        lipgloss.Style
	ownerToken        lipgloss.Style
	followerToken     lipgloss.Style
	floatToken        lipgloss.Style
	offscreenToken    lipgloss.Style
}

func defaultModernShellTheme() modernShellTheme {
	// 现代主壳依赖明确的语义色层级；这里固定到 TrueColor，
	// 避免测试环境或非 TTY 检测把 renderer 静默降成“只有字符、没有颜色”的假阳性。
	lipgloss.SetColorProfile(termenv.TrueColor)
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
		panelLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7dd3fc")).
			Bold(true),
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
		activeTiledBorder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#38bdf8")),
		idleTiledBorder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#475569")),
		activeFloatBorder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f59e0b")),
		idleFloatBorder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#b45309")),
		runToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10b981")),
		stopToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f59e0b")),
		exitToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fb7185")),
		waitToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fbbf24")),
		emptyToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94a3b8")),
		ownerToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60a5fa")),
		followerToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#93c5fd")),
		floatToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f59e0b")),
		offscreenToken: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fb923c")),
	}
}

// RenderShell 是默认第一视觉 renderer。
// 它只负责产品态主界面，不再输出 debug section；详细调试信息继续走 --debug-ui 的旧 renderer。
func (r modernScreenShellRenderer) RenderShell(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, notices []btui.Notice, metrics wireframeMetrics) string {
	theme := defaultModernShellTheme()
	width := max(64, metrics.ViewportWidth)
	height := max(18, metrics.ViewportHeight)

	header := r.renderTopBar(theme, state, workspace, tab, pane, metrics, width)
	tabs := r.renderTabBar(theme, state, workspace, tab, pane, width)
	context := r.renderContextBar(theme, state, workspace, tab, pane, metrics, width)
	footer := r.renderFooter(theme, state, pane, notices, width)

	bodyHeight := height - 4
	if bodyHeight < 8 {
		bodyHeight = 8
	}

	body := r.renderWorkbench(theme, state, tab, pane, metrics, width, bodyHeight)
	if state.UI.Overlay.Kind != types.OverlayNone {
		body = r.renderOverlayViewport(theme, state, tab, pane, metrics, width, bodyHeight)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, tabs, context, body, footer)
	return theme.app.Render(view)
}

func (r modernScreenShellRenderer) renderTopBar(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width int) string {
	contentWidth := max(1, width-2)
	left := renderModernHeaderBrand(theme, workspace)
	right := renderModernHeaderRightAdaptive(theme, state, workspace, tab, pane, metrics, width)
	return theme.topBar.Render(fillANSIHorizontal(left, right, contentWidth))
}

func (r modernScreenShellRenderer) renderTabBar(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, width int) string {
	contentWidth := max(1, width-2)
	left := renderModernTabStripAdaptive(theme, state, workspace, tab, pane, width)
	right := r.renderWorkspaceSummaryTextAdaptive(theme, workspace, width)
	return theme.subBar.Render(fillANSIHorizontal(left, right, contentWidth))
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
	return fmt.Sprintf("tabs %d  •  panes %d  •  terminals %d  •  floating %d", tabs, panes, len(terminals), floating)
}

func (r modernScreenShellRenderer) renderWorkspaceSummaryTextAdaptive(theme modernShellTheme, workspace types.WorkspaceState, width int) string {
	if shouldRenderUltraCompactChrome(width) {
		tabs, panes, terminals, floating := renderModernWorkspaceCounts(workspace)
		return theme.panelMeta.Render(fmt.Sprintf("%dt  •  %dp  •  %dx  •  %df", tabs, panes, terminals, floating))
	}
	tabs, panes, terminals, floating := renderModernWorkspaceCounts(workspace)
	items := []string{
		theme.chip.Render(renderModernCountLabel(tabs, "tab")),
		theme.chip.Render(renderModernCountLabel(panes, "pane")),
		theme.chip.Render(renderModernCountLabel(terminals, "term")),
	}
	if floating > 0 {
		items = append(items, theme.chip.Render(fmt.Sprintf("%d float", floating)))
	}
	return strings.Join(items, " ")
}

func (r modernScreenShellRenderer) renderContextBar(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width int) string {
	contentWidth := max(1, width-2)
	left := theme.panelMeta.Render(renderModernPanePathAdaptive(state, workspace, tab, pane, width))
	right := renderModernContextChromeLineAdaptive(theme, state, tab, pane, metrics, width)
	return theme.subBar.Render(fillANSIHorizontal(left, right, contentWidth))
}

func (r modernScreenShellRenderer) renderWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width, height int) string {
	tiledPaneIDs := orderedTiledPaneIDs(tab)
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	switch {
	case len(tiledPaneIDs) > 0 && len(floatingPaneIDs) > 0:
		return r.renderMixedWorkbench(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, metrics, width, height)
	case len(tiledPaneIDs) > 1:
		return r.renderSplitWorkbench(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, metrics, width, height)
	case len(floatingPaneIDs) > 0 && len(tiledPaneIDs) == 0:
		return r.renderFloatingWorkbench(theme, state, tab, pane, floatingPaneIDs, metrics, width, height)
	default:
		return r.renderSingleWorkbench(theme, state, pane, metrics, width, height, true)
	}
}

func (r modernScreenShellRenderer) renderSingleWorkbench(theme modernShellTheme, state types.AppState, pane types.PaneState, metrics wireframeMetrics, width, height int, active bool) string {
	tab := types.TabState{
		ActivePaneID: pane.ID,
		Panes:        map[types.PaneID]types.PaneState{pane.ID: pane},
	}
	if width < 72 || height < 12 {
		return r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, height), width, height, false)
	}
	if shouldRenderCompactWorkbenchRail(width, height) {
		return r.renderSingleWorkbenchCompact(theme, state, pane, metrics, width, height, active)
	}
	sidebarWidth := renderModernWorkbenchSidebarWidth(width)
	canvasWidth := max(32, width-sidebarWidth-1)
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, canvasWidth, height), canvasWidth, height, false)
	sidebar := r.renderSingleWorkbenchSidebar(theme, state, pane, sidebarWidth, height, active)
	return lipgloss.JoinHorizontal(lipgloss.Top, canvas, " ", sidebar)
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

func (r modernScreenShellRenderer) renderModernCanvasPaneTitle(state types.AppState, pane types.PaneState) string {
	title := renderModernPaneDisplayTitle(state, pane)
	if pane.Kind == types.PaneKindFloating {
		if pane.SlotState != types.PaneSlotConnected {
			title = renderModernPaneDisplayTitle(state, pane)
		}
	}
	return title
}

func (r modernScreenShellRenderer) renderModernCanvasPaneLines(state types.AppState, pane types.PaneState, overlayActive bool, maxRows int) []string {
	return runtimeRenderer{Screens: r.Screens}.renderScreenShellPaneLines(state, pane, overlayActive, maxRows)
}

func (r modernScreenShellRenderer) renderModernCanvasPaneMeta(theme modernShellTheme, state types.AppState, pane types.PaneState, metrics wireframeMetrics, active bool, paneWidth int) string {
	if paneWidth <= 22 {
		return ""
	}
	compact := paneWidth <= 28
	parts := make([]string, 0, 4)
	switch pane.SlotState {
	case types.PaneSlotConnected:
		stateToken := "● run"
		stateStyle := theme.runToken
		if pane.TerminalID != "" {
			if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
				switch terminal.State {
				case types.TerminalRunStateExited:
					stateToken = "○ exit"
					stateStyle = theme.exitToken
				case types.TerminalRunStateStopped:
					stateToken = "○ stop"
					stateStyle = theme.stopToken
				default:
					stateToken = "● run"
					stateStyle = theme.runToken
				}
			}
		}
		if compact {
			switch stateToken {
			case "○ exit":
				stateToken = "○"
			case "○ stop":
				stateToken = "◌"
			default:
				stateToken = "●"
			}
		}
		parts = append(parts, renderModernCanvasToken(stateStyle, stateToken))
		role := renderModernCanvasPaneRoleToken(state, pane)
		if compact {
			switch role {
			case "owner":
				role = "own"
			case "follow":
				role = "fol"
			}
		}
		if role != "" {
			parts = append(parts, renderModernCanvasRoleToken(theme, role))
		}
	case types.PaneSlotWaiting:
		if compact {
			parts = append(parts, renderModernCanvasToken(theme.waitToken, "◌"))
		} else {
			parts = append(parts, renderModernCanvasToken(theme.waitToken, "◌ waiting"))
		}
	case types.PaneSlotExited:
		if compact {
			parts = append(parts, renderModernCanvasToken(theme.exitToken, "○"))
		} else {
			parts = append(parts, renderModernCanvasToken(theme.exitToken, "○ exited"))
		}
	default:
		if compact {
			parts = append(parts, renderModernCanvasToken(theme.emptyToken, "○"))
		} else {
			parts = append(parts, renderModernCanvasToken(theme.emptyToken, "○ empty"))
		}
	}
	if pane.Kind == types.PaneKindFloating {
		zIndex, zTotal := renderModernFloatingZ(state, pane)
		if compact {
			parts = append(parts, theme.floatToken.Render("◫"))
		} else {
			parts = append(parts, theme.floatToken.Render("◫ float"))
		}
		if zIndex > 0 && zTotal > 1 {
			if compact {
				parts = append(parts, fmt.Sprintf("z%d", zIndex))
			} else {
				parts = append(parts, fmt.Sprintf("z %d/%d", zIndex, zTotal))
			}
		}
		if renderModernFloatingPaneOffscreen(pane, metrics) {
			if compact {
				parts = append(parts, theme.offscreenToken.Render("off"))
			} else {
				parts = append(parts, theme.offscreenToken.Render("offscreen"))
			}
			if active {
				parts = append(parts, theme.offscreenToken.Render("c center"))
			}
		}
	}
	return strings.Join(parts, "  ")
}

func renderModernCanvasPaneRoleToken(state types.AppState, pane types.PaneState) string {
	switch renderScreenShellPaneCardRole(state, pane) {
	case "owner":
		return "owner"
	case "follower":
		return "follow"
	default:
		return ""
	}
}

func renderModernCanvasToken(style lipgloss.Style, token string) string {
	return style.Render(token)
}

func renderModernCanvasRoleToken(theme modernShellTheme, token string) string {
	switch token {
	case "owner", "own":
		return theme.ownerToken.Render(token)
	case "follow", "fol":
		return theme.followerToken.Render(token)
	default:
		return theme.panelMeta.Render(token)
	}
}

// renderModernFloatingPaneOffscreen 基于当前 viewport 判断浮窗是否越过了当前可视区域。
// 这能在 modern 主壳里直接给出“为什么看起来被裁掉，以及可以 center 呼回”的反馈。
func renderModernFloatingPaneOffscreen(pane types.PaneState, metrics wireframeMetrics) bool {
	if pane.Kind != types.PaneKindFloating {
		return false
	}
	if pane.Rect.W <= 0 || pane.Rect.H <= 0 {
		return false
	}
	viewportWidth := metrics.ViewportWidth
	viewportHeight := metrics.ViewportHeight
	if viewportWidth <= 0 {
		viewportWidth = runtimeWireframeWidth
	}
	if viewportHeight <= 0 {
		viewportHeight = 24
	}
	return pane.Rect.X < 0 ||
		pane.Rect.Y < 0 ||
		pane.Rect.X+pane.Rect.W > viewportWidth ||
		pane.Rect.Y+pane.Rect.H > viewportHeight
}

func (r modernScreenShellRenderer) renderSplitWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, width, height int) string {
	if width < 72 || height < 12 {
		return r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, height), width, height, false)
	}
	if shouldRenderCompactWorkbenchRail(width, height) {
		return r.renderSplitWorkbenchCompact(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, metrics, width, height)
	}
	sidebarWidth := renderModernWorkbenchSidebarWidth(width)
	canvasWidth := max(32, width-sidebarWidth-1)
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, canvasWidth, height), canvasWidth, height, false)
	sidebar := r.renderSplitWorkbenchSidebar(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, sidebarWidth, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, canvas, " ", sidebar)
}

func (r modernScreenShellRenderer) renderFloatingWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, width, height int) string {
	if width < 72 || height < 12 {
		return r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, height), width, height, false)
	}
	if shouldRenderCompactWorkbenchRail(width, height) {
		return r.renderFloatingWorkbenchCompact(theme, state, tab, pane, floatingPaneIDs, metrics, width, height)
	}
	sidebarWidth := renderModernWorkbenchSidebarWidth(width)
	canvasWidth := max(32, width-sidebarWidth-1)
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, canvasWidth, height), canvasWidth, height, false)
	sidebar := r.renderFloatingWorkbenchSidebar(theme, state, tab, pane, floatingPaneIDs, sidebarWidth, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, canvas, " ", sidebar)
}

func (r modernScreenShellRenderer) renderMixedWorkbench(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, width, height int) string {
	if len(floatingPaneIDs) == 0 {
		return r.renderSplitWorkbench(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, metrics, width, height)
	}
	strip := r.renderDetachedFloatingStrip(theme, state, tab, floatingPaneIDs, width)
	mainHeight := max(8, height-1)
	if width < 72 || mainHeight < 11 {
		canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, mainHeight), width, mainHeight, false)
		return lipgloss.JoinVertical(lipgloss.Left, strip, canvas)
	}
	if shouldRenderCompactWorkbenchRail(width, mainHeight) {
		compact := r.renderMixedWorkbenchCompact(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, metrics, width, mainHeight)
		return lipgloss.JoinVertical(lipgloss.Left, strip, compact)
	}
	sidebarWidth := renderModernWorkbenchSidebarWidth(width)
	canvasWidth := max(32, width-sidebarWidth-1)
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, canvasWidth, mainHeight), canvasWidth, mainHeight, false)
	sidebar := r.renderMixedWorkbenchSidebar(theme, state, tab, pane, tiledPaneIDs, floatingPaneIDs, sidebarWidth, mainHeight)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		strip,
		lipgloss.JoinHorizontal(lipgloss.Top, canvas, " ", sidebar),
	)
}

func (r modernScreenShellRenderer) renderWorkbenchCanvas(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width, height int, overlayActive bool) string {
	return theme.terminalBody.Render(strings.Join(r.renderWorkbenchCanvasLines(theme, state, tab, pane, metrics, width, height, overlayActive), "\n"))
}

func (r modernScreenShellRenderer) renderWorkbenchCanvasLines(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width, height int, overlayActive bool) []string {
	canvasHeight := max(8, height)
	canvas := newScreenShellCanvas(width, canvasHeight)
	tiledPaneIDs := orderedTiledPaneIDs(tab)
	if len(tiledPaneIDs) == 0 && pane.Kind != types.PaneKindFloating {
		tiledPaneIDs = []types.PaneID{pane.ID}
	}
	if len(tiledPaneIDs) > 0 {
		rects := renderScreenShellTiledRects(tab, width, canvasHeight, tiledPaneIDs)
		for _, paneID := range tiledPaneIDs {
			targetPane, ok := tab.Panes[paneID]
			if !ok {
				if paneID == pane.ID {
					targetPane = pane
				} else {
					continue
				}
			}
			rect, ok := rects[paneID]
			if !ok {
				continue
			}
			bodyRows := renderScreenShellPaneCanvasBodyRows(rect.H)
			box := renderModernCanvasPaneBox(
				theme,
				rect.W,
				rect.H,
				r.renderModernCanvasPaneTitle(state, targetPane),
				r.renderModernCanvasPaneMeta(theme, state, targetPane, metrics, paneID == tab.ActivePaneID, rect.W),
				r.renderModernCanvasPaneLines(state, targetPane, overlayActive, bodyRows),
				paneID == tab.ActivePaneID,
				targetPane.Kind == types.PaneKindFloating,
			)
			canvas.stampLines(rect.X, rect.Y, box)
		}
	}
	for _, paneID := range orderedFloatingPaneIDs(tab) {
		targetPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		rect := normalizeFloatingCanvasRect(targetPane.Rect, metrics.ViewportWidth, metrics.ViewportHeight, canvasHeight)
		bodyRows := renderScreenShellPaneCanvasBodyRows(rect.H)
		box := renderModernCanvasPaneBox(
			theme,
			rect.W,
			rect.H,
			r.renderModernCanvasPaneTitle(state, targetPane),
			r.renderModernCanvasPaneMeta(theme, state, targetPane, metrics, paneID == tab.ActivePaneID, rect.W),
			r.renderModernCanvasPaneLines(state, targetPane, overlayActive, bodyRows),
			paneID == tab.ActivePaneID,
			targetPane.Kind == types.PaneKindFloating,
		)
		canvas.stampLines(rect.X, rect.Y, box)
	}
	return canvas.lines()
}

func renderModernCanvasPaneBox(theme modernShellTheme, width int, height int, title string, meta string, body []string, active bool, floating bool) []string {
	if width < 12 {
		width = 12
	}
	if height < 4 {
		height = 4
	}
	topLeft, topRight, bottomLeft, bottomRight, vertical, horizontal := renderModernCanvasPaneBorderGlyphs(active, floating)
	innerWidth := width - 2
	bodyRows := max(1, height-2)
	borderStyle := renderModernCanvasPaneBorderStyle(theme, active, floating)
	lines := []string{renderModernCanvasBorderLine(theme, borderStyle, topLeft, topRight, horizontal, title, meta, innerWidth)}
	for _, line := range clampPaddedLines(body, bodyRows) {
		lines = append(lines, borderStyle.Render(vertical)+padModernCanvasLine(line, innerWidth)+borderStyle.Render(vertical))
	}
	lines = append(lines, borderStyle.Render(bottomLeft)+borderStyle.Render(strings.Repeat(horizontal, innerWidth))+borderStyle.Render(bottomRight))
	return lines
}

func renderModernCanvasPaneBorderGlyphs(active bool, floating bool) (topLeft, topRight, bottomLeft, bottomRight, vertical, horizontal string) {
	switch {
	case floating && active:
		return "╔", "╗", "╚", "╝", "║", "═"
	case floating:
		return "╭", "╮", "╰", "╯", "│", "─"
	case active:
		return "┏", "┓", "┗", "┛", "┃", "━"
	default:
		return "┌", "┐", "└", "┘", "│", "─"
	}
}

func renderModernCanvasPaneBorderStyle(theme modernShellTheme, active bool, floating bool) lipgloss.Style {
	switch {
	case floating && active:
		return theme.activeFloatBorder
	case floating:
		return theme.idleFloatBorder
	case active:
		return theme.activeTiledBorder
	default:
		return theme.idleTiledBorder
	}
}

func renderModernCanvasBorderLine(theme modernShellTheme, borderStyle lipgloss.Style, left, right, horizontal, title, meta string, innerWidth int) string {
	if innerWidth < 1 {
		return borderStyle.Render(left) + borderStyle.Render(right)
	}
	title = strings.TrimSpace(title)
	meta = strings.TrimSpace(meta)
	title = theme.panelTitle.Render(title)
	if meta == "" {
		titleBudget := innerWidth - 2
		if titleBudget < 1 {
			titleBudget = 1
		}
		title = xansi.Truncate(title, titleBudget, "…")
		leftPart := borderStyle.Render(horizontal) + " " + title + " "
		fill := innerWidth - xansi.StringWidth(horizontal+" ") - xansi.StringWidth(title) - 1
		if fill < 0 {
			fill = 0
		}
		return borderStyle.Render(left) + leftPart + borderStyle.Render(strings.Repeat(horizontal, fill)) + borderStyle.Render(right)
	}
	leftPrefix := horizontal + " "
	leftSuffix := " "
	rightPrefix := " "
	rightSuffix := " " + horizontal
	metaWidth := xansi.StringWidth(rightPrefix + meta + rightSuffix)
	titleBudget := innerWidth - metaWidth - xansi.StringWidth(leftPrefix) - xansi.StringWidth(leftSuffix) - 1
	if titleBudget < 6 {
		metaBudget := max(8, innerWidth/3)
		meta = xansi.Truncate(meta, metaBudget, "…")
		metaWidth = xansi.StringWidth(rightPrefix + meta + rightSuffix)
		titleBudget = innerWidth - metaWidth - xansi.StringWidth(leftPrefix) - xansi.StringWidth(leftSuffix) - 1
	}
	if titleBudget < 1 {
		titleBudget = 1
	}
	title = xansi.Truncate(title, titleBudget, "…")
	leftPart := borderStyle.Render(horizontal) + " " + title + leftSuffix
	rightPart := rightPrefix + meta + " " + borderStyle.Render(horizontal)
	fill := innerWidth - xansi.StringWidth(leftPart) - xansi.StringWidth(rightPart)
	if fill < 1 {
		fill = 1
	}
	return borderStyle.Render(left) + leftPart + borderStyle.Render(strings.Repeat(horizontal, fill)) + rightPart + borderStyle.Render(right)
}

func padModernCanvasLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = xansi.Truncate(line, width, "…")
	padding := width - xansi.StringWidth(line)
	if padding < 0 {
		padding = 0
	}
	return line + strings.Repeat(" ", padding)
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
		label := renderModernDetachedFloatingLabel(state, targetPane)
		if paneID == tab.ActivePaneID && tab.ActiveLayer == types.FocusLayerFloating {
			items = append(items, theme.activeChip.Render(truncateModernLine(label, 44)))
			continue
		}
		items = append(items, theme.chip.Render(truncateModernLine(label, 44)))
	}
	return theme.subBar.Render(fillANSIHorizontal(strings.Join(items, " "), theme.panelMeta.Render(fmt.Sprintf("%d floating", len(floatingPaneIDs))), max(1, width-2)))
}

func renderModernWorkbenchSidebarWidth(width int) int {
	return min(32, max(30, width/3))
}

func shouldRenderCompactWorkbenchRail(width, height int) bool {
	return width < 108 || height < 18
}

type modernSidebarEntry struct {
	Label string
	Value string
	Tone  string
}

func renderModernWorkbenchRail(theme modernShellTheme, title string, entries []modernSidebarEntry, width int) string {
	items := []string{theme.activeChip.Render(strings.ToUpper(strings.TrimSpace(title)))}
	for _, entry := range entries {
		value := strings.TrimSpace(entry.Value)
		if value == "" {
			continue
		}
		style := theme.panelMeta
		if entry.Tone == "body" {
			style = theme.terminalBody
		}
		label := theme.panelLabel.Render(strings.ToUpper(strings.TrimSpace(entry.Label)))
		items = append(items, label+" "+style.Render(value))
	}
	lines := renderModernOverlayTokenLines(width, items, func(line string) string {
		return theme.subBar.Render(padANSIHorizontal(line, max(1, width-2)))
	})
	return strings.Join(lines, "\n")
}

func renderModernWorkbenchCompactSectionHeight(section string) int {
	if strings.TrimSpace(section) == "" {
		return 0
	}
	return strings.Count(section, "\n") + 1
}

func (r modernScreenShellRenderer) renderSingleWorkbenchCompact(theme modernShellTheme, state types.AppState, pane types.PaneState, metrics wireframeMetrics, width, height int, active bool) string {
	tab := types.TabState{
		ActivePaneID: pane.ID,
		Panes:        map[types.PaneID]types.PaneState{pane.ID: pane},
	}
	summary := renderModernWorkbenchRail(theme, "Workbench", []modernSidebarEntry{
		{Label: "Active", Value: renderModernSingleWorkbenchCompactActiveLine(state, pane)},
		{Label: "Link", Value: renderModernSingleWorkbenchRoleSlotLine(state, pane)},
		{Label: "State", Value: renderModernRuntimeLabel(state, pane)},
	}, width)
	context := renderModernWorkbenchRail(theme, "Context", compactWorkbenchSignalEntries(
		modernSidebarEntry{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
		modernSidebarEntry{Label: "Focus", Value: renderModernSingleWorkbenchCompactFocusLine(state, pane, active)},
		modernSidebarEntry{Label: "View", Value: renderModernSingleWorkbenchSessionLine(state, pane)},
		modernSidebarEntry{Label: "Layer", Value: renderModernSingleWorkbenchCompactLayerLine(state)},
		modernSidebarEntry{Label: "Action", Value: renderModernSingleWorkbenchCompactActionLine(state, pane)},
	), width)
	canvasHeight := max(8, height-renderModernWorkbenchCompactSectionHeight(summary)-renderModernWorkbenchCompactSectionHeight(context))
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, canvasHeight), width, canvasHeight, false)
	return lipgloss.JoinVertical(lipgloss.Left, summary, context, canvas)
}

func (r modernScreenShellRenderer) renderSplitWorkbenchCompact(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, width, height int) string {
	summaryEntries := []modernSidebarEntry{
		{Label: "Active", Value: renderModernSplitWorkbenchCompactActiveLine(state, pane, len(tiledPaneIDs))},
		{Label: "Split", Value: renderModernSplitLayoutCompactSummary(tab, len(tiledPaneIDs))},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
	}
	if len(floatingPaneIDs) > 0 {
		summaryEntries = append(summaryEntries, modernSidebarEntry{Label: "Float", Value: fmt.Sprintf("%d detached", len(floatingPaneIDs))})
	}
	summary := renderModernWorkbenchRail(theme, "Layout", summaryEntries, width)
	context := renderModernWorkbenchRail(theme, "Context", compactWorkbenchSignalEntries(
		modernSidebarEntry{Label: "Focus", Value: renderModernSingleWorkbenchCompactFocusLine(state, pane, true)},
		modernSidebarEntry{Label: "Link", Value: renderModernSingleWorkbenchRoleSlotLine(state, pane)},
		modernSidebarEntry{Label: "View", Value: renderModernSingleWorkbenchSessionLine(state, pane)},
		modernSidebarEntry{Label: "Action", Value: renderModernSplitCompactActionLine(state)},
	), width)
	canvasHeight := max(8, height-renderModernWorkbenchCompactSectionHeight(summary)-renderModernWorkbenchCompactSectionHeight(context))
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, canvasHeight), width, canvasHeight, false)
	return lipgloss.JoinVertical(lipgloss.Left, summary, context, canvas)
}

func (r modernScreenShellRenderer) renderFloatingWorkbenchCompact(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, width, height int) string {
	summary := renderModernWorkbenchRail(theme, "Floating", []modernSidebarEntry{
		{Label: "Active", Value: renderModernFloatingWorkbenchCompactActiveLine(state, tab, floatingPaneIDs)},
		{Label: "Stack", Value: renderModernFloatingWorkbenchCompactStackLine(state, tab, floatingPaneIDs)},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
	}, width)
	context := renderModernWorkbenchRail(theme, "Context", compactWorkbenchSignalEntries(
		modernSidebarEntry{Label: "Focus", Value: renderModernFloatingWorkbenchCompactFocusLine(state, tab, floatingPaneIDs)},
		modernSidebarEntry{Label: "Link", Value: renderModernSingleWorkbenchRoleSlotLine(state, pane)},
		modernSidebarEntry{Label: "Layer", Value: renderModernFloatingWorkbenchCompactLayerLine(state)},
		modernSidebarEntry{Label: "Action", Value: renderModernFloatingWorkbenchCompactActionLine(state, tab, floatingPaneIDs)},
	), width)
	deck := renderModernWorkbenchRail(theme, "Window deck", compactWorkbenchSignalEntries(
		modernSidebarEntry{Label: "Stack", Value: renderModernFloatingDeckCompactLine(state, tab, floatingPaneIDs)},
	), width)
	canvasHeight := max(8, height-renderModernWorkbenchCompactSectionHeight(summary)-renderModernWorkbenchCompactSectionHeight(context)-renderModernWorkbenchCompactSectionHeight(deck))
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, canvasHeight), width, canvasHeight, false)
	return lipgloss.JoinVertical(lipgloss.Left, summary, context, canvas, deck)
}

func (r modernScreenShellRenderer) renderMixedWorkbenchCompact(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, metrics wireframeMetrics, width, height int) string {
	summary := renderModernWorkbenchRail(theme, "Mixed", []modernSidebarEntry{
		{Label: "Active", Value: renderModernMixedWorkbenchCompactActiveLine(state, pane)},
		{Label: "Stack", Value: fmt.Sprintf("Tiled %d  •  floating %d", len(tiledPaneIDs), len(floatingPaneIDs))},
		{Label: "Split", Value: renderModernSplitLayoutCompactSummary(tab, len(tiledPaneIDs))},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
	}, width)
	context := renderModernWorkbenchRail(theme, "Context", compactWorkbenchSignalEntries(
		modernSidebarEntry{Label: "Focus", Value: renderModernSingleWorkbenchCompactFocusLine(state, pane, true)},
		modernSidebarEntry{Label: "Link", Value: renderModernSingleWorkbenchRoleSlotLine(state, pane)},
		modernSidebarEntry{Label: "View", Value: renderModernSingleWorkbenchSessionLine(state, pane)},
		modernSidebarEntry{Label: "Action", Value: renderModernSplitCompactActionLine(state)},
	), width)
	deck := renderModernWorkbenchRail(theme, "Window deck", compactWorkbenchSignalEntries(
		modernSidebarEntry{Label: "Stack", Value: renderModernFloatingDeckCompactLine(state, tab, floatingPaneIDs)},
	), width)
	canvasHeight := max(8, height-renderModernWorkbenchCompactSectionHeight(summary)-renderModernWorkbenchCompactSectionHeight(context)-renderModernWorkbenchCompactSectionHeight(deck))
	canvas := r.renderWorkbenchCanvas(theme, state, tab, pane, renderModernWorkbenchCanvasMetrics(metrics, width, canvasHeight), width, canvasHeight, false)
	return lipgloss.JoinVertical(lipgloss.Left, summary, context, canvas, deck)
}

// renderWorkbenchSidebarPanel 把 modern workbench 的辅助信息统一收成一个窄侧栏盒子。
// 这样 split/floating/mixed 都能共享一致的“标题 + 摘要 + 当前上下文”结构，而不是继续散落在顶部 token 里。
func renderWorkbenchSidebarPanel(theme modernShellTheme, title string, lines []string, width, height int) string {
	if width < 18 {
		width = 18
	}
	if height < 4 {
		height = 4
	}
	contentWidth := max(12, width-4)
	contentHeight := max(2, height-2)
	body := []string{theme.panelTitle.Render(title)}
	for _, line := range lines {
		if strings.TrimSpace(xansi.Strip(line)) == "" {
			body = append(body, "")
			continue
		}
		body = append(body, truncateModernLine(line, contentWidth))
	}
	body = normalizeModernPanelLines(body, contentWidth, contentHeight)
	return theme.backdropPanel.Width(width - 2).Height(height - 2).Render(strings.Join(body, "\n"))
}

// renderWorkbenchSidebarEntries 把“说明句子”收成 label/value 状态面板。
// 这样右侧 rail 更接近 legacy 的控制面板，而不是长段落说明文。
func renderWorkbenchSidebarEntries(theme modernShellTheme, entries []modernSidebarEntry, width int) []string {
	if width < 18 {
		width = 18
	}
	labelWidth := 6
	if width >= 28 {
		labelWidth = 7
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		value := strings.TrimSpace(entry.Value)
		if value == "" {
			continue
		}
		style := theme.panelMeta
		if entry.Tone == "body" {
			style = theme.terminalBody
		}
		label := theme.panelLabel.Render(strings.ToUpper(strings.TrimSpace(entry.Label)))
		label = padANSIHorizontal(label, labelWidth)
		line := label + " " + style.Render(value)
		lines = append(lines, truncateModernLine(line, width))
	}
	return lines
}

func padANSIHorizontal(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = xansi.Truncate(text, width, "…")
	padding := width - xansi.StringWidth(text)
	if padding < 0 {
		padding = 0
	}
	return text + strings.Repeat(" ", padding)
}

// renderSingleWorkbenchSidebar 收口默认首屏的单 pane 工作台信息栏。
// 目标不是重复渲染 pane 卡片，而是把“我在哪、当前连接了什么、接下来能做什么”稳定放到首屏右侧。
func (r modernScreenShellRenderer) renderSingleWorkbenchSidebar(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int, active bool) string {
	summaryHeight := min(10, max(7, height/3+1))
	summaryLines := renderWorkbenchSidebarEntries(theme, []modernSidebarEntry{
		{Label: "Active", Value: renderModernSingleWorkbenchSummaryLine(state, pane)},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
		{Label: "Link", Value: renderModernSingleWorkbenchRoleSlotLine(state, pane)},
		{Label: "State", Value: renderModernSingleWorkbenchStateLine(state, pane)},
	}, max(12, width-4))
	summary := renderWorkbenchSidebarPanel(theme, "Workbench", summaryLines, width, summaryHeight)

	infoHeight := max(4, height-summaryHeight-1)
	infoEntries := []modernSidebarEntry{
		{Label: "Focus", Value: renderModernSingleWorkbenchFocusLine(state, pane, active)},
		{Label: "Layer", Value: renderModernSingleWorkbenchLayerLine(state)},
	}
	if terminalLine := renderModernSingleWorkbenchTerminalLine(state, pane); terminalLine != "" {
		infoEntries = append(infoEntries, modernSidebarEntry{Label: "Term", Value: terminalLine})
	}
	if sessionLine := renderModernSingleWorkbenchSessionLine(state, pane); sessionLine != "" {
		infoEntries = append(infoEntries, modernSidebarEntry{Label: "View", Value: sessionLine})
	}
	if commandLine := renderModernSingleWorkbenchCommandLine(state, pane); commandLine != "" {
		infoEntries = append(infoEntries, modernSidebarEntry{Label: "Cmd", Value: commandLine, Tone: "body"})
	}
	if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
		infoEntries = append(infoEntries, modernSidebarEntry{Label: "Peek", Value: preview, Tone: "body"})
	}
	infoLines := renderWorkbenchSidebarEntries(theme, infoEntries, max(12, width-4))
	infoLines = append(infoLines, renderModernWorkbenchKeyLines(theme, width, pane)...)
	info := renderWorkbenchSidebarPanel(theme, "Context", infoLines, width, infoHeight)
	return lipgloss.JoinVertical(lipgloss.Left, summary, info)
}

func renderModernWorkbenchCanvasMetrics(metrics wireframeMetrics, width, height int) wireframeMetrics {
	next := metrics
	next.ViewportWidth = max(32, width)
	next.ViewportHeight = max(8, height)
	if next.OverlayWidth > 0 {
		next.OverlayWidth = min(next.OverlayWidth, max(24, width-4))
	}
	return next
}

func (r modernScreenShellRenderer) renderSplitWorkbenchSidebar(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, width, height int) string {
	summaryHeight := min(10, max(7, height/3+1))
	summaryEntries := []modernSidebarEntry{
		{Label: "Active", Value: renderModernSplitWorkbenchTitleLine(state, pane, len(tiledPaneIDs))},
		{Label: "Split", Value: renderModernSplitLayoutSummary(tab, len(tiledPaneIDs))},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
	}
	if len(floatingPaneIDs) > 0 {
		summaryEntries = append(summaryEntries, modernSidebarEntry{Label: "Float", Value: fmt.Sprintf("%d detached windows available", len(floatingPaneIDs))})
	}
	summaryLines := renderWorkbenchSidebarEntries(theme, summaryEntries, max(12, width-4))
	summary := renderWorkbenchSidebarPanel(theme, "Layout", summaryLines, width, summaryHeight)
	infoHeight := max(4, height-summaryHeight-1)
	info := r.renderWorkbenchSignalsSidebar(theme, state, pane, width, infoHeight, true, renderModernSplitActionLine(state))
	return lipgloss.JoinVertical(lipgloss.Left, summary, info)
}

func (r modernScreenShellRenderer) renderFloatingWorkbenchSidebar(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID, width, height int) string {
	summaryHeight := min(9, max(6, height/4+1))
	summaryLines := renderWorkbenchSidebarEntries(theme, []modernSidebarEntry{
		{Label: "Active", Value: renderModernFloatingWorkbenchTitleLine(state, tab, floatingPaneIDs)},
		{Label: "Stack", Value: renderModernFloatingWorkbenchSummary(state, tab, floatingPaneIDs)},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
	}, max(12, width-4))
	summary := renderWorkbenchSidebarPanel(theme, "Floating", summaryLines, width, summaryHeight)
	infoHeight := min(10, max(8, height/3))
	info := r.renderWorkbenchSignalsSidebar(theme, state, pane, width, infoHeight, true, compactWorkbenchSignalLines(
		renderModernFloatingWorkbenchStateLine(state, tab, floatingPaneIDs),
		renderModernFloatingWorkbenchControlLine(state),
		renderModernFloatingModeHint(state),
	)...)
	deckHeight := max(4, height-summaryHeight-infoHeight-2)
	deck := r.renderFloatingDeck(theme, state, tab, floatingPaneIDs, width, deckHeight)
	return lipgloss.JoinVertical(lipgloss.Left, summary, info, deck)
}

func (r modernScreenShellRenderer) renderMixedWorkbenchSidebar(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID, width, height int) string {
	summaryHeight := min(10, max(7, height/4+1))
	summaryLines := renderWorkbenchSidebarEntries(theme, []modernSidebarEntry{
		{Label: "Active", Value: fmt.Sprintf("Mixed workbench  •  active %s", renderModernPaneDisplayTitle(state, pane))},
		{Label: "Stack", Value: fmt.Sprintf("Tiled %d  •  floating %d", len(tiledPaneIDs), len(floatingPaneIDs))},
		{Label: "Split", Value: renderModernSplitLayoutSummary(tab, len(tiledPaneIDs))},
		{Label: "Path", Value: renderModernWorkbenchLocationLine(state, pane)},
	}, max(12, width-4))
	summary := renderWorkbenchSidebarPanel(theme, "Workbench", summaryLines, width, summaryHeight)
	infoHeight := min(10, max(8, height/3))
	info := r.renderWorkbenchSignalsSidebar(theme, state, pane, width, infoHeight, true, compactWorkbenchSignalLines(
		renderModernFloatingWorkbenchStateLine(state, tab, floatingPaneIDs),
		renderModernSplitActionLine(state),
	)...)
	deckHeight := max(4, height-summaryHeight-infoHeight-2)
	deck := r.renderFloatingDeck(theme, state, tab, floatingPaneIDs, width, deckHeight)
	return lipgloss.JoinVertical(lipgloss.Left, summary, info, deck)
}

func compactWorkbenchSignalLines(lines ...string) []string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	return filtered
}

func compactWorkbenchSignalEntries(entries ...modernSidebarEntry) []modernSidebarEntry {
	filtered := make([]modernSidebarEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Value) == "" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// renderWorkbenchSignalsSidebar 统一收口 modern workbench 的信号/操作提示面板。
// single/split/floating/mixed 都复用这一层，避免不同布局各自输出一套不同语法。
func (r modernScreenShellRenderer) renderWorkbenchSignalsSidebar(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int, active bool, extraLines ...string) string {
	entries := []modernSidebarEntry{
		{Label: "Focus", Value: renderModernSingleWorkbenchFocusLine(state, pane, active)},
		{Label: "Link", Value: renderModernSingleWorkbenchRoleSlotLine(state, pane)},
	}
	if terminalLine := renderModernSingleWorkbenchTerminalLine(state, pane); terminalLine != "" {
		entries = append(entries, modernSidebarEntry{Label: "Term", Value: terminalLine})
	}
	if sessionLine := renderModernSingleWorkbenchSessionLine(state, pane); sessionLine != "" {
		entries = append(entries, modernSidebarEntry{Label: "View", Value: sessionLine})
	}
	entries = append(entries, modernSidebarEntry{Label: "Layer", Value: renderModernSingleWorkbenchLayerLine(state)})
	if commandLine := renderModernSingleWorkbenchCommandLine(state, pane); commandLine != "" {
		entries = append(entries, modernSidebarEntry{Label: "Cmd", Value: commandLine, Tone: "body"})
	}
	if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
		entries = append(entries, modernSidebarEntry{Label: "Peek", Value: preview, Tone: "body"})
	}
	lines := renderWorkbenchSidebarEntries(theme, entries, max(12, width-4))
	for _, line := range compactWorkbenchSignalLines(extraLines...) {
		lines = append(lines, renderWorkbenchSidebarEntries(theme, []modernSidebarEntry{{Label: "Action", Value: line}}, max(12, width-4))...)
	}
	lines = append(lines, renderModernWorkbenchKeyLines(theme, width, pane)...)
	return renderWorkbenchSidebarPanel(theme, "Context", lines, width, height)
}

func (r modernScreenShellRenderer) renderFloatingDeck(theme modernShellTheme, state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID, width, height int) string {
	header := theme.panelTitle.Render(fmt.Sprintf("Window deck  •  %d windows", len(floatingPaneIDs)))
	if len(floatingPaneIDs) == 0 {
		return theme.mutedPanel.Width(width - 2).Height(height - 2).Render(strings.Join([]string{header, theme.panelMeta.Render("No floating windows")}, "\n"))
	}
	if height <= 6 {
		lines := normalizeModernPanelLines([]string{
			header,
			theme.panelMeta.Render(renderModernFloatingDeckCompactLine(state, tab, floatingPaneIDs)),
		}, max(14, width-4), max(2, height-2))
		return theme.mutedPanel.Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
	}
	cardHeight := 9
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

func renderModernFloatingDeckCompactLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	parts := make([]string, 0, len(floatingPaneIDs))
	for index := len(floatingPaneIDs) - 1; index >= 0; index-- {
		paneID := floatingPaneIDs[index]
		targetPane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		label := renderModernPaneDisplayTitle(state, targetPane)
		if index == len(floatingPaneIDs)-1 {
			label = "top " + label
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "No floating windows"
	}
	return strings.Join(parts, "  •  ")
}

func (r modernScreenShellRenderer) renderFloatingDeckCard(theme modernShellTheme, state types.AppState, pane types.PaneState, width, height int, active bool, index int, total int) string {
	panelStyle := theme.mutedPanel
	if active {
		panelStyle = theme.activePanel
	}
	top := index == total-1
	rectText := "rect auto"
	if pane.Rect.W > 0 || pane.Rect.H > 0 {
		rectText = fmt.Sprintf("rect %d,%d  %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H)
	}
	preview := r.renderPanePreview(pane.TerminalID)
	if preview == "" {
		preview = string(pane.SlotState)
	}
	runtimeLine, commandLine := renderModernFloatingDeckTerminalSummary(state, pane)
	contentWidth := max(14, width-4)
	contentHeight := max(4, height-2)
	lines := []string{
		renderModernDeckCardHeader(theme, state, pane, contentWidth, active, top, index+1, max(1, total)),
		theme.panelMeta.Render(renderModernFloatingDeckState(active, top) + "  •  " + rectText),
		theme.panelMeta.Render(runtimeLine),
	}
	if commandLine != "" {
		lines = append(lines, theme.terminalBody.Render(truncateModernLine(commandLine, contentWidth)))
	}
	lines = append(lines, theme.terminalBody.Render(preview))
	lines = normalizeModernPanelLines(lines, contentWidth, contentHeight)
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
		if active && contentWidth < 38 {
			zIndex, zTotal = 0, 0
		}
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
	compact := shouldRenderCompactPaneLayout(width, maxRows, pane)
	if includeTitle {
		lines = append(lines, renderModernPaneHeaderBar(theme, state, pane, width, active, zIndex, zTotal))
	}
	if !compact {
		lines = append(lines, theme.panelTitle.Render("Status"))
	}
	lines = append(lines, theme.panelMeta.Render(renderModernPaneStatusLine(state, pane)))
	lines = append(lines, theme.panelMeta.Render(renderModernPaneIdentityLine(pane)))
	if pane.Kind == types.PaneKindFloating && (pane.Rect.W > 0 || pane.Rect.H > 0) {
		lines = append(lines, theme.panelMeta.Render(fmt.Sprintf("Geometry  rect %d,%d  %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H)))
	}

	switch pane.SlotState {
	case types.PaneSlotEmpty:
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Details"))
		}
		lines = append(lines,
			theme.terminalBody.Render("No terminal connected yet."),
		)
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Actions"))
		}
		lines = append(lines, theme.panelMeta.Render("Press n to start one, or a to connect an existing terminal."))
	case types.PaneSlotWaiting:
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Details"))
		}
		lines = append(lines,
			theme.terminalBody.Render("Waiting for a terminal connection."),
		)
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Actions"))
		}
		lines = append(lines, theme.panelMeta.Render("This pane is reserved by layout or restore flow."))
	case types.PaneSlotExited:
		exitText := "history retained"
		if pane.LastExitCode != nil {
			exitText = fmt.Sprintf("history retained  exit %d", *pane.LastExitCode)
		}
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Details"))
		}
		lines = append(lines,
			theme.terminalBody.Render("Terminal program exited."),
			theme.panelMeta.Render(exitText),
		)
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Actions"))
		}
		lines = append(lines, theme.panelMeta.Render("Press r to restart, or a to connect another terminal."))
	default:
		lines = append(lines, r.renderTerminalMetaLines(theme, state, pane, width)...)
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Actions"))
		}
		lines = append(lines, theme.panelMeta.Render(truncateModernLine(renderModernPaneActionLine(state, pane), width)))
	}
	if !compact {
		lines = append(lines, theme.panelTitle.Render("Footer"))
	}
	lines = append(lines, renderModernPaneFooter(theme, renderModernPaneFooterLine(state, pane, active), width, active))
	if pane.SlotState == types.PaneSlotConnected {
		if !compact {
			lines = append(lines, theme.panelTitle.Render("Screen"))
			lines = append(lines, r.renderTerminalScreenLines(theme, pane, width, maxRows-len(lines)-1, active)...)
		} else {
			lines = append(lines, r.renderTerminalScreenLines(theme, pane, width, maxRows-len(lines), active)...)
		}
	}

	return lines
}

func shouldRenderCompactPaneLayout(width, maxRows int, pane types.PaneState) bool {
	if width <= 40 || maxRows <= 16 {
		return true
	}
	if pane.Kind == types.PaneKindFloating && maxRows <= 18 {
		return true
	}
	if pane.SlotState == types.PaneSlotConnected && width <= 48 {
		return true
	}
	return false
}

// renderModernPaneHeaderBar 把 pane 顶部收成“左标题 + 右状态 badge”，
// 让 split/floating/mixed 在首屏上更接近旧版单线标题栏，而不是整句说明文。
func renderModernPaneHeaderBar(theme modernShellTheme, state types.AppState, pane types.PaneState, width int, active bool, zIndex int, zTotal int) string {
	compact := width <= 32
	left := theme.panelTitle.Render(truncateModernLine(renderModernPaneDisplayTitle(state, pane), max(6, width/2)))
	rightParts := []string{
		renderModernPaneStateBadge(theme, state, pane, compact),
		renderModernPaneRoleBadge(theme, state, pane, compact),
	}
	if pane.Kind == types.PaneKindFloating {
		rightParts = append(rightParts, renderModernPaneFloatBadge(theme, zIndex, zTotal, compact))
	}
	return renderModernPaneHeader(theme, fillANSIHorizontal(left, joinModernHeaderBadges(rightParts), max(8, width-2)), width, active)
}

func renderModernDeckCardHeader(theme modernShellTheme, state types.AppState, pane types.PaneState, width int, active bool, top bool, zIndex int, zTotal int) string {
	left := theme.panelTitle.Render(truncateModernLine(renderModernPaneDisplayTitle(state, pane), max(6, width/2)))
	rightParts := []string{
		renderModernPaneStateBadge(theme, state, pane, width <= 30),
		renderModernPaneRoleBadge(theme, state, pane, width <= 30),
	}
	if top {
		rightParts = append(rightParts, theme.floatToken.Render("top"))
	}
	if zTotal > 0 {
		rightParts = append(rightParts, theme.panelMeta.Render(fmt.Sprintf("%d/%d", zIndex, zTotal)))
	}
	return renderModernPaneHeader(theme, fillANSIHorizontal(left, joinModernHeaderBadges(rightParts), max(8, width)), width+2, active)
}

func joinModernHeaderBadges(parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(xansi.Strip(part)) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "  ")
}

func renderModernPaneStateBadge(theme modernShellTheme, state types.AppState, pane types.PaneState, compact bool) string {
	token := "○ empty"
	style := theme.emptyToken
	switch pane.SlotState {
	case types.PaneSlotConnected:
		token = "● run"
		style = theme.runToken
		if pane.TerminalID != "" {
			if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
				switch terminal.State {
				case types.TerminalRunStateExited:
					token = "○ exit"
					style = theme.exitToken
				case types.TerminalRunStateStopped:
					token = "◌ stop"
					style = theme.stopToken
				}
			}
		}
	case types.PaneSlotWaiting:
		token = "◌ wait"
		style = theme.waitToken
	case types.PaneSlotExited:
		token = "○ exit"
		style = theme.exitToken
	}
	if compact {
		parts := strings.Fields(token)
		if len(parts) > 0 {
			token = parts[0]
		}
	}
	return style.Render(token)
}

func renderModernPaneRoleBadge(theme modernShellTheme, state types.AppState, pane types.PaneState, compact bool) string {
	role := renderScreenShellPaneCardRole(state, pane)
	switch role {
	case "owner":
		if compact {
			return theme.ownerToken.Render("own")
		}
		return theme.ownerToken.Render("owner")
	case "follower":
		if compact {
			return theme.followerToken.Render("fol")
		}
		return theme.followerToken.Render("follower")
	default:
		return ""
	}
}

func renderModernPaneFloatBadge(theme modernShellTheme, zIndex int, zTotal int, compact bool) string {
	if zTotal <= 0 {
		return theme.floatToken.Render("◫")
	}
	if compact {
		return theme.floatToken.Render(fmt.Sprintf("◫%d", zIndex))
	}
	return theme.floatToken.Render(fmt.Sprintf("◫ %d/%d", zIndex, zTotal))
}

func renderModernPaneTitleBar(state types.AppState, pane types.PaneState, active bool, zIndex int, zTotal int) string {
	parts := []string{renderModernPaneDisplayTitle(state, pane)}
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

func renderModernFloatingDeckState(active bool, top bool) string {
	switch {
	case active && top:
		return "active window  •  top window"
	case active:
		return "active window"
	case top:
		return "top window"
	default:
		return "stack window"
	}
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
	runtimeParts := []string{stateLabel}
	if terminal.Visible {
		runtimeParts = append(runtimeParts, "visible")
	} else {
		runtimeParts = append(runtimeParts, "hidden")
	}
	role := renderScreenShellPaneCardRole(state, pane)
	if role == "" {
		role = "attached"
	}
	lines := []string{
		theme.panelMeta.Render(truncateModernLine("Terminal  Runtime  "+strings.Join(runtimeParts, "  •  "), width)),
		theme.panelMeta.Render(truncateModernLine(fmt.Sprintf("Connection  terminal %s  •  %s", pane.TerminalID, role), width)),
	}
	if len(terminal.Command) > 0 {
		lines = append(lines, theme.terminalBody.Render(truncateModernLine("Command  cmd "+strings.Join(terminal.Command, " "), width)))
	}
	if tags := renderTerminalTags(terminal.Tags); tags != "" {
		lines = append(lines, theme.panelMeta.Render(truncateModernLine("Tags  tags "+tags, width)))
	}
	return lines
}

func (r modernScreenShellRenderer) renderTerminalScreenLines(theme modernShellTheme, pane types.PaneState, width, maxRows int, active bool) []string {
	if maxRows <= 0 {
		maxRows = 1
	}
	rows := []string{"<screen unavailable>"}
	totalRows := 0
	stateLabel := "unavailable"
	truncated := false
	if pane.TerminalID != "" && r.Screens != nil {
		if snapshot, ok := r.Screens.Snapshot(pane.TerminalID); ok && snapshot != nil {
			rows, totalRows, truncated = renderSnapshotRows(snapshot)
			if totalRows <= 0 && len(rows) > 0 && strings.TrimSpace(rows[0]) != "<empty>" {
				totalRows = len(rows)
			}
			if active {
				stateLabel = "live"
			} else {
				stateLabel = "standby"
			}
		}
	}
	if maxRows < 4 {
		maxRows = 4
	}
	frameInnerWidth := max(8, width-2)
	bodyBudget := max(1, maxRows-3)
	if len(rows) > bodyBudget {
		rows = rows[len(rows)-bodyBudget:]
		truncated = true
	}
	if totalRows == 0 && stateLabel != "unavailable" {
		totalRows = len(rows)
	}
	displayRows := len(rows)
	if stateLabel == "unavailable" {
		displayRows = 0
	}
	focusLabel := "secondary"
	if active {
		focusLabel = "primary"
	}
	meta := fmt.Sprintf("rows %d/%d  •  %s  •  %s", displayRows, totalRows, stateLabel, focusLabel)
	if truncated {
		meta += "  •  trimmed"
	}
	lines := []string{theme.panelMeta.Render(truncateModernLine(meta, width))}
	lines = append(lines, theme.panelMeta.Render("╭"+strings.Repeat("─", frameInnerWidth)+"╮"))
	for _, row := range rows {
		lines = append(lines, theme.terminalBody.Render(renderModernScreenFrameLine(row, frameInnerWidth)))
	}
	lines = append(lines, theme.panelMeta.Render("╰"+strings.Repeat("─", frameInnerWidth)+"╯"))
	return lines
}

func renderModernScreenFrameLine(text string, innerWidth int) string {
	contentWidth := max(1, innerWidth-1)
	content := xansi.Truncate(text, contentWidth, "…")
	padding := contentWidth - xansi.StringWidth(content)
	if padding < 0 {
		padding = 0
	}
	return "│ " + content + strings.Repeat(" ", padding) + "│"
}

func (r modernScreenShellRenderer) renderOverlayViewport(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width, height int) string {
	backdropLines := r.renderWorkbenchCanvasLines(theme, state, tab, pane, metrics, width, height, false)
	backdropLines = renderModernOverlayBackdropWash(backdropLines)
	dialogWidth := renderModernOverlayDialogWidth(metrics, width)
	dialogLines := r.renderModernOverlayDialogLines(state, dialogWidth, height)
	canvas := newScreenShellCanvas(width, max(height, max(len(backdropLines), len(dialogLines))))
	canvas.stampLines(0, 0, backdropLines)
	dialogX := max(0, (width-dialogWidth)/2)
	dialogY := max(0, (canvas.height-len(dialogLines))/2)
	if width >= 68 {
		canvas.stampLines(dialogX+2, dialogY+1, renderModernOverlayShadow(dialogWidth, len(dialogLines)))
	}
	canvas.clearRect(dialogX, dialogY, dialogWidth, len(dialogLines))
	canvas.stampLines(dialogX, dialogY, dialogLines)
	return theme.terminalBody.Render(strings.Join(canvas.lines(), "\n"))
}

func (r modernScreenShellRenderer) renderModernOverlayDialogLines(state types.AppState, width int, height int) []string {
	if width < 24 {
		width = 24
	}
	compact := shouldRenderCompactOverlayDialog(width, height)
	contentWidth := max(20, width-2)
	body := []string{}
	body = append(body, renderModernOverlaySectionBoxLines(contentWidth, "CONTEXT[overlay]", renderModernOverlayContextSectionLines(state, contentWidth-2, compact))...)
	if !compact {
		body = append(body, "")
	}
	body = append(body, renderModernOverlaySectionBoxLines(contentWidth, "BACKDROP[workbench]", renderModernOverlayBackdropSectionLines(state, contentWidth-2, compact))...)
	if !compact {
		body = append(body, "")
	}
	body = append(body, r.renderModernOverlayDialogBody(state, contentWidth, compact)...)
	if footerLines := renderModernOverlayFooterSectionLines(state, contentWidth-2); len(footerLines) > 0 {
		if !compact {
			body = append(body, "")
		}
		body = append(body, renderModernOverlaySectionBoxLines(contentWidth, "FOOTER[overlay]", footerLines)...)
	}
	return renderModernOverlayDialogBox(width, overlayTitle(state.UI.Overlay.Kind), body)
}

func (r modernScreenShellRenderer) renderModernOverlayDialogBody(state types.AppState, width int, compact bool) []string {
	switch state.UI.Overlay.Kind {
	case types.OverlayHelp:
		mode := state.UI.Mode.Active
		if mode == "" {
			mode = types.ModeNone
		}
		lines := []string{
			"MOST USED  Ctrl-p pane | Ctrl-t tab",
			"Ctrl-w ws | Ctrl-f picker | Ctrl-o float | Ctrl-g global",
			fmt.Sprintf("CONTEXT  layer=%s  mode=%s", renderModernPrimaryLayer(state), mode),
			"SHARED TERMINAL  owner controls metadata / resize / stop",
			"follower observes the terminal without control",
			"BACKDROP  active pane stays visible under the modal",
			"ESC closes help and returns to the workbench.",
		}
		if compact {
			lines = []string{
				lines[0],
				lines[2],
				lines[3],
				lines[len(lines)-1],
			}
		}
		return renderModernOverlaySectionBoxLines(width, "BODY[help]", lines)
	default:
		return renderScreenShellDialogSections(state.UI.Overlay)
	}
}

func renderModernOverlaySectionBoxLines(width int, title string, body []string) []string {
	if width < 20 {
		width = 20
	}
	innerWidth := width - 2
	lines := []string{renderModernOverlayDialogBorder("┌", "┐", "─", title, innerWidth)}
	for _, line := range body {
		lines = append(lines, "│"+padModernCanvasLine(line, innerWidth)+"│")
	}
	lines = append(lines, "└"+strings.Repeat("─", innerWidth)+"┘")
	return lines
}

func renderModernOverlayContextSectionLines(state types.AppState, width int, compact bool) []string {
	lines := []string{renderModernOverlayStateLine(state)}
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		lines = append(lines, "return "+returnFocus)
	}
	if !compact {
		if workspaceLine := renderModernBackdropContextLine(state); workspaceLine != "" {
			lines = append(lines, workspaceLine)
		}
		if selected := renderModernOverlaySelection(state.UI.Overlay); selected != "" {
			lines = append(lines, selected)
		}
	}
	return renderModernOverlayPlainLines(lines, width)
}

func renderModernOverlayBackdropSectionLines(state types.AppState, width int, compact bool) []string {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return renderModernOverlayPlainLines([]string{"backdrop unavailable"}, width)
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return renderModernOverlayPlainLines([]string{"backdrop unavailable"}, width)
	}
	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		return renderModernOverlayPlainLines([]string{"backdrop unavailable"}, width)
	}
	lines := []string{}
	if backdrop := renderModernOverlayBackdropLine(state); backdrop != "" {
		lines = append(lines, backdrop)
	}
	lines = append(lines, renderModernBackdropPaneLine(state, pane))
	if !compact {
		lines = append(lines, renderModernBackdropLocationLine(state, pane))
		lines = append(lines, renderModernBackdropPausedLine(state))
	}
	return renderModernOverlayPlainLines(lines, width)
}

func renderModernOverlayFooterSectionLines(state types.AppState, width int) []string {
	lines := []string{}
	if footer, actions := renderScreenShellDialogFooter(state.UI.Overlay.Kind); footer != "" || actions != "" {
		if footer != "" {
			lines = append(lines, footer)
		}
		if actions != "" {
			lines = append(lines, actions)
		}
	}
	lines = append(lines, renderModernOverlayStateLine(state))
	return renderModernOverlayPlainLines(lines, width)
}

func renderModernOverlayPlainLines(lines []string, width int) []string {
	if width <= 0 {
		return append([]string(nil), lines...)
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, truncateModernLine(line, width))
	}
	return out
}

func shouldRenderCompactOverlayDialog(width int, height int) bool {
	return width <= 64 || height <= 20
}

func renderModernOverlayDialogBox(width int, title string, body []string) []string {
	if width < 24 {
		width = 24
	}
	innerWidth := width - 2
	lines := []string{renderModernOverlayDialogBorder("╔", "╗", "═", title, innerWidth)}
	for _, line := range body {
		lines = append(lines, "║"+padModernCanvasLine(line, innerWidth)+"║")
	}
	lines = append(lines, "╚"+strings.Repeat("═", innerWidth)+"╝")
	return lines
}

func renderModernOverlayDialogBorder(left, right, horizontal, title string, innerWidth int) string {
	if innerWidth < 1 {
		return left + right
	}
	label := " " + strings.TrimSpace(title) + " "
	label = xansi.Truncate(label, innerWidth, "…")
	fill := innerWidth - xansi.StringWidth(label)
	if fill < 0 {
		fill = 0
	}
	return left + label + strings.Repeat(horizontal, fill) + right
}

func (r modernScreenShellRenderer) renderOverlayPanel(theme modernShellTheme, state types.AppState, width int) string {
	title := overlayTitle(state.UI.Overlay.Kind)
	lines := []string{theme.modalTitle.Render(title)}
	if chrome := renderModernOverlayChrome(theme, state, width-6); len(chrome) > 0 {
		lines = append(lines, chrome...)
	}
	if body := r.renderOverlayPanelBody(theme, state, width-6); len(body) > 0 {
		lines = append(lines, "")
		lines = append(lines, body...)
	}
	if footer := renderModernOverlayFooterPanel(theme, state, width-6); footer != "" {
		lines = append(lines, "", footer)
	}
	return theme.modalPanel.Width(width - 2).Render(strings.Join(lines, "\n"))
}

func renderModernOverlayChrome(theme modernShellTheme, state types.AppState, width int) []string {
	lines := []string{theme.modalMeta.Render("Context")}
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		lines = append(lines, theme.modalBody.Render(truncateModernLine("return "+returnFocus, width)))
	}
	if selected := renderModernOverlaySelection(state.UI.Overlay); selected != "" {
		lines = append(lines, theme.modalBody.Render(truncateModernLine(selected, width)))
	}
	lines = append(lines,
		"",
		theme.modalMeta.Render("State"),
		theme.modalBody.Render(truncateModernLine(renderModernOverlayStateLine(state), width)),
	)
	return lines
}

func renderModernOverlaySelection(overlay types.OverlayState) string {
	switch overlay.Kind {
	case types.OverlayTerminalManager:
		manager, _ := overlay.Data.(*terminalmanagerdomain.State)
		if manager == nil {
			return ""
		}
		row, ok := manager.SelectedRow()
		if !ok {
			return ""
		}
		return "selected " + activeRowLabel(row, true)
	case types.OverlayWorkspacePicker:
		picker, _ := overlay.Data.(*workspacedomain.PickerState)
		if picker == nil {
			return ""
		}
		row, ok := picker.SelectedRow()
		if !ok {
			return ""
		}
		return fmt.Sprintf("selected %s  •  %s", row.Node.Label, row.Node.Kind)
	case types.OverlayTerminalPicker:
		picker, _ := overlay.Data.(*terminalpickerdomain.State)
		if picker == nil {
			return ""
		}
		row, ok := picker.SelectedRow()
		if !ok {
			return ""
		}
		return "selected " + row.Label
	case types.OverlayLayoutResolve:
		resolve, _ := overlay.Data.(*layoutresolvedomain.State)
		if resolve == nil {
			return ""
		}
		row, ok := resolve.SelectedRow()
		if !ok {
			return ""
		}
		return fmt.Sprintf("selected %s", row.Action)
	case types.OverlayPrompt:
		prompt, _ := overlay.Data.(*promptdomain.State)
		if prompt == nil {
			return ""
		}
		if len(prompt.Fields) == 0 {
			return "draft prompt"
		}
		active := prompt.Active
		if active < 0 || active >= len(prompt.Fields) {
			active = 0
		}
		return "active " + prompt.Fields[active].Key
	default:
		return ""
	}
}

func renderModernOverlayStateLine(state types.AppState) string {
	focus := state.UI.Focus.Layer
	if focus == "" {
		focus = types.FocusLayerOverlay
	}
	return fmt.Sprintf("state overlay %s  •  focus %s", state.UI.Overlay.Kind, focus)
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

func (r modernScreenShellRenderer) renderOverlayPanelBody(theme modernShellTheme, state types.AppState, width int) []string {
	overlay := state.UI.Overlay
	switch overlay.Kind {
	case types.OverlayHelp:
		return renderModernHelpOverlay(theme, state, width)
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

// renderModernHelpOverlay 把 help 也纳入统一 modal panel 结构，
// 让快捷键、概念模型、关闭动作保持和 manager/picker 一致的信息层级。
func renderModernHelpOverlay(theme modernShellTheme, state types.AppState, width int) []string {
	contentWidth := modernOverlayContentWidth(width)
	leftLines := []string{
		theme.modalMeta.Render(truncateModernLine(renderModernHelpContextLine(state), contentWidth)),
		"",
		theme.modalMeta.Render("Keysets"),
		theme.modalBody.Render(truncateModernLine("Ctrl-p pane  •  Ctrl-t tab", contentWidth)),
		theme.modalBody.Render(truncateModernLine("Ctrl-w workspace  •  Ctrl-f picker", contentWidth)),
		theme.modalBody.Render(truncateModernLine("Ctrl-o floating  •  Ctrl-g global", contentWidth)),
	}
	rightLines := []string{
		theme.modalMeta.Render("Model"),
		theme.modalBody.Render(truncateModernLine("pane is the view slot, terminal is the running entity", contentWidth)),
		"",
		theme.modalMeta.Render("Roles"),
		theme.modalBody.Render(truncateModernLine("owner can connect, resize, and edit terminal metadata", contentWidth)),
		theme.modalBody.Render(truncateModernLine("follower stays attached for viewing without control", contentWidth)),
		"",
		theme.modalMeta.Render("Exit semantics"),
		theme.modalBody.Render(truncateModernLine("close pane != stop terminal != detach TUI", contentWidth)),
	}
	actionLines := []string{
		theme.modalBody.Render(truncateModernLine("Esc close  •  ? toggle help", width)),
	}
	return renderModernOverlayPanels(theme, width, "Most used panel", leftLines, "Concepts panel", rightLines, "Action bar", actionLines)
}

func renderModernHelpContextLine(state types.AppState) string {
	layer := state.UI.Overlay.ReturnFocus.Layer
	if layer == "" {
		layer = state.UI.Focus.Layer
	}
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	return fmt.Sprintf("layer %s  •  mode %s", layer, mode)
}

func renderModernTerminalManagerOverlay(theme modernShellTheme, manager *terminalmanagerdomain.State, width int) []string {
	if manager == nil {
		return []string{theme.modalBody.Render("No terminal data loaded yet.")}
	}
	rows := manager.VisibleRows()
	selected, ok := manager.SelectedRow()
	contentWidth := modernOverlayContentWidth(width)
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
	actionLines := renderModernOverlayBodyTokenLines(theme, width, "Enter connect here", "t new tab", "o floating", "e edit", "s stop")
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
		lines = appendModernOverlayDetailSection(lines, theme, width, "Create", "Create a new terminal in the current workbench.")
		return lines
	}
	detail, ok := manager.SelectedDetail()
	if !ok {
		lines = append(lines, theme.modalBody.Render("No terminal detail loaded yet."))
		return lines
	}
	summaryParts := []string{string(detail.State), detail.VisibilityLabel}
	lines = appendModernOverlayDetailSection(lines, theme, width, "Runtime", strings.Join(summaryParts, "  •  "))
	connectionLines := []string{}
	if detail.OwnerSlotLabel != "" {
		connectionLines = append(connectionLines, "owner "+detail.OwnerSlotLabel)
	}
	if detail.ConnectedPaneCount > 0 {
		connectionLines = append(connectionLines, fmt.Sprintf("%d panes connected", detail.ConnectedPaneCount))
	}
	if len(connectionLines) > 0 {
		lines = appendModernOverlayDetailSection(lines, theme, width, "Connections", connectionLines...)
	}
	if detail.Command != "" {
		lines = appendModernOverlayDetailSection(lines, theme, width, "Command", "cmd "+detail.Command)
	}
	if len(detail.Tags) > 0 {
		lines = appendModernOverlayDetailSection(lines, theme, width, "Tags", "tags "+renderModernTags(detail.Tags))
	}
	if len(detail.Locations) > 0 {
		locationLines := []string{"path " + renderModernLocation(detail.Locations[0])}
		if len(detail.Locations) > 1 {
			locationLines = append(locationLines, fmt.Sprintf("%d locations", len(detail.Locations)))
		}
		lines = appendModernOverlayDetailSection(lines, theme, width, "Locations", locationLines...)
	}
	return lines
}

func renderModernWorkspacePickerOverlay(theme modernShellTheme, picker *workspacedomain.PickerState, width int) []string {
	if picker == nil {
		return []string{theme.modalBody.Render("No workspace tree loaded yet.")}
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	contentWidth := modernOverlayContentWidth(width)
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
	actionLines := renderModernOverlayBodyTokenLines(theme, width, "Enter jump", "/ filter", "Esc close")
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
		lines = appendModernOverlayDetailSection(lines, theme, width, "Target", "Create a new workspace and switch focus into it.")
		lines = appendModernOverlayDetailSection(lines, theme, width, "Route", "workspace root")
	case workspacedomain.TreeNodeKindWorkspace:
		lines = appendModernOverlayDetailSection(lines, theme, width, "Target", fmt.Sprintf("workspace %s  (%s)", row.Node.Label, row.Node.WorkspaceID))
		lines = appendModernOverlayDetailSection(lines, theme, width, "Route", fmt.Sprintf("workspace %s", row.Node.WorkspaceID))
	case workspacedomain.TreeNodeKindTab:
		lines = appendModernOverlayDetailSection(lines, theme, width, "Target", fmt.Sprintf("workspace %s  •  tab %s", row.Node.WorkspaceID, row.Node.Label))
		lines = appendModernOverlayDetailSection(lines, theme, width, "Route", fmt.Sprintf("workspace %s / tab %s", row.Node.WorkspaceID, row.Node.TabID))
	case workspacedomain.TreeNodeKindPane:
		lines = appendModernOverlayDetailSection(lines, theme, width, "Target", fmt.Sprintf("workspace %s  •  tab %s  •  pane %s", row.Node.WorkspaceID, row.Node.TabID, row.Node.PaneID))
		lines = appendModernOverlayDetailSection(lines, theme, width, "Route", fmt.Sprintf("workspace %s / tab %s / pane %s", row.Node.WorkspaceID, row.Node.TabID, row.Node.PaneID), "direct jump target inside the workbench tree")
	}
	return lines
}

func renderModernTerminalPickerOverlay(theme modernShellTheme, picker *terminalpickerdomain.State, width int) []string {
	if picker == nil {
		return []string{theme.modalBody.Render("No terminal options loaded yet.")}
	}
	rows := picker.VisibleRows()
	selectedRow, hasSelected := picker.SelectedRow()
	contentWidth := modernOverlayContentWidth(width)
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
	actionLines := renderModernOverlayBodyTokenLines(theme, width, "Enter connect", "n create new", "Esc close")
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
		lines = appendModernOverlayDetailSection(lines, theme, width, "Create", "Create a new terminal using current shell defaults.")
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
	lines = appendModernOverlayDetailSection(lines, theme, width, "Runtime", strings.Join(summaryParts, "  •  "))
	if row.Command != "" {
		lines = appendModernOverlayDetailSection(lines, theme, width, "Command", "cmd "+row.Command)
	}
	if len(row.Tags) > 0 {
		lines = appendModernOverlayDetailSection(lines, theme, width, "Tags", "tags "+renderModernStringTags(row.Tags))
	}
	return lines
}

func renderModernLayoutResolveOverlay(theme modernShellTheme, resolve *layoutresolvedomain.State, width int) []string {
	if resolve == nil {
		return []string{theme.modalBody.Render("No layout action required.")}
	}
	rows := resolve.Rows()
	selectedRow, hasSelected := resolve.SelectedRow()
	contentWidth := modernOverlayContentWidth(width)
	leftLines := []string{
		theme.modalMeta.Render(fmt.Sprintf("pane %s  •  role %s", resolve.PaneID, resolve.Role)),
	}
	if hasSelected {
		leftLines = append(leftLines, "", theme.modalMeta.Render("Selection"))
		leftLines = append(leftLines, theme.modalBody.Render(truncateModernLine(renderModernLayoutResolveSelectionText(selectedRow), contentWidth)))
	}
	selectedIndex := 0
	if hasSelected {
		for idx, row := range rows {
			if row.Action == selectedRow.Action && row.Label == selectedRow.Label {
				selectedIndex = idx
				break
			}
		}
	}
	leftLines = append(leftLines, "", theme.modalMeta.Render("Choices"))
	slice, _ := overlayPreviewRowsAround(rows, 4, selectedIndex)
	for _, row := range slice {
		text := truncateModernLine(renderModernLayoutResolveChoiceText(row), contentWidth)
		if hasSelected && row.Action == selectedRow.Action && row.Label == selectedRow.Label {
			leftLines = append(leftLines, theme.selectedListItem.Render("> "+text))
			continue
		}
		leftLines = append(leftLines, theme.listItem.Render("  "+text))
	}
	rightLines := []string{
		theme.modalMeta.Render("Connect target"),
		theme.modalBody.Render(truncateModernLine(fmt.Sprintf("pane %s", resolve.PaneID), contentWidth)),
		theme.modalBody.Render(truncateModernLine("role "+resolve.Role, contentWidth)),
	}
	if resolve.Hint != "" {
		rightLines = append(rightLines, "", theme.modalMeta.Render("Hint"))
		rightLines = append(rightLines, theme.modalBody.Render(truncateModernLine(resolve.Hint, contentWidth)))
	}
	actionLines := renderModernOverlayBodyTokenLines(theme, width, "Enter confirm", "Esc close")
	return renderModernOverlayPanels(theme, width, "Choices panel", leftLines, "Target panel", rightLines, "Action bar", actionLines)
}

// appendModernOverlayDetailSection 把 detail/target 信息拆成有标题的小节，
// 避免 modal 正文继续退化成难读的字段堆叠。
func appendModernOverlayDetailSection(lines []string, theme modernShellTheme, width int, title string, body ...string) []string {
	lines = append(lines, "", theme.modalMeta.Render(title))
	for _, line := range body {
		lines = append(lines, theme.modalBody.Render(truncateModernLine(line, width)))
	}
	return lines
}

func renderModernLayoutResolveSelectionText(row layoutresolvedomain.Row) string {
	return fmt.Sprintf("%s  •  %s", row.Label, strings.ReplaceAll(string(row.Action), "_", " "))
}

func renderModernLayoutResolveChoiceText(row layoutresolvedomain.Row) string {
	return fmt.Sprintf("[%s] %s", strings.ReplaceAll(string(row.Action), "_", " "), row.Label)
}

func renderModernPromptOverlay(theme modernShellTheme, prompt *promptdomain.State, width int) []string {
	if prompt == nil {
		return []string{theme.modalBody.Render("Prompt not ready.")}
	}
	contentWidth := modernOverlayContentWidth(width)
	if len(prompt.Fields) == 0 {
		leftLines := []string{
			theme.modalMeta.Render("draft mode"),
			"",
			theme.modalMeta.Render("Fields"),
			theme.modalBody.Render(truncateModernLine(prompt.Draft, contentWidth)),
		}
		rightLines := []string{
			theme.modalMeta.Render("Context"),
			theme.modalBody.Render(truncateModernLine(renderModernPromptContext(prompt), contentWidth)),
		}
		actionLines := renderModernOverlayBodyTokenLines(theme, width, "Enter submit", "Esc cancel")
		return renderModernOverlayPanels(theme, width, "Fields panel", leftLines, "Context panel", rightLines, "Action bar", actionLines)
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	leftLines := []string{
		theme.modalMeta.Render(fmt.Sprintf("%d fields  •  editing %s", len(prompt.Fields), prompt.Fields[active].Label)),
		"",
		theme.modalMeta.Render("Fields"),
	}
	for idx, field := range prompt.Fields {
		text := truncateModernLine(fmt.Sprintf("%s: %s", field.Label, field.Value), contentWidth)
		if idx == active {
			leftLines = append(leftLines, theme.selectedListItem.Render("> "+text))
			continue
		}
		leftLines = append(leftLines, theme.listItem.Render("  "+text))
	}
	rightLines := []string{
		theme.modalMeta.Render("Context"),
		theme.modalBody.Render(truncateModernLine(renderModernPromptContext(prompt), contentWidth)),
		"",
		theme.modalMeta.Render("Active value"),
		theme.modalBody.Render(truncateModernLine("value "+prompt.Fields[active].Value, contentWidth)),
	}
	actionLines := renderModernOverlayBodyTokenLines(theme, width, "Enter submit", "Tab next field", "Esc cancel")
	return renderModernOverlayPanels(theme, width, "Fields panel", leftLines, "Context panel", rightLines, "Action bar", actionLines)
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
	if width < 64 {
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

func renderModernOverlayFooterPanel(theme modernShellTheme, state types.AppState, width int) string {
	footerLines := renderModernOverlayFooterLines(theme, state.UI.Overlay.Kind, max(12, width-4))
	if len(footerLines) == 0 {
		return ""
	}
	lines := append([]string{}, footerLines...)
	lines = append(lines, theme.modalMeta.Render(truncateModernLine(renderModernOverlayStateLine(state), max(12, width-4))))
	return renderModernOverlaySectionPanel(theme, "Footer", lines, width)
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
	depth := max(1, summary.Depth-1)
	return fmt.Sprintf("Layout %s %s  •  depth %d  •  leaves %d", summary.Root, ratio, depth, tiledPanes)
}

func renderModernSplitLayoutCompactSummary(tab types.TabState, tiledPanes int) string {
	summary := summarizeTiledLayout(tab.RootSplit, tiledPanes)
	ratio := "auto"
	if summary.HasRatio {
		ratio = fmt.Sprintf("%02.0f/%02.0f", summary.Ratio*100, (1-summary.Ratio)*100)
	}
	return fmt.Sprintf("%s %s", summary.Root, ratio)
}

// renderModernSplitWorkbenchTitleLine 把 split 工作台标题和 active 信息合在一行，
// 这样顶部 chrome 更紧凑，给 pane screen 预览腾出更多高度。
func renderModernSplitWorkbenchTitleLine(state types.AppState, pane types.PaneState, tiledPanes int) string {
	return fmt.Sprintf("Split workbench  •  active %s  •  %d tiled panes", renderModernPaneDisplayTitle(state, pane), tiledPanes)
}

func renderModernSplitWorkbenchCompactActiveLine(state types.AppState, pane types.PaneState, tiledPanes int) string {
	return fmt.Sprintf("%s  •  %d tiled", renderModernPaneDisplayTitle(state, pane), tiledPanes)
}

func renderModernSingleWorkbenchSummaryLine(state types.AppState, pane types.PaneState) string {
	return fmt.Sprintf("Pane %s  •  %s", renderModernPaneDisplayTitle(state, pane), string(pane.ID))
}

func renderModernSingleWorkbenchCompactActiveLine(state types.AppState, pane types.PaneState) string {
	return renderModernPaneDisplayTitle(state, pane)
}

func renderModernSingleWorkbenchRoleSlotLine(state types.AppState, pane types.PaneState) string {
	return fmt.Sprintf("%s  •  %s", renderModernPaneRole(state, pane), pane.SlotState)
}

func renderModernSingleWorkbenchStateLine(state types.AppState, pane types.PaneState) string {
	return "runtime " + renderModernRuntimeLabel(state, pane)
}

func renderModernSingleWorkbenchTerminalLine(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return ""
	}
	return "terminal " + renderModernTerminalLabel(state, pane)
}

func renderModernSingleWorkbenchSessionLine(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		switch pane.SlotState {
		case types.PaneSlotWaiting:
			return "waiting for connect"
		case types.PaneSlotExited:
			return "history retained"
		default:
			return "no terminal connected"
		}
	}
	terminal, ok := state.Domain.Terminals[pane.TerminalID]
	if !ok {
		return "visible"
	}
	parts := []string{}
	if terminal.Visible {
		parts = append(parts, "visible")
	} else {
		parts = append(parts, "hidden")
	}
	if conn, ok := state.Domain.Connections[pane.TerminalID]; ok && len(conn.ConnectedPaneIDs) > 0 {
		parts = append(parts, renderModernCountLabel(len(conn.ConnectedPaneIDs), "pane"))
	}
	if len(parts) == 0 {
		return "visible"
	}
	return strings.Join(parts, "  •  ")
}

func renderModernSingleWorkbenchCommandLine(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return ""
	}
	terminal, ok := state.Domain.Terminals[pane.TerminalID]
	if !ok {
		return ""
	}
	if len(terminal.Command) > 0 {
		return "Command  " + strings.Join(terminal.Command, " ")
	}
	if tags := renderModernStringTags(terminal.Tags); tags != "" {
		return "Tags  " + tags
	}
	return ""
}

func renderModernSingleWorkbenchFocusLine(state types.AppState, pane types.PaneState, active bool) string {
	focus := "standby"
	if active {
		focus = "live"
	}
	return fmt.Sprintf("%s focus  •  %s", focus, renderModernPaneDisplayTitle(state, pane))
}

func renderModernSingleWorkbenchCompactFocusLine(state types.AppState, pane types.PaneState, active bool) string {
	focus := "idle"
	if active {
		focus = "live"
	}
	return fmt.Sprintf("%s  •  %s", focus, renderModernPaneDisplayTitle(state, pane))
}

func renderModernSingleWorkbenchLayerLine(state types.AppState) string {
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	overlay := string(state.UI.Overlay.Kind)
	if overlay == "" {
		overlay = string(types.OverlayNone)
	}
	return fmt.Sprintf("%s  •  mode %s  •  overlay %s", renderModernLayerLabel(renderModernPrimaryLayer(state)), mode, overlay)
}

func renderModernSingleWorkbenchCompactLayerLine(state types.AppState) string {
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	return fmt.Sprintf("%s  •  %s", renderModernLayerLabel(renderModernPrimaryLayer(state)), mode)
}

func renderModernSingleWorkbenchActionLine(_ types.AppState, pane types.PaneState) string {
	if pane.Kind == types.PaneKindFloating {
		return "Ctrl-p pane  •  Ctrl-o float  •  Ctrl-f picker"
	}
	return "Ctrl-p pane  •  Ctrl-f picker  •  Ctrl-g global"
}

func renderModernSingleWorkbenchCompactActionLine(_ types.AppState, pane types.PaneState) string {
	if pane.Kind == types.PaneKindFloating {
		return "Ctrl-o float  •  Ctrl-f"
	}
	return "Ctrl-p pane  •  Ctrl-f"
}

func renderModernSplitActionLine(state types.AppState) string {
	return fmt.Sprintf("Focus %s  •  Ctrl-p pane  •  Ctrl-f picker  •  Ctrl-g global", renderModernPrimaryLayer(state))
}

func renderModernSplitCompactActionLine(state types.AppState) string {
	return fmt.Sprintf("%s  •  Ctrl-p  •  Ctrl-f", renderModernPrimaryLayer(state))
}

func renderModernFloatingWorkbenchSummary(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	activePane, _, topPane, _ := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("Deck active %s  •  top %s  •  windows %d", activePane, topPane, len(floatingPaneIDs))
}

func renderModernFloatingWorkbenchCompactStackLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	_, _, _, topTitle := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("top %s  •  %d windows", topTitle, len(floatingPaneIDs))
}

func renderModernFloatingWorkbenchStateLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	_, _, topPane, topTitle := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("Top %s  •  pane %s  •  stack %d", topTitle, topPane, len(floatingPaneIDs))
}

func renderModernFloatingWorkbenchControlLine(state types.AppState) string {
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	return fmt.Sprintf("Layer %s  •  mode %s  •  Ctrl-o float", renderModernPrimaryLayer(state), mode)
}

func renderModernFloatingWorkbenchCompactLayerLine(state types.AppState) string {
	mode := state.UI.Mode.Active
	if mode == "" {
		mode = types.ModeNone
	}
	return fmt.Sprintf("%s  •  %s", renderModernPrimaryLayer(state), mode)
}

func renderModernFloatingWorkbenchTargets(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) (activePane, activeTitle, topPane, topTitle string) {
	activePane = string(tab.ActivePaneID)
	if activePane == "" && len(floatingPaneIDs) > 0 {
		activePane = string(floatingPaneIDs[0])
	}
	topPane = "<none>"
	activeTitle = activePane
	topTitle = topPane
	if len(floatingPaneIDs) > 0 {
		topPane = string(floatingPaneIDs[len(floatingPaneIDs)-1])
		topTitle = topPane
	}
	if pane, ok := tab.Panes[types.PaneID(activePane)]; ok {
		activeTitle = renderModernPaneDisplayTitle(state, pane)
	}
	if pane, ok := tab.Panes[types.PaneID(topPane)]; ok {
		topTitle = renderModernPaneDisplayTitle(state, pane)
	}
	return activePane, activeTitle, topPane, topTitle
}

func renderModernFloatingWorkbenchTitleLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	activePane, activeTitle, _, _ := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("Pane %s  •  floating  •  %s", activeTitle, activePane)
}

func renderModernFloatingWorkbenchCompactActiveLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	activePane, activeTitle, _, _ := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("%s  •  %s", activeTitle, activePane)
}

func renderModernFloatingWorkbenchFocusLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	activePane, _, _, _ := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	if pane, ok := tab.Panes[types.PaneID(activePane)]; ok {
		return renderModernPaneTitleBar(state, pane, true, 0, 0)
	}
	return ""
}

func renderModernFloatingWorkbenchCompactFocusLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	_, activeTitle, _, _ := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("live  •  %s", activeTitle)
}

func renderModernFloatingWorkbenchCompactActionLine(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) string {
	_, _, _, topTitle := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	return fmt.Sprintf("top %s  •  %d", topTitle, len(floatingPaneIDs))
}

func renderModernMixedWorkbenchCompactActiveLine(state types.AppState, pane types.PaneState) string {
	return fmt.Sprintf("%s  •  mixed", renderModernPaneDisplayTitle(state, pane))
}

func renderModernFloatingModeHint(state types.AppState) string {
	if state.UI.Mode.Active != types.ModeFloating {
		return ""
	}
	return "move j/k  •  size H/J/K/L  •  c center  •  x close  •  Esc exit"
}

func renderModernOverlayFooterLines(theme modernShellTheme, kind types.OverlayKind, width int) []string {
	var items []string
	switch kind {
	case types.OverlayHelp:
		items = []string{"Esc close"}
	case types.OverlayTerminalManager:
		items = []string{"Enter connect", "t new tab", "o floating", "Esc close"}
	case types.OverlayWorkspacePicker:
		items = []string{"Enter jump", "/ filter", "Esc close"}
	case types.OverlayTerminalPicker:
		items = []string{"Enter connect", "n create", "Esc close"}
	case types.OverlayLayoutResolve:
		items = []string{"Enter confirm", "Esc close"}
	case types.OverlayPrompt:
		items = []string{"Enter submit", "Tab next", "Esc cancel"}
	default:
		return nil
	}
	return renderModernOverlayMetaTokenLines(theme, width, items...)
}

func modernOverlayContentWidth(width int) int {
	if width < 64 {
		return max(18, width-4)
	}
	return max(18, width/2-4)
}

func renderModernOverlayBodyTokenLines(theme modernShellTheme, width int, items ...string) []string {
	return renderModernOverlayTokenLines(width, items, func(line string) string {
		return theme.modalBody.Render(line)
	})
}

func renderModernOverlayMetaTokenLines(theme modernShellTheme, width int, items ...string) []string {
	return renderModernOverlayTokenLines(width, items, func(line string) string {
		return theme.modalMeta.Render(line)
	})
}

func renderModernOverlayTokenLines(width int, items []string, render func(string) string) []string {
	width = max(12, width-4)
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return nil
	}
	rows := make([]string, 0, len(filtered))
	current := filtered[0]
	for _, item := range filtered[1:] {
		candidate := current + "  •  " + item
		if xansi.StringWidth(candidate) <= width {
			current = candidate
			continue
		}
		rows = append(rows, render(truncateModernLine(current, width)))
		current = item
	}
	rows = append(rows, render(truncateModernLine(current, width)))
	return rows
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
	contentWidth := max(1, width-2)
	notice := renderModernNotice(theme, notices)
	left := renderModernFooterShortcutsAdaptive(theme, state, pane, width)
	right := renderModernFooterContext(theme, state, pane, notice, width)
	return theme.footer.Render(fillANSIHorizontal(left, right, contentWidth))
}

func renderModernFooterContext(theme modernShellTheme, state types.AppState, pane types.PaneState, notice string, width int) string {
	items := []string{}
	if shouldRenderCompactChrome(width) {
		items = append(items,
			theme.activeChip.Render(renderModernPaneDisplayTitle(state, pane)),
			theme.chip.Render(renderModernPaneRole(state, pane)),
			theme.chip.Render(renderModernFooterLayerBadge(state)),
		)
	} else {
		items = append(items,
			renderModernLabeledChip(theme, "pane", renderModernPaneDisplayTitle(state, pane), true),
			renderModernLabeledChip(theme, "role", renderModernPaneRole(state, pane), false),
		)
		if pane.TerminalID != "" {
			items = append(items, renderModernLabeledChip(theme, "term", string(pane.TerminalID), false))
		}
	}
	if !shouldRenderCompactChrome(width) {
		items = append(items, theme.chip.Render(renderModernFooterSlotBadge(pane)))
	}
	if pane.SlotState == types.PaneSlotConnected {
		items = append(items, theme.chip.Render(renderModernFooterActivityBadge(state, pane)))
	}
	items = append(items, theme.chip.Render(renderModernFooterLayerBadge(state)))
	if notice != "" && !strings.Contains(xansi.Strip(notice), "ready") {
		items = append([]string{notice}, items...)
	}
	return strings.Join(items, " ")
}

func renderModernCountLabel(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func renderModernLegacyHeaderLeft(workspace types.WorkspaceState) string {
	parts := []string{"termx", fmt.Sprintf("[%s]", safeWorkspaceLabel(workspace))}
	for index, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := fmt.Sprintf("%d:%s", index+1, safeTabLabel(tab))
		if tabID == workspace.ActiveTabID {
			parts = append(parts, "["+label+"]")
			continue
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}

func renderModernHeaderBrand(theme modernShellTheme, workspace types.WorkspaceState) string {
	items := []string{
		theme.panelTitle.Render("termx"),
		theme.activeChip.Render(fmt.Sprintf("[%s]", safeWorkspaceLabel(workspace))),
	}
	for index, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := fmt.Sprintf("%d:%s", index+1, safeTabLabel(tab))
		if tabID == workspace.ActiveTabID {
			items = append(items, theme.activeTab.Render("["+label+"]"))
			continue
		}
		items = append(items, theme.tab.Render(label))
	}
	return strings.Join(items, " ")
}

func renderModernLegacyHeaderRight(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics) string {
	_ = workspace
	parts := []string{renderModernLabeledChip(theme, "pane", renderModernPaneDisplayTitle(state, pane), true)}
	parts = append(parts,
		renderModernLabeledChip(theme, "role", renderModernPaneRole(state, pane), false),
		renderModernLabeledChip(theme, "float", fmt.Sprintf("%d", len(orderedFloatingPaneIDs(tab))), false),
	)
	if renderModernFloatingPaneOffscreen(pane, metrics) {
		parts = append(parts, renderModernLabeledChip(theme, "recall", "offscreen", true))
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		parts = append(parts, renderModernLabeledChip(theme, "overlay", string(state.UI.Overlay.Kind), true))
	}
	if state.UI.Mode.Active != "" && state.UI.Mode.Active != types.ModeNone {
		parts = append(parts, renderModernLabeledChip(theme, "mode", string(state.UI.Mode.Active), false))
	}
	return strings.Join(parts, " ")
}

func renderModernHeaderRightAdaptive(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width int) string {
	if shouldRenderCompactChrome(width) {
		return renderModernHeaderRightCompact(theme, state, workspace, tab, pane, metrics)
	}
	return renderModernLegacyHeaderRight(theme, state, workspace, tab, pane, metrics)
}

func renderModernHeaderRightCompact(theme modernShellTheme, state types.AppState, _ types.WorkspaceState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics) string {
	parts := []string{renderModernLabeledChip(theme, "pane", truncateModernLine(renderModernPaneDisplayTitle(state, pane), 16), true)}
	parts = append(parts,
		theme.chip.Render(renderModernPaneRole(state, pane)),
		theme.chip.Render(fmt.Sprintf("f%d", len(orderedFloatingPaneIDs(tab)))),
	)
	if renderModernFloatingPaneOffscreen(pane, metrics) {
		parts = append(parts, theme.activeChip.Render("offscreen"))
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		parts = append(parts, renderModernLabeledChip(theme, "overlay", string(state.UI.Overlay.Kind), true))
	}
	if state.UI.Mode.Active != "" && state.UI.Mode.Active != types.ModeNone {
		parts = append(parts, renderModernLabeledChip(theme, "mode", string(state.UI.Mode.Active), false))
	}
	return strings.Join(parts, " ")
}

func renderModernTopStatusLine(state types.AppState, _ types.TabState, pane types.PaneState) string {
	parts := []string{
		"focus " + renderModernPaneDisplayTitle(state, pane),
		"role " + renderModernPaneRole(state, pane),
		"slot " + string(pane.SlotState),
	}
	if runtime := renderModernContextRuntimeLine(state, pane); runtime != "" {
		parts = append(parts, runtime)
	}
	return strings.Join(parts, "  ")
}

func renderModernTabStrip(theme modernShellTheme, workspace types.WorkspaceState) string {
	items := make([]string, 0, len(workspace.TabOrder))
	for index, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := fmt.Sprintf("%d:%s", index+1, renderModernTabLabel(tab))
		if tabID == workspace.ActiveTabID {
			items = append(items, theme.activeTab.Render(label))
			continue
		}
		items = append(items, theme.tab.Render(label))
	}
	if len(items) == 0 {
		return theme.tab.Render("no tabs")
	}
	return strings.Join(items, " ")
}

func renderModernTabStripAdaptive(theme modernShellTheme, state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, width int) string {
	if shouldRenderUltraCompactChrome(width) {
		items := []string{
			theme.activeTab.Render(renderModernActiveTabCompact(tab)),
			theme.chip.Render(renderModernTopStatusCompact(state, pane)),
		}
		return strings.Join(items, " ")
	}
	if shouldRenderCompactChrome(width) {
		return strings.Join([]string{
			theme.activeTab.Render(renderModernActiveTabCompact(tab)),
			theme.activeChip.Render(renderModernTopStatusCompact(state, pane)),
		}, " ")
	}
	return strings.Join([]string{
		renderModernTabStrip(theme, workspace),
		theme.activeChip.Render(renderModernTopStatusCompact(state, pane)),
	}, " ")
}

func renderModernActiveTabCompact(tab types.TabState) string {
	label := safeTabLabel(tab)
	paneCount := len(tab.Panes)
	switch paneCount {
	case 0:
		return fmt.Sprintf("%s • empty", label)
	case 1:
		return fmt.Sprintf("%s • 1 pane", label)
	default:
		return fmt.Sprintf("%s • %d panes", label, paneCount)
	}
}

func renderModernTopStatusCompact(state types.AppState, pane types.PaneState) string {
	label := renderModernPaneDisplayTitle(state, pane)
	if pane.TerminalID != "" {
		label = truncateModernLine(label, 14)
	}
	return fmt.Sprintf("%s  •  %s", label, renderModernPaneRole(state, pane))
}

func renderModernContextChromeLine(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics) string {
	if tab.ActiveLayer == types.FocusLayerFloating && len(tab.FloatingOrder) > 0 {
		return renderModernFloatingContextWide(theme, state, tab, pane, metrics)
	}
	items := []string{
		renderModernLabeledChip(theme, "state", renderModernRuntimeLabel(state, pane), true),
		renderModernLabeledChip(theme, "role", renderModernPaneRole(state, pane), false),
		renderModernLabeledChip(theme, "view", renderModernLayerLabel(renderModernPrimaryLayer(state)), false),
	}
	if pane.TerminalID != "" {
		items = append(items, renderModernLabeledChip(theme, "term", string(pane.TerminalID), false))
	}
	if state.UI.Mode.Active != "" && state.UI.Mode.Active != types.ModeNone {
		items = append(items, renderModernLabeledChip(theme, "mode", string(state.UI.Mode.Active), false))
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		items = append(items, renderModernLabeledChip(theme, "overlay", string(state.UI.Overlay.Kind), true))
	}
	if renderModernFloatingPaneOffscreen(pane, metrics) {
		items = append(items, renderModernLabeledChip(theme, "recall", "offscreen", true), renderModernLabeledChip(theme, "c", "center", true))
	}
	return strings.Join(items, " ")
}

func renderModernContextChromeLineAdaptive(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics, width int) string {
	if tab.ActiveLayer == types.FocusLayerFloating && len(tab.FloatingOrder) > 0 {
		if shouldRenderCompactChrome(width) {
			return renderModernFloatingContextCompact(theme, state, tab, pane, metrics)
		}
		return renderModernFloatingContextWide(theme, state, tab, pane, metrics)
	}
	if shouldRenderCompactChrome(width) {
		items := []string{
			theme.activeChip.Render(renderModernRuntimeLabel(state, pane)),
			theme.chip.Render(renderModernPaneRole(state, pane)),
			theme.chip.Render(renderModernLayerLabel(renderModernPrimaryLayer(state))),
		}
		if pane.TerminalID != "" {
			items = append(items, theme.chip.Render(string(pane.TerminalID)))
		}
		if state.UI.Mode.Active != "" && state.UI.Mode.Active != types.ModeNone {
			items = append(items, theme.chip.Render("mode "+string(state.UI.Mode.Active)))
		}
		if state.UI.Overlay.Kind != types.OverlayNone {
			items = append(items, theme.activeChip.Render("overlay "+string(state.UI.Overlay.Kind)))
		}
		if renderModernFloatingPaneOffscreen(pane, metrics) {
			items = append(items, theme.activeChip.Render("offscreen"), theme.activeChip.Render("c center"))
		}
		return strings.Join(items, " ")
	}
	return renderModernContextChromeLine(theme, state, tab, pane, metrics)
}

func renderModernFloatingContextWide(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics) string {
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	_, _, _, topTitle := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	items := []string{
		renderModernLabeledChip(theme, "state", renderModernRuntimeLabel(state, pane), true),
		renderModernLabeledChip(theme, "top", topTitle, false),
		renderModernLabeledChip(theme, "float", fmt.Sprintf("%d", len(floatingPaneIDs)), false),
	}
	if renderModernFloatingPaneOffscreen(pane, metrics) {
		items = append(items, renderModernLabeledChip(theme, "recall", "offscreen", true), renderModernLabeledChip(theme, "c", "center", true))
	}
	return strings.Join(items, " ")
}

func renderModernFloatingContextCompact(theme modernShellTheme, state types.AppState, tab types.TabState, pane types.PaneState, metrics wireframeMetrics) string {
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	_, _, _, topTitle := renderModernFloatingWorkbenchTargets(state, tab, floatingPaneIDs)
	items := []string{
		renderModernLabeledChip(theme, "state", renderModernRuntimeLabel(state, pane), true),
		renderModernLabeledChip(theme, "top", truncateModernLine(topTitle, 14), false),
		renderModernLabeledChip(theme, "float", fmt.Sprintf("%d", len(floatingPaneIDs)), false),
	}
	if renderModernFloatingPaneOffscreen(pane, metrics) {
		items = append(items, renderModernLabeledChip(theme, "recall", "offscreen", true))
	}
	return strings.Join(items, " ")
}

func renderModernLegacyFooterShortcuts(theme modernShellTheme, state types.AppState, pane types.PaneState) string {
	parts := renderShortcutParts(state, pane)
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, renderModernShortcutChip(theme, part, false))
	}
	return strings.Join(segments, "  ")
}

func renderModernFooterShortcutsAdaptive(theme modernShellTheme, state types.AppState, pane types.PaneState, width int) string {
	if shouldRenderCompactChrome(width) {
		return renderModernFooterShortcutsCompact(theme, state, pane)
	}
	return renderModernLegacyFooterShortcuts(theme, state, pane)
}

func renderModernFooterShortcutsCompact(theme modernShellTheme, state types.AppState, pane types.PaneState) string {
	parts := renderShortcutParts(state, pane)
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, renderModernShortcutChip(theme, part, true))
	}
	if len(segments) == 0 {
		return renderModernLegacyFooterShortcuts(theme, state, pane)
	}
	return strings.Join(segments, " ")
}

func renderModernLegacyShortcut(part string) string {
	switch part {
	case "Ctrl-g global":
		return "<g> GLOBAL"
	case "Ctrl-p pane":
		return "<p> PANE"
	case "Ctrl-t tab":
		return "<t> TAB"
	case "Ctrl-w ws":
		return "<w> WS"
	case "Ctrl-o float":
		return "<o> FLOAT"
	case "Ctrl-f pick":
		return "<f> PICK"
	case "Esc close":
		return "<esc> CLOSE"
	case "Esc exit":
		return "<esc> EXIT"
	case "Enter confirm":
		return "<enter> CONFIRM"
	case "Enter here":
		return "<enter> HERE"
	case "Enter submit":
		return "<enter> SUBMIT"
	case "h/l focus":
		return "<h/l> FOCUS"
	case "j/k move":
		return "<j/k> MOVE"
	case "H/J/K/L size":
		return "<H/J/K/L> SIZE"
	case "[/] z":
		return "<[/]> Z"
	case "c center":
		return "<c> CENTER"
	case "x close":
		return "<x> CLOSE"
	case "r restart":
		return "<r> RESTART"
	case "a connect":
		return "<a> CONNECT"
	case "m manager":
		return "<m> MANAGER"
	case "n new":
		return "<n> NEW"
	case "t new-tab":
		return "<t+> TAB"
	case "o float":
		return "<o> FLOAT"
	case "e edit":
		return "<e> EDIT"
	case "k stop":
		return "<k> STOP"
	case "? help":
		return "<?> HELP"
	default:
		return strings.ToUpper(part)
	}
}

func renderModernShortcutChip(theme modernShellTheme, part string, compact bool) string {
	label := renderModernLegacyShortcut(part)
	if compact {
		return theme.panelMeta.Render(renderModernCompactShortcut(part))
	}
	return theme.panelMeta.Render(label)
}

func renderModernCompactShortcut(part string) string {
	switch part {
	case "Ctrl-g global":
		return "<g> GLOB"
	case "Ctrl-p pane":
		return "<p> PANE"
	case "Ctrl-t tab":
		return "<t> TAB"
	case "Ctrl-w ws":
		return "<w> WS"
	case "Ctrl-o float":
		return "<o> FLT"
	case "Ctrl-f pick":
		return "<f> PICK"
	case "Esc close":
		return "<esc> CLOSE"
	case "Esc exit":
		return "<esc> EXIT"
	case "Enter confirm":
		return "<enter> OK"
	case "Enter here":
		return "<enter> HERE"
	case "Enter submit":
		return "<enter> SAVE"
	case "h/l focus":
		return "<h/l> FOCUS"
	case "j/k move":
		return "<j/k> MOVE"
	case "H/J/K/L size":
		return "<HJKL> SIZE"
	case "[/] z":
		return "<[/]> Z"
	case "c center":
		return "<c> CTR"
	case "x close":
		return "<x> CLOSE"
	case "r restart":
		return "<r> RESTART"
	case "a connect":
		return "<a> CONNECT"
	case "m manager":
		return "<m> MGR"
	case "n new":
		return "<n> NEW"
	case "t new-tab":
		return "<t+> TAB"
	case "o float":
		return "<o> FLT"
	case "e edit":
		return "<e> EDIT"
	case "k stop":
		return "<k> STOP"
	case "? help":
		return "<?> HELP"
	default:
		return renderModernLegacyShortcut(part)
	}
}

func renderModernLabeledChip(theme modernShellTheme, label, value string, active bool) string {
	content := strings.TrimSpace(label)
	if value != "" {
		content += " " + strings.TrimSpace(value)
	}
	if active {
		return theme.activeChip.Render(content)
	}
	return theme.chip.Render(content)
}

func renderModernFooterLayerBadge(state types.AppState) string {
	switch renderModernPrimaryLayer(state) {
	case types.FocusLayerFloating:
		return "◫ float deck"
	case types.FocusLayerOverlay:
		return "◌ dialog"
	default:
		return "▣ desk"
	}
}

func renderModernFooterSlotBadge(pane types.PaneState) string {
	switch pane.SlotState {
	case types.PaneSlotConnected:
		return "● connected"
	case types.PaneSlotWaiting:
		return "◌ waiting"
	case types.PaneSlotExited:
		return "○ exited"
	default:
		return "○ empty"
	}
}

func renderModernFooterActivityBadge(state types.AppState, pane types.PaneState) string {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return "idle"
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return "idle"
	}
	if tab.ActivePaneID == pane.ID {
		return "live"
	}
	return "standby"
}

func renderModernNotice(theme modernShellTheme, notices []btui.Notice) string {
	total := countVisibleNotices(notices)
	if total == 0 {
		return theme.noticeInfo.Render("● ready")
	}
	last, ok := lastVisibleNotice(notices)
	if !ok {
		return theme.noticeInfo.Render(fmt.Sprintf("● %d notices", total))
	}
	if total > 1 {
		switch last.Level {
		case btui.NoticeLevelError:
			return theme.noticeError.Render(fmt.Sprintf("! error %d notices", total))
		default:
			return theme.noticeInfo.Render(fmt.Sprintf("● info %d notices", total))
		}
	}
	label := last.Text
	if last.Count > 1 {
		label = fmt.Sprintf("%s (x%d)", label, last.Count)
	}
	switch last.Level {
	case btui.NoticeLevelError:
		return theme.noticeError.Render("! error " + label)
	default:
		return theme.noticeInfo.Render("● info " + label)
	}
}

func renderModernOverlayBackdropLine(state types.AppState) string {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return ""
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return ""
	}
	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		return ""
	}
	role := renderModernPaneRole(state, pane)
	return fmt.Sprintf("workbench paused  •  %s  •  %s", renderPaneTitle(state, pane), role)
}

func renderModernOverlayShadow(width int, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	lines := make([]string, 0, height)
	for row := 0; row < height; row++ {
		switch {
		case row == 0:
			lines = append(lines, " "+strings.Repeat("░", max(0, width-1)))
		case row == height-1:
			lines = append(lines, strings.Repeat("░", width))
		default:
			lines = append(lines, strings.Repeat(" ", max(0, width-2))+strings.Repeat("░", min(2, width)))
		}
	}
	return lines
}

func renderModernOverlayBackdropWash(lines []string) []string {
	out := make([]string, 0, len(lines))
	for y, line := range lines {
		runes := []rune(line)
		for x, r := range runes {
			if r == ' ' && (x+y)%2 == 0 {
				runes[x] = '▒'
			}
		}
		out = append(out, string(runes))
	}
	return out
}

func fillANSIHorizontal(left, right string, width int) string {
	if width <= 0 {
		return left + right
	}
	left = truncateModernLine(left, width)
	right = truncateModernLine(right, width)
	leftW := xansi.StringWidth(left)
	rightW := xansi.StringWidth(right)
	if leftW+1+rightW > width {
		available := max(2, width-1)
		rightBudget := min(rightW, max(12, available/3))
		leftBudget := available - rightBudget
		if leftBudget < 8 {
			leftBudget = min(available-1, max(1, available/2))
			rightBudget = max(1, available-leftBudget)
		}
		left = truncateModernLine(left, leftBudget)
		right = truncateModernLine(right, rightBudget)
		leftW = xansi.StringWidth(left)
		rightW = xansi.StringWidth(right)
	}
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

func renderModernPrimaryLayer(state types.AppState) types.FocusLayer {
	if state.UI.Overlay.Kind != types.OverlayNone && state.UI.Overlay.ReturnFocus.Layer != "" {
		return state.UI.Overlay.ReturnFocus.Layer
	}
	if state.UI.Focus.Layer != "" {
		return state.UI.Focus.Layer
	}
	return types.FocusLayerTiled
}

func renderModernBackdropContextLine(state types.AppState) string {
	workspaceID := state.Domain.ActiveWorkspaceID
	tabID := types.TabID("")
	if state.UI.Overlay.Kind != types.OverlayNone {
		if state.UI.Overlay.ReturnFocus.WorkspaceID != "" {
			workspaceID = state.UI.Overlay.ReturnFocus.WorkspaceID
		}
		if state.UI.Overlay.ReturnFocus.TabID != "" {
			tabID = state.UI.Overlay.ReturnFocus.TabID
		}
	}
	workspace, ok := state.Domain.Workspaces[workspaceID]
	if !ok {
		return ""
	}
	if tabID == "" {
		tabID = workspace.ActiveTabID
	}
	tab, ok := workspace.Tabs[tabID]
	if !ok {
		return ""
	}
	return fmt.Sprintf("workspace %s  •  tab %s  •  layer %s", safeWorkspaceLabel(workspace), safeTabLabel(tab), renderModernPrimaryLayer(state))
}

func renderModernPaneRole(state types.AppState, pane types.PaneState) string {
	role := renderScreenShellPaneCardRole(state, pane)
	if role == "" {
		return "unassigned"
	}
	return role
}

func renderModernTabLabel(tab types.TabState) string {
	label := safeTabLabel(tab)
	paneCount := len(tab.Panes)
	switch paneCount {
	case 0:
		return label + " • empty"
	case 1:
		return label + " • 1 pane"
	default:
		return fmt.Sprintf("%s • %d panes", label, paneCount)
	}
}

func renderModernPanePath(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) string {
	return fmt.Sprintf("%s / %s / %s / %s", safeWorkspaceLabel(workspace), safeTabLabel(tab), safePaneKind(pane.Kind), renderModernPanePathLabel(state, pane))
}

func renderModernPanePathAdaptive(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, width int) string {
	if width < 72 {
		return fmt.Sprintf("%s / %s / %s", safeWorkspaceLabel(workspace), safeTabLabel(tab), renderModernPanePathLabel(state, pane))
	}
	return renderModernPanePath(state, workspace, tab, pane)
}

func renderModernContextRuntimeLine(state types.AppState, pane types.PaneState) string {
	stateLabel := string(pane.SlotState)
	if pane.TerminalID != "" {
		stateLabel = "running"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			stateLabel = string(terminal.State)
		}
	}
	return fmt.Sprintf("state %s", stateLabel)
}

func renderModernRuntimeLabel(state types.AppState, pane types.PaneState) string {
	stateLabel := string(pane.SlotState)
	if pane.TerminalID != "" {
		stateLabel = "running"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			stateLabel = string(terminal.State)
		}
	}
	return stateLabel
}

func renderModernLayerLabel(layer types.FocusLayer) string {
	switch layer {
	case types.FocusLayerFloating:
		return "float"
	case types.FocusLayerOverlay:
		return "dialog"
	default:
		return "desk"
	}
}

func renderModernContextStateToken(state types.AppState, pane types.PaneState) string {
	role := renderModernPaneRole(state, pane)
	stateLabel := string(pane.SlotState)
	if pane.TerminalID != "" {
		stateLabel = "running"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			stateLabel = string(terminal.State)
		}
	}
	if role == "" || role == "unassigned" {
		return stateLabel
	}
	return role + " " + stateLabel
}

func renderModernWorkspaceCounts(workspace types.WorkspaceState) (tabs, panes, terminals, floating int) {
	tabs = len(workspace.TabOrder)
	terminalSet := map[types.TerminalID]struct{}{}
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
				terminalSet[pane.TerminalID] = struct{}{}
			}
		}
	}
	terminals = len(terminalSet)
	return tabs, panes, terminals, floating
}

func renderModernOverlayDialogWidth(metrics wireframeMetrics, width int) int {
	dialogWidth := min(metrics.OverlayWidth, width-4)
	if shouldRenderCompactChrome(width) {
		dialogWidth = width - 2
	}
	if dialogWidth < 40 {
		dialogWidth = min(width, 40)
	}
	if dialogWidth > width {
		dialogWidth = width
	}
	return dialogWidth
}

func shouldRenderCompactChrome(width int) bool {
	return width <= 80
}

func shouldRenderUltraCompactChrome(width int) bool {
	return width <= 68
}

func renderModernWorkbenchLocationLine(state types.AppState, pane types.PaneState) string {
	workspaceID := state.Domain.ActiveWorkspaceID
	workspace, ok := state.Domain.Workspaces[workspaceID]
	if !ok {
		return "path unavailable"
	}
	tabID := workspace.ActiveTabID
	tab, ok := workspace.Tabs[tabID]
	if !ok {
		return "path unavailable"
	}
	return renderModernPanePath(state, workspace, tab, pane)
}

func renderModernWorkbenchSignalLine(state types.AppState, pane types.PaneState) string {
	role := renderModernPaneRole(state, pane)
	parts := []string{
		"role " + role,
		"slot " + string(pane.SlotState),
		renderModernContextRuntimeLine(state, pane),
	}
	return strings.Join(parts, "  •  ")
}

func renderModernWorkbenchKeyLines(theme modernShellTheme, width int, pane types.PaneState) []string {
	items := []string{
		"Ctrl-p pane",
		"Ctrl-w workspace",
		"Ctrl-f picker",
		"Ctrl-g global",
		"? help",
	}
	if pane.Kind == types.PaneKindFloating {
		items = append(items, "Ctrl-o float")
	}
	return renderModernOverlayMetaTokenLines(theme, max(18, width-4), items...)
}

func renderModernBackdropPaneLine(state types.AppState, pane types.PaneState) string {
	parts := []string{
		"active pane " + renderModernPaneDisplayTitle(state, pane),
		renderModernPaneRole(state, pane),
		string(pane.SlotState),
	}
	if terminalLabel := renderModernTerminalLabel(state, pane); terminalLabel != "" {
		parts = append(parts, "term "+terminalLabel)
	}
	return strings.Join(parts, "  •  ")
}

func renderModernBackdropLocationLine(state types.AppState, pane types.PaneState) string {
	line := renderModernWorkbenchLocationLine(state, pane)
	return strings.TrimPrefix(line, "path ")
}

func renderModernBackdropPausedLine(state types.AppState) string {
	line := fmt.Sprintf("overlay %s  •  focus %s", state.UI.Overlay.Kind, state.UI.Focus.Layer)
	if returnFocus := renderWireframeReturnFocus(state.UI.Overlay.ReturnFocus); returnFocus != "" {
		line += "  •  return " + returnFocus
	}
	return line
}

func renderModernDetachedFloatingLabel(state types.AppState, pane types.PaneState) string {
	label := fmt.Sprintf("%s %s", renderModernPaneDisplayTitle(state, pane), pane.SlotState)
	if pane.SlotState == types.PaneSlotConnected {
		if preview := renderPanePreviewWithoutStoreFallback(state, pane); preview != "" {
			return label + " " + preview
		}
	}
	return label
}

// renderModernPaneDisplayTitle 统一 modern 主壳里 pane 的用户可见名称：
// 已连接时优先显示 terminal 真名；未连接/等待时显示人类状态名，避免首屏暴露技术 ID。
func renderModernPaneDisplayTitle(state types.AppState, pane types.PaneState) string {
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return "waiting pane"
	case types.PaneSlotEmpty:
		return "unconnected pane"
	}
	title := renderPaneTitle(state, pane)
	switch title {
	case "", string(pane.ID):
		if pane.SlotState == types.PaneSlotExited && pane.TerminalID == "" {
			return "exited pane"
		}
		if pane.SlotState == types.PaneSlotEmpty {
			return "unconnected pane"
		}
		if pane.SlotState == types.PaneSlotWaiting {
			return "waiting pane"
		}
	}
	switch title {
	case "waiting-pane":
		return "waiting pane"
	case "empty-pane":
		return "unconnected pane"
	case "exited-pane":
		return "exited pane"
	default:
		if title != "" {
			return title
		}
	}
	if pane.ID != "" {
		return string(pane.ID)
	}
	return "pane"
}

func renderModernTerminalLabel(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return ""
	}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.Name != "" {
		return terminal.Name
	}
	return string(pane.TerminalID)
}

func renderModernPanePathLabel(state types.AppState, pane types.PaneState) string {
	if pane.SlotState == types.PaneSlotConnected || pane.SlotState == types.PaneSlotExited {
		if label := renderModernPaneDisplayTitle(state, pane); label != "" {
			return label
		}
	}
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return "waiting"
	case types.PaneSlotEmpty:
		return "unconnected"
	case types.PaneSlotExited:
		return "exited"
	default:
		if pane.ID != "" {
			return string(pane.ID)
		}
		return "pane"
	}
}

func renderModernFloatingDeckTerminalSummary(state types.AppState, pane types.PaneState) (runtimeLine, commandLine string) {
	runtimeLine = "runtime " + string(pane.SlotState)
	role := renderModernPaneRole(state, pane)
	if pane.TerminalID == "" {
		if role != "" {
			runtimeLine += "  •  " + role
		}
		return runtimeLine, ""
	}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
		stateLabel := string(terminal.State)
		if stateLabel == "" {
			stateLabel = "running"
		}
		runtimeLine = "runtime " + stateLabel
		if terminal.Visible {
			runtimeLine += "  •  visible"
		} else {
			runtimeLine += "  •  hidden"
		}
		if len(terminal.Command) > 0 {
			commandLine = "cmd " + strings.Join(terminal.Command, " ")
		} else if tags := renderModernStringTags(terminal.Tags); tags != "" {
			commandLine = "tags " + tags
		}
	}
	return runtimeLine, commandLine
}

func renderPanePreviewWithoutStoreFallback(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return ""
	}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
		if len(terminal.Command) > 0 {
			return truncateModernLine(strings.Join(terminal.Command, " "), 12)
		}
	}
	return ""
}
