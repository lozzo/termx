package app

import (
	"github.com/lozzow/termx/tui/render"
	"github.com/lozzow/termx/tui/state"
)

// NewCopyModeResizeRebindReducer 在 copy content rect 宽度变化后失效旧窗口并重新请求 latest。
// 进入 frozen copy mode 后，宽度变化只做本地重排；不再回 core 请求新投影。
func NewCopyModeResizeRebindReducer(deps CopyModeDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		if !copyModeLayoutMayNeedRebind(msg) || deps.Core == nil {
			return root, nil
		}
		root, activeViewID := rootWithActiveCopyHistorySession(root)
		if !root.CopyMode.Active {
			return root, nil
		}
		rect, ok := copyModeContentRect(root)
		if !ok {
			return root, nil
		}
		if rect.W == root.CopyMode.BoundCols {
			if rect.H == root.CopyMode.ViewRows {
				return root, nil
			}
			root.CopyMode = root.CopyMode.SetViewRows(rect.H)
			root.CopyMode = root.CopyMode.Scroll(0, len(root.History.Rows))
			root = root.Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		}
		if len(root.History.SourceLines) == 0 {
			root.History = root.History.EnsureSourceLines()
		}
		if len(root.History.SourceLines) == 0 {
			root.History = root.History.InvalidateWindow()
			root.CopyMode = root.CopyMode.Resize(rect.W, rect.H)
			root.CopyMode.BoundToken = ""
			root.CopyMode.ViewportTop = 0
			root.CopyMode.Cursor = state.CopyPosition{}
			root.CopyMode.Mark = nil
			root.CopyMode.Selection = nil
			root.CopyMode.Query = ""
			root.CopyMode.Matches = nil
			root.CopyMode.ActiveMatch = 0
			root.CopyMode.Empty = true
			root = root.Advance()
			return saveCopyHistorySessionForView(root, activeViewID), nil
		}
		root.CopyMode = root.CopyMode.Resize(rect.W, rect.H)
		beforeHistory := root.History
		root.History.Cols = rect.W
		root.History.Rows, root.History.Lines = state.ReflowHistoryLogicalLines(root.History.SourceLines, rect.W)
		root.CopyMode = root.CopyMode.RebindToReflowedHistory(beforeHistory, root.History)
		root.CopyMode = root.CopyMode.Scroll(0, len(root.History.Rows))
		root = root.Advance()
		return saveCopyHistorySessionForView(root, activeViewID), nil
	}
}

func copyModeLayoutMayNeedRebind(msg Msg) bool {
	switch msg.(type) {
	case HostResizeMsg,
		LiveResizeResultMsg,
		ShellSetPanelPresentationMsg,
		ShellTogglePanelPresentationMsg,
		ShellSetHeaderVisibleMsg,
		ShellToggleHeaderVisibleMsg,
		ShellSetFooterVisibleMsg,
		ShellToggleFooterVisibleMsg,
		ShellSplitActivePaneMsg,
		ShellPaneCommandMsg:
		return true
	default:
		return false
	}
}

func copyModeContentRect(root state.Root) (render.Rect, bool) {
	rect, ok := activeTerminalContentRect(root, render.Rect{})
	if ok {
		return rect, true
	}
	return render.Rect{}, false
}

func copyModeRowsHint(root state.Root) int {
	rect, ok := copyModeContentRect(root)
	if ok {
		return rect.H
	}
	return root.Session.Rows
}
