package protocol

import "time"

type RuntimeState string

const (
	StateDisabled    RuntimeState = "disabled"
	StateConfigured  RuntimeState = "configured"
	StateRegistering RuntimeState = "registering"
	StateOnline      RuntimeState = "online"
	StateDegraded    RuntimeState = "degraded"
)

type Config struct {
	Enabled     bool
	ControlURL  string
	HubURL      string
	AccessToken string
	DataDir     string
	DeviceName  string
	Region      string
}

type Status struct {
	State         RuntimeState `json:"state"`
	Detail        string       `json:"detail,omitempty"`
	DeviceID      string       `json:"device_id,omitempty"`
	DeviceName    string       `json:"device_name,omitempty"`
	ControlURL    string       `json:"control_url,omitempty"`
	HubURL        string       `json:"hub_url,omitempty"`
	DataDir       string       `json:"data_dir,omitempty"`
	TerminalCount int          `json:"terminal_count"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type PairStartParams struct {
	LocalPairURL string `json:"local_pair_url"`
	TTLSeconds   int    `json:"ttl_seconds,omitempty"`
}

type PairStartResult struct {
	Type                        string    `json:"type"`
	MachineID                   string    `json:"machine_id"`
	MachineName                 string    `json:"machine_name"`
	MachinePublicKeyFingerprint string    `json:"machine_public_key_fingerprint"`
	LocalPairURL                string    `json:"local_pair_url"`
	PairSessionID               string    `json:"pair_session_id"`
	PairSecret                  string    `json:"pair_secret"`
	ExpiresAt                   time.Time `json:"expires_at"`
}

type LocalEnableParams struct {
	LocalWebAddr string `json:"local_web_addr"`
	ICETCPAddr   string `json:"ice_tcp_addr,omitempty"`
}

type LocalStatus struct {
	Enabled       bool      `json:"enabled"`
	HTTPURL       string    `json:"http_url,omitempty"`
	LocalWebAddr  string    `json:"local_web_addr,omitempty"`
	LocalPairURL  string    `json:"local_pair_url,omitempty"`
	ICETCPEnabled bool      `json:"ice_tcp_enabled"`
	ICETCPAddr    string    `json:"ice_tcp_addr,omitempty"`
	ICETCPPort    int       `json:"ice_tcp_port,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}
