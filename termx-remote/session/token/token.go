package token

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/session/token/tokenpb"
	"google.golang.org/protobuf/proto"
)

const tokenVersion = "termx-session-v1:"
const answerProofKeyVersion = "termx-answer-proof-key-v1:"
const answerProofVersion = "termx-answer-proof-v1:"

type Claims struct {
	SessionID      string
	MachineID      string
	AppDeviceID    string
	AppName        string
	AnswerProofKey string
	Capabilities   []string
	Paths          []string
	IssuedAt       int64
	ExpiresAt      int64
}

func Issue(machineSecret []byte, claims Claims) (string, error) {
	if len(machineSecret) < 32 {
		return "", errors.New("machine secret must be at least 32 bytes")
	}
	caps := append([]string(nil), claims.Capabilities...)
	sort.Strings(caps)
	claims.Capabilities = caps
	paths := append([]string(nil), claims.Paths...)
	sort.Strings(paths)
	claims.Paths = paths
	payload, err := proto.Marshal(claimsToProto(claims))
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
	var msg tokenpb.Claims
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return Claims{}, fmt.Errorf("unmarshal claims: %w", err)
	}
	claims := claimsFromProto(&msg)
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

func ComputeAnswerProof(machineSecret []byte, claims Claims, rtcSessionID string, challenge string) (string, error) {
	proofKey, err := OpenAnswerProofKey(machineSecret, claims)
	if err != nil {
		return "", err
	}
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return "", errors.New("answer proof challenge is required")
	}
	mac := hmac.New(sha256.New, []byte(proofKey))
	mac.Write([]byte(answerProofVersion))
	mac.Write([]byte(strings.TrimSpace(claims.SessionID)))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.TrimSpace(rtcSessionID)))
	mac.Write([]byte(":"))
	mac.Write([]byte(challenge))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
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

func claimsToProto(claims Claims) *tokenpb.Claims {
	return &tokenpb.Claims{
		SessionId:      claims.SessionID,
		MachineId:      claims.MachineID,
		AppDeviceId:    claims.AppDeviceID,
		AppName:        claims.AppName,
		AnswerProofKey: claims.AnswerProofKey,
		Capabilities:   append([]string(nil), claims.Capabilities...),
		Paths:          append([]string(nil), claims.Paths...),
		IssuedAt:       claims.IssuedAt,
		ExpiresAt:      claims.ExpiresAt,
	}
}

func claimsFromProto(msg *tokenpb.Claims) Claims {
	if msg == nil {
		return Claims{}
	}
	return Claims{
		SessionID:      msg.GetSessionId(),
		MachineID:      msg.GetMachineId(),
		AppDeviceID:    msg.GetAppDeviceId(),
		AppName:        msg.GetAppName(),
		AnswerProofKey: msg.GetAnswerProofKey(),
		Capabilities:   append([]string(nil), msg.GetCapabilities()...),
		Paths:          append([]string(nil), msg.GetPaths()...),
		IssuedAt:       msg.GetIssuedAt(),
		ExpiresAt:      msg.GetExpiresAt(),
	}
}
