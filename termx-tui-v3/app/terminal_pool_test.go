package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestTerminalPoolListResultSchedulesResourceRefreshAndLivePreview(t *testing.T) {
	terminal := &services.FakeTerminalService{
		SurfaceResult: services.TerminalSurfaceResult{
			Ready: true,
			Snapshot: state.LiveSurfaceSnapshot{
				TerminalID: "term-1",
				Revision:   2,
				Lines:      []string{"preview"},
			},
		},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPool()}
	root.TerminalPool = root.TerminalPool.RequestList()
	seq := root.TerminalPool.RequestSeq

	next, effects := reducer(root, TerminalPoolListResultMsg{
		Seq: seq,
		Result: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "shell",
			State:      "running",
			Cols:       90,
			Rows:       20,
		}}},
	})

	if next.TerminalPool.Status != state.TerminalPoolReady || !terminalPoolRefreshLoopScheduled(effects) {
		t.Fatalf("list result should refresh inventory and arm background refresh, pool=%#v effects=%#v", next.TerminalPool, effects)
	}
	msg, ok := terminalPoolLiveSurfaceMsgFromEffects(t, effects)
	if !ok || msg.Snapshot.TerminalID != "term-1" || msg.RequestedCols != 90 || msg.RequestedRows != 20 {
		t.Fatalf("list result should refresh selected live preview, msg=%#v effects=%#v", msg, effects)
	}
	if len(terminal.Surfaces) != 1 || terminal.Surfaces[0].TerminalID != "term-1" || terminal.Surfaces[0].Cols != 90 || terminal.Surfaces[0].Rows != 20 {
		t.Fatalf("preview refresh should request selected terminal surface, got %#v", terminal.Surfaces)
	}
}

func TestTerminalPoolRefreshTickRequestsSilentList(t *testing.T) {
	terminal := &services.FakeTerminalService{
		ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{
			TerminalID: "term-1",
			Title:      "shell",
			State:      "running",
		}}},
	}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPool(),
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items:  []state.TerminalPoolItem{{TerminalID: "term-1", Title: "shell", State: "running"}},
		},
	}

	next, effects := reducer(root, TerminalPoolRefreshTickMsg{})
	if next.TerminalPool.Status != state.TerminalPoolReady || next.TerminalPool.RequestSeq == root.TerminalPool.RequestSeq {
		t.Fatalf("refresh tick should keep ready state and advance request seq, before=%#v after=%#v", root.TerminalPool, next.TerminalPool)
	}
	if len(effects) != 1 {
		t.Fatalf("refresh tick should only schedule list effect, got %#v", effects)
	}
	result, ok := effects[0].(FuncEffect).Run(context.Background()).(TerminalPoolListResultMsg)
	if !ok || !result.Refresh || result.Seq != next.TerminalPool.RequestSeq {
		t.Fatalf("refresh tick should return silent list result, got %#v", result)
	}
	if len(terminal.Lists) != 1 {
		t.Fatalf("refresh tick should call terminal list once, got %#v", terminal.Lists)
	}
}

func TestTerminalPickerListResultSchedulesBackgroundRefresh(t *testing.T) {
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: &services.FakeTerminalService{}})
	root := state.Root{Shell: state.DefaultShell().OpenTerminalPicker()}
	root.TerminalPool = root.TerminalPool.RequestList()
	seq := root.TerminalPool.RequestSeq

	next, effects := reducer(root, TerminalPoolListResultMsg{
		Seq:    seq,
		Result: services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-1", Title: "shell"}}},
	})
	if next.TerminalPool.Status != state.TerminalPoolReady || !terminalPoolRefreshLoopScheduled(effects) {
		t.Fatalf("picker list result should arm background inventory refresh, pool=%#v effects=%#v", next.TerminalPool, effects)
	}
	if terminalPoolPreviewRefreshScheduledIgnoringLoop(t, effects) {
		t.Fatalf("terminal picker must not schedule terminal-manager preview refresh, effects=%#v", effects)
	}
}

func TestTerminalPickerRefreshTickFansOutSilentEndpointList(t *testing.T) {
	terminal := &services.FakeTerminalService{ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-1", Title: "shell"}}}}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPicker(),
		Endpoints: state.EndpointStore{}.
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "Local", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true}).
			Upsert(state.EndpointItem{ID: "west", Label: "West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true}),
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items:  []state.TerminalPoolItem{{EndpointID: state.DefaultEndpointID, TerminalID: "term-1", Title: "shell"}},
		},
	}

	next, effects := reducer(root, TerminalPoolRefreshTickMsg{})
	if next.TerminalPool.Status != state.TerminalPoolReady || next.TerminalPool.RequestSeq == root.TerminalPool.RequestSeq || len(effects) != 2 {
		t.Fatalf("picker refresh tick should silently fan out endpoint lists, before=%#v after=%#v effects=%#v", root.TerminalPool, next.TerminalPool, effects)
	}
	seen := map[state.EndpointID]bool{}
	for _, effect := range effects {
		msg, ok := effect.(FuncEffect).Run(context.Background()).(TerminalPoolListResultMsg)
		if !ok || !msg.Refresh || msg.Seq != next.TerminalPool.RequestSeq {
			t.Fatalf("picker refresh effect should return silent endpoint list result, got %#v", msg)
		}
		seen[msg.EndpointID] = true
	}
	if !seen[state.DefaultEndpointID] || !seen["west"] {
		t.Fatalf("picker refresh should include local and west endpoints, got %#v", seen)
	}
}

func TestTerminalPoolListRequestFansOutToConnectableEndpoints(t *testing.T) {
	terminal := &services.FakeTerminalService{ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-1", Title: "shell"}}}}
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Endpoints: state.EndpointStore{}.
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "Local", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true}).
			Upsert(state.EndpointItem{ID: "west", Label: "West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true}).
			Upsert(state.EndpointItem{ID: "manual", Label: "Manual", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectManual, Enabled: true}).
			Upsert(state.EndpointItem{ID: "disabled", Label: "Disabled", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectAuto, Enabled: false}),
	}

	next, effects := reducer(root, TerminalPoolListRequestMsg{})
	if next.TerminalPool.Status != state.TerminalPoolLoading || len(effects) != 2 {
		t.Fatalf("expected fan-out effects for local and west only, pool=%#v effects=%#v", next.TerminalPool, effects)
	}
	seen := map[state.EndpointID]bool{}
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || !fn.Async || !fn.ForceSyncInTests {
			t.Fatalf("endpoint list effect should be async but syncable in tests, got %#v", effect)
		}
		msg, ok := fn.Run(context.Background()).(TerminalPoolListResultMsg)
		if !ok {
			t.Fatalf("endpoint list effect returned %#v", msg)
		}
		seen[msg.EndpointID] = true
	}
	if !seen[state.DefaultEndpointID] || !seen["west"] || seen["manual"] || seen["disabled"] {
		t.Fatalf("unexpected endpoint fan-out set %#v", seen)
	}
}

func TestTerminalPoolEndpointListFailureMarksOnlyThatEndpointOffline(t *testing.T) {
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: &services.FakeTerminalService{}})
	root := state.Root{
		TerminalPool: state.TerminalPoolStore{
			RequestSeq: 4,
			Items: []state.TerminalPoolItem{
				{EndpointID: state.DefaultEndpointID, TerminalID: "term-1", Title: "local"},
				{EndpointID: "west", TerminalID: "term-1", Title: "west"},
			},
		},
		Endpoints: state.EndpointStore{}.
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "Local", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true, Status: state.EndpointStatusConnected}).
			Upsert(state.EndpointItem{ID: "west", Label: "West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true, Status: state.EndpointStatusConnected}),
	}

	next, _ := reducer(root, TerminalPoolListResultMsg{EndpointID: "west", Seq: 4, Err: context.DeadlineExceeded})
	if len(next.TerminalPool.Items) != 2 {
		t.Fatalf("endpoint failure must not clear terminal pool rows, got %#v", next.TerminalPool.Items)
	}
	west, ok := next.Endpoints.Endpoint("west")
	if !ok || west.DisplayStatus() != state.EndpointStatusOffline || west.LastError == "" {
		t.Fatalf("west endpoint should be offline with error, got %#v", west)
	}
	local, ok := next.Endpoints.Endpoint(state.DefaultEndpointID)
	if !ok || local.DisplayStatus() != state.EndpointStatusConnected {
		t.Fatalf("local endpoint should stay connected, got %#v", local)
	}
}

func TestTerminalPoolRefreshFailureMarksEndpointOfflineSilently(t *testing.T) {
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: &services.FakeTerminalService{}})
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPool(),
		TerminalPool: state.TerminalPoolStore{
			RequestSeq: 7,
			Status:     state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{
				{EndpointID: state.DefaultEndpointID, TerminalID: "term-1", Title: "local"},
				{EndpointID: "west", TerminalID: "term-1", Title: "west"},
			},
		},
		Endpoints: state.EndpointStore{}.
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "Local", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true, Status: state.EndpointStatusConnected}).
			Upsert(state.EndpointItem{ID: "west", Label: "West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true, Status: state.EndpointStatusConnected}),
	}

	next, effects := reducer(root, TerminalPoolListResultMsg{EndpointID: "west", Seq: 7, Refresh: true, Err: context.DeadlineExceeded})
	if len(next.TerminalPool.Items) != 2 || next.TerminalPool.Status != state.TerminalPoolReady {
		t.Fatalf("refresh failure must not clear rows or move pool to error, pool=%#v", next.TerminalPool)
	}
	west, ok := next.Endpoints.Endpoint("west")
	if !ok || west.DisplayStatus() != state.EndpointStatusOffline || west.LastError == "" {
		t.Fatalf("west endpoint should be offline after refresh failure, got %#v", west)
	}
	if len(next.Shell.EnsureDefaults().Toasts) != 0 {
		t.Fatalf("refresh failure should stay silent, got toasts %#v", next.Shell.EnsureDefaults().Toasts)
	}
	if !terminalPoolRefreshLoopScheduled(effects) {
		t.Fatalf("refresh failure should keep background refresh loop, effects=%#v", effects)
	}
}

func TestTerminalPoolSelectionInputRefreshesPreview(t *testing.T) {
	reducer := NewUIInputReducer()
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPool(),
		TerminalPool: state.TerminalPoolStore{
			Status: state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{
				{TerminalID: "term-1", Title: "one"},
				{TerminalID: "term-2", Title: "two"},
			},
		},
	}

	next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDown}})
	if next.Shell.EnsureDefaults().Overlay.SelectedIndex != 1 || !terminalPoolPreviewRefreshScheduled(t, effects) {
		t.Fatalf("keyboard selection should refresh preview, shell=%#v effects=%#v", next.Shell, effects)
	}
	next, effects = reducer(next, ShellOverlayMouseSelectMsg{Delta: -1})
	if next.Shell.EnsureDefaults().Overlay.SelectedIndex != 0 || !terminalPoolPreviewRefreshScheduled(t, effects) {
		t.Fatalf("mouse selection should refresh preview, shell=%#v effects=%#v", next.Shell, effects)
	}

	shellNext, shellEffects := NewShellReducer()(next, ShellContentActionMsg{ActionID: render.ActionPoolSelect.String(), Row: 1})
	if shellNext.Shell.EnsureDefaults().Overlay.SelectedIndex != 1 || !terminalPoolPreviewRefreshScheduled(t, shellEffects) {
		t.Fatalf("row select action should refresh preview, shell=%#v effects=%#v", shellNext.Shell, shellEffects)
	}
}

func terminalPoolRefreshLoopScheduled(effects []Effect) bool {
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if ok && fn.Token == terminalPoolRefreshToken && fn.Async {
			return true
		}
	}
	return false
}

func terminalPoolLiveSurfaceMsgFromEffects(t *testing.T, effects []Effect) (LiveSurfaceMsg, bool) {
	t.Helper()
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || fn.Run == nil || fn.Token == terminalPoolRefreshToken {
			continue
		}
		if msg, ok := fn.Run(context.Background()).(LiveSurfaceMsg); ok {
			return msg, true
		}
	}
	return LiveSurfaceMsg{}, false
}

func terminalPoolPreviewRefreshScheduled(t *testing.T, effects []Effect) bool {
	t.Helper()
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || fn.Run == nil {
			continue
		}
		if _, ok := fn.Run(context.Background()).(TerminalPoolPreviewRefreshMsg); ok {
			return true
		}
	}
	return false
}

func terminalPoolPreviewRefreshScheduledIgnoringLoop(t *testing.T, effects []Effect) bool {
	t.Helper()
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if !ok || fn.Run == nil || fn.Token == terminalPoolRefreshToken {
			continue
		}
		if _, ok := fn.Run(context.Background()).(TerminalPoolPreviewRefreshMsg); ok {
			return true
		}
	}
	return false
}
