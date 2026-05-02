package rendezvous_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/rendezvous"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestCreateChannelIsAuthenticatedSTUNOnlyAndNoTURN(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRendezvousDB(t, ctx, "termx-rendezvous-create-test")
	ownerID, machineID := createOwnedMachine(t, ctx, db)
	svc := rendezvous.NewService(rendezvous.Config{
		DB:          db,
		Clock:       fixedClock(time.Date(2026, 5, 3, 6, 50, 0, 0, time.UTC)),
		STUNServers: []string{"stun:stun.termx.test:3478"},
	})

	channel, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_1",
		TTL:        10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if channel.Path != "public_p2p" {
		t.Fatalf("path = %q", channel.Path)
	}
	if channel.ID == "" || channel.Secret == "" {
		t.Fatalf("channel missing id/secret: %+v", channel)
	}
	if len(channel.ICEServers) != 1 || channel.ICEServers[0].URL != "stun:stun.termx.test:3478" {
		t.Fatalf("ice servers = %+v", channel.ICEServers)
	}
	if containsTURN(t, channel) {
		t.Fatalf("public_p2p channel contains TURN credentials: %+v", channel)
	}
	if _, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     "usr_other",
		MachineID:  machineID,
		TerminalID: "term_1",
		TTL:        time.Minute,
	}); err == nil {
		t.Fatal("other user created channel for owned machine")
	}
}

func TestCreateChannelFiltersICEConfigAndCapsTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRendezvousDB(t, ctx, "termx-rendezvous-ice-policy-test")
	ownerID, machineID := createOwnedMachine(t, ctx, db)
	clock := fixedClock(time.Date(2026, 5, 3, 6, 53, 0, 0, time.UTC))
	svc := rendezvous.NewService(rendezvous.Config{
		DB:    db,
		Clock: clock,
		STUNServers: []string{
			"stun:stun.termx.test:3478",
			"stuns:stun-secure.termx.test:5349",
			"https://not-ice.example.test",
			"turn:turn.termx.test:3478",
		},
	})

	channel, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_1",
		TTL:        2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if got, want := channel.ExpiresAt.Sub(clock.Now()), 15*time.Minute; got != want {
		t.Fatalf("ttl = %s, want capped %s", got, want)
	}
	if len(channel.ICEServers) != 2 {
		t.Fatalf("ice servers = %+v", channel.ICEServers)
	}
	for _, server := range channel.ICEServers {
		lower := strings.ToLower(server.URL)
		if !strings.HasPrefix(lower, "stun:") && !strings.HasPrefix(lower, "stuns:") {
			t.Fatalf("non-STUN ICE server returned: %+v", channel.ICEServers)
		}
	}
	if containsTURN(t, channel) {
		t.Fatalf("public_p2p channel contains TURN credentials: %+v", channel)
	}

	emptySTUN := rendezvous.NewService(rendezvous.Config{
		DB:          db,
		Clock:       clock,
		STUNServers: []string{"https://not-ice.example.test", "turn:turn.termx.test:3478"},
	})
	if _, err := emptySTUN.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_no_stun",
		TTL:        time.Minute,
	}); err == nil {
		t.Fatal("channel created without any usable STUN server")
	}
}

func TestMessageForwardingTTLSecretPayloadAndRateLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRendezvousDB(t, ctx, "termx-rendezvous-message-test")
	ownerID, machineID := createOwnedMachine(t, ctx, db)
	clock := &mutableClock{value: time.Date(2026, 5, 3, 6, 51, 0, 0, time.UTC)}
	svc := rendezvous.NewService(rendezvous.Config{
		DB:                    db,
		Clock:                 clock,
		STUNServers:           []string{"stun:stun.termx.test:3478"},
		MaxPayloadBytes:       128,
		MaxMessagesPerChannel: 2,
	})
	channel, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_1",
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	offer := map[string]any{"sdp": "offer-sdp", "terminal_data": "must-not-forward"}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    channel.Secret,
		Type:      rendezvous.MessageOffer,
		Payload:   mustJSON(t, offer),
	}); err == nil {
		t.Fatal("terminal_data payload was accepted")
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    "wrong-secret",
		Type:      rendezvous.MessageOffer,
		Payload:   mustJSON(t, map[string]any{"sdp": "offer-sdp"}),
	}); err == nil {
		t.Fatal("message with wrong secret was accepted")
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    channel.Secret,
		Type:      rendezvous.MessageOffer,
		Payload:   strings.Repeat("x", 129),
	}); err == nil {
		t.Fatal("oversized payload was accepted")
	}
	candidateChannel, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_candidates",
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("create candidate channel: %v", err)
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: candidateChannel.ID,
		Secret:    candidateChannel.Secret,
		Type:      rendezvous.MessageCandidate,
		Payload: mustJSON(t, map[string]any{
			"candidate":        "candidate:1 1 udp 1 192.0.2.1 1 typ host",
			"usernameFragment": "uf",
		}),
	}); err != nil {
		t.Fatalf("host ICE candidate was rejected: %v", err)
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: candidateChannel.ID,
		Secret:    candidateChannel.Secret,
		Type:      rendezvous.MessageCandidate,
		Payload: mustJSON(t, map[string]any{
			"candidate":        "candidate:1 1 udp 1 203.0.113.1 1 typ relay",
			"usernameFragment": "uf",
		}),
	}); err == nil {
		t.Fatal("relay ICE candidate was accepted")
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    channel.Secret,
		Type:      rendezvous.MessageOffer,
		Payload:   mustJSON(t, map[string]any{"sdp": "offer-sdp"}),
	}); err != nil {
		t.Fatalf("send offer: %v", err)
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    channel.Secret,
		Type:      rendezvous.MessageAnswer,
		Payload:   mustJSON(t, map[string]any{"sdp": "answer-sdp"}),
	}); err != nil {
		t.Fatalf("send answer: %v", err)
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    channel.Secret,
		Type:      rendezvous.MessageCandidate,
		Payload:   mustJSON(t, map[string]any{"candidate": "candidate"}),
	}); err == nil {
		t.Fatal("rate limit allowed third message")
	}
	messages, err := svc.ListMessages(ctx, rendezvous.ListMessagesInput{ChannelID: channel.ID, Secret: channel.Secret})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 || messages[0].Type != rendezvous.MessageOffer || messages[1].Type != rendezvous.MessageAnswer {
		t.Fatalf("messages = %+v", messages)
	}
	if containsTURN(t, messages) {
		t.Fatalf("rendezvous messages contain TURN credentials: %+v", messages)
	}

	expired, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_2",
		TTL:        time.Second,
	})
	if err != nil {
		t.Fatalf("create expiring channel: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Second)
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: expired.ID,
		Secret:    expired.Secret,
		Type:      rendezvous.MessageOffer,
		Payload:   mustJSON(t, map[string]any{"sdp": "late"}),
	}); err == nil {
		t.Fatal("expired channel accepted message")
	}
}

func TestStructuredPayloadValidationRejectsNonSignalingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRendezvousDB(t, ctx, "termx-rendezvous-payload-policy-test")
	ownerID, machineID := createOwnedMachine(t, ctx, db)
	svc := rendezvous.NewService(rendezvous.Config{
		DB:                    db,
		Clock:                 fixedClock(time.Date(2026, 5, 3, 6, 55, 0, 0, time.UTC)),
		STUNServers:           []string{"stun:stun.termx.test:3478"},
		MaxMessagesPerChannel: 10,
	})
	channel, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_1",
		TTL:        time.Minute,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	for name, payload := range map[string]string{
		"offer missing sdp":         mustJSON(t, map[string]any{"type": "offer"}),
		"offer object sdp":          mustJSON(t, map[string]any{"sdp": map[string]any{"blob": "not-signaling"}}),
		"offer sdp relay candidate": mustJSON(t, map[string]any{"sdp": "v=0\r\na=candidate:1 1 udp 1 203.0.113.1 1 typ    relay\r\n"}),
		"offer sdp tabbed relay":    mustJSON(t, map[string]any{"sdp": "v=0\r\na=candidate:1 1 udp 1 203.0.113.1 1 typ\trelay\r\n"}),
		"offer with runtime data":   mustJSON(t, map[string]any{"sdp": "offer-sdp", "terminal_data": "no"}),
		"offer private key":         mustJSON(t, map[string]any{"sdp": "offer-sdp", "machine_private_key": "secret"}),
		"offer private cert field": mustJSON(t, map[string]any{
			"app_certificate": map[string]any{
				"payload":   map[string]any{"kty": "OKP", "d": "private-scalar"},
				"signature": "base64-signature",
			},
			"offer": map[string]any{"sdp": "offer-sdp"},
		}),
		"candidate object value":     mustJSON(t, map[string]any{"candidate": map[string]any{"blob": "not-signaling"}}),
		"candidate escaped relay":    `{"candidate":"candidate:1 1 udp 1 203.0.113.1 1 typ\u0020relay","usernameFragment":"uf"}`,
		"candidate tabbed relay":     mustJSON(t, map[string]any{"candidate": "candidate:1 1 udp 1 203.0.113.1 1 typ\trelay"}),
		"candidate spaced relay":     mustJSON(t, map[string]any{"candidate": "candidate:1 1 udp 1 203.0.113.1 1 typ    relay"}),
		"candidate turn url":         mustJSON(t, map[string]any{"candidate": "candidate:1 1 udp 1 turn:turn.termx.test 1 typ host"}),
		"candidate numeric mline":    mustJSON(t, map[string]any{"candidate": "candidate:1 1 udp 1 192.0.2.1 1 typ host", "sdpMLineIndex": "0"}),
		"candidate numeric mline v2": mustJSON(t, map[string]any{"candidate": "candidate:1 1 udp 1 192.0.2.1 1 typ host", "mline_index": "0"}),
	} {
		t.Run(name, func(t *testing.T) {
			messageType := rendezvous.MessageOffer
			if strings.Contains(name, "candidate") {
				messageType = rendezvous.MessageCandidate
			}
			if err := svc.Send(ctx, rendezvous.SendMessageInput{
				ChannelID: channel.ID,
				Secret:    channel.Secret,
				Type:      messageType,
				Payload:   payload,
			}); err == nil {
				t.Fatal("non-signaling payload accepted")
			}
		})
	}
}

func TestCleanupExpiredChannelsRemovesMessages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openRendezvousDB(t, ctx, "termx-rendezvous-cleanup-test")
	ownerID, machineID := createOwnedMachine(t, ctx, db)
	clock := &mutableClock{value: time.Date(2026, 5, 3, 6, 54, 0, 0, time.UTC)}
	svc := rendezvous.NewService(rendezvous.Config{DB: db, Clock: clock, STUNServers: []string{"stun:stun.termx.test:3478"}})
	channel, err := svc.CreateChannel(ctx, rendezvous.CreateChannelInput{
		UserID:     ownerID,
		MachineID:  machineID,
		TerminalID: "term_1",
		TTL:        time.Second,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := svc.Send(ctx, rendezvous.SendMessageInput{
		ChannelID: channel.ID,
		Secret:    channel.Secret,
		Type:      rendezvous.MessageOffer,
		Payload:   mustJSON(t, map[string]any{"sdp": "offer-sdp"}),
	}); err != nil {
		t.Fatalf("send offer: %v", err)
	}

	clock.value = clock.value.Add(2 * time.Second)
	removed, err := svc.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("cleanup expired: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	var messages int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM rendezvous_messages WHERE channel_id = ?`, channel.ID).Scan(&messages); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messages != 0 {
		t.Fatalf("messages after cleanup = %d", messages)
	}
}

func openRendezvousDB(t *testing.T, ctx context.Context, name string) *sql.DB {
	t.Helper()
	db, err := store.OpenSQLite(ctx, "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func createOwnedMachine(t *testing.T, ctx context.Context, db *sql.DB) (string, string) {
	t.Helper()
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 6, 49, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-4-rendezvous-secret")),
	})
	auth, err := accounts.Register(ctx, account.RegisterInput{Email: "rv-owner@example.com", Password: "valid password"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: fixedClock(time.Date(2026, 5, 3, 6, 49, 0, 0, time.UTC))})
	boot, err := machineSvc.Bootstrap(ctx, machines.BootstrapInput{MachinePublicKey: "machine-public-key", DisplayName: "Rendezvous Machine"})
	if err != nil {
		t.Fatalf("bootstrap machine: %v", err)
	}
	if _, err := machineSvc.Claim(ctx, machines.ClaimInput{UserID: auth.User.ID, MachineID: boot.Machine.ID, ClaimToken: boot.ClaimToken}); err != nil {
		t.Fatalf("claim machine: %v", err)
	}
	return auth.User.ID, boot.Machine.ID
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(data)
}

func containsTURN(t *testing.T, value any) bool {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	text := strings.ToLower(string(data))
	return strings.Contains(text, "turn:") || strings.Contains(text, "turns:") || strings.Contains(text, "username")
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}
