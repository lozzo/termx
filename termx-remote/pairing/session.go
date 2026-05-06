package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/session/token"
)

const (
	defaultSessionTTL = 5 * time.Minute
	defaultTokenTTL   = 24 * time.Hour
)

var allowedCapabilities = map[string]struct{}{
	"terminal":            {},
	"file_manager":        {},
	"terminal_management": {},
}

type Config struct {
	MachineID       string
	MachineName     string
	MachineSecret   []byte
	DefaultTokenTTL time.Duration
	LocalPairURL    string
	Now             func() time.Time
}

type Manager struct {
	cfg Config

	mu       sync.Mutex
	sessions map[string]*sessionState
}

type Session struct {
	Type              string    `json:"type"`
	MachineID         string    `json:"machine_id"`
	MachineName       string    `json:"machine_name"`
	LocalPairURL      string    `json:"local_pair_url"`
	PairSessionID     string    `json:"pair_session_id"`
	PairSecret        string    `json:"pair_secret"`
	AnswerProofSecret string    `json:"answer_proof_secret,omitempty"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type ClaimRequest struct {
	PairSessionID         string   `json:"pair_session_id"`
	PairSecret            string   `json:"pair_secret"`
	AppDeviceID           string   `json:"app_device_id"`
	AppName               string   `json:"app_name"`
	RequestedCapabilities []string `json:"requested_capabilities"`
}

type ClaimResponse struct {
	MachineID    string    `json:"machine_id"`
	MachineName  string    `json:"machine_name"`
	SessionToken string    `json:"session_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type sessionState struct {
	session  Session
	cfg      Config
	consumed bool
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*sessionState),
	}
}

func (m *Manager) UpdateConfig(cfg Config) error {
	if m == nil {
		return errors.New("pairing manager is nil")
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return nil
}

func (m *Manager) CreateSession(ttl time.Duration) (Session, error) {
	if m == nil {
		return Session{}, errors.New("pairing manager is nil")
	}
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if err := validateConfig(cfg); err != nil {
		return Session{}, err
	}
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	now := nowFromConfig(cfg)
	sessionID, err := randomToken("pair_", 16)
	if err != nil {
		return Session{}, err
	}
	secret, err := randomToken("", 24)
	if err != nil {
		return Session{}, err
	}
	answerProofSecret, err := randomToken("", 32)
	if err != nil {
		return Session{}, err
	}
	session := Session{
		Type:              "termx_pair_v1",
		MachineID:         strings.TrimSpace(cfg.MachineID),
		MachineName:       strings.TrimSpace(cfg.MachineName),
		LocalPairURL:      strings.TrimSpace(cfg.LocalPairURL),
		PairSessionID:     sessionID,
		PairSecret:        secret,
		AnswerProofSecret: answerProofSecret,
		ExpiresAt:         now.Add(ttl).UTC(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)
	m.sessions[session.PairSessionID] = &sessionState{session: session, cfg: cfg}
	return session, nil
}

func (m *Manager) ClaimSession(req ClaimRequest) (ClaimResponse, error) {
	if m == nil {
		return ClaimResponse{}, errors.New("pairing manager is nil")
	}
	sessionID := strings.TrimSpace(req.PairSessionID)
	pairSecret := strings.TrimSpace(req.PairSecret)
	if sessionID == "" {
		return ClaimResponse{}, errors.New("pair_session_id is required")
	}
	if pairSecret == "" {
		return ClaimResponse{}, errors.New("pair_secret is required")
	}

	m.mu.Lock()
	m.cleanupExpiredLocked(nowFromConfig(m.cfg))
	state, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session not found")
	}
	if state.consumed {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session already consumed")
	}
	cfg := state.cfg
	now := nowFromConfig(cfg)
	if !now.Before(state.session.ExpiresAt) {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session expired")
	}
	if subtle.ConstantTimeCompare([]byte(state.session.PairSecret), []byte(pairSecret)) != 1 {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("invalid pair secret")
	}
	session := state.session
	m.mu.Unlock()
	if err := validateConfig(cfg); err != nil {
		return ClaimResponse{}, err
	}

	capabilities, err := normalizeRequestedCapabilities(req.RequestedCapabilities)
	if err != nil {
		return ClaimResponse{}, err
	}
	tokenTTL := cfg.DefaultTokenTTL
	if tokenTTL <= 0 {
		tokenTTL = defaultTokenTTL
	}
	expiresAt := now.Add(tokenTTL).UTC()
	claims := token.Claims{
		SessionID:    session.PairSessionID,
		MachineID:    strings.TrimSpace(cfg.MachineID),
		AppDeviceID:  strings.TrimSpace(req.AppDeviceID),
		AppName:      strings.TrimSpace(req.AppName),
		Capabilities: capabilities,
		IssuedAt:     now.Unix(),
		ExpiresAt:    expiresAt.Unix(),
	}
	if strings.TrimSpace(session.AnswerProofSecret) != "" {
		sealedProof, err := token.SealAnswerProofKey(cfg.MachineSecret, claims, session.AnswerProofSecret)
		if err != nil {
			return ClaimResponse{}, fmt.Errorf("seal answer proof key: %w", err)
		}
		claims.AnswerProofKey = sealedProof
	}
	tok, err := token.Issue(cfg.MachineSecret, claims)
	if err != nil {
		return ClaimResponse{}, fmt.Errorf("issue token: %w", err)
	}

	m.mu.Lock()
	state, ok = m.sessions[sessionID]
	if !ok || state.consumed {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session already consumed")
	}
	if state.session.PairSecret != session.PairSecret {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session changed")
	}
	if !nowFromConfig(cfg).Before(state.session.ExpiresAt) {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session expired")
	}
	state.consumed = true
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	return ClaimResponse{
		MachineID:    strings.TrimSpace(cfg.MachineID),
		MachineName:  strings.TrimSpace(cfg.MachineName),
		SessionToken: tok,
		ExpiresAt:    expiresAt,
	}, nil
}

func (m *Manager) CleanupExpired() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cleanupExpiredLocked(nowFromConfig(m.cfg))
}

func (m *Manager) cleanupExpiredLocked(now time.Time) int {
	removed := 0
	for sessionID, state := range m.sessions {
		if state == nil || !now.Before(state.session.ExpiresAt) {
			delete(m.sessions, sessionID)
			removed++
		}
	}
	return removed
}

func nowFromConfig(cfg Config) time.Time {
	if cfg.Now != nil {
		if now := cfg.Now().UTC(); !now.IsZero() {
			return now
		}
	}
	return time.Now().UTC()
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.MachineID) == "" {
		return errors.New("machine_id is required")
	}
	if strings.TrimSpace(cfg.MachineName) == "" {
		return errors.New("machine_name is required")
	}
	if len(cfg.MachineSecret) < 32 {
		return errors.New("machine secret is required")
	}
	return nil
}

func normalizeRequestedCapabilities(capabilities []string) ([]string, error) {
	if len(capabilities) == 0 {
		return nil, errors.New("requested_capabilities are required")
	}
	out := make([]string, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return nil, errors.New("requested_capabilities must not contain empty values")
		}
		if _, ok := allowedCapabilities[capability]; !ok {
			return nil, fmt.Errorf("unsupported capability %q", capability)
		}
		if _, ok := seen[capability]; ok {
			return nil, fmt.Errorf("duplicate capability %q", capability)
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	return out, nil
}

func randomToken(prefix string, byteLen int) (string, error) {
	if byteLen <= 0 {
		return "", errors.New("token byte length must be positive")
	}
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
