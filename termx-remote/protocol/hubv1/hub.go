package hubv1

type HubTerminalInventoryItem struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Cols    int      `json:"cols"`
	Rows    int      `json:"rows"`
	State   string   `json:"state"`
}

type HubRegisterRequest struct {
	Version        string                     `json:"version"`
	DeviceID       string                     `json:"device_id"`
	AgentID        string                     `json:"agent_id"`
	DisplayName    string                     `json:"display_name"`
	Hostname       string                     `json:"hostname"`
	Platform       string                     `json:"platform"`
	OwnerUserID    string                     `json:"owner_user_id,omitempty"`
	Labels         []string                   `json:"labels"`
	RuntimeVersion string                     `json:"runtime_version"`
	Terminals      []HubTerminalInventoryItem `json:"terminals"`
}

type RTCIceServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type RTCConfig struct {
	IceServers []RTCIceServerConfig `json:"ice_servers"`
}

type RelayPolicy struct {
	AllowRelay         bool `json:"allow_relay"`
	AllowRelayTransfer bool `json:"allow_relay_transfer"`
}

type HubRegisterResponse struct {
	Version                  string      `json:"version"`
	HubID                    string      `json:"hub_id"`
	AgentSessionID           string      `json:"agent_session_id"`
	HeartbeatIntervalSeconds int         `json:"heartbeat_interval_seconds"`
	RTCConfig                RTCConfig   `json:"rtc_config"`
	RelayPolicy              RelayPolicy `json:"relay_policy"`
}

type HubHeartbeatRequest struct {
	AgentSessionID string                     `json:"agent_session_id"`
	DeviceID       string                     `json:"device_id"`
	Terminals      []HubTerminalInventoryItem `json:"terminals"`
	LastSeenAt     string                     `json:"last_seen_at"`
}

type HubHeartbeatResponse struct {
	Accepted             bool `json:"accepted"`
	NextHeartbeatSeconds int  `json:"next_heartbeat_seconds"`
}

type SignalingOffer struct {
	SessionID    string   `json:"session_id"`
	MachineID    string   `json:"machine_id"`
	TerminalID   string   `json:"terminal_id,omitempty"`
	SDP          string   `json:"sdp"`
	Candidates   []string `json:"ice_candidates,omitempty"`
	SessionToken string   `json:"session_token"`
}

type SignalingAnswer struct {
	SessionID     string   `json:"session_id"`
	SDP           string   `json:"sdp"`
	ICECandidates []string `json:"ice_candidates"`
	Error         string   `json:"error,omitempty"`
}

type SignalingPollRequest struct {
	AgentSessionID string `json:"agent_session_id"`
	DeviceID       string `json:"device_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type SignalingPollResponse struct {
	Offer *SignalingOffer `json:"offer,omitempty"`
}

type SubmitSignalingAnswerRequest struct {
	AgentSessionID string          `json:"agent_session_id"`
	DeviceID       string          `json:"device_id"`
	Answer         SignalingAnswer `json:"answer"`
}

type PairingClaim struct {
	ClaimID               string   `json:"claim_id"`
	MachineID             string   `json:"machine_id"`
	PairSessionID         string   `json:"pair_session_id"`
	PairSecret            string   `json:"pair_secret"`
	AppDeviceID           string   `json:"app_device_id"`
	AppName               string   `json:"app_name"`
	RequestedCapabilities []string `json:"requested_capabilities"`
}

type PairingPollRequest struct {
	AgentSessionID string `json:"agent_session_id"`
	DeviceID       string `json:"device_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type PairingPollResponse struct {
	Claim *PairingClaim `json:"claim,omitempty"`
}

type PairingResult struct {
	ClaimID      string `json:"claim_id"`
	MachineID    string `json:"machine_id"`
	MachineName  string `json:"machine_name,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Error        string `json:"error,omitempty"`
}

type SubmitPairingResultRequest struct {
	AgentSessionID string        `json:"agent_session_id"`
	DeviceID       string        `json:"device_id"`
	Result         PairingResult `json:"result"`
}
