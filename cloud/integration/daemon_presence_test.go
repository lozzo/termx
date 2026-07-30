package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	controllerruntime "github.com/anytty/anytty/cloud/controller/runtime"
	"github.com/anytty/anytty/cloud/edge/agentgateway"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

func restartBinding(t *testing.T, privateKey ed25519.PrivateKey) (remoteauth.Identity, *cloudv1.SignedEnvelope, *cloudv1.DaemonBindingClaims) {
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
	signed, err := ticket.SignDaemonBinding("binding-restart", privateKey, claims)
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
	directoryState, err := directory.New(directory.Config{MailboxSize: 1024, GracePeriod: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.NewService(control.Config{ControllerID: testControllerID, ControllerBootID: uuid.NewString(), HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second, BindingKeyBundle: testBindingKeyBundleProvider(key), Directory: directoryState})
	if err != nil {
		directoryState.Close()
		t.Fatal(err)
	}
	runtime, err := controllerruntime.Start(controllerruntime.Config{GRPCListenAddress: listen, HealthListenAddress: "127.0.0.1:0", TLSCertificateFile: certificates.controllerCert, TLSPrivateKeyFile: certificates.controllerKey, EdgeCAFile: certificates.rootCA}, service)
	if err != nil {
		directoryState.Close()
		t.Fatal(err)
	}
	return runtime, directoryState
}
