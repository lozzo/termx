package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

func TestCloudSessionHTTPContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 9, 55, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{
		Cloud: svc,
		ICE: ice.NewService(ice.Config{
			Clock:        clock,
			SharedSecret: "turn-secret",
			STUNURLs:     []string{"stun:hub.termx.test:3478"},
			TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
		}),
	})

	requestBody := map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"app_certificate": map[string]any{
			"payload":   map[string]any{"machine_id": "mach_1"},
			"signature": "cert-signature",
		},
		"offer": map[string]any{
			"session_id":     "rtc_cloud_1",
			"sdp":            minimalSDP("offer"),
			"ice_candidates": []any{"candidate:1 1 udp 1 192.0.2.1 1 typ host"},
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     "nonce-1",
			"timestamp": 1770000000,
			"value":     "offer-signature",
		},
	}

	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseCh <- postJSON(t, router, "/api/v1/sessions", requestBody)
	}()

	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if polled.SDP != minimalSDP("offer") || polled.Path != cloud.PathCloud {
		t.Fatalf("polled offer = %+v", polled)
	}
	if polled.SessionID != "rtc_cloud_1" {
		t.Fatalf("polled session id = %q, want app session id", polled.SessionID)
	}
	if len(polled.AppCertificate) == 0 || !strings.Contains(string(polled.AppCertificate), "cert-signature") {
		t.Fatalf("polled offer missing app certificate: %+v", polled)
	}
	if polled.Signature.Value != "offer-signature" || polled.Signature.Nonce != "nonce-1" || polled.Signature.Timestamp != 1770000000 {
		t.Fatalf("polled offer missing signature envelope: %+v", polled.Signature)
	}
	if len(polled.ICECandidates) != 1 || polled.ICECandidates[0] != "candidate:1 1 udp 1 192.0.2.1 1 typ host" {
		t.Fatalf("polled offer missing standalone candidates: %+v", polled.ICECandidates)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   polled.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}

	var answer *httptest.ResponseRecorder
	select {
	case answer = <-responseCh:
	case <-time.After(2 * time.Second):
		t.Fatal("session request did not return after agent answered")
	}
	if answer.Code != http.StatusOK {
		t.Fatalf("answer status = %d body=%s", answer.Code, answer.Body.String())
	}
	var got struct {
		SessionID string `json:"session_id"`
		Path      string `json:"path"`
		MachineID string `json:"machine_id"`
		Answer    struct {
			SDP string `json:"sdp"`
		} `json:"answer"`
		ICEServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"ice_servers"`
		RelayPolicy struct {
			AllowRelay         bool `json:"allow_relay"`
			AllowRelayTransfer bool `json:"allow_relay_transfer"`
		} `json:"relay_policy"`
	}
	decodeJSON(t, answer, &got)
	if got.SessionID != "rtc_cloud_1" || got.Path != "cloud" || got.MachineID != "mach_1" || got.Answer.SDP != minimalSDP("answer") {
		t.Fatalf("answer response = %+v", got)
	}
	if got.RelayPolicy.AllowRelay || got.RelayPolicy.AllowRelayTransfer {
		t.Fatalf("relay policy = %+v", got.RelayPolicy)
	}
	if len(got.ICEServers) != 1 || got.ICEServers[0].URLs[0] != "stun:hub.termx.test:3478" {
		t.Fatalf("cloud ICE servers = %+v", got.ICEServers)
	}
	if strings.Contains(strings.ToLower(answer.Body.String()), `"path":"relay"`) {
		t.Fatalf("relay surfaced as client path: %s", answer.Body.String())
	}
}

func TestCloudSessionHTTPWaitsForDelayedAgentAnswer(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 10, 22, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:         svc,
		Registry:      reg,
		AnswerTimeout: 500 * time.Millisecond,
		PollInterval:  time.Millisecond,
	})
	register := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"version":   "remote.hub.v1",
		"device_id": "mach_1",
		"terminals": []map[string]any{{
			"id":    "term_1",
			"state": "running",
		}},
	})
	if register.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", register.Code, register.Body.String())
	}
	var registered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, register, &registered)

	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseCh <- postJSON(t, router, "/api/v1/sessions", validCloudSessionRequest())
	}()

	poll := postJSON(t, router, "/api/v1/agents/signaling/poll", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "mach_1",
		"timeout_seconds":  1,
	})
	if poll.Code != http.StatusOK {
		t.Fatalf("agent poll status = %d body=%s", poll.Code, poll.Body.String())
	}
	var pollResp struct {
		Offer struct {
			SessionID string `json:"session_id"`
		} `json:"offer"`
	}
	decodeJSON(t, poll, &pollResp)
	if pollResp.Offer.SessionID != "rtc_cloud_1" {
		t.Fatalf("poll session id = %q", pollResp.Offer.SessionID)
	}
	answer := postJSON(t, router, "/api/v1/agents/signaling/answer", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "mach_1",
		"answer": map[string]any{
			"session_id": pollResp.Offer.SessionID,
			"sdp":        minimalSDP("delayed-answer"),
		},
	})
	if answer.Code != http.StatusNoContent {
		t.Fatalf("agent answer status = %d body=%s", answer.Code, answer.Body.String())
	}

	var response *httptest.ResponseRecorder
	select {
	case response = <-responseCh:
	case <-time.After(2 * time.Second):
		t.Fatal("session request did not return")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("session response status = %d body=%s", response.Code, response.Body.String())
	}
	var got struct {
		Answer struct {
			SDP string `json:"sdp"`
		} `json:"answer"`
	}
	decodeJSON(t, response, &got)
	if got.Answer.SDP != minimalSDP("delayed-answer") {
		t.Fatalf("answer response = %+v", got)
	}
}

func TestCloudSessionHTTPRelaysAgentAnswerError(t *testing.T) {
	t.Parallel()

	clock := fixedClock(time.Date(2026, 5, 3, 10, 25, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:         svc,
		Registry:      reg,
		AnswerTimeout: time.Millisecond,
		PollInterval:  time.Millisecond,
	})
	register := postJSON(t, router, "/api/v1/agents/register", map[string]any{
		"version":   "remote.hub.v1",
		"device_id": "mach_1",
	})
	if register.Code != http.StatusOK {
		t.Fatalf("agent register status = %d body=%s", register.Code, register.Body.String())
	}
	var registered struct {
		AgentSessionID string `json:"agent_session_id"`
	}
	decodeJSON(t, register, &registered)

	session := postJSON(t, router, "/api/v1/sessions", validCloudSessionRequest())
	if session.Code != http.StatusAccepted {
		t.Fatalf("initial session status = %d body=%s", session.Code, session.Body.String())
	}
	poll := postJSON(t, router, "/api/v1/agents/signaling/poll", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "mach_1",
		"timeout_seconds":  1,
	})
	if poll.Code != http.StatusOK {
		t.Fatalf("agent poll status = %d body=%s", poll.Code, poll.Body.String())
	}
	var pollResp struct {
		Offer struct {
			SessionID string `json:"session_id"`
		} `json:"offer"`
	}
	decodeJSON(t, poll, &pollResp)

	answer := postJSON(t, router, "/api/v1/agents/signaling/answer", map[string]any{
		"agent_session_id": registered.AgentSessionID,
		"device_id":        "mach_1",
		"answer": map[string]any{
			"session_id": pollResp.Offer.SessionID,
			"error":      "set remote description: unsupported offer",
		},
	})
	if answer.Code != http.StatusNoContent {
		t.Fatalf("agent error answer status = %d body=%s", answer.Code, answer.Body.String())
	}

	appAnswer := postJSON(t, router, "/api/v1/sessions/rtc_cloud_1/answer", map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
	})
	if appAnswer.Code != http.StatusForbidden {
		t.Fatalf("app answer error status = %d body=%s", appAnswer.Code, appAnswer.Body.String())
	}
	if !strings.Contains(appAnswer.Body.String(), "unsupported offer") {
		t.Fatalf("app answer error did not include agent reason: %s", appAnswer.Body.String())
	}
}

func TestCloudSessionHTTPRejectsRuntimePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 1, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Cloud: cloud.NewService(cloud.Config{Registry: reg, Clock: clock}),
	})
	resp := postJSON(t, router, "/api/v1/sessions", map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"app_certificate": map[string]any{
			"payload":   map[string]any{"machine_id": "mach_1"},
			"signature": "cert-signature",
		},
		"offer": map[string]any{
			"session_id": "rtc_cloud_bad",
			"sdp":        "v=0\r\nterminal_data=must-not-forward",
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     "nonce-1",
			"timestamp": 1770000000,
			"value":     "offer-signature",
		},
	})
	if resp.Code != http.StatusForbidden {
		t.Fatalf("runtime payload status = %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "runtime") {
		t.Fatalf("runtime rejection body = %s", resp.Body.String())
	}
}

func TestCloudSessionHTTPRejectsMissingEnvelopeBeforeConsumingTicket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 18, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Cloud: cloud.NewService(cloud.Config{Registry: reg, Clock: clock}),
	})
	resp := postJSON(t, router, "/api/v1/sessions", map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"offer": map[string]any{
			"session_id": "rtc_missing_envelope",
			"sdp":        minimalSDP("offer"),
		},
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("missing envelope status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCloudSessionHTTPTimeoutReturnsRecoverableSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 20, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:         svc,
		AnswerTimeout: time.Millisecond,
		PollInterval:  time.Millisecond,
	})
	resp := postJSON(t, router, "/api/v1/sessions", validCloudSessionRequest())
	if resp.Code != http.StatusAccepted {
		t.Fatalf("timeout status = %d body=%s", resp.Code, resp.Body.String())
	}
	var got struct {
		SessionID string `json:"session_id"`
		Path      string `json:"path"`
		MachineID string `json:"machine_id"`
		Pending   bool   `json:"pending"`
	}
	decodeJSON(t, resp, &got)
	if got.SessionID != "rtc_cloud_1" || got.Path != "cloud" || got.MachineID != "mach_1" || !got.Pending {
		t.Fatalf("timeout response = %+v", got)
	}

	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll after timeout: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   polled.ID,
		SDP:       minimalSDP("answer-after-timeout"),
	}); err != nil {
		t.Fatalf("answer after timeout: %v", err)
	}
	answer := postJSON(t, router, "/api/v1/sessions/"+got.SessionID+"/answer", map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
	})
	if answer.Code != http.StatusOK {
		t.Fatalf("recover answer status = %d body=%s", answer.Code, answer.Body.String())
	}
	var recovered struct {
		SessionID string `json:"session_id"`
		Answer    struct {
			SDP string `json:"sdp"`
		} `json:"answer"`
	}
	decodeJSON(t, answer, &recovered)
	if recovered.SessionID != got.SessionID || recovered.Answer.SDP != minimalSDP("answer-after-timeout") {
		t.Fatalf("recovered answer = %+v", recovered)
	}
}

func TestCloudSessionHTTPPendingAnswerPollReturnsRecoverableSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 5, 10, 30, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:         cloud.NewService(cloud.Config{Registry: reg, Clock: clock}),
		AnswerTimeout: time.Millisecond,
		PollInterval:  time.Millisecond,
	})
	resp := postJSON(t, router, "/api/v1/sessions", validCloudSessionRequest())
	if resp.Code != http.StatusAccepted {
		t.Fatalf("timeout status = %d body=%s", resp.Code, resp.Body.String())
	}
	var pending struct {
		SessionID  string `json:"session_id"`
		Path       string `json:"path"`
		MachineID  string `json:"machine_id"`
		TerminalID string `json:"terminal_id"`
		Pending    bool   `json:"pending"`
	}
	decodeJSON(t, resp, &pending)
	if pending.SessionID == "" || !pending.Pending {
		t.Fatalf("pending session response = %+v", pending)
	}

	answer := postJSON(t, router, "/api/v1/sessions/"+pending.SessionID+"/answer", map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
	})
	if answer.Code != http.StatusAccepted {
		t.Fatalf("pending answer status = %d body=%s", answer.Code, answer.Body.String())
	}
	var got struct {
		SessionID  string `json:"session_id"`
		Path       string `json:"path"`
		MachineID  string `json:"machine_id"`
		TerminalID string `json:"terminal_id"`
		Pending    bool   `json:"pending"`
	}
	decodeJSON(t, answer, &got)
	if got.SessionID != pending.SessionID || got.Path != "cloud" || got.MachineID != "mach_1" || got.TerminalID != "term_1" || !got.Pending {
		t.Fatalf("pending answer response = %+v", got)
	}
}

func TestCloudSessionHTTPRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	router := httpapi.NewHandler(httpapi.Config{MaxBodyBytes: 128})
	resp := postJSON(t, router, "/api/v1/sessions", map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"app_certificate": map[string]any{
			"payload": strings.Repeat("x", 512),
		},
		"offer": map[string]any{
			"session_id": "rtc_oversized",
			"sdp":        minimalSDP("offer"),
		},
	})
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func validCloudSessionRequestWithIDs(machineID, terminalID, sessionID string) map[string]any {
	req := validCloudSessionRequest()
	req["machine_id"] = machineID
	req["terminal_id"] = terminalID
	offer := req["offer"].(map[string]any)
	offer["session_id"] = sessionID
	return req
}

func postJSON(t *testing.T, handler http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return postJSONWithSecret(t, handler, path, "", body)
}

func postJSONWithSecret(t *testing.T, handler http.Handler, path string, secret string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-TermX-Hub-Secret", secret)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func getJSON(t *testing.T, handler http.Handler, path string, debugToken string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if debugToken != "" {
		req.Header.Set("X-TermX-Debug-Token", debugToken)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func decodeJSON(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(resp.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response %q: %v", resp.Body.String(), err)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type mutableHTTPClock struct {
	value time.Time
}

func (c *mutableHTTPClock) Now() time.Time {
	return c.value
}

func minimalSDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}

func validCloudSessionRequest() map[string]any {
	return map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"app_certificate": map[string]any{
			"payload":   map[string]any{"machine_id": "mach_1"},
			"signature": "cert-signature",
		},
		"offer": map[string]any{
			"session_id":     "rtc_cloud_1",
			"sdp":            minimalSDP("offer"),
			"ice_candidates": []any{},
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     "nonce-1",
			"timestamp": 1770000000,
			"value":     "offer-signature",
		},
	}
}
