package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/muxvia/muxvia/tui/input"
	"github.com/muxvia/muxvia/tui/state"
)

// ShellOpenConnectionsMsg 请求打开独立 Connections 页面；真实策略由随后触发的 LoadConnections effect 刷新。
type ShellOpenConnectionsMsg struct{}

func (ShellOpenConnectionsMsg) isMsg() {}

// ConnectionsLoadRequestMsg 请求 effect 从 Go Endpoint registry/planner 读取最新连接投影。
type ConnectionsLoadRequestMsg struct{}

func (ConnectionsLoadRequestMsg) isMsg() {}

// ConnectionsLoadResultMsg 把 adapter 返回的连接投影送回 reducer；错误不会清空当前页面数据。
type ConnectionsLoadResultMsg struct {
	Store state.EndpointStore
	Err   error
}

func (ConnectionsLoadResultMsg) isMsg() {}

// ConnectionsApplyRoutePrioritiesRequestMsg 请求原子保存一个 Endpoint 的完整自动 Route priority 集合。
type ConnectionsApplyRoutePrioritiesRequestMsg struct {
	EndpointID state.EndpointID
	Priorities map[string]*int
}

func (ConnectionsApplyRoutePrioritiesRequestMsg) isMsg() {}

// ConnectionsApplyRoutePrioritiesResultMsg 返回已提交后的 Go projection 或事务失败。
type ConnectionsApplyRoutePrioritiesResultMsg struct {
	EndpointID state.EndpointID
	Store      state.EndpointStore
	Err        error
}

func (ConnectionsApplyRoutePrioritiesResultMsg) isMsg() {}

// NewEndpointConnectionsReducer 只编排 Connections 的 load/apply effect 与结果投影。
// priority 校验、registry 原子事务和 planner availability 均由 port 实现负责，reducer 不建立第二份连接策略。
func NewEndpointConnectionsReducer(deps LiveDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case ConnectionsLoadRequestMsg:
			return root, []Effect{connectionsLoadEffect(deps)}
		case ConnectionsLoadResultMsg:
			if msg.Err != nil {
				root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastError, Title: "Connections", Body: msg.Err.Error()})
				return root.Advance(), nil
			}
			selectedID := selectedConnectionsEndpointID(root)
			root.Endpoints = root.Endpoints.ApplyConnectionProjection(msg.Store)
			root.Shell = restoreConnectionsSelection(root.Shell, root.Endpoints, selectedID)
			return root.Advance(), nil
		case ConnectionsApplyRoutePrioritiesRequestMsg:
			return root, []Effect{connectionsApplyEffect(deps, msg)}
		case ConnectionsApplyRoutePrioritiesResultMsg:
			return reduceConnectionsApplyResult(root, msg)
		default:
			return root, nil
		}
	}
}

func connectionsLoadEffect(deps LiveDeps) Effect {
	return FuncEffect{Run: func(ctx context.Context) Msg {
		if deps.EndpointConnections == nil {
			return ConnectionsLoadResultMsg{Err: fmt.Errorf("connection settings are unavailable")}
		}
		store, err := deps.EndpointConnections.LoadConnections(ctx)
		return ConnectionsLoadResultMsg{Store: store, Err: err}
	}}
}

func connectionsApplyEffect(deps LiveDeps, request ConnectionsApplyRoutePrioritiesRequestMsg) Effect {
	return FuncEffect{Run: func(ctx context.Context) Msg {
		if deps.EndpointConnections == nil {
			return ConnectionsApplyRoutePrioritiesResultMsg{EndpointID: request.EndpointID, Err: fmt.Errorf("connection settings are unavailable")}
		}
		store, err := deps.EndpointConnections.ApplyRoutePriorities(ctx, request.EndpointID, request.Priorities)
		return ConnectionsApplyRoutePrioritiesResultMsg{EndpointID: request.EndpointID, Store: store, Err: err}
	}}
}

func reduceConnectionsApplyResult(root state.Root, msg ConnectionsApplyRoutePrioritiesResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		shell := root.Shell.EnsureDefaults()
		if shell.Overlay.Kind == state.OverlayPrompt && shell.Overlay.Prompt.Purpose == "connections.priority" {
			prompt := shell.Overlay.Prompt
			prompt.Submitted = false
			prompt.LastResult = msg.Err.Error()
			shell.Overlay.Prompt = prompt
		}
		root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastError, Title: "Route priority", Body: msg.Err.Error()})
		return root.Advance(), nil
	}
	root.Endpoints = root.Endpoints.ApplyConnectionProjection(msg.Store)
	root.Shell = restoreConnectionsSelection(root.Shell.OpenConnections(), root.Endpoints, msg.EndpointID)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastSuccess, Title: "Route priority saved", Body: "Applies to the next connection"})
	return root.Advance(), nil
}

func reduceConnectionsInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	if entry, ok := input.ShortcutEntryForEvent(root.Config.Shortcuts, "connections", event); ok {
		return reduceOverlayShortcutAction(root, entry, event)
	}
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MoveConnectionsSelection(-1, len(root.Endpoints.Items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MoveConnectionsSelection(1, len(root.Endpoints.Items))
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func reduceConnectionsEdit(root state.Root, row int) (state.Root, []Effect) {
	if row >= 0 {
		root.Shell = root.Shell.SetConnectionsSelectedIndex(row, len(root.Endpoints.Items))
	}
	selected, ok := selectedConnectionsEndpoint(root)
	if !ok {
		return shortcutUnavailable(root, "connections.edit", "no endpoint")
	}
	fields := make([]state.PromptFieldState, 0, len(selected.Routes))
	for _, route := range selected.Routes {
		if !route.Enabled || route.ManualOnly {
			continue
		}
		value := ""
		if route.Priority != nil {
			value = strconv.Itoa(*route.Priority)
		}
		fields = append(fields, state.PromptFieldState{
			Key: "route:" + string(route.ID), Label: string(route.ID) + " (" + string(route.Kind) + ")", Value: value, Placeholder: "Full race",
		})
	}
	if len(fields) == 0 {
		return shortcutUnavailable(root, "connections.edit", "no automatic route")
	}
	root.Shell = root.Shell.OpenPrompt(state.PromptState{
		Title: "Route priority", Context: "Next connection only. Lower first; equal values race together; all blank is Full race.",
		Purpose: "connections.priority", TargetEndpointID: selected.ID, Fields: fields,
	})
	return root.Advance(), []Effect{handledEffect{}}
}

func routePriorityRequestFromPrompt(root state.Root, prompt state.PromptState) (ConnectionsApplyRoutePrioritiesRequestMsg, error) {
	if prompt.Purpose != "connections.priority" || prompt.TargetEndpointID == "" {
		return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("route priority endpoint is required")
	}
	target, ok := root.Endpoints.Endpoint(prompt.TargetEndpointID)
	if !ok {
		return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("endpoint %q is unavailable", prompt.TargetEndpointID)
	}
	automatic := make(map[string]struct{})
	for _, route := range target.Routes {
		if route.Enabled && !route.ManualOnly {
			automatic[string(route.ID)] = struct{}{}
		}
	}
	priorities := make(map[string]*int, len(prompt.Fields))
	anyValue, allValue := false, true
	for _, field := range prompt.Fields {
		routeID := strings.TrimPrefix(field.Key, "route:")
		if !strings.HasPrefix(field.Key, "route:") {
			return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("route priority form is invalid")
		}
		if _, ok := automatic[routeID]; !ok {
			return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("route %q is no longer automatic", routeID)
		}
		value := strings.TrimSpace(field.Value)
		anyValue = anyValue || value != ""
		allValue = allValue && value != ""
		if value == "" {
			priorities[routeID] = nil
			continue
		}
		priority, err := strconv.Atoi(value)
		if err != nil || priority < 0 {
			return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("route %q priority must be a non-negative integer", routeID)
		}
		priorityCopy := priority
		priorities[routeID] = &priorityCopy
	}
	if len(priorities) != len(automatic) {
		return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("route priority form must include every automatic route")
	}
	if anyValue && !allValue {
		return ConnectionsApplyRoutePrioritiesRequestMsg{}, fmt.Errorf("set every route priority or leave every field blank for Full race")
	}
	return ConnectionsApplyRoutePrioritiesRequestMsg{EndpointID: prompt.TargetEndpointID, Priorities: priorities}, nil
}

func selectedConnectionsEndpoint(root state.Root) (state.EndpointItem, bool) {
	items := root.Endpoints.Normalize().Items
	if len(items) == 0 {
		return state.EndpointItem{}, false
	}
	index := root.Shell.EnsureDefaults().Overlay.SelectedIndex
	if index < 0 || index >= len(items) {
		index = 0
	}
	return items[index], true
}

func selectedConnectionsEndpointID(root state.Root) state.EndpointID {
	item, _ := selectedConnectionsEndpoint(root)
	return item.ID
}

func restoreConnectionsSelection(shell state.ShellStore, store state.EndpointStore, endpointID state.EndpointID) state.ShellStore {
	if shell.EnsureDefaults().Overlay.Kind != state.OverlayConnections {
		return shell
	}
	for index, item := range store.Normalize().Items {
		if item.ID == endpointID {
			return shell.SetConnectionsSelectedIndex(index, len(store.Items))
		}
	}
	return shell.SetConnectionsSelectedIndex(0, len(store.Items))
}
