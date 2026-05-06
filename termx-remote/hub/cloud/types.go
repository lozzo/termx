package cloud

import (
	"time"
)

const PathCloud = "cloud"

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

type OfferPolicy struct {
	MachineID  string
	TerminalID string
	Path       string
	AllowRelay bool
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type Offer struct {
	ID            string
	SessionID     string
	MachineID     string
	TerminalID    string
	SDP           string
	ICECandidates []string
	SessionToken  string
	Path          string
	AllowRelay    bool
	RelayInUse    bool
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
	SessionID     string
	MachineID     string
	TerminalID    string
	SDP           string
	ICECandidates []string
	SessionToken  string
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
	MachineID string
}
