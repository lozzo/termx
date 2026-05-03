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
	registry       *registry.Registry
	tickets        TicketVerifier
	clock          Clock
	mu             sync.Mutex
	offers         map[string]Ticket
	sessionOffers  map[string]string
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{
		registry:      cfg.Registry,
		tickets:       cfg.Tickets,
		clock:         clock,
		offers:        make(map[string]Ticket),
		sessionOffers: make(map[string]string),
	}
}

func (s *Service) SubmitOffer(ctx context.Context, in SubmitOfferInput) (Offer, error) {
	if s == nil || s.registry == nil || s.tickets == nil {
		return Offer{}, errors.New("managed service is not configured")
	}
	preflight := registry.OfferInput{
		MachineID:      strings.TrimSpace(in.MachineID),
		TerminalID:     strings.TrimSpace(in.TerminalID),
		TicketID:       strings.TrimSpace(in.TicketID),
		SDP:            in.SDP,
		SessionID:      strings.TrimSpace(in.SessionID),
		ICECandidates:  cloneStrings(in.ICECandidates),
		AppCertificate: cloneBytes(in.AppCertificate),
		Signature:      registrySignature(in.Signature),
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
		MachineID:      ticket.MachineID,
		TerminalID:     ticket.TerminalID,
		TicketID:       ticket.ID,
		SDP:            in.SDP,
		SessionID:      strings.TrimSpace(in.SessionID),
		ICECandidates:  cloneStrings(in.ICECandidates),
		AppCertificate: cloneBytes(in.AppCertificate),
		Signature:      registrySignature(in.Signature),
	})
	if err != nil {
		return Offer{}, err
	}
	s.mu.Lock()
	s.offers[offer.ID] = ticket
	if strings.TrimSpace(offer.SessionID) != "" {
		s.sessionOffers[sessionOfferKey(ticket.MachineID, ticket.ID, offer.SessionID)] = offer.ID
	}
	s.mu.Unlock()
	return Offer{
		ID:             offer.ID,
		SessionID:      offer.SessionID,
		MachineID:      offer.MachineID,
		TerminalID:     offer.TerminalID,
		TicketID:       offer.TicketID,
		SDP:            offer.SDP,
		ICECandidates:  cloneStrings(offer.ICECandidates),
		AppCertificate: cloneBytes(offer.AppCertificate),
		Signature:      managedSignature(offer.Signature),
		Path:           PathManaged,
		AllowRelay:     false,
		RelayInUse:     offer.RelayInUse,
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
		ID:             offer.ID,
		SessionID:      offer.SessionID,
		MachineID:      offer.MachineID,
		TerminalID:     offer.TerminalID,
		TicketID:       offer.TicketID,
		SDP:            offer.SDP,
		ICECandidates:  cloneStrings(offer.ICECandidates),
		AppCertificate: cloneBytes(offer.AppCertificate),
		Signature:      managedSignature(offer.Signature),
		Path:           PathManaged,
		RelayInUse:     offer.RelayInUse,
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

func registrySignature(in OfferSignature) registry.OfferSignature {
	return registry.OfferSignature{
		Algorithm: strings.TrimSpace(in.Algorithm),
		Nonce:     strings.TrimSpace(in.Nonce),
		Timestamp: in.Timestamp,
		Value:     strings.TrimSpace(in.Value),
	}
}

func managedSignature(in registry.OfferSignature) OfferSignature {
	return OfferSignature{
		Algorithm: in.Algorithm,
		Nonce:     in.Nonce,
		Timestamp: in.Timestamp,
		Value:     in.Value,
	}
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
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
	offerID := strings.TrimSpace(in.OfferID)
	if mapped := s.sessionOffers[sessionOfferKey(machineID, ticketID, offerID)]; mapped != "" {
		offerID = mapped
	}
	ticket, ok := s.offers[offerID]
	s.mu.Unlock()
	if !ok || ticket.ID != ticketID || ticket.MachineID != machineID {
		return Answer{}, ErrWrongMachine
	}
	answer, err := s.registry.GetAnswer(ctx, offerID)
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

func sessionOfferKey(machineID, ticketID, sessionID string) string {
	return strings.TrimSpace(machineID) + "\x00" + strings.TrimSpace(ticketID) + "\x00" + strings.TrimSpace(sessionID)
}
