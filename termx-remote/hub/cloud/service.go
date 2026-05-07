package cloud

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/registry"
)

var (
	ErrWrongMachine = errors.New("offer machine mismatch")
)

type Config struct {
	Registry                   *registry.Registry
	Clock                      Clock
	OfferTTL                   time.Duration
	MaxOffers                  int
	AllowRelayByDefault        bool
	AllowRelayTransferByDefault bool
}

type Service struct {
	registry           *registry.Registry
	clock              Clock
	offerTTL           time.Duration
	maxOffers          int
	allowRelay         bool
	allowRelayTransfer bool
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
		registry:           cfg.Registry,
		clock:              clock,
		offerTTL:           offerTTL,
		maxOffers:          maxOffers,
		allowRelay:         cfg.AllowRelayByDefault,
		allowRelayTransfer: cfg.AllowRelayTransferByDefault,
	}
}

func (s *Service) SubmitOffer(ctx context.Context, in SubmitOfferInput) (Offer, error) {
	if s == nil || s.registry == nil {
		return Offer{}, errors.New("cloud service is not configured")
	}
	preflight := registry.OfferInput{
		MachineID:            strings.TrimSpace(in.MachineID),
		TerminalID:           strings.TrimSpace(in.TerminalID),
		SDP:                  in.SDP,
		SessionID:            strings.TrimSpace(in.SessionID),
		ICECandidates:        cloneStrings(in.ICECandidates),
		SessionToken:         in.SessionToken,
		AnswerProofChallenge: strings.TrimSpace(in.AnswerProofChallenge),
	}
	if err := s.registry.PreflightOffer(ctx, preflight); err != nil {
		return Offer{}, err
	}
	s.registry.CleanupExpiredOffers(ctx)
	s.registry.EvictOldestOffers(ctx, s.maxOffers-1)
	now := s.clock.Now().UTC()
	offer, err := s.registry.SubmitOffer(ctx, registry.OfferInput{
		MachineID:            preflight.MachineID,
		TerminalID:           preflight.TerminalID,
		SDP:                  in.SDP,
		SessionID:            strings.TrimSpace(in.SessionID),
		ICECandidates:        cloneStrings(in.ICECandidates),
		SessionToken:         in.SessionToken,
		AnswerProofChallenge: preflight.AnswerProofChallenge,
		AllowRelay:           s.allowRelay,
		AllowRelayTransfer:   s.allowRelayTransfer,
		ExpiresAt:            now.Add(s.offerTTL),
	})
	if err != nil {
		return Offer{}, err
	}
	policy := OfferPolicy{
		MachineID:          offer.MachineID,
		TerminalID:         offer.TerminalID,
		Path:               PathCloud,
		AllowRelay:         s.allowRelay,
		AllowRelayTransfer: s.allowRelayTransfer,
		ExpiresAt:          offer.ExpiresAt,
		CreatedAt:          offer.CreatedAt,
	}
	return Offer{
		ID:                   offer.ID,
		SessionID:            offer.SessionID,
		MachineID:            offer.MachineID,
		TerminalID:           offer.TerminalID,
		SDP:                  offer.SDP,
		ICECandidates:        cloneStrings(offer.ICECandidates),
		SessionToken:         offer.SessionToken,
		AnswerProofChallenge: offer.AnswerProofChallenge,
		Path:                 PathCloud,
		AllowRelay:           policy.AllowRelay,
		AllowRelayTransfer:   policy.AllowRelayTransfer,
		RelayInUse:           offer.RelayInUse,
	}, nil
}

func (s *Service) PreflightSession(ctx context.Context, in PreflightSessionInput) (PreflightSession, error) {
	if s == nil || s.registry == nil {
		return PreflightSession{}, errors.New("cloud service is not configured")
	}
	machineID := strings.TrimSpace(in.MachineID)
	if strings.TrimSpace(in.SessionToken) == "" {
		return PreflightSession{}, errors.New("session token is required")
	}
	if err := s.registry.PreflightMachine(ctx, machineID); err != nil {
		return PreflightSession{}, err
	}
	return PreflightSession{
		MachineID:          machineID,
		TerminalID:         strings.TrimSpace(in.TerminalID),
		Path:               PathCloud,
		AllowRelay:         s.allowRelay,
		AllowRelayTransfer: s.allowRelayTransfer,
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
		ID:                   offer.ID,
		SessionID:            offer.SessionID,
		MachineID:            offer.MachineID,
		TerminalID:           offer.TerminalID,
		SDP:                  offer.SDP,
		ICECandidates:        cloneStrings(offer.ICECandidates),
		SessionToken:         offer.SessionToken,
		AnswerProofChallenge: offer.AnswerProofChallenge,
		Path:                 PathCloud,
		AllowRelay:           offer.AllowRelay,
		RelayInUse:           offer.RelayInUse,
	}
	return result, nil
}

func (s *Service) SubmitAnswer(ctx context.Context, in SubmitAnswerInput) error {
	if s == nil || s.registry == nil {
		return errors.New("cloud service is not configured")
	}
	machineID := strings.TrimSpace(in.MachineID)
	offerID := strings.TrimSpace(in.OfferID)
	_, err := s.registry.SubmitAnswer(ctx, registry.AnswerInput{
		AgentID:     in.AgentID,
		MachineID:   machineID,
		OfferID:     offerID,
		SDP:         in.SDP,
		Error:       in.Error,
		AnswerProof: in.AnswerProof,
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
	offerID := strings.TrimSpace(in.OfferID)
	offer, ok := s.registry.Offer(ctx, registry.OfferLookupInput{MachineID: machineID, OfferID: offerID})
	if !ok || offer.MachineID != machineID {
		return Answer{}, ErrWrongMachine
	}
	answer, err := s.registry.GetAnswerForOffer(ctx, registry.OfferLookupInput{MachineID: machineID, OfferID: offerID})
	if err != nil {
		return Answer{}, err
	}
	if answer.MachineID != machineID {
		return Answer{}, ErrWrongMachine
	}
	return Answer{
		ID:          answer.ID,
		OfferID:     answer.OfferID,
		MachineID:   answer.MachineID,
		SDP:         answer.SDP,
		Error:       answer.Error,
		AnswerProof: answer.AnswerProof,
	}, nil
}

func (s *Service) OfferPolicy(offerID string) (OfferPolicy, bool) {
	if s == nil || s.registry == nil {
		return OfferPolicy{}, false
	}
	offer, ok := s.registry.Offer(context.Background(), registry.OfferLookupInput{MachineID: "", OfferID: offerID})
	return offerPolicyFromRegistry(offer), ok
}

func (s *Service) OfferPolicyForAnswer(in GetAnswerInput) (OfferPolicy, bool) {
	if s == nil || s.registry == nil {
		return OfferPolicy{}, false
	}
	machineID := strings.TrimSpace(in.MachineID)
	offerID := strings.TrimSpace(in.OfferID)
	if machineID == "" || offerID == "" {
		return OfferPolicy{}, false
	}
	offer, ok := s.registry.Offer(context.Background(), registry.OfferLookupInput{MachineID: machineID, OfferID: offerID})
	if !ok || offer.MachineID != machineID {
		return OfferPolicy{}, false
	}
	return offerPolicyFromRegistry(offer), true
}

func (s *Service) CleanupExpired(ctx context.Context) int {
	if s == nil || s.registry == nil {
		return 0
	}
	return s.registry.CleanupExpiredOffers(ctx)
}

func offerPolicyFromRegistry(offer registry.Offer) OfferPolicy {
	return OfferPolicy{
		MachineID:          offer.MachineID,
		TerminalID:         offer.TerminalID,
		Path:               PathCloud,
		AllowRelay:         offer.AllowRelay,
		AllowRelayTransfer: offer.AllowRelayTransfer,
		ExpiresAt:          offer.ExpiresAt,
		CreatedAt:          offer.CreatedAt,
	}
}
