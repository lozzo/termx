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
	var lookup runtimeLookup
	lookupReady := false
	for i, entry := range entries {
		if !entry.Active {
			continue
		}
		snapshot := entry.Snapshot
		surface := entry.Surface
		if snapshot == nil && surface == nil && runtimeState != nil {
			if !lookupReady {
				lookup = newRuntimeLookup(runtimeState)
				lookupReady = true
			}
			if terminal := findVisibleTerminalWithLookup(lookup, entry.TerminalID); terminal != nil {
				surface = terminal.Surface
				if surface == nil {
					snapshot = terminal.Snapshot
				}
			}
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
