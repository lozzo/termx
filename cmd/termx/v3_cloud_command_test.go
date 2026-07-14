package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
)

func TestCloudHelpExposesCanonicalNodeAndCompanionNamespaces(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{args: []string{"cloud", "--help"}, want: []string{"node", "companion"}},
		{args: []string{"cloud", "node", "--help"}, want: []string{"enroll"}},
		{args: []string{"cloud", "companion", "--help"}, want: []string{"install", "update", "status", "uninstall"}},
	} {
		command := newRootCmd()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(io.Discard)
		command.SetArgs(test.args)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, expected := range test.want {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("termx %s help missing %q: %s", strings.Join(test.args, " "), expected, output.String())
			}
		}
	}
}

func TestCloudInstallUsesSignedInstallerRequest(t *testing.T) {
	cloudInstaller := &fakeCloudInstaller{}
	previousFactory := newV3CloudInstallerForCommand
	newV3CloudInstallerForCommand = func() (v3CloudInstaller, error) { return cloudInstaller, nil }
	defer func() { newV3CloudInstallerForCommand = previousFactory }()

	var output bytes.Buffer
	command := newRootCmd()
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"cloud", "install", "--channel", "beta", "--version", "v1.2.3"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if cloudInstaller.installRequest.Channel != "beta" || cloudInstaller.installRequest.Version != "v1.2.3" {
		t.Fatalf("install request = %#v", cloudInstaller.installRequest)
	}
	if !strings.Contains(output.String(), "v1.2.3") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestCloudStatusMissingCompanionExplainsOptionalInstallWithoutUsage(t *testing.T) {
	cloudInstaller := &fakeCloudInstaller{statusErr: cloudcompanion.NewError(
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING,
		"Cloud Companion is not installed",
	)}
	previousFactory := newV3CloudInstallerForCommand
	newV3CloudInstallerForCommand = func() (v3CloudInstaller, error) { return cloudInstaller, nil }
	defer func() { newV3CloudInstallerForCommand = previousFactory }()

	var stderr bytes.Buffer
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"cloud", "status", "--json"})
	err := command.Execute()
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) {
		t.Fatalf("status error = %v, want COMPANION_MISSING", err)
	}
	for _, want := range []string{"Cloud is optional", "not bundled", "termx cloud install", "local and SSH"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("status error must contain %q: %v", want, err)
		}
	}
	if stderr.Len() != 0 || !command.SilenceErrors || !command.SilenceUsage {
		t.Fatalf("runtime error must not duplicate Cobra error/Usage: stderr=%q silence=(%v,%v)", stderr.String(), command.SilenceErrors, command.SilenceUsage)
	}
}

func TestCloudStatusSourceBuildExplainsOfficialReleaseRequirement(t *testing.T) {
	previousKeyID := cloudReleaseRootKeyID
	previousPublicKey := cloudReleaseRootPublicKey
	previousFactory := newV3CloudInstallerForCommand
	cloudReleaseRootKeyID = ""
	cloudReleaseRootPublicKey = ""
	newV3CloudInstallerForCommand = func() (v3CloudInstaller, error) { return newV3CloudInstaller() }
	defer func() {
		cloudReleaseRootKeyID = previousKeyID
		cloudReleaseRootPublicKey = previousPublicKey
		newV3CloudInstallerForCommand = previousFactory
	}()

	var stderr bytes.Buffer
	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(&stderr)
	command.SetArgs([]string{"cloud", "status", "--json"})
	err := command.Execute()
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("source status error = %v, want COMPANION_UNTRUSTED", err)
	}
	for _, want := range []string{"Cloud is optional", "source build", "official termx release", "termx cloud install", "local and SSH"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("source status error must contain %q: %v", want, err)
		}
	}
	if stderr.Len() != 0 || !command.SilenceErrors || !command.SilenceUsage {
		t.Fatalf("runtime error must not duplicate Cobra error/Usage: stderr=%q silence=(%v,%v)", stderr.String(), command.SilenceErrors, command.SilenceUsage)
	}
}

func TestCloudLoginUsesLifecycleIPCWithoutReturningToken(t *testing.T) {
	completeAttempts := 0
	fake := &closableCloudFake{FakeClient: &cloudcompanion.FakeClient{
		BeginLoginFunc: func(_ context.Context, request *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
			if request.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE {
				t.Fatalf("login method = %s", request.GetMethod())
			}
			if request.GetClientMetadata().GetDisplayName() == "" || request.GetClientMetadata().GetPlatform() == "" {
				t.Fatalf("client metadata = %#v", request.GetClientMetadata())
			}
			return &cloudpb.LoginFlow{FlowId: "flow-1", VerificationUri: "https://login.example.test", UserCode: "ABCD-EFGH", ExpiresAtUnix: uint64(time.Now().Add(time.Minute).Unix()), PollIntervalMillis: 1}, nil
		},
		CompleteLoginFunc: func(_ context.Context, request *cloudpb.CompleteLoginRequest) (*cloudpb.CompleteLoginResponse, error) {
			if request.GetFlowId() != "flow-1" {
				t.Fatalf("flow id = %q", request.GetFlowId())
			}
			completeAttempts++
			if completeAttempts == 1 {
				err := cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "waiting for browser approval")
				err.Retryable = true
				return nil, err
			}
			return &cloudpb.CompleteLoginResponse{Session: &cloudpb.CloudSessionSummary{AccountId: "account-1", AccountLabel: "Alice"}}, nil
		},
	}}
	previousOpen := openV3CloudLifecycleClient
	openV3CloudLifecycleClient = func(_ context.Context, role cloudpb.CallerRole, capabilities ...cloudpb.CompanionCapability) (v3CloudClient, error) {
		if role != cloudpb.CallerRole_CALLER_ROLE_CLI || len(capabilities) != 1 || capabilities[0] != cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION {
			t.Fatalf("open role/capabilities = (%s, %v)", role, capabilities)
		}
		return fake, nil
	}
	defer func() { openV3CloudLifecycleClient = previousOpen }()

	var output bytes.Buffer
	command := newRootCmd()
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"cloud", "login", "--device-code"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !fake.closed || completeAttempts != 2 || strings.Contains(strings.ToLower(output.String()), "token") || !strings.Contains(output.String(), "Alice") {
		t.Fatalf("login output/close = (%q, %v)", output.String(), fake.closed)
	}
}

func TestCloudEnrollSignsChallengeWithDaemonIdentity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Date(2026, 7, 11, 13, 0, 0, 0, time.UTC)
	previousNow := v3CloudNow
	v3CloudNow = func() time.Time { return now }
	defer func() { v3CloudNow = previousNow }()
	challengeBytes := bytes.Repeat([]byte{0x44}, 32)
	var beginRequest *cloudpb.BeginDeviceEnrollmentRequest
	fake := &closableCloudFake{FakeClient: &cloudcompanion.FakeClient{
		BeginDeviceEnrollmentFunc: func(_ context.Context, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
			beginRequest = request
			return &cloudpb.DeviceEnrollmentChallenge{FlowId: "enroll-1", ChallengeId: "challenge-1", Challenge: challengeBytes, ExpiresAtUnix: uint64(now.Add(time.Minute).Unix())}, nil
		},
		CompleteDeviceEnrollmentFunc: func(_ context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.CompleteDeviceEnrollmentResponse, error) {
			proof := request.GetProof()
			if beginRequest == nil || beginRequest.GetOneTimeCode() != "ONE-TIME-CODE" || !bytes.Equal(beginRequest.GetDevicePublicKey(), proof.GetDevicePublicKey()) {
				t.Fatalf("enrollment request/proof mismatch: begin=%v proof=%v", beginRequest, proof)
			}
			signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{
				FlowId: request.GetFlowId(), ChallengeId: proof.GetChallengeId(), Challenge: challengeBytes,
				DeviceId: proof.GetDeviceId(), DevicePublicKey: proof.GetDevicePublicKey(), SignedAtUnixNano: proof.GetSignedAtUnixNano(),
			})
			if err != nil || !ed25519.Verify(ed25519.PublicKey(proof.GetDevicePublicKey()), signingBytes, proof.GetSignature()) {
				t.Fatalf("enrollment signature verification failed: %v", err)
			}
			return &cloudpb.CompleteDeviceEnrollmentResponse{Session: &cloudpb.CloudSessionSummary{DeviceId: proof.GetDeviceId()}}, nil
		},
	}}
	previousOpen := openV3CloudLifecycleClient
	openV3CloudLifecycleClient = func(context.Context, cloudpb.CallerRole, ...cloudpb.CompanionCapability) (v3CloudClient, error) {
		return fake, nil
	}
	defer func() { openV3CloudLifecycleClient = previousOpen }()

	var output bytes.Buffer
	command := newRootCmd()
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"cloud", "enroll", "ONE-TIME-CODE"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "ONE-TIME-CODE") || !strings.Contains(output.String(), "device-") {
		t.Fatalf("enroll output = %q", output.String())
	}
}

func TestCloudEnrollExplainsRejectedOneTimeCode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fake := &closableCloudFake{FakeClient: &cloudcompanion.FakeClient{
		BeginDeviceEnrollmentFunc: func(context.Context, *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
			return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "sanitized upstream rejection")
		},
	}}
	previousOpen := openV3CloudLifecycleClient
	openV3CloudLifecycleClient = func(context.Context, cloudpb.CallerRole, ...cloudpb.CompanionCapability) (v3CloudClient, error) {
		return fake, nil
	}
	defer func() { openV3CloudLifecycleClient = previousOpen }()

	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"cloud", "enroll", "EXPIRED-CODE"})
	err := command.Execute()
	if err == nil || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED) {
		t.Fatalf("enroll rejection = %v", err)
	}
	if message := err.Error(); !strings.Contains(message, "invalid, expired, or already used") || !strings.Contains(message, "within two minutes") || strings.Contains(message, "sanitized upstream rejection") {
		t.Fatalf("enroll rejection message = %q", message)
	}
}

func TestCloudNodeListUsesAccountDirectoryWithoutGrantData(t *testing.T) {
	fake := &closableCloudFake{FakeClient: &cloudcompanion.FakeClient{
		ListManagedDevicesFunc: func(context.Context, *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error) {
			return &cloudpb.ListManagedDevicesResponse{Devices: []*cloudpb.ManagedDevice{
				{DeviceId: "client-1", DisplayName: "Phone", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, Presence: cloudpb.PresenceState_PRESENCE_STATE_OFFLINE},
				{DeviceId: "daemon-1", DisplayName: "Workstation", Platform: "darwin/arm64", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, Presence: cloudpb.PresenceState_PRESENCE_STATE_ONLINE},
			}}, nil
		},
	}}
	previousOpen := openV3CloudLifecycleClient
	openV3CloudLifecycleClient = func(_ context.Context, role cloudpb.CallerRole, capabilities ...cloudpb.CompanionCapability) (v3CloudClient, error) {
		if role != cloudpb.CallerRole_CALLER_ROLE_CLI || len(capabilities) != 1 || capabilities[0] != cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_DIRECTORY {
			t.Fatalf("directory open = (%v, %v)", role, capabilities)
		}
		return fake, nil
	}
	defer func() { openV3CloudLifecycleClient = previousOpen }()

	var output bytes.Buffer
	command := newRootCmd()
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"cloud", "node", "list", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if value := output.String(); !strings.Contains(value, `"id":"daemon-1"`) || !strings.Contains(value, `"status":"online"`) || strings.Contains(strings.ToLower(value), "grant") {
		t.Fatalf("node list output = %q", value)
	}
}

func TestCloudUninstallPurgeDeletesCloudSessionsBeforeArtifact(t *testing.T) {
	cloudInstaller := &fakeCloudInstaller{status: installer.Installation{Version: "v1.0.0"}}
	previousFactory := newV3CloudInstallerForCommand
	newV3CloudInstallerForCommand = func() (v3CloudInstaller, error) { return cloudInstaller, nil }
	defer func() { newV3CloudInstallerForCommand = previousFactory }()
	order := make([]string, 0, 3)
	fake := &closableCloudFake{FakeClient: &cloudcompanion.FakeClient{
		LogoutFunc: func(_ context.Context, request *cloudpb.LogoutRequest) (*cloudpb.LogoutResponse, error) {
			if !request.GetAccountSession() || !request.GetDeviceSession() {
				t.Fatalf("purge request = %#v", request)
			}
			order = append(order, "logout")
			return &cloudpb.LogoutResponse{}, nil
		},
		ShutdownFunc: func(context.Context, *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error) {
			order = append(order, "shutdown")
			return &cloudpb.ShutdownResponse{}, nil
		},
	}}
	previousOpen := openV3CloudLifecycleClient
	openV3CloudLifecycleClient = func(context.Context, cloudpb.CallerRole, ...cloudpb.CompanionCapability) (v3CloudClient, error) {
		return fake, nil
	}
	defer func() { openV3CloudLifecycleClient = previousOpen }()
	cloudInstaller.uninstallFunc = func() error {
		order = append(order, "uninstall")
		return nil
	}

	command := newRootCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"cloud", "uninstall", "--purge"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "logout,shutdown,uninstall" {
		t.Fatalf("operation order = %v", order)
	}
}

type fakeCloudInstaller struct {
	installRequest installer.Request
	status         installer.Installation
	statusErr      error
	uninstallFunc  func() error
}

func (fake *fakeCloudInstaller) InstallRelease(_ context.Context, request installer.Request) (installer.Installation, error) {
	fake.installRequest = request
	return installer.Installation{Version: request.Version, Channel: request.Channel}, nil
}

func (fake *fakeCloudInstaller) Status() (installer.Installation, error) {
	return fake.status, fake.statusErr
}

func (fake *fakeCloudInstaller) Uninstall() error {
	if fake.uninstallFunc != nil {
		return fake.uninstallFunc()
	}
	return nil
}

type closableCloudFake struct {
	*cloudcompanion.FakeClient
	closed bool
}

func (fake *closableCloudFake) Close() error {
	fake.closed = true
	return nil
}
