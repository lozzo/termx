package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type TerminalPoolListRequestMsg struct{}

func (TerminalPoolListRequestMsg) isMsg() {}

type TerminalPoolListResultMsg struct {
	Seq    uint64
	Result services.TerminalListResult
	Err    error
}

func (TerminalPoolListResultMsg) isMsg() {}

type TerminalPoolAttachRequestMsg struct {
	TerminalID       string
	TargetFloatingID string
}

func (TerminalPoolAttachRequestMsg) isMsg() {}

type TerminalPoolAttachResultMsg struct {
	TerminalID       string
	TargetFloatingID string
	Result           services.TerminalAttachResult
	Err              error
}

func (TerminalPoolAttachResultMsg) isMsg() {}

type TerminalPoolCreateRequestMsg struct {
	Title            string
	Command          []string
	CWD              string
	Tags             map[string]string
	TargetFloatingID string
}

func (TerminalPoolCreateRequestMsg) isMsg() {}

type TerminalPoolCreateResultMsg struct {
	TargetFloatingID string
	Result           services.TerminalCreateResult
	Err              error
}

func (TerminalPoolCreateResultMsg) isMsg() {}

type TerminalPoolRestartRequestMsg struct {
	TerminalID string
}

func (TerminalPoolRestartRequestMsg) isMsg() {}

type TerminalPoolRestartResultMsg struct {
	TerminalID string
	Err        error
}

func (TerminalPoolRestartResultMsg) isMsg() {}

type TerminalPoolReconnectRequestMsg struct {
	TerminalID       string
	TargetFloatingID string
}

func (TerminalPoolReconnectRequestMsg) isMsg() {}

type TerminalPoolReconnectResultMsg struct {
	TerminalID       string
	TargetFloatingID string
	Result           services.TerminalAttachResult
	Err              error
}

func (TerminalPoolReconnectResultMsg) isMsg() {}

type TerminalPoolKillRequestMsg struct {
	TerminalID string
}

func (TerminalPoolKillRequestMsg) isMsg() {}

type TerminalPoolKillResultMsg struct {
	TerminalID string
	Err        error
}

func (TerminalPoolKillResultMsg) isMsg() {}

type TerminalPoolEditRequestMsg struct {
	TerminalID string
	Title      string
	Tags       map[string]string
}

func (TerminalPoolEditRequestMsg) isMsg() {}

type TerminalPoolEditResultMsg struct {
	TerminalID string
	Err        error
}

func (TerminalPoolEditResultMsg) isMsg() {}

func NewTerminalPoolReducer(deps LiveDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case TerminalPoolListRequestMsg:
			return reduceTerminalPoolListRequest(root, deps)
		case TerminalPoolListResultMsg:
			return reduceTerminalPoolListResult(root, msg)
		case TerminalPoolAttachRequestMsg:
			return reduceTerminalPoolAttachRequest(root, msg, deps)
		case TerminalPoolAttachResultMsg:
			return reduceTerminalPoolAttachResult(root, msg, deps)
		case TerminalPoolCreateRequestMsg:
			return reduceTerminalPoolCreateRequest(root, msg, deps)
		case TerminalPoolCreateResultMsg:
			return reduceTerminalPoolCreateResult(root, msg)
		case TerminalPoolRestartRequestMsg:
			return reduceTerminalPoolRestartRequest(root, msg, deps)
		case TerminalPoolRestartResultMsg:
			return reduceTerminalPoolRestartResult(root, msg)
		case TerminalPoolReconnectRequestMsg:
			return reduceTerminalPoolReconnectRequest(root, msg, deps)
		case TerminalPoolReconnectResultMsg:
			return reduceTerminalPoolReconnectResult(root, msg, deps)
		case TerminalPoolKillRequestMsg:
			return reduceTerminalPoolKillRequest(root, msg, deps)
		case TerminalPoolKillResultMsg:
			return reduceTerminalPoolKillResult(root, msg)
		case TerminalPoolEditRequestMsg:
			return reduceTerminalPoolEditRequest(root, msg, deps)
		case TerminalPoolEditResultMsg:
			return reduceTerminalPoolEditResult(root, msg)
		default:
			return root, nil
		}
	}
}

func reduceTerminalPoolListRequest(root state.Root, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		root.TerminalPool, _ = root.TerminalPool.ApplyList(root.TerminalPool.RequestSeq, nil, "terminal service missing")
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.pool", Body: "terminal service missing"})
		return root.Advance(), nil
	}
	root.TerminalPool = root.TerminalPool.RequestList()
	seq := root.TerminalPool.RequestSeq
	return root.Advance(), []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.List(ctx, services.TerminalListRequest{})
			return TerminalPoolListResultMsg{Seq: seq, Result: result, Err: err}
		},
	}}
}

func reduceTerminalPoolListResult(root state.Root, msg TerminalPoolListResultMsg) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	next, applied := root.TerminalPool.ApplyList(msg.Seq, terminalPoolItemsFromService(msg.Result.Items), errText)
	if !applied {
		return root, nil
	}
	root.TerminalPool = next
	if errText != "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.pool", Body: errText})
	}
	return root.Advance(), nil
}

func reduceTerminalPoolAttachRequest(root state.Root, msg TerminalPoolAttachRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: "missing terminal"})
		return root.Advance(), nil
	}
	if deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: "terminal service missing"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSize(root)
	targetFloatingID := msg.TargetFloatingID
	if targetFloatingID == "" {
		targetFloatingID = root.Shell.EnsureDefaults().ActiveFloatingID
	}
	viewID := state.TerminalPaneViewID(root.Shell.EnsureDefaults().ActivePaneID)
	if targetFloatingID != "" {
		viewID = state.TerminalFloatingViewID(targetFloatingID)
	}
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Attach(ctx, services.TerminalAttachRequest{
				TerminalID:   msg.TerminalID,
				Cols:         cols,
				Rows:         rows,
				Mode:         "collaborator",
				ResizePolicy: "owner",
				SurfaceID:    "termx-tui-v3",
				ViewID:       viewID,
			})
			return TerminalPoolAttachResultMsg{TerminalID: msg.TerminalID, TargetFloatingID: targetFloatingID, Result: result, Err: err}
		},
	}}
}

func reduceTerminalPoolAttachResult(root state.Root, msg TerminalPoolAttachResultMsg, deps LiveDeps) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	root.TerminalPool = root.TerminalPool.ApplyAttached(msg.TerminalID, errText)
	if errText != "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: errText})
		return root.Advance(), nil
	}
	result := msg.Result
	if result.TerminalID == "" {
		result.TerminalID = msg.TerminalID
	}
	root.Session = root.Session.AttachWithResizeOwner(result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, result.ViewID)
	root.Surface = root.Surface.Attach(result.TerminalID, result.Cols, result.Rows)
	if msg.TargetFloatingID != "" {
		paneID := ""
		for _, floating := range root.Shell.Floatings {
			if floating.ID == msg.TargetFloatingID {
				paneID = floating.Pane.ID
				break
			}
		}
		root.TerminalViews = root.TerminalViews.BindFloating(state.NewFloatingTerminalView(msg.TargetFloatingID, paneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, result.ViewID, result.CanResize))
		root.Shell = root.Shell.BindFloatingTerminal(msg.TargetFloatingID, result.TerminalID)
	} else {
		activePaneID := root.Shell.EnsureDefaults().ActivePaneID
		root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(activePaneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, result.ViewID, result.CanResize))
		root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: activePaneID}, result.TerminalID)
	}
	root.Shell = root.Shell.CloseOverlay()
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.attach", Body: result.TerminalID})
	return root.Advance(), liveEffects(result.TerminalID, result.Cols, result.Rows, deps)
}

func reduceTerminalPoolCreateRequest(root state.Root, msg TerminalPoolCreateRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.new", Body: "terminal service missing"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSize(root)
	terminalID := nextTerminalPoolID(root)
	title := strings.TrimSpace(msg.Title)
	if title == "" {
		title = terminalID
	}
	command := append([]string(nil), msg.Command...)
	if len(command) == 0 {
		command = services.DefaultTerminalCommand()
	}
	cwd := strings.TrimSpace(msg.CWD)
	tags := cloneStringMap(msg.Tags)
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Create(ctx, services.TerminalCreateRequest{
				TerminalID: terminalID,
				Title:      title,
				Command:    command,
				CWD:        cwd,
				Tags:       tags,
				Cols:       cols,
				Rows:       rows,
			})
			return TerminalPoolCreateResultMsg{TargetFloatingID: msg.TargetFloatingID, Result: result, Err: err}
		},
	}}
}

func reduceTerminalPoolCreateResult(root state.Root, msg TerminalPoolCreateResultMsg) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	root.TerminalPool = root.TerminalPool.ApplyCreated(msg.Result.TerminalID, errText)
	if errText != "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.new", Body: errText})
		return root.Advance(), nil
	}
	if msg.Result.TerminalID == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.new", Body: "missing terminal"})
		return root.Advance(), nil
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.new", Body: msg.Result.TerminalID})
	effects := []Effect{
		FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }},
		FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolAttachRequestMsg{TerminalID: msg.Result.TerminalID, TargetFloatingID: msg.TargetFloatingID}
		}},
	}
	return root.Advance(), effects
}

func reduceTerminalPoolRestartRequest(root state.Root, msg TerminalPoolRestartRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" || deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.restart", Body: "terminal unavailable"})
		return root.Advance(), nil
	}
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		err := deps.Terminal.Restart(ctx, services.TerminalRestartRequest{TerminalID: msg.TerminalID})
		return TerminalPoolRestartResultMsg{TerminalID: msg.TerminalID, Err: err}
	}}}
}

func reduceTerminalPoolRestartResult(root state.Root, msg TerminalPoolRestartResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.restart", Body: msg.Err.Error()})
		return root.Advance(), nil
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.restart", Body: msg.TerminalID})
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
}

func reduceTerminalPoolReconnectRequest(root state.Root, msg TerminalPoolReconnectRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" || deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.reconnect", Body: "terminal unavailable"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSize(root)
	targetFloatingID := msg.TargetFloatingID
	if targetFloatingID == "" {
		targetFloatingID = root.Shell.EnsureDefaults().ActiveFloatingID
	}
	viewID := state.TerminalPaneViewID(root.Shell.EnsureDefaults().ActivePaneID)
	if targetFloatingID != "" {
		viewID = state.TerminalFloatingViewID(targetFloatingID)
	}
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		result, err := deps.Terminal.Reconnect(ctx, services.TerminalReconnectRequest{TerminalID: msg.TerminalID, Cols: cols, Rows: rows, Mode: "collaborator", ResizePolicy: "owner", SurfaceID: "termx-tui-v3", ViewID: viewID})
		return TerminalPoolReconnectResultMsg{TerminalID: msg.TerminalID, TargetFloatingID: targetFloatingID, Result: result, Err: err}
	}}}
}

func reduceTerminalPoolReconnectResult(root state.Root, msg TerminalPoolReconnectResultMsg, deps LiveDeps) (state.Root, []Effect) {
	return reduceTerminalPoolAttachResult(root, TerminalPoolAttachResultMsg{TerminalID: msg.TerminalID, TargetFloatingID: msg.TargetFloatingID, Result: msg.Result, Err: msg.Err}, deps)
}

func reduceTerminalPoolKillRequest(root state.Root, msg TerminalPoolKillRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" || deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.kill", Body: "terminal unavailable"})
		return root.Advance(), nil
	}
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		err := deps.Terminal.Kill(ctx, services.TerminalKillRequest{TerminalID: msg.TerminalID})
		return TerminalPoolKillResultMsg{TerminalID: msg.TerminalID, Err: err}
	}}}
}

func reduceTerminalPoolKillResult(root state.Root, msg TerminalPoolKillResultMsg) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	root.TerminalPool = root.TerminalPool.ApplyKilled(msg.TerminalID, errText)
	if errText != "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.kill", Body: errText})
		return root.Advance(), nil
	}
	root.TerminalViews = root.TerminalViews.RemoveTerminal(msg.TerminalID)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.kill", Body: msg.TerminalID})
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
}

func reduceTerminalPoolEditRequest(root state.Root, msg TerminalPoolEditRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" || deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.edit", Body: "terminal unavailable"})
		return root.Advance(), nil
	}
	title := msg.Title
	if title == "" {
		title = msg.TerminalID
	}
	tags := cloneStringMap(msg.Tags)
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		err := deps.Terminal.EditMetadata(ctx, services.TerminalEditMetadataRequest{TerminalID: msg.TerminalID, Title: title, Tags: tags})
		return TerminalPoolEditResultMsg{TerminalID: msg.TerminalID, Err: err}
	}}}
}

func reduceTerminalPoolEditResult(root state.Root, msg TerminalPoolEditResultMsg) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	root.TerminalPool = root.TerminalPool.ApplyEdited(msg.TerminalID, errText)
	if errText != "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.edit", Body: errText})
		return root.Advance(), nil
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "pool.edit", Body: msg.TerminalID})
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
}

func terminalPoolItemsFromService(items []services.TerminalPoolItem) []state.TerminalPoolItem {
	out := make([]state.TerminalPoolItem, len(items))
	for i, item := range items {
		out[i] = state.TerminalPoolItem{
			TerminalID: item.TerminalID,
			Title:      item.Title,
			State:      item.State,
			CWD:        item.CWD,
			Tags:       cloneStringMap(item.Tags),
			Cols:       item.Cols,
			Rows:       item.Rows,
		}
	}
	return out
}

func terminalPoolAttachSize(root state.Root) (int, int) {
	// active floating 尚未绑定 terminal 时也要用自己的内容区尺寸发起 attach。
	if rect, ok := activeFloatingContentRect(root, render.Rect{}, false); ok {
		return rect.W, rect.H
	}
	cols, rows := liveAttachContentSize(root, LiveConfig{Cols: root.Session.Cols, Rows: root.Session.Rows})
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return cols, rows
}

func nextTerminalPoolID(root state.Root) string {
	used := map[string]struct{}{}
	for _, item := range root.TerminalPool.Items {
		used[item.TerminalID] = struct{}{}
	}
	if root.Session.TerminalID != "" {
		used[root.Session.TerminalID] = struct{}{}
	}
	for i := len(used) + 1; ; i++ {
		id := fmt.Sprintf("term-pool-%d", i)
		if _, ok := used[id]; !ok {
			return id
		}
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
