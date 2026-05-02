package relay

import "time"

const (
	PathManaged    = "managed"
	SessionLeased  = "leased"
	SessionActive  = "active"
	SessionExpired = "expired"
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
	RelayThrottled      bool      `json:"relay_throttled"`
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

type HeartbeatInput struct {
	SessionID          string
	AuthenticatedHubID string
	BytesRXTotal       int64
	BytesTXTotal       int64
}

type HeartbeatResult struct {
	SessionID           string    `json:"session_id"`
	UserID              string    `json:"user_id"`
	MachineID           string    `json:"machine_id"`
	HubID               string    `json:"hub_id"`
	Status              string    `json:"status"`
	RelayBytesUsed      int64     `json:"relay_bytes_used"`
	RelayBytesRemaining int64     `json:"relay_bytes_remaining"`
	RelayThrottled      bool      `json:"relay_throttled"`
	RateLimitBps        int64     `json:"rate_limit_bps,omitempty"`
	LastHeartbeatAt     time.Time `json:"last_heartbeat_at"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type CleanupInput struct {
	Now               time.Time
	HeartbeatTimeout  time.Duration
	ExpireLeasedAfter time.Duration
	ExpireActiveAfter time.Duration
}
