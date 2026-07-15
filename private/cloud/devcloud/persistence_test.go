package devcloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/proto/cloudpb"
)

func TestPersistentSecurityStateAllowsHubPresenceAfterSupervisorRestart(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)}
	directory := t.TempDir()
	config := Config{
		Now: clock.Now, EnrollmentCode: "persistent-enrollment",
		SecurityDirectoryPath: filepath.Join(directory, "security-directory.json"),
		AuthorityKeyPath:      filepath.Join(directory, "authority.json"),
		EdgeSnapshotPath:      filepath.Join(directory, "hub-policy.snapshot"),
		RefreshSessionPath:    filepath.Join(directory, "refresh-sessions.json"),
	}
	first, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	firstAdapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: first.Manifest().ControlPlaneURL, HubURL: first.Manifest().HubURL, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := firstAdapter.BeginDeviceEnrollment(ctx, &cloudpb.BeginDeviceEnrollmentRequest{
		OneTimeCode: "persistent-enrollment", DevicePublicKey: publicKey,
		Metadata: &cloudpb.DeviceMetadata{DisplayName: "Persistent daemon", Platform: "test", TermxVersion: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "daemon-persistent"
	deviceSession, err := firstAdapter.CompleteDeviceEnrollment(ctx, &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: challenge.GetFlowId(), Proof: signEnrollmentProof(t, privateKey, publicKey, deviceID, challenge, clock.Now())})
	if err != nil {
		t.Fatal(err)
	}
	authorization := deviceSession.Authorization()
	defer authorization.Destroy()
	refreshAuthorization, err := deviceSession.RefreshAuthorization(clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer refreshAuthorization.Destroy()
	rawRefresh := refreshAuthorization.Bytes()
	defer clear(rawRefresh)
	persistedRefresh, err := os.ReadFile(config.RefreshSessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persistedRefresh, rawRefresh) || bytes.Contains(persistedRefresh, []byte(base64.StdEncoding.EncodeToString(rawRefresh))) {
		t.Fatal("refresh session store persisted the bearer secret instead of its hash")
	}
	deviceSession.Destroy()
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}

	second, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(ctx)
	secondAdapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: second.Manifest().ControlPlaneURL, HubURL: second.Manifest().HubURL, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	presenceChallenge, err := secondAdapter.BeginPresence(ctx, authorization, &cloudpb.BeginPresenceRequest{DeviceId: deviceID})
	if err != nil {
		t.Fatalf("Hub-local presence after restart = %v", err)
	}
	presence, err := secondAdapter.OpenPresence(ctx, authorization, signPresenceRequest(t, privateKey, publicKey, deviceID, presenceChallenge, clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	ready, err := presence.Receive(ctx)
	if err != nil || ready.GetReady().GetPresenceSessionId() != presenceChallenge.GetPresenceSessionId() {
		t.Fatalf("restored Hub presence ready = (%v, %v)", ready, err)
	}
	refreshed, err := secondAdapter.RefreshSession(ctx, refreshAuthorization)
	if err != nil {
		t.Fatalf("persistent refresh after restart = %v", err)
	}
	refreshed.Destroy()
	if _, err := secondAdapter.RefreshSession(ctx, refreshAuthorization); err == nil {
		t.Fatal("replayed refresh credential was accepted")
	}
}
