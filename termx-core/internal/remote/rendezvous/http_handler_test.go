package rendezvous

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPCreateChannelReturnsPublicSTUNOnly(t *testing.T) {
	handler := NewHTTPHandler(HTTPConfig{
		Store: NewMemoryStore(Config{
			Now:             fixedHTTPNow,
			MaxPayloadBytes: 64 * 1024,
			PublicSTUNServers: []string{
				"stun:stun.l.google.com:19302",
				"stun:stun.cloudflare.com:3478",
			},
		}),
	})
	req := jsonRequest(http.MethodPost, "/api/v1/anonymous/channels", map[string]any{
		"machine_id":                     "mach_test",
		"machine_public_key_fingerprint": "sha256:test",
		"ttl_seconds":                    600,
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		ChannelID         string   `json:"channel_id"`
		ChannelSecret     string   `json:"channel_secret"`
		ExpiresAt         string   `json:"expires_at"`
		PublicSTUNServers []string `json:"public_stun_servers"`
	}
	decodeHTTPBody(t, rec, &body)
	if !strings.HasPrefix(body.ChannelID, "rv_") || body.ChannelSecret == "" {
		t.Fatalf("expected channel id and secret, got %#v", body)
	}
	if body.ExpiresAt != "2026-05-01T10:10:00Z" {
		t.Fatalf("unexpected expires_at %q", body.ExpiresAt)
	}
	if got := strings.ToLower(rec.Body.String()); strings.Contains(got, "turn:") || strings.Contains(got, "turns:") || strings.Contains(got, "credential") {
		t.Fatalf("anonymous create response must not include TURN credentials: %s", rec.Body.String())
	}
	if len(body.PublicSTUNServers) != 2 {
		t.Fatalf("expected public STUN servers, got %#v", body.PublicSTUNServers)
	}
}

func TestHTTPEventsRequireRendezvousAuthorization(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedHTTPNow, MaxPayloadBytes: 1024})
	channel := mustCreateHTTPChannel(t, store)
	if err := store.PostMessage(channel.ChannelID, channel.ChannelSecret, Message{
		Type:         MessageOffer,
		From:         "appdev_test",
		AppPublicKey: "app-public-1",
		Payload:      []byte(`{"sdp":"offer"}`),
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	handler := NewHTTPHandler(HTTPConfig{Store: store})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anonymous/channels/"+channel.ChannelID+"/events", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without auth, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertHTTPError(t, rec, "unauthorized")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anonymous/channels/"+channel.ChannelID+"/events", nil)
	req.Header.Set("Authorization", "Rendezvous "+channel.ChannelID+":"+channel.ChannelSecret)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []struct {
			Type    string          `json:"type"`
			From    string          `json:"from"`
			Payload json.RawMessage `json:"payload"`
		} `json:"messages"`
	}
	decodeHTTPBody(t, rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].Type != "offer" || body.Messages[0].From != "appdev_test" {
		t.Fatalf("unexpected events response: %#v", body.Messages)
	}
	if string(body.Messages[0].Payload) != `{"sdp":"offer"}` {
		t.Fatalf("expected payload object, got %s", body.Messages[0].Payload)
	}
}

func TestHTTPOfferAndAnswerUseChannelSecretAndStoreLimits(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(Config{
		Now:             func() time.Time { return now },
		MaxPayloadBytes: 32,
	})
	channel := mustCreateHTTPChannel(t, store)
	handler := NewHTTPHandler(HTTPConfig{Store: store})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/offer", map[string]any{
		"channel_secret": "wrong-secret",
		"app_public_key": "app-public-1",
		"offer": map[string]any{
			"sdp": "offer",
		},
	}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected wrong secret unauthorized, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/offer", map[string]any{
		"channel_secret": channel.ChannelSecret,
		"app_public_key": "app-public-1",
		"offer": map[string]any{
			"sdp": strings.Repeat("x", 64),
		},
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized payload bad request, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertHTTPError(t, rec, "rendezvous_message_rejected")

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/offer", map[string]any{
		"channel_secret": channel.ChannelSecret,
		"app_public_key": "app-public-1",
		"offer": map[string]any{
			"sdp": "offer",
		},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected accepted offer, got %d body=%s", rec.Code, rec.Body.String())
	}

	now = now.Add(601 * time.Second)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/answer", map[string]any{
		"channel_secret": channel.ChannelSecret,
		"answer": map[string]any{
			"sdp": "answer",
		},
	}))
	if rec.Code != http.StatusGone {
		t.Fatalf("expected expired channel gone, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertHTTPError(t, rec, "rendezvous_channel_expired")
}

func TestHTTPOfferForwardsCertificateAndSignatureEnvelope(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedHTTPNow, MaxPayloadBytes: 4096})
	channel := mustCreateHTTPChannel(t, store)
	handler := NewHTTPHandler(HTTPConfig{Store: store})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/offer", map[string]any{
		"channel_secret": channel.ChannelSecret,
		"from":           "appdev_test",
		"app_public_key": "app-public-1",
		"app_certificate": map[string]any{
			"payload": map[string]any{
				"machine_id":     "mach_test",
				"app_public_key": "app-public-1",
			},
			"signature": "machine-sig",
		},
		"offer": map[string]any{
			"sdp":            "offer",
			"ice_candidates": []any{},
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     "nonce-1",
			"timestamp": 1770000000,
			"value":     "app-sig",
		},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected accepted offer, got %d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anonymous/channels/"+channel.ChannelID+"/events", nil)
	req.Header.Set("Authorization", "Rendezvous "+channel.ChannelID+":"+channel.ChannelSecret)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected events 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Messages []struct {
			Type         string          `json:"type"`
			From         string          `json:"from"`
			AppPublicKey string          `json:"app_public_key"`
			Payload      json.RawMessage `json:"payload"`
		} `json:"messages"`
	}
	decodeHTTPBody(t, rec, &body)
	if len(body.Messages) != 1 {
		t.Fatalf("expected one message, got %#v", body.Messages)
	}
	if body.Messages[0].Type != "offer" || body.Messages[0].From != "appdev_test" || body.Messages[0].AppPublicKey != "app-public-1" {
		t.Fatalf("unexpected message metadata: %#v", body.Messages[0])
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body.Messages[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, key := range []string{"app_certificate", "offer", "signature"} {
		if len(payload[key]) == 0 {
			t.Fatalf("expected payload to forward %q, got %s", key, body.Messages[0].Payload)
		}
	}
	if got := strings.ToLower(string(body.Messages[0].Payload)); strings.Contains(got, "turn:") || strings.Contains(got, "terminal_data") || strings.Contains(got, "file_data") {
		t.Fatalf("offer envelope forwarded forbidden data: %s", body.Messages[0].Payload)
	}
}

func TestHTTPAnswerForwardsCertificateAndSignatureEnvelope(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedHTTPNow, MaxPayloadBytes: 4096})
	channel := mustCreateHTTPChannel(t, store)
	handler := NewHTTPHandler(HTTPConfig{Store: store})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/answer", map[string]any{
		"channel_secret": channel.ChannelSecret,
		"from":           "mach_test",
		"app_public_key": "app-public-1",
		"app_certificate": map[string]any{
			"payload": map[string]any{
				"machine_id":     "mach_test",
				"app_public_key": "app-public-1",
			},
			"signature": "machine-sig",
		},
		"answer": map[string]any{
			"sdp":            "answer",
			"ice_candidates": []any{},
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     "nonce-1",
			"timestamp": 1770000001,
			"value":     "agent-sig",
		},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected accepted answer, got %d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anonymous/channels/"+channel.ChannelID+"/events", nil)
	req.Header.Set("Authorization", "Rendezvous "+channel.ChannelID+":"+channel.ChannelSecret)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected events 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		} `json:"messages"`
	}
	decodeHTTPBody(t, rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].Type != "answer" {
		t.Fatalf("expected answer message, got %#v", body.Messages)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body.Messages[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, key := range []string{"app_certificate", "answer", "signature"} {
		if len(payload[key]) == 0 {
			t.Fatalf("expected payload to forward %q, got %s", key, body.Messages[0].Payload)
		}
	}
}

func TestHTTPCandidateEndpointForwardsTrickleICE(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedHTTPNow, MaxPayloadBytes: 4096})
	channel := mustCreateHTTPChannel(t, store)
	handler := NewHTTPHandler(HTTPConfig{Store: store})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/candidate", map[string]any{
		"channel_secret": channel.ChannelSecret,
		"from":           "appdev_test",
		"app_public_key": "app-public-1",
		"candidate": map[string]any{
			"candidate":   "candidate:1 1 udp 1 192.0.2.1 5000 typ host",
			"mid":         "0",
			"mline_index": 0,
		},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected accepted candidate, got %d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anonymous/channels/"+channel.ChannelID+"/events", nil)
	req.Header.Set("Authorization", "Rendezvous "+channel.ChannelID+":"+channel.ChannelSecret)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected events 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		} `json:"messages"`
	}
	decodeHTTPBody(t, rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].Type != "candidate" {
		t.Fatalf("expected candidate event, got %#v", body.Messages)
	}
	if !strings.Contains(string(body.Messages[0].Payload), "candidate:1") {
		t.Fatalf("expected candidate payload, got %s", body.Messages[0].Payload)
	}
}

func TestHTTPRejectsTerminalFileAndTurnPayloads(t *testing.T) {
	store := NewMemoryStore(Config{Now: fixedHTTPNow, MaxPayloadBytes: 1024})
	channel := mustCreateHTTPChannel(t, store)
	handler := NewHTTPHandler(HTTPConfig{Store: store})

	for name, body := range map[string]map[string]any{
		"terminal data": {
			"channel_secret": channel.ChannelSecret,
			"app_public_key": "app-public-1",
			"offer": map[string]any{
				"sdp":           "offer",
				"terminal_data": "not signaling",
			},
		},
		"turn urls": {
			"channel_secret": channel.ChannelSecret,
			"app_public_key": "app-public-1",
			"offer": map[string]any{
				"sdp":         "offer",
				"ice_servers": []string{"turn:relay.termx.example:3478"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/v1/anonymous/channels/"+channel.ChannelID+"/offer", body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected bad request, got %d body=%s", rec.Code, rec.Body.String())
			}
			assertHTTPError(t, rec, "rendezvous_message_rejected")
		})
	}
}

func TestHTTPHandlerWithoutStoreReturnsEnvelope(t *testing.T) {
	handler := NewHTTPHandler(HTTPConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/anonymous/channels/rv_test/events", nil)
	req.Header.Set("Authorization", "Rendezvous rv_test:secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected service unavailable, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertHTTPError(t, rec, "rendezvous_unavailable")
}

func fixedHTTPNow() time.Time {
	return time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
}

func mustCreateHTTPChannel(t *testing.T, store *Store) Channel {
	t.Helper()
	channel, err := store.CreateChannel(CreateChannelRequest{
		MachineID:                   "mach_test",
		MachinePublicKeyFingerprint: "sha256:test",
		TTLSeconds:                  600,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return channel
}

func jsonRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeHTTPBody(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
}

func assertHTTPError(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	decodeHTTPBody(t, rec, &body)
	if body.Error.Code != code || body.Error.Message == "" || body.Error.RequestID == "" {
		t.Fatalf("expected error code %q with message/request_id, got %#v", code, body.Error)
	}
}
