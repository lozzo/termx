package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/configsignature"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	controllerruntime "github.com/anytty/anytty/cloud/controller/runtime"
	"github.com/anytty/anytty/cloud/edge/controllerlink"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/securefs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testEdgeID              = "edge-integration-1"
	testControllerServer    = "controller.test"
	testEdgePublicServer    = "edge.test"
	testControllerID        = "controller-integration-1"
	testControllerBootID    = "controller-boot-integration-1"
	testEdgeBootID          = "edge-boot-integration-1"
	testEdgeSoftwareVersion = "integration-test"
)

type certificateFiles struct {
	rootCA           string
	controllerCert   string
	controllerKey    string
	edgeIdentityCert string
	edgeIdentityKey  string
	edgePublicCert   string
	edgePublicKey    string
	rootPool         *x509.CertPool
}

func TestEdgeControllerHelloWelcomeOverMutualTLS(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startController(t, certificates)
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress:             "127.0.0.1:0",
		PublicCertificateFile:     certificates.edgePublicCert,
		PublicPrivateKeyFile:      certificates.edgePublicKey,
		ControllerAddress:         controllerRuntime.GRPCAddress(),
		ControllerServerName:      testControllerServer,
		ControllerCAFile:          certificates.rootCA,
		IdentityCertificateFile:   certificates.edgeIdentityCert,
		IdentityPrivateKeyFile:    certificates.edgeIdentityKey,
		EdgeID:                    testEdgeID,
		BootID:                    testEdgeBootID,
		SoftwareVersion:           testEdgeSoftwareVersion,
		BindingKeyBundleCacheFile: testBindingKeyCacheFile(t),
	})
	if err != nil {
		t.Fatalf("start Edge: %v", err)
	}
	t.Cleanup(func() { shutdownEdge(t, edgeRuntime) })

	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	if err := edgeRuntime.WaitReady(readyContext); err != nil {
		t.Fatalf("wait for real EdgeHello/EdgeWelcome: %v", err)
	}
	agent := &cloudv1.AgentPresence{DaemonId: "daemon-r2-1", AccountId: "account-r2-1", BootId: "daemon-boot-r2-1", ConnectionId: "agent-connection-r2-1", Generation: 1, BindingId: "binding-r2-1", BindingIssuedAt: timestamppb.Now()}
	if err := edgeRuntime.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatalf("publish test agent through Edge runtime: %v", err)
	}
	session := &cloudv1.ClientSessionSummary{SessionId: "session-r2-1", AccountId: "account-r2-1", DaemonId: agent.GetDaemonId(), ClientId: "client-r2-1", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: 1}
	if err := edgeRuntime.AttachSession(context.Background(), session, func() {}); err != nil {
		t.Fatalf("publish test session through Edge runtime: %v", err)
	}
	eventually(t, 5*time.Second, func() bool {
		projection, found, queryErr := controllerRuntime.directory.Edge(context.Background(), testEdgeID)
		return queryErr == nil && found && projection.AgentCount == 1 && projection.SessionCount == 1 && projection.RuntimeRevision == 2
	})
	location, found, err := controllerRuntime.directory.LocateDaemon(context.Background(), agent.GetDaemonId())
	if err != nil || !found || location.EdgeID != testEdgeID || location.Generation != 1 {
		t.Fatalf("locate daemon = %+v, found=%v, err=%v", location, found, err)
	}
	assertHTTPStatus(t, http.DefaultClient, "http://"+controllerRuntime.HealthAddress()+"/readyz", http.StatusOK)

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certificates.rootPool,
		ServerName: testEdgePublicServer,
	}}}
	t.Cleanup(httpClient.CloseIdleConnections)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/healthz", http.StatusOK)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/readyz", http.StatusOK)

	connection, err := grpc.NewClient(edgeRuntime.PublicAddress(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certificates.rootPool,
		ServerName: testEdgePublicServer,
	})))
	if err != nil {
		t.Fatalf("create Edge public gRPC health client: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	checkContext, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	response, err := grpc_health_v1.NewHealthClient(connection).Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("check Edge public gRPC health: %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("Edge public gRPC health = %s, want SERVING", response.GetStatus())
	}
}

func TestControllerDisconnectCommandsReachExactEdgeRuntimeGeneration(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startController(t, certificates)
	tlsConfig, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: certificates.edgeIdentityCert, PrivateKeyFile: certificates.edgeIdentityKey,
		RootCAFile: certificates.rootCA, ServerName: testControllerServer,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 64, DeltaBuffer: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	if err := state.ApplyDaemonStateSnapshot(context.Background(), &cloudv1.DaemonStateSnapshot{Daemons: []*cloudv1.DaemonStateRecord{{DaemonId: "daemon-command-1", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1}}}); err != nil {
		t.Fatal(err)
	}
	daemonClosed := make(chan struct{}, 1)
	lifecycleCommands := make(chan *cloudv1.DaemonLifecycleCommand, 1)
	agent := &cloudv1.AgentPresence{DaemonId: "daemon-command-1", AccountId: "account-command-1", BootId: "daemon-boot-command", ConnectionId: "agent-command", BindingId: "binding-command", BindingIssuedAt: timestamppb.Now()}
	agentClaims := &cloudv1.DaemonBindingClaims{DaemonId: agent.GetDaemonId(), AccountId: agent.GetAccountId(), EdgeId: testEdgeID, DevicePublicKey: make([]byte, ed25519.PublicKeySize)}
	generation, daemonState, err := state.AttachAuthenticatedAgent(context.Background(), agent, agentClaims, func(command *cloudv1.EdgeCommand) bool {
		if command.GetLifecycle() != nil {
			lifecycleCommands <- command.GetLifecycle()
		}
		return true
	}, func() { daemonClosed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ApplyDaemonLifecycleResult(context.Background(), agent.GetDaemonId(), generation, &cloudv1.DaemonLifecycleResult{DaemonState: daemonState, AgentGeneration: generation, Applied: true}); err != nil {
		t.Fatal(err)
	}
	sessionClosed := make(chan struct{}, 1)
	sessionSummary := &cloudv1.ClientSessionSummary{SessionId: "session-command-1", AccountId: agent.GetAccountId(), DaemonId: agent.GetDaemonId(), ClientId: "client-command", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_ANDROID, Generation: 1}
	if err := state.AttachSession(context.Background(), sessionSummary, func() { sessionClosed <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	session, err := controllerlink.Open(context.Background(), controllerlink.Config{
		ControllerAddress: controllerRuntime.GRPCAddress(), TLSConfig: tlsConfig, EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: testEdgeSoftwareVersion,
		OpenRuntimeFeed: func(ctx context.Context) (*controllerlink.RuntimeFeed, error) {
			feed, openErr := state.OpenFeed(ctx)
			if openErr != nil {
				return nil, openErr
			}
			return &controllerlink.RuntimeFeed{Snapshot: feed.Snapshot, Deltas: feed.Deltas, Close: feed.Close}, nil
		},
		CloseDaemon: state.CloseAgentConnection, CloseSession: state.CloseSession,
		ApplyBindingKeyBundle:    func(*cloudv1.KeyBundle) error { return nil },
		ApplyDaemonStateSnapshot: state.ApplyDaemonStateSnapshot,
		ApplyDaemonStateDelta:    state.ApplyDaemonStateDelta,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	readyCtx, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	if err := session.WaitReady(readyCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case lifecycle := <-lifecycleCommands:
		if err := state.ApplyDaemonLifecycleResult(context.Background(), agent.GetDaemonId(), generation, &cloudv1.DaemonLifecycleResult{DaemonState: lifecycle.GetDaemonState(), AgentGeneration: generation, Applied: true}); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Controller snapshot did not reconcile daemon lifecycle state")
	}
	eventually(t, 5*time.Second, func() bool {
		projection, found, queryErr := controllerRuntime.directory.Edge(context.Background(), testEdgeID)
		return queryErr == nil && found && projection.AgentCount == 1 && projection.SessionCount == 1
	})
	commandCtx, cancelCommand := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCommand()
	if result := controllerRuntime.control.DisconnectDaemon(commandCtx, agent.GetDaemonId(), generation, "integration command"); result != cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED {
		t.Fatalf("disconnect daemon result = %s", result)
	}
	select {
	case <-daemonClosed:
	case <-time.After(time.Second):
		t.Fatal("daemon owner was not closed")
	}
	if result := controllerRuntime.control.DisconnectSession(commandCtx, sessionSummary.GetSessionId(), sessionSummary.GetGeneration(), "integration command"); result != cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED {
		t.Fatalf("disconnect session result = %s", result)
	}
	select {
	case <-sessionClosed:
	case <-time.After(time.Second):
		t.Fatal("session owner was not closed")
	}
	if result := controllerRuntime.control.DisconnectSession(commandCtx, sessionSummary.GetSessionId(), sessionSummary.GetGeneration()+1, "stale command"); result != cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_STALE {
		t.Fatalf("stale session command result = %s", result)
	}
}

func TestEdgeAppliesSignedDesiredConfigBeforeReady(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := configsignature.Sign(&cloudv1.EdgeDesiredConfig{EdgeId: testEdgeID, Version: 1, Name: "R3 Edge", Region: "cn-east", Capacity: 1000, PublicEndpoint: "edge.test:41102", Enabled: true}, "config-key-r3", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	controllerRuntime := startControllerWithDesired(t, certificates, func(context.Context, string) (*cloudv1.SignedEdgeDesiredConfig, error) { return signed, nil })
	publicKeyFile := writeTestFile(t, t.TempDir(), "config-public.key", publicKey)
	cacheFile := filepath.Join(t.TempDir(), "desired-config.pb")
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
		ControllerAddress: controllerRuntime.GRPCAddress(), ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
		EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: testEdgeSoftwareVersion,
		ConfigSigningKeyID: "config-key-r3", ConfigSigningPublicKeyFile: publicKeyFile, DesiredConfigCacheFile: cacheFile,
		BindingKeyBundleCacheFile: testBindingKeyCacheFile(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEdge(t, edgeRuntime) })
	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	if err := edgeRuntime.WaitReady(readyContext); err != nil {
		t.Fatalf("wait signed desired config: %v", err)
	}
	payload, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	cached := &cloudv1.SignedEdgeDesiredConfig{}
	if err := proto.Unmarshal(payload, cached); err != nil || !proto.Equal(cached, signed) {
		t.Fatalf("cached desired config=%v err=%v", cached, err)
	}
}

func TestControllerRejectsHelloIdentityMismatch(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startController(t, certificates)
	tlsConfig, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: certificates.edgeIdentityCert,
		PrivateKeyFile:  certificates.edgeIdentityKey,
		RootCAFile:      certificates.rootCA,
		ServerName:      testControllerServer,
	})
	if err != nil {
		t.Fatalf("load Edge identity TLS: %v", err)
	}
	openContext, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOpen()
	_, err = controllerlink.Open(openContext, controllerlink.Config{
		ControllerAddress:        controllerRuntime.GRPCAddress(),
		TLSConfig:                tlsConfig,
		EdgeID:                   "edge-does-not-match-certificate",
		BootID:                   testEdgeBootID,
		SoftwareVersion:          testEdgeSoftwareVersion,
		OpenRuntimeFeed:          emptyRuntimeFeed,
		ApplyBindingKeyBundle:    func(*cloudv1.KeyBundle) error { return nil },
		ApplyDaemonStateSnapshot: ignoreDaemonStateSnapshot,
		ApplyDaemonStateDelta:    ignoreDaemonStateDelta,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("mismatched Edge identity code = %s, want InvalidArgument; error: %v", status.Code(err), err)
	}
}

func TestControllerRejectsDisabledEdgeBeforeWelcome(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startControllerWithAdmission(t, certificates, nil, func(context.Context, string) (bool, error) { return false, nil })
	tlsConfig, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: certificates.edgeIdentityCert, PrivateKeyFile: certificates.edgeIdentityKey,
		RootCAFile: certificates.rootCA, ServerName: testControllerServer,
	})
	if err != nil {
		t.Fatal(err)
	}
	openContext, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOpen()
	_, err = controllerlink.Open(openContext, controllerlink.Config{
		ControllerAddress: controllerRuntime.GRPCAddress(), TLSConfig: tlsConfig, EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: testEdgeSoftwareVersion,
		OpenRuntimeFeed: emptyRuntimeFeed, ApplyBindingKeyBundle: func(*cloudv1.KeyBundle) error { return nil },
		ApplyDaemonStateSnapshot: ignoreDaemonStateSnapshot, ApplyDaemonStateDelta: ignoreDaemonStateDelta,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("disabled Edge admission code = %s, want PermissionDenied; error: %v", status.Code(err), err)
	}
}

func TestEdgeStaysAliveButNotReadyWhileControllerIsUnavailable(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress:             "127.0.0.1:0",
		PublicCertificateFile:     certificates.edgePublicCert,
		PublicPrivateKeyFile:      certificates.edgePublicKey,
		ControllerAddress:         unavailableAddress(t),
		ControllerServerName:      testControllerServer,
		ControllerCAFile:          certificates.rootCA,
		IdentityCertificateFile:   certificates.edgeIdentityCert,
		IdentityPrivateKeyFile:    certificates.edgeIdentityKey,
		EdgeID:                    testEdgeID,
		BootID:                    testEdgeBootID,
		SoftwareVersion:           testEdgeSoftwareVersion,
		BindingKeyBundleCacheFile: testBindingKeyCacheFile(t),
	})
	if err != nil {
		t.Fatalf("start Edge without Controller: %v", err)
	}
	t.Cleanup(func() { shutdownEdge(t, edgeRuntime) })

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certificates.rootPool,
		ServerName: testEdgePublicServer,
	}}}
	t.Cleanup(httpClient.CloseIdleConnections)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/healthz", http.StatusOK)
	assertEdgeReadiness(t, httpClient, edgeRuntime.PublicAddress(), http.StatusServiceUnavailable, false, false)
}

func TestControllerGracefullyShutsDownWithActiveEdgeStream(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startController(t, certificates)
	tlsConfig, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: certificates.edgeIdentityCert,
		PrivateKeyFile:  certificates.edgeIdentityKey,
		RootCAFile:      certificates.rootCA,
		ServerName:      testControllerServer,
	})
	if err != nil {
		t.Fatalf("load Edge identity TLS: %v", err)
	}
	session, err := controllerlink.Open(context.Background(), controllerlink.Config{
		ControllerAddress:        controllerRuntime.GRPCAddress(),
		TLSConfig:                tlsConfig,
		EdgeID:                   testEdgeID,
		BootID:                   testEdgeBootID,
		SoftwareVersion:          testEdgeSoftwareVersion,
		OpenRuntimeFeed:          emptyRuntimeFeed,
		ApplyBindingKeyBundle:    func(*cloudv1.KeyBundle) error { return nil },
		ApplyDaemonStateSnapshot: ignoreDaemonStateSnapshot,
		ApplyDaemonStateDelta:    ignoreDaemonStateDelta,
	})
	if err != nil {
		t.Fatalf("open active EdgeControl stream: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	if err := controllerRuntime.Shutdown(shutdownContext); err != nil {
		t.Fatalf("gracefully shutdown Controller with active stream: %v", err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- session.Wait() }()
	select {
	case err := <-waitResult:
		if !errors.Is(err, io.EOF) && status.Code(err) != codes.Unavailable {
			t.Fatalf("drained Edge stream = %v, want clean EOF or Unavailable", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drained Edge stream did not close")
	}
}

type controllerHarness struct {
	*controllerruntime.Runtime
	directory *directory.Directory
	control   *control.Service
}

func startController(t *testing.T, certificates certificateFiles) *controllerHarness {
	return startControllerWithDesired(t, certificates, nil)
}

func startControllerWithDesired(t *testing.T, certificates certificateFiles, desired func(context.Context, string) (*cloudv1.SignedEdgeDesiredConfig, error)) *controllerHarness {
	return startControllerWithAdmission(t, certificates, desired, integrationEdgeEnabled)
}

func startControllerWithAdmission(t *testing.T, certificates certificateFiles, desired func(context.Context, string) (*cloudv1.SignedEdgeDesiredConfig, error), edgeEnabled func(context.Context, string) (bool, error)) *controllerHarness {
	t.Helper()
	directoryState, err := directory.New(directory.Config{MailboxSize: 1024, GracePeriod: 25 * time.Millisecond})
	if err != nil {
		t.Fatalf("create Controller Directory: %v", err)
	}
	service, err := control.NewService(control.Config{
		ControllerID:          testControllerID,
		ControllerBootID:      testControllerBootID,
		HeartbeatInterval:     time.Second,
		HeartbeatTimeout:      3 * time.Second,
		Directory:             directoryState,
		BindingKeyBundle:      testBindingKeyBundleProvider(),
		EdgeEnabled:           edgeEnabled,
		DaemonStateSnapshot:   integrationDaemonStateSnapshot,
		ResolveDaemonState:    integrationDaemonStateResolver,
		DaemonConnectionLimit: integrationDaemonConnectionLimit,
		DesiredConfig:         desired,
	})
	if err != nil {
		t.Fatalf("create EdgeControl service: %v", err)
	}
	runtime, err := controllerruntime.Start(controllerruntime.Config{
		GRPCListenAddress:   "127.0.0.1:0",
		HealthListenAddress: "127.0.0.1:0",
		TLSCertificateFile:  certificates.controllerCert,
		TLSPrivateKeyFile:   certificates.controllerKey,
		EdgeCAFile:          certificates.rootCA,
	}, service)
	if err != nil {
		directoryState.Close()
		t.Fatalf("start Controller: %v", err)
	}
	t.Cleanup(func() {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if err := runtime.Shutdown(shutdownContext); err != nil {
			t.Errorf("shutdown Controller: %v", err)
		}
		directoryState.Close()
	})
	return &controllerHarness{Runtime: runtime, directory: directoryState, control: service}
}

func integrationEdgeEnabled(context.Context, string) (bool, error) { return true, nil }

func integrationDaemonConnectionLimit(context.Context, string, string) (uint32, error) {
	return 1024, nil
}

func integrationDaemonStateSnapshot(context.Context) (*cloudv1.DaemonStateSnapshot, error) {
	return &cloudv1.DaemonStateSnapshot{Daemons: []*cloudv1.DaemonStateRecord{
		{DaemonId: "daemon-r2-1", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1},
		{DaemonId: "daemon-command-1", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1},
	}}, nil
}

func integrationDaemonStateResolver(_ context.Context, daemonID string) (*cloudv1.DaemonStateRecord, bool, error) {
	return &cloudv1.DaemonStateRecord{DaemonId: daemonID, State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1}, true, nil
}

func emptyRuntimeFeed(context.Context) (*controllerlink.RuntimeFeed, error) {
	deltas := make(chan *cloudv1.RuntimeDelta)
	return &controllerlink.RuntimeFeed{Snapshot: &cloudv1.RuntimeSnapshot{}, Deltas: deltas, Close: func() { close(deltas) }}, nil
}

func ignoreDaemonStateSnapshot(context.Context, *cloudv1.DaemonStateSnapshot) error { return nil }
func ignoreDaemonStateDelta(context.Context, *cloudv1.DaemonStateDelta) error       { return nil }

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}

func testBindingKeyCacheFile(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "binding-key-state")
	handle, err := securefs.OpenOrCreatePrivateDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "binding-key-bundle.pb")
}

func testBindingKeyBundleProvider(keys ...*cloudv1.VerificationKey) func(context.Context) (*cloudv1.KeyBundle, error) {
	if len(keys) == 0 {
		keys = []*cloudv1.VerificationKey{{KeyId: "integration-binding", Algorithm: "Ed25519", PublicKey: make([]byte, ed25519.PublicKeySize)}}
	}
	return func(context.Context) (*cloudv1.KeyBundle, error) {
		now := time.Now().UTC()
		return &cloudv1.KeyBundle{Revision: 1, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(24 * time.Hour)), Keys: keys}, nil
	}
}

func shutdownEdge(t *testing.T, runtime *edgeruntime.Runtime) {
	t.Helper()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := runtime.Shutdown(shutdownContext); err != nil {
		t.Errorf("shutdown Edge: %v", err)
	}
}

func assertHTTPStatus(t *testing.T, client *http.Client, target string, expected int) {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != expected {
		t.Fatalf("GET %s status = %d, want %d", target, response.StatusCode, expected)
	}
}

func assertEdgeReadiness(t *testing.T, client *http.Client, address string, expectedStatus int, controllerConnected, bindingKeysUsable bool) {
	t.Helper()
	response, err := client.Get("https://" + address + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		ControllerConnected bool `json:"controller_connected"`
		BindingKeysUsable   bool `json:"binding_keys_usable"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != expectedStatus || body.ControllerConnected != controllerConnected || body.BindingKeysUsable != bindingKeysUsable {
		t.Fatalf("readyz status=%d body=%+v", response.StatusCode, body)
	}
}

func unavailableAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve unavailable address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release unavailable address: %v", err)
	}
	return address
}

func newCertificateFiles(t *testing.T, edgeID string) certificateFiles {
	t.Helper()
	directory := t.TempDir()
	caCertificate, caKey := newCertificateAuthority(t)
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate.Raw})
	rootPath := writeTestFile(t, directory, "root-ca.pem", rootPEM)
	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(rootPEM) {
		t.Fatal("append generated root CA")
	}
	controllerCert, controllerKey := issueCertificate(t, caCertificate, caKey, certificateRequest{
		commonName:  testControllerServer,
		dnsNames:    []string{testControllerServer},
		extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	edgeIdentityURI, err := securetransport.EdgeIdentityURI(edgeID)
	if err != nil {
		t.Fatalf("create Edge identity URI: %v", err)
	}
	edgeIdentityCert, edgeIdentityKey := issueCertificate(t, caCertificate, caKey, certificateRequest{
		commonName:  edgeID,
		uris:        []*url.URL{edgeIdentityURI},
		extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	edgePublicCert, edgePublicKey := issueCertificate(t, caCertificate, caKey, certificateRequest{
		commonName:  testEdgePublicServer,
		dnsNames:    []string{testEdgePublicServer},
		ipAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	edgePublicCert = append(edgePublicCert, rootPEM...)
	return certificateFiles{
		rootCA:           rootPath,
		controllerCert:   writeTestFile(t, directory, "controller-cert.pem", controllerCert),
		controllerKey:    writeTestFile(t, directory, "controller-key.pem", controllerKey),
		edgeIdentityCert: writeTestFile(t, directory, "edge-identity-cert.pem", edgeIdentityCert),
		edgeIdentityKey:  writeTestFile(t, directory, "edge-identity-key.pem", edgeIdentityKey),
		edgePublicCert:   writeTestFile(t, directory, "edge-public-cert.pem", edgePublicCert),
		edgePublicKey:    writeTestFile(t, directory, "edge-public-key.pem", edgePublicKey),
		rootPool:         rootPool,
	}
}

type certificateRequest struct {
	commonName  string
	dnsNames    []string
	ipAddresses []net.IP
	uris        []*url.URL
	extendedUse []x509.ExtKeyUsage
}

func newCertificateAuthority(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key := newPrivateKey(t)
	template := &x509.Certificate{
		SerialNumber:          newSerialNumber(t),
		Subject:               pkix.Name{CommonName: "AnyTTY integration root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create root CA: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse root CA: %v", err)
	}
	return certificate, key
}

func issueCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, request certificateRequest) ([]byte, []byte) {
	t.Helper()
	key := newPrivateKey(t)
	template := &x509.Certificate{
		SerialNumber: newSerialNumber(t),
		Subject:      pkix.Name{CommonName: request.commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  request.extendedUse,
		DNSNames:     request.dnsNames,
		IPAddresses:  request.ipAddresses,
		URIs:         request.uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("issue %s certificate: %v", request.commonName, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal %s private key: %v", request.commonName, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func newPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	return key
}

func newSerialNumber(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate certificate serial: %v", err)
	}
	return serial
}

func writeTestFile(t *testing.T, directory, name string, payload []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
