package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestEndpointRuntimeStatusMarksPanesWithoutGlobalToast(t *testing.T) {
	root := state.Root{
		Session: state.TerminalSessionStore{
			EndpointID:       state.DefaultEndpointID,
			TerminalID:       "local",
			Channel:          3,
			Attached:         true,
			State:            state.TerminalLiveAttached,
			InputChannels:    map[string]uint16{"local": 3, "west/remote": 9},
			ResizePolicy:     state.TerminalResizeRoleOwner,
			SurfaceID:        "surface",
			ViewID:           state.TerminalPaneViewID(state.DefaultPaneID),
			DesiredCols:      80,
			DesiredRows:      24,
			ResizeRequestSeq: 1,
		},
		Surface: state.TerminalSurfaceStore{EndpointID: state.DefaultEndpointID, TerminalID: "local", Lines: []string{"local"}, State: state.TerminalLiveAttached, Ready: true},
		Endpoints: (state.EndpointStore{}).
			Upsert(state.EndpointItem{ID: state.DefaultEndpointID, Label: "This Mac", Transport: state.EndpointTransportLocal, ConnectMode: state.EndpointConnectAuto, Enabled: true, Status: state.EndpointStatusConnected}).
			Upsert(state.EndpointItem{ID: "west", Label: "US West", Transport: state.EndpointTransportSSH, ConnectMode: state.EndpointConnectOnDemand, Enabled: true, Status: state.EndpointStatusConnected}),
		TerminalPool: state.TerminalPoolStore{Items: []state.TerminalPoolItem{
			{EndpointID: state.DefaultEndpointID, TerminalID: "local", Title: "local"},
			{EndpointID: "west", TerminalID: "remote", Title: "remote"},
		}},
		Shell: state.DefaultShell(),
	}
	root.TerminalViews = root.TerminalViews.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "local", 3, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)).
		BindPane(state.NewEndpointPaneTerminalView("west", "pane-west", "remote", 9, 80, 24, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-west"), false))

	reducer := NewEndpointStatusReducer(LiveDeps{})
	next, effects := reducer(root, EndpointRuntimeStatusMsg{Event: services.EndpointRuntimeEvent{
		EndpointID: "west",
		Status:     state.EndpointStatusOffline,
		ErrorKind:  state.EndpointErrorTransportClosed,
		Err:        errors.New("ssh transport closed: exit status 255"),
	}})

	if len(effects) != 0 || len(next.Shell.Toasts) != 0 {
		t.Fatalf("endpoint disconnect should not create effects or global toasts, effects=%#v toasts=%#v", effects, next.Shell.Toasts)
	}
	west, ok := next.Endpoints.Endpoint("west")
	if !ok || west.DisplayStatus() != state.EndpointStatusOffline || west.LastErrorKind != state.EndpointErrorTransportClosed || !strings.Contains(west.LastError, "ssh transport closed") {
		t.Fatalf("west endpoint should record offline transport error, got %#v ok=%v", west, ok)
	}
	if local, ok := next.Endpoints.Endpoint(state.DefaultEndpointID); !ok || local.DisplayStatus() != state.EndpointStatusConnected || local.LastError != "" {
		t.Fatalf("local endpoint should stay connected, got %#v ok=%v", local, ok)
	}
	if binding, ok := next.TerminalViews.PaneBinding("pane-west"); !ok || binding.Channel != 0 || binding.Attached || !strings.Contains(binding.LastError, "transport-closed") {
		t.Fatalf("west pane should show endpoint error and clear stale channel, binding=%#v ok=%v", binding, ok)
	}
	if binding, ok := next.TerminalViews.PaneBinding(state.DefaultPaneID); !ok || binding.Channel != 3 || !binding.Attached || binding.LastError != "" {
		t.Fatalf("local pane should stay attached, binding=%#v ok=%v", binding, ok)
	}
	remoteSurface := next.Surface.SurfaceForTerminalRef(state.NewTerminalRef("west", "remote"))
	if remoteSurface.State != state.TerminalLiveError || !strings.Contains(remoteSurface.Err, "transport-closed") {
		t.Fatalf("west surface should show endpoint error, got %#v", remoteSurface)
	}
	if !next.Surface.TerminalRef().Equal(state.LocalTerminalRef("local")) || next.Surface.Err != "" || next.Surface.Lines[0] != "local" {
		t.Fatalf("active local surface should not be poisoned, got %#v", next.Surface)
	}
	if _, ok := next.Session.InputChannelForRef(state.NewTerminalRef("west", "remote")); ok {
		t.Fatalf("west input channel should be cleared, channels=%#v", next.Session.InputChannels)
	}
	if channel, ok := next.Session.InputChannelForRef(state.LocalTerminalRef("local")); !ok || channel != 3 {
		t.Fatalf("local input channel should survive, channel=%d ok=%v channels=%#v", channel, ok, next.Session.InputChannels)
	}
}
