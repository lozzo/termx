package enrollment

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"github.com/muxvia/muxvia/shared/remoteauth"
)

func TestCompleteDaemonEnrollmentReturnsReactivatedIdentity(t *testing.T) {
	now := time.Date(2026, 7, 27, 14, 0, 0, 0, time.UTC)
	identity, err := remoteauth.NewIdentity("device-reactivated", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	challenge := bytes.Repeat([]byte{0x63}, remoteauth.DeviceIdentityChallengeBytes)
	tokenDigest := bytes.Repeat([]byte{0x74}, 32)
	store := &reactivationStoreFake{result: Daemon{
		ID: "daemon-existing", AccountID: "account-existing", DisplayName: "Restored daemon", DeviceID: identity.DeviceID,
		DevicePublicKey: identity.PublicKey, DeviceFingerprint: identity.Fingerprint, Revoked: false, Revision: 3,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}}
	service := &Service{
		config: Config{Store: store, Now: func() time.Time { return now }},
		challenges: map[string]challengeState{"challenge-reactivate": {
			kind: challengeEnrollment, value: challenge, expires: now.Add(time.Minute), tokenDigest: tokenDigest,
			deviceID: identity.DeviceID, fingerprint: identity.Fingerprint, publicKey: identity.PublicKey,
		}},
	}
	proof, err := remoteauth.SignDeviceIdentityProof(identity, challenge)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.CompleteDaemonEnrollment(context.Background(), &cloudv1.CompleteDaemonEnrollmentRequest{ChallengeId: "challenge-reactivate", DeviceProof: proof})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(store.digest, tokenDigest) || store.deviceID != identity.DeviceID || !bytes.Equal(store.publicKey, identity.PublicKey) {
		t.Fatalf("store enrollment input digest=%x device=%q key=%x", store.digest, store.deviceID, store.publicKey)
	}
	daemon := response.GetDaemon()
	if daemon.GetDaemonId() != "daemon-existing" || daemon.GetRevoked() || daemon.GetRevision() != 3 || daemon.GetDisplayName() != "Restored daemon" {
		t.Fatalf("reactivated daemon response=%+v", daemon)
	}
}

type reactivationStoreFake struct {
	result                Daemon
	digest, publicKey     []byte
	deviceID, fingerprint string
}

func (*reactivationStoreFake) CreateDaemonEnrollment(context.Context, string, string, string, []byte, time.Time, time.Time) (string, error) {
	return "", nil
}

func (store *reactivationStoreFake) ConsumeDaemonEnrollment(_ context.Context, digest []byte, deviceID, fingerprint string, publicKey ed25519.PublicKey, _ time.Time) (Daemon, error) {
	store.digest = append([]byte(nil), digest...)
	store.deviceID = deviceID
	store.fingerprint = fingerprint
	store.publicKey = append([]byte(nil), publicKey...)
	return store.result, nil
}

func (*reactivationStoreFake) GetDaemon(context.Context, string) (Daemon, error) {
	return Daemon{}, nil
}

func (*reactivationStoreFake) ListDaemons(context.Context) ([]Daemon, error) {
	return nil, nil
}
