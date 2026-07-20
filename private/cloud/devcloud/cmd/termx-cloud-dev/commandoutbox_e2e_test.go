package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	cloudcommerce "github.com/lozzow/termx/private/cloud/control-plane/commerce"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	"github.com/lozzow/termx/private/cloud/controller"
	cloudedge "github.com/lozzow/termx/private/cloud/edge"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	remotedaemon "github.com/lozzow/termx/remote/daemon"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestSignedCloseCommandCrossesControllerEdgeAndDaemonHTTPBoundaries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	hubPublic, hubPrivate, _ := ed25519.GenerateKey(rand.Reader)
	projectionPublic, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	daemonControlPublic, daemonControlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	daemonPublic, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: "relay-fingerprint"}
	databasePath := filepath.Join(t.TempDir(), "controller.db")
	catalogPath := filepath.Join(findRepoRoot(t), "private/cloud/web-controller/config/plans.json")
	account := seedCommandAccount(t, databasePath, catalogPath, now)
	controllerRuntime, err := controller.Start(controller.Config{DatabasePath: databasePath, PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: catalogPath, ProjectionKeyID: "projection-1", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-1", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate), Deployments: []controller.DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic)}}, Devices: []*cloudpb.CloudDevicePolicy{{AccountId: account.GetAccountId(), DeviceId: "client-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: account.GetAuthRevision()}, {AccountId: account.GetAccountId(), DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision(), PublicKey: daemonPublic}}, Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: account.GetAccountId(), HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer controllerRuntime.Close(context.Background())
	edgeRuntime, err := cloudedge.Start(cloudedge.Config{ControllerURL: controllerRuntime.Manifest().InternalControlURL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: filepath.Join(t.TempDir(), "usage.outbox"), Metadata: metadata, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(hubPrivate), ControllerProjectionKeyID: "projection-1", ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPublic)})
	if err != nil {
		t.Fatal(err)
	}
	defer edgeRuntime.Close(context.Background())
	readyContext, cancelReady := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelReady()
	if err := edgeRuntime.WaitReady(readyContext); err != nil {
		t.Fatal(err)
	}
	signer, _ := servicecredential.NewSigner("projection-1", projectionPrivate, now.Add(-time.Minute), now.Add(time.Hour))
	issuer, _ := servicecredential.NewEdgeAccessIssuer("termx-cloud-controller", signer)
	daemonToken, _ := issuer.IssueEdgeAccessForPrincipal("daemon-token", "hub-1", account.GetAccountId(), "daemon-1", servicecredential.EdgePrincipalDaemon, account.GetAuthRevision(), time.Hour, now)
	clientToken, _ := issuer.IssueEdgeAccessForPrincipal("client-token", "hub-1", account.GetAccountId(), "client-1", servicecredential.EdgePrincipalClient, account.GetAuthRevision(), time.Hour, now)
	daemonSession, _ := session.New(session.Metadata{Kind: session.KindDevice, AccountID: account.GetAccountId(), DeviceID: "daemon-1", ExpiresAt: now.Add(time.Hour)}, daemonToken, now)
	clientSession, _ := session.New(session.Metadata{Kind: session.KindAccount, AccountID: account.GetAccountId(), DeviceID: "client-1", ExpiresAt: now.Add(time.Hour)}, clientToken, now)
	adapter, _ := httpapi.New(httpapi.Config{ControlPlaneURL: edgeRuntime.Manifest().HubURL, HubURL: edgeRuntime.Manifest().HubURL})
	challenge, err := adapter.BeginPresence(context.Background(), daemonSession.Authorization(), &cloudpb.BeginPresenceRequest{DeviceId: "daemon-1"})
	if err != nil {
		t.Fatal(err)
	}
	signedAt := now.Add(time.Second)
	proofBytes, _ := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{PresenceSessionId: challenge.GetPresenceSessionId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(), DeviceId: "daemon-1", DevicePublicKey: daemonPublic, SignedAtUnixNano: signedAt.UnixNano()})
	presenceContext, cancelPresence := context.WithCancel(context.Background())
	defer cancelPresence()
	presence, err := adapter.OpenPresence(presenceContext, daemonSession.Authorization(), &cloudpb.OpenPresenceRequest{PresenceSessionId: challenge.GetPresenceSessionId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-1", DevicePublicKey: daemonPublic, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(daemonPrivate, proofBytes), SignedAtUnixNano: signedAt.UnixNano()}, Metadata: &cloudpb.DeviceMetadata{Platform: "test", TermxVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	if event, err := presence.Receive(context.Background()); err != nil || event.GetReady() == nil {
		t.Fatalf("PresenceReady = (%v, %v)", event, err)
	}
	resolved, err := adapter.ResolveEndpoint(context.Background(), clientSession.Authorization(), &cloudpb.ResolveEndpointRequest{EndpointId: "endpoint-1", TargetDeviceId: "daemon-1"})
	if err != nil {
		t.Fatal(err)
	}
	signaling, err := adapter.CreateSignalingSession(context.Background(), clientSession.Authorization(), &cloudpb.CreateSignalingSessionRequest{EndpointId: "endpoint-1", ManagedSessionId: resolved.GetManagedSessionId(), TargetDeviceId: "daemon-1", OfferSdp: "offer", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY})
	if err != nil {
		t.Fatal(err)
	}
	defer signaling.Close()
	offerEvent, _ := presence.Receive(context.Background())
	offer := offerEvent.GetOffer()
	_, err = adapter.CompleteSignalingOffer(context.Background(), daemonSession.Authorization(), &cloudpb.CompleteSignalingOfferRequest{SignalingSessionId: offer.GetSignalingSessionId(), Result: &cloudpb.CompleteSignalingOfferRequest_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: offer.GetSignalingSessionId(), Sdp: "answer"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = signaling.Receive(context.Background())
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: resolved.GetManagedSessionId(), SessionIncarnation: offer.GetSessionIncarnation(), AssignmentEpoch: 1, ControlPresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1"}
	report := &cloudpb.ReportDaemonRuntimeRequest{ReportId: "runtime-1:1", HubId: "hub-1", AssignmentEpoch: 1, PresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", RegistryRevision: 1, PeerSessions: &cloudpb.PeerSessionInventorySnapshot{ReportId: "runtime-1:1", DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-1", AssignmentEpoch: 1, ControlPresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", RegistryRevision: 1, Sessions: []*cloudpb.ManagedPeerSessionProjection{{Target: target, ClientDeviceId: "client-1", EstablishedPresenceSessionId: challenge.GetPresenceSessionId(), ControlOwnerHubId: "hub-1", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}}, ObservedAtUnixMillis: now.UnixMilli()}}
	if _, err := adapter.ReportDaemonRuntime(context.Background(), daemonSession.Authorization(), report); err != nil {
		t.Fatal(err)
	}
	cookies := loginCommandAccount(t, controllerRuntime.Manifest().PublicURL)
	created := createCloseCommandWhenTopologyArrives(t, controllerRuntime.Manifest().PublicURL, cookies, account.GetAccountId(), target)
	commandContext, cancelCommand := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelCommand()
	commandEvent, err := presence.Receive(commandContext)
	if err != nil {
		t.Fatal(err)
	}
	verifier, _ := cloudpb.NewDaemonControlVerifier(map[string]ed25519.PublicKey{"daemon-control-1": daemonControlPublic})
	if err := verifier.Verify(commandEvent.GetDaemonCommand(), time.Now().UTC()); err != nil {
		t.Fatalf("daemon command signature = %v", err)
	}
	result := &cloudpb.DaemonCommandResult{CommandId: created.GetChildren()[0].GetChildCommandId(), DaemonDeviceId: "daemon-1", ManagedSessionId: target.GetManagedSessionId(), SessionIncarnation: target.GetSessionIncarnation(), AssignmentEpoch: 1, PresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, ClosedRegistryRevision: 2, CompletedAtUnixMillis: time.Now().UTC().UnixMilli()}
	if _, err := adapter.ReportDaemonCommandResult(context.Background(), daemonSession.Authorization(), &cloudpb.ReportDaemonCommandResultRequest{Result: result}); err != nil {
		t.Fatal(err)
	}
	waitCommandApplied(t, controllerRuntime.Manifest().PublicURL, cookies, created.GetCommandId())

	daemonIdentity, err := remoteauth.NewIdentity("daemon-1", daemonPrivate)
	if err != nil {
		t.Fatal(err)
	}
	accessStore, err := remoteauth.LoadAccessStore(filepath.Join(t.TempDir(), "access"), daemonIdentity, remoteauth.AccessStoreOptions{Now: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	defer accessStore.Close()
	clientIdentity, _ := remoteauth.GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	bundle, _, err := accessStore.IssuePairingBundle(remoteauth.PairingIssueOptions{Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	bundlePayload, _ := remoteauth.EncodePairingBundle(bundle)
	if _, err := accessStore.RedeemPairingBundle(bundlePayload, clientIdentity.PublicKey, "Android", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	accessRecord := accessStore.ListClientAccess()[0]
	opaqueReference := remotedaemon.OpaqueAccessReference("daemon-1", accessRecord.GrantID)
	daemonRuntime, err := remotedaemon.NewManagedRuntime("daemon-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemonRuntime.BindPresence("hub-1", 1, challenge.GetPresenceSessionId(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	accessTarget := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-access", SessionIncarnation: 1, AssignmentEpoch: 1, ControlPresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: daemonRuntime.RuntimeGeneration()}
	closer := &accessSessionCloser{done: make(chan struct{})}
	handle, _, err := daemonRuntime.Registry().Begin(&cloudpb.ManagedPeerSessionProjection{Target: accessTarget, ClientDeviceId: "client-1", EstablishedPresenceSessionId: challenge.GetPresenceSessionId(), ControlOwnerHubId: "hub-1", OpaqueAccessReference: opaqueReference, ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}, closer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	closer.handle = handle
	if _, err := handle.MarkReady(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	peerInventory, err := daemonRuntime.Registry().Inventory("access-report", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	accessInventory := remotedaemon.BuildTerminalAccessInventory(accessStore, "access-report", "daemon-1", "hub-1", 1, challenge.GetPresenceSessionId(), daemonRuntime.RuntimeGeneration(), peerInventory.GetRegistryRevision(), time.Now().UTC())
	accessReport := &cloudpb.ReportDaemonRuntimeRequest{ReportId: "access-report", HubId: "hub-1", AssignmentEpoch: 1, PresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: daemonRuntime.RuntimeGeneration(), RegistryRevision: peerInventory.GetRegistryRevision(), PeerSessions: peerInventory, TerminalAccesses: accessInventory}
	if _, err := adapter.ReportDaemonRuntime(context.Background(), daemonSession.Authorization(), accessReport); err != nil {
		t.Fatal(err)
	}
	waitTerminalAccessProjection(t, controllerRuntime.Manifest().PublicURL, cookies, opaqueReference)
	accessCommand := createTerminalAccessCommandWhenTopologyArrives(t, controllerRuntime.Manifest().PublicURL, cookies, account.GetAccountId(), opaqueReference)
	accessCommandContext, cancelAccessCommand := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelAccessCommand()
	accessEvent, err := presence.Receive(accessCommandContext)
	if err != nil {
		t.Fatal(err)
	}
	controlReceipts, err := remotedaemon.LoadControlReceiptStore(filepath.Join(t.TempDir(), "control"), daemonIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer controlReceipts.Close()
	if err := controlReceipts.InstallEnrollment(&cloudpb.DaemonControlEnrollment{AccountId: account.GetAccountId(), DaemonDeviceId: "daemon-1", AuthEpoch: account.GetAuthRevision(), EnrolledAtUnixMillis: now.UnixMilli(), VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: "daemon-control-1", PublicKey: daemonControlPublic, NotBeforeUnixMillis: now.Add(-time.Hour).UnixMilli(), NotAfterUnixMillis: now.Add(time.Hour).UnixMilli()}}}); err != nil {
		t.Fatal(err)
	}
	accessResult, err := daemonRuntime.ExecuteControlCommand(context.Background(), accessEvent.GetDaemonCommand(), controlReceipts, accessStore, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if accessResult.GetOpaqueAccessReference() != opaqueReference || accessResult.GetClosedSessionCount() != 1 || accessResult.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || !closer.requested {
		t.Fatalf("daemon terminal revoke result = %v closer=%v", accessResult, closer.requested)
	}
	if _, err := adapter.ReportDaemonCommandResult(context.Background(), daemonSession.Authorization(), &cloudpb.ReportDaemonCommandResultRequest{Result: accessResult}); err != nil {
		t.Fatal(err)
	}
	waitCommandApplied(t, controllerRuntime.Manifest().PublicURL, cookies, accessCommand.GetCommandId())
	if !accessStore.Revoked(accessRecord.RevocationID) {
		t.Fatal("terminal grant remained active after applied Cloud revoke")
	}
}

type accessSessionCloser struct {
	handle    *remotedaemon.ManagedSessionHandle
	done      chan struct{}
	requested bool
}

func (closer *accessSessionCloser) RequestClose() {
	if closer.requested {
		return
	}
	closer.requested = true
	_, _ = closer.handle.MarkClosed("terminal_access_revoked", time.Now().UTC())
	close(closer.done)
}

func (closer *accessSessionCloser) Done() <-chan struct{} { return closer.done }

func seedCommandAccount(t *testing.T, databasePath, catalogPath string, now time.Time) *cloudpb.AccountProjection {
	t.Helper()
	store, _ := cloudsqlite.Open(databasePath)
	defer store.Close()
	catalog, _ := webcontroller.LoadCatalog(catalogPath)
	service, _ := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalog.Contract(), Now: func() time.Time { return now }})
	registered, err := service.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "command@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	return registered.GetSession().GetAccount()
}

func loginCommandAccount(t *testing.T, origin string) map[string]*http.Cookie {
	t.Helper()
	body, _ := protojson.Marshal(&cloudpb.PasswordLoginRequest{Email: "command@example.com", Password: "secure-password"})
	request, _ := http.NewRequest(http.MethodPost, origin+"/api/v1/account/login", bytes.NewReader(body))
	request.Header.Set("Origin", origin)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("login = (%v, %v)", response, err)
	}
	defer response.Body.Close()
	result := map[string]*http.Cookie{}
	for _, cookie := range response.Cookies() {
		result[cookie.Name] = cookie
	}
	return result
}

func createCloseCommandWhenTopologyArrives(t *testing.T, origin string, cookies map[string]*http.Cookie, accountID string, target *cloudpb.ManagedPeerSessionTarget) *cloudpb.ManagementCommandProjection {
	t.Helper()
	requestBody, _ := protojson.Marshal(&cloudpb.CreateManagementCommandRequest{AccountId: accountID, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_PeerSession{PeerSession: target}}, IdempotencyKey: "close-1"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		request := authenticatedCommandRequest(http.MethodPost, origin+"/api/v1/management/commands", requestBody, origin, cookies, true)
		response, err := http.DefaultClient.Do(request)
		if err == nil && response.StatusCode == http.StatusAccepted {
			defer response.Body.Close()
			value := &cloudpb.CreateManagementCommandResponse{}
			if err := protojson.Unmarshal(readResponse(t, response), value); err != nil {
				t.Fatal(err)
			}
			return value.GetCommand()
		}
		if response != nil {
			response.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("management command topology did not become available: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func createTerminalAccessCommandWhenTopologyArrives(t *testing.T, origin string, cookies map[string]*http.Cookie, accountID, opaqueReference string) *cloudpb.ManagementCommandProjection {
	t.Helper()
	target := &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: "daemon-1", OpaqueAccessReference: opaqueReference}
	requestBody, _ := protojson.Marshal(&cloudpb.CreateManagementCommandRequest{AccountId: accountID, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_TERMINAL_ACCESS, Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_TerminalAccess{TerminalAccess: target}}, IdempotencyKey: "revoke-access-1"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		request := authenticatedCommandRequest(http.MethodPost, origin+"/api/v1/management/commands", requestBody, origin, cookies, true)
		response, err := http.DefaultClient.Do(request)
		if err == nil && response.StatusCode == http.StatusAccepted {
			defer response.Body.Close()
			value := &cloudpb.CreateManagementCommandResponse{}
			if err := protojson.Unmarshal(readResponse(t, response), value); err != nil {
				t.Fatal(err)
			}
			return value.GetCommand()
		}
		if response != nil {
			response.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal access command topology did not become available: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitTerminalAccessProjection(t *testing.T, origin string, cookies map[string]*http.Cookie, opaqueReference string) {
	t.Helper()
	body, _ := protojson.Marshal(&cloudpb.ListDaemonTerminalAccessRequest{DaemonDeviceId: "daemon-1", State: cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE, Page: &cloudpb.PageRequest{PageSize: 10}})
	deadline := time.Now().Add(3 * time.Second)
	for {
		request := authenticatedCommandRequest(http.MethodPost, origin+"/api/v1/management/terminal-access/list", body, origin, cookies, false)
		response, err := http.DefaultClient.Do(request)
		if err == nil && response.StatusCode == http.StatusOK {
			value := &cloudpb.ListDaemonTerminalAccessResponse{}
			payload := readResponse(t, response)
			response.Body.Close()
			if protojson.Unmarshal(payload, value) == nil && len(value.GetAccesses()) == 1 && value.GetAccesses()[0].GetOpaqueAccessReference() == opaqueReference {
				return
			}
		} else if response != nil {
			response.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal access projection did not become visible: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitCommandApplied(t *testing.T, origin string, cookies map[string]*http.Cookie, commandID string) {
	t.Helper()
	body, _ := protojson.Marshal(&cloudpb.GetManagementCommandRequest{CommandId: commandID})
	deadline := time.Now().Add(3 * time.Second)
	for {
		request := authenticatedCommandRequest(http.MethodPost, origin+"/api/v1/management/commands/get", body, origin, cookies, false)
		response, err := http.DefaultClient.Do(request)
		if err == nil && response.StatusCode == http.StatusOK {
			value := &cloudpb.GetManagementCommandResponse{}
			payload := readResponse(t, response)
			response.Body.Close()
			if protojson.Unmarshal(payload, value) == nil && value.GetCommand().GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
				return
			}
		} else if response != nil {
			response.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("command %s did not become APPLIED: %v", commandID, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func authenticatedCommandRequest(method, endpoint string, body []byte, origin string, cookies map[string]*http.Cookie, csrf bool) *http.Request {
	request, _ := http.NewRequest(method, endpoint, bytes.NewReader(body))
	request.Header.Set("Origin", origin)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if csrf && cookies["termx_cloud_csrf"] != nil {
		request.Header.Set("X-TermX-CSRF", cookies["termx_cloud_csrf"].Value)
	}
	return request
}

func readResponse(t *testing.T, response *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
