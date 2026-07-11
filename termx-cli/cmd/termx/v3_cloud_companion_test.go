package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	remotev2client "github.com/lozzow/termx/termx-remote-v2/client"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/connection"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func TestV3ManagedEndpointFailsClosedWhenCompanionIsUnavailable(t *testing.T) {
	dialer := v3ManagedCloudEndpointDialer()
	_, err := dialer(context.Background(), connection.Config{
		ID: "lab", Label: "Lab", Transport: connection.TransportHubP2P, ConnectMode: connection.ConnectOnDemand, Enabled: true,
		HubURL: "https://hub.example.com", HubDeviceID: "device-1", DeviceFingerprint: "SHA256:device-1",
		GrantRef: "grant-lab", RelayMode: connection.RelayAuto,
	})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) {
		t.Fatalf("dial error = %v, want COMPANION_MISSING", err)
	}
}

func TestV3ManagedEndpointPassesSharedIdentityCredentialAndSmartRoutePolicyToRemoteV2(t *testing.T) {
	previousOpen := openV3CloudCompanion
	previousDial := dialV3ManagedSession
	defer func() {
		openV3CloudCompanion = previousOpen
		dialV3ManagedSession = previousDial
	}()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := remoteauth.NewCredentialStore(v3RemoteCredentialDir()).Put("grant-lab", "opaque-capability-grant"); err != nil {
		t.Fatalf("put grant fixture: %v", err)
	}
	companion := &cloudcompanion.FakeClient{}
	openV3CloudCompanion = func(context.Context) (cloudcompanion.Client, error) { return companion, nil }
	wantErr := errors.New("stop after dial options")
	var received remotev2client.DialOptions
	dialV3ManagedSession = func(_ context.Context, options remotev2client.DialOptions) (remotev2client.Session, error) {
		received = options
		return remotev2client.Session{}, wantErr
	}
	cfg := connection.Config{
		ID: "lab", Label: "Lab", Transport: connection.TransportHubP2P, ConnectMode: connection.ConnectOnDemand, Enabled: true,
		HubURL: "https://hub.example.com", HubDeviceID: "device-1", DeviceFingerprint: "ed25519-sha256:device-1",
		GrantRef: "grant-lab", RelayMode: connection.RelaySmart,
	}
	_, err := v3ManagedCloudEndpointDialer()(context.Background(), cfg)
	if !errors.Is(err, wantErr) {
		t.Fatalf("dial error = %v, want injected stop", err)
	}
	if received.Companion != companion || received.EndpointID != "lab" || received.TargetDeviceID != "device-1" ||
		received.DeviceFingerprint != cfg.DeviceFingerprint || received.CapabilityGrant != "opaque-capability-grant" ||
		received.RoutePreference != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE || received.RelayOnly ||
		!received.QualityObservation.Enabled || received.QualityObservation.NetworkClass != "unknown" {
		t.Fatalf("managed remote-v2 options lost endpoint contract: %#v", received)
	}
}

func TestManagedCompanionClosesAfterTransportAndObservation(t *testing.T) {
	clientTransport, _ := memory.NewPair()
	observationDone := make(chan struct{})
	closer := &recordingCloser{closed: make(chan struct{})}
	closeManagedCompanionAfterSession(remotev2client.Session{Transport: clientTransport, ObservationDone: observationDone}, closer)
	select {
	case <-closer.closed:
		t.Fatal("Companion closed before transport")
	default:
	}
	if err := clientTransport.Close(); err != nil {
		t.Fatalf("close transport: %v", err)
	}
	select {
	case <-closer.closed:
		t.Fatal("Companion closed before final observation")
	case <-time.After(10 * time.Millisecond):
	}
	close(observationDone)
	select {
	case <-closer.closed:
	case <-time.After(time.Second):
		t.Fatal("Companion remained open after observation completion")
	}
}

type recordingCloser struct {
	once   sync.Once
	closed chan struct{}
}

func (closer *recordingCloser) Close() error {
	closer.once.Do(func() { close(closer.closed) })
	return nil
}
