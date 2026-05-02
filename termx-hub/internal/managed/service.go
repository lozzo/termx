package managed

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-hub/internal/registry"
)

var (
	ErrTicketExpired = errors.New("managed ticket expired")
	ErrTicketUsed    = errors.New("managed ticket already used")
	ErrWrongMachine  = errors.New("managed ticket machine mismatch")
	ErrWrongTerminal = errors.New("managed ticket terminal mismatch")
)

type Config struct {
	Registry *registry.Registry
	Tickets  TicketVerifier
	Clock    Clock
}

type Service struct {
	registry *registry.Registry
	tickets  TicketVerifier
	clock    Clock
	mu       sync.Mutex
	offers   map[string]Ticket
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{registry: cfg.Registry, tickets: cfg.Tickets, clock: clock, offers: make(map[string]Ticket)}
}

func (s *Service) SubmitOffer(ctx context.Context, in SubmitOfferInput) (Offer, error) {
	if s == nil || s.registry == nil || s.tickets == nil {
		return Offer{}, errors.New("managed service is not configured")
	}
	preflight := registry.OfferInput{
		MachineID:  strings.TrimSpace(in.MachineID),
		TerminalID: strings.TrimSpace(in.TerminalID),
		TicketID:   strings.TrimSpace(in.TicketID),
		SDP:        in.SDP,
	}
	if err := s.tickets.VerifyOfferTicket(ctx, registry.OfferTicket{
		MachineID:  preflight.MachineID,
		TerminalID: preflight.TerminalID,
		TicketID:   preflight.TicketID,
	}); err != nil {
		return Offer{}, err
	}
	if err := s.registry.PreflightOffer(ctx, preflight); err != nil {
		return Offer{}, err
	}
	ticket, err := s.tickets.VerifyManagedTicket(ctx, VerifyTicketInput{
		TicketID:   preflight.TicketID,
		MachineID:  preflight.MachineID,
		TerminalID: preflight.TerminalID,
	})
	if err != nil {
		return Offer{}, err
	}
	if ticket.Path != PathManaged {
		return Offer{}, ErrWrongMachine
	}
	if strings.TrimSpace(in.TerminalID) != "" && ticket.TerminalID != strings.TrimSpace(in.TerminalID) {
		return Offer{}, ErrWrongTerminal
	}
	if !ticket.ExpiresAt.IsZero() && !s.clock.Now().UTC().Before(ticket.ExpiresAt) {
		return Offer{}, ErrTicketExpired
	}
	offer, err := s.registry.SubmitOffer(ctx, registry.OfferInput{
		MachineID:  ticket.MachineID,
		TerminalID: ticket.TerminalID,
		TicketID:   ticket.ID,
		SDP:        in.SDP,
	})
	if err != nil {
		return Offer{}, err
	}
	s.mu.Lock()
	s.offers[offer.ID] = ticket
	s.mu.Unlock()
	return Offer{
		ID:         offer.ID,
		MachineID:  offer.MachineID,
		TerminalID: offer.TerminalID,
		TicketID:   offer.TicketID,
		SDP:        offer.SDP,
		Path:       PathManaged,
		AllowRelay: false,
		RelayInUse: offer.RelayInUse,
	}, nil
}

func (s *Service) PollAgentOffer(ctx context.Context, in PollAgentOfferInput) (Offer, error) {
	if s == nil || s.registry == nil {
		return Offer{}, errors.New("managed service is not configured")
	}
	offer, err := s.registry.Poll(ctx, registry.PollInput{
		AgentID:   in.AgentID,
		MachineID: in.MachineID,
		Timeout:   in.Timeout,
	})
	if err != nil {
		return Offer{}, err
	}
	return Offer{
		ID:         offer.ID,
		MachineID:  offer.MachineID,
		TerminalID: offer.TerminalID,
		TicketID:   offer.TicketID,
		SDP:        offer.SDP,
		Path:       PathManaged,
		RelayInUse: offer.RelayInUse,
	}, nil
}

func (s *Service) SubmitAnswer(ctx context.Context, in SubmitAnswerInput) error {
	if s == nil || s.registry == nil {
		return errors.New("managed service is not configured")
	}
	_, err := s.registry.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:   in.AgentID,
		MachineID: in.MachineID,
		OfferID:   in.OfferID,
		SDP:       in.SDP,
	})
	return err
}

func (s *Service) GetAnswer(ctx context.Context, in GetAnswerInput) (Answer, error) {
	if s == nil || s.registry == nil {
		return Answer{}, errors.New("managed service is not configured")
	}
	ticketID := strings.TrimSpace(in.TicketID)
	machineID := strings.TrimSpace(in.MachineID)
	if ticketID == "" || machineID == "" {
		return Answer{}, ErrWrongMachine
	}
	s.mu.Lock()
	ticket, ok := s.offers[strings.TrimSpace(in.OfferID)]
	s.mu.Unlock()
	if !ok || ticket.ID != ticketID || ticket.MachineID != machineID {
		return Answer{}, ErrWrongMachine
	}
	answer, err := s.registry.GetAnswer(ctx, in.OfferID)
	if err != nil {
		return Answer{}, err
	}
	if answer.MachineID != machineID {
		return Answer{}, ErrWrongMachine
	}
	return Answer{
		ID:        answer.ID,
		OfferID:   answer.OfferID,
		MachineID: answer.MachineID,
		SDP:       answer.SDP,
	}, nil
}
