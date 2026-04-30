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

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
	"github.com/lozzow/termx/termx-core/internal/remote/identity"
)

const (
	defaultSessionTTL     = 5 * time.Minute
	defaultCertificateTTL = 365 * 24 * time.Hour
)

var allowedCapabilities = map[string]struct{}{
	"terminal":     {},
	"file_manager": {},
}

type Config struct {
	MachineID    string
	MachineName  string
	MachineKey   identity.MachineKey
	LocalPairURL string
	Now          func() time.Time
}

type Manager struct {
	cfg Config

	mu       sync.Mutex
	sessions map[string]*sessionState
}

type Session struct {
	Type                        string    `json:"type"`
	MachineID                   string    `json:"machine_id"`
	MachineName                 string    `json:"machine_name"`
	MachinePublicKeyFingerprint string    `json:"machine_public_key_fingerprint"`
	LocalPairURL                string    `json:"local_pair_url"`
	PairSessionID               string    `json:"pair_session_id"`
	PairSecret                  string    `json:"pair_secret"`
	ExpiresAt                   time.Time `json:"expires_at"`
}

type ClaimRequest struct {
	PairSessionID          string        `json:"pair_session_id"`
	PairSecret             string        `json:"pair_secret"`
	AppDeviceID            string        `json:"app_device_id"`
	AppName                string        `json:"app_name"`
	AppPublicKey           string        `json:"app_public_key"`
	RequestedCapabilities  []string      `json:"requested_capabilities"`
	CertificateTTL         time.Duration `json:"-"`
	CertificateIDGenerator func() string `json:"-"`
}

type ClaimResponse struct {
	MachineID        string                      `json:"machine_id"`
	MachineName      string                      `json:"machine_name"`
	MachinePublicKey string                      `json:"machine_public_key"`
	AppCertificate   cert.AppCertificateEnvelope `json:"app_certificate"`
}

type sessionState struct {
	session  Session
	consumed bool
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*sessionState),
	}
}

func (m *Manager) CreateSession(ttl time.Duration) (Session, error) {
	if m == nil {
		return Session{}, errors.New("pairing manager is nil")
	}
	if err := validateConfig(m.cfg); err != nil {
		return Session{}, err
	}
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	now := m.now()
	sessionID, err := randomToken("pair_", 16)
	if err != nil {
		return Session{}, err
	}
	secret, err := randomToken("", 24)
	if err != nil {
		return Session{}, err
	}
	session := Session{
		Type:                        "termx_pair_v1",
		MachineID:                   strings.TrimSpace(m.cfg.MachineID),
		MachineName:                 strings.TrimSpace(m.cfg.MachineName),
		MachinePublicKeyFingerprint: identity.MachinePublicKeyFingerprint(m.cfg.MachineKey.PublicKey),
		LocalPairURL:                strings.TrimSpace(m.cfg.LocalPairURL),
		PairSessionID:               sessionID,
		PairSecret:                  secret,
		ExpiresAt:                   now.Add(ttl).UTC(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.PairSessionID] = &sessionState{session: session}
	return session, nil
}

func (m *Manager) ClaimSession(req ClaimRequest) (ClaimResponse, error) {
	if m == nil {
		return ClaimResponse{}, errors.New("pairing manager is nil")
	}
	if err := validateConfig(m.cfg); err != nil {
		return ClaimResponse{}, err
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
	state, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session not found")
	}
	if state.consumed {
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session already consumed")
	}
	now := m.now()
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

	certID := ""
	if req.CertificateIDGenerator != nil {
		certID = strings.TrimSpace(req.CertificateIDGenerator())
	}
	if certID == "" {
		var err error
		certID, err = randomToken("cert_", 16)
		if err != nil {
			return ClaimResponse{}, err
		}
	}
	certificateTTL := req.CertificateTTL
	if certificateTTL <= 0 {
		certificateTTL = defaultCertificateTTL
	}
	capabilities, err := normalizeRequestedCapabilities(req.RequestedCapabilities)
	if err != nil {
		return ClaimResponse{}, err
	}
	payload := cert.AppCertificatePayload{
		Version:                     1,
		CertID:                      certID,
		MachineID:                   strings.TrimSpace(m.cfg.MachineID),
		MachinePublicKeyFingerprint: identity.MachinePublicKeyFingerprint(m.cfg.MachineKey.PublicKey),
		AppDeviceID:                 strings.TrimSpace(req.AppDeviceID),
		AppPublicKey:                strings.TrimSpace(req.AppPublicKey),
		AppName:                     strings.TrimSpace(req.AppName),
		Capabilities:                capabilities,
		IssuedAt:                    now.UTC(),
		ExpiresAt:                   now.Add(certificateTTL).UTC(),
	}
	envelope, err := cert.SignAppCertificate(payload, m.cfg.MachineKey)
	if err != nil {
		return ClaimResponse{}, err
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
	if !m.now().Before(state.session.ExpiresAt) {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return ClaimResponse{}, errors.New("pair session expired")
	}
	state.consumed = true
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	return ClaimResponse{
		MachineID:        strings.TrimSpace(m.cfg.MachineID),
		MachineName:      strings.TrimSpace(m.cfg.MachineName),
		MachinePublicKey: base64.StdEncoding.EncodeToString(m.cfg.MachineKey.PublicKey),
		AppCertificate:   envelope,
	}, nil
}

func (m *Manager) now() time.Time {
	if m != nil && m.cfg.Now != nil {
		return m.cfg.Now().UTC()
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
	if len(cfg.MachineKey.PublicKey) == 0 {
		return errors.New("machine key is required")
	}
	if strings.TrimSpace(cfg.LocalPairURL) == "" {
		return errors.New("local_pair_url is required")
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
