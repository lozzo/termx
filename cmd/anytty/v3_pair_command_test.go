package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	endpointdomain "github.com/anytty/anytty/client/endpoint"
	corev2 "github.com/anytty/anytty/core"
	"github.com/anytty/anytty/shared/filelock"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/anytty/anytty/shared/securefs"
	unixtransport "github.com/anytty/anytty/shared/transport/unix"
	qrcode "github.com/skip2/go-qrcode"
)

func TestPairCreateAndImportUsesTicketThenClientBoundCredential(t *testing.T) {
	runtimeDir, stateHome, configHome := configurePairCommandTest(t)
	created := executePairCommand(t, nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--raw", "--route", "direct", "--label", "Lab daemon", "--ttl", "1h", "--grant-ttl", "24h")
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
	registryPath := filepath.Join(configHome, "anytty", endpointdomain.DefaultFileName)
	savePairTestManagedEndpoint(t, registryPath, "lab", bundle, endpointdomain.RelayDirect)
	pairSocket := filepath.Join(runtimeDir, "daemon.sock.pair")
	imported := executePairCommand(t, created, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", pairSocket, "-")
	registryPayload, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(registryPayload, created) || strings.Contains(string(registryPayload), "capability_grant") || strings.Contains(string(registryPayload), "anytty-grant-v2") {
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
	credential, err := remoteauth.NewCredentialStore(filepath.Join(stateHome, "anytty", "remote-v2", "credentials")).Resolve(route.CredentialRef)
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

func TestPairImportAddsAdvertisedDirectRouteToExistingSSHEndpoint(t *testing.T) {
	runtimeDir, _, configHome := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	created := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw", "--route", "direct", "--label", "Daemon label")
	_, _, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(configHome, "anytty", endpointdomain.DefaultFileName)
	ssh := endpointdomain.NewSSHEndpoint("lab", "Existing SSH label", "lab.example", "ssh:lab", "127.0.0.1:41120", "127.0.0.1:41121", endpointdomain.ConnectOnDemand)
	if err := endpointdomain.Save(registryPath, endpointdomain.Registry{Version: endpointdomain.RegistryVersion, Default: "lab", Endpoints: map[endpointdomain.EndpointID]endpointdomain.Endpoint{"lab": ssh}}); err != nil {
		t.Fatal(err)
	}
	imported := executePairCommand(t, created, "pair", "import", "--id", "lab", "--registry", registryPath, "--pair-socket", v3PairingSocketPath(socket), "-")
	if !strings.Contains(string(imported), "Paired endpoint lab") {
		t.Fatalf("pair import output = %q", imported)
	}
	registry, err := endpointdomain.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := registry.Endpoints["lab"]
	if len(registry.Endpoints) != 1 || endpoint.Label != "Existing SSH label" || endpoint.DaemonIdentity.Empty() {
		t.Fatalf("pairing import did not bind the existing endpoint: %#v", registry)
	}
	if _, ok := endpoint.Route("ssh"); !ok {
		t.Fatal("pair import deleted existing SSH route")
	}
	if _, ok := endpoint.Route("cloud"); ok {
		t.Fatalf("pair import invented managed route: %#v", endpoint)
	}
	direct, ok := endpoint.Route("direct")
	if !ok || direct.Kind != endpointdomain.RouteDirectWebRTCTCP || direct.CredentialRef == "" {
		t.Fatalf("pair import did not add the advertised Direct route: %#v", endpoint)
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
	previousSize := v3PairTerminalSize
	v3PairOutputIsTerminal = func(io.Writer) bool { return true }
	v3PairTerminalSize = func(io.Writer) (int, int, bool) { return 240, 120, true }
	defer func() {
		v3PairOutputIsTerminal = previousTerminal
		v3PairTerminalSize = previousSize
	}()
	qr := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--ttl", "5m")
	if !strings.Contains(string(qr), "Scan with the AnyTTY App") || !strings.Contains(string(qr), "\x1b[40m") || strings.Contains(string(qr), `"pairing_ticket"`) {
		t.Fatalf("terminal QR output = %q", qr)
	}
	assertTerminalQRUsesSquareCells(t, qr)
}

func TestPairCreateTextAndPNGOutputsArePortableAndOwnerOnly(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	textOutput := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--text")
	portableURI := strings.TrimSpace(string(textOutput))
	if !strings.HasPrefix(portableURI, remoteauth.PairingClaimCodePrefix) {
		t.Fatalf("pair text output = %q", portableURI)
	}
	payload, err := readV3PairingBundle(context.Background(), strings.NewReader(portableURI), "-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteauth.ParsePairingClaimOffer(payload, time.Now().UTC()); err != nil {
		t.Fatalf("portable pairing URI is invalid: %v", err)
	}
	code, err := qrcode.New(portableURI, qrcode.Low)
	if err != nil || code.VersionNumber > 10 {
		t.Fatalf("pairing claim QR version = %v err=%v", code.VersionNumber, err)
	}
	medium, err := qrcode.New(portableURI, qrcode.Medium)
	if err != nil {
		t.Fatal(err)
	}
	if code.VersionNumber >= medium.VersionNumber {
		t.Fatalf("Low QR version=%d Medium version=%d", code.VersionNumber, medium.VersionNumber)
	}

	path := filepath.Join(t.TempDir(), "pairing", "anytty-pair.png")
	output := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--qr-file", path)
	if strings.Contains(string(output), "\t") ||
		!strings.Contains(string(output), "Fingerprint") ||
		!strings.Contains(string(output), "Routes") ||
		!strings.Contains(string(output), "DETAILS") ||
		!strings.Contains(string(output), "Pairing QR PNG written to") {
		t.Fatalf("pair PNG output = %q", output)
	}
	info, err := os.Stat(path)
	if err != nil || !securefs.IsPrivateFile(path, info) {
		t.Fatalf("pairing PNG permissions = %v err=%v", info, err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	configuration, err := png.DecodeConfig(file)
	if err != nil || configuration.Width != configuration.Height || configuration.Width < 512 {
		t.Fatalf("pairing PNG configuration = %#v err=%v", configuration, err)
	}
}

func TestPairCreateCommandEmitsDirectlyImportableInlineClaim(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	commandOutput := strings.TrimSpace(string(executePairCommand(t, nil, "--socket", socket, "pair", "create", "--command")))
	if !strings.HasPrefix(commandOutput, "anytty pair import --id '") {
		t.Fatalf("pair command output = %q", commandOutput)
	}
	match := regexp.MustCompile(`MXP1-[A-Za-z0-9_-]+`).FindString(commandOutput)
	if match == "" {
		t.Fatalf("pair command has no inline claim: %q", commandOutput)
	}
	payload, err := readV3PairingBundle(context.Background(), nil, match)
	if err != nil {
		t.Fatalf("read inline claim: %v", err)
	}
	offer, err := remoteauth.ParsePairingClaimOffer(payload, time.Now().UTC())
	if err != nil {
		t.Fatalf("parse inline claim: %v", err)
	}
	if !strings.Contains(commandOutput, "--id '"+offer.GetDeviceId()+"'") {
		t.Fatalf("pair command does not bind the advertised device ID: %q", commandOutput)
	}
	legacyURI := pairingBootstrapURIPrefix + base64.RawURLEncoding.EncodeToString(payload)
	if decoded, err := readV3PairingBundle(context.Background(), nil, legacyURI); err != nil || !bytes.Equal(decoded, payload) {
		t.Fatalf("read bootstrap URI: equal=%v err=%v", bytes.Equal(decoded, payload), err)
	}
}

func TestPairCreateRejectsSmallTerminalBeforeWritingPartialQR(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	previousTerminal := v3PairOutputIsTerminal
	previousSize := v3PairTerminalSize
	v3PairOutputIsTerminal = func(io.Writer) bool { return true }
	v3PairTerminalSize = func(io.Writer) (int, int, bool) { return 80, 24, true }
	defer func() {
		v3PairOutputIsTerminal = previousTerminal
		v3PairTerminalSize = previousSize
	}()

	var output bytes.Buffer
	command := newRootCmd()
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create"})
	err := command.Execute()
	if cliExitCode(err) != 2 || !strings.Contains(err.Error(), "--qr-file") {
		t.Fatalf("small terminal error = %v code=%d", err, cliExitCode(err))
	}
	if output.Len() != 0 {
		t.Fatalf("small terminal wrote partial output: %q", output.Bytes())
	}
}

func TestPairCreateRejectsConflictingOutputModes(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	err := executePairCommandError(nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--text", "--qr-file", filepath.Join(t.TempDir(), "pair.png"))
	if cliExitCode(err) != 2 {
		t.Fatalf("conflicting output mode error = %v code=%d", err, cliExitCode(err))
	}
	err = executePairCommandError(nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--command", "--text")
	if cliExitCode(err) != 2 {
		t.Fatalf("command/text output mode error = %v code=%d", err, cliExitCode(err))
	}
}

func TestPairCreatePublishesExplicitTCPMappingWithoutChangingIdentity(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	base := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw")
	mapped := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw",
		"--route", "direct", "--signaling-address", "frp.example:51020", "--ice-tcp-address", "frp.example:51021", "--server-name", "frp.example")
	baseBundle, _, err := remoteauth.ParsePairingBundle(base, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mappedBundle, _, err := remoteauth.ParsePairingBundle(mapped, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if baseBundle.GetIdentity().GetDeviceId() != mappedBundle.GetIdentity().GetDeviceId() ||
		baseBundle.GetIdentity().GetDeviceFingerprint() != mappedBundle.GetIdentity().GetDeviceFingerprint() {
		t.Fatalf("locator override changed daemon identity: base=%v mapped=%v", baseBundle.GetIdentity(), mappedBundle.GetIdentity())
	}
	direct := mappedBundle.GetRoutes()[0].GetDirectWebrtcTcp()
	if got := direct.GetSignalingAddresses(); len(got) != 1 || got[0] != "frp.example:51020" {
		t.Fatalf("mapped signaling addresses = %#v", got)
	}
	if got := direct.GetIceTcpAddresses(); len(got) != 1 || got[0] != "frp.example:51021" {
		t.Fatalf("mapped ICE-TCP addresses = %#v", got)
	}
	if direct.GetServerName() != "frp.example" || len(direct.GetAdvertisedAddresses()) != 2 {
		t.Fatalf("mapped Direct route = %#v", direct)
	}
	inspect := executePairCommand(t, mapped, "pair", "inspect", "--json", "-")
	if !strings.Contains(string(inspect), `"signaling_addresses":["frp.example:51020"]`) || !strings.Contains(string(inspect), `"server_name":"frp.example"`) {
		t.Fatalf("mapped inspect preview = %s", inspect)
	}
}

func TestPairCreateDefaultsToDirectOnly(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	releaseRecord, err := acquireDaemonRuntimeRecord(socket, filepath.Join(runtimeDir, "daemon.log"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRecord()
	created := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw")
	bundle, _, err := remoteauth.ParsePairingBundle(created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.GetRoutes()) != 1 || bundle.GetRoutes()[0].GetDirectWebrtcTcp() == nil {
		t.Fatalf("pairing routes = %#v", bundle.GetRoutes())
	}
	inspect := executePairCommand(t, created, "pair", "inspect", "--json", "-")
	if !strings.Contains(string(inspect), `"kind":"direct-webrtc-tcp"`) || strings.Contains(string(inspect), `managed-webrtc`) {
		t.Fatalf("Direct pair inspect = %s", inspect)
	}
	directOnly := executePairCommand(t, nil, "--socket", socket, "pair", "create", "--raw", "--route", "direct")
	directBundle, _, err := remoteauth.ParsePairingBundle(directOnly, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(directBundle.GetRoutes()) != 1 || directBundle.GetRoutes()[0].GetDirectWebrtcTcp() == nil {
		t.Fatalf("explicit Direct pairing routes = %#v", directBundle.GetRoutes())
	}
}

func TestPairCreateRejectsPartialOrUnreachableAddressOverride(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	socket := filepath.Join(runtimeDir, "daemon.sock")
	for _, args := range [][]string{
		{"--route", "direct", "--signaling-address", "frp.example:51020"},
		{"--route", "direct", "--signaling-address", "0.0.0.0:51020", "--ice-tcp-address", "frp.example:51021"},
		{"--route", "direct", "--signaling-address", "frp.example:bad", "--ice-tcp-address", "frp.example:51021"},
	} {
		commandArgs := append([]string{"--socket", socket, "pair", "create", "--raw"}, args...)
		if err := executePairCommandError(nil, commandArgs...); cliExitCode(err) != 2 {
			t.Fatalf("pair create args=%v error=%v code=%d", args, err, cliExitCode(err))
		}
	}
}

func assertTerminalQRUsesSquareCells(t *testing.T, output []byte) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("terminal QR output has %d lines", len(lines))
	}
	firstQR, lastQR := -1, -1
	for index, line := range lines {
		if strings.Contains(line, "This QR contains") {
			break
		}
		if strings.Contains(line, "\x1b[") {
			if firstQR < 0 {
				firstQR = index
			}
			lastQR = index
		}
	}
	if firstQR < 0 || lastQR < firstQR {
		t.Fatalf("terminal QR rows are missing from %q", output)
	}
	qrLines := lines[firstQR : lastQR+1]
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	wantColumns := 2*len(qrLines) - 1
	for index, line := range qrLines {
		columns := utf8.RuneCountInString(ansi.ReplaceAllString(line, ""))
		if columns != wantColumns {
			t.Fatalf("terminal QR row %d uses %d columns for %d packed rows; want %d", index, columns, len(qrLines), wantColumns)
		}
	}
}

func TestPairCreateWritesOwnerOnlyTicketBundle(t *testing.T) {
	runtimeDir, _, _ := configurePairCommandTest(t)
	path := filepath.Join(t.TempDir(), "pair", "bundle.json")
	executePairCommand(t, nil, "--socket", filepath.Join(runtimeDir, "daemon.sock"), "pair", "create", "--out", path)
	info, err := os.Stat(path)
	if err != nil || !securefs.IsPrivateFile(path, info) {
		t.Fatalf("pairing bundle permissions = %v err=%v", info, err)
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
	registryPath := filepath.Join(configHome, "anytty", endpointdomain.DefaultFileName)
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
	registryPath := filepath.Join(configHome, "anytty", endpointdomain.DefaultFileName)
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
	registryPath := filepath.Join(configHome, "anytty", endpointdomain.DefaultFileName)
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
	registryPath := filepath.Join(configHome, "anytty", endpointdomain.DefaultFileName)
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
	credentialStore := remoteauth.NewCredentialStore(filepath.Join(stateHome, "anytty", "remote-v2", "credentials"))
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
	if err := command.Execute(); cliExitCode(err) != 2 || !strings.Contains(err.Error(), "--text") {
		t.Fatalf("non-terminal create error = %v", err)
	}
}

func configurePairCommandTest(t *testing.T) (string, string, string) {
	t.Helper()
	t.Setenv("ANYTTY_DIRECT_SIGNALING_LISTEN", "127.0.0.1:41120")
	t.Setenv("ANYTTY_DIRECT_ICE_TCP_LISTEN", "127.0.0.1:41121")
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
