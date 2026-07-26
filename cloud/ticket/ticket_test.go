package ticket_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/ticket"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAgentTicketBindsEdgeAndRejectsTamper(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.AgentTicketClaims{TicketId: "ticket", DaemonId: "daemon", AccountId: "account", EdgeId: "edge-a", DeviceId: "device", DevicePublicKey: make([]byte, ed25519.PublicKeySize), Capabilities: []cloudv1.AgentCapability{cloudv1.AgentCapability_AGENT_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute))}
	envelope, err := ticket.SignAgentTicket("key", privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	keys := ticket.KeySet{"key": publicKey}
	if _, err := ticket.VerifyAgentTicket(envelope, keys, "edge-a", now, 30*time.Second); err != nil {
		t.Fatal(err)
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
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(7 * 24 * time.Hour)),
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
	tampered := proto.Clone(grant).(*cloudv1.SignedEnvelope)
	tampered.KeyId = "another-device-fingerprint"
	if _, err := ticket.VerifyCloudRouteGrant(tampered, daemonIdentity.PublicKey, "daemon-r5", now); err == nil {
		t.Fatal("CloudRouteGrant accepted a mismatched DeviceIdentity key ID")
	}
}

func TestClientTicketBindsP2PEdgeAndHelloGeneration(t *testing.T) {
	controllerPublicKey, controllerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.ClientTicketClaims{
		TicketId: "client-ticket-r5", AccountId: "account-r5", EdgeId: "edge-r5", DaemonId: "daemon-r5", ClientId: remoteauth.Fingerprint(clientPublicKey),
		ClientPublicKey: clientPublicKey, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, RoutePolicy: cloudv1.CloudRoutePolicy_CLOUD_ROUTE_POLICY_P2P_ONLY,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(2 * time.Minute)),
	}
	envelope, err := ticket.SignClientTicket("controller-r5", controllerPrivateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	keys := ticket.KeySet{"controller-r5": controllerPublicKey}
	if _, err := ticket.VerifyClientTicket(envelope, keys, "edge-r5", now, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := ticket.VerifyClientTicket(envelope, keys, "edge-other", now, 30*time.Second); err == nil {
		t.Fatal("ClientTicket accepted another Edge")
	}
	canonical, err := ticket.ClientHelloProofBytes(envelope, "session-r5", 7)
	if err != nil {
		t.Fatal(err)
	}
	proof := ed25519.Sign(clientPrivateKey, canonical)
	if err := ticket.VerifyClientHelloProof(clientPublicKey, proof, envelope, "session-r5", 7); err != nil {
		t.Fatal(err)
	}
	if err := ticket.VerifyClientHelloProof(clientPublicKey, proof, envelope, "session-r5", 8); err == nil {
		t.Fatal("ClientHello proof accepted another attempt generation")
	}
	tooLong := proto.Clone(claims).(*cloudv1.ClientTicketClaims)
	tooLong.ExpiresAt = timestamppb.New(now.Add(2*time.Minute + time.Nanosecond))
	if _, err := ticket.SignClientTicket("controller-r5", controllerPrivateKey, tooLong); err == nil {
		t.Fatal("ClientTicket accepted a lifetime longer than two minutes")
	}
}
