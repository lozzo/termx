package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/private/cloud/controller"
	cloudedge "github.com/muxvia/muxvia/private/cloud/edge"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestOPSHUB001AddsEdgeWithoutControllerRestart(t *testing.T) {
	now := time.Now().UTC()
	sourceHubPublic, sourceHubPrivate, _ := ed25519.GenerateKey(rand.Reader)
	sourceRelayPublic, sourceRelayPrivate, _ := ed25519.GenerateKey(rand.Reader)
	standbyHubPublic, standbyHubPrivate, _ := ed25519.GenerateKey(rand.Reader)
	standbyRelayPublic, standbyRelayPrivate, _ := ed25519.GenerateKey(rand.Reader)
	targetHubPublic, targetHubPrivate, _ := ed25519.GenerateKey(rand.Reader)
	targetRelayPublic, targetRelayPrivate, _ := ed25519.GenerateKey(rand.Reader)
	projectionPublic, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	_, daemonControlPrivate, _ := ed25519.GenerateKey(rand.Reader)
	operatorToken := make([]byte, 32)
	if _, err := rand.Read(operatorToken); err != nil {
		t.Fatal(err)
	}
	databaseKey := filepath.Join(t.TempDir(), "opshub001-postgres")
	catalogPath := filepath.Join(findRepoRoot(t), "private/cloud/web-controller/config/plans.json")
	account := seedCommandAccount(t, databaseKey, catalogPath, now)
	store, err := postgrestest.Open(t, databaseKey)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := hubregistry.New(store)
	if err != nil {
		t.Fatal(err)
	}
	source := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-source", Region: "local-1", PublicLabel: "Source Edge", HubId: "hub-source", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(sourceHubPublic), RelayId: "relay-source", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(sourceRelayPublic)}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: source, ControlPublicKey: sourceHubPublic, RelayControlPublicKey: sourceRelayPublic, PublicHubURL: "http://127.0.0.1:43001", HealthURL: "http://127.0.0.1:43001/healthz", MaxAssignments: 100, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	standby := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-standby", Region: "local-2", PublicLabel: "Standby Edge", HubId: "hub-standby", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(standbyHubPublic), RelayId: "relay-standby", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(standbyRelayPublic)}
	if err := registry.RegisterDeployment(context.Background(), hubregistry.Deployment{Metadata: standby, ControlPublicKey: standbyHubPublic, RelayControlPublicKey: standbyRelayPublic, PublicHubURL: "http://127.0.0.1:43002", HealthURL: "http://127.0.0.1:43002/healthz", MaxAssignments: 100, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	runtime, err := controller.Start(controller.Config{
		PostgresDSN: postgrestest.DSN(t, databaseKey), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: catalogPath,
		ProjectionKeyID: "projection-opshub001", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), DaemonControlKeyID: "daemon-control-opshub001", DaemonControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(daemonControlPrivate),
		OperatorID: "operator-opshub001", OperatorRole: "admin", OperatorAccessTokenBase64: base64.RawStdEncoding.EncodeToString(operatorToken),
		Devices:     []*cloudpb.CloudDevicePolicy{{AccountId: account.GetAccountId(), DeviceId: "daemon-opshub001", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision()}},
		Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-opshub001", AccountId: account.GetAccountId(), HubId: source.GetHubId(), AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	sourceEdge, err := cloudedge.Start(cloudedge.Config{ControllerURL: runtime.Manifest().InternalControlURL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: filepath.Join(t.TempDir(), "source-usage.outbox"), Metadata: source, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(sourceHubPrivate), RelayControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(sourceRelayPrivate), ControllerProjectionKeyID: "projection-opshub001", ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPublic)})
	if err != nil {
		t.Fatal(err)
	}
	defer sourceEdge.Close(context.Background())
	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	if err := sourceEdge.WaitReady(readyContext); err != nil {
		cancelReady()
		t.Fatal(err)
	}
	cancelReady()
	standbyEdge, err := cloudedge.Start(cloudedge.Config{ControllerURL: runtime.Manifest().InternalControlURL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: filepath.Join(t.TempDir(), "standby-usage.outbox"), Metadata: standby, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(standbyHubPrivate), RelayControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(standbyRelayPrivate), ControllerProjectionKeyID: "projection-opshub001", ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPublic)})
	if err != nil {
		t.Fatal(err)
	}
	defer standbyEdge.Close(context.Background())
	standbyReadyContext, cancelStandbyReady := context.WithTimeout(context.Background(), 5*time.Second)
	if err := standbyEdge.WaitReady(standbyReadyContext); err != nil {
		cancelStandbyReady()
		t.Fatal(err)
	}
	cancelStandbyReady()

	jar, _ := cookiejar.New(nil)
	operatorClient := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	operatorLoginHTTP(t, operatorClient, runtime.Manifest().OperatorURL, base64.RawURLEncoding.EncodeToString(operatorToken))
	target := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-dynamic", Region: "local-3", PublicLabel: "Dynamic Edge", HubId: "hub-dynamic", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(targetHubPublic), RelayId: "relay-dynamic", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(targetRelayPublic)}
	created := &cloudpb.CreateHubDeploymentResponse{}
	operatorPost(t, operatorClient, runtime.Manifest().OperatorURL, "/api/v1/operator/fleet/create", &cloudpb.CreateHubDeploymentRequest{HubId: target.GetHubId(), EdgeDeploymentId: target.GetEdgeDeploymentId(), RelayId: target.GetRelayId(), Region: target.GetRegion(), PublicLabel: target.GetPublicLabel(), PublicHubUrl: "http://127.0.0.1:43003", HealthUrl: "http://127.0.0.1:43003/healthz", MaxAssignments: 10, HubControlPublicKey: targetHubPublic, RelayControlPublicKey: targetRelayPublic, Reason: "add a third nearer region", RequestId: "opshub001-create"}, created)
	if created.GetDeployment().GetEnabled() || created.GetDeployment().GetIdentityApproved() {
		t.Fatalf("new deployment bypassed identity approval: %v", created.GetDeployment())
	}
	targetEdge, err := cloudedge.Start(cloudedge.Config{ControllerURL: runtime.Manifest().InternalControlURL, HubListen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", RelayListen: "127.0.0.1:0", UsageOutboxPath: filepath.Join(t.TempDir(), "target-usage.outbox"), Metadata: target, HubControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(targetHubPrivate), RelayControlPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(targetRelayPrivate), ControllerProjectionKeyID: "projection-opshub001", ControllerProjectionPublicKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPublic)})
	if err != nil {
		t.Fatal(err)
	}
	defer targetEdge.Close(context.Background())
	pendingContext, cancelPending := context.WithTimeout(context.Background(), 150*time.Millisecond)
	if err := targetEdge.WaitReady(pendingContext); err == nil {
		cancelPending()
		t.Fatal("pending Edge became ready before fingerprint approval")
	}
	cancelPending()
	approved := &cloudpb.ApproveHubDeploymentIdentityResponse{}
	operatorPost(t, operatorClient, runtime.Manifest().OperatorURL, "/api/v1/operator/fleet/approve", &cloudpb.ApproveHubDeploymentIdentityRequest{HubId: target.GetHubId(), ExpectedRevision: 1, HubControlIdentityFingerprint: target.GetHubControlIdentityFingerprint(), RelayControlIdentityFingerprint: target.GetRelayControlIdentityFingerprint(), Reason: "fingerprints reviewed", RequestId: "opshub001-approve"}, approved)
	if !approved.GetDeployment().GetEnabled() || approved.GetDeployment().GetDirectoryRevision() != 2 {
		t.Fatalf("approved deployment = %v", approved.GetDeployment())
	}
	targetReadyContext, cancelTargetReady := context.WithTimeout(context.Background(), 5*time.Second)
	if err := targetEdge.WaitReady(targetReadyContext); err != nil {
		cancelTargetReady()
		t.Fatal(err)
	}
	cancelTargetReady()
	assignment, err := registry.Assignment(context.Background(), "daemon-opshub001")
	if err != nil || assignment.Value.GetHubId() != source.GetHubId() || assignment.Value.GetAssignmentEpoch() != 1 {
		t.Fatalf("existing assignment drifted when dynamic Edge attached = (%v, %v)", assignment, err)
	}
}
