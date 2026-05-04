package controlclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/cloud"
	"github.com/lozzow/termx/termx-hub/internal/controlclient"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

func TestConnectionTicketVerifierChecksAndConsumesThroughWebControl(t *testing.T) {
	t.Parallel()

	var seen []string
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if r.Header.Get("X-TermX-Hub-Secret") != "hub-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			TicketID   string `json:"ticket_id"`
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.TicketID != "ticket-1" || req.MachineID != "mach-1" || req.TerminalID != "term-1" {
			t.Fatalf("ticket request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ticket": map[string]any{
				"id":          req.TicketID,
				"machine_id":  req.MachineID,
				"terminal_id": req.TerminalID,
				"path":        "cloud",
				"allow_relay": false,
				"expires_at":  "2026-05-03T13:00:00Z",
			},
		})
	}))
	defer control.Close()

	verifier := controlclient.NewConnectionTicketVerifier(controlclient.ConnectionTicketVerifierConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	if err := verifier.VerifyOfferTicket(context.Background(), registry.OfferTicket{
		TicketID:   "ticket-1",
		MachineID:  "mach-1",
		TerminalID: "term-1",
	}); err != nil {
		t.Fatalf("check ticket: %v", err)
	}
	ticket, err := verifier.ConsumeConnectionTicket(context.Background(), cloud.VerifyTicketInput{
		TicketID:   "ticket-1",
		MachineID:  "mach-1",
		TerminalID: "term-1",
	})
	if err != nil {
		t.Fatalf("consume ticket: %v", err)
	}
	if ticket.ID != "ticket-1" || ticket.Path != cloud.PathCloud || !ticket.ExpiresAt.Equal(time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("ticket = %+v", ticket)
	}
	if len(seen) != 2 || seen[0] != "/api/v1/hub/connection-tickets/check" || seen[1] != "/api/v1/hub/connection-tickets/consume" {
		t.Fatalf("paths = %v", seen)
	}
}

func TestConnectionTicketVerifierVerifiesAgentRegistrationThroughWebControl(t *testing.T) {
	t.Parallel()

	var got struct {
		MachineID string `json:"machine_id"`
		AgentID   string `json:"agent_id"`
		Signature struct {
			Algorithm string `json:"algorithm"`
			Nonce     string `json:"nonce"`
			Timestamp int64  `json:"timestamp"`
			Value     string `json:"value"`
		} `json:"signature"`
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/hub/agents/verify-registration" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-TermX-Hub-Secret") != "hub-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer control.Close()

	verifier := controlclient.NewConnectionTicketVerifier(controlclient.ConnectionTicketVerifierConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	err := verifier.VerifyAgentRegistration(context.Background(), registry.AgentRegistration{
		MachineID:          "mach-1",
		AgentID:            "agent-1",
		SignatureAlgorithm: "ed25519",
		SignatureNonce:     "nonce-1",
		SignatureTimestamp: 1777808400,
		SignatureValue:     "signature-value",
	})
	if err != nil {
		t.Fatalf("verify agent registration: %v", err)
	}
	if got.MachineID != "mach-1" || got.AgentID != "agent-1" ||
		got.Signature.Algorithm != "ed25519" || got.Signature.Nonce != "nonce-1" ||
		got.Signature.Timestamp != 1777808400 || got.Signature.Value != "signature-value" {
		t.Fatalf("agent verifier request = %+v", got)
	}
}

func TestConnectionTicketVerifierFetchesAgentPolicyThroughWebControl(t *testing.T) {
	t.Parallel()

	var got struct {
		MachineID string `json:"machine_id"`
		AgentID   string `json:"agent_id"`
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/hub/agents/policy" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-TermX-Hub-Secret") != "hub-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"policy": map[string]any{
				"machine_id":     got.MachineID,
				"agent_id":       got.AgentID,
				"force_offline":  true,
				"reason":         "owner requested",
				"allow_relay":    false,
				"relay_in_use":   false,
				"transport_path": "cloud",
			},
		})
	}))
	defer control.Close()

	verifier := controlclient.NewConnectionTicketVerifier(controlclient.ConnectionTicketVerifierConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	policy, err := verifier.GetAgentPolicy(context.Background(), registry.AgentPolicyRequest{
		MachineID: "mach-1",
		AgentID:   "agent-1",
	})
	if err != nil {
		t.Fatalf("get agent policy: %v", err)
	}
	if got.MachineID != "mach-1" || got.AgentID != "agent-1" {
		t.Fatalf("policy request = %+v", got)
	}
	if !policy.ForceOffline || policy.Reason != "owner requested" || policy.MachineID != "mach-1" || policy.AgentID != "agent-1" {
		t.Fatalf("policy = %+v", policy)
	}
}

func TestConnectionTicketVerifierRejectsMismatchedAgentPolicy(t *testing.T) {
	t.Parallel()

	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"policy": map[string]any{
				"machine_id":    "other-machine",
				"agent_id":      "agent-1",
				"force_offline": true,
			},
		})
	}))
	defer control.Close()

	verifier := controlclient.NewConnectionTicketVerifier(controlclient.ConnectionTicketVerifierConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	if _, err := verifier.GetAgentPolicy(context.Background(), registry.AgentPolicyRequest{
		MachineID: "mach-1",
		AgentID:   "agent-1",
	}); err == nil {
		t.Fatal("mismatched agent policy was accepted")
	}
}

func TestConnectionTicketVerifierRejectsUnsignedAgentRegistration(t *testing.T) {
	t.Parallel()

	verifier := controlclient.NewConnectionTicketVerifier(controlclient.ConnectionTicketVerifierConfig{
		BaseURL:      "http://127.0.0.1:1",
		SharedSecret: "hub-secret",
	})
	if err := verifier.VerifyAgentRegistration(context.Background(), registry.AgentRegistration{
		MachineID: "mach-1",
		AgentID:   "agent-1",
	}); err == nil {
		t.Fatal("unsigned agent registration was accepted")
	}
}

func TestConnectionTicketVerifierFailsClosedWithoutTURNOrRuntimePayload(t *testing.T) {
	t.Parallel()

	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ticket": map[string]any{
				"id":          "ticket-1",
				"machine_id":  "mach-1",
				"terminal_id": "term-1",
				"path":        "relay",
				"allow_relay": true,
				"expires_at":  "2026-05-03T13:00:00Z",
			},
		})
	}))
	defer control.Close()

	verifier := controlclient.NewConnectionTicketVerifier(controlclient.ConnectionTicketVerifierConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	_, err := verifier.ConsumeConnectionTicket(context.Background(), cloud.VerifyTicketInput{
		TicketID:   "ticket-1",
		MachineID:  "mach-1",
		TerminalID: "term-1",
	})
	if err == nil {
		t.Fatal("relay path ticket was accepted as cloud")
	}
}
