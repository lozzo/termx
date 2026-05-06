package cloud

import (
	"encoding/json"
	"time"
)

const PathCloud = "cloud"

// PathManaged is kept as a compatibility alias for older tests and clients.
const PathManaged = PathCloud

const (
	defaultOfferTTL  = 5 * time.Minute
	defaultMaxOffers = 4096
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type Ticket struct {
	ID         string
	MachineID  string
	TerminalID string
	Path       string
	AllowRelay bool
	ExpiresAt  time.Time
}

type OfferPolicy struct {
	Ticket    Ticket
	CreatedAt time.Time
}

type ConnectionTicketInput struct {
	TicketID   string
	MachineID  string
	TerminalID string
}

// VerifyTicketInput is kept as a compatibility alias for older internal callers.
type VerifyTicketInput = ConnectionTicketInput

type Offer struct {
	ID             string
	SessionID      string
	MachineID      string
	TerminalID     string
	TicketID       string
	SDP            string
	ICECandidates  []string
	AppCertificate json.RawMessage
	Signature      OfferSignature
	Path           string
	AllowRelay     bool
	RelayInUse     bool
}

type Answer struct {
	ID         string
	OfferID    string
	MachineID  string
	SDP        string
	Error      string
	RelayInUse bool
}

type SubmitOfferInput struct {
	SessionID      string
	TicketID       string
	MachineID      string
	TerminalID     string
	SDP            string
	ICECandidates  []string
	AppCertificate json.RawMessage
	Signature      OfferSignature
}

type PollAgentOfferInput struct {
	AgentID   string
	MachineID string
	Timeout   time.Duration
}

type SubmitAnswerInput struct {
	AgentID   string
	MachineID string
	OfferID   string
	SDP       string
	Error     string
}

type GetAnswerInput struct {
	OfferID   string
	TicketID  string
	MachineID string
}

type OfferSignature struct {
	Algorithm string
	Nonce     string
	Timestamp int64
	Value     string
}
