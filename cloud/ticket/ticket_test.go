package ticket_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDaemonBindingBindsEdgeAndRejectsTamper(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.DaemonBindingClaims{BindingId: "binding", DaemonId: "daemon", AccountId: "account", EdgeId: "edge-a", DeviceId: "device", DevicePublicKey: make([]byte, ed25519.PublicKeySize), Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute)), RelayDelegation: &cloudv1.DaemonRelayDelegation{MaxBytesPerLease: 1024, MaxRateBytesPerSecond: 512, MaxConcurrentAllocations: 2}, Revision: 1, EdgeLocatorSha256: bytes.Repeat([]byte{0x41}, sha256.Size)}
	envelope, err := ticket.SignDaemonBinding("key", privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	keys := ticket.KeySet{"key": publicKey}
	verified, err := ticket.VerifyDaemonBinding(envelope, keys, "edge-a", now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if verified.GetRelayDelegation().GetMaxBytesPerLease() != 1024 {
		t.Fatalf("daemon binding Relay delegation = %v", verified.GetRelayDelegation())
	}
	if _, err := ticket.VerifyDaemonBinding(envelope, keys, "edge-b", now, 30*time.Second); err == nil {
		t.Fatal("binding accepted on another Edge")
	}
	withoutLocator := proto.Clone(claims).(*cloudv1.DaemonBindingClaims)
	withoutLocator.EdgeLocatorSha256 = nil
	if _, err := ticket.SignDaemonBinding("key", privateKey, withoutLocator); err == nil {
		t.Fatal("binding without an Edge locator digest was signed")
	}
	envelope.Payload[0] ^= 0xff
	if _, err := ticket.VerifyDaemonBinding(envelope, keys, "edge-a", now, 30*time.Second); err == nil {
		t.Fatal("tampered binding accepted")
	}
}

func TestCloudRouteGrantBindsDaemonClientAndProduct(t *testing.T) {
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonIdentity, err := remoteauth.NewIdentity("device-r5-ticket", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	grant, err := ticket.SignCloudRouteGrant(daemonIdentity, &cloudv1.CloudRouteGrantClaims{
		GrantId: "grant-r5", DaemonId: "daemon-r5", ClientPublicKey: clientPublicKey, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(90 * 24 * time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ticket.VerifyCloudRouteGrant(grant, daemonIdentity.PublicKey, "daemon-r5", now)
	if err != nil || claims.GetProduct() != cloudv1.ClientProduct_CLIENT_PRODUCT_CLI {
		t.Fatalf("verified CloudRouteGrant=%v err=%v", claims, err)
	}
	if _, err := ticket.VerifyCloudRouteGrant(grant, daemonIdentity.PublicKey, "daemon-other", now); err == nil {
		t.Fatal("CloudRouteGrant accepted another daemon")
	}
	canonical, err := ticket.ClientRouteProofBytes("challenge-r5", bytes.Repeat([]byte{0x52}, 32), grant, "request-r5")
	if err != nil {
		t.Fatal(err)
	}
	proof := ed25519.Sign(clientPrivateKey, canonical)
	if err := ticket.VerifyClientRouteProof(clientPublicKey, proof, canonical); err != nil {
		t.Fatal(err)
	}
	if err := ticket.VerifyClientRouteProof(daemonIdentity.PublicKey, proof, canonical); err == nil {
		t.Fatal("Cloud route proof accepted another client key")
	}
	helloCanonical, err := ticket.CloudRouteHelloProofBytes(grant, "edge-r5", "session-r5", 7)
	if err != nil {
		t.Fatal(err)
	}
	helloProof := ed25519.Sign(clientPrivateKey, helloCanonical)
	if err := ticket.VerifyCloudRouteHelloProof(clientPublicKey, helloProof, grant, "edge-r5", "session-r5", 7); err != nil {
		t.Fatal(err)
	}
	if err := ticket.VerifyCloudRouteHelloProof(clientPublicKey, helloProof, grant, "edge-other", "session-r5", 7); err == nil {
		t.Fatal("Cloud Route hello proof accepted another Edge")
	}
	tampered := proto.Clone(grant).(*cloudv1.SignedEnvelope)
	tampered.KeyId = "another-device-fingerprint"
	if _, err := ticket.VerifyCloudRouteGrant(tampered, daemonIdentity.PublicKey, "daemon-r5", now); err == nil {
		t.Fatal("CloudRouteGrant accepted a mismatched DeviceIdentity key ID")
	}
}

func TestPairingRouteGrantBindsClaimAndLocator(t *testing.T) {
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-pairing-route", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimDigest := bytes.Repeat([]byte{0x71}, 32)
	locatorDigest := bytes.Repeat([]byte{0x72}, 32)
	grant, err := ticket.SignPairingRouteGrant(identity, &cloudv1.PairingRouteGrantClaims{
		GrantId: "pairing-route", DaemonId: "daemon-pairing", DeviceId: identity.DeviceID, PairingClaimSha256: claimDigest,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(10 * time.Minute)), EdgeLocatorSha256: locatorDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ticket.VerifyPairingRouteGrant(grant, identity.PublicKey, "daemon-pairing", now)
	if err != nil || !bytes.Equal(claims.GetPairingClaimSha256(), claimDigest) || !bytes.Equal(claims.GetEdgeLocatorSha256(), locatorDigest) {
		t.Fatalf("verified PairingRouteGrant=%v err=%v", claims, err)
	}
	tampered := proto.Clone(grant).(*cloudv1.SignedEnvelope)
	tampered.Payload[0] ^= 0xff
	if _, err := ticket.VerifyPairingRouteGrant(tampered, identity.PublicKey, "daemon-pairing", now); err == nil {
		t.Fatal("tampered PairingRouteGrant was accepted")
	}
}
