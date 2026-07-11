package servicecredential

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	// ErrMalformedCredential 表示 token framing、canonical JSON 或字段约束非法。
	ErrMalformedCredential = errors.New("malformed service credential")
	// ErrCredentialExpired 表示短期服务凭据已经过期或尚未生效。
	ErrCredentialExpired = errors.New("service credential outside validity window")
	// ErrCredentialBinding 表示 audience、principal、session、route 或 operation 不匹配。
	ErrCredentialBinding = errors.New("service credential binding mismatch")
)

const (
	hubAdmissionPrefix = "TXHA1"
	relayLeasePrefix   = "TXRL1"
)

func signToken(prefix string, claims any, signer Signer, issuedAt time.Time) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode credential claims: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	canonical := []byte(prefix + "." + encodedPayload)
	signature, err := signer.Sign(canonical, issuedAt)
	if err != nil {
		return "", err
	}
	return string(canonical) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func verifyToken(prefix, token string, keyID func([]byte) (string, error), destination any, ring *KeyRing, now time.Time) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return ErrMalformedCredential
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrMalformedCredential
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		return ErrMalformedCredential
	}
	id, err := keyID(payload)
	if err != nil || id == "" {
		return ErrMalformedCredential
	}
	if err := ring.Verify(id, []byte(parts[0]+"."+parts[1]), signature, now); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrMalformedCredential
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrMalformedCredential
	}
	canonicalPayload, err := json.Marshal(destination)
	if err != nil || !bytes.Equal(payload, canonicalPayload) {
		return ErrMalformedCredential
	}
	return nil
}

func tokenKeyID(payload []byte) (string, error) {
	var header struct {
		KeyID string `json:"key_id"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return "", err
	}
	return header.KeyID, nil
}

func containsString[T ~string](values []T, target T) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
