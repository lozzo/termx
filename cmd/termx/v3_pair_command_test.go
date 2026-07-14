package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/remoteauth"
)

func TestPairCreateAndImportKeepsGrantOutOfRegistry(t *testing.T) {
	stateHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var created bytes.Buffer
	create := newRootCmd()
	create.SetOut(&created)
	create.SetErr(io.Discard)
	create.SetArgs([]string{"pair", "create", "--raw", "--label", "Lab daemon", "--ttl", "1h"})
	if err := create.Execute(); err != nil {
		t.Fatal(err)
	}
	bundle, claims, err := remoteauth.ParsePairingBundle(created.Bytes(), time.Now().UTC())
	if err != nil || !claims.Scope.AllowDaemon || bundle.DeviceID == "" {
		t.Fatalf("created pairing bundle = (%#v, %#v, %v)", bundle, claims, err)
	}

	registryPath := filepath.Join(configHome, "termx", "connections.yaml")
	var imported bytes.Buffer
	importCommand := newRootCmd()
	importCommand.SetIn(bytes.NewReader(created.Bytes()))
	importCommand.SetOut(&imported)
	importCommand.SetErr(io.Discard)
	importCommand.SetArgs([]string{"pair", "import", "--id", "lab", "--registry", registryPath, "--relay", "direct", "-"})
	if err := importCommand.Execute(); err != nil {
		t.Fatal(err)
	}
	registryPayload, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registryPayload), bundle.CapabilityGrant) || strings.Contains(string(registryPayload), "hub_url") {
		t.Fatalf("registry leaked grant or caller Hub assignment: %s", registryPayload)
	}
	registry, err := connection.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := registry.Connections["lab"]
	if cfg.HubDeviceID != bundle.DeviceID || cfg.DeviceFingerprint != bundle.DeviceFingerprint || cfg.RelayMode != connection.RelayDirect || cfg.GrantRef == "" {
		t.Fatalf("imported endpoint = %#v", cfg)
	}
	stored, err := remoteauth.NewCredentialStore(v3RemoteCredentialDir()).Resolve(cfg.GrantRef)
	if err != nil || stored != bundle.CapabilityGrant {
		t.Fatalf("stored grant mismatch: err=%v", err)
	}
	if !strings.Contains(imported.String(), "Imported managed endpoint lab") || strings.Contains(imported.String(), bundle.CapabilityGrant) {
		t.Fatalf("import output leaked bearer or lost result: %q", imported.String())
	}
}

func TestPairCreateWritesOwnerOnlyBundle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "pair", "bundle.json")
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"pair", "create", "--out", path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("pairing bundle mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestPairCreateDefaultsLabelToDaemonHostname(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	previousHostname := v3PairHostname
	v3PairHostname = func() (string, error) { return "build-daemon", nil }
	defer func() { v3PairHostname = previousHostname }()

	var output bytes.Buffer
	command := newRootCmd()
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"pair", "create", "--raw"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	bundle, _, err := remoteauth.ParsePairingBundle(output.Bytes(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Label != "build-daemon" {
		t.Fatalf("pairing label = %q, want daemon hostname", bundle.Label)
	}
}

func TestPairInspectRedactsBearerGrant(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var created bytes.Buffer
	create := newRootCmd()
	create.SetOut(&created)
	create.SetErr(io.Discard)
	create.SetArgs([]string{"pair", "create", "--raw", "--label", "Inspect daemon", "--ttl", "1h"})
	if err := create.Execute(); err != nil {
		t.Fatal(err)
	}
	bundle, _, err := remoteauth.ParsePairingBundle(created.Bytes(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	inspect := newRootCmd()
	var output bytes.Buffer
	inspect.SetIn(bytes.NewReader(created.Bytes()))
	inspect.SetOut(&output)
	inspect.SetErr(io.Discard)
	inspect.SetArgs([]string{"pair", "inspect", "--json", "-"})
	if err := inspect.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"kind":"pairing_bundle"`) || !strings.Contains(output.String(), `"device_id"`) || !strings.Contains(output.String(), `"scope"`) {
		t.Fatalf("inspect lost public metadata: %s", output.String())
	}
	if strings.Contains(output.String(), bundle.CapabilityGrant) || strings.Contains(output.String(), "capability_grant") {
		t.Fatalf("inspect leaked bearer grant: %s", output.String())
	}
}

func TestPairCreateDefaultsToTerminalQRAndRequiresExplicitRawForPipes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	previousTerminal := v3PairOutputIsTerminal
	defer func() { v3PairOutputIsTerminal = previousTerminal }()

	var qrOutput bytes.Buffer
	v3PairOutputIsTerminal = func(io.Writer) bool { return true }
	qrCommand := newRootCmd()
	qrCommand.SetOut(&qrOutput)
	qrCommand.SetErr(io.Discard)
	qrCommand.SetArgs([]string{"pair", "create", "--ttl", "5m"})
	if err := qrCommand.Execute(); err != nil {
		t.Fatal(err)
	}
	if output := qrOutput.String(); !strings.Contains(output, "Scan with the TermX App") || !strings.Contains(output, "\x1b[40m") || strings.Contains(output, `"capability_grant"`) {
		t.Fatalf("terminal QR output = %q", output)
	}

	v3PairOutputIsTerminal = func(io.Writer) bool { return false }
	pipeCommand := newRootCmd()
	pipeCommand.SetOut(io.Discard)
	pipeCommand.SetErr(io.Discard)
	pipeCommand.SetArgs([]string{"pair", "create"})
	err := pipeCommand.Execute()
	if cliExitCode(err) != 2 || !strings.Contains(err.Error(), "use --raw") {
		t.Fatalf("non-terminal create error = %v", err)
	}
}

func TestPairCreateRejectsRawAndFileOutputTogether(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"pair", "create", "--raw", "--out", filepath.Join(t.TempDir(), "bundle.json")})
	if err := command.Execute(); cliExitCode(err) != 2 {
		t.Fatalf("raw/out conflict = %v", err)
	}
}

func TestPairCreateTerminalScopeAcceptsOnlyLocalTerminalRef(t *testing.T) {
	if id, err := localPairTerminalID("local:term-1"); err != nil || id != "term-1" {
		t.Fatalf("local terminal scope = (%q, %v)", id, err)
	}
	if _, err := localPairTerminalID("west:term-1"); cliExitCode(err) != 2 {
		t.Fatalf("remote terminal scope error = %v", err)
	}
}

func TestPairImportRestoresExistingCredentialWhenRegistryWriteFails(t *testing.T) {
	stateHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	registryPath := filepath.Join(configHome, "termx", "connections.yaml")

	createBundle := func() []byte {
		var output bytes.Buffer
		command := newRootCmd()
		command.SetOut(&output)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"pair", "create", "--raw", "--ttl", "1h"})
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	importBundle := func(payload []byte) error {
		command := newRootCmd()
		command.SetIn(bytes.NewReader(payload))
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"pair", "import", "--id", "lab", "--registry", registryPath, "-"})
		return command.Execute()
	}

	firstBundle := createBundle()
	if err := importBundle(firstBundle); err != nil {
		t.Fatal(err)
	}
	registry, err := connection.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	grantRef := registry.Connections["lab"].GrantRef
	credentials := remoteauth.NewCredentialStore(v3RemoteCredentialDir())
	firstGrant, err := credentials.Resolve(grantRef)
	if err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("injected registry write failure")
	previousSave := saveV3ConnectionRegistry
	saveV3ConnectionRegistry = func(string, connection.Registry) error { return wantErr }
	defer func() { saveV3ConnectionRegistry = previousSave }()
	if err := importBundle(createBundle()); !errors.Is(err, wantErr) {
		t.Fatalf("pair import error = %v, want injected write failure", err)
	}
	stored, err := credentials.Resolve(grantRef)
	if err != nil || stored != firstGrant {
		t.Fatalf("failed registry write lost prior credential: stored=%q err=%v", stored, err)
	}
}
