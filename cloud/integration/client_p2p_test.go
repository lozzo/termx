package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	apilayer "github.com/anytty/anytty/api_layer"
	cloudadapter "github.com/anytty/anytty/client/adapter/cloud"
	peeradapter "github.com/anytty/anytty/client/adapter/peer"
	pionadapter "github.com/anytty/anytty/client/adapter/webrtc/pion"
	"github.com/anytty/anytty/client/endpoint"
	clientruntime "github.com/anytty/anytty/client/runtime"
	cloudclient "github.com/anytty/anytty/cloud/client"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/directoryapi"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	clouddaemon "github.com/anytty/anytty/cloud/daemon"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	"github.com/anytty/anytty/cloud/ticket"
	corev2 "github.com/anytty/anytty/core"
	"github.com/anytty/anytty/proto/apipb"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/proto/remoteauthpb"
	remotedaemon "github.com/anytty/anytty/remote/daemon"
	remotewebrtc "github.com/anytty/anytty/remote/webrtc"
	"github.com/anytty/anytty/shared/remoteauth"
	pionwebrtc "github.com/pion/webrtc/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCloudP2PCompletesCLIAndTUITerminalIOAndTracksMemorySession(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	ticketPublicKey, ticketPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticketKeyID := "ticket-r5"
	controllerRuntime, directoryState := startPresenceController(t, certificates, "127.0.0.1:0", &cloudv1.VerificationKey{KeyId: ticketKeyID, Algorithm: "Ed25519", PublicKey: ticketPublicKey})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = controllerRuntime.Shutdown(ctx)
		directoryState.Close()
	})

	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonIdentity, err := remoteauth.NewIdentity("device-r5", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	daemonRecord := enrollment.Daemon{
		ID: uuid.NewString(), AccountID: uuid.NewString(), AccountName: "R5 Account", DisplayName: "R5 daemon",
		DeviceID: daemonIdentity.DeviceID, DeviceFingerprint: daemonIdentity.Fingerprint, DevicePublicKey: append(ed25519.PublicKey(nil), daemonIdentity.PublicKey...),
		Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	edgeStore := &r5EdgeStore{edge: edgeconfig.Edge{ID: testEdgeID, Name: "R5 Edge", Region: "local", Capacity: 100, PublicEndpoint: "127.0.0.1:1", Enabled: true, ConfigVersion: 1, Revision: 1}}
	_, configSigningKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: edgeStore, SigningKey: configSigningKey, SigningKeyID: "config-r5", ClaimTTL: time.Minute})
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
		Store:       identityStore, Edges: edges, Directory: directoryState, TicketSigningKey: ticketPrivateKey, TicketSigningKeyID: ticketKeyID,
		EdgeCACertificate: edgeCAPEM, EnrollmentTTL: time.Minute, ChallengeTTL: time.Minute, AgentTicketTTL: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	directoryService, err := directoryapi.NewService(directoryapi.Config{
		Entitlement: testEntitlementReader{},
		Store:       identityStore, Directory: directoryState, Edges: edges, EdgeCACertificate: edgeCAPEM,
		TicketSigningKey: ticketPrivateKey, TicketSigningKeyID: ticketKeyID, ChallengeTTL: time.Minute, ClientTicketTTL: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	publicControllerAddress := startR5PublicController(t, certificates, enrollmentService, directoryService)

	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
		ControllerAddress: controllerRuntime.GRPCAddress(), ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
		EdgeID: testEdgeID, BootID: testEdgeBootID, SoftwareVersion: "r5-integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdownEdge(t, edgeRuntime) })
	edgeStore.setPublicEndpoint(edgeRuntime.PublicAddress())
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
	t.Cleanup(func() { _ = accessStore.Close() })
	if err := accessStore.ConfigureManagedRouteGrantIssuer(func(clientPublicKey ed25519.PublicKey, product uint32, now time.Time) ([]byte, error) {
		claims := &cloudv1.CloudRouteGrantClaims{
			GrantId: uuid.NewString(), DaemonId: daemonRecord.ID, ClientPublicKey: append([]byte(nil), clientPublicKey...), Product: cloudv1.ClientProduct(product),
			IssuedAt: timestamppb.New(now.UTC()), ExpiresAt: timestamppb.New(now.UTC().Add(7 * 24 * time.Hour)),
		}
		signed, signErr := ticket.SignCloudRouteGrant(daemonIdentity, claims)
		if signErr != nil {
			return nil, signErr
		}
		return proto.MarshalOptions{Deterministic: true}.Marshal(signed)
	}); err != nil {
		t.Fatal(err)
	}
	if err := accessStore.ConfigureManagedPairingGrantIssuer(func(claimDigest []byte, expiresAt time.Time, issuedAt time.Time) ([]byte, error) {
		claims := &cloudv1.PairingRouteGrantClaims{
			GrantId: uuid.NewString(), DaemonId: daemonRecord.ID, DeviceId: daemonIdentity.DeviceID, PairingClaimSha256: append([]byte(nil), claimDigest...),
			IssuedAt: timestamppb.New(issuedAt.UTC()), ExpiresAt: timestamppb.New(expiresAt.UTC()),
		}
		signed, signErr := ticket.SignPairingRouteGrant(daemonIdentity, claims)
		if signErr != nil {
			return nil, signErr
		}
		return proto.MarshalOptions{Deterministic: true}.Marshal(signed)
	}); err != nil {
		t.Fatal(err)
	}
	coreServer := corev2.NewServer(
		corev2.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory),
		corev2.WithSocketPath(filepath.Join(t.TempDir(), "core.sock")),
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = coreServer.Shutdown(ctx)
	})
	loopbackAPI := r5LoopbackWebRTCAPI()
	daemonRuntime, err := clouddaemon.NewRuntime(clouddaemon.Config{
		Record:   clouddaemon.EnrollmentRecord{Version: 1, DaemonID: daemonRecord.ID, AccountID: daemonRecord.AccountID, ControllerAddress: publicControllerAddress, ControllerServerName: testControllerServer, EnrolledAt: time.Now().UTC()},
		Identity: daemonIdentity, AccessStore: accessStore, SoftwareVersion: "r5-integration",
		ControllerTLS: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: certificates.rootPool, ServerName: testControllerServer},
		Answerer:      remotewebrtc.Answerer{Handler: remotedaemon.SessionAcceptor{Core: coreServer, Identity: daemonIdentity, AccessStore: accessStore}, PeerConnections: loopbackAPI.NewPeerConnection},
	})
	if err != nil {
		t.Fatal(err)
	}
	daemonContext, cancelDaemon := context.WithCancel(context.Background())
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemonRuntime.Run(daemonContext) }()
	t.Cleanup(func() {
		cancelDaemon()
		select {
		case runErr := <-daemonDone:
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				t.Errorf("stop R5 daemon runtime: %v", runErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("R5 daemon runtime did not stop")
		}
	})
	eventually(t, 8*time.Second, func() bool {
		location, found, locateErr := directoryState.LocateDaemon(context.Background(), daemonRecord.ID)
		return locateErr == nil && found && location.EdgeID == testEdgeID
	})

	cloudNetwork, err := cloudclient.NewClient(cloudclient.Config{ControllerAddress: publicControllerAddress, ControllerServerName: testControllerServer, ControllerCAPEM: edgeCAPEM})
	if err != nil {
		t.Fatal(err)
	}
	pairedCredential := pairR5CloudCredential(t, accessStore, daemonIdentity, cloudNetwork, loopbackAPI)
	pairedDialer := &cloudadapter.Dialer{
		Peers: pionadapter.Factory{PeerConnections: loopbackAPI.NewPeerConnection}, Cloud: cloudNetwork, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI,
		Authorization: peeradapter.CapabilityAuthorizer{Credentials: r5CredentialSource{credential: pairedCredential}},
	}
	pairedContext, cancelPaired := context.WithTimeout(context.Background(), 45*time.Second)
	pairedReady, err := pairedDialer.Connect(pairedContext, r5CloudAttempt(t, daemonIdentity, pairedCredential.EndpointID, 80))
	if err != nil {
		cancelPaired()
		t.Fatalf("connect with credential issued through Cloud pairing: %v", err)
	}
	cancelPaired()
	if err := pairedReady.Close(); err != nil {
		t.Fatalf("close post-pairing Cloud session: %v", err)
	}
	eventually(t, 5*time.Second, func() bool { return r5SessionCount(directoryState) == 0 })

	products := []cloudv1.ClientProduct{cloudv1.ClientProduct_CLIENT_PRODUCT_CLI, cloudv1.ClientProduct_CLIENT_PRODUCT_TUI}
	var revokedCredential remoteauth.ClientAccessCredential
	var revokedGrantID string
	for index, product := range products {
		endpointID := fmt.Sprintf("cloud-r5-%s", strings.ToLower(strings.TrimPrefix(product.String(), "CLIENT_PRODUCT_")))
		credential, grantID := issueR5CloudCredential(t, accessStore, daemonIdentity, endpointID, product)
		dialer := &cloudadapter.Dialer{
			Peers: pionadapter.Factory{PeerConnections: loopbackAPI.NewPeerConnection}, Cloud: cloudNetwork, Product: product,
			Authorization: peeradapter.CapabilityAuthorizer{Credentials: r5CredentialSource{credential: credential}},
		}
		attempt := r5CloudAttempt(t, daemonIdentity, endpointID, clientruntime.SessionGeneration(index+1))
		connectContext, cancelConnect := context.WithTimeout(context.Background(), 45*time.Second)
		ready, connectErr := dialer.Connect(connectContext, attempt)
		if connectErr != nil {
			cancelConnect()
			t.Fatalf("connect %s through Cloud P2P: %v", product, connectErr)
		}
		// 模拟 route racer 发布 winner 后取消 attempt；ClientGateway 必须继续由 ReadyPeerSession 持有。
		cancelConnect()
		session, ok := ready.(*cloudadapter.Session)
		if !ok {
			_ = ready.Close()
			t.Fatalf("Cloud ready session type = %T", ready)
		}
		eventually(t, 5*time.Second, func() bool { return r5SessionCount(directoryState) == 1 })
		assertR5TerminalIO(t, session, endpointID, strings.ToLower(product.String()))
		if err := session.Close(); err != nil {
			t.Fatalf("close %s Cloud session: %v", product, err)
		}
		eventually(t, 5*time.Second, func() bool { return r5SessionCount(directoryState) == 0 })
		if product == cloudv1.ClientProduct_CLIENT_PRODUCT_CLI {
			revokedCredential, revokedGrantID = credential, grantID
		}
	}

	if _, err := accessStore.RevokeGrant(revokedGrantID); err != nil {
		t.Fatal(err)
	}
	revokedDialer := &cloudadapter.Dialer{
		Peers: pionadapter.Factory{PeerConnections: loopbackAPI.NewPeerConnection}, Cloud: cloudNetwork, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI,
		Authorization: peeradapter.CapabilityAuthorizer{Credentials: r5CredentialSource{credential: revokedCredential}},
	}
	revokedContext, cancelRevoked := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelRevoked()
	if _, err := revokedDialer.Connect(revokedContext, r5CloudAttempt(t, daemonIdentity, revokedCredential.EndpointID, 99)); err == nil || !strings.Contains(err.Error(), "CLIENT_REVOKED") {
		t.Fatalf("revoked Cloud client error = %v, want daemon precheck rejection", err)
	}
	eventually(t, 5*time.Second, func() bool { return r5SessionCount(directoryState) == 0 })
}

func pairR5CloudCredential(t *testing.T, store *remoteauth.AccessStore, daemonIdentity remoteauth.Identity, network *cloudclient.Client, api *pionwebrtc.API) remoteauth.ClientAccessCredential {
	t.Helper()
	const endpointID = "cloud-r5-paired"
	issued, err := store.IssuePairingClaim(remoteauth.PairingIssueOptions{
		Scope: remoteauth.FullDaemonScope(), TicketTTL: time.Minute, GrantLifetime: time.Hour,
		Routes: []*remoteauthpb.EndpointRouteConfigV1{{
			SchemaVersion: 1, RouteId: "cloud", Enabled: true,
			Route: &remoteauthpb.EndpointRouteConfigV1_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.ManagedWebRTCRouteConfig{TargetDeviceId: daemonIdentity.DeviceID, RelayMode: remoteauthpb.ManagedWebRTCRelayMode_MANAGED_WEBRTC_RELAY_MODE_DIRECT}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := remoteauth.PairingClaimEndpointCandidate(issued.Offer)
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity(endpointID, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := remoteauth.NewPrivateClientAccessSigner(clientIdentity)
	if err != nil {
		t.Fatal(err)
	}
	route := candidate.Routes[0]
	route.CredentialRef = "credential:" + endpointID
	route.RelayMode = endpoint.RelayDirect
	target := endpoint.Endpoint{
		ID: endpointID, DaemonIdentity: candidate.Identity,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{route.ID: route},
	}
	attempt, err := clientruntime.NewAttemptRequest(target, route.ID, 70, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	pairContext, cancelPair := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelPair()
	paired, err := (&cloudadapter.PairingConnector{
		Peers: pionadapter.Factory{PeerConnections: api.NewPeerConnection}, Cloud: network, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI, Now: time.Now,
	}).Redeem(pairContext, attempt, remoteauth.ClientPairingRequest{
		ExpectedDeviceID: daemonIdentity.DeviceID, ExpectedDeviceFingerprint: daemonIdentity.Fingerprint,
		PairingClaimOffer: issued.OfferPayload, Identity: clientIdentity, Signer: signer, ClientLabel: "cloud-pairing-e2e", ClientProduct: uint32(cloudv1.ClientProduct_CLIENT_PRODUCT_CLI),
	})
	if err != nil {
		t.Fatalf("pair through Cloud bootstrap: %v", err)
	}
	if paired.Grant == "" || len(paired.CloudRouteGrant) == 0 || len(paired.Bundle) == 0 || !store.AllowsClientPublicKey(clientIdentity.PublicKey, time.Now().UTC()) {
		t.Fatalf("Cloud pairing result is incomplete: %#v", paired)
	}
	intruderIdentity, err := remoteauth.GenerateClientAccessIdentity("cloud-r5-intruder", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intruderSigner, err := remoteauth.NewPrivateClientAccessSigner(intruderIdentity)
	if err != nil {
		t.Fatal(err)
	}
	intruderRoute := route
	intruderRoute.CredentialRef = "credential:cloud-r5-intruder"
	intruderTarget := endpoint.Endpoint{ID: "cloud-r5-intruder", DaemonIdentity: candidate.Identity, Routes: map[endpoint.RouteID]endpoint.AccessRoute{intruderRoute.ID: intruderRoute}}
	intruderAttempt, err := clientruntime.NewAttemptRequest(intruderTarget, intruderRoute.ID, 71, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	intruderContext, cancelIntruder := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelIntruder()
	_, err = (&cloudadapter.PairingConnector{
		Peers: pionadapter.Factory{PeerConnections: api.NewPeerConnection}, Cloud: network, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI, Now: time.Now,
	}).Redeem(intruderContext, intruderAttempt, remoteauth.ClientPairingRequest{
		ExpectedDeviceID: daemonIdentity.DeviceID, ExpectedDeviceFingerprint: daemonIdentity.Fingerprint,
		PairingClaimOffer: issued.OfferPayload, Identity: intruderIdentity, Signer: intruderSigner, ClientLabel: "cloud-pairing-intruder", ClientProduct: uint32(cloudv1.ClientProduct_CLIENT_PRODUCT_CLI),
	})
	if err == nil || !strings.Contains(err.Error(), "PAIRING_CLAIM_INVALID") {
		t.Fatalf("bound Cloud pairing claim admitted another client: %v", err)
	}
	return remoteauth.ClientAccessCredential{
		Version: 1, EndpointID: endpointID, Identity: clientIdentity, CapabilityGrant: paired.Grant, CloudRouteGrant: paired.CloudRouteGrant, UpdatedAt: time.Now().UTC(),
	}
}

func issueR5CloudCredential(t *testing.T, store *remoteauth.AccessStore, daemonIdentity remoteauth.Identity, endpointID string, product cloudv1.ClientProduct) (remoteauth.ClientAccessCredential, string) {
	t.Helper()
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity(endpointID, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Scope: remoteauth.FullDaemonScope(), TicketTTL: time.Minute, GrantLifetime: time.Hour,
		Routes: []*remoteauthpb.EndpointRouteConfigV1{{SchemaVersion: 1, RouteId: "cloud", Enabled: true, Route: &remoteauthpb.EndpointRouteConfigV1_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.ManagedWebRTCRouteConfig{TargetDeviceId: daemonIdentity.DeviceID, RelayMode: remoteauthpb.ManagedWebRTCRelayMode_MANAGED_WEBRTC_RELAY_MODE_DIRECT}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	exchanged, err := store.RedeemPairingBundleForProduct(payload, clientIdentity.PublicKey, product.String(), uint32(product), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(exchanged.CloudRouteGrant) == 0 {
		t.Fatal("managed pairing did not issue CloudRouteGrant")
	}
	return remoteauth.ClientAccessCredential{Version: 1, EndpointID: endpointID, Identity: clientIdentity, CapabilityGrant: exchanged.Grant, CloudRouteGrant: exchanged.CloudRouteGrant, UpdatedAt: time.Now().UTC()}, exchanged.GrantID
}

func r5CloudAttempt(t *testing.T, identity remoteauth.Identity, endpointID string, generation clientruntime.SessionGeneration) clientruntime.AttemptRequest {
	t.Helper()
	target := endpoint.Endpoint{
		ID: endpoint.EndpointID(endpointID), DaemonIdentity: endpoint.DaemonIdentity{DeviceID: identity.DeviceID, DeviceFingerprint: identity.Fingerprint},
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{"cloud": {ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser, CredentialRef: "credential:" + endpointID, TargetDeviceID: identity.DeviceID, AccountProfileRef: "default", RelayMode: endpoint.RelayDirect}},
	}
	attempt, err := clientruntime.NewAttemptRequest(target, "cloud", generation, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func assertR5TerminalIO(t *testing.T, session *cloudadapter.Session, endpointID, suffix string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	terminalID := "term-r5-" + suffix
	if _, err := session.TerminalCreate(ctx, &apipb.TerminalCreateCommand{Terminal: &apipb.TerminalCreateSpec{TerminalId: terminalID, Command: testIdleTerminalCommand(), Size: &apipb.TerminalSize{Cols: 80, Rows: 24}}}); err != nil {
		t.Fatalf("create terminal through Cloud: %v", err)
	}
	attached, err := session.TerminalAttach(ctx, &apipb.TerminalAttachCommand{Terminal: &apipb.TerminalRef{EndpointId: endpointID, TerminalId: terminalID}, Mode: apipb.AttachmentMode_ATTACHMENT_MODE_COLLABORATOR, ResizePolicy: apipb.ResizePolicy_RESIZE_POLICY_OWNER, SurfaceId: "r5-surface", ViewId: "r5-view"})
	if err != nil {
		t.Fatalf("attach terminal through Cloud: %v", err)
	}
	resource := attached.GetAttachment().GetResource()
	marker := "anytty-r5-" + suffix
	if err := session.TerminalInput(ctx, &apipb.TerminalInputCommand{Attachment: resource, Data: []byte(marker + "\n")}); err != nil {
		t.Fatalf("write terminal input through Cloud: %v", err)
	}
	eventually(t, 5*time.Second, func() bool {
		screen, screenErr := session.LiveScreen(ctx, &apipb.LiveScreenGetCommand{Terminal: &apipb.TerminalRef{EndpointId: endpointID, TerminalId: terminalID}})
		return screenErr == nil && strings.Contains(r5ScreenText(screen), marker)
	})
}

func r5ScreenText(screen *apipb.NativeScreenResult) string {
	var text strings.Builder
	for _, row := range screen.GetRows() {
		for _, cell := range row.GetCells() {
			text.WriteString(cell.GetContent())
		}
		text.WriteByte('\n')
	}
	return text.String()
}

func r5SessionCount(state *directory.Directory) int {
	projection, found, err := state.Edge(context.Background(), testEdgeID)
	if err != nil || !found {
		return -1
	}
	return projection.SessionCount
}

func r5LoopbackWebRTCAPI() *pionwebrtc.API {
	settings := pionwebrtc.SettingEngine{}
	settings.SetNetworkTypes([]pionwebrtc.NetworkType{pionwebrtc.NetworkTypeUDP4})
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetIPFilter(func(address net.IP) bool { return address.IsLoopback() })
	return pionwebrtc.NewAPI(pionwebrtc.WithSettingEngine(settings))
}

func startR5PublicController(t *testing.T, certificates certificateFiles, enrollmentService *enrollment.Service, directoryService *directoryapi.Service) string {
	t.Helper()
	certificate, err := tls.LoadX509KeyPair(certificates.controllerCert, certificates.controllerKey)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}})))
	cloudv1.RegisterEnrollmentServiceServer(server, enrollmentService)
	cloudv1.RegisterDirectoryServiceServer(server, directoryService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

type r5CredentialSource struct {
	credential remoteauth.ClientAccessCredential
}

func (source r5CredentialSource) ResolveClientCredential(context.Context, string, string) (remoteauth.ClientAccessCredential, error) {
	return source.credential, nil
}

type r5EnrollmentStore struct{ daemon enrollment.Daemon }

func (r5EnrollmentStore) CreateDaemonEnrollment(context.Context, string, string, string, []byte, time.Time, time.Time) (string, error) {
	return "", errors.New("not implemented by R5 identity fixture")
}
func (r5EnrollmentStore) ConsumeDaemonEnrollment(context.Context, []byte, string, string, ed25519.PublicKey, time.Time) (enrollment.Daemon, error) {
	return enrollment.Daemon{}, errors.New("not implemented by R5 identity fixture")
}
func (store r5EnrollmentStore) GetDaemon(_ context.Context, daemonID string) (enrollment.Daemon, error) {
	if daemonID != store.daemon.ID {
		return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
	}
	return store.daemon, nil
}
func (store r5EnrollmentStore) ListDaemons(context.Context) ([]enrollment.Daemon, error) {
	return []enrollment.Daemon{store.daemon}, nil
}

type r5EdgeStore struct {
	mu   sync.RWMutex
	edge edgeconfig.Edge
}

func (store *r5EdgeStore) setPublicEndpoint(value string) {
	store.mu.Lock()
	store.edge.PublicEndpoint = value
	store.mu.Unlock()
}
func (store *r5EdgeStore) ListEdges(context.Context) ([]edgeconfig.Edge, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return []edgeconfig.Edge{store.edge}, nil
}
func (store *r5EdgeStore) GetEdge(_ context.Context, edgeID string) (edgeconfig.Edge, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if edgeID != store.edge.ID {
		return edgeconfig.Edge{}, errors.New("Edge not found")
	}
	return store.edge, nil
}
func (*r5EdgeStore) CreateEdge(context.Context, edgeconfig.Edge, []byte, time.Time) error {
	return errors.New("not implemented by R5 Edge fixture")
}
func (*r5EdgeStore) UpdateEdge(context.Context, edgeconfig.UpdateInput, edgeconfig.Edge) error {
	return errors.New("not implemented by R5 Edge fixture")
}
func (*r5EdgeStore) ConsumeInstallClaim(context.Context, []byte, []byte, time.Time) (edgeconfig.Edge, error) {
	return edgeconfig.Edge{}, errors.New("not implemented by R5 Edge fixture")
}
func (*r5EdgeStore) ConsumeBootstrapClaim(context.Context, []byte, string, []byte) (edgeconfig.Edge, error) {
	return edgeconfig.Edge{}, errors.New("not implemented by R5 Edge fixture")
}
