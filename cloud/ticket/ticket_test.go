package ticket_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/ticket"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
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
