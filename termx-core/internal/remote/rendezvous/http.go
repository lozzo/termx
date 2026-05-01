package rendezvous

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

type HTTPConfig struct {
	Store *Store
}

type HTTPHandler struct {
	store *Store
}

type channelResponse struct {
	ChannelID         string   `json:"channel_id"`
	ChannelSecret     string   `json:"channel_secret"`
	ExpiresAt         string   `json:"expires_at"`
	PublicSTUNServers []string `json:"public_stun_servers"`
}

type messagesResponse struct {
	Messages []httpMessage `json:"messages"`
}

type httpMessage struct {
	Type         MessageType      `json:"type"`
	From         string           `json:"from,omitempty"`
	AppPublicKey string           `json:"app_public_key,omitempty"`
	Payload      *json.RawMessage `json:"payload"`
}

type postOfferRequest struct {
	ChannelSecret  string          `json:"channel_secret"`
	From           string          `json:"from"`
	AppPublicKey   string          `json:"app_public_key"`
	AppCertificate json.RawMessage `json:"app_certificate"`
	Offer          json.RawMessage `json:"offer"`
	Signature      json.RawMessage `json:"signature"`
}

type postAnswerRequest struct {
	ChannelSecret  string          `json:"channel_secret"`
	From           string          `json:"from"`
	AppPublicKey   string          `json:"app_public_key"`
	AppCertificate json.RawMessage `json:"app_certificate"`
	Answer         json.RawMessage `json:"answer"`
	Signature      json.RawMessage `json:"signature"`
}

type postCandidateRequest struct {
	ChannelSecret string          `json:"channel_secret"`
	From          string          `json:"from"`
	AppPublicKey  string          `json:"app_public_key"`
	Candidate     json.RawMessage `json:"candidate"`
}

func NewHTTPHandler(cfg HTTPConfig) http.Handler {
	return &HTTPHandler{store: cfg.Store}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/v1/anonymous/channels":
		h.handleCreateChannel(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/anonymous/channels/"):
		h.handleChannelPath(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		writeHTTPError(w, http.StatusNotFound, "not_found", "not found")
	default:
		writeHTTPError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (h *HTTPHandler) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !h.storeAvailable(w) {
		return
	}
	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	channel, err := h.store.CreateChannel(req)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "channel_create_failed", err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, channelResponse{
		ChannelID:         channel.ChannelID,
		ChannelSecret:     channel.ChannelSecret,
		ExpiresAt:         channel.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		PublicSTUNServers: append([]string(nil), channel.PublicSTUNServers...),
	})
}

func (h *HTTPHandler) handleChannelPath(w http.ResponseWriter, r *http.Request) {
	if !h.storeAvailable(w) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/anonymous/channels/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeHTTPError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	channelID, action := parts[0], parts[1]
	switch action {
	case "events":
		h.handleEvents(w, r, channelID)
	case "offer":
		h.handleOffer(w, r, channelID)
	case "answer":
		h.handleAnswer(w, r, channelID)
	case "candidate":
		h.handleCandidate(w, r, channelID)
	default:
		writeHTTPError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (h *HTTPHandler) storeAvailable(w http.ResponseWriter) bool {
	if h.store != nil {
		return true
	}
	writeHTTPError(w, http.StatusServiceUnavailable, "rendezvous_unavailable", "rendezvous unavailable")
	return false
}

func (h *HTTPHandler) handleEvents(w http.ResponseWriter, r *http.Request, channelID string) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	secret, ok := rendezvousAuthorization(r.Header.Get("Authorization"), channelID)
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized", "rendezvous authorization is required")
		return
	}
	messages, err := h.store.Events(channelID, secret)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]httpMessage, len(messages))
	for i, msg := range messages {
		payload := json.RawMessage(append([]byte(nil), msg.Payload...))
		out[i] = httpMessage{
			Type:         msg.Type,
			From:         msg.From,
			AppPublicKey: msg.AppPublicKey,
			Payload:      &payload,
		}
	}
	writeHTTPJSON(w, http.StatusOK, messagesResponse{Messages: out})
}

func (h *HTTPHandler) handleOffer(w http.ResponseWriter, r *http.Request, channelID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req postOfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	if err := h.store.PostMessage(channelID, req.ChannelSecret, Message{
		Type:         MessageOffer,
		From:         strings.TrimSpace(req.From),
		AppPublicKey: strings.TrimSpace(req.AppPublicKey),
		Payload: mustMarshalSignalingEnvelope(map[string]json.RawMessage{
			"app_certificate": req.AppCertificate,
			"offer":           req.Offer,
			"signature":       req.Signature,
		}),
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"accepted": true})
}

func (h *HTTPHandler) handleAnswer(w http.ResponseWriter, r *http.Request, channelID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req postAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	if err := h.store.PostMessage(channelID, req.ChannelSecret, Message{
		Type:         MessageAnswer,
		From:         strings.TrimSpace(req.From),
		AppPublicKey: strings.TrimSpace(req.AppPublicKey),
		Payload: mustMarshalSignalingEnvelope(map[string]json.RawMessage{
			"app_certificate": req.AppCertificate,
			"answer":          req.Answer,
			"signature":       req.Signature,
		}),
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"accepted": true})
}

func (h *HTTPHandler) handleCandidate(w http.ResponseWriter, r *http.Request, channelID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req postCandidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	if err := h.store.PostMessage(channelID, req.ChannelSecret, Message{
		Type:         MessageCandidate,
		From:         strings.TrimSpace(req.From),
		AppPublicKey: strings.TrimSpace(req.AppPublicKey),
		Payload:      append([]byte(nil), req.Candidate...),
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"accepted": true})
}

func mustMarshalSignalingEnvelope(fields map[string]json.RawMessage) []byte {
	out := make(map[string]json.RawMessage)
	for key, value := range fields {
		if len(value) > 0 && string(value) != "null" {
			out[key] = append([]byte(nil), value...)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return []byte("{}")
	}
	return data
}

func rendezvousAuthorization(header, channelID string) (string, bool) {
	const prefix = "Rendezvous "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	id, secret, ok := strings.Cut(token, ":")
	if !ok {
		return "", false
	}
	if strings.TrimSpace(id) != channelID || strings.TrimSpace(secret) == "" {
		return "", false
	}
	return strings.TrimSpace(secret), true
}

func writeStoreError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid channel secret"):
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized", msg)
	case strings.Contains(msg, "channel expired"):
		writeHTTPError(w, http.StatusGone, "rendezvous_channel_expired", msg)
	case strings.Contains(msg, "channel not found"):
		writeHTTPError(w, http.StatusNotFound, "not_found", msg)
	default:
		writeHTTPError(w, http.StatusBadRequest, "rendezvous_message_rejected", msg)
	}
}

func writeHTTPJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeHTTPError(w http.ResponseWriter, status int, code string, message string) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(status)
	}
	writeHTTPJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":       code,
			"message":    message,
			"request_id": newHTTPRequestID(),
		},
	})
}

func newHTTPRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "rv_error"
	}
	return "req_" + hex.EncodeToString(b[:])
}
