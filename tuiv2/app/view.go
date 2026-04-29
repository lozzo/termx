package app

import "github.com/lozzow/termx/termx-core/perftrace"

func (m *Model) View() string {
	finish := perftrace.Measure("app.view")
	viewBytes := 0
	defer func() {
		finish(viewBytes)
		if m != nil && m.perfProfile != nil {
			m.perfProfile.SetContext(m.currentPerfProfileContext(viewBytes))
			m.perfProfile.Sample("view")
		}
	}()
	if m == nil || m.render == nil {
		return ""
	}
	if m.visibleAltScreenGeometryChanged() {
		m.forceFullRedraw()
	}
	m.reconcileCopyModeContext()
	if rowsWriter, ok := m.frameOut.(frameLinesWriter); ok {
		if directWriter, ok := rowsWriter.(*outputCursorWriter); ok {
			mode, _ := m.verticalScrollOptimizationMode()
			directWriter.SetVerticalScrollMode(mode)
			directWriter.SetOwnerAwareDeltaEnabled(true)
			directWriter.SetForceFullFrameLines(false)
			if lines, cursor, ok := m.render.CachedFrameLinesAndCursorRef(); ok {
				viewBytes = joinedLinesLen(lines) + len(cursor)
				m.lastViewFrame = ""
				m.lastViewCursor = cursor
				return ""
			}
			result := m.render.Render()
			cursor := result.CursorSequence()
			viewBytes = joinedLinesLen(result.Lines) + len(cursor)
			_ = directWriter.WriteFrameLinesWithMeta(result.Lines, cursor, presentMetaFromRender(result.Meta))
			m.lastViewFrame = ""
			m.lastViewCursor = cursor
			return ""
		}
		if lines, cursor, ok := m.render.CachedFrameLinesAndCursorRef(); ok {
			viewBytes = joinedLinesLen(lines) + len(cursor)
			m.lastViewFrame = ""
			m.lastViewCursor = cursor
			return ""
		}
		lines, cursor := m.render.RenderFrameLinesRef()
		viewBytes = joinedLinesLen(lines) + len(cursor)
		_ = rowsWriter.WriteFrameLines(lines, cursor)
		m.lastViewFrame = ""
		m.lastViewCursor = cursor
		return ""
	}
	if frame, cursor, ok := m.render.CachedFrameAndCursor(); ok {
		perftrace.Count("app.view.reuse", len(frame)+len(cursor))
		viewBytes = len(frame) + len(cursor)
		if m.frameOut != nil {
			m.lastViewFrame = frame
			m.lastViewCursor = cursor
			return ""
		}
		if m.cursorOut != nil {
			m.cursorOut.SetCursorSequence(cursor)
		}
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return frame + cursor
	}
	frame := m.render.RenderFrame()
	cursor := m.render.CursorSequence()
	viewBytes = len(frame) + len(cursor)
	if m.frameOut != nil {
		_ = m.frameOut.WriteFrame(frame, cursor)
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return ""
	}
	if m.cursorOut != nil {
		// 中文说明：Bubble Tea 的 standardRenderer.flush() 在渲染完所有行后，
		// 总会追加 MoveCursor(linesRendered, 0) 把终端光标移到屏幕左下角。
		// 仅靠 View() 内嵌 cursor CUP 无法最终生效，因为 BT 的尾部 MoveCursor
		// 会覆盖它。
		//
		// 双路策略：
		// 1. View() 仍然返回 frame+cursor，确保 cursor 变化时 BT 能检测到
		//    字符串差异并触发 flush（否则仅 cursor 移动但 frame 不变时 BT 会
		//    跳过输出）。
		// 2. 同时把 cursor 设到 outputCursorWriter 上，让它在 BT 整段输出
		//   （含尾部 MoveCursor）之后再追加一次 cursor CUP。
		//    这样终端光标最终停留在 pane 内的正确输入位置，输入法锚点就不会跑偏。
		m.cursorOut.SetCursorSequence(cursor)
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return frame + cursor
	}
	m.lastViewFrame = frame
	m.lastViewCursor = cursor
	return frame + cursor
}
