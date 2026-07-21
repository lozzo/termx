package clientruntimeadapter

import (
	"context"
	"testing"

	"github.com/muxvia/muxvia/client/endpoint"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/tui/state"
)

func TestProjectEndpointEventOwnsTUIProjection(t *testing.T) {
	event := ProjectEndpointEvent(clientruntime.EndpointEvent{
		EndpointID: "studio", Phase: clientruntime.EndpointPhaseReady,
		ObservedPath: "single_relay", RouteSelectionReason: "lower_loss",
	})
	if event.EndpointID != "studio" || event.Status != state.EndpointStatusConnected || event.Phase != state.EndpointConnectionConnected {
		t.Fatalf("projected event = %#v", event)
	}
	if event.ObservedPath != "single_relay" || event.RouteSelectionReason != "lower_loss" {
		t.Fatalf("projected managed route = %#v", event)
	}
}

func TestEndpointEventSourceProjectsSharedRuntimeMailbox(t *testing.T) {
	runtimeEvents := make(chan clientruntime.EndpointEvent, 1)
	source := EndpointEventSource{Runtime: eventRuntime{events: runtimeEvents}, EndpointID: "studio"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := source.WatchEndpointEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtimeEvents <- clientruntime.EndpointEvent{EndpointID: "studio", Phase: clientruntime.EndpointPhaseReady, ObservedPath: "direct"}
	event := <-events
	if event.EndpointID != "studio" || event.Status != state.EndpointStatusConnected || event.ObservedPath != "direct" {
		t.Fatalf("projected event = %#v", event)
	}
}

type eventRuntime struct {
	events <-chan clientruntime.EndpointEvent
}

func (eventRuntime) EnsureSession(context.Context, clientruntime.ConnectRequest) (clientruntime.SessionLease, error) {
	return clientruntime.SessionLease{}, nil
}
func (eventRuntime) Disconnect(context.Context, clientruntime.DisconnectRequest) error { return nil }
func (runtime eventRuntime) WatchEndpoint(context.Context, endpoint.EndpointID) (<-chan clientruntime.EndpointEvent, error) {
	return runtime.events, nil
}

func TestProjectEndpointEventMapsStableRuntimeErrors(t *testing.T) {
	event := ProjectEndpointEvent(clientruntime.EndpointEvent{
		EndpointID: "studio", Phase: clientruntime.EndpointPhaseOffline,
		ErrorCode: clientruntime.ErrorAuthorization, Message: "authorization failed",
	})
	if event.Status != state.EndpointStatusOffline || event.Phase != state.EndpointConnectionFailed || event.ErrorKind != state.EndpointErrorAuth {
		t.Fatalf("projected failure = %#v", event)
	}
}
