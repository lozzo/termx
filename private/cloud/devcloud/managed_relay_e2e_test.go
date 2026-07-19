package devcloud

import (
	"bytes"
	"context"
	"net/http"
	"runtime"
	"slices"
	"testing"
	"time"

	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	"github.com/lozzow/termx/proto/cloudpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

// TestManagedSingleRelayE2EAcrossRealBoundaries 是 CLOUD004 的用户链路 harness。
// client 与 daemon 必须为同一 ManagedSession 取得不同 TURN credential，实际 ICE path 必须是 single_relay；DataChannel 内授权和 core-v2 protocol 与 direct 完全相同。
func TestManagedSingleRelayE2EAcrossRealBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CLOUD004 PTY harness currently requires a POSIX shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clock := &testClock{now: time.Now().UTC()}
	usageOutboxPath := t.TempDir() + "/relay-usage.outbox"
	cloudRuntime, err := Start(Config{Now: clock.Now, EnrollmentCode: "managed-relay-enroll", UsageOutboxPath: usageOutboxPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = cloudRuntime.Close(shutdownContext)
	})
	manifest := cloudRuntime.Manifest()
	capture := &captureTransport{}
	httpClient := &http.Client{Transport: capture}
	clientAdapter := managedDirectAdapter(t, manifest, httpClient, clock)
	daemonAdapter := managedDirectAdapter(t, manifest, httpClient, clock)
	credentialStore := &memoryCredentialStore{secrets: make(map[string][]byte)}
	_, clientService := newTestCompanion(t, credentialStore, "managed-relay-client", clock, clientAdapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
	)
	_, daemonService := newTestCompanion(t, credentialStore, "managed-relay-daemon", clock, daemonAdapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
	)
	clientSocket := startManagedDirectCompanionIPC(t, ctx, clientService, "relay-client")
	daemonSocket := startManagedDirectCompanionIPC(t, ctx, daemonService, "relay-daemon")
	loginManagedDirectClient(t, ctx, clientSocket)
	identity := enrollManagedDirectDaemon(t, ctx, daemonSocket, manifest.EnrollmentCode, clock)
	accessStore, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accessStore.Close() })

	localCore := startManagedDirectCore(t, ctx, "relay-local")
	remoteCore := startManagedDirectCore(t, ctx, "relay-remote")
	createManagedDirectTerminal(t, ctx, "relay-local", localCore.client, "relay-local-terminal", []string{"/bin/sh", "-c", "printf 'RELAY-LOCAL-READY\\n'; sleep 30"})
	createManagedDirectTerminal(t, ctx, "relay-remote", remoteCore.client, "relay-remote-terminal", []string{"/bin/sh", "-c", "printf 'RELAY-REMOTE-READY\\n'; while IFS= read -r line; do printf 'RELAY-ECHO:%s\\n' \"$line\"; done"})

	daemonCompanion, _, err := ipc.DialAndHello(ctx, daemonSocket, ipc.HelloOptions{
		TermxVersion: "managed-relay-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_DAEMON,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	agentContext, stopAgent := context.WithCancel(ctx)
	t.Cleanup(stopAgent)
	agentErrors := make(chan error, 1)
	go func() {
		agentErrors <- (remotev2daemon.Agent{
			Companion: daemonCompanion, Identity: identity, Now: clock.Now,
			Metadata: &cloudpb.DeviceMetadata{DisplayName: "Managed Relay daemon", Platform: runtime.GOOS, TermxVersion: "managed-relay-e2e"},
			Answerer: remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
				Core: remoteCore.server, Identity: identity, AccessStore: accessStore, Now: clock.Now,
			}},
		}).Run(agentContext)
	}()

	clientCompanion, _, err := ipc.DialAndHello(ctx, clientSocket, ipc.HelloOptions{
		TermxVersion: "managed-relay-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_TUI,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitManagedDirectPresence(t, ctx, clientCompanion, identity.DeviceID)
	presenceOpenObserved := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		for _, request := range capture.snapshot() {
			if request.path == httpapi.HubOpenPresencePath {
				presenceOpenObserved = true
				break
			}
		}
		if presenceOpenObserved {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !presenceOpenObserved {
		t.Fatal("daemon Hub presence stream was not established before Control Plane shutdown")
	}
	controlShutdownContext, controlShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := cloudRuntime.controlServer.Shutdown(controlShutdownContext); err != nil {
		controlShutdownCancel()
		t.Fatal(err)
	}
	controlShutdownCancel()
	credential := issueManagedE2ECredential(t, accessStore, "managed-relay", "Managed Relay client", clock.Now())

	managedEndpointID := state.EndpointID("managed-relay")
	phases := make([]clientruntime.EndpointPhase, 0, 5)
	session, err := dialManagedE2ESession(ctx, clientCompanion, string(managedEndpointID), identity, credential, clientendpoint.RelayOnly, clock.Now, "managed-relay-e2e", func(phase clientruntime.EndpointPhase) {
		if len(phases) == 0 || phases[len(phases)-1] != phase {
			phases = append(phases, phase)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	remoteProtocolClient := session.FramingClient()
	managed := newManagedE2EServices(t, managedEndpointID, remoteProtocolClient)
	local := newManagedE2EServices(t, state.DefaultEndpointID, localCore.client)
	listed, err := managed.List(ctx, port.TerminalListRequest{EndpointID: managedEndpointID})
	if err != nil || !managedDirectListContains(listed.Items, managedEndpointID, "relay-remote-terminal") {
		select {
		case agentErr := <-agentErrors:
			t.Logf("daemon agent error before Relay connection: %v", agentErr)
		default:
		}
		for _, captured := range capture.snapshot() {
			t.Logf("cloud request host=%s path=%s", captured.host, captured.path)
		}
		t.Fatalf("managed Relay list = (%#v, %v)", listed, err)
	}
	if session.ObservedPath() != string(clientendpoint.PathSingleRelay) {
		t.Fatalf("managed endpoint path = %q, want single_relay", session.ObservedPath())
	}
	wantPhases := []clientruntime.EndpointPhase{
		clientruntime.EndpointPhaseResolving, clientruntime.EndpointPhaseSignaling,
		clientruntime.EndpointPhaseConnecting, clientruntime.EndpointPhaseAuthorizing,
		clientruntime.EndpointPhaseReady,
	}
	if !slices.Equal(phases, wantPhases) {
		t.Fatalf("managed Relay phases = %v, want %v", phases, wantPhases)
	}
	attached, err := managed.Attach(ctx, port.TerminalAttachRequest{
		EndpointID: managedEndpointID, TerminalID: "relay-remote-terminal", Cols: 72, Rows: 20,
		Mode: "collaborator", ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "managed-relay", ViewID: "managed-relay-view",
		OperationID: "attach:managed-relay",
	})
	if err != nil || attached.Channel == 0 {
		t.Fatalf("managed Relay attach = (%#v, %v)", attached, err)
	}
	inputPayload := []byte("managed-relay-payload\n")
	if err := managed.SendInput(ctx, port.TerminalInputRequest{
		EndpointID: managedEndpointID, TerminalID: "relay-remote-terminal", Channel: attached.Channel,
		SurfaceID: "managed-relay", ViewID: "managed-relay-view", Session: attached.Session, OperationID: "input:managed-relay", Bytes: inputPayload,
	}); err != nil {
		t.Fatal(err)
	}
	waitManagedDirectLiveText(t, ctx, managed, managedEndpointID, "relay-remote-terminal", "RELAY-ECHO:managed-relay-payload")
	waitManagedDirectHistoryText(t, ctx, managed, managedEndpointID, "relay-remote-terminal", "managed-relay-payload")
	clock.Advance(2 * time.Second)
	if err := cloudRuntime.flushRelayUsage(ctx, ""); err == nil {
		t.Fatal("Relay usage unexpectedly reached the stopped Control Plane")
	}
	cloudRuntime.state.relayControl.mu.Lock()
	managedSessionID := ""
	for _, relaySession := range cloudRuntime.state.relayControl.sessions {
		managedSessionID = relaySession.claims.ManagedSessionID
		break
	}
	cloudRuntime.state.relayControl.mu.Unlock()
	restartedOutbox, err := cloudrelay.NewUsageOutbox(usageOutboxPath)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := restartedOutbox.Pending()
	if err != nil || len(pending) == 0 {
		t.Fatalf("Control Plane down usage outbox = (%#v, %v)", pending, err)
	}
	for _, record := range pending {
		claims, err := servicecredential.VerifyRelayLeaseForService(cloudRuntime.state.relayControl.leaseKeyRing, record.SignedLease, devRelayLeaseIssuer, devRelayPool, time.Minute, clock.Now())
		if err != nil {
			t.Fatalf("restored usage signed lease = %v", err)
		}
		if _, err := cloudRuntime.state.relayControl.usageLedger.Apply(claims, record.Event, clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err := restartedOutbox.Ack(record.Event.EventID, record.Event.Sequence); err != nil {
			t.Fatal(err)
		}
	}
	settled := cloudRuntime.state.relayControl.usageLedger.Aggregate(managedSessionID, "")
	if settled.BytesUp+settled.BytesDown == 0 || settled.ActiveSeconds == 0 {
		t.Fatalf("managed Relay settled usage = %#v", settled)
	}

	for _, request := range capture.snapshot() {
		if bytes.Contains(request.body, []byte(credential.CapabilityGrant)) || bytes.Contains(request.body, inputPayload) || bytes.Contains(request.body, identity.PrivateKey) {
			t.Fatalf("cloud boundary observed capability, terminal payload, or daemon private key at %s", request.path)
		}
	}

	if err := cloudRuntime.relayServer.Close(); err != nil {
		t.Fatal(err)
	}
	failureCompanion, _, err := ipc.DialAndHello(ctx, clientSocket, ipc.HelloOptions{
		TermxVersion: "managed-relay-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_TUI,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer failureCompanion.Close()
	failureContext, cancelFailure := context.WithTimeout(ctx, 3*time.Second)
	defer cancelFailure()
	failedSession, relayFailure := dialManagedE2ESession(failureContext, failureCompanion, "managed-relay", identity, credential, clientendpoint.RelayOnly, clock.Now, "managed-relay-failure", nil)
	if relayFailure == nil {
		_ = failedSession.Close()
		t.Fatal("managed Relay reconnected after its only TURN server stopped")
	}

	localList, err := local.List(ctx, port.TerminalListRequest{EndpointID: state.DefaultEndpointID})
	if err != nil || !managedDirectListContains(localList.Items, state.DefaultEndpointID, "relay-local-terminal") {
		t.Fatalf("managed Relay affected local endpoint: list=%#v err=%v", localList, err)
	}
	stopAgent()
	_ = clientCompanion.Close()
	_ = daemonCompanion.Close()
	select {
	case <-agentErrors:
	case <-time.After(2 * time.Second):
		t.Fatal("managed Relay daemon agent did not stop")
	}
}
