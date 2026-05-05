package cloud

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/registry"
)

var (
	ErrTicketExpired = errors.New("connection ticket expired")
	ErrTicketUsed    = errors.New("connection ticket already used")
	ErrWrongMachine  = errors.New("connection ticket machine mismatch")
	ErrWrongTerminal = errors.New("connection ticket terminal mismatch")
)

type Config struct {
	Registry            *registry.Registry
	Clock               Clock
	OfferTTL            time.Duration
	MaxOffers           int
	AllowRelayByDefault bool
}

type Service struct {
	registry      *registry.Registry
	clock         Clock
	offerTTL      time.Duration
	maxOffers     int
	allowRelay    bool
	mu            sync.Mutex
	offers        map[string]OfferPolicy
	sessionOffers map[string]string
	ticketOffers  map[string]string
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	offerTTL := cfg.OfferTTL
	if offerTTL <= 0 {
		offerTTL = defaultOfferTTL
	}
	maxOffers := cfg.MaxOffers
	if maxOffers <= 0 {
		maxOffers = defaultMaxOffers
	}
	return &Service{
		registry:      cfg.Registry,
		clock:         clock,
		offerTTL:      offerTTL,
		maxOffers:     maxOffers,
		allowRelay:    cfg.AllowRelayByDefault,
		offers:        make(map[string]OfferPolicy),
		sessionOffers: make(map[string]string),
		ticketOffers:  make(map[string]string),
	}
}

func (s *Service) SubmitOffer(ctx context.Context, in SubmitOfferInput) (Offer, error) {
	if s == nil || s.registry == nil {
		return Offer{}, errors.New("cloud service is not configured")
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
	if err := s.registry.PreflightOffer(ctx, preflight); err != nil {
		return Offer{}, err
	}
	ticket := Ticket{
		ID:         preflight.TicketID,
		MachineID:  preflight.MachineID,
		TerminalID: preflight.TerminalID,
		Path:       PathCloud,
		AllowRelay: s.allowRelay,
		ExpiresAt:  s.clock.Now().UTC().Add(s.offerTTL),
	}
	ticketKey := ticketOfferKey(preflight.MachineID, preflight.TicketID)
	s.mu.Lock()
	s.cleanupExpiredLocked(s.clock.Now().UTC())
	s.evictToLimitLocked(s.maxOffers - 1)
	if s.ticketOffers[ticketKey] != "" {
		s.mu.Unlock()
		return Offer{}, ErrTicketUsed
	}
	s.ticketOffers[ticketKey] = "__pending__"
	s.mu.Unlock()
	offer, err := s.registry.SubmitOffer(ctx, registry.OfferInput{
		MachineID:      preflight.MachineID,
		TerminalID:     preflight.TerminalID,
		TicketID:       preflight.TicketID,
		SDP:            in.SDP,
		SessionID:      strings.TrimSpace(in.SessionID),
		ICECandidates:  cloneStrings(in.ICECandidates),
		AppCertificate: cloneBytes(in.AppCertificate),
		Signature:      registrySignature(in.Signature),
	})
	if err != nil {
		s.mu.Lock()
		delete(s.ticketOffers, ticketKey)
		s.mu.Unlock()
		return Offer{}, err
	}
	s.mu.Lock()
	s.offers[offer.ID] = OfferPolicy{Ticket: ticket, CreatedAt: s.clock.Now().UTC()}
	s.ticketOffers[ticketKey] = offer.ID
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
		Signature:      cloudSignature(offer.Signature),
		Path:           PathCloud,
		AllowRelay:     ticket.AllowRelay,
		RelayInUse:     offer.RelayInUse,
	}, nil
}

func (s *Service) ConsumeOfferTicket(ctx context.Context, in GetAnswerInput) (Ticket, error) {
	_ = ctx
	if s == nil {
		return Ticket{}, errors.New("cloud service is not configured")
	}
	ticketID := strings.TrimSpace(in.TicketID)
	machineID := strings.TrimSpace(in.MachineID)
	offerID := strings.TrimSpace(in.OfferID)
	if ticketID == "" || machineID == "" || offerID == "" {
		return Ticket{}, ErrWrongMachine
	}
	s.mu.Lock()
	if mapped := s.sessionOffers[sessionOfferKey(machineID, ticketID, offerID)]; mapped != "" {
		offerID = mapped
	}
	policy, ok := s.offerPolicyLocked(offerID)
	s.mu.Unlock()
	if !ok || policy.Ticket.ID != ticketID || policy.Ticket.MachineID != machineID {
		return Ticket{}, ErrWrongMachine
	}
	return policy.Ticket, nil
}

func (s *Service) PollAgentOffer(ctx context.Context, in PollAgentOfferInput) (Offer, error) {
	if s == nil || s.registry == nil {
		return Offer{}, errors.New("cloud service is not configured")
	}
	offer, err := s.registry.Poll(ctx, registry.PollInput{
		AgentID:   in.AgentID,
		MachineID: in.MachineID,
		Timeout:   in.Timeout,
	})
	if err != nil {
		return Offer{}, err
	}
	result := Offer{
		ID:             offer.ID,
		SessionID:      offer.SessionID,
		MachineID:      offer.MachineID,
		TerminalID:     offer.TerminalID,
		TicketID:       offer.TicketID,
		SDP:            offer.SDP,
		ICECandidates:  cloneStrings(offer.ICECandidates),
		AppCertificate: cloneBytes(offer.AppCertificate),
		Signature:      cloudSignature(offer.Signature),
		Path:           PathCloud,
		RelayInUse:     offer.RelayInUse,
	}
	s.mu.Lock()
	if policy, ok := s.offerPolicyLocked(offer.ID); ok {
		result.AllowRelay = policy.Ticket.AllowRelay
	}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) SubmitAnswer(ctx context.Context, in SubmitAnswerInput) error {
	if s == nil || s.registry == nil {
		return errors.New("cloud service is not configured")
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

func cloudSignature(in registry.OfferSignature) OfferSignature {
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
		return Answer{}, errors.New("cloud service is not configured")
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
	policy, ok := s.offerPolicyLocked(offerID)
	s.mu.Unlock()
	if !ok || policy.Ticket.ID != ticketID || policy.Ticket.MachineID != machineID {
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

func (s *Service) OfferPolicy(offerID string) (OfferPolicy, bool) {
	if s == nil {
		return OfferPolicy{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offerPolicyLocked(offerID)
}

func (s *Service) OfferPolicyForAnswer(in GetAnswerInput) (OfferPolicy, bool) {
	if s == nil {
		return OfferPolicy{}, false
	}
	ticketID := strings.TrimSpace(in.TicketID)
	machineID := strings.TrimSpace(in.MachineID)
	offerID := strings.TrimSpace(in.OfferID)
	if ticketID == "" || machineID == "" || offerID == "" {
		return OfferPolicy{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mapped := s.sessionOffers[sessionOfferKey(machineID, ticketID, offerID)]; mapped != "" {
		offerID = mapped
	}
	policy, ok := s.offerPolicyLocked(offerID)
	if !ok || policy.Ticket.ID != ticketID || policy.Ticket.MachineID != machineID {
		return OfferPolicy{}, false
	}
	return policy, true
}

func (s *Service) CleanupExpired(ctx context.Context) int {
	_ = ctx
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.policyEntryCountLocked()
	s.cleanupExpiredLocked(s.clock.Now().UTC())
	return before - s.policyEntryCountLocked()
}

func (s *Service) offerPolicyLocked(offerID string) (OfferPolicy, bool) {
	s.cleanupExpiredLocked(s.clock.Now().UTC())
	policy, ok := s.offers[strings.TrimSpace(offerID)]
	if !ok {
		return OfferPolicy{}, false
	}
	return policy, true
}

func (s *Service) cleanupExpiredLocked(now time.Time) {
	for offerID, policy := range s.offers {
		if !now.Before(policy.CreatedAt.Add(s.offerTTL)) || (!policy.Ticket.ExpiresAt.IsZero() && !now.Before(policy.Ticket.ExpiresAt)) {
			s.deleteOfferLocked(offerID, policy.Ticket)
		}
	}
}

func (s *Service) evictToLimitLocked(limit int) {
	for limit >= 0 && len(s.offers) > limit {
		var oldestID string
		var oldest OfferPolicy
		for id, policy := range s.offers {
			if oldestID == "" || policy.CreatedAt.Before(oldest.CreatedAt) {
				oldestID = id
				oldest = policy
			}
		}
		if oldestID == "" {
			return
		}
		s.deleteOfferLocked(oldestID, oldest.Ticket)
	}
}

func (s *Service) deleteOfferLocked(offerID string, ticket Ticket) {
	delete(s.offers, offerID)
	delete(s.ticketOffers, ticketOfferKey(ticket.MachineID, ticket.ID))
	for key, mapped := range s.sessionOffers {
		if mapped == offerID {
			delete(s.sessionOffers, key)
		}
	}
}

func (s *Service) policyEntryCountLocked() int {
	return len(s.offers) + len(s.sessionOffers) + len(s.ticketOffers)
}

func sessionOfferKey(machineID, ticketID, sessionID string) string {
	return strings.TrimSpace(machineID) + "\x00" + strings.TrimSpace(ticketID) + "\x00" + strings.TrimSpace(sessionID)
}

func ticketOfferKey(machineID, ticketID string) string {
	return strings.TrimSpace(machineID) + "\x00" + strings.TrimSpace(ticketID)
}
