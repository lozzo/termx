package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/activation"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
)

func TestSmokeServerCompletesPublicHelloStatusAndShutdown(t *testing.T) {
	runtimeDir, err := os.MkdirTemp("/tmp", "termx-cloud-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtimeDir)
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(runtimeDir, "cloud-companion.sock")
	runDone := make(chan error, 1)
	go func() {
		runDone <- run(context.Background(), []string{"serve", "--socket", endpoint, "--smoke"}, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var client *ipc.Client
	for {
		client, err = ipc.Dial(deadline, endpoint)
		if err == nil {
			break
		}
		if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) && !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING) {
			t.Fatal(err)
		}
		select {
		case runErr := <-runDone:
			t.Fatalf("smoke Companion exited before ready: %v", runErr)
		case <-deadline.Done():
			t.Fatal("smoke Companion did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer client.Close()
	hello, err := client.Hello(deadline, &cloudpb.CompanionHelloRequest{
		ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax, TermxVersion: "test", CallerRole: cloudpb.CallerRole_CALLER_ROLE_CLI,
		RequestedCapabilities: []cloudpb.CompanionCapability{cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION},
		RequestNonce:          bytes.Repeat([]byte{1}, 32),
	})
	if err != nil || hello.GetCompanionVersion() != companionVersion {
		t.Fatalf("Hello = (%v, %v)", hello, err)
	}
	status, err := client.Status(deadline, &cloudpb.StatusRequest{})
	if err != nil || status.GetState() != cloudpb.CompanionState_COMPANION_STATE_LOGIN_REQUIRED {
		t.Fatalf("Status = (%v, %v)", status, err)
	}
	if _, err := client.Shutdown(deadline, &cloudpb.ShutdownRequest{Reason: "test_complete"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("smoke Companion did not shut down")
	}
}

func TestVersionCommandIsMachineReadable(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(companionVersion)) || !bytes.Contains(stdout.Bytes(), []byte(buildChannel)) {
		t.Fatalf("version output = %s", stdout.String())
	}
}

func TestCloudAdapterDefaultsToFailClosed(t *testing.T) {
	runtimeConfiguration, err := cloudRuntimeConfigurationFor("", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeConfiguration.controlPlane.(*cloudservice.UnconfiguredAdapter); !ok {
		t.Fatalf("default Control Plane adapter = %T", runtimeConfiguration.controlPlane)
	}
	if _, ok := runtimeConfiguration.hub.(*cloudservice.UnconfiguredAdapter); !ok {
		t.Fatalf("default Hub adapter = %T", runtimeConfiguration.hub)
	}
	if runtimeConfiguration.allowPublicHTTPLoginURL {
		t.Fatal("default runtime allowed a public HTTP login URL")
	}
}

func TestExplicitDevelopmentManifestSelectsNetworkAdapter(t *testing.T) {
	manifestPath := writeDevManifest(t)
	runtimeConfiguration, err := cloudRuntimeConfigurationFor(manifestPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeConfiguration.controlPlane.(*httpapi.Adapter); !ok {
		t.Fatalf("dev Control Plane adapter = %T", runtimeConfiguration.controlPlane)
	}
	if _, ok := runtimeConfiguration.hub.(*httpapi.Adapter); !ok {
		t.Fatalf("dev Hub adapter = %T", runtimeConfiguration.hub)
	}
}

func TestEmbeddedDevelopmentManifestSelectsNetworkAdapterWithoutRuntimeFile(t *testing.T) {
	manifestPath := writeDevManifest(t)
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var embeddedManifest httpapi.Manifest
	if err := json.Unmarshal(payload, &embeddedManifest); err != nil {
		t.Fatal(err)
	}
	embeddedManifest.Profile = httpapi.ProfileStagingPublicHTTP
	embeddedManifest.ControlPlaneURL = "http://114.66.58.243:41101"
	embeddedManifest.HubURL = "http://114.66.58.243:41102"
	embeddedManifest.RelayURL = "turn:114.66.58.243:41003?transport=udp"
	payload, err = json.Marshal(embeddedManifest)
	if err != nil {
		t.Fatal(err)
	}
	originalEmbedded := embeddedDevelopmentManifestBase64
	embeddedDevelopmentManifestBase64 = base64.RawStdEncoding.EncodeToString(payload)
	t.Cleanup(func() { embeddedDevelopmentManifestBase64 = originalEmbedded })
	runtimeConfiguration, err := cloudRuntimeConfigurationFor("", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeConfiguration.controlPlane.(*httpapi.Adapter); !ok {
		t.Fatalf("embedded Control Plane adapter = %T", runtimeConfiguration.controlPlane)
	}
	if _, ok := runtimeConfiguration.hub.(*httpapi.Adapter); !ok {
		t.Fatalf("embedded Hub adapter = %T", runtimeConfiguration.hub)
	}
	if !runtimeConfiguration.allowPublicHTTPLoginURL {
		t.Fatal("embedded staging-public-http runtime did not allow its login URL")
	}
	if _, err := cloudRuntimeConfigurationFor(manifestPath, false); err == nil {
		t.Fatal("runtime manifest overrode embedded development manifest")
	}
	runtimeConfiguration, err = cloudRuntimeConfigurationFor("", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeConfiguration.controlPlane.(*cloudservice.UnconfiguredAdapter); !ok {
		t.Fatalf("smoke Control Plane adapter = %T", runtimeConfiguration.controlPlane)
	}
	if _, ok := runtimeConfiguration.hub.(*cloudservice.UnconfiguredAdapter); !ok {
		t.Fatalf("smoke Hub adapter = %T", runtimeConfiguration.hub)
	}
	if runtimeConfiguration.allowPublicHTTPLoginURL {
		t.Fatal("installer smoke allowed a public HTTP login URL")
	}
}

func TestProductionAndSmokeRejectDevelopmentManifest(t *testing.T) {
	manifestPath := writeDevManifest(t)
	originalChannel := buildChannel
	buildChannel = "stable"
	t.Cleanup(func() { buildChannel = originalChannel })
	if _, err := cloudRuntimeConfigurationFor(manifestPath, false); err == nil {
		t.Fatal("stable build accepted dev cloud manifest")
	}
	buildChannel = "development"
	if _, err := cloudRuntimeConfigurationFor(manifestPath, true); err == nil {
		t.Fatal("installer smoke accepted dev cloud manifest")
	}
}

func TestLicensesCommandPrintsEmbeddedThirdPartyNotices(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"licenses"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("TermX Cloud Companion Third-Party Notices")) ||
		!bytes.Contains(stdout.Bytes(), []byte("github.com/zalando/go-keyring")) {
		t.Fatalf("licenses output does not contain embedded notices: %s", stdout.String())
	}
}

func TestBuiltArtifactPassesPublicActivationSmoke(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "termx-cloud")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build artifact: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	manifest := installer.Manifest{
		Version: companionVersion, Channel: buildChannel,
		MinCompanionProtocol: cloudcompanion.ProtocolVersionMin,
		MaxCompanionProtocol: cloudcompanion.ProtocolVersionMax,
	}
	if err := activation.SmokeFunc("artifact-test")(ctx, binaryPath, manifest); err != nil {
		t.Fatal(err)
	}
}

func writeDevManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	manifest := httpapi.Manifest{
		Version: httpapi.ManifestVersion, Profile: httpapi.ProfileDevLocal,
		ControlPlaneURL: "http://127.0.0.1:41001", HubURL: "http://127.0.0.1:41002",
		RelayURL: "turn:127.0.0.1:41003?transport=udp",
		HubID:    "hub-dev", Region: "local-1", AccountLabel: "Dev Account",
		EnrollmentCode: "enroll-dev", StartedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
	if err := json.NewEncoder(file).Encode(manifest); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
