package relay

import "time"

const (
	PathManaged   = "managed"
	SessionLeased = "leased"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type RelayLease struct {
	ID                  string    `json:"id"`
	UserID              string    `json:"user_id"`
	MachineID           string    `json:"machine_id"`
	HubID               string    `json:"hub_id"`
	Path                string    `json:"path"`
	AllowRelay          bool      `json:"allow_relay"`
	RelayInUse          bool      `json:"relay_in_use"`
	RelayBytesRemaining int64     `json:"relay_bytes_remaining"`
	RelaySessionLimit   int       `json:"relay_session_limit"`
	RelayThrottleBps    int64     `json:"relay_throttle_bps,omitempty"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type CreateLeaseInput struct {
	UserID    string
	MachineID string
	HubID     string
	TTL       time.Duration
}
