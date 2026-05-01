package localweb

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	"github.com/lozzow/termx/termx-core/internal/remote/pairing"
)

type statusSourceStub struct {
	status Status
}

func (s statusSourceStub) LocalStatus(context.Context) (Status, error) {
	return s.status, nil
}

type terminalSourceStub struct {
	terminals []Terminal
}

func (s terminalSourceStub) ListTerminals(context.Context) ([]Terminal, error) {
	return append([]Terminal(nil), s.terminals...), nil
}

type pairClaimerStub struct {
	got pairing.ClaimRequest
	out pairing.ClaimResponse
}

func (s *pairClaimerStub) ClaimPairSession(_ context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error) {
	s.got = req
	return s.out, nil
}

type rtcAnswererStub struct {
	got RTCOfferRequest
	out RTCOfferResponse
}

func (s *rtcAnswererStub) AnswerLocalRTCOffer(_ context.Context, req RTCOfferRequest) (RTCOfferResponse, error) {
	s.got = req
	return s.out, nil
}

func TestHandlerServesEmbeddedAssetsWithSPAFallback(t *testing.T) {
	handler := NewHandler(Config{
		Assets: NewStaticAssets(map[string]string{
			"index.html":             "<!doctype html><title>TermX Remote</title><div id=\"root\"></div>",
			"assets/app.js":          "console.log('termx')",
			"assets/app.css":         "body{margin:0}",
			"assets/not-index.html":  "<p>asset</p>",
			"assets/nested/file.txt": "asset text",
		}),
	})

	for _, tc := range []struct {
		path        string
		contentType string
		want        string
	}{
		{path: "/", contentType: "text/html", want: "TermX Remote"},
		{path: "/machines/local", contentType: "text/html", want: "TermX Remote"},
		{path: "/assets/app.js", contentType: "text/javascript", want: "console.log"},
		{path: "/assets/app.css", contentType: "text/css", want: "body"},
		{path: "/assets/nested/file.txt", contentType: "text/plain", want: "asset text"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("expected content type containing %q, got %q", tc.contentType, rec.Header().Get("Content-Type"))
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("expected body to contain %q, got %q", tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandlerServesDefaultEmbeddedAssets(t *testing.T) {
	handler := NewHandler(Config{Assets: EmbeddedAssets()})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "TermX Remote") {
		t.Fatalf("expected default embedded page, got %q", rec.Body.String())
	}
}

func TestHandlerErrorResponseUsesDocumentedEnvelope(t *testing.T) {
	handler := NewHandler(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/local/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%q", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "not_found" || body.Error.Message == "" || body.Error.RequestID == "" {
		t.Fatalf("expected documented error envelope, got %#v", body.Error)
	}
}

func TestHandlerLocalStatusUsesMachineFieldsAndNoTurnCredentials(t *testing.T) {
	handler := NewHandler(Config{
		Status: statusSourceStub{status: Status{
			MachineID:                   "mach_local",
			MachineName:                 "Local Mac",
			MachinePublicKeyFingerprint: "sha256:machine",
			RemoteEnabled:               true,
			LocalRTC: LocalRTCStatus{
				HTTPURL:       "http://127.0.0.1:7342",
				ICETCPEnabled: true,
				ICETCPPort:    7342,
			},
			UpdatedAt: time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC),
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/local/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["machine_id"] != "mach_local" {
		t.Fatalf("expected machine_id, got %#v", body)
	}
	if _, ok := body["device_id"]; ok {
		t.Fatalf("local status must not expose device_id: %#v", body)
	}
	raw := rec.Body.String()
	if strings.Contains(strings.ToLower(raw), "turn:") || strings.Contains(strings.ToLower(raw), "turns:") {
		t.Fatalf("local status must not expose TURN credentials: %s", raw)
	}
}

func TestHandlerLocalTerminalsUsesTerminalModelOnly(t *testing.T) {
	handler := NewHandler(Config{
		Terminals: terminalSourceStub{terminals: []Terminal{{
			TerminalID:   "term_1",
			Name:         "zsh",
			Command:      []string{"zsh"},
			Cols:         120,
			Rows:         34,
			State:        "running",
			LastActiveAt: time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC),
		}}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/local/terminals", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var body map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body["terminals"]) != 1 {
		t.Fatalf("expected one terminal, got %#v", body)
	}
	terminal := body["terminals"][0]
	if terminal["terminal_id"] != "term_1" {
		t.Fatalf("expected terminal_id term_1, got %#v", terminal)
	}
	for _, forbidden := range []string{"workspace_id", "tab_id", "pane_id"} {
		if _, ok := terminal[forbidden]; ok {
			t.Fatalf("local terminal response must not expose %s: %#v", forbidden, terminal)
		}
	}
}

func TestHandlerLocalPairClaimsAppCertificate(t *testing.T) {
	appPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	claimer := &pairClaimerStub{
		out: pairing.ClaimResponse{
			MachineID:        "mach_local",
			MachineName:      "Local Mac",
			MachinePublicKey: base64.StdEncoding.EncodeToString([]byte("machine-public")),
			AppCertificate: cert.AppCertificateEnvelope{
				Payload: cert.AppCertificatePayload{
					Version:                     1,
					CertID:                      "cert_test",
					MachineID:                   "mach_local",
					MachinePublicKeyFingerprint: "sha256:machine",
					AppDeviceID:                 "app_1",
					AppName:                     "TermX Local Web",
					AppPublicKey:                base64.StdEncoding.EncodeToString(appPublic),
					Capabilities:                []string{"terminal", "file_manager"},
					ExpiresAt:                   time.Date(2026, 5, 1, 10, 10, 0, 0, time.UTC),
				},
				Signature: "signed",
			},
		},
	}
	handler := NewHandler(Config{Pairing: claimer})
	payload := []byte(`{
		"pair_session_id":"pair_1",
		"pair_secret":"secret",
		"app_device_id":"app_1",
		"app_name":"TermX Local Web",
		"app_public_key":"` + base64.StdEncoding.EncodeToString(appPublic) + `",
		"requested_capabilities":["terminal","file_manager"]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/local/pair", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if claimer.got.PairSessionID != "pair_1" || claimer.got.PairSecret != "secret" {
		t.Fatalf("pair claim did not pass session fields: %#v", claimer.got)
	}
	if got := strings.Join(claimer.got.RequestedCapabilities, ","); got != "terminal,file_manager" {
		t.Fatalf("unexpected capabilities %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["machine_id"] != "mach_local" {
		t.Fatalf("expected machine_id mach_local, got %#v", body)
	}
	if body["machine_public_key_fingerprint"] != "sha256:machine" {
		t.Fatalf("expected machine fingerprint in pair response, got %#v", body)
	}
	if body["expires_at"] != "2026-05-01T10:10:00Z" {
		t.Fatalf("expected certificate expiration in pair response, got %#v", body)
	}
	if _, ok := body["machine_private_key"]; ok {
		t.Fatalf("pair response must not expose machine_private_key: %#v", body)
	}
}

func TestHandlerLocalRTCOfferAnswersWithLocalContract(t *testing.T) {
	answerer := &rtcAnswererStub{out: RTCOfferResponse{
		Answer: RTCSessionAnswer{
			SessionID:     "rtc_local_1",
			SDP:           "answer-sdp",
			ICECandidates: []string{"candidate:host-tcp"},
		},
		ICETCPEnabled: true,
		DataChannels:  []string{"api", "terminal:{terminal_id}", "file:{transfer_id}"},
	}}
	handler := NewHandler(Config{RTC: answerer})
	payload := []byte(`{
		"app_certificate":{"payload":{},"signature":"signed"},
		"offer":{
			"session_id":"rtc_local_1",
			"machine_id":"mach_local",
			"terminal_id":"term_1",
			"sdp":"offer-sdp",
			"ice_candidates":["candidate:host"]
		},
		"signature":{
			"algorithm":"ed25519",
			"nonce":"nonce-1",
			"timestamp":1770000000,
			"value":"base64-signature"
		},
		"client":{"type":"browser","transport":"local"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/local/rtc/offer", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if answerer.got.Offer.MachineID != "mach_local" || answerer.got.Offer.TerminalID != "term_1" {
		t.Fatalf("RTC offer did not pass machine/terminal fields: %#v", answerer.got.Offer)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	answer, ok := body["answer"].(map[string]any)
	if !ok || answer["sdp"] != "answer-sdp" {
		t.Fatalf("expected answer SDP, got %#v", body)
	}
	if body["ice_tcp_enabled"] != true {
		t.Fatalf("expected ICE TCP to be enabled, got %#v", body)
	}
	raw := strings.ToLower(rec.Body.String())
	if strings.Contains(raw, "turn:") || strings.Contains(raw, "turns:") {
		t.Fatalf("local RTC response must not expose TURN credentials: %s", raw)
	}
	for _, forbidden := range []string{"workspace", "tab", "pane"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("local RTC response must not expose %s concepts: %s", forbidden, raw)
		}
	}
}
