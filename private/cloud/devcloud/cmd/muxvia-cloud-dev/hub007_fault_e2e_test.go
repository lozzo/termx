package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice"
	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice/httpapi"
	"github.com/muxvia/muxvia/private/cloud/companion/session"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/controller"
	"github.com/muxvia/muxvia/private/cloud/edge"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestHUB007EdgeRestartAssignmentMigrationAndControllerOutage(t *testing.T) {
	root := findRepoRoot(t)
	artifactDir := t.TempDir()
	manifestPath := filepath.Join(artifactDir, "runtime.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--manifest", manifestPath, "--repo-root", root, "--fault-harness"}) }()
	var manifest supervisorManifest
	if err := waitManifest(ctx, manifestPath, &manifest, done); err != nil {
		cancel()
		t.Fatal(err)
	}
	postgresDSN := controllerPostgresDSN(t, manifest)
	var restarted []*childProcess
	defer func() {
		stopChildren(restarted)
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("fault supervisor did not stop")
		}
	}()
	credentials := readDevelopmentCredentials(t, manifest.CredentialsPath)
	operatorClient := operatorHTTPClient(t)
	operatorLoginHTTP(t, operatorClient, manifest.Controller.PublicURL, manifest.Controller.OperatorURL, credentials.AccountEmail, credentials.AccountPassword)
	accounts := &cloudpb.ListOperatorAccountsResponse{}
	operatorPost(t, operatorClient, manifest.Controller.OperatorURL, "/api/v1/operator/accounts/list", &cloudpb.ListOperatorAccountsRequest{Page: &cloudpb.PageRequest{PageSize: 10}}, accounts)
	if len(accounts.GetAccounts()) != 1 {
		t.Fatalf("development accounts = %v", accounts)
	}
	accountID := accounts.GetAccounts()[0].GetAccount().GetAccountId()
	authEpoch := accounts.GetAccounts()[0].GetAccount().GetAuthRevision()
	clients := []*developmentClient{
		newDevelopmentClient(t, manifest, accountID, authEpoch, "client-dev-local", "hub-edge-b"),
		newDevelopmentClient(t, manifest, accountID, authEpoch, "client-dev-secondary", "hub-edge-b"),
	}
	oldPresenceB := openDevelopmentPresence(t, manifest, accountID, authEpoch, "daemon-edge-b", "hub-edge-b", 1)
	oldPresenceB.reportInventory(t, "runtime-b-before", 1)
	for _, client := range clients {
		client.resolve(t, "daemon-edge-b")
	}

	edgeRecord := processByName(t, manifest.Processes, "hub-edge-b")
	oldEdge := edgeByHub(t, manifest.Edges, "hub-edge-b")
	terminatePID(t, edgeRecord.PID)
	if !waitUnavailable(oldEdge.HealthURL+"/healthz", 5*time.Second) {
		t.Fatal("Edge B health listener remained available after process exit")
	}
	closedEdgeContext, cancelEdgeClosed := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelEdgeClosed()
	if event, receiveErr := oldPresenceB.presence.Receive(closedEdgeContext); receiveErr == nil {
		t.Fatalf("old Edge B Presence survived process restart: %v", event)
	}
	oldPresenceB.Close()
	_ = os.Remove(edgeRecord.ManifestPath)
	restartedEdge, err := startChild(edgeRecord.BinaryPath, edgeRecord.ConfigPath, edgeRecord.ManifestPath, filepath.Join(artifactDir, "hub-edge-b-restart.log"), "hub-edge-b-restart")
	if err != nil {
		t.Fatal(err)
	}
	restarted = append(restarted, restartedEdge)
	var edgeRuntime edge.Manifest
	if err := waitManifest(ctx, edgeRecord.ManifestPath, &edgeRuntime, restartedEdge.done); err != nil {
		t.Fatal(err)
	}
	if err := waitHealth(ctx, edgeRuntime.HealthURL+"/healthz", true); err != nil {
		t.Fatal(err)
	}
	if edgeRuntime.PID == oldEdge.PID || edgeRuntime.ControlGeneration != oldEdge.ControlGeneration+1 || edgeRuntime.ProjectionRevision != oldEdge.ProjectionRevision {
		t.Fatalf("Edge restart did not recover a new generation/full projection: old=%+v new=%+v", oldEdge, edgeRuntime)
	}
	presenceB := openDevelopmentPresence(t, manifest, accountID, authEpoch, "daemon-edge-b", "hub-edge-b", 1)
	defer presenceB.Close()
	presenceB.reportInventory(t, "runtime-b-after", 1)
	for _, client := range clients {
		client.resolve(t, "daemon-edge-b")
	}
	managedTarget := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-edge-b", ManagedSessionId: "managed-before-control-outage", SessionIncarnation: 1, AssignmentEpoch: 1, ControlPresenceSessionId: presenceB.presenceID, DaemonRuntimeGeneration: "runtime-b-after"}
	managedSession := &cloudpb.ManagedPeerSessionProjection{Target: managedTarget, ClientDeviceId: "client-dev-local", EstablishedPresenceSessionId: presenceB.presenceID, ControlOwnerHubId: "hub-edge-b", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
	presenceB.reportSessions(t, "runtime-b-after", 2, []*cloudpb.ManagedPeerSessionProjection{managedSession})
	waitOperatorManagedSession(t, operatorClient, manifest.Controller.OperatorURL, accountID, managedTarget.GetManagedSessionId(), true)
	proxyValue, ok := hub007FaultProxies.Load("hub-edge-b")
	if !ok {
		t.Fatal("Edge B control fault proxy is unavailable")
	}
	controlProxy := proxyValue.(*controlFaultProxy)
	controlProxy.SetBlocked(true)
	if err := presenceB.reportSessionsError("runtime-b-after", 3, nil); err != nil {
		t.Fatalf("runtime replacement during control outage = %v", err)
	}
	for _, client := range clients {
		client.resolve(t, "daemon-edge-b")
	}
	if err := presenceB.reportSessionsError("runtime-b-after", 2, nil); err == nil {
		t.Fatal("stale runtime inventory revision was accepted")
	}
	controlProxy.SetBlocked(false)
	waitControlGenerations(t, postgresDSN, map[string]uint64{"hub-edge-b": 3})
	waitOperatorManagedSession(t, operatorClient, manifest.Controller.OperatorURL, accountID, managedTarget.GetManagedSessionId(), false)
	presenceA := openDevelopmentPresence(t, manifest, accountID, authEpoch, "daemon-edge-a", "hub-edge-a", 1)
	defer presenceA.Close()
	presenceA.reportInventory(t, "runtime-a-before", 1)
	created := &cloudpb.CreateManagementCommandResponse{}
	operatorPost(t, operatorClient, manifest.Controller.OperatorURL, "/api/v1/operator/commands", &cloudpb.CreateManagementCommandRequest{AccountId: accountID, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT, IdempotencyKey: "hub007-migrate-daemon-a", Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_AssignmentMigration{AssignmentMigration: &cloudpb.AssignmentMigrationTarget{DaemonDeviceId: "daemon-edge-a", TargetHubId: "hub-edge-b"}}}}, created)
	if created.GetCommand().GetCommandId() == "" {
		t.Fatalf("migration response = %v", created)
	}
	waitMigrationApplied(t, postgresDSN, accountID, created.GetCommand().GetCommandId())
	closedContext, cancelClosed := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelClosed()
	if event, receiveErr := presenceA.presence.Receive(closedContext); receiveErr == nil {
		t.Fatalf("source Presence remained open after migration: %v", event)
	}
	migratedPresence := openDevelopmentPresence(t, manifest, accountID, authEpoch, "daemon-edge-a", "hub-edge-b", 2)
	defer migratedPresence.Close()
	migratedPresence.reportInventory(t, "runtime-a-after", 1)
	for _, client := range clients {
		client.resolve(t, "daemon-edge-a")
		client.resolve(t, "daemon-edge-b")
	}

	controllerRecord := processByName(t, manifest.Processes, "controller")
	terminatePID(t, controllerRecord.PID)
	if !waitUnavailable(manifest.Controller.OperatorURL+"/healthz", 5*time.Second) {
		t.Fatal("Controller listener remained available after process exit")
	}
	for _, runningEdge := range []edge.Manifest{edgeByHub(t, manifest.Edges, "hub-edge-a"), edgeRuntime} {
		response, requestErr := (&http.Client{Timeout: time.Second}).Get(runningEdge.HealthURL + "/healthz")
		if requestErr != nil {
			t.Fatalf("Edge process stopped with Controller outage: %s: %v", runningEdge.HubID, requestErr)
		}
		response.Body.Close()
	}
	for _, client := range clients {
		client.resolve(t, "daemon-edge-a")
		client.resolve(t, "daemon-edge-b")
	}
	_ = os.Remove(controllerRecord.ManifestPath)
	restartedController, err := startChild(controllerRecord.BinaryPath, controllerRecord.ConfigPath, controllerRecord.ManifestPath, filepath.Join(artifactDir, "controller-restart.log"), "controller-restart")
	if err != nil {
		t.Fatal(err)
	}
	restarted = append(restarted, restartedController)
	var controllerRuntime controller.Manifest
	if err := waitManifest(ctx, controllerRecord.ManifestPath, &controllerRuntime, restartedController.done); err != nil {
		t.Fatal(err)
	}
	if err := waitHealth(ctx, controllerRuntime.OperatorURL+"/healthz", true); err != nil {
		t.Fatal(err)
	}
	waitControlGenerations(t, postgresDSN, map[string]uint64{"hub-edge-a": 2, "hub-edge-b": 4})
	store, err := cloudpostgres.Open(context.Background(), postgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assignment, err := store.Assignment(context.Background(), "daemon-edge-a")
	if err != nil || assignment.Value.GetHubId() != "hub-edge-b" || assignment.Value.GetAssignmentEpoch() != 2 {
		t.Fatalf("persisted migration after Controller restart = (%+v, %v)", assignment, err)
	}
	restartedOperatorClient := operatorHTTPClient(t)
	operatorLoginHTTP(t, restartedOperatorClient, controllerRuntime.PublicURL, controllerRuntime.OperatorURL, credentials.AccountEmail, credentials.AccountPassword)
	agent := waitOperatorAgent(t, restartedOperatorClient, controllerRuntime.OperatorURL, accountID, "daemon-edge-a", "hub-edge-b", 2)
	kick := &cloudpb.CreateManagementCommandResponse{}
	operatorPost(t, restartedOperatorClient, controllerRuntime.OperatorURL, "/api/v1/operator/commands", &cloudpb.CreateManagementCommandRequest{AccountId: accountID, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_KICK_PRESENCE, IdempotencyKey: "opsuser001-kick-migrated-presence", Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_Presence{Presence: &cloudpb.KickPresenceTarget{DaemonDeviceId: agent.GetPresence().GetDaemonDeviceId(), AssignmentEpoch: agent.GetPresence().GetAssignmentEpoch(), PresenceSessionId: agent.GetPresence().GetPresenceSessionId()}}}}, kick)
	closedAfterKick, cancelAfterKick := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelAfterKick()
	if event, receiveErr := migratedPresence.presence.Receive(closedAfterKick); receiveErr == nil {
		t.Fatalf("Agent Presence remained open after operator Kick: %v", event)
	}
	waitOperatorCommandApplied(t, postgresDSN, accountID, kick.GetCommand().GetCommandId())
	scanHUB007RuntimeLogs(t, manifest, credentials, []string{restartedEdge.record.LogPath, restartedController.record.LogPath})
}

func processByName(t *testing.T, records []processRecord, name string) processRecord {
	t.Helper()
	for _, record := range records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("process %q not found", name)
	return processRecord{}
}

func edgeByHub(t *testing.T, edges []edge.Manifest, hubID string) edge.Manifest {
	t.Helper()
	for _, value := range edges {
		if value.HubID == hubID {
			return value
		}
	}
	t.Fatalf("Edge %q not found", hubID)
	return edge.Manifest{}
}

func terminatePID(t *testing.T, pid int) {
	t.Helper()
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.SIGTERM) != nil {
		t.Fatalf("terminate pid %d: %v", pid, err)
	}
}

func readDevelopmentCredentials(t *testing.T, path string) developmentCredentials {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result developmentCredentials
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func operatorHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 5 * time.Second}
}

type developmentDaemon struct {
	deviceID        string
	hubID           string
	assignmentEpoch uint64
	presenceID      string
	adapter         *httpapi.Adapter
	authorization   session.Authorization
	presence        cloudservice.PresenceSource
}

func openDevelopmentPresence(t *testing.T, manifest supervisorManifest, accountID string, authEpoch uint64, deviceID, hubID string, assignmentEpoch uint64) *developmentDaemon {
	t.Helper()
	now := time.Now().UTC()
	issuer := developmentEdgeIssuer(t, manifest, now)
	token, err := issuer.IssueEdgeAccessForPrincipal("hub007-daemon-token-"+deviceID, hubID, accountID, deviceID, servicecredential.EdgePrincipalDaemon, authEpoch, 30*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	edgeRuntime := edgeByHub(t, manifest.Edges, hubID)
	storedSession, err := session.New(session.Metadata{Kind: session.KindDevice, AccountID: accountID, DeviceID: deviceID, ExpiresAt: now.Add(30 * time.Minute), HubID: hubID, HubURL: edgeRuntime.HubURL, HubRegion: "development", HubDirectoryVersion: 1}, token, now)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: edgeRuntime.HubURL})
	if err != nil {
		t.Fatal(err)
	}
	var challenge *cloudpb.PresenceChallenge
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		challenge, err = adapter.BeginPresence(context.Background(), storedSession.Authorization(), &cloudpb.BeginPresenceRequest{DeviceId: deviceID})
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("BeginPresence(%s on %s): %v", deviceID, hubID, err)
	}
	privateKey := developmentDevicePrivateKey(deviceID)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	signedAt := time.Now().UTC()
	proofBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{PresenceSessionId: challenge.GetPresenceSessionId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(), DeviceId: deviceID, DevicePublicKey: publicKey, SignedAtUnixNano: signedAt.UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	presence, err := adapter.OpenPresence(context.Background(), storedSession.Authorization(), &cloudpb.OpenPresenceRequest{PresenceSessionId: challenge.GetPresenceSessionId(), Proof: &cloudpb.DeviceProof{DeviceId: deviceID, DevicePublicKey: publicKey, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(privateKey, proofBytes), SignedAtUnixNano: signedAt.UnixNano()}, Metadata: &cloudpb.DeviceMetadata{DisplayName: "HUB007 daemon", Platform: "test", MuxviaVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	readyContext, cancelReady := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelReady()
	if event, err := presence.Receive(readyContext); err != nil || event.GetReady() == nil {
		presence.Close()
		t.Fatalf("development Presence ready = (%v, %v)", event, err)
	}
	return &developmentDaemon{deviceID: deviceID, hubID: hubID, assignmentEpoch: assignmentEpoch, presenceID: challenge.GetPresenceSessionId(), adapter: adapter, authorization: storedSession.Authorization(), presence: presence}
}

func (daemon *developmentDaemon) Close() error { return daemon.presence.Close() }

func (daemon *developmentDaemon) reportInventory(t *testing.T, runtimeGeneration string, revision uint64) {
	t.Helper()
	if err := daemon.reportSessionsError(runtimeGeneration, revision, nil); err != nil {
		t.Fatalf("ReportDaemonRuntime(%s) = %v", daemon.deviceID, err)
	}
}

func (daemon *developmentDaemon) reportSessions(t *testing.T, runtimeGeneration string, revision uint64, sessions []*cloudpb.ManagedPeerSessionProjection) {
	t.Helper()
	if err := daemon.reportSessionsError(runtimeGeneration, revision, sessions); err != nil {
		t.Fatalf("ReportDaemonRuntime(%s) = %v", daemon.deviceID, err)
	}
}

func (daemon *developmentDaemon) reportSessionsError(runtimeGeneration string, revision uint64, sessions []*cloudpb.ManagedPeerSessionProjection) error {
	now := time.Now().UTC()
	reportID := runtimeGeneration + ":" + fmt.Sprint(revision)
	request := &cloudpb.ReportDaemonRuntimeRequest{ReportId: reportID, HubId: daemon.hubID, AssignmentEpoch: daemon.assignmentEpoch, PresenceSessionId: daemon.presenceID, DaemonRuntimeGeneration: runtimeGeneration, RegistryRevision: revision, PeerSessions: &cloudpb.PeerSessionInventorySnapshot{ReportId: reportID, DaemonDeviceId: daemon.deviceID, ControlOwnerHubId: daemon.hubID, AssignmentEpoch: daemon.assignmentEpoch, ControlPresenceSessionId: daemon.presenceID, DaemonRuntimeGeneration: runtimeGeneration, RegistryRevision: revision, Sessions: sessions, ObservedAtUnixMillis: now.UnixMilli()}}
	_, err := daemon.adapter.ReportDaemonRuntime(context.Background(), daemon.authorization, request)
	return err
}

type developmentClient struct {
	deviceID      string
	adapter       *httpapi.Adapter
	authorization session.Authorization
}

func newDevelopmentClient(t *testing.T, manifest supervisorManifest, accountID string, authEpoch uint64, clientID, hubID string) *developmentClient {
	t.Helper()
	now := time.Now().UTC()
	issuer := developmentEdgeIssuer(t, manifest, now)
	token, err := issuer.IssueEdgeAccessForPrincipal("hub007-client-token-"+clientID, hubID, accountID, clientID, servicecredential.EdgePrincipalClient, authEpoch, 30*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	edgeRuntime := edgeByHub(t, manifest.Edges, hubID)
	storedSession, err := session.New(session.Metadata{Kind: session.KindAccount, AccountID: accountID, DeviceID: clientID, ExpiresAt: now.Add(30 * time.Minute), HubID: hubID, HubURL: edgeRuntime.HubURL, HubRegion: "development", HubDirectoryVersion: 1}, token, now)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: edgeRuntime.HubURL})
	if err != nil {
		t.Fatal(err)
	}
	return &developmentClient{deviceID: clientID, adapter: adapter, authorization: storedSession.Authorization()}
}

func (client *developmentClient) resolve(t *testing.T, targetDeviceID string) {
	t.Helper()
	resolved, err := client.adapter.ResolveEndpoint(context.Background(), client.authorization, &cloudpb.ResolveEndpointRequest{EndpointId: "hub007-" + client.deviceID + "-" + targetDeviceID, TargetDeviceId: targetDeviceID})
	if err != nil || resolved.GetManagedSessionId() == "" {
		t.Fatalf("ResolveEndpoint(%s -> %s) = (%v, %v)", client.deviceID, targetDeviceID, resolved, err)
	}
}

func developmentEdgeIssuer(t *testing.T, manifest supervisorManifest, now time.Time) servicecredential.EdgeAccessIssuer {
	t.Helper()
	controllerConfigBody, err := os.ReadFile(processByName(t, manifest.Processes, "controller").ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var config controller.Config
	if err := json.Unmarshal(controllerConfigBody, &config); err != nil {
		t.Fatal(err)
	}
	projectionPrivate, err := base64.RawStdEncoding.DecodeString(config.ProjectionPrivateKeyBase64)
	if err != nil || len(projectionPrivate) != ed25519.PrivateKeySize {
		t.Fatal("development projection private key is invalid")
	}
	signer, err := servicecredential.NewSigner(config.ProjectionKeyID, ed25519.PrivateKey(projectionPrivate), now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgeAccessIssuer("muxvia-cloud-controller", signer)
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

func operatorLoginHTTP(t *testing.T, client *http.Client, publicOrigin, operatorOrigin, email, password string) {
	t.Helper()
	body, err := protojson.Marshal(&cloudpb.PasswordLoginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, publicOrigin+"/api/v1/account/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", publicOrigin)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(response.Body)
		t.Fatalf("account login = %d: %s", response.StatusCode, detail)
	}
	operatorPost(t, client, operatorOrigin, "/api/v1/operator/reauth", &cloudpb.RecentAuthenticationRequest{Password: password}, &cloudpb.RecentAuthenticationResponse{})
}

func operatorPost(t *testing.T, client *http.Client, origin, path string, requestBody, responseBody proto.Message) {
	t.Helper()
	body, err := protojson.Marshal(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, origin+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	for _, cookie := range client.Jar.Cookies(request.URL) {
		if cookie.Name == "muxvia_cloud_csrf" {
			request.Header.Set("X-Muxvia-CSRF", cookie.Value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s = %d: %s", path, response.StatusCode, detail)
	}
	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(responseBytes, responseBody); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func waitMigrationApplied(t *testing.T, postgresDSN, accountID, commandID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastCommand *cloudpb.ManagementCommandProjection
	var lastAssignment string
	var lastError error
	for time.Now().Before(deadline) {
		store, err := cloudpostgres.Open(context.Background(), postgresDSN)
		if err == nil {
			service, _ := commandoutbox.New(store)
			command, commandErr := service.Get(context.Background(), accountID, commandID)
			assignment, assignmentErr := store.Assignment(context.Background(), "daemon-edge-a")
			lastCommand = command
			lastAssignment = fmt.Sprintf("%+v", assignment)
			if commandErr != nil {
				lastError = commandErr
			} else {
				lastError = assignmentErr
			}
			_ = store.Close()
			state := command.GetExecutionState()
			if commandErr == nil && assignmentErr == nil && (state == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED || state == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED) && assignment.Value.GetHubId() == "hub-edge-b" && assignment.Value.GetAssignmentEpoch() == 2 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("assignment migration did not apply: command=%v assignment=%s error=%v", lastCommand, lastAssignment, lastError)
}

func waitOperatorCommandApplied(t *testing.T, postgresDSN, accountID, commandID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last *cloudpb.ManagementCommandProjection
	for time.Now().Before(deadline) {
		store, err := cloudpostgres.Open(context.Background(), postgresDSN)
		if err == nil {
			service, _ := commandoutbox.New(store)
			last, err = service.Get(context.Background(), accountID, commandID)
			_ = store.Close()
			if err == nil && (last.GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED || last.GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("operator command %q did not apply: %v", commandID, last)
}

func waitOperatorAgent(t *testing.T, client *http.Client, origin, accountID, deviceID, hubID string, assignmentEpoch uint64) *cloudpb.OperatorAgentProjection {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last *cloudpb.ListOperatorAgentsResponse
	for time.Now().Before(deadline) {
		last = &cloudpb.ListOperatorAgentsResponse{}
		operatorPost(t, client, origin, "/api/v1/operator/agents/list", &cloudpb.ListOperatorAgentsRequest{Query: deviceID, Freshness: cloudpb.Freshness_FRESHNESS_FRESH, Page: &cloudpb.PageRequest{PageSize: 10}}, last)
		if len(last.GetAgents()) == 1 {
			agent := last.GetAgents()[0]
			if agent.GetAccount().GetAccountId() == accountID && agent.GetDevice().GetDeviceId() == deviceID && agent.GetPresence().GetControlOwnerHubId() == hubID && agent.GetPresence().GetAssignmentEpoch() == assignmentEpoch {
				return agent
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Agent projection did not converge after migration/restart: %v", last)
	return nil
}

func waitOperatorManagedSession(t *testing.T, client *http.Client, origin, accountID, managedSessionID string, present bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response := &cloudpb.GetOperatorAccountResponse{}
		operatorPost(t, client, origin, "/api/v1/operator/accounts/get", &cloudpb.GetOperatorAccountRequest{AccountId: accountID}, response)
		found := false
		for _, peer := range response.GetTopology().GetPeerSessions() {
			if peer.GetTarget().GetManagedSessionId() == managedSessionID {
				found = true
				break
			}
		}
		if found == present {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("operator managed session %q present=%v did not converge", managedSessionID, present)
}

func waitControlGenerations(t *testing.T, postgresDSN string, expected map[string]uint64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		store, err := cloudpostgres.Open(context.Background(), postgresDSN)
		if err == nil {
			matched := true
			for hubID, generation := range expected {
				deployment, loadErr := store.Deployment(context.Background(), hubID)
				if loadErr != nil || deployment.ControlGeneration < generation {
					matched = false
				}
			}
			_ = store.Close()
			if matched {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("control generations did not reach %v", expected))
}

func scanHUB007RuntimeLogs(t *testing.T, manifest supervisorManifest, credentials developmentCredentials, extraLogs []string) {
	t.Helper()
	forbidden := []string{credentials.AccountPassword, base64.RawStdEncoding.EncodeToString(developmentDevicePrivateKey("daemon-edge-a")), base64.RawStdEncoding.EncodeToString(developmentDevicePrivateKey("daemon-edge-b")), "offer_sdp", "ice_pwd", "terminal_payload"}
	controllerConfigBody, err := os.ReadFile(processByName(t, manifest.Processes, "controller").ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var controllerConfig controller.Config
	if err := json.Unmarshal(controllerConfigBody, &controllerConfig); err != nil {
		t.Fatal(err)
	}
	forbidden = append(forbidden, controllerConfig.ProjectionPrivateKeyBase64, controllerConfig.DaemonControlPrivateKeyBase64)
	var logs []string
	for _, process := range manifest.Processes {
		logs = append(logs, process.LogPath)
		if process.Name == "controller" {
			continue
		}
		configBody, readErr := os.ReadFile(process.ConfigPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var edgeConfig edge.Config
		if err := json.Unmarshal(configBody, &edgeConfig); err != nil {
			t.Fatal(err)
		}
		forbidden = append(forbidden, edgeConfig.HubControlPrivateKeyBase64, edgeConfig.RelayControlPrivateKeyBase64)
	}
	logs = append(logs, extraLogs...)
	for _, logPath := range logs {
		body, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range forbidden {
			if secret != "" && bytes.Contains(body, []byte(secret)) {
				t.Fatalf("runtime log %s contains forbidden secret or payload marker", logPath)
			}
		}
	}
}
