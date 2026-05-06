package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

type Config struct {
	Cloud            *cloud.Service
	Registry         *registry.Registry
	ICE              *ice.Service
	ICEServers       []hubv1.RTCIceServerConfig
	HubID            string
	InternalSecret   string
	DebugToken       string
	Clock            Clock
	AnswerTimeout    time.Duration
	PollInterval     time.Duration
	MaxBodyBytes     int64
	AgentSessionTTL  time.Duration
	MaxAgentSessions int
	KickTTL          time.Duration
	AllowedOrigins   []string
	LocalDiscovery   bool
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type Handler struct {
	router          http.Handler
	registry        *registry.Registry
	cloud           *cloud.Service
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
			if h.cloud != nil {
				h.cloud.CleanupExpired(ctx)
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
	kickTTL := cfg.KickTTL
	if kickTTL <= 0 {
		kickTTL = time.Minute
	}
	hubID := strings.TrimSpace(cfg.HubID)
	if hubID == "" {
		hubID = "termx-hub-local"
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
	if cfg.LocalDiscovery {
		mux.HandleFunc("GET /api/v1/agents/online", func(w http.ResponseWriter, r *http.Request) {
			if cfg.Registry == nil {
				writeError(w, http.StatusServiceUnavailable, "registry_unavailable", "agent registry is not configured")
				return
			}
			agents := cfg.Registry.Agents()
			out := make([]map[string]any, 0, len(agents))
			for _, agent := range agents {
				terminals := make([]map[string]any, 0, len(agent.Terminals))
				for _, terminal := range agent.Terminals {
					terminals = append(terminals, map[string]any{
						"terminal_id": terminal.ID,
						"title":       terminal.ID,
						"state":       terminal.State,
					})
				}
				out = append(out, map[string]any{
					"agent_id":     agent.ID,
					"machine_id":   agent.MachineID,
					"machine_name": agent.MachineID,
					"status":       agent.Status,
					"last_seen_at": agent.LastSeenAt.Format(time.RFC3339Nano),
					"terminals":    terminals,
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"agents": out,
			})
		})
	}
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
			MachineID: deviceID,
			AgentID:   agentID,
			Terminals: terminals,
		}); err != nil {
			writeError(w, http.StatusForbidden, "agent_register_failed", err.Error())
			return
		}
		sessionID := agents.put(agentSession{AgentID: agentID, DeviceID: deviceID}, clock.Now().UTC(), agentSessionTTL, maxAgentSessions)
		writeJSON(w, http.StatusOK, map[string]any{
			"version":                    "remote.hub.v1",
			"hub_id":                     hubID,
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
	mux.HandleFunc("POST /api/internal/kick", func(w http.ResponseWriter, r *http.Request) {
		if !authorizedInternalRequest(r, cfg.InternalSecret) {
			writeError(w, http.StatusForbidden, "internal_unauthorized", "valid hub secret is required")
			return
		}
		var req struct {
			AgentID   string `json:"agent_id"`
			MachineID string `json:"machine_id"`
			Reason    string `json:"reason"`
		}
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		agentID := strings.TrimSpace(req.AgentID)
		machineID := strings.TrimSpace(req.MachineID)
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "invalid_kick_request", "agent_id is required")
			return
		}
		if cfg.Registry == nil {
			writeError(w, http.StatusServiceUnavailable, "registry_unavailable", "agent registry is not configured")
			return
		}
		kicked := true
		if machineID == "" {
			agent, ok := cfg.Registry.GetAgent(agentID)
			if !ok {
				kicked = false
			} else {
				machineID = agent.MachineID
			}
		}
		if machineID != "" {
			reason := strings.TrimSpace(req.Reason)
			if reason == "" {
				reason = "control requested"
			}
			cfg.Registry.ForceOffline(registry.ForceOfflineInput{
				MachineID: machineID,
				AgentID:   agentID,
				Reason:    reason,
				TTL:       kickTTL,
			})
		}
		agents.deleteByAgentID(agentID)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"kicked":     kicked,
			"agent_id":   agentID,
			"machine_id": machineID,
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
		if err := cfg.Registry.Heartbeat(r.Context(), registry.HeartbeatInput{
			AgentID:   session.AgentID,
			MachineID: session.DeviceID,
			Terminals: registryTerminalsFromHub(req.Terminals),
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
		if cfg.Cloud == nil {
			writeError(w, http.StatusServiceUnavailable, "cloud_service_unavailable", "cloud service is not configured")
			return
		}
		session, ok := agents.get(req.AgentSessionID, req.DeviceID, clock.Now().UTC(), agentSessionTTL)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_agent_session", "agent session is invalid")
			return
		}
		timeout := time.Duration(req.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		if timeout > 20*time.Second {
			timeout = 20 * time.Second
		}
		offer, err := cfg.Cloud.PollAgentOffer(r.Context(), cloud.PollAgentOfferInput{
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
			writeError(w, http.StatusForbidden, "poll_cloud_offer_failed", err.Error())
			return
		}
		publicID := publicSessionID(offer)
		agents.rememberPoll(req.AgentSessionID, publicID)
		agents.rememberOffer(req.AgentSessionID, publicID, offer.ID)
		if _, err := rtcConfigForOffer(r.Context(), cfg, offer); err != nil {
			agents.rememberPollError(req.AgentSessionID, err.Error())
			writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, hubv1.SignalingPollResponse{
			Offer: &hubv1.SignalingOffer{
				SessionID:    publicID,
				MachineID:    offer.MachineID,
				TerminalID:   offer.TerminalID,
				SDP:          offer.SDP,
				Candidates:   append([]string(nil), offer.ICECandidates...),
				SessionToken: offer.SessionToken,
			},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/signaling/answer", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.SubmitSignalingAnswerRequest
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Cloud == nil {
			writeError(w, http.StatusServiceUnavailable, "cloud_service_unavailable", "cloud service is not configured")
			return
		}
		session, ok := agents.get(req.AgentSessionID, req.DeviceID, clock.Now().UTC(), agentSessionTTL)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_agent_session", "agent session is invalid")
			return
		}
		offerID := agents.resolveOfferID(req.AgentSessionID, req.Answer.SessionID)
		if offerID == "" {
			offerID = req.Answer.SessionID
		}
		if err := cfg.Cloud.SubmitAnswer(r.Context(), cloud.SubmitAnswerInput{
			AgentID:   session.AgentID,
			MachineID: session.DeviceID,
			OfferID:   offerID,
			SDP:       req.Answer.SDP,
			Error:     req.Answer.Error,
		}); err != nil {
			agents.rememberAnswer(req.AgentSessionID, req.Answer.SessionID, err.Error())
			writeError(w, http.StatusForbidden, "submit_cloud_answer_failed", err.Error())
			return
		}
		agents.rememberAnswer(req.AgentSessionID, req.Answer.SessionID, "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/agents/pairing/poll", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.PairingPollRequest
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
		timeout := time.Duration(req.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		if timeout > 20*time.Second {
			timeout = 20 * time.Second
		}
		claim, err := cfg.Registry.PollPairingClaim(r.Context(), registry.PairingPollInput{
			AgentID:   session.AgentID,
			MachineID: session.DeviceID,
			Timeout:   timeout,
		})
		if err != nil {
			if errors.Is(err, registry.ErrPollTimeout) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeError(w, http.StatusForbidden, "poll_pairing_claim_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, hubv1.PairingPollResponse{
			Claim: &hubv1.PairingClaim{
				ClaimID:               claim.ID,
				MachineID:             claim.MachineID,
				PairSessionID:         claim.PairSessionID,
				PairSecret:            claim.PairSecret,
				AppDeviceID:           claim.AppDeviceID,
				AppName:               claim.AppName,
				RequestedCapabilities: append([]string(nil), claim.RequestedCapabilities...),
			},
		})
	})
	mux.HandleFunc("POST /api/v1/agents/pairing/result", func(w http.ResponseWriter, r *http.Request) {
		var req hubv1.SubmitPairingResultRequest
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
		if _, err := cfg.Registry.SubmitPairingResult(r.Context(), registry.PairingResultInput{
			AgentID:      session.AgentID,
			MachineID:    session.DeviceID,
			ClaimID:      req.Result.ClaimID,
			MachineName:  req.Result.MachineName,
			SessionToken: req.Result.SessionToken,
			ExpiresAt:    req.Result.ExpiresAt,
			Error:        req.Result.Error,
		}); err != nil {
			writeError(w, http.StatusForbidden, "submit_pairing_result_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id,omitempty"`
			Offer      struct {
				SessionID  string   `json:"session_id"`
				SDP        string   `json:"sdp"`
				Candidates []string `json:"ice_candidates,omitempty"`
			} `json:"offer"`
			SessionToken string `json:"session_token"`
		}
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Cloud == nil {
			writeError(w, http.StatusServiceUnavailable, "cloud_service_unavailable", "cloud service is not configured")
			return
		}
		if strings.TrimSpace(req.SessionToken) == "" {
			writeError(w, http.StatusBadRequest, "session_token_required", "session_token is required")
			return
		}
		offer, err := cfg.Cloud.SubmitOffer(r.Context(), cloud.SubmitOfferInput{
			SessionID:     req.Offer.SessionID,
			MachineID:     req.MachineID,
			TerminalID:    req.TerminalID,
			SDP:           req.Offer.SDP,
			ICECandidates: req.Offer.Candidates,
			SessionToken:  req.SessionToken,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "submit_cloud_offer_failed", err.Error())
			return
		}
		answer, err := waitForAnswer(r.Context(), cfg, offer.ID, offer.MachineID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"session_id":   publicSessionID(offer),
					"path":         cloud.PathCloud,
					"machine_id":   offer.MachineID,
					"terminal_id":  offer.TerminalID,
					"pending":      true,
					"relay_policy": relayPolicyResponse(offer.AllowRelay),
				})
				return
			}
			writeError(w, statusForAnswerError(err), "get_cloud_answer_failed", err.Error())
			return
		}
		if _, err := iceServersForLease(r.Context(), cfg, offer.ID, offer.AllowRelay); err != nil {
			writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
			return
		}
		writeSessionAnswer(w, r.Context(), cfg, publicSessionID(offer), offer.MachineID, offer.TerminalID, offer.AllowRelay, offer.ID, answer)
	})
	mux.HandleFunc("POST /api/v1/sessions/{session_id}/answer", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MachineID string `json:"machine_id"`
		}
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Cloud == nil {
			writeError(w, http.StatusServiceUnavailable, "cloud_service_unavailable", "cloud service is not configured")
			return
		}
		answer, err := cfg.Cloud.GetAnswer(r.Context(), cloud.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			MachineID: req.MachineID,
		})
		if err != nil {
			if errors.Is(err, registry.ErrOfferNotFound) {
				if policy, ok := cfg.Cloud.OfferPolicyForAnswer(cloud.GetAnswerInput{
					OfferID:   r.PathValue("session_id"),
					MachineID: req.MachineID,
				}); ok {
					writeJSON(w, http.StatusAccepted, map[string]any{
						"session_id":   strings.TrimSpace(r.PathValue("session_id")),
						"path":         cloud.PathCloud,
						"machine_id":   policy.MachineID,
						"terminal_id":  policy.TerminalID,
						"pending":      true,
						"relay_policy": relayPolicyResponse(policy.AllowRelay),
					})
					return
				}
			}
			writeError(w, http.StatusForbidden, "get_cloud_answer_failed", err.Error())
			return
		}
		policy, ok := cfg.Cloud.OfferPolicyForAnswer(cloud.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			MachineID: req.MachineID,
		})
		allowRelay := ok && policy.AllowRelay
		if _, err := iceServersForLease(r.Context(), cfg, r.PathValue("session_id"), allowRelay); err != nil {
			writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
			return
		}
		terminalID := ""
		if ok {
			terminalID = policy.TerminalID
		}
		writeSessionAnswer(w, r.Context(), cfg, r.PathValue("session_id"), answer.MachineID, terminalID, allowRelay, r.PathValue("session_id"), answer)
	})
	mux.HandleFunc("POST /api/v1/pairing/claims", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MachineID             string   `json:"machine_id"`
			PairSessionID         string   `json:"pair_session_id"`
			PairSecret            string   `json:"pair_secret"`
			AppDeviceID           string   `json:"app_device_id"`
			AppName               string   `json:"app_name"`
			RequestedCapabilities []string `json:"requested_capabilities"`
		}
		if err := decodeJSON(w, r, maxBodyBytes, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if cfg.Registry == nil {
			writeError(w, http.StatusServiceUnavailable, "registry_unavailable", "agent registry is not configured")
			return
		}
		claim, err := cfg.Registry.SubmitPairingClaim(r.Context(), registry.PairingClaimInput{
			MachineID:             req.MachineID,
			PairSessionID:         req.PairSessionID,
			PairSecret:            req.PairSecret,
			AppDeviceID:           req.AppDeviceID,
			AppName:               req.AppName,
			RequestedCapabilities: req.RequestedCapabilities,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "submit_pairing_claim_failed", err.Error())
			return
		}
		result, err := waitForPairingResult(r.Context(), cfg, claim.ID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"claim_id":   claim.ID,
					"machine_id": claim.MachineID,
					"pending":    true,
				})
				return
			}
			writeError(w, statusForAnswerError(err), "get_pairing_result_failed", err.Error())
			return
		}
		if strings.TrimSpace(result.Error) != "" {
			writeError(w, http.StatusForbidden, "pairing_rejected", result.Error)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"claim_id":      result.ClaimID,
			"machine_id":    result.MachineID,
			"machine_name":  result.MachineName,
			"session_token": result.SessionToken,
			"expires_at":    result.ExpiresAt,
		})
	})
	return &Handler{
		router:          corsMiddleware(mux, cfg.AllowedOrigins),
		registry:        cfg.Registry,
		cloud:           cfg.Cloud,
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

func (s *agentSessions) deleteByAgentID(agentID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	agentID = strings.TrimSpace(agentID)
	for id, session := range s.sessions {
		if session.AgentID == agentID {
			s.deleteLocked(id)
			removed++
		}
	}
	return removed
}

func registryTerminalsFromHub(in []hubv1.HubTerminalInventoryItem) []registry.Terminal {
	out := make([]registry.Terminal, 0, len(in))
	for _, terminal := range in {
		if strings.TrimSpace(terminal.ID) == "" {
			continue
		}
		out = append(out, registry.Terminal{
			ID:    terminal.ID,
			State: terminal.State,
		})
	}
	return out
}

func waitForAnswer(ctx context.Context, cfg Config, offerID string, machineID string) (cloud.Answer, error) {
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
		answer, err := cfg.Cloud.GetAnswer(ctx, cloud.GetAnswerInput{
			OfferID:   offerID,
			MachineID: machineID,
		})
		if err == nil {
			return answer, nil
		}
		if !errors.Is(err, registry.ErrOfferNotFound) {
			return cloud.Answer{}, err
		}
		select {
		case <-ctx.Done():
			return cloud.Answer{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForPairingResult(ctx context.Context, cfg Config, claimID string) (registry.PairingResult, error) {
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
		result, err := cfg.Registry.GetPairingResult(ctx, claimID)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, registry.ErrPairingClaimNotFound) {
			return registry.PairingResult{}, err
		}
		select {
		case <-ctx.Done():
			return registry.PairingResult{}, ctx.Err()
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

func publicSessionID(offer cloud.Offer) string {
	if strings.TrimSpace(offer.SessionID) != "" {
		return strings.TrimSpace(offer.SessionID)
	}
	return offer.ID
}

func writeSessionAnswer(w http.ResponseWriter, ctx context.Context, cfg Config, sessionID string, machineID string, terminalID string, allowRelay bool, leaseID string, answer cloud.Answer) {
	if strings.TrimSpace(answer.Error) != "" {
		writeError(w, http.StatusForbidden, "cloud_answer_error", answer.Error)
		return
	}
	iceServers, err := iceServersForLease(ctx, cfg, leaseID, allowRelay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
		return
	}
	response := map[string]any{
		"session_id": sessionID,
		"path":       cloud.PathCloud,
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

func rtcConfigForOffer(ctx context.Context, cfg Config, offer cloud.Offer) (hubv1.RTCConfig, error) {
	servers, err := iceServersForLease(ctx, cfg, offer.ID, offer.AllowRelay)
	if err != nil {
		return hubv1.RTCConfig{}, err
	}
	return hubv1.RTCConfig{IceServers: servers}, nil
}

func iceServersForLease(ctx context.Context, cfg Config, leaseID string, allowRelay bool) ([]hubv1.RTCIceServerConfig, error) {
	if cfg.ICE == nil {
		return cloneICEServers(cfg.ICEServers), nil
	}
	rtc, err := cfg.ICE.ConfigForLease(ctx, ice.Lease{
		ID:         leaseID,
		Path:       ice.PathCloud,
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

func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	_ = allowedOrigins
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-TermX-Debug-Token, X-TermX-Hub-Secret, X-Hub-Secret")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authorizedInternalRequest(r *http.Request, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-TermX-Hub-Secret"))
	if provided == "" {
		provided = strings.TrimSpace(r.Header.Get("X-Hub-Secret"))
	}
	if provided == "" || len(provided) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}
