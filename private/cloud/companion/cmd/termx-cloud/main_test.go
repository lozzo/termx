package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
