package controlclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/controlclient"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

func TestAgentControlClientVerifiesAgentRegistrationThroughWebControl(t *testing.T) {
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

	client := controlclient.NewAgentControlClient(controlclient.AgentControlClientConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	err := client.VerifyAgentRegistration(context.Background(), registry.AgentRegistration{
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

func TestAgentControlClientFetchesAgentPolicyThroughWebControl(t *testing.T) {
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

	client := controlclient.NewAgentControlClient(controlclient.AgentControlClientConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	policy, err := client.GetAgentPolicy(context.Background(), registry.AgentPolicyRequest{
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

func TestAgentControlClientVerifiesMachineScopedConnectionTicketThroughWebControl(t *testing.T) {
	t.Parallel()

	var paths []string
	var got []struct {
		TicketID   string `json:"ticket_id"`
		MachineID  string `json:"machine_id"`
		TerminalID string `json:"terminal_id"`
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("X-TermX-Hub-Secret") != "hub-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			TicketID   string `json:"ticket_id"`
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		got = append(got, body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ticket": map[string]any{
				"id":          body.TicketID,
				"machine_id":  body.MachineID,
				"terminal_id": body.TerminalID,
				"path":        "cloud",
				"allow_relay": false,
				"expires_at":  "2026-05-04T12:30:00Z",
			},
		})
	}))
	defer control.Close()

	client := controlclient.NewAgentControlClient(controlclient.AgentControlClientConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	if err := client.VerifyOfferTicket(context.Background(), registry.OfferTicket{
		MachineID: "mach-1",
		TicketID:  "ticket-1",
	}); err != nil {
		t.Fatalf("verify offer ticket: %v", err)
	}
	ticket, err := client.ConsumeConnectionTicket(context.Background(), cloud.ConnectionTicketInput{
		MachineID: "mach-1",
		TicketID:  "ticket-1",
	})
	if err != nil {
		t.Fatalf("consume connection ticket: %v", err)
	}
	if ticket.ID != "ticket-1" || ticket.MachineID != "mach-1" || ticket.TerminalID != "" || ticket.Path != cloud.PathCloud {
		t.Fatalf("ticket = %+v", ticket)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/hub/connection-tickets/check" || paths[1] != "/api/v1/hub/connection-tickets/consume" {
		t.Fatalf("paths = %#v", paths)
	}
	if len(got) != 2 || got[0].TerminalID != "" || got[1].TerminalID != "" {
		t.Fatalf("requests = %#v", got)
	}
}

func TestAgentControlClientRejectsMismatchedAgentPolicy(t *testing.T) {
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

	client := controlclient.NewAgentControlClient(controlclient.AgentControlClientConfig{
		BaseURL:      control.URL,
		SharedSecret: "hub-secret",
		Client:       control.Client(),
	})
	if _, err := client.GetAgentPolicy(context.Background(), registry.AgentPolicyRequest{
		MachineID: "mach-1",
		AgentID:   "agent-1",
	}); err == nil {
		t.Fatal("mismatched agent policy was accepted")
	}
}

func TestAgentControlClientRejectsUnsignedAgentRegistration(t *testing.T) {
	t.Parallel()

	client := controlclient.NewAgentControlClient(controlclient.AgentControlClientConfig{
		BaseURL:      "http://127.0.0.1:1",
		SharedSecret: "hub-secret",
	})
	if err := client.VerifyAgentRegistration(context.Background(), registry.AgentRegistration{
		MachineID: "mach-1",
		AgentID:   "agent-1",
	}); err == nil {
		t.Fatal("unsigned agent registration was accepted")
	}
}
