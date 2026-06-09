package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	InternalSecret   string
	Clock            Clock
	AnswerTimeout    time.Duration
	PollInterval     time.Duration
	MaxBodyBytes     int64
	KickTTL          time.Duration
	AllowedOrigins   []string
	LocalDiscovery   bool
	PairingRateLimit PairingRateLimitConfig
}

type PairingRateLimitConfig struct {
	Window     time.Duration
	PerIP      int
	PerMachine int
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type Handler struct {
	router   http.Handler
	registry *registry.Registry
	cloud    *cloud.Service
	clock    Clock
}

var answerProofChallengeGenerator = randomAnswerProofChallenge

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
		case _, ok := <-ticks:
			if !ok {
				return
			}
			if h.registry != nil {
				h.registry.CleanupExpired(ctx)
			}
			if h.cloud != nil {
				h.cloud.CleanupExpired(ctx)
			}
		}
	}
}

type pairingRateLimiter struct {
	mu         sync.Mutex
	clock      Clock
	window     time.Duration
	perIP      int
	perMachine int
	buckets    map[string]rateBucket
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

func newPairingRateLimiter(cfg PairingRateLimitConfig, clock Clock) *pairingRateLimiter {
	window := cfg.Window
	if window <= 0 {
		window = time.Minute
	}
	perIP := cfg.PerIP
	if perIP <= 0 {
		perIP = 60
	}
	perMachine := cfg.PerMachine
	if perMachine <= 0 {
		perMachine = 20
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &pairingRateLimiter{
		clock:      clock,
		window:     window,
		perIP:      perIP,
		perMachine: perMachine,
		buckets:    map[string]rateBucket{},
	}
}

func (l *pairingRateLimiter) Allow(ip, machineID string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	if ip = strings.TrimSpace(ip); ip != "" {
		if !l.allowKeyLocked(now, "ip:"+ip, l.perIP) {
			return false
		}
	}
	if machineID = strings.TrimSpace(machineID); machineID != "" {
		if !l.allowKeyLocked(now, "machine:"+machineID, l.perMachine) {
			return false
		}
	}
	return true
}

func (l *pairingRateLimiter) allowKeyLocked(now time.Time, key string, limit int) bool {
	bucket := l.buckets[key]
	if bucket.resetAt.IsZero() || !now.Before(bucket.resetAt) {
		bucket = rateBucket{resetAt: now.Add(l.window)}
	}
	if bucket.count >= limit {
		l.buckets[key] = bucket
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func NewHandler(cfg Config) http.Handler {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	kickTTL := cfg.KickTTL
	if kickTTL <= 0 {
		kickTTL = time.Minute
	}
	maxBodyBytes := cfg.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 64 * 1024
	}
	pairingRateLimiter := newPairingRateLimiter(cfg.PairingRateLimit, clock)
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
					title := strings.TrimSpace(terminal.Name)
					if title == "" {
						title = terminal.ID
					}
					terminals = append(terminals, map[string]any{
						"terminal_id": terminal.ID,
						"title":       title,
						"name":        title,
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
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"kicked":     kicked,
			"agent_id":   agentID,
			"machine_id": machineID,
		})
	})
	mux.HandleFunc("POST /api/v1/sessions/ice", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MachineID    string `json:"machine_id"`
			TerminalID   string `json:"terminal_id,omitempty"`
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
		preflight, err := cfg.Cloud.PreflightSession(r.Context(), cloud.PreflightSessionInput{
			MachineID:    req.MachineID,
			TerminalID:   req.TerminalID,
			Path:         sessionPathForPublicAPI(cfg),
			SessionToken: req.SessionToken,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "preflight_cloud_session_failed", err.Error())
			return
		}
		leaseID := preflightICELeaseID(preflight.MachineID)
		iceServers, err := iceServersForLease(r.Context(), cfg, leaseID, preflight.Path, preflight.AllowRelay)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
			return
		}
		response := map[string]any{
			"path":         preflight.Path,
			"machine_id":   preflight.MachineID,
			"ice_servers":  iceServers,
			"relay_policy": relayPolicyResponse(preflight.AllowRelay, preflight.AllowRelayTransfer),
		}
		if strings.TrimSpace(preflight.TerminalID) != "" {
			response["terminal_id"] = strings.TrimSpace(preflight.TerminalID)
		}
		writeJSON(w, http.StatusOK, response)
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
			SessionToken         string `json:"session_token"`
			AnswerProofChallenge string `json:"answer_proof_challenge"`
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
		challenge := strings.TrimSpace(req.AnswerProofChallenge)
		if challenge == "" {
			challenge = answerProofChallengeGenerator()
		}
		offer, err := cfg.Cloud.SubmitOffer(r.Context(), cloud.SubmitOfferInput{
			SessionID:            req.Offer.SessionID,
			MachineID:            req.MachineID,
			TerminalID:           req.TerminalID,
			Path:                 sessionPathForPublicAPI(cfg),
			SDP:                  req.Offer.SDP,
			ICECandidates:        req.Offer.Candidates,
			SessionToken:         req.SessionToken,
			AnswerProofChallenge: challenge,
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
					"path":         offer.Path,
					"machine_id":   offer.MachineID,
					"terminal_id":  offer.TerminalID,
					"pending":      true,
					"relay_policy": relayPolicyResponse(offer.AllowRelay, offer.AllowRelayTransfer),
				})
				return
			}
			writeError(w, statusForAnswerError(err), "get_cloud_answer_failed", err.Error())
			return
		}
		if _, err := iceServersForLease(r.Context(), cfg, offer.ID, offer.Path, offer.AllowRelay); err != nil {
			writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
			return
		}
		writeSessionAnswer(w, r.Context(), cfg, publicSessionID(offer), offer.Path, offer.MachineID, offer.TerminalID, offer.AllowRelay, offer.AllowRelayTransfer, offer.ID, answer)
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
		policy, hasPolicy := cfg.Cloud.OfferPolicyForAnswer(cloud.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			MachineID: req.MachineID,
		})
		answer, err := cfg.Cloud.GetAnswer(r.Context(), cloud.GetAnswerInput{
			OfferID:   r.PathValue("session_id"),
			MachineID: req.MachineID,
		})
		if err != nil {
			if errors.Is(err, registry.ErrOfferNotFound) {
				if hasPolicy {
					writeJSON(w, http.StatusAccepted, map[string]any{
						"session_id":   strings.TrimSpace(r.PathValue("session_id")),
						"path":         policy.Path,
						"machine_id":   policy.MachineID,
						"terminal_id":  policy.TerminalID,
						"pending":      true,
						"relay_policy": relayPolicyResponse(policy.AllowRelay, policy.AllowRelayTransfer),
					})
					return
				}
			}
			writeError(w, http.StatusForbidden, "get_cloud_answer_failed", err.Error())
			return
		}
		allowRelay := hasPolicy && policy.AllowRelay
		allowRelayTransfer := hasPolicy && policy.AllowRelayTransfer
		path := cloud.PathHub
		if hasPolicy {
			path = policy.Path
		}
		if _, err := iceServersForLease(r.Context(), cfg, r.PathValue("session_id"), path, allowRelay); err != nil {
			writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
			return
		}
		terminalID := ""
		if hasPolicy {
			terminalID = policy.TerminalID
		}
		writeSessionAnswer(w, r.Context(), cfg, r.PathValue("session_id"), path, answer.MachineID, terminalID, allowRelay, allowRelayTransfer, r.PathValue("session_id"), answer)
	})
	mux.HandleFunc("POST /api/v1/pairing/claims", func(w http.ResponseWriter, r *http.Request) {
		handlePairingClaim(w, r, cfg, maxBodyBytes, pairingRateLimiter)
	})
	return &Handler{
		router:   corsMiddleware(mux, cfg.AllowedOrigins),
		registry: cfg.Registry,
		cloud:    cfg.Cloud,
		clock:    clock,
	}
}

func handlePairingClaim(w http.ResponseWriter, r *http.Request, cfg Config, maxBodyBytes int64, limiter *pairingRateLimiter) {
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
	if limiter != nil && !limiter.Allow(clientIP(r), req.MachineID) {
		writeError(w, http.StatusTooManyRequests, "pairing_rate_limited", "too many pairing attempts; try again later")
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
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if comma := strings.IndexByte(forwarded, ','); comma >= 0 {
			forwarded = forwarded[:comma]
		}
		return strings.TrimSpace(forwarded)
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func pollUntilReady[T any](ctx context.Context, timeout, interval time.Duration, fetch func(context.Context) (T, error), notFoundErr error) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		result, err := fetch(ctx)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, notFoundErr) {
			var zero T
			return zero, err
		}
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-ticker.C:
		}
	}
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
	return pollUntilReady(ctx, timeout, interval, func(ctx context.Context) (cloud.Answer, error) {
		return cfg.Cloud.GetAnswer(ctx, cloud.GetAnswerInput{
			OfferID:   offerID,
			MachineID: machineID,
		})
	}, registry.ErrOfferNotFound)
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
	return pollUntilReady(ctx, timeout, interval, func(ctx context.Context) (registry.PairingResult, error) {
		return cfg.Registry.GetPairingResult(ctx, claimID)
	}, registry.ErrPairingClaimNotFound)
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

func preflightICELeaseID(machineID string) string {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return "browser-preflight"
	}
	replacer := strings.NewReplacer(":", "_", " ", "_", "\t", "_", "\n", "_", "\r", "_")
	return "browser-" + replacer.Replace(machineID)
}

func writeSessionAnswer(w http.ResponseWriter, ctx context.Context, cfg Config, sessionID string, path string, machineID string, terminalID string, allowRelay bool, allowRelayTransfer bool, leaseID string, answer cloud.Answer) {
	if strings.TrimSpace(answer.Error) != "" {
		writeError(w, http.StatusForbidden, "cloud_answer_error", answer.Error)
		return
	}
	path = normalizeSessionPath(path)
	iceServers, err := iceServersForLease(ctx, cfg, leaseID, path, allowRelay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cloud_ice_config_failed", err.Error())
		return
	}
	response := map[string]any{
		"session_id": sessionID,
		"path":       path,
		"machine_id": machineID,
		"answer": map[string]any{
			"sdp":            answer.SDP,
			"ice_candidates": []string{},
			"answer_proof":   answer.AnswerProof,
		},
		"ice_servers":  iceServers,
		"relay_policy": relayPolicyResponse(allowRelay, allowRelayTransfer),
		"relay_in_use": answer.RelayInUse,
	}
	if strings.TrimSpace(terminalID) != "" {
		response["terminal_id"] = strings.TrimSpace(terminalID)
	}
	writeJSON(w, http.StatusOK, response)
}

func iceServersForLease(ctx context.Context, cfg Config, leaseID string, path string, allowRelay bool) ([]hubv1.RTCIceServerConfig, error) {
	path = normalizeSessionPath(path)
	if path == cloud.PathLocal {
		return cloneICEServers(cfg.ICEServers), nil
	}
	if cfg.ICE == nil {
		return cloneICEServers(cfg.ICEServers), nil
	}
	rtc, err := cfg.ICE.ConfigForLease(ctx, ice.Lease{
		ID:         leaseID,
		Path:       ice.PathHub,
		AllowRelay: allowRelay,
	})
	if err != nil {
		return nil, err
	}
	return hubIceServers(rtc.ICEServers), nil
}

func sessionPathForPublicAPI(cfg Config) string {
	if cfg.LocalDiscovery {
		return cloud.PathLocal
	}
	return cloud.PathHub
}

func normalizeSessionPath(path string) string {
	switch strings.TrimSpace(path) {
	case cloud.PathLocal:
		return cloud.PathLocal
	default:
		return cloud.PathHub
	}
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

func relayPolicyResponse(allowRelay bool, allowRelayTransfer bool) map[string]any {
	return map[string]any{
		"allow_relay":          allowRelay,
		"allow_relay_transfer": allowRelayTransfer,
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
	allowed := normalizeAllowedOrigins(allowedOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := allowedOrigin(r.Header.Get("Origin"), allowed); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
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

func normalizeAllowedOrigins(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		allowed[origin] = struct{}{}
	}
	return allowed
}

func allowedOrigin(origin string, allowed map[string]struct{}) string {
	origin = strings.TrimSpace(origin)
	if len(allowed) == 0 {
		return "*"
	}
	if origin == "" {
		return ""
	}
	if _, ok := allowed[origin]; ok {
		return origin
	}
	return ""
}

func randomAnswerProofChallenge() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
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
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}
