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
	ErrWrongMachine = errors.New("offer machine mismatch")
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
	}
}

func (s *Service) SubmitOffer(ctx context.Context, in SubmitOfferInput) (Offer, error) {
	if s == nil || s.registry == nil {
		return Offer{}, errors.New("cloud service is not configured")
	}
	preflight := registry.OfferInput{
		MachineID:     strings.TrimSpace(in.MachineID),
		TerminalID:    strings.TrimSpace(in.TerminalID),
		SDP:           in.SDP,
		SessionID:     strings.TrimSpace(in.SessionID),
		ICECandidates: cloneStrings(in.ICECandidates),
		SessionToken:  in.SessionToken,
	}
	if err := s.registry.PreflightOffer(ctx, preflight); err != nil {
		return Offer{}, err
	}
	s.mu.Lock()
	s.cleanupExpiredLocked(s.clock.Now().UTC())
	s.evictToLimitLocked(s.maxOffers - 1)
	s.mu.Unlock()
	offer, err := s.registry.SubmitOffer(ctx, registry.OfferInput{
		MachineID:     preflight.MachineID,
		TerminalID:    preflight.TerminalID,
		SDP:           in.SDP,
		SessionID:     strings.TrimSpace(in.SessionID),
		ICECandidates: cloneStrings(in.ICECandidates),
		SessionToken:  in.SessionToken,
	})
	if err != nil {
		return Offer{}, err
	}
	policy := OfferPolicy{
		MachineID:  offer.MachineID,
		TerminalID: offer.TerminalID,
		Path:       PathCloud,
		AllowRelay: s.allowRelay,
		ExpiresAt:  s.clock.Now().UTC().Add(s.offerTTL),
		CreatedAt:  s.clock.Now().UTC(),
	}
	s.mu.Lock()
	s.offers[offer.ID] = policy
	if strings.TrimSpace(offer.SessionID) != "" {
		s.sessionOffers[sessionOfferKey(policy.MachineID, offer.SessionID)] = offer.ID
	}
	s.mu.Unlock()
	return Offer{
		ID:            offer.ID,
		SessionID:     offer.SessionID,
		MachineID:     offer.MachineID,
		TerminalID:    offer.TerminalID,
		SDP:           offer.SDP,
		ICECandidates: cloneStrings(offer.ICECandidates),
		SessionToken:  offer.SessionToken,
		Path:          PathCloud,
		AllowRelay:    policy.AllowRelay,
		RelayInUse:    offer.RelayInUse,
	}, nil
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
		ID:            offer.ID,
		SessionID:     offer.SessionID,
		MachineID:     offer.MachineID,
		TerminalID:    offer.TerminalID,
		SDP:           offer.SDP,
		ICECandidates: cloneStrings(offer.ICECandidates),
		SessionToken:  offer.SessionToken,
		Path:          PathCloud,
		RelayInUse:    offer.RelayInUse,
	}
	s.mu.Lock()
	if policy, ok := s.offerPolicyLocked(offer.ID); ok {
		result.AllowRelay = policy.AllowRelay
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
		Error:     in.Error,
	})
	return err
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func (s *Service) GetAnswer(ctx context.Context, in GetAnswerInput) (Answer, error) {
	if s == nil || s.registry == nil {
		return Answer{}, errors.New("cloud service is not configured")
	}
	machineID := strings.TrimSpace(in.MachineID)
	if machineID == "" {
		return Answer{}, ErrWrongMachine
	}
	s.mu.Lock()
	offerID := strings.TrimSpace(in.OfferID)
	if mapped := s.sessionOffers[sessionOfferKey(machineID, offerID)]; mapped != "" {
		offerID = mapped
	}
	policy, ok := s.offerPolicyLocked(offerID)
	s.mu.Unlock()
	if !ok || policy.MachineID != machineID {
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
		Error:     answer.Error,
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
	machineID := strings.TrimSpace(in.MachineID)
	offerID := strings.TrimSpace(in.OfferID)
	if machineID == "" || offerID == "" {
		return OfferPolicy{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mapped := s.sessionOffers[sessionOfferKey(machineID, offerID)]; mapped != "" {
		offerID = mapped
	}
	policy, ok := s.offerPolicyLocked(offerID)
	if !ok || policy.MachineID != machineID {
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
		if !now.Before(policy.CreatedAt.Add(s.offerTTL)) || (!policy.ExpiresAt.IsZero() && !now.Before(policy.ExpiresAt)) {
			s.deleteOfferLocked(offerID)
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
		s.deleteOfferLocked(oldestID)
	}
}

func (s *Service) deleteOfferLocked(offerID string) {
	delete(s.offers, offerID)
	for key, mapped := range s.sessionOffers {
		if mapped == offerID {
			delete(s.sessionOffers, key)
		}
	}
}

func (s *Service) policyEntryCountLocked() int {
	return len(s.offers) + len(s.sessionOffers)
}

func sessionOfferKey(machineID, sessionID string) string {
	return strings.TrimSpace(machineID) + "\x00" + strings.TrimSpace(sessionID)
}
