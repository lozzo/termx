package app

import (
	"context"
	"errors"
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

func TestTerminalPoolEndpointListSuccessDisconnectsMissingRemoteBinding(t *testing.T) {
	reducer := NewTerminalPoolReducer(LiveDeps{Terminal: &services.FakeTerminalService{}})
	shell := state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-1")
	shell = shell.SplitActivePane(state.PaneState{ID: "pane-local", Title: "local", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)
	surface := (state.TerminalSurfaceStore{}).ApplySnapshot(state.LiveSurfaceSnapshot{
		EndpointID: "west",
		TerminalID: "term-1",
		Lines:      []string{"old remote"},
		State:      state.TerminalLiveAttached,
	})
	root := state.Root{
		Shell: shell,
		Session: state.TerminalSessionStore{
			EndpointID:       "west",
			TerminalID:       "term-1",
			Channel:          9,
			Attached:         true,
			InputChannels:    map[string]uint16{"west/term-1": 9},
			State:            state.TerminalLiveAttached,
			ResizePolicy:     state.TerminalResizeRoleOwner,
			SurfaceID:        "surface-west",
			ViewID:           state.TerminalPaneViewID(state.DefaultPaneID),
			DesiredCols:      100,
			DesiredRows:      30,
			LastError:        "",
			ResizeRequestSeq: 2,
		},
		Surface: surface,
		TerminalPool: state.TerminalPoolStore{
			RequestSeq: 4,
			Status:     state.TerminalPoolReady,
			Items: []state.TerminalPoolItem{
				{EndpointID: "west", TerminalID: "term-1", Title: "west"},
				{EndpointID: state.DefaultEndpointID, TerminalID: "term-1", Title: "local"},
			},
		},
		Endpoints: state.EndpointStore{}.
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "Local", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true, Status: state.EndpointStatusConnected}).
			Upsert(state.EndpointItem{ID: "west", Label: "West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true, Status: state.EndpointStatusConnected}),
		History:  state.HistoryStore{EndpointID: "west", TerminalID: "term-1", Token: "tok-remote"},
		CopyMode: state.CopyModeStore{Active: true, EndpointID: "west", TerminalID: "term-1", BoundToken: "tok-remote"},
	}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewEndpointPaneTerminalView("west", state.DefaultPaneID, "term-1", 9, 100, 30, state.TerminalResizeRoleOwner, "surface-west", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewEndpointPaneTerminalView(state.DefaultEndpointID, "pane-local", "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-local", state.TerminalPaneViewID("pane-local"), true))

	next, effects := reducer(root, TerminalPoolListResultMsg{EndpointID: "west", Seq: 4, Refresh: true, Result: services.TerminalListResult{Items: nil}})
	if _, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID); ok {
		t.Fatalf("missing remote terminal should clear west pane binding, views=%#v", next.TerminalViews)
	}
	if pane, ok := next.Shell.Pane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}); !ok || pane.TerminalID != "" || pane.Kind != state.PaneEmpty {
		t.Fatalf("missing remote terminal should leave pane unconnected, pane=%#v ok=%v", pane, ok)
	}
	if binding, ok := next.TerminalViews.PaneBinding("pane-local"); !ok || !binding.TerminalRef().Equal(state.LocalTerminalRef("term-1")) {
		t.Fatalf("local same-id binding must survive remote missing list, binding=%#v ok=%v", binding, ok)
	}
	if pane, ok := next.Shell.Pane(state.PaneCommandTarget{PaneID: "pane-local"}); !ok || pane.TerminalID != "term-1" || pane.Kind != state.PaneTerminalLive {
		t.Fatalf("local same-id pane must stay connected, pane=%#v ok=%v", pane, ok)
	}
	if next.Session.TerminalID != "" || next.Surface.TerminalID != "" || next.History.TerminalID != "" || next.CopyMode.TerminalID != "" {
		t.Fatalf("missing remote terminal should clear live and copy state, session=%#v surface=%#v history=%#v copy=%#v", next.Session, next.Surface, next.History, next.CopyMode)
	}
	if len(next.TerminalPool.Items) != 1 || !next.TerminalPool.Items[0].TerminalRef().Equal(state.LocalTerminalRef("term-1")) {
		t.Fatalf("endpoint list should only remove west pool rows, pool=%#v", next.TerminalPool.Items)
	}
	if msg := firstWorkbenchPersistEffect(t, effects); msg.Reason != "terminal.inventory-missing" {
		t.Fatalf("missing terminal disconnect should persist workbench, msg=%#v effects=%#v", msg, effects)
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

func TestTerminalPoolCreateResultFallsBackToRequestedRemoteIDForAttach(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	next, effects := reduceTerminalPoolCreateResult(root, TerminalPoolCreateResultMsg{
		EndpointID:  "west",
		RequestedID: "term-requested",
		Result:      services.TerminalCreateResult{State: "running"},
	})
	if next.TerminalPool.LastCreatedRef != (state.TerminalRef{EndpointID: "west", TerminalID: "term-requested"}) {
		t.Fatalf("create result should record requested remote ref, pool=%#v", next.TerminalPool)
	}
	if len(effects) != 2 {
		t.Fatalf("create result should schedule list and attach, effects=%#v", effects)
	}
	attach, ok := effects[1].(FuncEffect).Run(context.Background()).(TerminalPoolAttachRequestMsg)
	if !ok || attach.EndpointID != "west" || attach.TerminalID != "term-requested" {
		t.Fatalf("create result should attach requested remote terminal, msg=%#v", attach)
	}
}

func TestTerminalPickerRemoteCreateRowOpensEndpointPrompt(t *testing.T) {
	root := state.Root{
		Shell: state.DefaultShell().OpenTerminalPicker(),
		Endpoints: (state.EndpointStore{}).
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "This Mac", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true}).
			Upsert(state.EndpointItem{ID: "us-west", Label: "US West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true}),
	}
	remoteRow := -1
	for index, item := range state.TerminalPickerItems(root) {
		if item.CreateNew && item.EndpointID == "us-west" {
			remoteRow = index
			break
		}
	}
	if remoteRow < 0 {
		t.Fatalf("expected remote create row, picker=%#v", state.TerminalPickerItems(root))
	}

	_, effects := reduceShellContentAction(root, ShellContentActionMsg{ActionID: render.ActionPickerNew.String(), Row: remoteRow})
	if len(effects) != 1 {
		t.Fatalf("expected prompt effect, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(context.Background()).(ShellOpenPromptMsg)
	if !ok {
		t.Fatalf("expected prompt message, got %#v", effects[0])
	}
	if msg.Prompt.TargetEndpointID != "us-west" || msg.Prompt.FieldRawValue("server") != "US West (us-west)" {
		t.Fatalf("remote create row should default prompt server to endpoint, prompt=%#v", msg.Prompt)
	}
}

func TestLiveAttachTerminalNotFoundDisconnectsPane(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachErr: errors.New("protocol error 404: terminal not found")}
	reducer := NewLiveReducer(LiveDeps{Terminal: terminal})
	root := state.Root{Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-missing")}

	pending, effects := reducer(root, LiveAttachMsg{Config: LiveConfig{
		EndpointID: "west",
		TerminalID: "term-missing",
		Cols:       80,
		Rows:       24,
		SurfaceID:  "surface-west",
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
	}})
	if len(effects) != 1 {
		t.Fatalf("expected attach effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	next, _ := reducer(pending, msg)
	if _, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID); ok {
		t.Fatalf("terminal-not-found attach should clear pending binding, views=%#v", next.TerminalViews)
	}
	if pane, ok := next.Shell.Pane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}); !ok || pane.TerminalID != "" || pane.Kind != state.PaneEmpty {
		t.Fatalf("terminal-not-found attach should leave pane unconnected, pane=%#v ok=%v", pane, ok)
	}
	if next.Session.TerminalID != "" || next.Surface.TerminalID != "" {
		t.Fatalf("terminal-not-found attach should clear live state, session=%#v surface=%#v", next.Session, next.Surface)
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
