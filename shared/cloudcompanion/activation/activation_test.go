package activation

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
)

func TestManagerStartsVerifiedInstallationOnceAndNegotiatesRequestedCapabilities(t *testing.T) {
	var mu sync.Mutex
	started := false
	startCount := 0
	helloRequests := make(chan *cloudpb.CompanionHelloRequest, 1)
	fake := &cloudcompanion.FakeClient{
		HelloFunc: func(_ context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
			helloRequests <- request
			return &cloudpb.CompanionHelloResponse{
				SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "v1.2.3", BuildChannel: "stable",
				SupportedCapabilities: append([]cloudpb.CompanionCapability(nil), request.GetRequestedCapabilities()...),
				ResponseNonce:         bytes.Repeat([]byte{0x77}, 32),
			}, nil
		},
	}
	manager, err := New(Config{
		Installations: staticInstallationSource{installation: installer.Installation{Version: "v1.2.3", Channel: "stable", BinaryPath: "/verified/termx-cloud"}},
		Endpoint:      "test-endpoint", TermxVersion: "v3-test", RetryInterval: time.Millisecond, ReadyTimeout: time.Second,
		Start: func(binaryPath, endpoint string, smoke bool) error {
			if binaryPath != "/verified/termx-cloud" || endpoint != "test-endpoint" || smoke {
				t.Fatalf("Start(%q, %q, %v)", binaryPath, endpoint, smoke)
			}
			mu.Lock()
			started = true
			startCount++
			mu.Unlock()
			return nil
		},
		Dial: func(context.Context, string) (*ipc.Client, error) {
			mu.Lock()
			ready := started
			mu.Unlock()
			if !ready {
				return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "not ready")
			}
			return pipeClient(t, fake), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := manager.Open(context.Background(), cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	request := <-helloRequests
	if request.GetCallerRole() != cloudpb.CallerRole_CALLER_ROLE_TUI || len(request.GetRequestedCapabilities()) != 1 || request.GetRequestedCapabilities()[0] != cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING {
		t.Fatalf("Hello request = %#v", request)
	}
	if startCount != 1 {
		t.Fatalf("start count = %d", startCount)
	}
}

func TestManagerStopsMismatchedProcessBeforeStartingActiveInstallation(t *testing.T) {
	var mu sync.Mutex
	running := true
	runningVersion := "v1.2.2"
	shutdownCount := 0
	startCount := 0
	fake := &cloudcompanion.FakeClient{
		HelloFunc: func(context.Context, *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
			mu.Lock()
			version := runningVersion
			mu.Unlock()
			return &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: version, BuildChannel: "stable", ResponseNonce: bytes.Repeat([]byte{4}, 32)}, nil
		},
		ShutdownFunc: func(context.Context, *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error) {
			mu.Lock()
			running = false
			shutdownCount++
			mu.Unlock()
			return &cloudpb.ShutdownResponse{}, nil
		},
	}
	manager, err := New(Config{
		Installations: staticInstallationSource{installation: installer.Installation{Version: "v1.2.3", Channel: "stable", BinaryPath: "/verified/termx-cloud"}},
		Endpoint:      "test-endpoint", TermxVersion: "test", RetryInterval: time.Millisecond, ReadyTimeout: time.Second,
		Dial: func(context.Context, string) (*ipc.Client, error) {
			mu.Lock()
			ready := running
			mu.Unlock()
			if !ready {
				return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "not running")
			}
			return pipeClient(t, fake), nil
		},
		Start: func(binaryPath, endpoint string, smoke bool) error {
			if binaryPath != "/verified/termx-cloud" || endpoint != "test-endpoint" || smoke {
				t.Fatalf("Start(%q, %q, %v)", binaryPath, endpoint, smoke)
			}
			mu.Lock()
			running = true
			runningVersion = "v1.2.3"
			startCount++
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := manager.Open(context.Background(), cloudpb.CallerRole_CALLER_ROLE_CLI)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	mu.Lock()
	defer mu.Unlock()
	if shutdownCount != 1 || startCount != 1 || runningVersion != "v1.2.3" {
		t.Fatalf("replacement lifecycle = shutdown:%d start:%d version:%s", shutdownCount, startCount, runningVersion)
	}
}

func TestManagerDoesNotStartUntrustedOrCapabilityExpandingCompanion(t *testing.T) {
	started := false
	manager, err := New(Config{
		Installations: staticInstallationSource{err: cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "bad hash")},
		TermxVersion:  "test", Start: func(string, string, bool) error { started = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), cloudpb.CallerRole_CALLER_ROLE_CLI); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) || started {
		t.Fatalf("untrusted Open = (%v, started=%v)", err, started)
	}

	fake := &cloudcompanion.FakeClient{HelloFunc: func(context.Context, *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
		return &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "test", BuildChannel: "test", ResponseNonce: bytes.Repeat([]byte{3}, 32), SupportedCapabilities: []cloudpb.CompanionCapability{cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE}}, nil
	}}
	manager, err = New(Config{
		Installations: staticInstallationSource{installation: installer.Installation{Version: "test", Channel: "test", BinaryPath: "/verified/termx-cloud"}}, TermxVersion: "test",
		Dial: func(context.Context, string) (*ipc.Client, error) { return pipeClient(t, fake), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), cloudpb.CallerRole_CALLER_ROLE_CLI); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("capability expansion error = %v", err)
	}
}

func TestReleaseHelloMustMatchSignedManifest(t *testing.T) {
	manifest := installer.Manifest{Version: "v1.2.3", Channel: "stable", MinCompanionProtocol: cloudcompanion.ProtocolVersionMin, MaxCompanionProtocol: cloudcompanion.ProtocolVersionMax}
	valid := &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "v1.2.3", BuildChannel: "stable"}
	if err := validateReleaseHello(valid, manifest); err != nil {
		t.Fatal(err)
	}
	mislabeled := &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "v1.2.2", BuildChannel: "stable"}
	if err := validateReleaseHello(mislabeled, manifest); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
		t.Fatalf("mislabeled release error = %v", err)
	}
}

type staticInstallationSource struct {
	installation installer.Installation
	err          error
}

func (source staticInstallationSource) Status() (installer.Installation, error) {
	return source.installation, source.err
}

func pipeClient(t *testing.T, fake cloudcompanion.FullClient) *ipc.Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := &ipc.Server{NewClient: func() (cloudcompanion.FullClient, error) { return fake, nil }}
	go func() { _ = server.ServeConn(context.Background(), serverConn) }()
	client, err := ipc.NewClient(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
