package app

import (
	"context"
	"fmt"

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
	TerminalID string
}

func (TerminalPoolAttachRequestMsg) isMsg() {}

type TerminalPoolAttachResultMsg struct {
	TerminalID string
	Result     services.TerminalAttachResult
	Err        error
}

func (TerminalPoolAttachResultMsg) isMsg() {}

type TerminalPoolCreateRequestMsg struct{}

func (TerminalPoolCreateRequestMsg) isMsg() {}

type TerminalPoolCreateResultMsg struct {
	Result services.TerminalCreateResult
	Err    error
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
	TerminalID string
}

func (TerminalPoolReconnectRequestMsg) isMsg() {}

type TerminalPoolReconnectResultMsg struct {
	TerminalID string
	Result     services.TerminalAttachResult
	Err        error
}

func (TerminalPoolReconnectResultMsg) isMsg() {}

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
			return reduceTerminalPoolAttachResult(root, msg)
		case TerminalPoolCreateRequestMsg:
			return reduceTerminalPoolCreateRequest(root, deps)
		case TerminalPoolCreateResultMsg:
			return reduceTerminalPoolCreateResult(root, msg)
		case TerminalPoolRestartRequestMsg:
			return reduceTerminalPoolRestartRequest(root, msg, deps)
		case TerminalPoolRestartResultMsg:
			return reduceTerminalPoolRestartResult(root, msg)
		case TerminalPoolReconnectRequestMsg:
			return reduceTerminalPoolReconnectRequest(root, msg, deps)
		case TerminalPoolReconnectResultMsg:
			return reduceTerminalPoolReconnectResult(root, msg)
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
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Attach(ctx, services.TerminalAttachRequest{
				TerminalID:   msg.TerminalID,
				Cols:         cols,
				Rows:         rows,
				Mode:         "collaborator",
				ResizePolicy: "owner",
			})
			return TerminalPoolAttachResultMsg{TerminalID: msg.TerminalID, Result: result, Err: err}
		},
	}}
}

func reduceTerminalPoolAttachResult(root state.Root, msg TerminalPoolAttachResultMsg) (state.Root, []Effect) {
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
	root.Session = root.Session.Attach(result.TerminalID, result.Channel, result.Cols, result.Rows)
	root.Surface.TerminalID = result.TerminalID
	root.Surface = root.Surface.Resize(result.Cols, result.Rows)
	root.Shell = root.Shell.CloseOverlay()
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.attach", Body: result.TerminalID})
	return root.Advance(), nil
}

func reduceTerminalPoolCreateRequest(root state.Root, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.new", Body: "terminal service missing"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSize(root)
	terminalID := nextTerminalPoolID(root)
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Create(ctx, services.TerminalCreateRequest{
				TerminalID: terminalID,
				Title:      terminalID,
				Cols:       cols,
				Rows:       rows,
			})
			return TerminalPoolCreateResultMsg{Result: result, Err: err}
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
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.new", Body: msg.Result.TerminalID})
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
		return TerminalPoolListRequestMsg{}
	}}}
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
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		result, err := deps.Terminal.Reconnect(ctx, services.TerminalReconnectRequest{TerminalID: msg.TerminalID, Cols: cols, Rows: rows, Mode: "collaborator", ResizePolicy: "owner"})
		return TerminalPoolReconnectResultMsg{TerminalID: msg.TerminalID, Result: result, Err: err}
	}}}
}

func reduceTerminalPoolReconnectResult(root state.Root, msg TerminalPoolReconnectResultMsg) (state.Root, []Effect) {
	return reduceTerminalPoolAttachResult(root, TerminalPoolAttachResultMsg{TerminalID: msg.TerminalID, Result: msg.Result, Err: msg.Err})
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
		}
	}
	return out
}

func terminalPoolAttachSize(root state.Root) (int, int) {
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
