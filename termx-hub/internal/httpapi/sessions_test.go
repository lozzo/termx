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

	"github.com/lozzow/termx/termx-hub/internal/httpapi"
	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestManagedSessionHTTPContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 9, 55, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{Managed: svc})

	requestBody := map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"app_certificate": map[string]any{
			"payload":   map[string]any{"machine_id": "mach_1"},
			"signature": "cert-signature",
		},
		"offer": map[string]any{
			"session_id":     "rtc_managed_1",
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

	polled, err := svc.PollAgentOffer(ctx, managed.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if polled.SDP != minimalSDP("offer") || polled.Path != managed.PathManaged {
		t.Fatalf("polled offer = %+v", polled)
	}
	if polled.SessionID != "rtc_managed_1" {
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
	if err := svc.SubmitAnswer(ctx, managed.SubmitAnswerInput{
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
	}
	decodeJSON(t, answer, &got)
	if got.SessionID != "rtc_managed_1" || got.Path != "managed" || got.MachineID != "mach_1" || got.Answer.SDP != minimalSDP("answer") {
		t.Fatalf("answer response = %+v", got)
	}
	if strings.Contains(strings.ToLower(answer.Body.String()), `"path":"relay"`) {
		t.Fatalf("relay surfaced as client path: %s", answer.Body.String())
	}
}

func TestManagedSessionHTTPRejectsRuntimePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 1, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Managed: managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock}),
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
			"session_id": "rtc_managed_bad",
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

func TestManagedSessionHTTPRejectsMissingEnvelopeBeforeConsumingTicket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 18, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Managed: managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock}),
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
	if len(verifier.consumeCalls) != 0 {
		t.Fatalf("missing envelope consumed ticket: %v", verifier.consumeCalls)
	}
}

func TestManagedSessionHTTPTimeoutReturnsRecoverableSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 20, 0, 0, time.UTC))
	verifier := &fakeTicketVerifier{now: clock.Now(), tickets: map[string]managed.Ticket{
		"ticket_allowed": {
			ID:         "ticket_allowed",
			MachineID:  "mach_1",
			TerminalID: "term_1",
			Path:       managed.PathManaged,
			ExpiresAt:  clock.Now().Add(time.Minute),
		},
	}}
	reg := registry.New(registry.Config{Clock: clock, Verifier: verifier})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier, Clock: clock})
	router := httpapi.NewHandler(httpapi.Config{
		Managed:       svc,
		AnswerTimeout: time.Millisecond,
		PollInterval:   time.Millisecond,
	})
	resp := postJSON(t, router, "/api/v1/sessions", validManagedSessionRequest())
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
	if got.SessionID != "rtc_managed_1" || got.Path != "managed" || got.MachineID != "mach_1" || !got.Pending {
		t.Fatalf("timeout response = %+v", got)
	}

	polled, err := svc.PollAgentOffer(ctx, managed.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll after timeout: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, managed.SubmitAnswerInput{
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

func TestManagedSessionHTTPRejectsOversizedBody(t *testing.T) {
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

func postJSON(t *testing.T, handler http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
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

type fakeTicketVerifier struct {
	tickets map[string]managed.Ticket
	used    map[string]bool
	now     time.Time
	consumeCalls []string
}

func (f *fakeTicketVerifier) VerifyManagedTicket(_ context.Context, in managed.VerifyTicketInput) (managed.Ticket, error) {
	f.consumeCalls = append(f.consumeCalls, in.MachineID+"/"+in.TerminalID+"/"+in.TicketID)
	ticket, ok := f.tickets[in.TicketID]
	if !ok {
		return managed.Ticket{}, managed.ErrTicketExpired
	}
	if ticket.MachineID != in.MachineID {
		return managed.Ticket{}, managed.ErrWrongMachine
	}
	if in.TerminalID != "" && ticket.TerminalID != in.TerminalID {
		return managed.Ticket{}, managed.ErrWrongTerminal
	}
	if !ticket.ExpiresAt.After(f.now) {
		return managed.Ticket{}, managed.ErrTicketExpired
	}
	if f.used == nil {
		f.used = make(map[string]bool)
	}
	if f.used[in.TicketID] {
		return managed.Ticket{}, managed.ErrTicketUsed
	}
	f.used[in.TicketID] = true
	return ticket, nil
}

func (f *fakeTicketVerifier) VerifyAgentRegistration(context.Context, registry.AgentRegistration) error {
	return nil
}

func (f *fakeTicketVerifier) VerifyOfferTicket(_ context.Context, in registry.OfferTicket) error {
	ticket, ok := f.tickets[in.TicketID]
	if !ok {
		return managed.ErrTicketExpired
	}
	if ticket.MachineID != in.MachineID {
		return managed.ErrWrongMachine
	}
	if in.TerminalID != "" && ticket.TerminalID != in.TerminalID {
		return managed.ErrWrongTerminal
	}
	if !ticket.ExpiresAt.After(f.now) {
		return managed.ErrTicketExpired
	}
	return nil
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

func minimalSDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}

func validManagedSessionRequest() map[string]any {
	return map[string]any{
		"connect_ticket": "ticket_allowed",
		"machine_id":     "mach_1",
		"terminal_id":    "term_1",
		"app_certificate": map[string]any{
			"payload":   map[string]any{"machine_id": "mach_1"},
			"signature": "cert-signature",
		},
		"offer": map[string]any{
			"session_id":     "rtc_managed_1",
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
