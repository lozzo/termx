package ticket_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAgentTicketBindsEdgeAndRejectsTamper(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.AgentTicketClaims{TicketId: "ticket", DaemonId: "daemon", AccountId: "account", EdgeId: "edge-a", DeviceId: "device", DevicePublicKey: make([]byte, ed25519.PublicKeySize), Capabilities: []cloudv1.AgentCapability{cloudv1.AgentCapability_AGENT_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute)), RelayDelegation: &cloudv1.AgentRelayDelegation{MaxBytesPerLease: 1024, MaxRateBytesPerSecond: 512, MaxConcurrentAllocations: 2}}
	envelope, err := ticket.SignAgentTicket("key", privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	keys := ticket.KeySet{"key": publicKey}
	verified, err := ticket.VerifyAgentTicket(envelope, keys, "edge-a", now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if verified.GetRelayDelegation().GetMaxBytesPerLease() != 1024 {
		t.Fatalf("AgentTicket Relay delegation = %v", verified.GetRelayDelegation())
	}
	if _, err := ticket.VerifyAgentTicket(envelope, keys, "edge-b", now, 30*time.Second); err == nil {
		t.Fatal("ticket accepted on another Edge")
	}
	envelope.Payload[0] ^= 0xff
	if _, err := ticket.VerifyAgentTicket(envelope, keys, "edge-a", now, 30*time.Second); err == nil {
		t.Fatal("tampered ticket accepted")
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

func TestPairingRouteGrantCarriesOnlyClaimDigestAndBindsClientProof(t *testing.T) {
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-pairing-route", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimDigest := bytes.Repeat([]byte{0x71}, 32)
	grant, err := ticket.SignPairingRouteGrant(identity, &cloudv1.PairingRouteGrantClaims{
		GrantId: "pairing-route", DaemonId: "daemon-pairing", DeviceId: identity.DeviceID, PairingClaimSha256: claimDigest,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(10 * time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ticket.VerifyPairingRouteGrant(grant, identity.PublicKey, "daemon-pairing", now)
	if err != nil || !bytes.Equal(claims.GetPairingClaimSha256(), claimDigest) {
		t.Fatalf("verified PairingRouteGrant=%v err=%v", claims, err)
	}
	canonical, err := ticket.PairingRouteProofBytes("pairing-challenge", bytes.Repeat([]byte{0x72}, 32), grant, "pairing-request")
	if err != nil {
		t.Fatal(err)
	}
	proof := ed25519.Sign(clientPrivateKey, canonical)
	if err := ticket.VerifyClientRouteProof(clientPublicKey, proof, canonical); err != nil {
		t.Fatal(err)
	}
	tampered := proto.Clone(grant).(*cloudv1.SignedEnvelope)
	tampered.Payload[0] ^= 0xff
	if _, err := ticket.VerifyPairingRouteGrant(tampered, identity.PublicKey, "daemon-pairing", now); err == nil {
		t.Fatal("tampered PairingRouteGrant was accepted")
	}
}

func TestRelayLeaseBindsSessionLimitsAndExpiry(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.RelayLeaseClaims{
		LeaseId: "lease-r6", AccountId: "account-r6", EdgeId: "edge-r6", DaemonId: "daemon-r6", ClientId: "client-r6", SessionId: "session-r6",
		MaxBytes: 1 << 20, MaxRateBytesPerSecond: 64 << 10, MaxConcurrentAllocations: 1,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(5 * time.Minute)),
	}
	envelope, err := ticket.SignRelayLease("controller-r6", privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	keys := ticket.KeySet{"controller-r6": publicKey}
	verified, err := ticket.VerifyRelayLease(envelope, keys, "edge-r6", "session-r6", now, 30*time.Second)
	if err != nil || verified.GetMaxBytes() != 1<<20 {
		t.Fatalf("verified RelayLease=%v err=%v", verified, err)
	}
	if _, err := ticket.VerifyRelayLease(envelope, keys, "edge-r6", "session-other", now, 30*time.Second); err == nil {
		t.Fatal("RelayLease accepted another session")
	}
	if _, err := ticket.VerifyRelayLease(envelope, keys, "edge-r6", "session-r6", now.Add(6*time.Minute), 30*time.Second); err == nil {
		t.Fatal("expired RelayLease was accepted")
	}
	tooLong := proto.Clone(claims).(*cloudv1.RelayLeaseClaims)
	tooLong.ExpiresAt = timestamppb.New(now.Add(5*time.Minute + time.Nanosecond))
	if _, err := ticket.SignRelayLease("controller-r6", privateKey, tooLong); err == nil {
		t.Fatal("RelayLease accepted a lifetime longer than five minutes")
	}
}
