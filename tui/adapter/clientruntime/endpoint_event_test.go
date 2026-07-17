package clientruntimeadapter

import (
	"testing"

	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/tui/state"
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

func TestProjectEndpointEventMapsStableRuntimeErrors(t *testing.T) {
	event := ProjectEndpointEvent(clientruntime.EndpointEvent{
		EndpointID: "studio", Phase: clientruntime.EndpointPhaseOffline,
		ErrorCode: clientruntime.ErrorAuthorization, Message: "authorization failed",
	})
	if event.Status != state.EndpointStatusOffline || event.Phase != state.EndpointConnectionFailed || event.ErrorKind != state.EndpointErrorAuth {
		t.Fatalf("projected failure = %#v", event)
	}
}
