package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

type Config struct {
	Managed       *managed.Service
	AnswerTimeout time.Duration
	PollInterval   time.Duration
	MaxBodyBytes  int64
}

func NewHandler(cfg Config) http.Handler {
	maxBodyBytes := cfg.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 64 * 1024
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ConnectTicket  string          `json:"connect_ticket"`
			MachineID      string          `json:"machine_id"`
			TerminalID     string          `json:"terminal_id"`
			AppCertificate json.RawMessage `json:"app_certificate"`
			Offer          struct {
				SessionID     string   `json:"session_id"`
				SDP           string   `json:"sdp"`
				ICECandidates []string `json:"ice_candidates"`
			} `json:"offer"`
			Signature offerSignatureRequest `json:"signature"`
		}
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Managed == nil {
			writeError(w, http.StatusServiceUnavailable, "managed_service_unavailable", "managed service is not configured")
			return
		}
		if err := validateSessionRequestEnvelope(req.Offer.SessionID, req.AppCertificate, req.Signature); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_managed_session_envelope", err.Error())
			return
		}
		offer, err := cfg.Managed.SubmitOffer(r.Context(), managed.SubmitOfferInput{
			SessionID:      req.Offer.SessionID,
			TicketID:       req.ConnectTicket,
			MachineID:      req.MachineID,
			TerminalID:     req.TerminalID,
			SDP:            req.Offer.SDP,
			ICECandidates:  req.Offer.ICECandidates,
			AppCertificate: req.AppCertificate,
			Signature: managed.OfferSignature{
				Algorithm: req.Signature.Algorithm,
				Nonce:     req.Signature.Nonce,
				Timestamp: req.Signature.Timestamp,
				Value:     req.Signature.Value,
			},
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "submit_managed_offer_failed", err.Error())
			return
		}
		answer, err := waitForAnswer(r.Context(), cfg, offer.ID, offer.TicketID, offer.MachineID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"session_id": publicSessionID(offer),
					"path":       managed.PathManaged,
					"machine_id": offer.MachineID,
					"terminal_id": offer.TerminalID,
					"pending":    true,
				})
				return
			}
			writeError(w, statusForAnswerError(err), "get_managed_answer_failed", err.Error())
			return
		}
		writeSessionAnswer(w, publicSessionID(offer), offer.MachineID, offer.TerminalID, answer)
	})
	mux.HandleFunc("POST /api/v1/sessions/{session_id}/answer", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ConnectTicket string `json:"connect_ticket"`
			MachineID     string `json:"machine_id"`
		}
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Managed == nil {
			writeError(w, http.StatusServiceUnavailable, "managed_service_unavailable", "managed service is not configured")
			return
		}
		answer, err := cfg.Managed.GetAnswer(r.Context(), managed.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			TicketID:  req.ConnectTicket,
			MachineID: req.MachineID,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "get_managed_answer_failed", err.Error())
			return
		}
		writeSessionAnswer(w, r.PathValue("session_id"), answer.MachineID, "", answer)
	})
	return mux
}

type offerSignatureRequest struct {
	Algorithm string `json:"algorithm"`
	Nonce     string `json:"nonce"`
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
}

func validateSessionRequestEnvelope(sessionID string, appCertificate json.RawMessage, signature offerSignatureRequest) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("offer session_id is required")
	}
	if len(appCertificate) == 0 || !json.Valid(appCertificate) || string(appCertificate) == "null" {
		return errors.New("app_certificate is required")
	}
	if strings.TrimSpace(signature.Algorithm) == "" || strings.TrimSpace(signature.Nonce) == "" ||
		signature.Timestamp == 0 || strings.TrimSpace(signature.Value) == "" {
		return errors.New("signature envelope is required")
	}
	return nil
}

func waitForAnswer(ctx context.Context, cfg Config, offerID string, ticketID string, machineID string) (managed.Answer, error) {
	timeout := cfg.AnswerTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		answer, err := cfg.Managed.GetAnswer(ctx, managed.GetAnswerInput{
			OfferID:   offerID,
			TicketID:  ticketID,
			MachineID: machineID,
		})
		if err == nil {
			return answer, nil
		}
		if !errors.Is(err, registry.ErrOfferNotFound) {
			return managed.Answer{}, err
		}
		select {
		case <-ctx.Done():
			return managed.Answer{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func statusForAnswerError(err error) int {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout
	}
	return http.StatusForbidden
}

func publicSessionID(offer managed.Offer) string {
	if strings.TrimSpace(offer.SessionID) != "" {
		return strings.TrimSpace(offer.SessionID)
	}
	return offer.ID
}

func writeSessionAnswer(w http.ResponseWriter, sessionID string, machineID string, terminalID string, answer managed.Answer) {
	response := map[string]any{
		"session_id": sessionID,
		"path":       managed.PathManaged,
		"machine_id": machineID,
		"answer": map[string]any{
			"sdp":            answer.SDP,
			"ice_candidates": []string{},
		},
		"relay_policy": map[string]any{
			"allow_relay":          false,
			"allow_relay_transfer": false,
		},
		"relay_in_use": answer.RelayInUse,
	}
	if strings.TrimSpace(terminalID) != "" {
		response["terminal_id"] = strings.TrimSpace(terminalID)
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBodyBytes int64, out any) error {
	defer r.Body.Close()
	limited := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(limited).Decode(out); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			return errBodyTooLarge
		}
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

var errBodyTooLarge = errors.New("request body too large")

func writeDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    strings.TrimSpace(code),
			"message": strings.TrimSpace(message),
		},
	})
}
