package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/lozzow/termx/termx-hub/internal/ice"
	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

type Config struct {
	Managed          *managed.Service
	Registry         *registry.Registry
	AgentPolicy      AgentPolicyProvider
	ICE              *ice.Service
	ICEServers       []hubv1.RTCIceServerConfig
	DebugToken       string
	Clock            Clock
	AnswerTimeout    time.Duration
	PollInterval     time.Duration
	MaxBodyBytes     int64
	AgentSessionTTL  time.Duration
	MaxAgentSessions int
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type AgentPolicyProvider interface {
	GetAgentPolicy(context.Context, registry.AgentPolicyRequest) (registry.AgentPolicy, error)
}

type Handler struct {
	router          http.Handler
	registry        *registry.Registry
	managed         *managed.Service
	agents          *agentSessions
	clock           Clock
	agentSessionTTL time.Duration
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) StartCleanup(ctx context.Context, ticks <-chan time.Time) {
	if h == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-ticks:
			if !ok {
				return
			}
			if h.registry != nil {
				h.registry.CleanupExpired(ctx)
			}
			if h.managed != nil {
				h.managed.CleanupExpired(ctx)
			}
			if h.agents != nil {
				h.agents.cleanupExpired(tick.UTC())
			}
		}
	}
}

func NewHandler(cfg Config) http.Handler {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	agentSessionTTL := cfg.AgentSessionTTL
	if agentSessionTTL <= 0 {
		agentSessionTTL = 10 * time.Minute
	}
	maxAgentSessions := cfg.MaxAgentSessions
	if maxAgentSessions <= 0 {
		maxAgentSessions = 4096
	}
	maxBodyBytes := cfg.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 64 * 1024
	}
	agents := newAgentSessions()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service":   "termx-hub",
			"status":    "ok",
			"runtime":   "hub-signaling",
			"transport": "signaling-control-only",
		})
	})
	mux.HandleFunc("GET /api/debug/agents", func(w http.ResponseWriter, r *http.Request) {
		debugToken := strings.TrimSpace(cfg.DebugToken)
		if debugToken == "" || r.Header.Get("X-TermX-Debug-Token") != debugToken {
			writeError(w, http.StatusUnauthorized, "debug_unauthorized", "debug token is required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"agents": agents.snapshot(clock.Now().UTC()),
		})
	})
	mux.HandleFunc("POST /api/v1/agents/register", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.HubRegisterRequest
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Registry == nil {
			writeError(w, http.StatusServiceUnavailable, "registry_unavailable", "agent registry is not configured")
			return
		}
		deviceID := strings.TrimSpace(req.DeviceID)
		if deviceID == "" {
			writeError(w, http.StatusBadRequest, "invalid_agent_registration", "device_id is required")
			return
		}
		agentID := strings.TrimSpace(req.AgentID)
		if agentID == "" {
			agentID = randomID("agent")
		}
		terminals := make([]registry.Terminal, 0, len(req.Terminals))
		for _, terminal := range req.Terminals {
			if strings.TrimSpace(terminal.ID) == "" {
				continue
			}
			terminals = append(terminals, registry.Terminal{
				ID:    terminal.ID,
				State: terminal.State,
			})
		}
		if _, err := cfg.Registry.Register(r.Context(), registry.RegisterInput{
			MachineID:          deviceID,
			AgentID:            agentID,
			SignatureAlgorithm: req.Signature.Algorithm,
			SignatureNonce:     req.Signature.Nonce,
			SignatureTimestamp: req.Signature.Timestamp,
			SignatureValue:     req.Signature.Value,
			Terminals:          terminals,
		}); err != nil {
			writeError(w, http.StatusForbidden, "agent_register_failed", err.Error())
			return
		}
		sessionID := agents.put(agentSession{AgentID: agentID, DeviceID: deviceID}, clock.Now().UTC(), agentSessionTTL, maxAgentSessions)
		writeJSON(w, http.StatusOK, map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     "termx-devstack-hub",
			"agent_session_id":           sessionID,
			"heartbeat_interval_seconds": 5,
			"rtc_config": map[string]any{
				"ice_servers": cloneICEServers(cfg.ICEServers),
			},
			"relay_policy": map[string]any{
				"allow_relay":          false,
				"allow_relay_transfer": false,
			},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.HubHeartbeatRequest
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Registry == nil {
			writeError(w, http.StatusServiceUnavailable, "registry_unavailable", "agent registry is not configured")
			return
		}
		session, ok := agents.get(req.AgentSessionID, req.DeviceID, clock.Now().UTC(), agentSessionTTL)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_agent_session", "agent session is invalid")
			return
		}
		if checkAgentPolicy(w, r, cfg, session) {
			return
		}
		if err := cfg.Registry.Heartbeat(r.Context(), registry.HeartbeatInput{
			AgentID:   session.AgentID,
			MachineID: session.DeviceID,
		}); err != nil {
			writeError(w, http.StatusUnauthorized, "agent_heartbeat_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, hubv1.HubHeartbeatResponse{
			Accepted:             true,
			NextHeartbeatSeconds: 5,
		})
	})
	mux.HandleFunc("POST /api/v1/agents/signaling/poll", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.SignalingPollRequest
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Managed == nil {
			writeError(w, http.StatusServiceUnavailable, "managed_service_unavailable", "managed service is not configured")
			return
		}
		session, ok := agents.get(req.AgentSessionID, req.DeviceID, clock.Now().UTC(), agentSessionTTL)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_agent_session", "agent session is invalid")
			return
		}
		if checkAgentPolicy(w, r, cfg, session) {
			return
		}
		timeout := time.Duration(req.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		if timeout > 20*time.Second {
			timeout = 20 * time.Second
		}
		offer, err := cfg.Managed.PollAgentOffer(r.Context(), managed.PollAgentOfferInput{
			AgentID:   session.AgentID,
			MachineID: session.DeviceID,
			Timeout:   timeout,
		})
		if err != nil {
			if errors.Is(err, registry.ErrPollTimeout) {
				agents.rememberPoll(req.AgentSessionID, "")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			agents.rememberPollError(req.AgentSessionID, err.Error())
			writeError(w, http.StatusForbidden, "poll_managed_offer_failed", err.Error())
			return
		}
		publicID := publicSessionID(offer)
		agents.rememberPoll(req.AgentSessionID, publicID)
		agents.rememberOffer(req.AgentSessionID, publicID, offer.ID)
		rtcConfig, err := rtcConfigForOffer(r.Context(), cfg, offer)
		if err != nil {
			agents.rememberPollError(req.AgentSessionID, err.Error())
			writeError(w, http.StatusInternalServerError, "managed_ice_config_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, hubv1.SignalingPollResponse{
			Offer: &hubv1.SignalingOffer{
				SessionID:          publicID,
				TicketID:           offer.TicketID,
				DeviceID:           offer.MachineID,
				TerminalID:         offer.TerminalID,
				SDP:                offer.SDP,
				ICECandidates:      append([]string(nil), offer.ICECandidates...),
				RTCConfig:          rtcConfig,
				AllowRelay:         offer.AllowRelay,
				AllowRelayTransfer: false,
				AppCertificate:     append([]byte(nil), offer.AppCertificate...),
				Signature: hubv1.OfferSignature{
					Algorithm: offer.Signature.Algorithm,
					Nonce:     offer.Signature.Nonce,
					Timestamp: offer.Signature.Timestamp,
					Value:     offer.Signature.Value,
				},
			},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/signaling/answer", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.SubmitSignalingAnswerRequest
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Managed == nil {
			writeError(w, http.StatusServiceUnavailable, "managed_service_unavailable", "managed service is not configured")
			return
		}
		session, ok := agents.get(req.AgentSessionID, req.DeviceID, clock.Now().UTC(), agentSessionTTL)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_agent_session", "agent session is invalid")
			return
		}
		if checkAgentPolicy(w, r, cfg, session) {
			return
		}
		if strings.TrimSpace(req.Answer.Error) != "" {
			agents.rememberAnswer(req.AgentSessionID, req.Answer.SessionID, req.Answer.Error)
			writeError(w, http.StatusForbidden, "managed_answer_error", req.Answer.Error)
			return
		}
		offerID := agents.resolveOfferID(req.AgentSessionID, req.Answer.SessionID)
		if offerID == "" {
			offerID = req.Answer.SessionID
		}
		if err := cfg.Managed.SubmitAnswer(r.Context(), managed.SubmitAnswerInput{
			AgentID:   session.AgentID,
			MachineID: session.DeviceID,
			OfferID:   offerID,
			SDP:       req.Answer.SDP,
		}); err != nil {
			agents.rememberAnswer(req.AgentSessionID, req.Answer.SessionID, err.Error())
			writeError(w, http.StatusForbidden, "submit_managed_answer_failed", err.Error())
			return
		}
		agents.rememberAnswer(req.AgentSessionID, req.Answer.SessionID, "")
		w.WriteHeader(http.StatusNoContent)
	})
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
					"session_id":   publicSessionID(offer),
					"path":         managed.PathManaged,
					"machine_id":   offer.MachineID,
					"terminal_id":  offer.TerminalID,
					"pending":      true,
					"relay_policy": relayPolicyResponse(offer.AllowRelay),
				})
				return
			}
			writeError(w, statusForAnswerError(err), "get_managed_answer_failed", err.Error())
			return
		}
		if _, err := iceServersForTicket(r.Context(), cfg, offer.TicketID, offer.AllowRelay); err != nil {
			writeError(w, http.StatusInternalServerError, "managed_ice_config_failed", err.Error())
			return
		}
		if _, err := cfg.Managed.ConsumeOfferTicket(r.Context(), managed.GetAnswerInput{
			OfferID:   offer.ID,
			TicketID:  offer.TicketID,
			MachineID: offer.MachineID,
		}); err != nil {
			writeError(w, http.StatusForbidden, "consume_managed_ticket_failed", err.Error())
			return
		}
		writeSessionAnswer(w, r.Context(), cfg, publicSessionID(offer), offer.MachineID, offer.TerminalID, offer.AllowRelay, offer.TicketID, answer)
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
		policy, ok := cfg.Managed.OfferPolicyForAnswer(managed.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			TicketID:  req.ConnectTicket,
			MachineID: req.MachineID,
		})
		allowRelay := ok && policy.Ticket.AllowRelay
		if _, err := iceServersForTicket(r.Context(), cfg, req.ConnectTicket, allowRelay); err != nil {
			writeError(w, http.StatusInternalServerError, "managed_ice_config_failed", err.Error())
			return
		}
		if _, err := cfg.Managed.ConsumeOfferTicket(r.Context(), managed.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			TicketID:  req.ConnectTicket,
			MachineID: req.MachineID,
		}); err != nil {
			writeError(w, http.StatusForbidden, "consume_managed_ticket_failed", err.Error())
			return
		}
		writeSessionAnswer(w, r.Context(), cfg, r.PathValue("session_id"), answer.MachineID, "", allowRelay, req.ConnectTicket, answer)
	})
	return &Handler{
		router:          mux,
		registry:        cfg.Registry,
		managed:         cfg.Managed,
		agents:          agents,
		clock:           clock,
		agentSessionTTL: agentSessionTTL,
	}
}

type agentSession struct {
	AgentID    string
	DeviceID   string
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

type agentSessionStats struct {
	PollCount           int
	AnswerCount         int
	LastOfferSessionID  string
	LastAnswerSessionID string
	LastError           string
}

type agentSessions struct {
	mu       sync.Mutex
	sessions map[string]agentSession
	offers   map[string]string
	stats    map[string]agentSessionStats
}

func newAgentSessions() *agentSessions {
	return &agentSessions{
		sessions: make(map[string]agentSession),
		offers:   make(map[string]string),
		stats:    make(map[string]agentSessionStats),
	}
}

func (s *agentSessions) put(session agentSession, now time.Time, ttl time.Duration, maxSessions int) string {
	id := randomID("agent_session")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	for maxSessions > 0 && len(s.sessions) >= maxSessions {
		s.evictOldestLocked()
	}
	session.LastSeenAt = now
	session.ExpiresAt = now.Add(ttl)
	s.sessions[id] = session
	if _, ok := s.stats[id]; !ok {
		s.stats[id] = agentSessionStats{}
	}
	return id
}

func (s *agentSessions) get(id string, deviceID string, now time.Time, ttl time.Duration) (agentSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(id)
	s.cleanupExpiredLocked(now)
	session, ok := s.sessions[key]
	if !ok || session.DeviceID != strings.TrimSpace(deviceID) || !now.Before(session.ExpiresAt) {
		if ok {
			s.deleteLocked(key)
		}
		return agentSession{}, false
	}
	session.LastSeenAt = now
	session.ExpiresAt = now.Add(ttl)
	s.sessions[key] = session
	return session, true
}

func (s *agentSessions) rememberPoll(agentSessionID string, publicSessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.stats[strings.TrimSpace(agentSessionID)]
	stats.PollCount++
	stats.LastOfferSessionID = strings.TrimSpace(publicSessionID)
	stats.LastError = ""
	s.stats[strings.TrimSpace(agentSessionID)] = stats
}

func (s *agentSessions) rememberPollError(agentSessionID string, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.stats[strings.TrimSpace(agentSessionID)]
	stats.PollCount++
	stats.LastError = strings.TrimSpace(message)
	s.stats[strings.TrimSpace(agentSessionID)] = stats
}

func (s *agentSessions) rememberAnswer(agentSessionID string, publicSessionID string, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.stats[strings.TrimSpace(agentSessionID)]
	stats.AnswerCount++
	stats.LastAnswerSessionID = strings.TrimSpace(publicSessionID)
	stats.LastError = strings.TrimSpace(message)
	s.stats[strings.TrimSpace(agentSessionID)] = stats
}

func (s *agentSessions) rememberOffer(agentSessionID string, publicSessionID string, internalOfferID string) {
	s.mu.Lock()
	s.offers[agentOfferKey(agentSessionID, publicSessionID)] = strings.TrimSpace(internalOfferID)
	s.mu.Unlock()
}

func (s *agentSessions) resolveOfferID(agentSessionID string, publicSessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offers[agentOfferKey(agentSessionID, publicSessionID)]
}

func agentOfferKey(agentSessionID string, publicSessionID string) string {
	return strings.TrimSpace(agentSessionID) + "\x00" + strings.TrimSpace(publicSessionID)
}

func checkAgentPolicy(w http.ResponseWriter, r *http.Request, cfg Config, session agentSession) bool {
	if cfg.AgentPolicy == nil {
		return false
	}
	policy, err := cfg.AgentPolicy.GetAgentPolicy(r.Context(), registry.AgentPolicyRequest{
		MachineID: session.DeviceID,
		AgentID:   session.AgentID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "agent_policy_check_failed", err.Error())
		return true
	}
	if !policy.ForceOffline {
		return false
	}
	if cfg.Registry != nil {
		cfg.Registry.ForceOffline(registry.ForceOfflineInput{
			MachineID: session.DeviceID,
			AgentID:   session.AgentID,
			Reason:    policy.Reason,
			TTL:       time.Minute,
		})
	}
	writeError(w, http.StatusForbidden, "agent_forced_offline", registry.ErrAgentForcedOffline.Error())
	return true
}

func (s *agentSessions) snapshot(now time.Time) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	out := make([]map[string]any, 0, len(s.sessions))
	for sessionID, session := range s.sessions {
		stats := s.stats[sessionID]
		out = append(out, map[string]any{
			"agent_session_id":       sessionID,
			"agent_id":               session.AgentID,
			"machine_id":             session.DeviceID,
			"poll_count":             stats.PollCount,
			"answer_count":           stats.AnswerCount,
			"last_offer_session_id":  stats.LastOfferSessionID,
			"last_answer_session_id": stats.LastAnswerSessionID,
			"last_error":             stats.LastError,
			"expires_at":             session.ExpiresAt,
		})
	}
	return out
}

func (s *agentSessions) cleanupExpired(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.sessions)
	s.cleanupExpiredLocked(now)
	return before - len(s.sessions)
}

func (s *agentSessions) cleanupExpiredLocked(now time.Time) {
	for id, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			s.deleteLocked(id)
		}
	}
}

func (s *agentSessions) evictOldestLocked() {
	var oldestID string
	var oldest time.Time
	for id, session := range s.sessions {
		if oldestID == "" || session.LastSeenAt.Before(oldest) {
			oldestID = id
			oldest = session.LastSeenAt
		}
	}
	if oldestID != "" {
		s.deleteLocked(oldestID)
	}
}

func (s *agentSessions) deleteLocked(id string) {
	delete(s.sessions, id)
	delete(s.stats, id)
	for key := range s.offers {
		if strings.HasPrefix(key, id+"\x00") {
			delete(s.offers, key)
		}
	}
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

func writeSessionAnswer(w http.ResponseWriter, ctx context.Context, cfg Config, sessionID string, machineID string, terminalID string, allowRelay bool, ticketID string, answer managed.Answer) {
	iceServers, err := iceServersForTicket(ctx, cfg, ticketID, allowRelay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "managed_ice_config_failed", err.Error())
		return
	}
	response := map[string]any{
		"session_id": sessionID,
		"path":       managed.PathManaged,
		"machine_id": machineID,
		"answer": map[string]any{
			"sdp":            answer.SDP,
			"ice_candidates": []string{},
		},
		"ice_servers":  iceServers,
		"relay_policy": relayPolicyResponse(allowRelay),
		"relay_in_use": answer.RelayInUse,
	}
	if strings.TrimSpace(terminalID) != "" {
		response["terminal_id"] = strings.TrimSpace(terminalID)
	}
	writeJSON(w, http.StatusOK, response)
}

func rtcConfigForOffer(ctx context.Context, cfg Config, offer managed.Offer) (hubv1.RTCConfig, error) {
	servers, err := iceServersForTicket(ctx, cfg, offer.TicketID, offer.AllowRelay)
	if err != nil {
		return hubv1.RTCConfig{}, err
	}
	return hubv1.RTCConfig{IceServers: servers}, nil
}

func iceServersForTicket(ctx context.Context, cfg Config, ticketID string, allowRelay bool) ([]hubv1.RTCIceServerConfig, error) {
	if cfg.ICE == nil {
		return cloneICEServers(cfg.ICEServers), nil
	}
	rtc, err := cfg.ICE.ConfigForLease(ctx, ice.Lease{
		ID:         ticketID,
		Path:       ice.PathManaged,
		AllowRelay: allowRelay,
	})
	if err != nil {
		return nil, err
	}
	return hubIceServers(rtc.ICEServers), nil
}

func hubIceServers(in []ice.ICEServer) []hubv1.RTCIceServerConfig {
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		if len(server.URLs) == 0 {
			continue
		}
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
}

func relayPolicyResponse(allowRelay bool) map[string]any {
	return map[string]any{
		"allow_relay":          allowRelay,
		"allow_relay_transfer": false,
	}
}

func cloneICEServers(in []hubv1.RTCIceServerConfig) []hubv1.RTCIceServerConfig {
	if len(in) == 0 {
		return []hubv1.RTCIceServerConfig{}
	}
	out := make([]hubv1.RTCIceServerConfig, 0, len(in))
	for _, server := range in {
		if len(server.URLs) == 0 {
			continue
		}
		out = append(out, hubv1.RTCIceServerConfig{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
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

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}
