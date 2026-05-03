package hubregistry

import "time"

const (
	HubOnline    = "online"
	HubOffline   = "offline"
	AgentOnline  = "online"
	AgentOffline = "offline"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type Hub struct {
	ID              string    `json:"id"`
	Region          string    `json:"region"`
	HTTPURL         string    `json:"http_url"`
	Status          string    `json:"status"`
	Capacity        int       `json:"capacity"`
	Weight          int       `json:"weight"`
	Health          string    `json:"health"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type AgentReport struct {
	MachineID     string
	AgentID       string
	Status        string
	TerminalCount int
}

type AgentPolicy struct {
	MachineID    string `json:"machine_id"`
	AgentID      string `json:"agent_id"`
	ForceOffline bool   `json:"force_offline"`
	Reason       string `json:"reason,omitempty"`
}

type ReportHubInput struct {
	HubID    string
	Region   string
	HTTPURL  string
	Status   string
	Capacity int
	Weight   int
	Health   string
	TTL      time.Duration
	Agents   []AgentReport
}

type ReportHubResult struct {
	Hub           Hub
	AgentPolicies []AgentPolicy
}

type DiscoverHubsInput struct {
	Now time.Time
}

type ForceOfflineInput struct {
	UserID    string
	MachineID string
	AgentID   string
	Reason    string
}

type AgentPolicyInput struct {
	MachineID string
	AgentID   string
}

type CleanupInput struct {
	Now time.Time
}
