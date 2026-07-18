package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

func TestReplacedAttachCandidateCleansLateResourceWithoutReplacingCommittedBinding(t *testing.T) {
	viewID := state.TerminalPaneViewID(state.DefaultPaneID)
	committed := state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-old", viewID, true)
	committed.Session = testEndpointSessionStamp(state.DefaultEndpointID)
	committed.OperationID = "attach-old"
	root := state.Root{Shell: state.DefaultShell(), TerminalViews: state.TerminalViewStore{}.BindPane(committed)}

	first := state.TerminalViewBinding{ViewID: viewID, EndpointID: state.DefaultEndpointID, TerminalID: "term-first", SurfaceID: "surface", PaneID: state.DefaultPaneID}
	var firstCandidate state.TerminalAttachCandidate
	root.TerminalViews, firstCandidate = root.TerminalViews.BeginAttach(first)
	second := first
	second.TerminalID = "term-second"
	var secondCandidate state.TerminalAttachCandidate
	root.TerminalViews, secondCandidate = root.TerminalViews.BeginAttach(second)

	late := port.TerminalAttachResult{
		EndpointID: state.DefaultEndpointID, TerminalID: "term-first", Channel: 9, SurfaceID: "surface", ViewID: viewID,
		Session: testEndpointSessionStamp(state.DefaultEndpointID), OperationID: firstCandidate.OperationID,
	}
	next, effects := reduceLiveAttachResult(root, LiveAttachResultMsg{
		EndpointID: state.DefaultEndpointID, TerminalID: "term-first", ViewID: viewID,
		OperationID: firstCandidate.OperationID, Result: late,
	}, LiveDeps{})
	binding, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID)
	if !ok || binding.Channel != 7 || binding.TerminalID != "term-old" || binding.AttachCandidate == nil || binding.AttachCandidate.OperationID != secondCandidate.OperationID {
		t.Fatalf("late candidate changed committed binding: %#v", binding)
	}
	if len(effects) != 1 {
		t.Fatalf("late candidate cleanup effects = %#v", effects)
	}
	request, ok := effects[0].(FuncEffect).Run(context.Background()).(LiveDetachRequestMsg)
	if !ok || request.Request.Channel != 9 || request.Request.OperationID != "cleanup:"+firstCandidate.OperationID || !protoEndpointSessionEqual(request.Request.Session, late.Session) {
		t.Fatalf("late candidate cleanup request = %#v", request)
	}
}

func prepareLiveAttachResultForTest(root state.Root, msg LiveAttachResultMsg) (state.Root, LiveAttachResultMsg) {
	viewID := msg.ViewID
	if viewID == "" {
		viewID = msg.Result.ViewID
	}
	if viewID == "" {
		viewID = state.TerminalPaneViewID(root.Shell.EnsureDefaults().ActivePaneID)
	}
	endpointID := msg.EndpointID
	if endpointID == "" {
		endpointID = msg.Result.EndpointID
	}
	endpointID = state.NormalizeEndpointID(endpointID)
	terminalID := msg.TerminalID
	if terminalID == "" {
		terminalID = msg.Result.TerminalID
	}
	target, ok := liveAttachTargetForViewID(root, viewID)
	if !ok {
		target.PaneID = root.Shell.EnsureDefaults().ActivePaneID
	}
	binding := state.TerminalViewBinding{
		ViewID: viewID, SurfaceID: msg.Result.SurfaceID, EndpointID: endpointID, TerminalID: terminalID,
		ResizeRole: msg.Result.ResizePolicy, DesiredCols: msg.Result.Cols, DesiredRows: msg.Result.Rows,
		PaneID: target.PaneID, FloatingID: target.FloatingID,
	}
	var candidate state.TerminalAttachCandidate
	root.TerminalViews, candidate = root.TerminalViews.BeginAttach(binding)
	msg.EndpointID = endpointID
	msg.TerminalID = terminalID
	msg.ViewID = viewID
	msg.OperationID = candidate.OperationID
	msg.Result.EndpointID = endpointID
	msg.Result.TerminalID = terminalID
	msg.Result.ViewID = viewID
	msg.Result.OperationID = candidate.OperationID
	if msg.Result.Session == nil {
		msg.Result.Session = testEndpointSessionStamp(endpointID)
	}
	return root, msg
}

func reduceLiveAttachResultPrepared(root state.Root, msg LiveAttachResultMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.OperationID == "" {
		root, msg = prepareLiveAttachResultForTest(root, msg)
	}
	return reduceLiveAttachResult(root, msg, deps)
}

func newLiveReducerPrepared(deps LiveDeps) Reducer {
	reducer := NewLiveReducer(deps)
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		if attach, ok := msg.(LiveAttachResultMsg); ok && attach.OperationID == "" {
			root, attach = prepareLiveAttachResultForTest(root, attach)
			msg = attach
		}
		if resize, ok := msg.(LiveResizeResultMsg); ok && resize.Session == nil && resize.ViewID != "" {
			if binding, found := root.TerminalViews.Views[resize.ViewID]; found {
				if binding.Session == nil {
					binding.Session = testEndpointSessionStamp(binding.EndpointID)
					root.TerminalViews.Views = cloneTerminalViewBindingsForTest(root.TerminalViews.Views)
					root.TerminalViews.Views[resize.ViewID] = binding
				}
				resize.Session = binding.AttachmentSession()
				if resize.OperationID == "" {
					resize.OperationID = "resize-test"
				}
				msg = resize
			}
		}
		return reducer(root, msg)
	}
}

func cloneTerminalViewBindingsForTest(source map[string]state.TerminalViewBinding) map[string]state.TerminalViewBinding {
	clone := make(map[string]state.TerminalViewBinding, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func testEndpointSessionStamp(endpointID state.EndpointID) *apipb.EndpointSessionStamp {
	return &apipb.EndpointSessionStamp{EndpointId: string(state.NormalizeEndpointID(endpointID)), RouteId: "test", Generation: 1}
}

func newTerminalPoolReducerPrepared(deps LiveDeps) Reducer {
	reducer := NewTerminalPoolReducer(deps)
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch value := msg.(type) {
		case TerminalPoolAttachResultMsg:
			if value.OperationID == "" {
				root, value = prepareTerminalPoolAttachResultForTest(root, value)
				msg = value
			}
		case TerminalPoolReconnectResultMsg:
			if value.OperationID == "" {
				attach := TerminalPoolAttachResultMsg{
					EndpointID: value.EndpointID, TerminalID: value.TerminalID, TargetPaneID: value.TargetPaneID,
					TargetFloatingID: value.TargetFloatingID, ResizePolicy: value.ResizePolicy, Result: value.Result, Err: value.Err,
				}
				root, attach = prepareTerminalPoolAttachResultForTest(root, attach)
				value.OperationID = attach.OperationID
				value.Result = attach.Result
				msg = value
			}
		}
		return reducer(root, msg)
	}
}

func prepareTerminalPoolAttachResultForTest(root state.Root, msg TerminalPoolAttachResultMsg) (state.Root, TerminalPoolAttachResultMsg) {
	target, _ := terminalPoolTargetFromRequest(root, msg.TargetPaneID, msg.TargetFloatingID)
	endpointID := state.NormalizeEndpointID(msg.EndpointID)
	if msg.Result.EndpointID != "" {
		endpointID = state.NormalizeEndpointID(msg.Result.EndpointID)
	}
	terminalID := msg.TerminalID
	if terminalID == "" {
		terminalID = msg.Result.TerminalID
	}
	viewID := msg.Result.ViewID
	if viewID == "" {
		viewID = target.ViewID
	}
	binding := state.TerminalViewBinding{
		ViewID: viewID, SurfaceID: msg.Result.SurfaceID, EndpointID: endpointID, TerminalID: terminalID,
		ResizeRole: msg.ResizePolicy, DesiredCols: msg.Result.Cols, DesiredRows: msg.Result.Rows,
		PaneID: target.PaneID, FloatingID: target.FloatingID,
	}
	var candidate state.TerminalAttachCandidate
	root.TerminalViews, candidate = root.TerminalViews.BeginAttach(binding)
	msg.OperationID = candidate.OperationID
	msg.Result.EndpointID = endpointID
	msg.Result.TerminalID = terminalID
	msg.Result.ViewID = viewID
	msg.Result.OperationID = candidate.OperationID
	if msg.Result.Session == nil {
		msg.Result.Session = testEndpointSessionStamp(endpointID)
	}
	return root, msg
}

func reduceTerminalPoolAttachResultPrepared(root state.Root, msg TerminalPoolAttachResultMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.OperationID == "" {
		root, msg = prepareTerminalPoolAttachResultForTest(root, msg)
	}
	return reduceTerminalPoolAttachResult(root, msg, deps)
}

func postPreparedLiveAttachResult(runtime *AppRuntime, msg LiveAttachResultMsg) error {
	runtime.state, msg = prepareLiveAttachResultForTest(runtime.state, msg)
	return runtime.Post(msg)
}

func postPreparedTerminalPoolAttachResult(runtime *AppRuntime, msg TerminalPoolAttachResultMsg) error {
	runtime.state, msg = prepareTerminalPoolAttachResultForTest(runtime.state, msg)
	return runtime.Post(msg)
}

func postPreparedTerminalPoolReconnectResult(runtime *AppRuntime, msg TerminalPoolReconnectResultMsg) error {
	attach := TerminalPoolAttachResultMsg{
		EndpointID: msg.EndpointID, TerminalID: msg.TerminalID, TargetPaneID: msg.TargetPaneID,
		TargetFloatingID: msg.TargetFloatingID, ResizePolicy: msg.ResizePolicy, Result: msg.Result, Err: msg.Err,
	}
	runtime.state, attach = prepareTerminalPoolAttachResultForTest(runtime.state, attach)
	msg.OperationID = attach.OperationID
	msg.Result = attach.Result
	return runtime.Post(msg)
}

func reduceTerminalPoolReconnectResultPrepared(root state.Root, msg TerminalPoolReconnectResultMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.OperationID == "" {
		attach := TerminalPoolAttachResultMsg{
			EndpointID: msg.EndpointID, TerminalID: msg.TerminalID, TargetPaneID: msg.TargetPaneID,
			TargetFloatingID: msg.TargetFloatingID, ResizePolicy: msg.ResizePolicy, Result: msg.Result, Err: msg.Err,
		}
		root, attach = prepareTerminalPoolAttachResultForTest(root, attach)
		msg.OperationID = attach.OperationID
		msg.Result = attach.Result
	}
	return reduceTerminalPoolReconnectResult(root, msg, deps)
}
