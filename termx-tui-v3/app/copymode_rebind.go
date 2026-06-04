package app

import (
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// NewCopyModeResizeRebindReducer 在 copy content rect 宽度变化后失效旧窗口并重新请求 latest。
// 它只处理 authoritative HistoryWindow 重新绑定，不读取 live surface 或本地 scrollback。
func NewCopyModeResizeRebindReducer(deps CopyModeDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		if !copyModeLayoutMayNeedRebind(msg) || !root.CopyMode.Active || deps.Core == nil {
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
			return root.Advance(), nil
		}
		terminalID := root.CopyMode.TerminalID
		if terminalID == "" {
			terminalID = root.Session.TerminalID
		}
		if terminalID == "" {
			return setCopyModeError(root, "copy mode requires attached terminal and cols"), nil
		}
		root.History = root.History.InvalidateWindow()
		root.CopyMode = root.CopyMode.Resize(rect.W, rect.H)
		return beginCopyModeLatestForCols(root, deps, terminalID, rect.W, rect.H)
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
