package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apilayer "github.com/anytty/anytty/api_layer"
	cloudadapter "github.com/anytty/anytty/client/adapter/cloud"
	peeradapter "github.com/anytty/anytty/client/adapter/peer"
	pionadapter "github.com/anytty/anytty/client/adapter/webrtc/pion"
	"github.com/anytty/anytty/client/endpoint"
	clientruntime "github.com/anytty/anytty/client/runtime"
	cloudclient "github.com/anytty/anytty/cloud/client"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/directoryapi"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	controllerruntime "github.com/anytty/anytty/cloud/controller/runtime"
	clouddaemon "github.com/anytty/anytty/cloud/daemon"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	"github.com/anytty/anytty/cloud/ticket"
	corev2 "github.com/anytty/anytty/core"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	remotedaemon "github.com/anytty/anytty/remote/daemon"
	remotewebrtc "github.com/anytty/anytty/remote/webrtc"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	pionwebrtc "github.com/pion/webrtc/v4"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCloudAutoFallsBackToRelaySurvivesControllerOutageAndFlushesDurableUsage(t *testing.T) {
	for _, transport := range []string{"udp", "tcp"} {
		t.Run(transport, func(t *testing.T) {
			testCloudRelayOutageAndUsage(t, transport)
		})
	}
}

func TestCorruptUsageOutboxDegradesRelayWithoutStoppingEdge(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	outboxPath := filepath.Join(t.TempDir(), "usage-outbox.db")
	if err := os.WriteFile(outboxPath, []byte("not-a-bbolt-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
		ControllerAddress: "127.0.0.1:1", ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
		EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: "r6-integration", TURNListenAddress: freeTURNAddress(t), TURNPublicEndpoint: "127.0.0.1:3478", TURNRealm: "r6.local", UsageOutboxFile: outboxPath,
	})
	if err != nil {
		t.Fatalf("usage corruption stopped the whole Edge: %v", err)
	}
	if !runtime.RelayDegraded() || runtime.TURNAddress() != "" || runtime.PublicAddress() == "" {
		t.Fatalf("degraded=%v turn=%q public=%q", runtime.RelayDegraded(), runtime.TURNAddress(), runtime.PublicAddress())
	}
	shutdownEdge(t, runtime)
}

func testCloudRelayOutageAndUsage(t *testing.T, transport string) {
	certificates := newCertificateFiles(t, testEdgeID)
	bindingPublicKey, bindingPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bindingKeyID := "binding-r6"
	usageStore := newR6UsageStore()
	controllerRuntime, controllerDirectory := startR6ControlController(t, certificates, "127.0.0.1:0", bindingKeyID, bindingPublicKey, usageStore)
	controllerAddress := controllerRuntime.GRPCAddress()
	controllerStopped := false
	defer func() {
		if !controllerStopped {
			shutdownR6Controller(t, controllerRuntime, controllerDirectory)
		}
	}()

	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonIdentity, err := remoteauth.NewIdentity("device-r6", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	daemonRecord := enrollment.Daemon{
		ID: uuid.NewString(), AccountID: uuid.NewString(), AccountName: "R6 Account", DisplayName: "R6 daemon", DeviceID: daemonIdentity.DeviceID,
		DeviceFingerprint: daemonIdentity.Fingerprint, DevicePublicKey: append(ed25519.PublicKey(nil), daemonIdentity.PublicKey...), Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	edgeStore := &r5EdgeStore{edge: edgeconfig.Edge{ID: testEdgeID, Name: "R6 Edge", Region: "local", Capacity: 100, PublicEndpoint: "127.0.0.1:1", Enabled: true, ConfigVersion: 1, Revision: 1}}
	_, configSigningKey, _ := ed25519.GenerateKey(rand.Reader)
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: edgeStore, SigningKey: configSigningKey, SigningKeyID: "config-r6", ClaimTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	edgeCAPEM, err := os.ReadFile(certificates.rootCA)
	if err != nil {
		t.Fatal(err)
	}
	identityStore := r5EnrollmentStore{daemon: daemonRecord}
	enrollmentService, err := enrollment.NewService(enrollment.Config{
		Entitlement: testEntitlementReader{},
		Store:       identityStore, Edges: edges, Directory: controllerDirectory, BindingSigningKey: bindingPrivateKey, BindingSigningKeyID: bindingKeyID,
		EdgeCACertificate: edgeCAPEM, EnrollmentTTL: time.Minute, ChallengeTTL: time.Minute, BindingTTL: 365 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	directoryService, err := directoryapi.NewService(directoryapi.Config{
		Entitlement: testEntitlementReader{},
		Store:       identityStore, Directory: controllerDirectory, Edges: edges, EdgeCACertificate: edgeCAPEM,
		ChallengeTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	publicControllerAddress, _ := startR5PublicController(t, certificates, enrollmentService, directoryService)

	turnAddress := freeTURNAddress(t)
	outboxPath := filepath.Join(t.TempDir(), "usage-outbox.db")
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
		ControllerAddress: controllerAddress, ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
		EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: "r6-integration", TURNListenAddress: turnAddress, TURNPublicEndpoint: turnAddress, TURNRealm: "r6.local", UsageOutboxFile: outboxPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeStopped := false
	defer func() {
		if !edgeStopped {
			shutdownEdge(t, edgeRuntime)
		}
	}()
	edgeStore.setPublicEndpoint(edgeRuntime.PublicAddress())
	edgeLocator := &cloudv1.EdgeLocator{EdgeId: testEdgeID, Name: "R6 Edge", Region: "local", PublicEndpoint: edgeRuntime.PublicAddress(), ServerName: testEdgePublicServer, CaCertificatePem: edgeCAPEM, Revision: 1}
	edgeLocatorPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(edgeLocator)
	if err != nil {
		t.Fatal(err)
	}
	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	if err := edgeRuntime.WaitReady(readyContext); err != nil {
		cancelReady()
		t.Fatal(err)
	}
	cancelReady()

	accessStore, err := remoteauth.LoadAccessStore(t.TempDir(), daemonIdentity, remoteauth.AccessStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer accessStore.Close()
	if err := accessStore.ConfigureManagedRouteGrantIssuer(func(clientPublicKey ed25519.PublicKey, product uint32, issuedAt, expiresAt time.Time) ([]byte, []byte, error) {
		claims := &cloudv1.CloudRouteGrantClaims{GrantId: uuid.NewString(), DaemonId: daemonRecord.ID, ClientPublicKey: append([]byte(nil), clientPublicKey...), Product: cloudv1.ClientProduct(product), IssuedAt: timestamppb.New(issuedAt.UTC()), ExpiresAt: timestamppb.New(expiresAt.UTC())}
		signed, signErr := ticket.SignCloudRouteGrant(daemonIdentity, claims)
		if signErr != nil {
			return nil, nil, signErr
		}
		grant, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(signed)
		return grant, append([]byte(nil), edgeLocatorPayload...), marshalErr
	}); err != nil {
		t.Fatal(err)
	}
	coreServer := corev2.NewServer(corev2.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory), corev2.WithSocketPath(filepath.Join(t.TempDir(), "core.sock")))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = coreServer.Shutdown(ctx)
	}()
	loopbackAPI := r5LoopbackWebRTCAPI()
	// 本测试强制客户端成为 Relay candidate，daemon 保留 host candidate，稳定证明单 Relay 数据路径。
	// 双端都配置 TURN 会在同机 loopback harness 里产生无现实意义的 relay-to-relay hairpin 竞速。
	daemonPeerFactory := func(configuration pionwebrtc.Configuration) (*pionwebrtc.PeerConnection, error) {
		configuration.ICEServers = nil
		return loopbackAPI.NewPeerConnection(configuration)
	}
	daemonRuntime, err := clouddaemon.NewRuntime(clouddaemon.Config{
		Record: r5DaemonEnrollmentRecord(t, daemonRecord, daemonIdentity, bindingKeyID, bindingPrivateKey, edgeLocatorPayload, &cloudv1.DaemonRelayDelegation{
			MaxBytesPerLease: 16 << 20, MaxRateBytesPerSecond: 4 << 20, MaxConcurrentAllocations: 2,
		}),
		Identity: daemonIdentity, AccessStore: accessStore, SoftwareVersion: "r6-integration",
		Answerer: remotewebrtc.Answerer{Handler: remotedaemon.SessionAcceptor{Core: coreServer, Identity: daemonIdentity, AccessStore: accessStore}, PeerConnections: daemonPeerFactory},
	})
	if err != nil {
		t.Fatal(err)
	}
	daemonContext, cancelDaemon := context.WithCancel(context.Background())
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemonRuntime.Run(daemonContext) }()
	defer func() {
		cancelDaemon()
		select {
		case runErr := <-daemonDone:
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				t.Errorf("stop R6 daemon runtime: %v", runErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("R6 daemon runtime did not stop")
		}
	}()
	eventually(t, 8*time.Second, func() bool {
		location, found, locateErr := controllerDirectory.LocateDaemon(context.Background(), daemonRecord.ID)
		return locateErr == nil && found && location.EdgeID == testEdgeID
	})

	cloudNetwork, err := cloudclient.NewClient(cloudclient.Config{ControllerAddress: publicControllerAddress, ControllerServerName: testControllerServer, ControllerCAPEM: edgeCAPEM, BootID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	credential, _ := issueR5CloudCredential(t, accessStore, daemonIdentity, "cloud-r6-relay", cloudv1.ClientProduct_CLIENT_PRODUCT_CLI)
	credential.CloudEdgeLocator = append([]byte(nil), edgeLocatorPayload...)
	relayClientFactory := func(configuration pionwebrtc.Configuration) (*pionwebrtc.PeerConnection, error) {
		// 故障注入只移除本次 auto attempt 的 direct candidate，并锁定一个 URL，分别覆盖 TURN UDP/TCP。
		configuration.ICETransportPolicy = pionwebrtc.ICETransportPolicyRelay
		for index := range configuration.ICEServers {
			urls := configuration.ICEServers[index].URLs[:0]
			for _, value := range configuration.ICEServers[index].URLs {
				if strings.Contains(value, "transport="+transport) {
					urls = append(urls, value)
				}
			}
			configuration.ICEServers[index].URLs = urls
		}
		return loopbackAPI.NewPeerConnection(configuration)
	}
	credentialSource := &r5CredentialSource{credential: credential}
	dialer := &cloudadapter.Dialer{
		Peers: pionadapter.Factory{PeerConnections: relayClientFactory}, Cloud: cloudNetwork, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI,
		Authorization: peeradapter.CapabilityAuthorizer{Credentials: credentialSource},
	}
	connectContext, cancelConnect := context.WithTimeout(context.Background(), 90*time.Second)
	ready, err := dialer.Connect(connectContext, r6AutoAttempt(t, daemonIdentity, credential.EndpointID, 1))
	cancelConnect()
	if err != nil {
		t.Fatalf("connect through forced Cloud Relay: %v", err)
	}
	session := ready.(*cloudadapter.Session)
	assertR5TerminalIO(t, session, credential.EndpointID, "r6-relay-before-outage")
	if snapshot, ok := session.ConnectionSnapshot(time.Now().UTC()); !ok || snapshot.ObservedPath != string(endpoint.PathSingleRelay) {
		t.Fatalf("forced Relay selected path = %+v ok=%v", snapshot, ok)
	}
	shutdownR6Controller(t, controllerRuntime, controllerDirectory)
	controllerStopped = true
	eventually(t, 5*time.Second, func() bool { return !edgeRuntime.Ready() })
	assertR5TerminalIO(t, session, credential.EndpointID, "r6-relay-controller-down")
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	offlineContext, cancelOffline := context.WithTimeout(context.Background(), 90*time.Second)
	offlineReady, err := dialer.Connect(offlineContext, r6AutoAttempt(t, daemonIdentity, credential.EndpointID, 2))
	cancelOffline()
	if err != nil {
		t.Fatalf("create new Cloud Relay session while Controller is offline: %v", err)
	}
	offlineSession := offlineReady.(*cloudadapter.Session)
	assertR5TerminalIO(t, offlineSession, credential.EndpointID, "r6-new-relay-controller-down")
	if snapshot, ok := offlineSession.ConnectionSnapshot(time.Now().UTC()); !ok || snapshot.ObservedPath != string(endpoint.PathSingleRelay) {
		t.Fatalf("offline delegated Relay selected path = %+v ok=%v", snapshot, ok)
	}
	if err := offlineSession.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, func() bool {
		depth, depthErr := edgeRuntime.UsageOutboxDepth()
		return depthErr == nil && depth > 0
	})

	secondController, secondDirectory := startR6ControlController(t, certificates, controllerAddress, bindingKeyID, bindingPublicKey, usageStore)
	defer shutdownR6Controller(t, secondController, secondDirectory)
	eventually(t, 8*time.Second, func() bool { return edgeRuntime.Ready() })
	eventually(t, 8*time.Second, func() bool {
		depth, depthErr := edgeRuntime.UsageOutboxDepth()
		return depthErr == nil && depth == 0 && usageStore.uniqueCount() > 0
	})
	shutdownEdge(t, edgeRuntime)
	edgeStopped = true
}

func r6AutoAttempt(t *testing.T, identity remoteauth.Identity, endpointID string, generation clientruntime.SessionGeneration) clientruntime.AttemptRequest {
	t.Helper()
	target := endpoint.Endpoint{
		ID: endpoint.EndpointID(endpointID), DaemonIdentity: endpoint.DaemonIdentity{DeviceID: identity.DeviceID, DeviceFingerprint: identity.Fingerprint},
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{"cloud": {ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser, CredentialRef: "credential:" + endpointID, TargetDeviceID: identity.DeviceID, AccountProfileRef: "default", RelayMode: endpoint.RelayAuto}},
	}
	attempt, err := clientruntime.NewAttemptRequest(target, "cloud", generation, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func startR6ControlController(t *testing.T, certificates certificateFiles, listen, keyID string, publicKey ed25519.PublicKey, usageStore *r6UsageStore) (*controllerruntime.Runtime, *directory.Directory) {
	t.Helper()
	directoryState, err := directory.New(directory.Config{MailboxSize: 1024, GracePeriod: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.NewService(control.Config{
		ControllerID: testControllerID, ControllerBootID: uuid.NewString(), HeartbeatInterval: 250 * time.Millisecond, HeartbeatTimeout: time.Second,
		BindingVerificationKeys: []*cloudv1.VerificationKey{{KeyId: keyID, Algorithm: "Ed25519", PublicKey: publicKey}}, Directory: directoryState,
		UsageStore: usageStore,
	})
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

func shutdownR6Controller(t *testing.T, runtime *controllerruntime.Runtime, directoryState *directory.Directory) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown R6 Controller: %v", err)
	}
	directoryState.Close()
}

func freeTURNAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	packetConn, err := net.ListenPacket("udp4", address)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if err := packetConn.Close(); err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

type r6UsageStore struct {
	mu     sync.Mutex
	events map[string]*cloudv1.UsageEvent
}

func newR6UsageStore() *r6UsageStore {
	return &r6UsageStore{events: make(map[string]*cloudv1.UsageEvent)}
}

func (store *r6UsageStore) CommitRelayUsage(_ context.Context, edgeID string, events []*cloudv1.UsageEvent) ([]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	ack := make([]string, 0, len(events))
	for _, event := range events {
		if event.GetEdgeId() != edgeID {
			return nil, errors.New("usage event targets another Edge")
		}
		key := edgeID + ":" + event.GetEventId()
		if _, exists := store.events[key]; !exists {
			store.events[key] = proto.Clone(event).(*cloudv1.UsageEvent)
		}
		ack = append(ack, event.GetEventId())
	}
	return ack, nil
}

func (store *r6UsageStore) uniqueCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.events)
}
