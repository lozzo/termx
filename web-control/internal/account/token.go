package account

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TokenIssuer interface {
	IssueAccess(userID string, now time.Time) (string, error)
	VerifyAccess(token string, now time.Time) (string, error)
	NewRefreshToken() (string, string, error)
	HashRefreshToken(token string) string
}

type HMACTokenIssuer struct {
	secret []byte
}

func NewHMACTokenIssuer(secret []byte) *HMACTokenIssuer {
	cp := make([]byte, len(secret))
	copy(cp, secret)
	return &HMACTokenIssuer{secret: cp}
}

func (i *HMACTokenIssuer) IssueAccess(userID string, now time.Time) (string, error) {
	if len(i.secret) == 0 {
		return "", errors.New("token secret is required")
	}
	exp := now.Add(15 * time.Minute).Unix()
	payload := fmt.Sprintf("%s:%d:%s", userID, exp, randomID("nonce"))
	sig := sign(i.secret, payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig)), nil
}

func (i *HMACTokenIssuer) VerifyAccess(token string, now time.Time) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", errors.New("invalid token")
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 4 {
		return "", errors.New("invalid token")
	}
	payload := strings.Join(parts[:3], ":")
	if !hmac.Equal([]byte(sign(i.secret, payload)), []byte(parts[3])) {
		return "", errors.New("invalid token signature")
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", errors.New("invalid token expiry")
	}
	if now.Unix() >= expiry {
		return "", errors.New("token expired")
	}
	return parts[0], nil
}

func (i *HMACTokenIssuer) NewRefreshToken() (string, string, error) {
	token := randomID("rfr")
	return token, i.HashRefreshToken(token), nil
}

func (i *HMACTokenIssuer) HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func sign(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}
