package devcloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/private/cloud/companion"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/wire"
	remotev2client "github.com/lozzow/termx/remote/client"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
	"github.com/lozzow/termx/shared/remoteauth"
	unixtransport "github.com/lozzow/termx/shared/transport/unix"
	protocoladapter "github.com/lozzow/termx/tui/adapter/protocol"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

// TestManagedDirectE2EAcrossRealBoundaries 是 CLOUD003 的最终纵向 harness。
// cloud signaling 经过真实 loopback HTTP 与 owner-only IPC，terminal payload 经过真实 Pion DTLS DataChannel、capability handshake 和 core-v2 protocol；没有 fake Companion 或 memory transport。
func TestManagedDirectE2EAcrossRealBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CLOUD003 PTY harness currently requires a POSIX shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clock := &testClock{now: time.Now().UTC()}
	cloudRuntime, err := Start(Config{Now: clock.Now, EnrollmentCode: "managed-direct-enroll"})
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
	_, clientService := newTestCompanion(t, credentialStore, "managed-direct-client", clock, clientAdapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	)
	_, daemonService := newTestCompanion(t, credentialStore, "managed-direct-daemon", clock, daemonAdapter,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
		cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
	)
	clientSocket := startManagedDirectCompanionIPC(t, ctx, clientService, "client")
	daemonSocket := startManagedDirectCompanionIPC(t, ctx, daemonService, "daemon")

	loginManagedDirectClient(t, ctx, clientSocket)
	identity := enrollManagedDirectDaemon(t, ctx, daemonSocket, manifest.EnrollmentCode, clock)
	accessStore, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accessStore.Close() })

	localCore := startManagedDirectCore(t, ctx, "local")
	remoteCore := startManagedDirectCore(t, ctx, "remote")
	createManagedDirectTerminal(t, ctx, "local", localCore.client, "local-terminal", []string{"/bin/sh", "-c", "printf 'LOCAL-READY\\n'; sleep 30"})
	createManagedDirectTerminal(t, ctx, "remote", remoteCore.client, "remote-terminal", []string{"/bin/sh", "-c", "printf 'REMOTE-READY\\n'; while IFS= read -r line; do printf 'REMOTE-ECHO:%s\\n' \"$line\"; done"})

	daemonCompanion, _, err := ipc.DialAndHello(ctx, daemonSocket, ipc.HelloOptions{
		TermxVersion: "managed-direct-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_DAEMON,
		Capabilities: []cloudpb.CompanionCapability{
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE,
			cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
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
			Metadata: &cloudpb.DeviceMetadata{DisplayName: "Managed direct daemon", Platform: runtime.GOOS, TermxVersion: "managed-direct-e2e"},
			Answerer: remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
				Core: remoteCore.server, Identity: identity, AccessStore: accessStore, Now: clock.Now,
			}},
		}).Run(agentContext)
	}()

	clientCompanion, _, err := ipc.DialAndHello(ctx, clientSocket, ipc.HelloOptions{
		TermxVersion: "managed-direct-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_TUI,
		Capabilities: []cloudpb.CompanionCapability{cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitManagedDirectPresence(t, ctx, clientCompanion, identity.DeviceID)
	credential := issueManagedE2ECredential(t, accessStore, "managed-direct", "Managed direct client", clock.Now())

	managedEndpointID := state.EndpointID("managed-direct")
	phases := make([]cloudcompanion.EndpointPhase, 0, 5)
	session, err := remotev2client.DialSession(ctx, remotev2client.DialOptions{
		Companion: clientCompanion, EndpointID: string(managedEndpointID), TargetDeviceID: identity.DeviceID,
		DeviceFingerprint: identity.Fingerprint, Credential: credential,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, Now: clock.Now(),
		Phase: func(phase cloudcompanion.EndpointPhase) {
			if len(phases) == 0 || phases[len(phases)-1] != phase {
				phases = append(phases, phase)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteProtocolClient := protocol.NewClient(session.Transport)
	defer remoteProtocolClient.Close()
	if err := remoteProtocolClient.Hello(ctx, protocol.Hello{Version: wire.Version, Client: "managed-direct-e2e"}); err != nil {
		t.Fatal(err)
	}
	managed := newManagedE2EServices(t, managedEndpointID, remoteProtocolClient)
	local := newManagedE2EServices(t, state.DefaultEndpointID, localCore.client)
	listed, err := managed.List(ctx, port.TerminalListRequest{EndpointID: managedEndpointID})
	if err != nil || !managedDirectListContains(listed.Items, managedEndpointID, "remote-terminal") {
		t.Fatalf("managed list = (%#v, %v)", listed, err)
	}
	if session.ObservedPath != cloudcompanion.PathDirect {
		t.Fatalf("managed endpoint path = %q, want direct", session.ObservedPath)
	}
	wantPhases := []cloudcompanion.EndpointPhase{
		cloudcompanion.EndpointPhaseResolving, cloudcompanion.EndpointPhaseSignaling,
		cloudcompanion.EndpointPhaseConnecting, cloudcompanion.EndpointPhaseAuthorizing,
		cloudcompanion.EndpointPhaseConnected,
	}
	if !slices.Equal(phases, wantPhases) {
		t.Fatalf("managed endpoint phases = %v, want %v", phases, wantPhases)
	}
	attached, err := managed.Attach(ctx, port.TerminalAttachRequest{
		EndpointID: managedEndpointID, TerminalID: "remote-terminal", Cols: 72, Rows: 20,
		Mode: "collaborator", ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "managed-e2e", ViewID: "managed-e2e-view",
	})
	if err != nil || attached.Channel == 0 {
		t.Fatalf("managed attach = (%#v, %v)", attached, err)
	}
	resized, err := managed.Resize(ctx, port.TerminalResizeRequest{
		EndpointID: managedEndpointID, TerminalID: "remote-terminal", Channel: attached.Channel,
		Cols: 68, Rows: 18, ResizePolicy: state.TerminalResizeRoleOwner, SurfaceID: "managed-e2e", ViewID: "managed-e2e-view",
	})
	if err != nil || !resized.Resized || resized.Cols != 68 || resized.Rows != 18 {
		t.Fatalf("managed resize = (%#v, %v)", resized, err)
	}
	inputPayload := []byte("managed-direct-payload\n")
	if err := managed.SendInput(ctx, port.TerminalInputRequest{
		EndpointID: managedEndpointID, TerminalID: "remote-terminal", Channel: attached.Channel,
		SurfaceID: "managed-e2e", ViewID: "managed-e2e-view", Bytes: inputPayload,
	}); err != nil {
		t.Fatal(err)
	}
	waitManagedDirectLiveText(t, ctx, managed, managedEndpointID, "remote-terminal", "REMOTE-ECHO:managed-direct-payload")
	waitManagedDirectHistoryText(t, ctx, managed, managedEndpointID, "remote-terminal", "managed-direct-payload")

	for _, request := range capture.snapshot() {
		if bytes.Contains(request.body, []byte(credential.CapabilityGrant)) || bytes.Contains(request.body, inputPayload) || bytes.Contains(request.body, identity.PrivateKey) {
			t.Fatalf("cloud boundary observed capability, terminal payload, or daemon private key at %s", request.path)
		}
	}

	remoteShutdownContext, remoteShutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := remoteCore.server.Shutdown(remoteShutdownContext); err != nil {
		remoteShutdownCancel()
		t.Fatal(err)
	}
	remoteShutdownCancel()
	select {
	case <-remoteProtocolClient.Done():
	case <-ctx.Done():
		t.Fatal("managed protocol session did not close after remote daemon shutdown")
	}
	localList, err := local.List(ctx, port.TerminalListRequest{EndpointID: state.DefaultEndpointID})
	if err != nil || !managedDirectListContains(localList.Items, state.DefaultEndpointID, "local-terminal") {
		t.Fatalf("remote daemon shutdown affected local endpoint: list=%#v err=%v", localList, err)
	}

	stopAgent()
	_ = clientCompanion.Close()
	_ = daemonCompanion.Close()
	select {
	case <-agentErrors:
	case <-time.After(2 * time.Second):
		t.Fatal("managed daemon agent did not stop")
	}
}

func issueManagedE2ECredential(t *testing.T, store *remoteauth.AccessStore, endpointID string, label string, now time.Time) remoteauth.ClientAccessCredential {
	t.Helper()
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Label: label, Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: 24 * time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	client, err := remoteauth.GenerateClientAccessIdentity(endpointID, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.RedeemPairingBundle(payload, client.PublicKey, label, now)
	if err != nil {
		t.Fatal(err)
	}
	return remoteauth.ClientAccessCredential{
		Version: 1, EndpointID: endpointID, Identity: client, CapabilityGrant: result.Grant, UpdatedAt: now,
	}
}

func managedDirectAdapter(t *testing.T, manifest httpapi.Manifest, client *http.Client, clock *testClock) *httpapi.Adapter {
	t.Helper()
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: manifest.ControlPlaneURL, HubURL: manifest.HubURL, HTTPClient: client, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func startManagedDirectCompanionIPC(t *testing.T, ctx context.Context, service *companion.Service, name string) string {
	t.Helper()
	runtimeDir := managedDirectShortTempDir(t, "txc-")
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(runtimeDir, name+"-companion.sock")
	listener, err := ipc.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := &ipc.Server{NewClient: func() (cloudcompanion.FullClient, error) { return service.NewConnection(), nil }}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("%s Companion IPC did not stop", name)
		}
	})
	return endpoint
}

func loginManagedDirectClient(t *testing.T, ctx context.Context, endpoint string) {
	t.Helper()
	client, _, err := ipc.DialAndHello(ctx, endpoint, ipc.HelloOptions{
		TermxVersion: "managed-direct-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_CLI,
		Capabilities: []cloudpb.CompanionCapability{cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	flow, err := client.BeginLogin(ctx, &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteLogin(ctx, &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()}); err != nil {
		t.Fatal(err)
	}
}

func enrollManagedDirectDaemon(t *testing.T, ctx context.Context, endpoint, enrollmentCode string, clock *testClock) remoteauth.Identity {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("daemon-managed-direct", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := ipc.DialAndHello(ctx, endpoint, ipc.HelloOptions{
		TermxVersion: "managed-direct-e2e", CallerRole: cloudpb.CallerRole_CALLER_ROLE_CLI,
		Capabilities: []cloudpb.CompanionCapability{cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	challenge, err := client.BeginDeviceEnrollment(ctx, &cloudpb.BeginDeviceEnrollmentRequest{
		OneTimeCode: enrollmentCode, DevicePublicKey: identity.PublicKey,
		Metadata: &cloudpb.DeviceMetadata{DisplayName: "Managed direct daemon", Platform: runtime.GOOS, TermxVersion: "managed-direct-e2e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proof := signEnrollmentProof(t, identity.PrivateKey, identity.PublicKey, identity.DeviceID, challenge, clock.Now())
	if _, err := client.CompleteDeviceEnrollment(ctx, &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: challenge.GetFlowId(), Proof: proof}); err != nil {
		t.Fatal(err)
	}
	return identity
}

type managedDirectCore struct {
	server *corev2.Server
	client *protocol.Client
}

func startManagedDirectCore(t *testing.T, ctx context.Context, name string) managedDirectCore {
	t.Helper()
	socketDir := managedDirectShortTempDir(t, "txd-")
	socket := filepath.Join(socketDir, name+"-core.sock")
	server := corev2.NewServer(corev2.WithSocketPath(socket), corev2.WithHistoryStorageDir(filepath.Join(t.TempDir(), name+"-history")))
	serveContext, cancelServe := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(serveContext) }()
	var transportError error
	var client *protocol.Client
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		transport, dialErr := unixtransport.Dial(socket)
		if dialErr == nil {
			client = protocol.NewClient(transport)
			transportError = client.Hello(ctx, protocol.Hello{Version: wire.Version, Client: "managed-direct-local:" + name})
			if transportError == nil {
				break
			}
			_ = client.Close()
		}
		transportError = dialErr
		time.Sleep(10 * time.Millisecond)
	}
	if client == nil || transportError != nil {
		cancelServe()
		t.Fatalf("start %s core client: %v", name, transportError)
	}
	t.Cleanup(func() {
		_ = client.Close()
		cancelServe()
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		_ = server.Shutdown(shutdownContext)
		shutdownCancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("%s core did not stop", name)
		}
	})
	return managedDirectCore{server: server, client: client}
}

func createManagedDirectTerminal(t *testing.T, ctx context.Context, endpointID clientendpoint.EndpointID, client *protocol.Client, terminalID string, command []string) {
	t.Helper()
	application, err := clientruntime.NewApplicationSession(clientruntime.EndpointSessionStamp{EndpointID: endpointID, RouteID: "unix", Generation: 1}, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.TerminalCreate(ctx, &apipb.TerminalCreateCommand{Terminal: &apipb.TerminalCreateSpec{TerminalId: terminalID, Name: terminalID, Command: command, Size: &apipb.TerminalSize{Cols: 80, Rows: 24}}}); err != nil {
		t.Fatal(err)
	}
}

func waitManagedDirectPresence(t *testing.T, ctx context.Context, client cloudcompanion.Client, deviceID string) {
	t.Helper()
	for {
		_, err := client.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{EndpointId: "presence-probe", TargetDeviceId: deviceID})
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("managed daemon presence did not become ready: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type managedE2EServices struct {
	endpointID state.EndpointID
	terminal   protocoladapter.ProtocolTerminalServiceAdapter
	core       protocoladapter.ProtocolCoreClientAdapter
}

func newManagedE2EServices(t *testing.T, endpointID state.EndpointID, client *protocol.Client) *managedE2EServices {
	t.Helper()
	terminal, err := protocoladapter.NewProtocolTerminalServiceAdapter(client, clientruntime.EndpointSessionStamp{EndpointID: clientendpoint.EndpointID(endpointID), RouteID: "e2e", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	return &managedE2EServices{
		endpointID: endpointID,
		terminal:   terminal,
		core:       protocoladapter.ProtocolCoreClientAdapter{Client: client},
	}
}

func (services *managedE2EServices) List(ctx context.Context, req port.TerminalListRequest) (port.TerminalListResult, error) {
	result, err := services.terminal.List(ctx, req)
	for index := range result.Items {
		result.Items[index].EndpointID = services.endpointID
	}
	return result, err
}

func (services *managedE2EServices) Attach(ctx context.Context, req port.TerminalAttachRequest) (port.TerminalAttachResult, error) {
	return services.terminal.Attach(ctx, req)
}

func (services *managedE2EServices) Resize(ctx context.Context, req port.TerminalResizeRequest) (port.TerminalResizeResult, error) {
	return services.terminal.Resize(ctx, req)
}

func (services *managedE2EServices) SendInput(ctx context.Context, req port.TerminalInputRequest) error {
	return services.terminal.SendInput(ctx, req)
}

func (services *managedE2EServices) LiveSurface(ctx context.Context, req port.TerminalSurfaceRequest) (port.TerminalSurfaceResult, error) {
	return services.terminal.LiveSurface(ctx, req)
}

func (services *managedE2EServices) HistoryLatest(ctx context.Context, req port.HistoryLatestRequest) (port.HistoryResult, error) {
	return services.core.HistoryLatest(ctx, req)
}

func (services *managedE2EServices) ReleaseHistory(ctx context.Context, req port.HistoryReleaseRequest) error {
	return services.core.ReleaseHistory(ctx, req)
}

func waitManagedDirectLiveText(t *testing.T, ctx context.Context, services *managedE2EServices, endpointID state.EndpointID, terminalID, text string) {
	t.Helper()
	for {
		result, err := services.LiveSurface(ctx, port.TerminalSurfaceRequest{EndpointID: endpointID, TerminalID: terminalID, Cols: 68, Rows: 18})
		if err == nil && strings.Contains(managedDirectLiveText(result.Snapshot), text) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("managed live surface did not contain %q: result=%#v err=%v", text, result, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitManagedDirectHistoryText(t *testing.T, ctx context.Context, services *managedE2EServices, endpointID state.EndpointID, terminalID, text string) {
	t.Helper()
	for requestID := port.RequestID(1); ; requestID++ {
		result, err := services.HistoryLatest(ctx, port.HistoryLatestRequest{EndpointID: endpointID, TerminalID: terminalID, RequestID: requestID, Cols: 68, Rows: 18})
		if err == nil {
			rows := make([]string, 0, len(result.Window.Rows))
			for _, row := range result.Window.Rows {
				rows = append(rows, row.Text)
			}
			if strings.Contains(strings.Join(rows, "\n"), text) {
				if result.Window.Token != "" {
					_ = services.ReleaseHistory(context.Background(), port.HistoryReleaseRequest{EndpointID: endpointID, TerminalID: terminalID, Token: result.Window.Token})
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("managed history did not contain %q: result=%#v err=%v", text, result, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func managedDirectListContains(items []port.TerminalPoolItem, endpointID state.EndpointID, terminalID string) bool {
	for _, item := range items {
		if item.EndpointID == endpointID && item.TerminalID == terminalID {
			return true
		}
	}
	return false
}

func managedDirectLiveText(snapshot state.LiveSurfaceSnapshot) string {
	if len(snapshot.Lines) > 0 {
		return strings.Join(snapshot.Lines, "\n")
	}
	rows := make([]string, 0, len(snapshot.Screen))
	for _, cells := range snapshot.Screen {
		var row strings.Builder
		for _, cell := range cells {
			row.WriteString(cell.Text)
		}
		rows = append(rows, row.String())
	}
	return strings.Join(rows, "\n")
}

func managedDirectShortTempDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
