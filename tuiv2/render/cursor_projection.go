package render

import (
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func projectActiveEntryCursor(canvas *composedCanvas, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	canvas.clearCursor()
	canvas.syntheticCursorBlink = false
	target, ok := activeEntryCursorRenderTarget(entries, runtimeState)
	if !ok {
		return
	}
	// 中文说明：活动 pane 采用“双光标”策略：
	// 1. 宿主光标始终 hidden + positioned，用来给 IME/preedit 提供正确锚点；
	// 2. pane 内真正可见的光标由合成画布里的 synthetic cursor 承担，避免宿主侧
	//    的整行高亮/预编辑背景越过 pane 边界。
	canvas.setHiddenCursor(target.X, target.Y, target.Shape, target.Blink)
	if !target.Visible {
		return
	}
	drawSyntheticCursor(canvas, target.X, target.Y, protocol.CursorState{
		Visible: true,
		Shape:   target.Shape,
		Blink:   target.Blink,
	})
}

type cursorProjectionTarget struct {
	X     int
	Y     int
	Shape string
	Blink bool
}

type cursorRenderTarget struct {
	cursorProjectionTarget
	Visible bool
}

func activeEntryCursorRenderTarget(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) (cursorRenderTarget, bool) {
	lookup := newRuntimeLookup(runtimeState)
	for i, entry := range entries {
		if !entry.Active {
			continue
		}
		terminal := findVisibleTerminalWithLookup(lookup, entry.TerminalID)
		snapshot := entry.Snapshot
		surface := entry.Surface
		if snapshot == nil && surface == nil && terminal != nil {
			surface = terminal.Surface
		}
		if snapshot == nil && surface == nil && terminal != nil {
			snapshot = terminal.Snapshot
		}
		source := renderSource(snapshot, surface)
		if source == nil {
			return cursorRenderTarget{}, false
		}
		rect := contentRectForEntry(entry)
		if entry.CopyModeActive || entry.ScrollOffset > 0 {
			return cursorRenderTarget{}, false
		}
		target, ok := entryCursorRenderTarget(rect, source)
		if !ok {
			return cursorRenderTarget{}, false
		}
		if activeCursorOccluded(entries, i, target.cursorProjectionTarget) {
			return cursorRenderTarget{}, false
		}
		return target, true
	}
	return cursorRenderTarget{}, false
}

func activeEntryCursorTarget(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) (cursorProjectionTarget, bool) {
	target, ok := activeEntryCursorRenderTarget(entries, runtimeState)
	if !ok {
		return cursorProjectionTarget{}, false
	}
	return target.cursorProjectionTarget, true
}

func entryCursorRenderTarget(rect workbench.Rect, source terminalRenderSource) (cursorRenderTarget, bool) {
	snapshotTarget, snapshotOK := renderSourceCursorProjectionTarget(rect, source)
	fallbackTarget, fallbackOK := visualCursorProjectionTargetForSource(rect, source)
	cursor := protocol.CursorState{}
	if source != nil {
		cursor = source.Cursor()
	}
	switch {
	case snapshotOK && shouldPreferVisualCursorTargetForSource(source, snapshotTarget, fallbackTarget, fallbackOK):
		return cursorRenderTarget{
			cursorProjectionTarget: fallbackTarget,
			Visible:                cursor.Visible,
		}, true
	case snapshotOK:
		return cursorRenderTarget{
			cursorProjectionTarget: snapshotTarget,
			Visible:                cursor.Visible,
		}, true
	case fallbackOK:
		return cursorRenderTarget{
			cursorProjectionTarget: fallbackTarget,
			Visible:                cursor.Visible,
		}, true
	default:
		return cursorRenderTarget{}, false
	}
}

func snapshotCursorProjectionTarget(rect workbench.Rect, snapshot *protocol.Snapshot) (cursorProjectionTarget, bool) {
	if snapshot == nil {
		return cursorProjectionTarget{}, false
	}
	cursorX := rect.X + snapshot.Cursor.Col
	cursorY := rect.Y + snapshot.Cursor.Row
	if cursorX < rect.X || cursorY < rect.Y || cursorX >= rect.X+rect.W || cursorY >= rect.Y+rect.H {
		return cursorProjectionTarget{}, false
	}
	return cursorProjectionTarget{
		X:     cursorX,
		Y:     cursorY,
		Shape: snapshot.Cursor.Shape,
		Blink: snapshot.Cursor.Blink,
	}, true
}

func shouldPreferVisualCursorTarget(snapshot *protocol.Snapshot, snapshotTarget, visualTarget cursorProjectionTarget, visualOK bool) bool {
	if snapshot == nil || !visualOK {
		return false
	}
	if !snapshot.Cursor.Visible {
		return true
	}
	if !snapshotLikelyOwnsVisualCursor(snapshot) {
		return false
	}
	// 中文说明：Claude/Cloud Code 这类全屏 TUI 可能把真实终端 cursor 留在顶部，
	// 再在底部输入区自己画一个块光标。这里仅在“真实 cursor 还停在顶部，而视觉
	// 光标明确出现在更下方”时切换，避免误伤普通终端程序。
	return snapshot.Cursor.Row <= 1 && visualTarget.Y >= snapshotTarget.Y+2
}

func visualCursorProjectionTarget(rect workbench.Rect, snapshot *protocol.Snapshot) (cursorProjectionTarget, bool) {
	if snapshot == nil || !snapshotLikelyOwnsVisualCursor(snapshot) {
		return cursorProjectionTarget{}, false
	}
	rows := snapshot.Screen.Cells
	if len(rows) == 0 {
		return cursorProjectionTarget{}, false
	}
	startRow := maxInt(0, len(rows)/2)
	for row := len(rows) - 1; row >= startRow; row-- {
		cells := rows[row]
		for col := 0; col < len(cells) && col < rect.W; col++ {
			if !cellLooksLikeVisualCursor(cells, col) {
				continue
			}
			return cursorProjectionTarget{
				X:     rect.X + col,
				Y:     rect.Y + row,
				Shape: "block",
				Blink: false,
			}, true
		}
	}
	return cursorProjectionTarget{}, false
}

func snapshotLikelyOwnsVisualCursor(snapshot *protocol.Snapshot) bool {
	if snapshot == nil {
		return false
	}
	return snapshot.Screen.IsAlternateScreen ||
		snapshot.Modes.AlternateScreen ||
		snapshot.Modes.MouseTracking ||
		snapshot.Modes.BracketedPaste
}

func cellLooksLikeVisualCursor(row []protocol.Cell, col int) bool {
	if col < 0 || col >= len(row) {
		return false
	}
	cell := row[col]
	if cell.Content == "" && cell.Width == 0 {
		return false
	}
	if !styleLooksLikeVisualCursor(cell.Style) {
		return false
	}
	run := styledCellRunLength(row, col)
	return run >= 1 && run <= 2
}

func styleLooksLikeVisualCursor(style protocol.CellStyle) bool {
	if style.Reverse {
		return true
	}
	return (style.FG == "#000000" && style.BG == "#ffffff") ||
		(style.FG == "#ffffff" && style.BG == "#000000")
}

func styledCellRunLength(row []protocol.Cell, col int) int {
	if col < 0 || col >= len(row) {
		return 0
	}
	style := row[col].Style
	run := 1
	for i := col - 1; i >= 0 && sameCellStyle(row[i].Style, style); i-- {
		run++
	}
	for i := col + 1; i < len(row) && sameCellStyle(row[i].Style, style); i++ {
		run++
	}
	return run
}

func sameCellStyle(a, b protocol.CellStyle) bool {
	return a.FG == b.FG &&
		a.BG == b.BG &&
		a.Bold == b.Bold &&
		a.Italic == b.Italic &&
		a.Underline == b.Underline &&
		a.Blink == b.Blink &&
		a.Reverse == b.Reverse &&
		a.Strikethrough == b.Strikethrough
}

func activeCursorOccluded(entries []paneRenderEntry, activeIdx int, target cursorProjectionTarget) bool {
	if activeIdx < 0 || activeIdx >= len(entries) {
		return false
	}
	for i := activeIdx + 1; i < len(entries); i++ {
		entryRect := entries[i].Rect
		if target.X >= entryRect.X && target.X < entryRect.X+entryRect.W &&
			target.Y >= entryRect.Y && target.Y < entryRect.Y+entryRect.H {
			return true
		}
	}
	return false
}
