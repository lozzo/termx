package controlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

type AgentControlClientConfig struct {
	BaseURL      string
	SharedSecret string
	Client       *http.Client
}

type AgentControlClient struct {
	baseURL      string
	sharedSecret string
	client       *http.Client
}

func NewAgentControlClient(cfg AgentControlClientConfig) *AgentControlClient {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &AgentControlClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		sharedSecret: strings.TrimSpace(cfg.SharedSecret),
		client:       client,
	}
}

func (v *AgentControlClient) VerifyOfferTicket(ctx context.Context, in registry.OfferTicket) error {
	_, err := v.requestTicket(ctx, "/api/v1/hub/connection-tickets/check", in.TicketID, in.MachineID, in.TerminalID)
	return err
}

func (v *AgentControlClient) CheckConnectionTicket(ctx context.Context, in cloud.ConnectionTicketInput) (cloud.Ticket, error) {
	return v.requestTicket(ctx, "/api/v1/hub/connection-tickets/check", in.TicketID, in.MachineID, in.TerminalID)
}

func (v *AgentControlClient) ConsumeConnectionTicket(ctx context.Context, in cloud.ConnectionTicketInput) (cloud.Ticket, error) {
	return v.requestTicket(ctx, "/api/v1/hub/connection-tickets/consume", in.TicketID, in.MachineID, in.TerminalID)
}

func (v *AgentControlClient) VerifyAgentRegistration(ctx context.Context, in registry.AgentRegistration) error {
	if v == nil || v.baseURL == "" || v.sharedSecret == "" {
		return errors.New("control agent verifier is not configured")
	}
	if strings.TrimSpace(in.MachineID) == "" || strings.TrimSpace(in.AgentID) == "" ||
		strings.TrimSpace(in.SignatureAlgorithm) == "" || strings.TrimSpace(in.SignatureNonce) == "" ||
		in.SignatureTimestamp == 0 || strings.TrimSpace(in.SignatureValue) == "" {
		return errors.New("agent registration signature is required")
	}
	payload, err := json.Marshal(map[string]any{
		"machine_id": strings.TrimSpace(in.MachineID),
		"agent_id":   strings.TrimSpace(in.AgentID),
		"signature": map[string]any{
			"algorithm": strings.TrimSpace(in.SignatureAlgorithm),
			"nonce":     strings.TrimSpace(in.SignatureNonce),
			"timestamp": in.SignatureTimestamp,
			"value":     strings.TrimSpace(in.SignatureValue),
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/api/v1/hub/agents/verify-registration", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TermX-Hub-Secret", v.sharedSecret)
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("control agent verifier rejected request: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (v *AgentControlClient) GetAgentPolicy(ctx context.Context, in registry.AgentPolicyRequest) (registry.AgentPolicy, error) {
	if v == nil || v.baseURL == "" || v.sharedSecret == "" {
		return registry.AgentPolicy{}, errors.New("control agent policy client is not configured")
	}
	payload, err := json.Marshal(map[string]string{
		"machine_id": strings.TrimSpace(in.MachineID),
		"agent_id":   strings.TrimSpace(in.AgentID),
	})
	if err != nil {
		return registry.AgentPolicy{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/api/v1/hub/agents/policy", bytes.NewReader(payload))
	if err != nil {
		return registry.AgentPolicy{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TermX-Hub-Secret", v.sharedSecret)
	resp, err := v.client.Do(req)
	if err != nil {
		return registry.AgentPolicy{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return registry.AgentPolicy{}, fmt.Errorf("control agent policy rejected request: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Policy struct {
			MachineID    string `json:"machine_id"`
			AgentID      string `json:"agent_id"`
			ForceOffline bool   `json:"force_offline"`
			Reason       string `json:"reason"`
		} `json:"policy"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return registry.AgentPolicy{}, err
	}
	if decoded.Policy.MachineID != strings.TrimSpace(in.MachineID) || decoded.Policy.AgentID != strings.TrimSpace(in.AgentID) {
		return registry.AgentPolicy{}, fmt.Errorf("control agent policy identity mismatch")
	}
	return registry.AgentPolicy{
		MachineID:    decoded.Policy.MachineID,
		AgentID:      decoded.Policy.AgentID,
		ForceOffline: decoded.Policy.ForceOffline,
		Reason:       decoded.Policy.Reason,
	}, nil
}

func (v *AgentControlClient) requestTicket(ctx context.Context, path string, ticketID string, machineID string, terminalID string) (cloud.Ticket, error) {
	if v == nil || v.baseURL == "" || v.sharedSecret == "" {
		return cloud.Ticket{}, errors.New("control ticket verifier is not configured")
	}
	trimmedTicketID := strings.TrimSpace(ticketID)
	trimmedMachineID := strings.TrimSpace(machineID)
	trimmedTerminalID := strings.TrimSpace(terminalID)
	if trimmedTicketID == "" || trimmedMachineID == "" {
		return cloud.Ticket{}, errors.New("ticket id and machine id are required")
	}
	payload, err := json.Marshal(map[string]string{
		"ticket_id":   trimmedTicketID,
		"machine_id":  trimmedMachineID,
		"terminal_id": trimmedTerminalID,
	})
	if err != nil {
		return cloud.Ticket{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return cloud.Ticket{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TermX-Hub-Secret", v.sharedSecret)
	resp, err := v.client.Do(req)
	if err != nil {
		return cloud.Ticket{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return cloud.Ticket{}, fmt.Errorf("control ticket verifier rejected request: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Ticket struct {
			ID         string `json:"id"`
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id"`
			Path       string `json:"path"`
			AllowRelay bool   `json:"allow_relay"`
			ExpiresAt  string `json:"expires_at"`
		} `json:"ticket"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return cloud.Ticket{}, err
	}
	ticketID = strings.TrimSpace(decoded.Ticket.ID)
	machineID = strings.TrimSpace(decoded.Ticket.MachineID)
	terminalID = strings.TrimSpace(decoded.Ticket.TerminalID)
	if ticketID != trimmedTicketID || machineID != trimmedMachineID || terminalID != trimmedTerminalID {
		return cloud.Ticket{}, fmt.Errorf("control ticket identity mismatch")
	}
	if decoded.Ticket.Path != cloud.PathCloud {
		return cloud.Ticket{}, fmt.Errorf("control ticket path %q is not cloud", decoded.Ticket.Path)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, decoded.Ticket.ExpiresAt)
	if err != nil {
		return cloud.Ticket{}, fmt.Errorf("parse ticket expiry: %w", err)
	}
	return cloud.Ticket{
		ID:         ticketID,
		MachineID:  machineID,
		TerminalID: terminalID,
		Path:       decoded.Ticket.Path,
		AllowRelay: decoded.Ticket.AllowRelay,
		ExpiresAt:  expiresAt,
	}, nil
}
