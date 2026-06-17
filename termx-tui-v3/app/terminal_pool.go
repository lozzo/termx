package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-shared/terminalmeta"
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
	TargetPaneID     string
	TargetFloatingID string
	ResizePolicy     string
}

func (TerminalPoolAttachRequestMsg) isMsg() {}

type TerminalPoolAttachResultMsg struct {
	TerminalID       string
	TargetPaneID     string
	TargetFloatingID string
	ResizePolicy     string
	Result           services.TerminalAttachResult
	Err              error
}

func (TerminalPoolAttachResultMsg) isMsg() {}

type TerminalPoolCreateRequestMsg struct {
	Title            string
	Command          []string
	CWD              string
	Tags             map[string]string
	TargetPaneID     string
	TargetFloatingID string
}

func (TerminalPoolCreateRequestMsg) isMsg() {}

type TerminalPoolCreateResultMsg struct {
	TargetPaneID     string
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
	TargetPaneID     string
	TargetFloatingID string
}

func (TerminalPoolReconnectRequestMsg) isMsg() {}

type TerminalPoolReconnectResultMsg struct {
	TerminalID       string
	TargetPaneID     string
	TargetFloatingID string
	ResizePolicy     string
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

type TerminalPoolRemoveRequestMsg struct {
	TerminalID string
}

func (TerminalPoolRemoveRequestMsg) isMsg() {}

type TerminalPoolRemoveResultMsg struct {
	TerminalID string
	Err        error
}

func (TerminalPoolRemoveResultMsg) isMsg() {}

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

type TerminalSizeLockToggleRequestMsg struct{}

func (TerminalSizeLockToggleRequestMsg) isMsg() {}

type TerminalSizeLockToggleResultMsg struct {
	TerminalID string
	Tags       map[string]string
	Locked     bool
	Err        error
}

func (TerminalSizeLockToggleResultMsg) isMsg() {}

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
		case TerminalPoolRemoveRequestMsg:
			return reduceTerminalPoolRemoveRequest(root, msg, deps)
		case TerminalPoolRemoveResultMsg:
			return reduceTerminalPoolRemoveResult(root, msg)
		case TerminalPoolEditRequestMsg:
			return reduceTerminalPoolEditRequest(root, msg, deps)
		case TerminalPoolEditResultMsg:
			return reduceTerminalPoolEditResult(root, msg)
		case TerminalSizeLockToggleRequestMsg:
			return reduceTerminalSizeLockToggleRequest(root, deps)
		case TerminalSizeLockToggleResultMsg:
			return reduceTerminalSizeLockToggleResult(root, msg)
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
	root.Surface = projectTerminalPoolExitMetadata(root.Surface, root.TerminalPool.Items)
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
	target, ok := terminalPoolTargetFromRequest(root, msg.TargetPaneID, msg.TargetFloatingID)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: "missing target panel"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSizeForTarget(root, target)
	resizePolicy := msg.ResizePolicy
	if resizePolicy == "" {
		resizePolicy = state.TerminalResizeRoleFollower
	}
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Attach(ctx, services.TerminalAttachRequest{
				TerminalID:   msg.TerminalID,
				Cols:         cols,
				Rows:         rows,
				Mode:         "collaborator",
				ResizePolicy: resizePolicy,
				SurfaceID:    "termx-tui-v3",
				ViewID:       target.ViewID,
			})
			return TerminalPoolAttachResultMsg{TerminalID: msg.TerminalID, TargetPaneID: target.PaneID, TargetFloatingID: target.FloatingID, ResizePolicy: resizePolicy, Result: result, Err: err}
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
	target, _ := terminalPoolTargetFromRequest(root, msg.TargetPaneID, msg.TargetFloatingID)
	if result.ViewID == "" {
		result.ViewID = target.ViewID
	}
	if result.SurfaceID == "" {
		result.SurfaceID = "termx-tui-v3"
	}
	if shouldPreserveTerminalPoolAttachResizePolicy(root, result.TerminalID, msg.ResizePolicy) {
		result.ResizePolicy = msg.ResizePolicy
		if msg.ResizePolicy != state.TerminalResizeRoleOwner {
			result.CanResize = false
		}
	}
	result = normalizeTerminalAttachResultForLock(root, result)
	root.Session = root.Session.AttachWithResizeOwner(result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, result.ViewID)
	root.Surface = root.Surface.Attach(result.TerminalID, result.Cols, result.Rows)
	if msg.TargetFloatingID != "" {
		paneID := msg.TargetPaneID
		for _, floating := range root.Shell.Floatings {
			if floating.ID == msg.TargetFloatingID {
				paneID = floating.Pane.ID
				break
			}
		}
		root = invalidateCopyModeForTerminalRebind(root, paneID, result.ViewID, result.TerminalID)
		root.TerminalViews = root.TerminalViews.BindFloating(state.NewFloatingTerminalView(msg.TargetFloatingID, paneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, result.ViewID, result.CanResize))
		root.TerminalViews = projectTerminalAttachResultLock(root.TerminalViews, result)
		root.Shell = root.Shell.BindFloatingTerminal(msg.TargetFloatingID, result.TerminalID)
	} else {
		root.Shell = root.Shell.EnsureActiveTabForAttach()
		targetPaneID := msg.TargetPaneID
		if targetPaneID == "" {
			targetPaneID = root.Shell.EnsureDefaults().ActivePaneID
		}
		root = invalidateCopyModeForTerminalRebind(root, targetPaneID, result.ViewID, result.TerminalID)
		root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: targetPaneID}, result.TerminalID)
		root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(targetPaneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, result.ViewID, result.CanResize))
		root.TerminalViews = projectTerminalAttachResultLock(root.TerminalViews, result)
	}
	root.Shell = root.Shell.CloseOverlay().ExitInteractionMode()
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.attach", Body: result.TerminalID})
	effects := workbenchPersistEffects("terminal.attach")
	effects = append(effects, liveEffects(result.TerminalID, result.Cols, result.Rows, deps)...)
	return root.Advance(), effects
}

func reduceTerminalPoolCreateRequest(root state.Root, msg TerminalPoolCreateRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.new", Body: "terminal service missing"})
		return root.Advance(), nil
	}
	target, ok := terminalPoolTargetFromRequest(root, msg.TargetPaneID, msg.TargetFloatingID)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.new", Body: "missing target panel"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSizeForTarget(root, target)
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
			return TerminalPoolCreateResultMsg{TargetPaneID: target.PaneID, TargetFloatingID: target.FloatingID, Result: result, Err: err}
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
			return TerminalPoolAttachRequestMsg{TerminalID: msg.Result.TerminalID, TargetPaneID: msg.TargetPaneID, TargetFloatingID: msg.TargetFloatingID, ResizePolicy: state.TerminalResizeRoleOwner}
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
	root.Surface = root.Surface.Attach(msg.TerminalID, root.Surface.Cols, root.Surface.Rows)
	root.TerminalViews = root.TerminalViews.MarkTerminalReattaching(msg.TerminalID)
	root.Session = root.Session.ClearInputChannel(msg.TerminalID)
	effects := []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	effects = append(effects, restartTerminalViewEffects(root, msg.TerminalID)...)
	return root.Advance(), effects
}

func reduceTerminalPoolReconnectRequest(root state.Root, msg TerminalPoolReconnectRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" || deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.reconnect", Body: "terminal unavailable"})
		return root.Advance(), nil
	}
	target, ok := terminalPoolTargetFromRequest(root, msg.TargetPaneID, msg.TargetFloatingID)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.reconnect", Body: "missing target panel"})
		return root.Advance(), nil
	}
	cols, rows := terminalPoolAttachSizeForTarget(root, target)
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		result, err := deps.Terminal.Reconnect(ctx, services.TerminalReconnectRequest{TerminalID: msg.TerminalID, Cols: cols, Rows: rows, Mode: "collaborator", ResizePolicy: state.TerminalResizeRoleFollower, SurfaceID: "termx-tui-v3", ViewID: target.ViewID})
		return TerminalPoolReconnectResultMsg{TerminalID: msg.TerminalID, TargetPaneID: target.PaneID, TargetFloatingID: target.FloatingID, ResizePolicy: state.TerminalResizeRoleFollower, Result: result, Err: err}
	}}}
}

func reduceTerminalPoolReconnectResult(root state.Root, msg TerminalPoolReconnectResultMsg, deps LiveDeps) (state.Root, []Effect) {
	return reduceTerminalPoolAttachResult(root, TerminalPoolAttachResultMsg{TerminalID: msg.TerminalID, TargetPaneID: msg.TargetPaneID, TargetFloatingID: msg.TargetFloatingID, ResizePolicy: msg.ResizePolicy, Result: msg.Result, Err: msg.Err}, deps)
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
	root = removeTerminalFromRoot(root, msg.TerminalID)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.kill", Body: msg.TerminalID})
	effects := workbenchPersistEffects("terminal.kill")
	effects = append(effects, FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }})
	return root.Advance(), effects
}

func reduceTerminalPoolRemoveRequest(root state.Root, msg TerminalPoolRemoveRequestMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.TerminalID == "" || deps.Terminal == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.delete", Body: "terminal unavailable"})
		return root.Advance(), nil
	}
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		err := deps.Terminal.Remove(ctx, services.TerminalRemoveRequest{TerminalID: msg.TerminalID})
		return TerminalPoolRemoveResultMsg{TerminalID: msg.TerminalID, Err: err}
	}}}
}

func reduceTerminalPoolRemoveResult(root state.Root, msg TerminalPoolRemoveResultMsg) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	root.TerminalPool = root.TerminalPool.ApplyRemoved(msg.TerminalID, errText)
	if errText != "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.delete", Body: errText})
		return root.Advance(), nil
	}
	root = removeTerminalFromRoot(root, msg.TerminalID)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.delete", Body: msg.TerminalID})
	effects := workbenchPersistEffects("terminal.delete")
	effects = append(effects, FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }})
	return root.Advance(), effects
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

func reduceTerminalSizeLockToggleRequest(root state.Root, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.size", Body: "terminal service missing"})
		return root.Advance(), nil
	}
	target, ok := activeTerminalSizeLockTarget(root)
	if !ok {
		root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.size", Body: "no active terminal"})
		return root.Advance(), nil
	}
	tags, ok := terminalPoolTags(root.TerminalPool, target.TerminalID)
	if !ok {
		root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.size", Body: "terminal metadata pending"})
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	}
	locked := !terminalmeta.SizeLocked(tags)
	if locked {
		tags[terminalmeta.SizeLockTag] = terminalmeta.SizeLockLock
	} else {
		delete(tags, terminalmeta.SizeLockTag)
	}
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		err := deps.Terminal.EditTags(ctx, services.TerminalEditTagsRequest{TerminalID: target.TerminalID, Tags: tags})
		return TerminalSizeLockToggleResultMsg{TerminalID: target.TerminalID, Tags: tags, Locked: locked, Err: err}
	}}}
}

func reduceTerminalSizeLockToggleResult(root state.Root, msg TerminalSizeLockToggleResultMsg) (state.Root, []Effect) {
	errText := errorString(msg.Err)
	root.TerminalPool = root.TerminalPool.ApplyTagsEdited(msg.TerminalID, msg.Tags, errText)
	if errText != "" {
		root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.size", Body: errText})
		return root.Advance(), nil
	}
	root.TerminalViews = root.TerminalViews.ApplyTerminalSizeLock(msg.TerminalID, msg.Locked)
	body := "terminal size lock disabled"
	if msg.Locked {
		body = "terminal size is locked"
	}
	root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "terminal.size", Body: body})
	return root.Advance(), nil
}

type terminalSizeLockTarget struct {
	TerminalID string
}

type terminalPoolTarget struct {
	PaneID     string
	FloatingID string
	ViewID     string
}

func terminalPoolTargetFromRequest(root state.Root, paneID string, floatingID string) (terminalPoolTarget, bool) {
	shell := root.Shell.EnsureDefaults()
	if floatingID != "" {
		paneID = ""
		for _, floating := range shell.Floatings {
			if floating.ID == floatingID {
				paneID = floating.Pane.ID
				break
			}
		}
		return terminalPoolTarget{PaneID: paneID, FloatingID: floatingID, ViewID: state.TerminalFloatingViewID(floatingID)}, true
	}
	if paneID == "" {
		paneID = shell.ActivePaneID
	}
	if paneID == "" {
		return terminalPoolTarget{}, false
	}
	return terminalPoolTarget{PaneID: paneID, ViewID: state.TerminalPaneViewID(paneID)}, true
}

func activeTerminalSizeLockTarget(root state.Root) (terminalSizeLockTarget, bool) {
	if binding, ok := activeTerminalViewBinding(root); ok && binding.TerminalID != "" {
		if binding.HasResizeOwner() {
			return terminalSizeLockTarget{TerminalID: binding.TerminalID}, true
		}
		return terminalSizeLockTarget{}, false
	}
	return terminalSizeLockTarget{}, false
}

func terminalPoolTags(pool state.TerminalPoolStore, terminalID string) (map[string]string, bool) {
	for _, item := range pool.Items {
		if item.TerminalID == terminalID {
			tags := cloneStringMap(item.Tags)
			if tags == nil {
				tags = map[string]string{}
			}
			return tags, true
		}
	}
	return nil, false
}

func normalizeTerminalAttachResultForLock(root state.Root, result services.TerminalAttachResult) services.TerminalAttachResult {
	if terminalAttachResultSizeLocked(root, result) {
		// 中文说明：terminal size lock 是 terminal 级最高优先级；attach result 即使返回 owner/canResize，
		// 也不能冲掉 metadata 或已有 binding 上的锁，否则新 pane attach 会用自己的尺寸改 PTY。
		result.SizeLocked = true
		result.CanResize = false
		result.ControlReason = "size_locked"
		if result.ResizePolicy == "" {
			result.ResizePolicy = state.TerminalResizeRoleOwner
		}
	}
	return result
}

func projectTerminalAttachResultLock(store state.TerminalViewStore, result services.TerminalAttachResult) state.TerminalViewStore {
	if !result.SizeLocked {
		return store
	}
	return store.ApplyTerminalSizeLock(result.TerminalID, true)
}

func terminalAttachResultSizeLocked(root state.Root, result services.TerminalAttachResult) bool {
	terminalID := result.TerminalID
	if terminalID == "" {
		return result.SizeLocked
	}
	if result.SizeLocked {
		return true
	}
	for _, binding := range root.TerminalViews.BindingsForTerminal(terminalID) {
		if binding.SizeLocked {
			return true
		}
	}
	tags, ok := terminalPoolTags(root.TerminalPool, terminalID)
	return ok && terminalmeta.SizeLocked(tags)
}

func shouldPreserveTerminalPoolAttachResizePolicy(root state.Root, terminalID string, resizePolicy string) bool {
	if resizePolicy == "" {
		return false
	}
	if resizePolicy == state.TerminalResizeRoleOwner {
		return true
	}
	// 中文说明：同 terminal 已有本地 binding 时，picker/reconnect attach 是新增 follower view，
	// 不能因为 core 旧 owner 状态或 auto-owner 结果让新 pane 抢走 resize authority。
	return len(root.TerminalViews.BindingsForTerminal(terminalID)) > 0
}

func terminalPoolItemsFromService(items []services.TerminalPoolItem) []state.TerminalPoolItem {
	out := make([]state.TerminalPoolItem, len(items))
	for i, item := range items {
		out[i] = state.TerminalPoolItem{
			TerminalID: item.TerminalID,
			Title:      item.Title,
			State:      item.State,
			CWD:        item.CWD,
			Command:    append([]string(nil), item.Command...),
			Tags:       cloneStringMap(item.Tags),
			ExitCode:   cloneIntPointer(item.ExitCode),
			ExitedAt:   item.ExitedAt,
			Cols:       item.Cols,
			Rows:       item.Rows,
		}
	}
	return out
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func projectTerminalPoolExitMetadata(surface state.TerminalSurfaceStore, items []state.TerminalPoolItem) state.TerminalSurfaceStore {
	for _, item := range items {
		if item.TerminalID == "" || item.State != string(state.TerminalLiveExited) {
			continue
		}
		exitCode := 0
		if item.ExitCode != nil {
			exitCode = *item.ExitCode
		}
		// 中文说明：terminal pool 是 core-v2 lifecycle metadata 的投影；这里仅补 live surface 的展示元数据，
		// 不把 pool/list 当作输入路由或历史 truth。
		surface = surface.MarkExitedWithMetadata(item.TerminalID, exitCode, "exited", item.ExitedAt, item.Command)
	}
	return surface
}

func restartTerminalViewEffects(root state.Root, terminalID string) []Effect {
	if terminalID == "" {
		return nil
	}
	bindings := root.TerminalViews.BindingsForTerminal(terminalID)
	if len(bindings) == 0 {
		return nil
	}
	effects := make([]Effect, 0, len(bindings))
	for _, binding := range bindings {
		cols, rows := binding.DesiredCols, binding.DesiredRows
		if cols <= 0 || rows <= 0 {
			cols, rows = root.Surface.SurfaceForTerminal(terminalID).Cols, root.Surface.SurfaceForTerminal(terminalID).Rows
		}
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		resizePolicy := binding.ResizeRole
		if resizePolicy == "" {
			resizePolicy = state.TerminalResizeRoleFollower
		}
		cfg := LiveConfig{
			TerminalID:   terminalID,
			Cols:         cols,
			Rows:         rows,
			Mode:         "collaborator",
			ResizePolicy: resizePolicy,
			SurfaceID:    binding.SurfaceID,
			ViewID:       binding.ViewID,
		}
		cfgCopy := cfg
		effects = append(effects, FuncEffect{Run: func(context.Context) Msg { return LiveAttachMsg{Config: cfgCopy} }})
	}
	return effects
}

func removeTerminalFromRoot(root state.Root, terminalID string) state.Root {
	root.TerminalViews = root.TerminalViews.RemoveTerminal(terminalID)
	root.Shell = root.Shell.RemoveTerminalBindings(terminalID)
	root.Session = root.Session.RemoveTerminal(terminalID)
	root.Surface = root.Surface.RemoveTerminal(terminalID)
	if root.History.TerminalID == terminalID {
		root.History = root.History.InvalidateWindow()
		root.History.TerminalID = ""
	}
	if root.CopyMode.TerminalID == terminalID {
		root.CopyMode = state.CopyModeStore{}
	}
	return root
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

func terminalPoolAttachSizeForTarget(root state.Root, target terminalPoolTarget) (int, int) {
	if rect, ok := terminalPoolTargetContentRect(root, target, render.Rect{}); ok {
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

func terminalPoolTargetContentRect(root state.Root, target terminalPoolTarget, fallbackViewport render.Rect) (render.Rect, bool) {
	plan, ok := terminalLayoutPlan(root, fallbackViewport)
	if !ok {
		return render.Rect{}, false
	}
	if target.FloatingID != "" {
		for _, layout := range plan.Floatings {
			if layout.Floating.ID == target.FloatingID && layout.ContentRect.W > 0 && layout.ContentRect.H > 0 {
				return layout.ContentRect, true
			}
		}
		return render.Rect{}, false
	}
	for _, panel := range plan.Panels {
		if panel.Panel.ID == target.PaneID && panel.ContentRect.W > 0 && panel.ContentRect.H > 0 {
			return panel.ContentRect, true
		}
	}
	return render.Rect{}, false
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
