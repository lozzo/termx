package rendezvous

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestCreateChannelReturnsPublicSTUNOnlyAndStrongSecret(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(Config{
		Now:             func() time.Time { return now },
		MaxPayloadBytes: 64 * 1024,
		PublicSTUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun.cloudflare.com:3478",
		},
	})

	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}
	if !strings.HasPrefix(channel.ChannelID, "rv_") {
		t.Fatalf("expected rv_ channel id, got %q", channel.ChannelID)
	}
	secret, err := base64.RawURLEncoding.DecodeString(channel.ChannelSecret)
	if err != nil {
		t.Fatalf("channel secret is not base64url: %v", err)
	}
	if len(secret) < 24 {
		t.Fatalf("expected at least 192-bit channel secret, got %d bytes", len(secret))
	}
	if channel.ExpiresAt.Sub(now) != 10*time.Minute {
		t.Fatalf("unexpected expiry %s", channel.ExpiresAt)
	}
	if len(channel.PublicSTUNServers) != 2 {
		t.Fatalf("expected public STUN servers, got %#v", channel.PublicSTUNServers)
	}
	for _, server := range channel.PublicSTUNServers {
		if strings.HasPrefix(server, "turn:") || strings.HasPrefix(server, "turns:") {
			t.Fatalf("anonymous rendezvous must not return TURN server %q", server)
		}
	}
}

func TestChannelSecretRequiredForMessages(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedNow, MaxPayloadBytes: 64})
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}

	if err := store.PostMessage(channel.ChannelID, "wrong-secret", Message{
		Type:    MessageOffer,
		From:    "appdev_test",
		Payload: []byte(`{"sdp":"offer"}`),
	}); err == nil {
		t.Fatal("expected wrong channel secret to be rejected")
	}
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:    MessageOffer,
		From:    "appdev_test",
		Payload: []byte(`{"sdp":"offer"}`),
	}); err != nil {
		t.Fatalf("PostMessage returned error: %v", err)
	}
	events, err := store.Events(channel.ChannelID, channel.ChannelSecret)
	if err != nil {
		t.Fatalf("Events returned error: %v", err)
	}
	if len(events) != 1 || events[0].Type != MessageOffer {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestUnsupportedMessageTypeRejected(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedNow, MaxPayloadBytes: 64})
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}

	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:    MessageType("terminal_data"),
		From:    "appdev_test",
		Payload: []byte("not signaling"),
	}); err == nil {
		t.Fatal("expected unsupported message type to be rejected")
	}
}

func TestChannelTTLAndPayloadLimit(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(Config{
		Now:             func() time.Time { return now },
		MaxPayloadBytes: 8,
	})
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  60,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}

	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:    MessageOffer,
		From:    "appdev_test",
		Payload: []byte("123456789"),
	}); err == nil {
		t.Fatal("expected oversized payload to be rejected")
	}

	now = now.Add(61 * time.Second)
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:    MessageOffer,
		From:    "appdev_test",
		Payload: []byte("ok"),
	}); err == nil {
		t.Fatal("expected expired channel to reject messages")
	}
	if _, err := store.Events(channel.ChannelID, channel.ChannelSecret); err == nil {
		t.Fatal("expected expired channel to reject events")
	}
}

func TestCreateChannelRejectsExcessiveTTL(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedNow, MaxPayloadBytes: 64})
	if _, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  3600,
	}); err == nil {
		t.Fatal("expected excessive anonymous channel TTL to be rejected")
	}
}

func TestSignalingPayloadMustBeStructured(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedNow, MaxPayloadBytes: 64})
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:    MessageOffer,
		From:    "appdev_test",
		Payload: []byte("terminal bytes hidden in an offer"),
	}); err == nil {
		t.Fatal("expected non-JSON signaling payload to be rejected")
	}
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:    MessageOffer,
		From:    "appdev_test",
		Payload: []byte(`{"terminal_data":"hidden"}`),
	}); err == nil {
		t.Fatal("expected offer without sdp to be rejected")
	}
}

func TestChannelBindsToFirstAppPublicKey(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedNow, MaxPayloadBytes: 128})
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}

	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:         MessageOffer,
		From:         "appdev_test",
		AppPublicKey: "app-public-1",
		Payload:      []byte(`{"sdp":"offer"}`),
	}); err != nil {
		t.Fatalf("PostMessage returned error: %v", err)
	}
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:         MessageCandidate,
		From:         "appdev_test",
		AppPublicKey: "app-public-2",
		Payload:      []byte(`{"candidate":"candidate"}`),
	}); err == nil {
		t.Fatal("expected different app public key to be rejected after channel claim")
	}
}

func TestPerChannelMessageLimit(t *testing.T) {
	store := NewMemoryStore(Config{
		Now:                   fixedNow,
		MaxPayloadBytes:       128,
		MaxMessagesPerChannel: 1,
	})
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel returned error: %v", err)
	}
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:         MessageOffer,
		From:         "appdev_test",
		AppPublicKey: "app-public-1",
		Payload:      []byte(`{"sdp":"offer"}`),
	}); err != nil {
		t.Fatalf("PostMessage returned error: %v", err)
	}
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:         MessageCandidate,
		From:         "appdev_test",
		AppPublicKey: "app-public-1",
		Payload:      []byte(`{"candidate":"candidate"}`),
	}); err == nil {
		t.Fatal("expected per-channel message limit to be enforced")
	}
}

func TestInvalidTURNConfigRejectedForAnonymousStore(t *testing.T) {
	store := NewMemoryStore(Config{
		Now:             fixedNow,
		MaxPayloadBytes: 64 * 1024,
		PublicSTUNServers: []string{
			"stun:stun.l.google.com:19302",
			"turn:relay.termx.example:3478",
		},
	})
	if _, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	}); err == nil {
		t.Fatal("expected anonymous rendezvous config containing TURN to be rejected")
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
}
