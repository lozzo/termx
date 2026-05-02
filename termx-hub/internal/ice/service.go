package ice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidPath   = errors.New("invalid connection path")
	ErrInvalidICEURL = errors.New("invalid ice url")
	ErrLeaseExpired  = errors.New("relay lease expired")
	ErrLeaseRequired = errors.New("relay lease id is required")
)

const MaxCredentialTTL = 10 * time.Minute

type Config struct {
	Clock        Clock
	Realm        string
	SharedSecret string
	STUNURLs     []string
	TURNURLs     []string
}

type Service struct {
	clock        Clock
	realm        string
	sharedSecret string
	stunURLs     []string
	turnURLs     []string
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{
		clock:        clock,
		realm:        strings.TrimSpace(cfg.Realm),
		sharedSecret: cfg.SharedSecret,
		stunURLs:     cleanURLs(cfg.STUNURLs),
		turnURLs:     cleanURLs(cfg.TURNURLs),
	}
}

func (s *Service) ConfigForLease(ctx context.Context, lease Lease) (RTCConfig, error) {
	_ = ctx
	path := strings.TrimSpace(lease.Path)
	if path == "" {
		path = PathManaged
	}
	switch path {
	case PathManaged, PathPublicP2P:
	default:
		return RTCConfig{}, ErrInvalidPath
	}
	if !lease.ExpiresAt.IsZero() && !s.clock.Now().UTC().Before(lease.ExpiresAt.UTC()) {
		return RTCConfig{}, ErrLeaseExpired
	}
	cfg := RTCConfig{
		Path:       path,
		ICEServers: []ICEServer{{URLs: append([]string(nil), s.stunURLs...)}},
	}
	if !onlyAllowedSchemes(s.stunURLs, "stun:", "stuns:") {
		return RTCConfig{}, ErrInvalidICEURL
	}
	if path != PathManaged || !lease.AllowRelay {
		return cfg, nil
	}
	if len(s.turnURLs) == 0 {
		return RTCConfig{}, errors.New("turn urls are required for relay lease")
	}
	if !onlyAllowedSchemes(s.turnURLs, "turn:", "turns:") {
		return RTCConfig{}, ErrInvalidICEURL
	}
	if strings.TrimSpace(s.sharedSecret) == "" {
		return RTCConfig{}, errors.New("turn shared secret is required")
	}
	if strings.TrimSpace(lease.ID) == "" {
		return RTCConfig{}, ErrLeaseRequired
	}
	expiresAt := s.credentialExpiry(lease)
	username := lease.ID + ":" + strconv.FormatInt(expiresAt.Unix(), 10)
	cfg.ICEServers = append(cfg.ICEServers, ICEServer{
		URLs:       append([]string(nil), s.turnURLs...),
		Username:   username,
		Credential: s.computeCredential(username),
		ExpiresAt:  &expiresAt,
	})
	return cfg, nil
}

func (s *Service) VerifyCredential(username string, credential string, now time.Time, leaseID string) bool {
	leasePart, expiryPart, ok := strings.Cut(strings.TrimSpace(username), ":")
	if !ok || leasePart != strings.TrimSpace(leaseID) {
		return false
	}
	expiry, err := strconv.ParseInt(expiryPart, 10, 64)
	if err != nil || now.Unix() > expiry {
		return false
	}
	return hmac.Equal([]byte(s.computeCredential(username)), []byte(credential))
}

func (s *Service) credentialExpiry(lease Lease) time.Time {
	expiresAt := s.clock.Now().UTC().Add(MaxCredentialTTL)
	if !lease.ExpiresAt.IsZero() && lease.ExpiresAt.Before(expiresAt) {
		expiresAt = lease.ExpiresAt.UTC()
	}
	return expiresAt
}

func (s *Service) computeCredential(username string) string {
	mac := hmac.New(sha1.New, []byte(s.sharedSecret))
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func onlyAllowedSchemes(urls []string, schemes ...string) bool {
	for _, value := range urls {
		lower := strings.ToLower(strings.TrimSpace(value))
		ok := false
		for _, scheme := range schemes {
			if strings.HasPrefix(lower, scheme) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func cleanURLs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
