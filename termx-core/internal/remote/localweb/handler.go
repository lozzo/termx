package localweb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	"github.com/lozzow/termx/termx-core/internal/remote/pairing"
)

type Status struct {
	MachineID                   string         `json:"machine_id"`
	MachineName                 string         `json:"machine_name"`
	MachinePublicKeyFingerprint string         `json:"machine_public_key_fingerprint"`
	RemoteEnabled               bool           `json:"remote_enabled"`
	LocalRTC                    LocalRTCStatus `json:"local_rtc"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

type LocalRTCStatus struct {
	HTTPURL       string `json:"http_url"`
	ICETCPEnabled bool   `json:"ice_tcp_enabled"`
	ICETCPPort    int    `json:"ice_tcp_port,omitempty"`
}

type Terminal struct {
	TerminalID   string    `json:"terminal_id"`
	Name         string    `json:"name"`
	Command      []string  `json:"command"`
	Cols         int       `json:"cols"`
	Rows         int       `json:"rows"`
	State        string    `json:"state"`
	LastActiveAt time.Time `json:"last_active_at,omitempty"`
	SizeLocked   bool      `json:"size_locked"`
	SizeLockMode string    `json:"size_lock_mode,omitempty"`
	CWD          string    `json:"cwd,omitempty"`
	Environment  string    `json:"environment,omitempty"`
}

type PairResponse struct {
	MachineID                   string                      `json:"machine_id"`
	MachineName                 string                      `json:"machine_name"`
	MachinePublicKey            string                      `json:"machine_public_key"`
	MachinePublicKeyFingerprint string                      `json:"machine_public_key_fingerprint"`
	AppCertificate              cert.AppCertificateEnvelope `json:"app_certificate"`
	ExpiresAt                   time.Time                   `json:"expires_at"`
}

type RTCSessionOffer struct {
	SessionID     string   `json:"session_id"`
	MachineID     string   `json:"machine_id"`
	TerminalID    string   `json:"terminal_id"`
	SDP           string   `json:"sdp"`
	ICECandidates []string `json:"ice_candidates"`
}

type RTCOfferSignature struct {
	Algorithm string `json:"algorithm"`
	Nonce     string `json:"nonce"`
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
}

type RTCOfferClient struct {
	Type      string `json:"type"`
	Transport string `json:"transport"`
}

type RTCOfferRequest struct {
	AppCertificate cert.AppCertificateEnvelope `json:"app_certificate"`
	Offer          RTCSessionOffer             `json:"offer"`
	Signature      RTCOfferSignature           `json:"signature"`
	Client         RTCOfferClient              `json:"client"`
}

type RTCSessionAnswer struct {
	SessionID     string   `json:"session_id"`
	SDP           string   `json:"sdp"`
	ICECandidates []string `json:"ice_candidates"`
}

type RTCOfferResponse struct {
	Answer        RTCSessionAnswer `json:"answer"`
	ICETCPEnabled bool             `json:"ice_tcp_enabled"`
	DataChannels  []string         `json:"data_channels"`
}

type StatusSource interface {
	LocalStatus(ctx context.Context) (Status, error)
}

type TerminalSource interface {
	ListTerminals(ctx context.Context) ([]Terminal, error)
}

type PairClaimer interface {
	ClaimPairSession(ctx context.Context, req pairing.ClaimRequest) (pairing.ClaimResponse, error)
}

type RTCOfferAnswerer interface {
	AnswerLocalRTCOffer(ctx context.Context, req RTCOfferRequest) (RTCOfferResponse, error)
}

type Config struct {
	Assets    fs.FS
	Status    StatusSource
	Terminals TerminalSource
	Pairing   PairClaimer
	RTC       RTCOfferAnswerer
}

type Handler struct {
	assets    fs.FS
	status    StatusSource
	terminals TerminalSource
	pairing   PairClaimer
	rtc       RTCOfferAnswerer
}

func NewHandler(cfg Config) http.Handler {
	return &Handler{
		assets:    cfg.Assets,
		status:    cfg.Status,
		terminals: cfg.Terminals,
		pairing:   cfg.Pairing,
		rtc:       cfg.RTC,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/local/status":
		h.handleStatus(w, r)
	case r.URL.Path == "/api/local/terminals":
		h.handleTerminals(w, r)
	case r.URL.Path == "/api/local/pair":
		h.handlePair(w, r)
	case r.URL.Path == "/api/local/rtc/offer":
		h.handleRTCOffer(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		h.handleAsset(w, r)
	}
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if h.status == nil {
		writeError(w, http.StatusServiceUnavailable, "local_status_unavailable", "local status unavailable")
		return
	}
	status, err := h.status.LocalStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "local_status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) handleTerminals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if h.terminals == nil {
		writeError(w, http.StatusServiceUnavailable, "local_terminals_unavailable", "local terminal inventory unavailable")
		return
	}
	terminals, err := h.terminals.ListTerminals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "local_terminals_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"terminals": terminals})
}

func (h *Handler) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if h.pairing == nil {
		writeError(w, http.StatusServiceUnavailable, "local_pairing_unavailable", "local pairing unavailable")
		return
	}
	var req pairing.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	out, err := h.pairing.ClaimPairSession(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "pair_claim_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, PairResponse{
		MachineID:                   out.MachineID,
		MachineName:                 out.MachineName,
		MachinePublicKey:            out.MachinePublicKey,
		MachinePublicKeyFingerprint: out.AppCertificate.Payload.MachinePublicKeyFingerprint,
		AppCertificate:              out.AppCertificate,
		ExpiresAt:                   out.AppCertificate.Payload.ExpiresAt,
	})
}

func (h *Handler) handleRTCOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if h.rtc == nil {
		writeError(w, http.StatusServiceUnavailable, "local_rtc_unavailable", "local RTC unavailable")
		return
	}
	var req RTCOfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	out, err := h.rtc.AnswerLocalRTCOffer(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "rtc_offer_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if h.assets == nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(h.assets, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !strings.Contains(name, ".") {
			name = "index.html"
			data, err = fs.ReadFile(h.assets, name)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				writeError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "asset_read_failed", err.Error())
			return
		}
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if strings.EqualFold(path.Ext(name), ".js") {
		contentType = "text/javascript; charset=utf-8"
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":       code,
			"message":    message,
			"request_id": newRequestID(),
		},
	})
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "local_error"
	}
	return "req_" + hex.EncodeToString(b[:])
}
