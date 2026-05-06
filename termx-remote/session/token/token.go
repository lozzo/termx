package token

import (
	"crypto/hmac"
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

type Claims struct {
	SessionID    string   `json:"sid"`
	MachineID    string   `json:"mid"`
	Capabilities []string `json:"cap"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
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

func computeMAC(secret []byte, msg string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(tokenVersion))
	h.Write([]byte(msg))
	return h.Sum(nil)
}
