package hubv1

type RTCIceServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type RelayPolicy struct {
	AllowRelay         bool `json:"allow_relay"`
	AllowRelayTransfer bool `json:"allow_relay_transfer"`
}

type SignalingOffer struct {
	SessionID            string   `json:"session_id"`
	MachineID            string   `json:"machine_id"`
	TerminalID           string   `json:"terminal_id,omitempty"`
	Path                 string   `json:"path,omitempty"`
	SDP                  string   `json:"sdp"`
	Candidates           []string `json:"ice_candidates,omitempty"`
	SessionToken         string   `json:"session_token"`
	AnswerProofChallenge string   `json:"answer_proof_challenge,omitempty"`
}

type SignalingAnswer struct {
	SessionID     string   `json:"session_id"`
	SDP           string   `json:"sdp"`
	Error         string   `json:"error,omitempty"`
	AnswerProof   string   `json:"answer_proof,omitempty"`
	ICECandidates []string `json:"ice_candidates"`
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

type PairingResult struct {
	ClaimID      string `json:"claim_id"`
	MachineID    string `json:"machine_id"`
	MachineName  string `json:"machine_name,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Error        string `json:"error,omitempty"`
}
