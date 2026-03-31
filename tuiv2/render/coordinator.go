package render

import (
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// Coordinator 负责 render invalidation / schedule / flush / ticker。
// 它通过 VisibleStateFn 拉取 workbench + runtime 的当前可见状态。
type Coordinator struct {
	visibleFn VisibleStateFn
}

type VisibleStateFn func() VisibleRenderState

func NewCoordinator(fn VisibleStateFn) *Coordinator {
	return &Coordinator{visibleFn: fn}
}

func (c *Coordinator) Invalidate()   {}
func (c *Coordinator) Schedule()     {}
func (c *Coordinator) FlushPending() {}
func (c *Coordinator) StartTicker()  {}

func (c *Coordinator) RenderFrame() string {
	if c == nil || c.visibleFn == nil {
		return ""
	}
	state := c.visibleFn()
	if state.Workbench == nil {
		return "tuiv2"
	}

	tabBar := renderTabBar(state)
	statusBar := renderStatusBar(state)
	bodyHeight := maxInt(1, state.TermSize.Height-2)
	body := renderBody(state, state.TermSize.Width, bodyHeight)

	if overlay := renderPromptOverlay(state.Prompt, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
	}
	if overlay := renderPickerOverlay(state.Picker, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
	}
	if overlay := renderWorkspacePickerOverlay(state.WorkspacePicker, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
	}
	if overlay := renderHelpOverlay(state.Help, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
	}
	return strings.Join([]string{tabBar, body, statusBar}, "\n")
}

func renderBody(state VisibleRenderState, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if state.Workbench == nil {
		return strings.Repeat("\n", maxInt(0, height-1))
	}

	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return strings.Repeat("\n", maxInt(0, height-1))
	}
	tab := state.Workbench.Tabs[activeTabIdx]

	canvas := newComposedCanvas(width, height)
	canvas.cursorOffsetY = 1 // account for the tab bar row above the body
	zoomedPaneID := tab.ZoomedPaneID
	for _, pane := range tab.Panes {
		rect := pane.Rect
		if zoomedPaneID != "" {
			if pane.ID != zoomedPaneID {
				continue
			}
			rect = workbench.Rect{X: 0, Y: 0, W: width, H: height}
		}
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		active := pane.ID == tab.ActivePaneID
		drawPaneFrame(canvas, rect, pane.Title, active)
		drawPaneContent(canvas, rect, pane, state.Runtime, tab.ScrollOffset, active)
	}
	for _, pane := range state.Workbench.FloatingPanes {
		rect := pane.Rect
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		active := pane.ID == tab.ActivePaneID
		drawPaneFrame(canvas, rect, pane.Title, active)
		drawPaneContent(canvas, rect, pane, state.Runtime, tab.ScrollOffset, active)
	}
	return canvas.String()
}

// drawPaneFrame draws the border box with a title label.
func drawPaneFrame(canvas *composedCanvas, rect workbench.Rect, title string, active bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	borderFG := "#d1d5db"
	titleFG := "#e5e7eb"
	if active {
		borderFG = "#4ade80"
		titleFG = "#f0fdf4"
	}
	borderStyle := drawStyle{FG: borderFG}
	titleStyle := drawStyle{FG: titleFG, Bold: true}

	// horizontal edges
	for x := rect.X; x < rect.X+rect.W; x++ {
		canvas.set(x, rect.Y, drawCell{Content: "─", Width: 1, Style: borderStyle})
		canvas.set(x, rect.Y+rect.H-1, drawCell{Content: "─", Width: 1, Style: borderStyle})
	}
	// vertical edges
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		canvas.set(rect.X, y, drawCell{Content: "│", Width: 1, Style: borderStyle})
		canvas.set(rect.X+rect.W-1, y, drawCell{Content: "│", Width: 1, Style: borderStyle})
	}
	// corners
	canvas.set(rect.X, rect.Y, drawCell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.set(rect.X+rect.W-1, rect.Y, drawCell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.set(rect.X, rect.Y+rect.H-1, drawCell{Content: "└", Width: 1, Style: borderStyle})
	canvas.set(rect.X+rect.W-1, rect.Y+rect.H-1, drawCell{Content: "┘", Width: 1, Style: borderStyle})

	// title
	if title != "" && rect.W > 4 {
		label := " " + title + " "
		innerW := rect.W - 4
		if len(label) > innerW {
			label = label[:innerW]
		}
		for i, ch := range label {
			canvas.set(rect.X+2+i, rect.Y, drawCell{Content: string(ch), Width: 1, Style: titleStyle})
		}
	}
}

// drawPaneContent fills the interior of a pane with terminal snapshot content.
func drawPaneContent(canvas *composedCanvas, rect workbench.Rect, pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy, scrollOffset int, active bool) {
	if rect.W < 3 || rect.H < 3 {
		return
	}
	contentRect := workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}

	if runtimeState == nil || pane.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, pane.TerminalID)
		return
	}

	for _, terminal := range runtimeState.Terminals {
		if terminal.TerminalID != pane.TerminalID {
			continue
		}
		if terminal.Snapshot == nil || len(terminal.Snapshot.Screen.Cells) == 0 {
			canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: "#94a3b8"})
			return
		}
		snapshot := applyScrollbackOffset(terminal.Snapshot, scrollOffset, contentRect.H)
		canvas.drawSnapshotInRect(contentRect, snapshot)
		if active {
			projectPaneCursor(canvas, contentRect, snapshot)
		}
		return
	}
	drawEmptyPaneContent(canvas, contentRect, pane.TerminalID)
}

func projectPaneCursor(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot) {
	if canvas == nil || snapshot == nil || !snapshot.Cursor.Visible {
		return
	}
	x := rect.X + snapshot.Cursor.Col
	y := rect.Y + snapshot.Cursor.Row
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return
	}
	canvas.setCursor(x, y)
}

func drawEmptyPaneContent(canvas *composedCanvas, rect workbench.Rect, terminalID string) {
	msg := "(unbound pane)"
	if terminalID != "" {
		msg = "terminal=" + terminalID
	}
	canvas.drawText(rect.X, rect.Y, msg, drawStyle{FG: "#64748b"})
}

func applyScrollbackOffset(snapshot *protocol.Snapshot, offset int, height int) *protocol.Snapshot {
	if snapshot == nil || offset <= 0 || height <= 0 {
		return snapshot
	}
	rows := make([][]protocol.Cell, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	rows = append(rows, snapshot.Scrollback...)
	rows = append(rows, snapshot.Screen.Cells...)
	if len(rows) == 0 {
		return snapshot
	}
	end := len(rows) - offset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	window := rows[start:end]
	cells := make([][]protocol.Cell, 0, len(window))
	for _, row := range window {
		cells = append(cells, append([]protocol.Cell(nil), row...))
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cells,
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	return &cloned
}
