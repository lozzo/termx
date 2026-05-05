package cert

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/identity"
)

type AppCertificatePayload struct {
	Version                     int       `json:"version"`
	CertID                      string    `json:"cert_id"`
	MachineID                   string    `json:"machine_id"`
	MachinePublicKeyFingerprint string    `json:"machine_public_key_fingerprint"`
	AppDeviceID                 string    `json:"app_device_id"`
	AppPublicKey                string    `json:"app_public_key"`
	AppName                     string    `json:"app_name"`
	Capabilities                []string  `json:"capabilities"`
	IssuedAt                    time.Time `json:"issued_at"`
	ExpiresAt                   time.Time `json:"expires_at"`
}

type AppCertificateEnvelope struct {
	Payload   AppCertificatePayload `json:"payload"`
	Signature string                `json:"signature"`
}

func CanonicalPayload(payload AppCertificatePayload) ([]byte, error) {
	if err := validatePayload(payload); err != nil {
		return nil, err
	}
	normalized := normalizePayload(payload)
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode app certificate payload: %w", err)
	}
	return data, nil
}

func SignAppCertificate(payload AppCertificatePayload, machineKey identity.MachineKey) (AppCertificateEnvelope, error) {
	payload.MachinePublicKeyFingerprint = identity.MachinePublicKeyFingerprint(machineKey.PublicKey)
	canonical, err := CanonicalPayload(payload)
	if err != nil {
		return AppCertificateEnvelope{}, err
	}
	signature := machineKey.Sign(canonical)
	if len(signature) != ed25519.SignatureSize {
		return AppCertificateEnvelope{}, errors.New("machine key cannot sign app certificate")
	}
	return AppCertificateEnvelope{
		Payload:   normalizePayload(payload),
		Signature: base64.StdEncoding.EncodeToString(signature),
	}, nil
}

func VerifyAppCertificate(envelope AppCertificateEnvelope, machinePublicKey ed25519.PublicKey, now time.Time) error {
	if len(machinePublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("machine public key has size %d, want %d", len(machinePublicKey), ed25519.PublicKeySize)
	}
	canonical, err := CanonicalPayload(envelope.Payload)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return fmt.Errorf("decode app certificate signature: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("app certificate signature has size %d, want %d", len(signature), ed25519.SignatureSize)
	}
	expectedFingerprint := identity.MachinePublicKeyFingerprint(machinePublicKey)
	if envelope.Payload.MachinePublicKeyFingerprint != expectedFingerprint {
		return fmt.Errorf("machine public key fingerprint mismatch")
	}
	if !now.Before(envelope.Payload.ExpiresAt) {
		return fmt.Errorf("app certificate expired")
	}
	if now.Before(envelope.Payload.IssuedAt.Add(-time.Minute)) {
		return fmt.Errorf("app certificate is not valid yet")
	}
	if !ed25519.Verify(machinePublicKey, canonical, signature) {
		return fmt.Errorf("app certificate signature verification failed")
	}
	return nil
}

type ReplayWindow struct {
	maxSkew time.Duration

	mu    sync.Mutex
	seen  map[string]time.Time
	order []string
}

func NewReplayWindow(maxSkew time.Duration) *ReplayWindow {
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	return &ReplayWindow{
		maxSkew: maxSkew,
		seen:    make(map[string]time.Time),
	}
}

func (w *ReplayWindow) Accept(nonce string, timestamp time.Time, now time.Time) error {
	if w == nil {
		return errors.New("replay window is nil")
	}
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return errors.New("nonce is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	timestamp = timestamp.UTC()
	now = now.UTC()
	if timestamp.Before(now.Add(-w.maxSkew)) {
		return errors.New("timestamp is stale")
	}
	if timestamp.After(now.Add(w.maxSkew)) {
		return errors.New("timestamp is too far in the future")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked(now)
	if _, ok := w.seen[nonce]; ok {
		return errors.New("nonce has already been used")
	}
	w.seen[nonce] = timestamp
	w.order = append(w.order, nonce)
	return nil
}

func (w *ReplayWindow) pruneLocked(now time.Time) {
	cutoff := now.Add(-w.maxSkew)
	keep := w.order[:0]
	for _, nonce := range w.order {
		ts, ok := w.seen[nonce]
		if !ok {
			continue
		}
		if ts.Before(cutoff) {
			delete(w.seen, nonce)
			continue
		}
		keep = append(keep, nonce)
	}
	w.order = keep
}

func validatePayload(payload AppCertificatePayload) error {
	if payload.Version != 1 {
		return fmt.Errorf("unsupported app certificate version %d", payload.Version)
	}
	required := map[string]string{
		"cert_id":                        payload.CertID,
		"machine_id":                     payload.MachineID,
		"machine_public_key_fingerprint": payload.MachinePublicKeyFingerprint,
		"app_device_id":                  payload.AppDeviceID,
		"app_public_key":                 payload.AppPublicKey,
		"app_name":                       payload.AppName,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	appPublicKey, err := base64.StdEncoding.DecodeString(payload.AppPublicKey)
	if err != nil {
		return fmt.Errorf("decode app public key: %w", err)
	}
	if len(appPublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("app public key has size %d, want %d", len(appPublicKey), ed25519.PublicKeySize)
	}
	if len(payload.Capabilities) == 0 {
		return errors.New("capabilities are required")
	}
	seenCapabilities := make(map[string]struct{}, len(payload.Capabilities))
	for _, capability := range payload.Capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			return errors.New("capabilities must not contain empty values")
		}
		if _, ok := seenCapabilities[capability]; ok {
			return fmt.Errorf("duplicate capability %q", capability)
		}
		seenCapabilities[capability] = struct{}{}
	}
	if payload.IssuedAt.IsZero() {
		return errors.New("issued_at is required")
	}
	if payload.ExpiresAt.IsZero() {
		return errors.New("expires_at is required")
	}
	if !payload.ExpiresAt.After(payload.IssuedAt) {
		return errors.New("expires_at must be after issued_at")
	}
	return nil
}

func normalizePayload(payload AppCertificatePayload) AppCertificatePayload {
	payload.CertID = strings.TrimSpace(payload.CertID)
	payload.MachineID = strings.TrimSpace(payload.MachineID)
	payload.MachinePublicKeyFingerprint = strings.TrimSpace(payload.MachinePublicKeyFingerprint)
	payload.AppDeviceID = strings.TrimSpace(payload.AppDeviceID)
	payload.AppPublicKey = strings.TrimSpace(payload.AppPublicKey)
	payload.AppName = strings.TrimSpace(payload.AppName)
	payload.IssuedAt = payload.IssuedAt.UTC().Round(0)
	payload.ExpiresAt = payload.ExpiresAt.UTC().Round(0)
	payload.Capabilities = append([]string(nil), payload.Capabilities...)
	for i := range payload.Capabilities {
		payload.Capabilities[i] = strings.TrimSpace(payload.Capabilities[i])
	}
	sort.Strings(payload.Capabilities)
	return payload
}
