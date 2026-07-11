package presence_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/directory"
	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/presence"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

func TestServiceIssuesOneTimeDeviceScopedPresenceAdmission(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	store := directory.NewStore()
	mustSeedAccount(t, store, now)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(domain.DeviceRegistration{
		ID: "daemon-1", AccountID: "account-1", OwnerUserID: "user-1", Kind: domain.DeviceKindDaemon,
		PublicKey: publicKey, Fingerprint: "daemon-fingerprint", RegisteredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	signerKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	signer, err := servicecredential.NewSigner("cp-dev", signerKey, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewHubAdmissionIssuer("control-plane.dev", signer)
	if err != nil {
		t.Fatal(err)
	}
	service, err := presence.NewService(presence.Config{
		Devices: store, Issuer: issuer, HubID: "hub-dev", ChallengeTTL: time.Minute, AdmissionTTL: time.Minute,
		MaxChallenges: 1, Now: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{9}, 256)),
	})
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := service.Begin(context.Background(), "account-1", "daemon-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Begin(context.Background(), "account-1", "daemon-1"); !errors.Is(err, presence.ErrCapacity) {
		t.Fatalf("presence challenge capacity error = %v", err)
	}
	signedAt := now.Add(time.Second)
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{
		PresenceSessionId: challenge.PresenceSessionID, ChallengeId: challenge.ChallengeID,
		Challenge: challenge.Value, DeviceId: "daemon-1", DevicePublicKey: publicKey, SignedAtUnixNano: signedAt.UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	proof := presence.Proof{
		PresenceSessionID: challenge.PresenceSessionID, ChallengeID: challenge.ChallengeID,
		DeviceID: "daemon-1", PublicKey: publicKey, Signature: ed25519.Sign(privateKey, signingBytes), SignedAt: signedAt,
	}
	admission, err := service.Issue(context.Background(), "account-1", proof)
	if err != nil {
		t.Fatal(err)
	}
	if admission.PresenceSessionID == "" || admission.PresenceSessionID == "managed-1" {
		t.Fatalf("presence admission = %#v", admission)
	}
	ring, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := servicecredential.VerifyHubAdmission(ring, admission.Ticket.Bytes(), servicecredential.HubAdmissionExpectation{
		Issuer: "control-plane.dev", AudienceHubID: "hub-dev", PrincipalKind: servicecredential.PrincipalDaemon,
		AccountID: "account-1", DeviceID: "daemon-1", SessionKind: servicecredential.HubSessionPresence,
		SessionID: challenge.PresenceSessionID, Operation: servicecredential.HubOperationPresence,
	}, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Issue(context.Background(), "account-1", proof); !errors.Is(err, presence.ErrChallengeNotFound) {
		t.Fatalf("replayed proof error = %v", err)
	}
}

func mustSeedAccount(t *testing.T, store *directory.Store, now time.Time) {
	t.Helper()
	if err := store.PutAccount(domain.Account{ID: "account-1", DisplayName: "Dev", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutUser(domain.User{ID: "user-1", AccountID: "account-1", Email: "dev@example.test", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
}
