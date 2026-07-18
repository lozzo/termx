package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/shared/filelock"
	"github.com/lozzow/termx/shared/remoteauth"
	unixtransport "github.com/lozzow/termx/shared/transport/unix"
)

func TestPairCreateAndImportUsesTicketThenClientBoundCredential(t *testing.T) {
	runtimeDir, stateHome, configHome := configurePairCommandTest(t)
	created := executePairCommand(t, nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--raw", "--label", "Lab daemon", "--ttl", "1h", "--grant-ttl", "24h")
	bundle, ticketClaims, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil || !ticketClaims.ScopeCeiling.AllowDaemon || bundle.GetIdentity().GetDeviceId() == "" {
		t.Fatalf("created pairing bundle = (%#v, %#v, %v)", bundle, ticketClaims, err)
	}
	canonicalBundle, err := endpointdomain.ParseEndpointBootstrapBundle(created)
	if err != nil || canonicalBundle.GetBundleId() != bundle.GetBundleId() {
		t.Fatalf("pair create did not emit canonical EndpointBootstrapBundleV2: bundle=%#v err=%v", canonicalBundle, err)
	}
	if strings.Contains(string(created), "capability_grant") || strings.Contains(string(created), "private_key") {
		t.Fatalf("static pairing bundle leaked long-lived credential: %s", created)
	}
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	savePairTestManagedEndpoint(t, registryPath, "lab", bundle, endpointdomain.RelayDirect)
	pairSocket := filepath.Join(runtimeDir, "daemon.sock.pair")
	imported := executePairCommand(t, created, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", pairSocket, "-")
	registryPayload, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(registryPayload, created) || strings.Contains(string(registryPayload), "capability_grant") || strings.Contains(string(registryPayload), "termx-grant-v2") {
		t.Fatalf("registry leaked ticket or grant: %s", registryPayload)
	}
	registry, err := endpointdomain.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := registry.Endpoints["lab"]
	route, ok := endpoint.Route("cloud")
	if !ok || route.CredentialRef == "" || route.RelayMode != endpointdomain.RelayDirect || endpoint.DaemonIdentity.DeviceFingerprint != bundle.GetIdentity().GetDeviceFingerprint() {
		t.Fatalf("imported endpoint = %#v", endpoint)
	}
	credential, err := remoteauth.NewCredentialStore(filepath.Join(stateHome, "termx", "remote-v2", "credentials")).Resolve(route.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	grantClaims, err := remoteauth.Verify(credential.CapabilityGrant, bundle.GetIdentity().GetDeviceFingerprint(), time.Now().UTC(), nil)
	if err != nil || grantClaims.SubjectKeyFingerprint != credential.Identity.Fingerprint || grantClaims.Version != 2 {
		t.Fatalf("stored bound grant = claims %#v credential %#v err=%v", grantClaims, credential, err)
	}
	if !strings.Contains(string(imported), "Paired endpoint lab") || strings.Contains(string(imported), credential.CapabilityGrant) {
		t.Fatalf("pair output lost result or leaked grant: %q", imported)
	}
}

func TestPairImportDoesNotInventManagedRouteForExistingSSHEndpoint(t *testing.T) {
	runtimeDir, _, configHome := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	created := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw", "--label", "Daemon label")
	_, _, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	ssh := endpointdomain.NewSSHEndpoint("lab", "Existing SSH label", "lab.example", "ssh:lab", "auto", endpointdomain.ConnectOnDemand)
	if err := endpointdomain.Save(registryPath, endpointdomain.Registry{Version: endpointdomain.RegistryVersion, Default: "lab", Endpoints: map[endpointdomain.EndpointID]endpointdomain.Endpoint{"lab": ssh}}); err != nil {
		t.Fatal(err)
	}
	err = executePairCommandError(created, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", v3PairingSocketPath(socket), "-")
	if err == nil || !strings.Contains(err.Error(), "no direct-tls or managed-webrtc route") {
		t.Fatalf("SSH-only pairing import error = %v", err)
	}
	registry, err := endpointdomain.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := registry.Endpoints["lab"]
	if len(registry.Endpoints) != 1 || endpoint.Label != "Existing SSH label" || !endpoint.DaemonIdentity.Empty() {
		t.Fatalf("failed pairing import mutated existing endpoint: %#v", registry)
	}
	if _, ok := endpoint.Route("ssh"); !ok {
		t.Fatal("pair import deleted existing SSH route")
	}
	if _, ok := endpoint.Route("cloud"); ok {
		t.Fatalf("pair import invented managed route: %#v", endpoint)
	}
}

func TestPairInspectAndTerminalQRNeverPrintLongLivedGrant(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	created := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw", "--label", "Inspect daemon")
	inspect := executePairCommand(t, created, "pair", "inspect", "--json", "-")
	if !strings.Contains(string(inspect), `"ticket_id"`) || !strings.Contains(string(inspect), `"scope_ceiling"`) || strings.Contains(string(inspect), "pairing_ticket") || strings.Contains(string(inspect), "capability_grant") {
		t.Fatalf("inspect projection = %s", inspect)
	}
	previousTerminal := v3PairOutputIsTerminal
	v3PairOutputIsTerminal = func(io.Writer) bool { return true }
	defer func() { v3PairOutputIsTerminal = previousTerminal }()
	qr := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--ttl", "5m")
	if !strings.Contains(string(qr), "Scan with the TermX App") || !strings.Contains(string(qr), "\x1b[40m") || strings.Contains(string(qr), `"pairing_ticket"`) {
		t.Fatalf("terminal QR output = %q", qr)
	}
}

func TestPairCreateWritesOwnerOnlyTicketBundle(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	path := filepath.Join(t.TempDir(), "pair", "bundle.json")
	executePairCommand(t, nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--out", path)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("pairing bundle mode = %v err=%v", info, err)
	}
}

func TestPairImportRegistryFailureKeepsRecoverableBoundCredential(t *testing.T) {
	runtimeDir, _, configHome := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	created := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw")
	bundle, _, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	savePairTestManagedEndpoint(t, registryPath, "lab", bundle, endpointdomain.RelayAuto)
	wantErr := errors.New("injected registry write failure")
	previousUpdate := updateV3ConnectionRegistry
	updateV3ConnectionRegistry = func(_ context.Context, path string, _ bool, mutate func(endpointdomain.Registry) (endpointdomain.Registry, error)) (endpointdomain.Registry, error) {
		registry, loadErr := endpointdomain.Load(path)
		if loadErr != nil {
			return endpointdomain.Registry{}, loadErr
		}
		updated, updateErr := mutate(registry)
		if updateErr != nil {
			return endpointdomain.Registry{}, updateErr
		}
		return updated, wantErr
	}
	command := newRootCmd()
	command.SetIn(bytes.NewReader(created))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", v3PairingSocketPath(socket), "-"})
	if err := command.Execute(); !errors.Is(err, wantErr) {
		t.Fatalf("pair import error = %v", err)
	}
	updateV3ConnectionRegistry = previousUpdate
	t.Cleanup(func() { updateV3ConnectionRegistry = previousUpdate })
	if retryErr := executePairCommandError(created, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", v3PairingSocketPath(socket), "-"); retryErr != nil {
		t.Fatalf("idempotent retry after registry failure: %v", retryErr)
	}
}

func TestPairImportRequiresExplicitScopeExpansionConfirmation(t *testing.T) {
	runtimeDir, _, configHome := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	narrow := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw", "--terminal", "term-1")
	bundle, _, err := remoteauth.ParsePairingBundle(narrow, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	savePairTestManagedEndpoint(t, registryPath, "lab", bundle, endpointdomain.RelayAuto)
	pairSocket := v3PairingSocketPath(socket)
	executePairCommand(t, narrow, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", pairSocket, "-")

	expanded := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw")
	err = executePairCommandError(expanded, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", pairSocket, "-")
	if !errors.Is(err, remoteauth.ErrGrantScopeExpansion) {
		t.Fatalf("silent scope expansion error = %v", err)
	}
	if err := executePairCommandError(expanded, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", pairSocket, "--allow-scope-expansion", "-"); err != nil {
		t.Fatalf("confirmed scope expansion failed: %v", err)
	}
}

func TestPairImportRootTimeoutCancelsLockedDaemonHelloAndReleasesLocks(t *testing.T) {
	runtimeDir, stateHome, configHome := configurePairCommandTest(t)
	created := executePairCommand(t, nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--raw")
	bundle, _, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	savePairTestManagedEndpoint(t, registryPath, "lab", bundle, endpointdomain.RelayAuto)

	hungSocket := filepath.Join(t.TempDir(), "hung.sock")
	previousStart := startV3Daemon
	var listener *unixtransport.Listener
	releaseServer := make(chan struct{})
	accepted := make(chan struct{})
	startV3Daemon = func(path string, _ string) error {
		if path != hungSocket {
			t.Fatalf("auto-start socket = %q, want %q", path, hungSocket)
		}
		var listenErr error
		listener, listenErr = unixtransport.NewListener(path)
		if listenErr != nil {
			return listenErr
		}
		go func() {
			conn, acceptErr := listener.Accept(context.Background())
			if acceptErr != nil {
				return
			}
			close(accepted)
			<-releaseServer
			_ = conn.Close()
		}()
		return nil
	}
	t.Cleanup(func() {
		startV3Daemon = previousStart
		close(releaseServer)
		if listener != nil {
			_ = listener.Close()
		}
	})

	started := time.Now()
	err = executePairCommandError(created,
		"--timeout", "100ms", "--socket", hungSocket,
		"pair", "import", "--id", "lab", "--registry", registryPath, "-",
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pair import stalled Hello error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("pair import ignored root timeout: %s", elapsed)
	}
	select {
	case <-accepted:
	default:
		t.Fatal("pair import did not reach the stalled daemon Hello path")
	}

	assertPairImportLocksReleased(t, stateHome, registryPath, "lab", bundle)
}

func TestPairImportRootTimeoutCancelsPairingSocketDialAndReleasesLocks(t *testing.T) {
	runtimeDir, stateHome, configHome := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	created := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw")
	bundle, _, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	savePairTestManagedEndpoint(t, registryPath, "lab", bundle, endpointdomain.RelayAuto)

	previousDial := dialV3PairingTransport
	dialStarted := make(chan struct{})
	dialV3PairingTransport = func(ctx context.Context, _ string) (*unixtransport.Transport, error) {
		close(dialStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(func() { dialV3PairingTransport = previousDial })

	started := time.Now()
	err = executePairCommandError(created,
		"--timeout", "100ms", "pair", "import", "--id", "lab", "--registry", registryPath,
		"--pair-socket", filepath.Join(runtimeDir, "stalled.pair.sock"), "-",
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pair import stalled PairingExchange dial error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("PairingExchange dial ignored root timeout: %s", elapsed)
	}
	select {
	case <-dialStarted:
	default:
		t.Fatal("pair import did not reach the stalled PairingExchange dial")
	}
	assertPairImportLocksReleased(t, stateHome, registryPath, "lab", bundle)
}

func assertPairImportLocksReleased(t *testing.T, stateHome string, registryPath string, endpointID endpointdomain.EndpointID, bundle *remoteauth.PairingBundle) {
	t.Helper()
	lockCtx, cancelLock := context.WithTimeout(context.Background(), 250*time.Millisecond)
	registryLock, lockErr := filelock.AcquireContext(lockCtx, registryPath+".lock", false)
	cancelLock()
	if lockErr != nil {
		t.Fatalf("registry lock remained held after timeout: %v", lockErr)
	}
	if err := registryLock.Close(); err != nil {
		t.Fatal(err)
	}

	grantRef := v3PairingGrantRef(endpointID, bundle.GetIdentity().GetDeviceId())
	credentialStore := remoteauth.NewCredentialStore(filepath.Join(stateHome, "termx", "remote-v2", "credentials"))
	credentialCtx, cancelCredential := context.WithTimeout(context.Background(), 250*time.Millisecond)
	_, credentialErr := credentialStore.ResolveContext(credentialCtx, grantRef)
	cancelCredential()
	if errors.Is(credentialErr, context.DeadlineExceeded) {
		t.Fatalf("credential ref lock remained held after timeout: %v", credentialErr)
	}
}

func TestPairCreateRejectsPipeWithoutExplicitOutputAndRemoteTerminalScope(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	if id, err := localPairTerminalID("local:term-1"); err != nil || id != "term-1" {
		t.Fatalf("local terminal scope = (%q, %v)", id, err)
	}
	if _, err := localPairTerminalID("west:term-1"); cliExitCode(err) != 2 {
		t.Fatalf("remote terminal scope error = %v", err)
	}
	previousTerminal := v3PairOutputIsTerminal
	v3PairOutputIsTerminal = func(io.Writer) bool { return false }
	defer func() { v3PairOutputIsTerminal = previousTerminal }()
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create"})
	if err := command.Execute(); cliExitCode(err) != 2 || !strings.Contains(err.Error(), "use --raw") {
		t.Fatalf("non-terminal create error = %v", err)
	}
}

func configurePairCommandTest(t *testing.T) (string, string, string) {
	t.Helper()
	runtimeDir := t.TempDir()
	stateHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	clientAccess, err := loadV3ClientAccessRuntime(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	closePairing, err := startV3PairingListener(ctx, clientAccess, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := newCoreV2TestServer(
		corev2.WithSocketPath(socket), corev2.WithHistoryDisabled(),
		corev2.WithClientAccessService(v3ClientAccessService{identity: clientAccess.Identity, store: clientAccess.Store}),
	)
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx) }()
	if err := waitForSocket(socket, time.Second, func() error {
		client, dialErr := v3DialClient(socket)
		if dialErr == nil {
			_ = client.Close()
		}
		return dialErr
	}); err != nil {
		cancel()
		closePairing()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		closePairing()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		_ = clientAccess.Close()
	})
	return runtimeDir, stateHome, configHome
}

func savePairTestManagedEndpoint(t *testing.T, registryPath string, endpointID endpointdomain.EndpointID, bundle *remoteauth.PairingBundle, relayMode endpointdomain.RelayMode) {
	t.Helper()
	identity := endpointdomain.DaemonIdentity{
		DeviceID: bundle.GetIdentity().GetDeviceId(), DeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint(),
	}
	endpoint := endpointdomain.NewManagedEndpoint(endpointID, "Existing managed endpoint", identity, identity.DeviceID, "", relayMode, endpointdomain.ConnectOnDemand)
	if err := endpointdomain.Save(registryPath, endpointdomain.Registry{
		Version: endpointdomain.RegistryVersion, Default: endpointID, Endpoints: map[endpointdomain.EndpointID]endpointdomain.Endpoint{endpointID: endpoint},
	}); err != nil {
		t.Fatal(err)
	}
}

func executePairCommand(t *testing.T, stdin []byte, args ...string) []byte {
	t.Helper()
	var output bytes.Buffer
	command := newRootCmd()
	if stdin != nil {
		command.SetIn(bytes.NewReader(stdin))
	}
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func executePairCommandError(stdin []byte, args ...string) error {
	command := newRootCmd()
	command.SetIn(bytes.NewReader(stdin))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	return command.Execute()
}
