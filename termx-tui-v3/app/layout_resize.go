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
		if !terminalLayoutMayNeedResize(root, msg) || !root.Session.Attached {
			return root, nil
		}
		rect, ok := activeTerminalContentRect(root, render.Rect{})
		if !ok {
			return root, nil
		}
		binding, hasBinding := activeTerminalViewBinding(root)
		if hasBinding {
			nextViews, decision := root.TerminalViews.RequestViewResize(binding.ViewID, rect.W, rect.H)
			if !decision.Allowed || !decision.Changed {
				return root, nil
			}
			root.TerminalViews = nextViews
			root.Session = root.Session.RequestResize(rect.W, rect.H)
			return root, []Effect{FuncEffect{
				Run: func(context.Context) Msg {
					return LiveResizeMsg{Cols: rect.W, Rows: rect.H, Seq: decision.Seq, ViewID: binding.ViewID}
				},
			}}
		}
		desiredCols, desiredRows := root.Session.DesiredSize()
		if rect.W == desiredCols && rect.H == desiredRows {
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

func activeTerminalViewBinding(root state.Root) (state.TerminalViewBinding, bool) {
	shell := root.Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "" {
		if binding, ok := root.TerminalViews.FloatingBinding(shell.ActiveFloatingID); ok {
			return binding, true
		}
	}
	return root.TerminalViews.PaneBinding(shell.ActivePaneID)
}

func terminalLayoutMayNeedResize(root state.Root, msg Msg) bool {
	switch msg := msg.(type) {
	case HostResizeMsg,
		LiveAttachResultMsg,
		TerminalPoolAttachResultMsg,
		ShellSetPanelPresentationMsg,
		ShellTogglePanelPresentationMsg,
		ShellSetHeaderVisibleMsg,
		ShellToggleHeaderVisibleMsg,
		ShellSetFooterVisibleMsg,
		ShellToggleFooterVisibleMsg,
		ShellSplitActivePaneMsg,
		ShellPaneCommandMsg:
		return true
	case ShellFloatingCommandMsg:
		return floatingCommandMayResizeTerminal(root, msg.Command)
	default:
		return false
	}
}

func floatingCommandMayResizeTerminal(root state.Root, command state.FloatingCommand) bool {
	switch command.Action {
	case state.FloatingCommandCreate,
		state.FloatingCommandCenter,
		state.FloatingCommandToggleCollapse,
		state.FloatingCommandMove,
		state.FloatingCommandResize:
		return activeFloatingHasTerminal(root)
	default:
		return false
	}
}

func activeFloatingHasTerminal(root state.Root) bool {
	shell := root.Shell.EnsureDefaults()
	if shell.ActiveFloatingID == "" {
		return false
	}
	for _, floating := range shell.Floatings {
		if floating.ID == shell.ActiveFloatingID && floating.Pane.TerminalID != "" {
			return true
		}
	}
	return false
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
	if rect, ok := activeFloatingContentRectFromPlan(root, plan, true); ok {
		return rect, true
	}
	activePaneID := root.Shell.EnsureDefaults().ActivePaneID
	for _, panel := range plan.Panels {
		if panel.Panel.ID == activePaneID && panel.ContentRect.W > 0 && panel.ContentRect.H > 0 {
			return panel.ContentRect, true
		}
	}
	return render.Rect{}, false
}

func activeFloatingContentRect(root state.Root, fallbackViewport render.Rect, requireTerminal bool) (render.Rect, bool) {
	if !root.Viewport.Valid {
		if fallbackViewport.W <= 0 || fallbackViewport.H <= 0 {
			return render.Rect{}, false
		}
		root.Viewport = state.ViewportStore{Valid: true, Cols: fallbackViewport.W, Rows: fallbackViewport.H}
	}
	vm := render.NewRenderVMBuilder().Build(root)
	plan := render.MeasureLayout(vm.Shell, vm.Shell.Layout.Viewport)
	return activeFloatingContentRectFromPlan(root, plan, requireTerminal)
}

func activeFloatingContentRectFromPlan(root state.Root, plan render.LayoutPlan, requireTerminal bool) (render.Rect, bool) {
	shell := root.Shell.EnsureDefaults()
	activeFloatingID := shell.ActiveFloatingID
	if activeFloatingID == "" {
		return render.Rect{}, false
	}
	if requireTerminal {
		hasTerminal := false
		for _, floating := range shell.Floatings {
			if floating.ID == activeFloatingID && floating.Pane.TerminalID != "" {
				hasTerminal = true
				break
			}
		}
		if !hasTerminal {
			return render.Rect{}, false
		}
	}
	for _, layout := range plan.Floatings {
		if layout.Floating.ID == activeFloatingID && layout.ContentRect.W > 0 && layout.ContentRect.H > 0 {
			return layout.ContentRect, true
		}
	}
	return render.Rect{}, false
}
