package app

import (
	"context"

	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// NewTerminalLayoutResizeReducer 把最新 shell/layout state 投影成 active terminal content rect。
// 这里不直接调用 terminal service，而是通过 LiveResizeMsg 回到 live reducer，保持 service IO 不越过 message path。
func NewTerminalLayoutResizeReducer() Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		if !terminalLayoutMayNeedResize(msg) || !root.Session.Attached {
			return root, nil
		}
		rect, ok := activeTerminalContentRect(root, render.Rect{})
		desiredCols, desiredRows := root.Session.DesiredSize()
		if !ok || rect.W == desiredCols && rect.H == desiredRows {
			return root, nil
		}
		cols := rect.W
		rows := rect.H
		root.Session = root.Session.RequestResize(cols, rows)
		seq := root.Session.ResizeRequestSeq
		return root, []Effect{FuncEffect{
			Run: func(context.Context) Msg {
				return LiveResizeMsg{Cols: cols, Rows: rows, Seq: seq}
			},
		}}
	}
}

func terminalLayoutMayNeedResize(msg Msg) bool {
	switch msg.(type) {
	case HostResizeMsg,
		LiveAttachResultMsg,
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

func liveAttachContentSize(root state.Root, cfg LiveConfig) (int, int) {
	rect, ok := activeTerminalContentRect(root, render.Rect{W: cfg.Cols, H: cfg.Rows})
	if !ok {
		return cfg.Cols, cfg.Rows
	}
	return rect.W, rect.H
}

func activeTerminalContentRect(root state.Root, fallbackViewport render.Rect) (render.Rect, bool) {
	if !root.Viewport.Valid {
		if fallbackViewport.W <= 0 || fallbackViewport.H <= 0 {
			return render.Rect{}, false
		}
		root.Viewport = state.ViewportStore{Valid: true, Cols: fallbackViewport.W, Rows: fallbackViewport.H}
	}
	vm := render.NewRenderVMBuilder().Build(root)
	plan := render.MeasureLayout(vm.Shell, vm.Shell.Layout.Viewport)
	for _, panel := range plan.Panels {
		if panel.Panel.Active && panel.ContentRect.W > 0 && panel.ContentRect.H > 0 {
			return panel.ContentRect, true
		}
	}
	return render.Rect{}, false
}
