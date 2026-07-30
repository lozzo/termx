package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controllerbindingkeys "github.com/anytty/anytty/cloud/controller/bindingkeys"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/postgres"
	controllerruntime "github.com/anytty/anytty/cloud/controller/runtime"
	"github.com/anytty/anytty/cloud/edge/agentgateway"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAuthenticatedAgentPresenceRebuildsAfterControllerRestart(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verification := &cloudv1.VerificationKey{KeyId: "binding-r4", Algorithm: "Ed25519", PublicKey: publicKey}
	firstRuntime, firstDirectory := startPresenceController(t, certificates, "127.0.0.1:0", verification)
	controllerAddress := firstRuntime.GRPCAddress()

	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
		ControllerAddress: controllerAddress, ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
		EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: testEdgeSoftwareVersion,
		BindingKeyBundleCacheFile: testBindingKeyCacheFile(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownEdge(t, edgeRuntime)
	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	if err := edgeRuntime.WaitReady(readyContext); err != nil {
		t.Fatal(err)
	}

	_, daemonPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-r4", daemonPrivate)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.DaemonBindingClaims{BindingId: uuid.NewString(), DaemonId: uuid.NewString(), AccountId: uuid.NewString(), EdgeId: testEdgeID, DeviceId: identity.DeviceID, DevicePublicKey: identity.PublicKey, Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(10 * time.Minute)), Revision: 1, EdgeLocatorSha256: make([]byte, sha256.Size)}
	signed, err := ticket.SignDaemonBinding("binding-r4", privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	edgeConnection, err := grpc.NewClient(edgeRuntime.PublicAddress(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: certificates.rootPool, ServerName: testEdgePublicServer})))
	if err != nil {
		t.Fatal(err)
	}
	defer edgeConnection.Close()
	stream, err := cloudv1.NewAgentGatewayClient(edgeConnection).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bootID, connectionID := uuid.NewString(), uuid.NewString()
	challengeCommand, err := stream.Recv()
	if err != nil || challengeCommand.GetChallenge() == nil {
		t.Fatalf("AgentGateway challenge=%v err=%v", challengeCommand, err)
	}
	hello := &cloudv1.AgentEvent{ProtocolVersion: agentgateway.ProtocolVersion, MessageId: uuid.NewString(), SenderId: claims.GetDaemonId(), BootId: bootID, ConnectionId: connectionID, StreamSeq: 1, SentAt: timestamppb.Now(), Payload: &cloudv1.AgentEvent_Hello{Hello: &cloudv1.AgentHello{DaemonBinding: signed, SoftwareVersion: "r4-test", AttemptGeneration: 1}}}
	proof, err := ticket.SignAgentHelloProof(identity, challengeCommand.GetChallenge(), hello, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	hello.GetHello().DeviceProof = proof
	if err := stream.Send(hello); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Recv()
	if err != nil || ready.GetReady() == nil {
		t.Fatalf("AgentReady=%v err=%v", ready, err)
	}
	eventually(t, 5*time.Second, func() bool {
		location, found, queryErr := firstDirectory.LocateDaemon(context.Background(), claims.GetDaemonId())
		return queryErr == nil && found && location.EdgeID == testEdgeID && location.Generation == ready.GetReady().GetGeneration()
	})

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := firstRuntime.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	cancelShutdown()
	firstDirectory.Close()
	secondRuntime, secondDirectory := startPresenceController(t, certificates, controllerAddress, verification)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = secondRuntime.Shutdown(ctx)
		secondDirectory.Close()
	}()
	eventually(t, 8*time.Second, func() bool {
		location, found, queryErr := secondDirectory.LocateDaemon(context.Background(), claims.GetDaemonId())
		return queryErr == nil && found && location.EdgeID == testEdgeID && location.Generation == ready.GetReady().GetGeneration()
	})
	_ = stream.CloseSend()
}

func TestBindingKeyBundleRecoversAcrossControllerAndEdgeRestart(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verification := &cloudv1.VerificationKey{KeyId: "binding-restart", Algorithm: "Ed25519", PublicKey: publicKey}
	firstController, firstDirectory := startPresenceController(t, certificates, "127.0.0.1:0", verification)
	controllerAddress := firstController.GRPCAddress()
	cachePath := filepath.Join(t.TempDir(), "binding-key-bundle.pb")
	startEdge := func(bootID string) *edgeruntime.Runtime {
		t.Helper()
		runtime, startErr := edgeruntime.Start(context.Background(), edgeruntime.Config{
			ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
			ControllerAddress: controllerAddress, ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
			IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
			EdgeID: testEdgeID, BootID: bootID, SoftwareVersion: testEdgeSoftwareVersion, BindingKeyBundleCacheFile: cachePath,
		})
		if startErr != nil {
			t.Fatal(startErr)
		}
		return runtime
	}
	firstEdge := startEdge("edge-boot-before-restart")
	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	if err := firstEdge.WaitReady(readyContext); err != nil {
		cancelReady()
		t.Fatal(err)
	}
	cancelReady()
	shutdownEdge(t, firstEdge)
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := firstController.Shutdown(shutdownContext); err != nil {
		cancelShutdown()
		t.Fatal(err)
	}
	cancelShutdown()
	firstDirectory.Close()

	secondEdge := startEdge("edge-boot-after-restart")
	defer shutdownEdge(t, secondEdge)
	if !secondEdge.BindingKeysUsable() || secondEdge.ControllerConnected() || secondEdge.Ready() {
		t.Fatalf("restarted Edge connected=%v keys=%v ready=%v", secondEdge.ControllerConnected(), secondEdge.BindingKeysUsable(), secondEdge.Ready())
	}
	offlineHTTP := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: certificates.rootPool, ServerName: testEdgePublicServer}}}
	defer offlineHTTP.CloseIdleConnections()
	assertEdgeReadiness(t, offlineHTTP, secondEdge.PublicAddress(), http.StatusServiceUnavailable, false, true)
	identity, signed, claims := restartBinding(t, privateKey)
	assertAgentAdmission(t, secondEdge, certificates.rootPool, identity, signed, claims)

	secondController, secondDirectory := startPresenceController(t, certificates, controllerAddress, verification)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = secondController.Shutdown(ctx)
		secondDirectory.Close()
	}()
	eventually(t, 8*time.Second, secondEdge.Ready)
}

func TestPostgresOwnerAndEdgeCacheRecoverAcrossRestarts(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	keyID := "binding-pg-restart"
	verification := &cloudv1.VerificationKey{KeyId: keyID, Algorithm: "Ed25519", PublicKey: publicKey}
	firstOwner, err := controllerbindingkeys.New(ctx, controllerbindingkeys.Config{Store: database, Keys: []*cloudv1.VerificationKey{verification}, TTL: 2 * time.Hour})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	firstBundle, err := firstOwner.Bundle(ctx)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}

	certificates := newCertificateFiles(t, testEdgeID)
	firstController, firstDirectory := startPresenceControllerWithProvider(t, certificates, "127.0.0.1:0", firstOwner.Bundle)
	controllerAddress := firstController.GRPCAddress()
	cachePath := filepath.Join(t.TempDir(), "binding-key-bundle.pb")
	startEdge := func(bootID string) *edgeruntime.Runtime {
		t.Helper()
		runtime, startErr := edgeruntime.Start(context.Background(), edgeruntime.Config{
			ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
			ControllerAddress: controllerAddress, ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
			IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
			EdgeID: testEdgeID, BootID: bootID, SoftwareVersion: testEdgeSoftwareVersion, BindingKeyBundleCacheFile: cachePath,
		})
		if startErr != nil {
			t.Fatal(startErr)
		}
		return runtime
	}
	firstEdge := startEdge("edge-pg-before-restart")
	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	if err := firstEdge.WaitReady(readyContext); err != nil {
		cancelReady()
		database.Close()
		t.Fatal(err)
	}
	cancelReady()
	shutdownEdge(t, firstEdge)
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := firstController.Shutdown(shutdownContext); err != nil {
		cancelShutdown()
		database.Close()
		t.Fatal(err)
	}
	cancelShutdown()
	firstDirectory.Close()
	database.Close()

	secondEdge := startEdge("edge-pg-after-restart")
	defer shutdownEdge(t, secondEdge)
	eventually(t, 3*time.Second, func() bool { return secondEdge.BindingKeysUsable() && !secondEdge.ControllerConnected() })
	identity, signed, claims := restartBindingForKey(t, keyID, privateKey)
	assertAgentAdmission(t, secondEdge, certificates.rootPool, identity, signed, claims)

	restartedDatabase, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedDatabase.Close()
	if err := restartedDatabase.VerifySchema(ctx); err != nil {
		t.Fatal(err)
	}
	secondOwner, err := controllerbindingkeys.New(ctx, controllerbindingkeys.Config{Store: restartedDatabase, Keys: []*cloudv1.VerificationKey{verification}, TTL: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, err := secondOwner.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(firstBundle, secondBundle) {
		t.Fatalf("database metadata changed across restart: first=%v second=%v", firstBundle, secondBundle)
	}
	secondController, secondDirectory := startPresenceControllerWithProvider(t, certificates, controllerAddress, secondOwner.Bundle)
	defer func() {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = secondController.Shutdown(shutdownContext)
		secondDirectory.Close()
	}()
	eventually(t, 8*time.Second, secondEdge.Ready)
}

func TestControllerDoesNotPublishWelcomeForStaleBindingOwner(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	var providerCalls atomic.Int32
	controllerRuntime, directoryState := startPresenceControllerWithProvider(t, certificates, "127.0.0.1:0", func(context.Context) (*cloudv1.KeyBundle, error) {
		providerCalls.Add(1)
		return nil, controllerbindingkeys.ErrKeySetReplay
	})
	defer func() {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = controllerRuntime.Shutdown(shutdownContext)
		directoryState.Close()
	}()
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
		ControllerAddress: controllerRuntime.GRPCAddress(), ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
		EdgeID: testEdgeID, BootID: "edge-stale-owner", SoftwareVersion: testEdgeSoftwareVersion, BindingKeyBundleCacheFile: testBindingKeyCacheFile(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownEdge(t, edgeRuntime)
	eventually(t, 5*time.Second, func() bool { return providerCalls.Load() > 0 })
	if edgeRuntime.ControllerConnected() || edgeRuntime.BindingKeysUsable() || edgeRuntime.Ready() {
		t.Fatalf("stale owner published state: connected=%v keys=%v ready=%v", edgeRuntime.ControllerConnected(), edgeRuntime.BindingKeysUsable(), edgeRuntime.Ready())
	}
}

func TestPostgresStaleOwnerPermanentlyRevokesControllerReadiness(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	publicA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ownerA, err := controllerbindingkeys.New(ctx, controllerbindingkeys.Config{
		Store: database, TTL: 2 * time.Hour,
		Keys: []*cloudv1.VerificationKey{{KeyId: "health-key-a", Algorithm: "Ed25519", PublicKey: publicA}},
	})
	if err != nil {
		t.Fatal(err)
	}
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime, directoryState := startPresenceControllerWithOwnership(t, certificates, "127.0.0.1:0", ownerA.Bundle, ownerA)
	defer func() {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = controllerRuntime.Shutdown(shutdownContext)
		directoryState.Close()
	}()
	assertHTTPStatus(t, http.DefaultClient, "http://"+controllerRuntime.HealthAddress()+"/readyz", http.StatusOK)
	assertControllerGRPCHealth(t, controllerRuntime, certificates, grpc_health_v1.HealthCheckResponse_SERVING)

	publicB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controllerbindingkeys.New(ctx, controllerbindingkeys.Config{
		Store: database, TTL: 2 * time.Hour,
		Keys: []*cloudv1.VerificationKey{{KeyId: "health-key-b", Algorithm: "Ed25519", PublicKey: publicB}},
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsOut := make(chan error, 32)
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, bundleErr := ownerA.Bundle(ctx)
			errorsOut <- bundleErr
		}()
	}
	close(start)
	group.Wait()
	close(errorsOut)
	for bundleErr := range errorsOut {
		if !errors.Is(bundleErr, controllerbindingkeys.ErrKeySetReplay) {
			t.Fatalf("stale owner error=%v", bundleErr)
		}
	}
	assertHTTPStatus(t, http.DefaultClient, "http://"+controllerRuntime.HealthAddress()+"/readyz", http.StatusServiceUnavailable)
	assertControllerGRPCHealth(t, controllerRuntime, certificates, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	if _, err := ownerA.Bundle(ctx); !errors.Is(err, controllerbindingkeys.ErrKeySetReplay) {
		t.Fatalf("latched stale owner error=%v", err)
	}
	assertHTTPStatus(t, http.DefaultClient, "http://"+controllerRuntime.HealthAddress()+"/readyz", http.StatusServiceUnavailable)

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := controllerRuntime.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if err := controllerRuntime.Shutdown(shutdownContext); err != nil {
		t.Fatalf("idempotent shutdown: %v", err)
	}
}

func restartBinding(t *testing.T, privateKey ed25519.PrivateKey) (remoteauth.Identity, *cloudv1.SignedEnvelope, *cloudv1.DaemonBindingClaims) {
	return restartBindingForKey(t, "binding-restart", privateKey)
}

func restartBindingForKey(t *testing.T, keyID string, privateKey ed25519.PrivateKey) (remoteauth.Identity, *cloudv1.SignedEnvelope, *cloudv1.DaemonBindingClaims) {
	t.Helper()
	_, daemonPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-restart", daemonPrivate)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: uuid.NewString(), DaemonId: uuid.NewString(), AccountId: uuid.NewString(), EdgeId: testEdgeID, DeviceId: identity.DeviceID, DevicePublicKey: identity.PublicKey,
		Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(10 * time.Minute)), Revision: 1, EdgeLocatorSha256: make([]byte, sha256.Size),
	}
	signed, err := ticket.SignDaemonBinding(keyID, privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	return identity, signed, claims
}

func assertAgentAdmission(t *testing.T, edgeRuntime *edgeruntime.Runtime, rootPool *x509.CertPool, identity remoteauth.Identity, signed *cloudv1.SignedEnvelope, claims *cloudv1.DaemonBindingClaims) {
	t.Helper()
	connection, err := grpc.NewClient(edgeRuntime.PublicAddress(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: rootPool, ServerName: testEdgePublicServer})))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	stream, err := cloudv1.NewAgentGatewayClient(connection).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := stream.Recv()
	if err != nil || challenge.GetChallenge() == nil {
		t.Fatalf("AgentGateway challenge=%v err=%v", challenge, err)
	}
	hello := &cloudv1.AgentEvent{
		ProtocolVersion: agentgateway.ProtocolVersion, MessageId: uuid.NewString(), SenderId: claims.GetDaemonId(), BootId: uuid.NewString(), ConnectionId: uuid.NewString(), StreamSeq: 1, SentAt: timestamppb.Now(),
		Payload: &cloudv1.AgentEvent_Hello{Hello: &cloudv1.AgentHello{DaemonBinding: signed, SoftwareVersion: "restart-test", AttemptGeneration: 1}},
	}
	proof, err := ticket.SignAgentHelloProof(identity, challenge.GetChallenge(), hello, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	hello.GetHello().DeviceProof = proof
	if err := stream.Send(hello); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Recv()
	if err != nil || ready.GetReady() == nil {
		t.Fatalf("cached bundle Agent admission ready=%v err=%v", ready, err)
	}
	_ = stream.CloseSend()
}

func startPresenceController(t *testing.T, certificates certificateFiles, listen string, key *cloudv1.VerificationKey) (*controllerruntime.Runtime, *directory.Directory) {
	t.Helper()
	return startPresenceControllerWithProvider(t, certificates, listen, testBindingKeyBundleProvider(key))
}

func startPresenceControllerWithProvider(t *testing.T, certificates certificateFiles, listen string, provider func(context.Context) (*cloudv1.KeyBundle, error)) (*controllerruntime.Runtime, *directory.Directory) {
	return startPresenceControllerWithOwnership(t, certificates, listen, provider, nil)
}

func startPresenceControllerWithOwnership(t *testing.T, certificates certificateFiles, listen string, provider func(context.Context) (*cloudv1.KeyBundle, error), ownership interface{ SetOwnershipLostHandler(func()) }) (*controllerruntime.Runtime, *directory.Directory) {
	t.Helper()
	directoryState, err := directory.New(directory.Config{MailboxSize: 1024, GracePeriod: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.NewService(control.Config{ControllerID: testControllerID, ControllerBootID: uuid.NewString(), HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second, BindingKeyBundle: provider, Directory: directoryState})
	if err != nil {
		directoryState.Close()
		t.Fatal(err)
	}
	runtime, err := controllerruntime.Start(controllerruntime.Config{GRPCListenAddress: listen, HealthListenAddress: "127.0.0.1:0", TLSCertificateFile: certificates.controllerCert, TLSPrivateKeyFile: certificates.controllerKey, EdgeCAFile: certificates.rootCA, BindingKeyOwnership: ownership}, service)
	if err != nil {
		directoryState.Close()
		t.Fatal(err)
	}
	return runtime, directoryState
}

func assertControllerGRPCHealth(t *testing.T, controllerRuntime *controllerruntime.Runtime, certificates certificateFiles, expected grpc_health_v1.HealthCheckResponse_ServingStatus) {
	t.Helper()
	clientCertificate, err := tls.LoadX509KeyPair(certificates.edgeIdentityCert, certificates.edgeIdentityKey)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := grpc.NewClient(controllerRuntime.GRPCAddress(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: certificates.rootPool, ServerName: testControllerServer, Certificates: []tls.Certificate{clientCertificate},
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	checkContext, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	response, err := grpc_health_v1.NewHealthClient(connection).Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetStatus() != expected {
		t.Fatalf("Controller gRPC health=%v want=%v", response.GetStatus(), expected)
	}
}
