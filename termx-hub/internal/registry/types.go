package registry

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	AgentOnline = "online"
	PathManaged = "managed"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type AuthorityVerifier interface {
	VerifyAgentRegistration(context.Context, AgentRegistration) error
	VerifyOfferTicket(context.Context, OfferTicket) error
}

type AgentRegistration struct {
	MachineID          string
	AgentID            string
	SignatureAlgorithm string
	SignatureNonce     string
	SignatureTimestamp int64
	SignatureValue     string
}

type OfferTicket struct {
	MachineID  string
	TerminalID string
	TicketID   string
}

type Terminal struct {
	ID    string
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
	ID              string
	SessionID       string
	MachineID       string
	TerminalID      string
	TicketID        string
	SDP             string
	ICECandidates   []string
	AppCertificate  json.RawMessage
	Signature       OfferSignature
	Path            string
	RelayInUse      bool
	AssignedAgentID string
	DeliveredAt     time.Time
	CreatedAt       time.Time
}

func (o Offer) ContainsRuntimePayload() bool {
	return containsRuntimePayload(o.SDP)
}

type Answer struct {
	ID        string
	OfferID   string
	AgentID   string
	MachineID string
	SDP       string
	CreatedAt time.Time
}

type RegisterInput struct {
	MachineID          string
	AgentID            string
	SignatureAlgorithm string
	SignatureNonce     string
	SignatureTimestamp int64
	SignatureValue     string
	Terminals          []Terminal
}

type HeartbeatInput struct {
	AgentID   string
	MachineID string
}

type PollInput struct {
	AgentID   string
	MachineID string
	Timeout   time.Duration
}

type OfferInput struct {
	SessionID      string
	MachineID      string
	TerminalID     string
	TicketID       string
	SDP            string
	ICECandidates  []string
	AppCertificate json.RawMessage
	Signature      OfferSignature
}

type AnswerInput struct {
	AgentID   string
	MachineID string
	OfferID   string
	SDP       string
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

type OfferSignature struct {
	Algorithm string
	Nonce     string
	Timestamp int64
	Value     string
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
