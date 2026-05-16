package registry

import (
	"strings"
	"time"
)

const (
	AgentOnline = "online"
	PathCloud   = "cloud"
	PathLocal   = "local"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type AgentRegistration struct {
	MachineID string
	AgentID   string
}

type Terminal struct {
	ID    string
	Name  string
	State string
}

type Agent struct {
	ID         string
	MachineID  string
	Status     string
	Path       string
	RelayInUse bool
	Terminals  []Terminal
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

type Offer struct {
	ID                   string
	SessionID            string
	MachineID            string
	TerminalID           string
	SDP                  string
	ICECandidates        []string
	SessionToken         string
	AnswerProofChallenge string
	Path                 string
	AllowRelay           bool
	AllowRelayTransfer   bool
	RelayInUse           bool
	AssignedAgentID      string
	DeliveredAt          time.Time
	CreatedAt            time.Time
	ExpiresAt            time.Time
}

func (o Offer) ContainsRuntimePayload() bool {
	return containsRuntimePayload(o.SDP)
}

type Answer struct {
	ID          string
	OfferID     string
	AgentID     string
	MachineID   string
	SDP         string
	Error       string
	AnswerProof string
	CreatedAt   time.Time
}

type PairingClaim struct {
	ID                    string
	MachineID             string
	PairSessionID         string
	PairSecret            string
	AppDeviceID           string
	AppName               string
	RequestedCapabilities []string
	AllowedPaths          []string
	AssignedAgentID       string
	DeliveredAt           time.Time
	CreatedAt             time.Time
}

type PairingResult struct {
	ClaimID      string
	MachineID    string
	MachineName  string
	SessionToken string
	ExpiresAt    string
	Error        string
	CreatedAt    time.Time
}

type RegisterInput struct {
	MachineID string
	AgentID   string
	Terminals []Terminal
}

type HeartbeatInput struct {
	AgentID   string
	MachineID string
	Terminals []Terminal
}

type ForceOfflineInput struct {
	MachineID string
	AgentID   string
	Reason    string
	TTL       time.Duration
}

type PollInput struct {
	AgentID   string
	MachineID string
	Timeout   time.Duration
}

type OfferInput struct {
	SessionID            string
	MachineID            string
	TerminalID           string
	Path                 string
	SDP                  string
	ICECandidates        []string
	SessionToken         string
	AnswerProofChallenge string
	AllowRelay           bool
	AllowRelayTransfer   bool
	ExpiresAt            time.Time
}

type AnswerInput struct {
	AgentID     string
	MachineID   string
	OfferID     string
	SDP         string
	Error       string
	AnswerProof string
}

type OfferLookupInput struct {
	MachineID string
	OfferID   string
}

type PairingClaimInput struct {
	MachineID             string
	PairSessionID         string
	PairSecret            string
	AppDeviceID           string
	AppName               string
	RequestedCapabilities []string
	AllowedPaths          []string
}

type PairingPollInput struct {
	AgentID   string
	MachineID string
	Timeout   time.Duration
}

type PairingResultInput struct {
	AgentID      string
	MachineID    string
	ClaimID      string
	MachineName  string
	SessionToken string
	ExpiresAt    string
	Error        string
}

func containsRuntimePayload(payload string) bool {
	lower := strings.ToLower(payload)
	for _, marker := range []string{
		"terminal_data",
		"file_data",
		"api_data",
		"events_data",
		"terminal:",
		"file:",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isBasicSDP(payload string) bool {
	required := map[string]bool{
		"v=": false,
		"o=": false,
		"s=": false,
		"t=": false,
		"m=": false,
	}
	for _, line := range strings.FieldsFunc(payload, func(r rune) bool {
		return r == '\n' || r == '\r'
	}) {
		line = strings.TrimSpace(line)
		for prefix := range required {
			if strings.HasPrefix(line, prefix) {
				required[prefix] = true
			}
		}
	}
	for _, ok := range required {
		if !ok {
			return false
		}
	}
	return true
}
