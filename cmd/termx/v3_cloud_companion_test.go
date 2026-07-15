package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	remotev2client "github.com/lozzow/termx/remote/client"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport/memory"
)

func TestDevelopmentCompanionSocketUsesRealIPCWithoutReleaseActivation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a unique Unix socket path")
	}
	runtimeDir, err := os.MkdirTemp("", "termx-cloud-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtimeDir)
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(runtimeDir, "companion.sock")
	listener, err := ipc.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	serveContext, stop := context.WithCancel(context.Background())
	defer stop()
	fake := &cloudcompanion.FakeClient{HelloFunc: func(_ context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
		return &cloudpb.CompanionHelloResponse{
			SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "dev-companion", BuildChannel: "development",
			SupportedCapabilities: append([]cloudpb.CompanionCapability(nil), request.GetRequestedCapabilities()...),
			ResponseNonce:         bytes.Repeat([]byte{0x52}, 32),
		}, nil
	}}
	done := make(chan error, 1)
	go func() {
		done <- (&ipc.Server{NewClient: func() (cloudcompanion.FullClient, error) { return fake, nil }}).Serve(serveContext, listener)
	}()
	t.Setenv(v3CloudCompanionSocketEnv, endpoint)
	previousVersion := termxBuildVersion
	termxBuildVersion = v3DevelopmentBuildVersion
	defer func() { termxBuildVersion = previousVersion }()
	client, err := defaultOpenV3CloudLifecycleClient(context.Background(), cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	requests := fake.Requests().Hello
	if len(requests) != 1 || requests[0].GetCallerRole() != cloudpb.CallerRole_CALLER_ROLE_TUI || len(requests[0].GetRequestedCapabilities()) != 1 {
		t.Fatalf("development IPC Hello = %#v", requests)
	}
	stop()
	_ = listener.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("development Companion IPC server did not stop")
	}
}

func TestStableBuildRejectsExplicitDevelopmentCompanionSocket(t *testing.T) {
	t.Setenv(v3CloudCompanionSocketEnv, filepath.Join(t.TempDir(), "untrusted.sock"))
	previousVersion := termxBuildVersion
	termxBuildVersion = "v1.0.0"
	defer func() { termxBuildVersion = previousVersion }()
	_, err := defaultOpenV3CloudLifecycleClient(context.Background(), cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING)
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("stable explicit socket error = %v", err)
	}
}

func TestDefaultDaemonCompanionRequestsRelayLeaseCapability(t *testing.T) {
	previousOpen := openV3CloudLifecycleClient
	defer func() { openV3CloudLifecycleClient = previousOpen }()
	var role cloudpb.CallerRole
	var capabilities []cloudpb.CompanionCapability
	openV3CloudLifecycleClient = func(_ context.Context, currentRole cloudpb.CallerRole, currentCapabilities ...cloudpb.CompanionCapability) (v3CloudClient, error) {
		role = currentRole
		capabilities = append([]cloudpb.CompanionCapability(nil), currentCapabilities...)
		return &closableCloudFake{FakeClient: &cloudcompanion.FakeClient{}}, nil
	}
	client, err := defaultOpenV3CloudDaemonCompanion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	want := []cloudpb.CompanionCapability{
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
	}
	if role != cloudpb.CallerRole_CALLER_ROLE_DAEMON || !slices.Equal(capabilities, want) {
		t.Fatalf("daemon Companion Hello = role %s capabilities %v, want %s %v", role, capabilities, cloudpb.CallerRole_CALLER_ROLE_DAEMON, want)
	}
}

func TestV3ManagedEndpointFailsClosedWhenCompanionIsUnavailable(t *testing.T) {
	dialer := v3ManagedCloudEndpointDialer()
	endpoint := testManagedEndpoint("lab", "Lab", "device-1", "SHA256:device-1", "grant-lab", connection.RelayAuto, connection.ConnectOnDemand, true)
	_, err := dialer(context.Background(), endpoint, testOnlyRoute(endpoint))
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) || !strings.Contains(err.Error(), "official termx release") {
		t.Fatalf("dial error = %v, want source-build COMPANION_UNTRUSTED guidance", err)
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
	cfg := testManagedEndpoint("lab", "Lab", "device-1", "ed25519-sha256:device-1", "grant-lab", connection.RelaySmart, connection.ConnectOnDemand, true)
	_, err := v3ManagedCloudEndpointDialer()(context.Background(), cfg, testOnlyRoute(cfg))
	if !errors.Is(err, wantErr) {
		t.Fatalf("dial error = %v, want injected stop", err)
	}
	if received.Companion != companion || received.EndpointID != "lab" || received.TargetDeviceID != "device-1" ||
		received.DeviceFingerprint != cfg.DaemonIdentity.DeviceFingerprint || received.CapabilityGrant != "opaque-capability-grant" ||
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
