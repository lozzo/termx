package compositor

import (
	"strings"

	"github.com/lozzow/termx/tui/domain/types"
	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/surface"
)

type Pane struct {
	ID      types.PaneID
	Rect    types.Rect
	Active  bool
	Surface surface.Pane
}

type View struct {
	Width  int
	Height int
	Panes  []Pane
}

// ComposeWorkbench 只负责把已经准备好的 pane surface 平铺到画布里，
// 不承担 slot/runtime 推导，这样后续 floating/overlay 也能独立扩展。
func ComposeWorkbench(view View) *canvas.Canvas {
	workbench := canvas.New(max(0, view.Width), max(0, view.Height))
	for _, pane := range view.Panes {
		drawPane(workbench, pane)
	}
	return workbench
}

func drawPane(workbench *canvas.Canvas, pane Pane) {
	rect := pane.Rect
	if rect.W <= 1 || rect.H <= 1 {
		return
	}

	hBorder := "-"
	vBorder := "|"
	if pane.Active {
		hBorder = "="
		vBorder = "#"
	}

	for x := rect.X; x < rect.X+rect.W; x++ {
		workbench.Set(x, rect.Y, canvas.Cell{Content: hBorder, Width: 1})
		workbench.Set(x, rect.Y+rect.H-1, canvas.Cell{Content: hBorder, Width: 1})
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		workbench.Set(rect.X, y, canvas.Cell{Content: vBorder, Width: 1})
		workbench.Set(rect.X+rect.W-1, y, canvas.Cell{Content: vBorder, Width: 1})
	}
	workbench.Set(rect.X, rect.Y, canvas.Cell{Content: "+", Width: 1})
	workbench.Set(rect.X+rect.W-1, rect.Y, canvas.Cell{Content: "+", Width: 1})
	workbench.Set(rect.X, rect.Y+rect.H-1, canvas.Cell{Content: "+", Width: 1})
	workbench.Set(rect.X+rect.W-1, rect.Y+rect.H-1, canvas.Cell{Content: "+", Width: 1})

	title := strings.TrimSpace(pane.Surface.Title)
	if title == "" {
		title = string(pane.ID)
	}
	if pane.Active {
		title = "* " + title
	}
	workbench.DrawText(rect, rect.X+2, rect.Y, clipLine(title, rect.W-4), canvas.DrawStyle{})

	bodyRect := types.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	for row, line := range pane.Surface.Body {
		if row >= bodyRect.H {
			break
		}
		workbench.DrawText(bodyRect, bodyRect.X, bodyRect.Y+row, clipLine(line, bodyRect.W), canvas.DrawStyle{})
	}

	if !pane.Surface.Cursor.Visible {
		return
	}
	cursorX := bodyRect.X + pane.Surface.Cursor.Col
	cursorY := bodyRect.Y + pane.Surface.Cursor.Row
	if cursorX < bodyRect.X || cursorX >= bodyRect.X+bodyRect.W || cursorY < bodyRect.Y || cursorY >= bodyRect.Y+bodyRect.H {
		return
	}
	workbench.Set(cursorX, cursorY, canvas.Cell{Content: "█", Width: 1})
}

func clipLine(line string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= limit {
		return line
	}
	return string(runes[:limit])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
