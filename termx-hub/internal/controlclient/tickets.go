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

	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

type ManagedTicketVerifierConfig struct {
	BaseURL      string
	SharedSecret string
	Client       *http.Client
}

type ManagedTicketVerifier struct {
	baseURL      string
	sharedSecret string
	client       *http.Client
}

func NewManagedTicketVerifier(cfg ManagedTicketVerifierConfig) *ManagedTicketVerifier {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &ManagedTicketVerifier{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		sharedSecret: strings.TrimSpace(cfg.SharedSecret),
		client:       client,
	}
}

func (v *ManagedTicketVerifier) VerifyOfferTicket(ctx context.Context, in registry.OfferTicket) error {
	_, err := v.requestTicket(ctx, "/api/v1/hub/managed-tickets/check", in.TicketID, in.MachineID, in.TerminalID)
	return err
}

func (v *ManagedTicketVerifier) CheckManagedTicket(ctx context.Context, in managed.VerifyTicketInput) (managed.Ticket, error) {
	return v.requestTicket(ctx, "/api/v1/hub/managed-tickets/check", in.TicketID, in.MachineID, in.TerminalID)
}

func (v *ManagedTicketVerifier) ConsumeManagedTicket(ctx context.Context, in managed.VerifyTicketInput) (managed.Ticket, error) {
	return v.requestTicket(ctx, "/api/v1/hub/managed-tickets/consume", in.TicketID, in.MachineID, in.TerminalID)
}

func (v *ManagedTicketVerifier) VerifyAgentRegistration(ctx context.Context, in registry.AgentRegistration) error {
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

func (v *ManagedTicketVerifier) requestTicket(ctx context.Context, path string, ticketID string, machineID string, terminalID string) (managed.Ticket, error) {
	if v == nil || v.baseURL == "" || v.sharedSecret == "" {
		return managed.Ticket{}, errors.New("control ticket verifier is not configured")
	}
	payload, err := json.Marshal(map[string]string{
		"ticket_id":   strings.TrimSpace(ticketID),
		"machine_id":  strings.TrimSpace(machineID),
		"terminal_id": strings.TrimSpace(terminalID),
	})
	if err != nil {
		return managed.Ticket{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return managed.Ticket{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TermX-Hub-Secret", v.sharedSecret)
	resp, err := v.client.Do(req)
	if err != nil {
		return managed.Ticket{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return managed.Ticket{}, fmt.Errorf("control ticket verifier rejected request: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
		return managed.Ticket{}, err
	}
	if decoded.Ticket.Path != managed.PathManaged {
		return managed.Ticket{}, fmt.Errorf("control ticket path %q is not managed", decoded.Ticket.Path)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, decoded.Ticket.ExpiresAt)
	if err != nil {
		return managed.Ticket{}, fmt.Errorf("parse ticket expiry: %w", err)
	}
	return managed.Ticket{
		ID:         decoded.Ticket.ID,
		MachineID:  decoded.Ticket.MachineID,
		TerminalID: decoded.Ticket.TerminalID,
		Path:       decoded.Ticket.Path,
		AllowRelay: decoded.Ticket.AllowRelay,
		ExpiresAt:  expiresAt,
	}, nil
}
