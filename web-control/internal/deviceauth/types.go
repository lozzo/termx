package deviceauth

import (
	"errors"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusExpired  = "expired"
	StatusConsumed = "consumed"
)

var (
	ErrAuthorizationPending = errors.New("authorization pending")
	ErrAccessDenied         = errors.New("access denied")
	ErrExpired              = errors.New("device code expired")
	ErrAlreadyConsumed      = errors.New("device code already consumed")
	ErrRateLimited          = errors.New("device code rate limited")
	ErrAttemptLocked        = errors.New("device code locked after too many attempts")
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type CreateInput struct {
	ClientName      string
	VerificationURI string
	ExpiresIn       time.Duration
}

type CreateResult struct {
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
	ExpiresAt               time.Time `json:"expires_at"`
	ExpiresInSeconds        int       `json:"expires_in"`
	IntervalSeconds         int       `json:"interval"`
}

type PollInput struct {
	DeviceCode string
}

type PollResult struct {
	Status string             `json:"status"`
	Auth   account.AuthResult `json:"auth,omitempty"`
}

type DecisionInput struct {
	UserID   string
	UserCode string
	Reason   string
}

type CleanupInput struct {
	Now       time.Time
	Retention time.Duration
}

type CleanupResult struct {
	Expired int64
	Deleted int64
}

func IsAuthorizationPending(err error) bool {
	return errors.Is(err, ErrAuthorizationPending)
}

func IsAccessDenied(err error) bool {
	return errors.Is(err, ErrAccessDenied)
}

func IsExpired(err error) bool {
	return errors.Is(err, ErrExpired)
}

func IsAlreadyConsumed(err error) bool {
	return errors.Is(err, ErrAlreadyConsumed)
}

func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited)
}

func IsAttemptLocked(err error) bool {
	return errors.Is(err, ErrAttemptLocked)
}
