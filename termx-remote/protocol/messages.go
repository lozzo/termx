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
	Enabled         bool
	ControlURL      string
	HubURL          string
	HubURLs         []string
	AccessToken     string
	DataDir         string
	DeviceName      string
	Region          string
	Mode            string
	LocalWebAddr    string
	ICETCPAddr      string
	AllowLAN        bool
	LANIPs          []string
	TokenTTLSeconds int
}

type Status struct {
	State         RuntimeState `json:"state"`
	Detail        string       `json:"detail,omitempty"`
	DeviceID      string       `json:"device_id,omitempty"`
	DeviceName    string       `json:"device_name,omitempty"`
	ControlURL    string       `json:"control_url,omitempty"`
	HubURL        string       `json:"hub_url,omitempty"`
	HubURLs       []string     `json:"hub_urls,omitempty"`
	DataDir       string       `json:"data_dir,omitempty"`
	Mode          string       `json:"mode,omitempty"`
	AllowLAN      bool         `json:"allow_lan"`
	TerminalCount int          `json:"terminal_count"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type PairStartParams struct {
	LocalPairURL   string `json:"local_pair_url"`
	TTLSeconds     int    `json:"ttl_seconds,omitempty"`
	AuthTTLSeconds int    `json:"auth_ttl_seconds,omitempty"`
}

type PairStartResult struct {
	Type              string    `json:"type"`
	MachineID         string    `json:"machine_id"`
	MachineName       string    `json:"machine_name"`
	LocalPairURL      string    `json:"local_pair_url"`
	PairSessionID     string    `json:"pair_session_id"`
	PairSecret        string    `json:"pair_secret"`
	AnswerProofSecret string    `json:"answer_proof_secret,omitempty"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type LocalEnableParams struct {
	LocalWebAddr string   `json:"local_web_addr"`
	ICETCPAddr   string   `json:"ice_tcp_addr,omitempty"`
	HubURLs      []string `json:"hub_urls,omitempty"`
	ControlURL   string   `json:"control_url,omitempty"`
	AccessToken  string   `json:"access_token,omitempty"`
	Region       string   `json:"region,omitempty"`
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
