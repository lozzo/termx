package token

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const tokenVersion = "termx-session-v1:"
const answerProofKeyVersion = "termx-answer-proof-key-v1:"

type Claims struct {
	SessionID      string   `json:"sid"`
	MachineID      string   `json:"mid"`
	AppDeviceID    string   `json:"app_device_id,omitempty"`
	AppName        string   `json:"app_name,omitempty"`
	AnswerProofKey string   `json:"apk,omitempty"`
	Capabilities   []string `json:"cap"`
	IssuedAt       int64    `json:"iat"`
	ExpiresAt      int64    `json:"exp"`
}

func Issue(machineSecret []byte, claims Claims) (string, error) {
	if len(machineSecret) < 32 {
		return "", errors.New("machine secret must be at least 32 bytes")
	}
	caps := append([]string(nil), claims.Capabilities...)
	sort.Strings(caps)
	claims.Capabilities = caps
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal token claims: %w", err)
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	m := base64.RawURLEncoding.EncodeToString(computeMAC(machineSecret, p))
	return p + "." + m, nil
}

func Verify(tok string, machineSecret []byte, now time.Time) (Claims, error) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, errors.New("invalid token format")
	}
	expected := computeMAC(machineSecret, parts[0])
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("decode mac: %w", err)
	}
	if !hmac.Equal(expected, provided) {
		return Claims{}, errors.New("token signature invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("unmarshal claims: %w", err)
	}
	if now.Unix() >= claims.ExpiresAt {
		return Claims{}, errors.New("token expired")
	}
	return claims, nil
}

func SealAnswerProofKey(machineSecret []byte, claims Claims, proofSecret string) (string, error) {
	if len(machineSecret) < 32 {
		return "", errors.New("machine secret must be at least 32 bytes")
	}
	proofSecret = strings.TrimSpace(proofSecret)
	if proofSecret == "" {
		return "", errors.New("answer proof secret is required")
	}
	block, err := aes.NewCipher(machineSecret[:32])
	if err != nil {
		return "", fmt.Errorf("create answer proof cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create answer proof gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate answer proof nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(proofSecret), answerProofAAD(claims))
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func OpenAnswerProofKey(machineSecret []byte, claims Claims) (string, error) {
	if len(machineSecret) < 32 {
		return "", errors.New("machine secret must be at least 32 bytes")
	}
	if strings.TrimSpace(claims.AnswerProofKey) == "" {
		return "", errors.New("answer proof key is not present")
	}
	raw, err := base64.RawURLEncoding.DecodeString(claims.AnswerProofKey)
	if err != nil {
		return "", fmt.Errorf("decode answer proof key: %w", err)
	}
	block, err := aes.NewCipher(machineSecret[:32])
	if err != nil {
		return "", fmt.Errorf("create answer proof cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create answer proof gcm: %w", err)
	}
	if len(raw) <= gcm.NonceSize() {
		return "", errors.New("answer proof key is truncated")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	opened, err := gcm.Open(nil, nonce, ciphertext, answerProofAAD(claims))
	if err != nil {
		return "", fmt.Errorf("open answer proof key: %w", err)
	}
	return string(opened), nil
}

func computeMAC(secret []byte, msg string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(tokenVersion))
	h.Write([]byte(msg))
	return h.Sum(nil)
}

func answerProofAAD(claims Claims) []byte {
	return []byte(answerProofKeyVersion + strings.TrimSpace(claims.SessionID) + ":" + strings.TrimSpace(claims.MachineID))
}
