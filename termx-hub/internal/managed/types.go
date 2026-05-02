package managed

import (
	"context"
	"time"

	"github.com/lozzow/termx/termx-hub/internal/registry"
)

const PathManaged = "managed"

type TicketVerifier interface {
	VerifyManagedTicket(context.Context, VerifyTicketInput) (Ticket, error)
	VerifyOfferTicket(context.Context, registry.OfferTicket) error
}

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

type VerifyTicketInput struct {
	TicketID   string
	MachineID  string
	TerminalID string
}

type Offer struct {
	ID         string
	MachineID  string
	TerminalID string
	TicketID   string
	SDP        string
	Path       string
	AllowRelay bool
	RelayInUse bool
}

type Answer struct {
	ID         string
	OfferID    string
	MachineID  string
	SDP        string
	RelayInUse bool
}

type SubmitOfferInput struct {
	TicketID   string
	MachineID  string
	TerminalID string
	SDP        string
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
}

type GetAnswerInput struct {
	OfferID   string
	TicketID  string
	MachineID string
}
