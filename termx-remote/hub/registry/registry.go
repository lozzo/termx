package registry

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrPollTimeout          = errors.New("poll timeout")
	ErrUnauthorizedAgent    = errors.New("unauthorized agent")
	ErrAgentNotFound        = errors.New("agent not found")
	ErrOfferNotFound        = errors.New("offer not found")
	ErrPairingClaimNotFound = errors.New("pairing claim not found")
	ErrOfferNotAssigned     = errors.New("offer not assigned")
	ErrRuntimePayload       = errors.New("runtime payload is not allowed in signaling")
	ErrPayloadTooLarge      = errors.New("signaling payload too large")
	ErrInvalidSDP           = errors.New("invalid sdp")
	ErrAgentRebound         = errors.New("agent already belongs to another machine")
	ErrAnswerAlreadyExists  = errors.New("answer already exists")
	ErrAgentForcedOffline   = errors.New("agent forced offline")
)

type Config struct {
	Clock        Clock
	AgentTTL     time.Duration
	SignalingTTL time.Duration
	MaxSDPBytes  int
}

type Registry struct {
	mu             sync.Mutex
	clock          Clock
	agentTTL       time.Duration
	signalingTTL   time.Duration
	maxSDPBytes    int
	agents         map[string]Agent
	machineAgents  map[string]map[string]struct{} // machineID → set of agentIDs
	answers        map[string]Answer
	offerQueue     signalQueue[Offer]
	pairingResults map[string]PairingResult
	pairingQueue   signalQueue[PairingClaim]
	forced         map[string]forceOfflineState
}

type forceOfflineState struct {
	MachineID string
	AgentID   string
	Reason    string
	ExpiresAt time.Time
}

func New(cfg Config) *Registry {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	ttl := cfg.AgentTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	signalingTTL := cfg.SignalingTTL
	if signalingTTL <= 0 {
		signalingTTL = 5 * time.Minute
	}
	maxSDPBytes := cfg.MaxSDPBytes
	if maxSDPBytes <= 0 {
		maxSDPBytes = 256 * 1024
	}
	return &Registry{
		clock:         clock,
		agentTTL:      ttl,
		signalingTTL:  signalingTTL,
		maxSDPBytes:   maxSDPBytes,
		agents:        make(map[string]Agent),
		machineAgents: make(map[string]map[string]struct{}),
		answers:       make(map[string]Answer),
		offerQueue: newSignalQueue(func(offer Offer) string {
			return offer.ID
		}),
		pairingResults: make(map[string]PairingResult),
		pairingQueue: newSignalQueue(func(claim PairingClaim) string {
			return claim.ID
		}),
		forced: make(map[string]forceOfflineState),
	}
}

func (r *Registry) Register(ctx context.Context, in RegisterInput) (Agent, error) {
	machineID := strings.TrimSpace(in.MachineID)
	agentID := strings.TrimSpace(in.AgentID)
	if machineID == "" || agentID == "" {
		return Agent{}, errors.New("machine id and agent id are required")
	}
	now := r.clock.Now().UTC()
	if r.forceOfflineActive(machineID, agentID, now) {
		return Agent{}, ErrAgentForcedOffline
	}
	agent := Agent{
		ID:         agentID,
		MachineID:  machineID,
		Status:     AgentOnline,
		Path:       PathHub,
		Terminals:  cloneTerminals(in.Terminals),
		LastSeenAt: now,
		ExpiresAt:  now.Add(r.agentTTL),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, exists := r.agents[agent.ID]
	if exists && existing.MachineID != agent.MachineID {
		return Agent{}, ErrAgentRebound
	}
	r.agents[agent.ID] = agent
	r.indexAgentLocked(agent.MachineID, agent.ID)
	return cloneAgent(agent), nil
}

func (r *Registry) Heartbeat(ctx context.Context, in HeartbeatInput) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	agentID := strings.TrimSpace(in.AgentID)
	machineID := strings.TrimSpace(in.MachineID)
	if r.forceOfflineActiveLocked(machineID, agentID, r.clock.Now().UTC()) {
		if a, ok := r.agents[agentID]; ok {
			r.unindexAgentLocked(a.MachineID, agentID)
		}
		delete(r.agents, agentID)
		return ErrAgentForcedOffline
	}
	agent, ok := r.agents[agentID]
	if !ok || r.expired(agent) {
		return ErrAgentNotFound
	}
	if agent.MachineID != machineID {
		return ErrUnauthorizedAgent
	}
	now := r.clock.Now().UTC()
	agent.Status = AgentOnline
	agent.Terminals = cloneTerminals(in.Terminals)
	agent.LastSeenAt = now
	agent.ExpiresAt = now.Add(r.agentTTL)
	r.agents[agent.ID] = agent
	return nil
}

func (r *Registry) GetAgent(agentID string) (Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	agent, ok := r.agents[strings.TrimSpace(agentID)]
	if !ok || r.expired(agent) || r.forceOfflineActiveLocked(agent.MachineID, agent.ID, r.clock.Now().UTC()) {
		return Agent{}, false
	}
	return cloneAgent(agent), true
}

func (r *Registry) Agents() []Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now().UTC()
	out := make([]Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		if r.expired(agent) || r.forceOfflineActiveLocked(agent.MachineID, agent.ID, now) {
			continue
		}
		out = append(out, cloneAgent(agent))
	}
	return out
}

func (r *Registry) CleanupExpired(ctx context.Context) int {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for id, agent := range r.agents {
		if r.expired(agent) || r.forceOfflineActiveLocked(agent.MachineID, agent.ID, r.clock.Now().UTC()) {
			r.unindexAgentLocked(agent.MachineID, id)
			delete(r.agents, id)
			removed++
		}
	}
	for key, forced := range r.forced {
		if !r.clock.Now().UTC().Before(forced.ExpiresAt) {
			delete(r.forced, key)
			removed++
		}
	}
	for _, offer := range r.offerQueue.itemsSnapshot() {
		id := offer.ID
		if r.offerExpired(offer) {
			if _, ok := r.answers[id]; ok {
				removed++
			}
			r.deleteOfferLocked(id)
			removed++
			continue
		}
		if offer.AssignedAgentID != "" {
			if agent, ok := r.agents[offer.AssignedAgentID]; !ok || r.expired(agent) {
				if _, answered := r.answers[id]; !answered {
					offer.AssignedAgentID = ""
					offer.DeliveredAt = time.Time{}
					r.offerQueue.set(offer)
					r.offerQueue.enqueue(offer.MachineID, offer)
				}
			}
		}
	}
	r.pruneQueuesLocked()
	for offerID, answer := range r.answers {
		offer, ok := r.offerQueue.get(offerID)
		if !ok || r.offerExpired(offer) {
			delete(r.answers, offerID)
			removed++
			continue
		}
		if r.signalingExpired(answer.CreatedAt) {
			delete(r.answers, offerID)
			removed++
		}
	}
	for _, claim := range r.pairingQueue.itemsSnapshot() {
		id := claim.ID
		if r.signalingExpired(claim.CreatedAt) {
			r.pairingQueue.delete(id)
			if _, ok := r.pairingResults[id]; ok {
				delete(r.pairingResults, id)
			}
			removed++
		}
	}
	r.prunePairingQueuesLocked()
	return removed
}

func (r *Registry) ForceOffline(in ForceOfflineInput) {
	machineID := strings.TrimSpace(in.MachineID)
	agentID := strings.TrimSpace(in.AgentID)
	if machineID == "" || agentID == "" {
		return
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = r.agentTTL
	}
	now := r.clock.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forced[forceOfflineKey(machineID, agentID)] = forceOfflineState{
		MachineID: machineID,
		AgentID:   agentID,
		Reason:    strings.TrimSpace(in.Reason),
		ExpiresAt: now.Add(ttl),
	}
	r.unindexAgentLocked(machineID, agentID)
	delete(r.agents, agentID)
	r.dropQueuedOffersLocked(machineID)
	r.notifyLocked(machineID)
}

func (r *Registry) SubmitOffer(ctx context.Context, in OfferInput) (Offer, error) {
	machineID := strings.TrimSpace(in.MachineID)
	terminalID := strings.TrimSpace(in.TerminalID)
	if err := r.PreflightOffer(ctx, OfferInput{
		MachineID:            machineID,
		TerminalID:           terminalID,
		Path:                 in.Path,
		SDP:                  in.SDP,
		SessionID:            strings.TrimSpace(in.SessionID),
		ICECandidates:        cloneStrings(in.ICECandidates),
		SessionToken:         in.SessionToken,
		AnswerProofChallenge: in.AnswerProofChallenge,
		AllowRelay:           in.AllowRelay,
		AllowRelayTransfer:   in.AllowRelayTransfer,
		ExpiresAt:            in.ExpiresAt,
	}); err != nil {
		return Offer{}, err
	}
	return r.submitVerifiedOffer(ctx, OfferInput{
		MachineID:            machineID,
		TerminalID:           terminalID,
		Path:                 in.Path,
		SDP:                  in.SDP,
		SessionID:            strings.TrimSpace(in.SessionID),
		ICECandidates:        cloneStrings(in.ICECandidates),
		SessionToken:         in.SessionToken,
		AnswerProofChallenge: in.AnswerProofChallenge,
		AllowRelay:           in.AllowRelay,
		AllowRelayTransfer:   in.AllowRelayTransfer,
		ExpiresAt:            in.ExpiresAt,
	})
}

func (r *Registry) PreflightOffer(ctx context.Context, in OfferInput) error {
	_ = ctx
	machineID := strings.TrimSpace(in.MachineID)
	sdp := strings.TrimSpace(in.SDP)
	if machineID == "" || sdp == "" {
		return errors.New("machine id and sdp are required")
	}
	if err := r.validateSignalingPayload(sdp); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(machineID) {
		if r.machineHasActiveForceOfflineLocked(machineID, r.clock.Now().UTC()) {
			return ErrAgentForcedOffline
		}
		return ErrAgentNotFound
	}
	return nil
}

func (r *Registry) PreflightMachine(ctx context.Context, machineID string) error {
	_ = ctx
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return errors.New("machine id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(machineID) {
		if r.machineHasActiveForceOfflineLocked(machineID, r.clock.Now().UTC()) {
			return ErrAgentForcedOffline
		}
		return ErrAgentNotFound
	}
	return nil
}

func (r *Registry) submitVerifiedOffer(ctx context.Context, in OfferInput) (Offer, error) {
	_ = ctx
	offer := Offer{
		ID:                   randomID("offer"),
		SessionID:            strings.TrimSpace(in.SessionID),
		MachineID:            strings.TrimSpace(in.MachineID),
		TerminalID:           strings.TrimSpace(in.TerminalID),
		SDP:                  in.SDP,
		ICECandidates:        cloneStrings(in.ICECandidates),
		SessionToken:         strings.TrimSpace(in.SessionToken),
		AnswerProofChallenge: strings.TrimSpace(in.AnswerProofChallenge),
		Path:                 normalizeOfferPath(in.Path),
		AllowRelay:           in.AllowRelay,
		AllowRelayTransfer:   in.AllowRelayTransfer,
		CreatedAt:            r.clock.Now().UTC(),
		ExpiresAt:            in.ExpiresAt.UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(offer.MachineID) {
		if r.machineHasActiveForceOfflineLocked(offer.MachineID, r.clock.Now().UTC()) {
			return Offer{}, ErrAgentForcedOffline
		}
		return Offer{}, ErrAgentNotFound
	}
	r.offerQueue.enqueue(offer.MachineID, offer)
	r.offerQueue.notify(offer.MachineID)
	return cloneOffer(offer), nil
}

func normalizeOfferPath(path string) string {
	switch strings.TrimSpace(path) {
	case PathLocal:
		return PathLocal
	default:
		return PathHub
	}
}

func (r *Registry) Poll(ctx context.Context, in PollInput) (Offer, error) {
	agentID := strings.TrimSpace(in.AgentID)
	machineID := strings.TrimSpace(in.MachineID)
	if agentID == "" || machineID == "" {
		return Offer{}, ErrUnauthorizedAgent
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		r.mu.Lock()
		agent, ok := r.agents[agentID]
		if r.forceOfflineActiveLocked(machineID, agentID, r.clock.Now().UTC()) {
			if ok {
				r.unindexAgentLocked(agent.MachineID, agentID)
			}
			delete(r.agents, agentID)
			r.mu.Unlock()
			return Offer{}, ErrAgentForcedOffline
		}
		if !ok || r.expired(agent) {
			r.mu.Unlock()
			return Offer{}, ErrAgentNotFound
		}
		if agent.MachineID != machineID {
			r.mu.Unlock()
			return Offer{}, ErrUnauthorizedAgent
		}
		for {
			offer, ok := r.offerQueue.dequeue(machineID)
			if ok {
				if r.expireOfferLocked(offer.ID, offer) {
					continue
				}
				offer.AssignedAgentID = agent.ID
				offer.DeliveredAt = r.clock.Now().UTC()
				r.offerQueue.set(offer)
				r.mu.Unlock()
				return cloneOffer(offer), nil
			}
			break
		}
		waiter := r.offerQueue.addWaiter(machineID)
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			r.removeOfferWaiter(machineID, waiter)
			return Offer{}, ctx.Err()
		case <-timer.C:
			r.removeOfferWaiter(machineID, waiter)
			return Offer{}, ErrPollTimeout
		case <-waiter:
		}
	}
}

func (r *Registry) SubmitAnswer(ctx context.Context, in AnswerInput) (Answer, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	agent, ok := r.agents[strings.TrimSpace(in.AgentID)]
	if r.forceOfflineActiveLocked(strings.TrimSpace(in.MachineID), strings.TrimSpace(in.AgentID), r.clock.Now().UTC()) {
		if ok {
			r.unindexAgentLocked(agent.MachineID, strings.TrimSpace(in.AgentID))
		}
		delete(r.agents, strings.TrimSpace(in.AgentID))
		return Answer{}, ErrAgentForcedOffline
	}
	if !ok || r.expired(agent) {
		return Answer{}, ErrAgentNotFound
	}
	if agent.MachineID != strings.TrimSpace(in.MachineID) {
		return Answer{}, ErrUnauthorizedAgent
	}
	offer, ok := r.offerByIDLocked(OfferLookupInput{MachineID: in.MachineID, OfferID: in.OfferID})
	if !ok {
		return Answer{}, ErrOfferNotFound
	}
	if r.expireOfferLocked(offer.ID, offer) {
		return Answer{}, ErrOfferNotFound
	}
	if offer.MachineID != agent.MachineID {
		return Answer{}, ErrUnauthorizedAgent
	}
	if offer.AssignedAgentID == "" || offer.AssignedAgentID != agent.ID {
		return Answer{}, ErrOfferNotAssigned
	}
	sdp := in.SDP
	trimmedSDP := strings.TrimSpace(in.SDP)
	answerError := strings.TrimSpace(in.Error)
	if trimmedSDP == "" && answerError == "" {
		return Answer{}, errors.New("answer sdp or error is required")
	}
	if trimmedSDP != "" && answerError != "" {
		return Answer{}, errors.New("answer sdp and error are mutually exclusive")
	}
	if trimmedSDP != "" {
		if err := r.validateSignalingPayload(sdp); err != nil {
			return Answer{}, err
		}
	}
	if _, exists := r.answers[offer.ID]; exists {
		return Answer{}, ErrAnswerAlreadyExists
	}
	answer := Answer{
		ID:          randomID("answer"),
		OfferID:     offer.ID,
		AgentID:     agent.ID,
		MachineID:   agent.MachineID,
		SDP:         sdp,
		Error:       answerError,
		AnswerProof: strings.TrimSpace(in.AnswerProof),
		CreatedAt:   r.clock.Now().UTC(),
	}
	r.answers[answer.OfferID] = answer
	return answer, nil
}

func (r *Registry) SubmitPairingClaim(ctx context.Context, in PairingClaimInput) (PairingClaim, error) {
	_ = ctx
	machineID := strings.TrimSpace(in.MachineID)
	claim := PairingClaim{
		ID:                    randomID("pair_claim"),
		MachineID:             machineID,
		PairSessionID:         strings.TrimSpace(in.PairSessionID),
		PairSecret:            strings.TrimSpace(in.PairSecret),
		AppDeviceID:           strings.TrimSpace(in.AppDeviceID),
		AppName:               strings.TrimSpace(in.AppName),
		RequestedCapabilities: cloneStrings(in.RequestedCapabilities),
		AllowedPaths:          cloneStrings(in.AllowedPaths),
		CreatedAt:             r.clock.Now().UTC(),
	}
	if claim.MachineID == "" || claim.PairSessionID == "" || claim.PairSecret == "" ||
		claim.AppDeviceID == "" || claim.AppName == "" {
		return PairingClaim{}, errors.New("machine id, pair session, pair secret, app device, and app name are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(machineID) {
		if r.machineHasActiveForceOfflineLocked(machineID, r.clock.Now().UTC()) {
			return PairingClaim{}, ErrAgentForcedOffline
		}
		return PairingClaim{}, ErrAgentNotFound
	}
	r.pairingQueue.enqueue(machineID, claim)
	r.pairingQueue.notify(machineID)
	return clonePairingClaim(claim), nil
}

func (r *Registry) PollPairingClaim(ctx context.Context, in PairingPollInput) (PairingClaim, error) {
	agentID := strings.TrimSpace(in.AgentID)
	machineID := strings.TrimSpace(in.MachineID)
	if agentID == "" || machineID == "" {
		return PairingClaim{}, ErrUnauthorizedAgent
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		r.mu.Lock()
		agent, ok := r.agents[agentID]
		if r.forceOfflineActiveLocked(machineID, agentID, r.clock.Now().UTC()) {
			if ok {
				r.unindexAgentLocked(agent.MachineID, agentID)
			}
			delete(r.agents, agentID)
			r.mu.Unlock()
			return PairingClaim{}, ErrAgentForcedOffline
		}
		if !ok || r.expired(agent) {
			r.mu.Unlock()
			return PairingClaim{}, ErrAgentNotFound
		}
		if agent.MachineID != machineID {
			r.mu.Unlock()
			return PairingClaim{}, ErrUnauthorizedAgent
		}
		for {
			claim, ok := r.pairingQueue.dequeue(machineID)
			if ok {
				stored, ok := r.pairingQueue.get(claim.ID)
				if !ok || r.signalingExpired(stored.CreatedAt) {
					r.pairingQueue.delete(claim.ID)
					delete(r.pairingResults, claim.ID)
					continue
				}
				stored.AssignedAgentID = agent.ID
				stored.DeliveredAt = r.clock.Now().UTC()
				r.pairingQueue.set(stored)
				r.mu.Unlock()
				return clonePairingClaim(stored), nil
			}
			break
		}
		waiter := r.pairingQueue.addWaiter(machineID)
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			r.removePairingWaiter(machineID, waiter)
			return PairingClaim{}, ctx.Err()
		case <-timer.C:
			r.removePairingWaiter(machineID, waiter)
			return PairingClaim{}, ErrPollTimeout
		case <-waiter:
		}
	}
}

func (r *Registry) SubmitPairingResult(ctx context.Context, in PairingResultInput) (PairingResult, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	agent, ok := r.agents[strings.TrimSpace(in.AgentID)]
	machineID := strings.TrimSpace(in.MachineID)
	agentID := strings.TrimSpace(in.AgentID)
	if r.forceOfflineActiveLocked(machineID, agentID, r.clock.Now().UTC()) {
		if ok {
			r.unindexAgentLocked(agent.MachineID, agentID)
		}
		delete(r.agents, agentID)
		return PairingResult{}, ErrAgentForcedOffline
	}
	if !ok || r.expired(agent) {
		return PairingResult{}, ErrAgentNotFound
	}
	if agent.MachineID != machineID {
		return PairingResult{}, ErrUnauthorizedAgent
	}
	claim, ok := r.pairingQueue.get(strings.TrimSpace(in.ClaimID))
	if !ok || r.signalingExpired(claim.CreatedAt) {
		return PairingResult{}, ErrPairingClaimNotFound
	}
	if claim.MachineID != agent.MachineID || claim.AssignedAgentID != agent.ID {
		return PairingResult{}, ErrUnauthorizedAgent
	}
	result := PairingResult{
		ClaimID:      claim.ID,
		MachineID:    agent.MachineID,
		MachineName:  strings.TrimSpace(in.MachineName),
		SessionToken: strings.TrimSpace(in.SessionToken),
		ExpiresAt:    strings.TrimSpace(in.ExpiresAt),
		Error:        strings.TrimSpace(in.Error),
		CreatedAt:    r.clock.Now().UTC(),
	}
	if result.Error == "" && result.SessionToken == "" {
		return PairingResult{}, errors.New("pairing result session token or error is required")
	}
	r.pairingResults[result.ClaimID] = result
	return clonePairingResult(result), nil
}

func (r *Registry) GetPairingResult(ctx context.Context, claimID string) (PairingResult, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	claimID = strings.TrimSpace(claimID)
	result, ok := r.pairingResults[claimID]
	if !ok {
		if claim, ok := r.pairingQueue.get(claimID); ok && r.signalingExpired(claim.CreatedAt) {
			r.pairingQueue.delete(claimID)
			delete(r.pairingResults, claimID)
		}
		return PairingResult{}, ErrPairingClaimNotFound
	}
	claim, claimOK := r.pairingQueue.get(claimID)
	if !claimOK || r.signalingExpired(claim.CreatedAt) || r.signalingExpired(result.CreatedAt) {
		r.pairingQueue.delete(claimID)
		delete(r.pairingResults, claimID)
		return PairingResult{}, ErrPairingClaimNotFound
	}
	r.pairingQueue.delete(claimID)
	delete(r.pairingResults, claimID)
	return clonePairingResult(result), nil
}

func (r *Registry) GetAnswer(ctx context.Context, offerID string) (Answer, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	answer, ok := r.answers[strings.TrimSpace(offerID)]
	if !ok {
		return Answer{}, ErrOfferNotFound
	}
	offer, ok := r.offerQueue.get(answer.OfferID)
	if !ok || r.expireOfferLocked(offer.ID, offer) || r.signalingExpired(answer.CreatedAt) {
		r.deleteOfferLocked(answer.OfferID)
		return Answer{}, ErrOfferNotFound
	}
	r.deleteOfferLocked(answer.OfferID)
	return answer, nil
}

func (r *Registry) GetAnswerForOffer(ctx context.Context, in OfferLookupInput) (Answer, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	offer, ok := r.offerByIDLocked(in)
	if !ok || r.offerExpired(offer) {
		if ok {
			r.deleteOfferLocked(offer.ID)
		}
		return Answer{}, ErrOfferNotFound
	}
	answer, ok := r.answers[offer.ID]
	if !ok || r.signalingExpired(answer.CreatedAt) {
		if ok {
			delete(r.answers, offer.ID)
		}
		return Answer{}, ErrOfferNotFound
	}
	if answer.MachineID != strings.TrimSpace(in.MachineID) {
		return Answer{}, ErrUnauthorizedAgent
	}
	r.deleteOfferLocked(offer.ID)
	return answer, nil
}

func (r *Registry) Offer(ctx context.Context, in OfferLookupInput) (Offer, bool) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	offer, ok := r.offerByIDLocked(in)
	if !ok || r.offerExpired(offer) {
		if ok {
			r.deleteOfferLocked(offer.ID)
		}
		return Offer{}, false
	}
	return cloneOffer(offer), true
}

func (r *Registry) CleanupExpiredOffers(ctx context.Context) int {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for _, offer := range r.offerQueue.itemsSnapshot() {
		if r.offerExpired(offer) {
			r.deleteOfferLocked(offer.ID)
			removed++
		}
	}
	r.pruneQueuesLocked()
	return removed
}

func (r *Registry) EvictOldestOffers(ctx context.Context, limit int) int {
	_ = ctx
	if limit < 0 {
		limit = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for {
		offers := r.offerQueue.itemsSnapshot()
		if len(offers) <= limit {
			return removed
		}
		var oldest Offer
		for _, offer := range offers {
			if oldest.ID == "" || offer.CreatedAt.Before(oldest.CreatedAt) {
				oldest = offer
			}
		}
		if oldest.ID == "" {
			return removed
		}
		r.deleteOfferLocked(oldest.ID)
		removed++
	}
}

func (r *Registry) expired(agent Agent) bool {
	return !r.clock.Now().UTC().Before(agent.ExpiresAt)
}

func (r *Registry) signalingExpired(createdAt time.Time) bool {
	return !r.clock.Now().UTC().Before(createdAt.Add(r.signalingTTL))
}

func (r *Registry) machineOnlineLocked(machineID string) bool {
	now := r.clock.Now().UTC()
	agentIDs, ok := r.machineAgents[machineID]
	if !ok {
		return false
	}
	for agentID := range agentIDs {
		agent, ok := r.agents[agentID]
		if !ok {
			continue
		}
		if !r.expired(agent) && !r.forceOfflineActiveLocked(agent.MachineID, agent.ID, now) {
			return true
		}
	}
	return false
}

func (r *Registry) indexAgentLocked(machineID, agentID string) {
	if r.machineAgents[machineID] == nil {
		r.machineAgents[machineID] = make(map[string]struct{})
	}
	r.machineAgents[machineID][agentID] = struct{}{}
}

func (r *Registry) unindexAgentLocked(machineID, agentID string) {
	delete(r.machineAgents[machineID], agentID)
	if len(r.machineAgents[machineID]) == 0 {
		delete(r.machineAgents, machineID)
	}
}

func (r *Registry) machineHasActiveForceOfflineLocked(machineID string, now time.Time) bool {
	for _, forced := range r.forced {
		if forced.MachineID == machineID && now.Before(forced.ExpiresAt) {
			return true
		}
	}
	return false
}

func (r *Registry) forceOfflineActive(machineID string, agentID string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.forceOfflineActiveLocked(machineID, agentID, now)
}

func (r *Registry) forceOfflineActiveLocked(machineID string, agentID string, now time.Time) bool {
	forced, ok := r.forced[forceOfflineKey(machineID, agentID)]
	return ok && now.Before(forced.ExpiresAt)
}

func (r *Registry) dropQueuedOffersLocked(machineID string) {
	r.offerQueue.deleteQueue(machineID)
	r.pairingQueue.deleteQueue(machineID)
	for _, offer := range r.offerQueue.itemsSnapshot() {
		id := offer.ID
		if offer.MachineID == machineID {
			r.deleteOfferLocked(id)
		}
	}
	for _, claim := range r.pairingQueue.itemsSnapshot() {
		id := claim.ID
		if claim.MachineID == machineID {
			r.pairingQueue.delete(id)
			delete(r.pairingResults, id)
		}
	}
}

func (r *Registry) validateSignalingPayload(payload string) error {
	if len(payload) > r.maxSDPBytes {
		return ErrPayloadTooLarge
	}
	if containsRuntimePayload(payload) {
		return ErrRuntimePayload
	}
	if !isBasicSDP(payload) {
		return ErrInvalidSDP
	}
	return nil
}

func (r *Registry) expireOfferLocked(id string, offer Offer) bool {
	if !r.offerExpired(offer) {
		return false
	}
	r.deleteOfferLocked(id)
	return true
}

func (r *Registry) offerExpired(offer Offer) bool {
	if r.signalingExpired(offer.CreatedAt) {
		return true
	}
	return !offer.ExpiresAt.IsZero() && !r.clock.Now().UTC().Before(offer.ExpiresAt)
}

func (r *Registry) offerByIDLocked(in OfferLookupInput) (Offer, bool) {
	machineID := strings.TrimSpace(in.MachineID)
	offerID := strings.TrimSpace(in.OfferID)
	if offerID == "" {
		return Offer{}, false
	}
	if machineID != "" {
		for _, offer := range r.offerQueue.itemsSnapshot() {
			if offer.MachineID == machineID && strings.TrimSpace(offer.SessionID) == offerID {
				return offer, true
			}
		}
	}
	if offer, ok := r.offerQueue.get(offerID); ok {
		if machineID == "" || offer.MachineID == machineID {
			return offer, true
		}
	}
	return Offer{}, false
}

func (r *Registry) deleteOfferLocked(offerID string) {
	offerID = strings.TrimSpace(offerID)
	r.offerQueue.delete(offerID)
	delete(r.answers, offerID)
}

func (r *Registry) pruneQueuesLocked() {
	r.offerQueue.pruneQueues(func(offer Offer) (Offer, bool) {
		if offer.AssignedAgentID != "" || r.offerExpired(offer) {
			return Offer{}, false
		}
		return offer, true
	})
}

func (r *Registry) prunePairingQueuesLocked() {
	r.pairingQueue.pruneQueues(func(claim PairingClaim) (PairingClaim, bool) {
		if claim.AssignedAgentID != "" || r.signalingExpired(claim.CreatedAt) {
			return PairingClaim{}, false
		}
		return claim, true
	})
}

func (r *Registry) notifyLocked(machineID string) {
	r.offerQueue.notify(machineID)
}

func (r *Registry) removeOfferWaiter(machineID string, target chan struct{}) {
	r.offerQueue.removeWaiter(machineID, target)
}

func (r *Registry) removePairingWaiter(machineID string, target chan struct{}) {
	r.pairingQueue.removeWaiter(machineID, target)
}

func cloneTerminals(in []Terminal) []Terminal {
	out := make([]Terminal, len(in))
	copy(out, in)
	return out
}

func cloneAgent(agent Agent) Agent {
	agent.Terminals = cloneTerminals(agent.Terminals)
	return agent
}

func cloneOffer(offer Offer) Offer {
	offer.ICECandidates = cloneStrings(offer.ICECandidates)
	return offer
}

func clonePairingClaim(claim PairingClaim) PairingClaim {
	claim.RequestedCapabilities = cloneStrings(claim.RequestedCapabilities)
	claim.AllowedPaths = cloneStrings(claim.AllowedPaths)
	return claim
}

func clonePairingResult(result PairingResult) PairingResult {
	return result
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}

func forceOfflineKey(machineID string, agentID string) string {
	return strings.TrimSpace(machineID) + "\x00" + strings.TrimSpace(agentID)
}
