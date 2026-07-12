package devcloud

import (
	"bytes"
	"context"
	"net/http"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/wire"
	remotev2client "github.com/lozzow/termx/remote/client"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/tui/services"
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

	localCore := startManagedDirectCore(t, ctx, "relay-local")
	remoteCore := startManagedDirectCore(t, ctx, "relay-remote")
	createManagedDirectTerminal(t, ctx, localCore.client, "relay-local-terminal", []string{"/bin/sh", "-c", "printf 'RELAY-LOCAL-READY\\n'; sleep 30"})
	createManagedDirectTerminal(t, ctx, remoteCore.client, "relay-remote-terminal", []string{"/bin/sh", "-c", "printf 'RELAY-REMOTE-READY\\n'; while IFS= read -r line; do printf 'RELAY-ECHO:%s\\n' \"$line\"; done"})

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
				Core: remoteCore.server, Identity: identity, Revocations: remoteauth.NewRevocations(), Now: clock.Now,
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
	bundle, err := remoteauth.IssuePairingBundle(identity, remoteauth.PairingIssueOptions{
		Label: "Managed Relay daemon", Scope: remoteauth.Scope{AllowDaemon: true}, Lifetime: time.Hour, Now: clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	localTerminal := services.ProtocolTerminalServiceAdapter{Client: localCore.client}
	registry := connection.Registry{Version: 1, Default: connection.DefaultEndpointID, Connections: map[connection.EndpointID]connection.Config{
		connection.DefaultEndpointID: {
			ID: connection.DefaultEndpointID, Label: "Local", Transport: connection.TransportLocal,
			ConnectMode: connection.ConnectAuto, Enabled: true, Socket: "auto",
		},
		"managed-relay": {
			ID: "managed-relay", Label: "Managed Relay daemon", Transport: connection.TransportHubP2P,
			ConnectMode: connection.ConnectOnDemand, Enabled: true, HubDeviceID: identity.DeviceID,
			DeviceFingerprint: identity.Fingerprint, GrantRef: "managed-relay-grant", RelayMode: connection.RelayOnly,
		},
	}}
	manager := services.NewEndpointManagerWithDialers(registry, map[connection.TransportKind]services.EndpointDialer{
		connection.TransportHubP2P: func(dialContext context.Context, cfg connection.Config) (services.EndpointServiceBundle, error) {
			session, dialErr := remotev2client.DialSession(dialContext, remotev2client.DialOptions{
				Companion: clientCompanion, EndpointID: string(cfg.ID), TargetDeviceID: cfg.HubDeviceID,
				DeviceFingerprint: cfg.DeviceFingerprint, CapabilityGrant: bundle.CapabilityGrant,
				RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, RelayOnly: true, Now: clock.Now(),
				Phase: func(phase cloudcompanion.EndpointPhase) { services.ReportEndpointDialPhase(dialContext, phase) },
			})
			if dialErr != nil {
				return services.EndpointServiceBundle{}, dialErr
			}
			remoteProtocolClient := protocol.NewClient(session.Transport)
			if helloErr := remoteProtocolClient.Hello(dialContext, protocol.Hello{Version: wire.Version, Client: "managed-relay-e2e"}); helloErr != nil {
				_ = remoteProtocolClient.Close()
				return services.EndpointServiceBundle{}, helloErr
			}
			terminal := services.ProtocolTerminalServiceAdapter{Client: remoteProtocolClient}
			return services.EndpointServiceBundle{
				EndpointID: state.EndpointID(cfg.ID), Terminal: terminal, Core: services.ProtocolCoreClientAdapter{Client: remoteProtocolClient},
				Surface: terminal, LiveEvents: terminal, Path: services.ProtocolPathServiceAdapter{Client: remoteProtocolClient},
				ObservedPath: string(session.ObservedPath), Lifecycle: services.EndpointLifecycle{Done: remoteProtocolClient.Done(), Err: remoteProtocolClient.Err},
			}, nil
		},
	}, services.EndpointServiceBundle{
		EndpointID: state.DefaultEndpointID, Terminal: localTerminal, Core: services.ProtocolCoreClientAdapter{Client: localCore.client},
		Surface: localTerminal, LiveEvents: localTerminal, Path: services.ProtocolPathServiceAdapter{Client: localCore.client},
		Lifecycle: services.EndpointLifecycle{Done: localCore.client.Done(), Err: localCore.client.Err},
	})
	events, err := manager.WatchEndpointEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	managedEndpointID := state.EndpointID("managed-relay")
	listed, err := manager.List(ctx, services.TerminalListRequest{EndpointID: managedEndpointID})
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
	connected, phases := waitManagedDirectConnected(t, ctx, events, managedEndpointID)
	if connected.ObservedPath != string(cloudcompanion.PathSingleRelay) {
		t.Fatalf("managed endpoint path = %q, want single_relay", connected.ObservedPath)
	}
	wantPhases := []cloudcompanion.EndpointPhase{
		cloudcompanion.EndpointPhaseResolving, cloudcompanion.EndpointPhaseSignaling,
		cloudcompanion.EndpointPhaseConnecting, cloudcompanion.EndpointPhaseAuthorizing,
		cloudcompanion.EndpointPhaseConnected,
	}
	if !slices.Equal(phases, wantPhases) {
		t.Fatalf("managed Relay phases = %v, want %v", phases, wantPhases)
	}
	attached, err := manager.Attach(ctx, services.TerminalAttachRequest{
		EndpointID: managedEndpointID, TerminalID: "relay-remote-terminal", Cols: 72, Rows: 20,
		Mode: "collaborator", ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "managed-relay", ViewID: "managed-relay-view",
	})
	if err != nil || attached.Channel == 0 {
		t.Fatalf("managed Relay attach = (%#v, %v)", attached, err)
	}
	inputPayload := []byte("managed-relay-payload\n")
	if err := manager.SendInput(ctx, services.TerminalInputRequest{
		EndpointID: managedEndpointID, TerminalID: "relay-remote-terminal", Channel: attached.Channel,
		SurfaceID: "managed-relay", ViewID: "managed-relay-view", Bytes: inputPayload,
	}); err != nil {
		t.Fatal(err)
	}
	waitManagedDirectLiveText(t, ctx, manager, managedEndpointID, "relay-remote-terminal", "RELAY-ECHO:managed-relay-payload")
	waitManagedDirectHistoryText(t, ctx, manager, managedEndpointID, "relay-remote-terminal", "managed-relay-payload")
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
		if bytes.Contains(request.body, []byte(bundle.CapabilityGrant)) || bytes.Contains(request.body, inputPayload) || bytes.Contains(request.body, identity.PrivateKey) {
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
	failedSession, relayFailure := remotev2client.DialSession(failureContext, remotev2client.DialOptions{
		Companion: failureCompanion, EndpointID: "managed-relay-after-stop", TargetDeviceID: identity.DeviceID,
		DeviceFingerprint: identity.Fingerprint, CapabilityGrant: bundle.CapabilityGrant,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, RelayOnly: true, Now: clock.Now(),
	})
	if relayFailure == nil {
		_ = failedSession.Transport.Close()
		t.Fatal("managed Relay reconnected after its only TURN server stopped")
	}

	localList, err := manager.List(ctx, services.TerminalListRequest{EndpointID: state.DefaultEndpointID})
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
