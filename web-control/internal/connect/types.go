package connect

import (
	"context"
	"time"
)

const PathManaged = "managed"

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type ManagedTicket struct {
	ID                  string    `json:"id"`
	UserID              string    `json:"user_id"`
	MachineID           string    `json:"machine_id"`
	TerminalID          string    `json:"terminal_id"`
	Path                string    `json:"path"`
	AllowRelay          bool      `json:"allow_relay"`
	RelayInUse          bool      `json:"relay_in_use"`
	RelayBytesRemaining int64     `json:"relay_bytes_remaining"`
	RelayThrottled      bool      `json:"relay_throttled"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type CreateManagedTicketInput struct {
	UserID     string
	MachineID  string
	TerminalID string
	TTL        time.Duration
}

type VerifyManagedTicketInput struct {
	TicketID   string
	MachineID  string
	TerminalID string
}

type ManagedRelayPolicyInput struct {
	UserID     string
	MachineID  string
	TerminalID string
}

type ManagedRelayPolicy struct {
	AllowRelay          bool
	RelayBytesRemaining int64
	RelayThrottled      bool
}

type ManagedRelayPolicyProvider interface {
	ManagedRelayPolicy(context.Context, ManagedRelayPolicyInput) (ManagedRelayPolicy, error)
}
