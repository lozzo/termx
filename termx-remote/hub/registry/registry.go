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
	ErrUnauthorizedTicket   = errors.New("unauthorized ticket")
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
	offers         map[string]Offer
	answers        map[string]Answer
	queues         map[string][]Offer
	waiters        map[string][]chan struct{}
	pairingClaims  map[string]PairingClaim
	pairingResults map[string]PairingResult
	pairingQueues  map[string][]PairingClaim
	pairingWaiters map[string][]chan struct{}
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
		clock:          clock,
		agentTTL:       ttl,
		signalingTTL:   signalingTTL,
		maxSDPBytes:    maxSDPBytes,
		agents:         make(map[string]Agent),
		offers:         make(map[string]Offer),
		answers:        make(map[string]Answer),
		queues:         make(map[string][]Offer),
		waiters:        make(map[string][]chan struct{}),
		pairingClaims:  make(map[string]PairingClaim),
		pairingResults: make(map[string]PairingResult),
		pairingQueues:  make(map[string][]PairingClaim),
		pairingWaiters: make(map[string][]chan struct{}),
		forced:         make(map[string]forceOfflineState),
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
		Path:       PathCloud,
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
	return cloneAgent(agent), nil
}

func (r *Registry) Heartbeat(ctx context.Context, in HeartbeatInput) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceOfflineActiveLocked(strings.TrimSpace(in.MachineID), strings.TrimSpace(in.AgentID), r.clock.Now().UTC()) {
		delete(r.agents, strings.TrimSpace(in.AgentID))
		return ErrAgentForcedOffline
	}
	agent, ok := r.agents[strings.TrimSpace(in.AgentID)]
	if !ok || r.expired(agent) {
		return ErrAgentNotFound
	}
	if agent.MachineID != strings.TrimSpace(in.MachineID) {
		return ErrUnauthorizedAgent
	}
	now := r.clock.Now().UTC()
	agent.Status = AgentOnline
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
	for id, offer := range r.offers {
		if r.signalingExpired(offer.CreatedAt) {
			delete(r.offers, id)
			removed++
			if _, ok := r.answers[id]; ok {
				delete(r.answers, id)
				removed++
			}
			continue
		}
		if offer.AssignedAgentID != "" {
			if agent, ok := r.agents[offer.AssignedAgentID]; !ok || r.expired(agent) {
				if _, answered := r.answers[id]; !answered {
					offer.AssignedAgentID = ""
					offer.DeliveredAt = time.Time{}
					r.offers[id] = offer
					r.queues[offer.MachineID] = append(r.queues[offer.MachineID], offer)
				}
			}
		}
	}
	r.pruneQueuesLocked()
	for offerID, answer := range r.answers {
		if _, ok := r.offers[offerID]; !ok {
			delete(r.answers, offerID)
			removed++
			continue
		}
		if r.signalingExpired(answer.CreatedAt) {
			delete(r.answers, offerID)
			removed++
		}
	}
	for id, claim := range r.pairingClaims {
		if r.signalingExpired(claim.CreatedAt) {
			delete(r.pairingClaims, id)
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
	delete(r.agents, agentID)
	r.dropQueuedOffersLocked(machineID)
	r.notifyLocked(machineID)
}

func (r *Registry) SubmitOffer(ctx context.Context, in OfferInput) (Offer, error) {
	machineID := strings.TrimSpace(in.MachineID)
	terminalID := strings.TrimSpace(in.TerminalID)
	ticketID := strings.TrimSpace(in.TicketID)
	if err := r.PreflightOffer(ctx, OfferInput{
		MachineID:      machineID,
		TerminalID:     terminalID,
		TicketID:       ticketID,
		SDP:            in.SDP,
		SessionID:      strings.TrimSpace(in.SessionID),
		ICECandidates:  cloneStrings(in.ICECandidates),
		AppCertificate: cloneRawMessage(in.AppCertificate),
		Signature:      in.Signature,
	}); err != nil {
		return Offer{}, err
	}
	return r.submitVerifiedOffer(ctx, OfferInput{
		MachineID:      machineID,
		TerminalID:     terminalID,
		TicketID:       ticketID,
		SDP:            in.SDP,
		SessionID:      strings.TrimSpace(in.SessionID),
		ICECandidates:  cloneStrings(in.ICECandidates),
		AppCertificate: cloneRawMessage(in.AppCertificate),
		Signature:      in.Signature,
	})
}

func (r *Registry) PreflightOffer(ctx context.Context, in OfferInput) error {
	_ = ctx
	machineID := strings.TrimSpace(in.MachineID)
	ticketID := strings.TrimSpace(in.TicketID)
	sdp := strings.TrimSpace(in.SDP)
	if machineID == "" || ticketID == "" || sdp == "" {
		return errors.New("machine id, ticket id, and sdp are required")
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

func (r *Registry) submitVerifiedOffer(ctx context.Context, in OfferInput) (Offer, error) {
	_ = ctx
	offer := Offer{
		ID:             randomID("offer"),
		SessionID:      strings.TrimSpace(in.SessionID),
		MachineID:      strings.TrimSpace(in.MachineID),
		TerminalID:     strings.TrimSpace(in.TerminalID),
		TicketID:       strings.TrimSpace(in.TicketID),
		SDP:            in.SDP,
		ICECandidates:  cloneStrings(in.ICECandidates),
		AppCertificate: cloneRawMessage(in.AppCertificate),
		Signature:      in.Signature,
		Path:           PathCloud,
		CreatedAt:      r.clock.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(offer.MachineID) {
		if r.machineHasActiveForceOfflineLocked(offer.MachineID, r.clock.Now().UTC()) {
			return Offer{}, ErrAgentForcedOffline
		}
		return Offer{}, ErrAgentNotFound
	}
	r.offers[offer.ID] = offer
	r.queues[offer.MachineID] = append(r.queues[offer.MachineID], offer)
	r.notifyLocked(offer.MachineID)
	return cloneOffer(offer), nil
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
		if queue := r.queues[machineID]; len(queue) > 0 {
			for len(queue) > 0 {
				offer := queue[0]
				queue = queue[1:]
				if r.expireOfferLocked(offer.ID, offer) {
					continue
				}
				offer.AssignedAgentID = agent.ID
				offer.DeliveredAt = r.clock.Now().UTC()
				r.offers[offer.ID] = offer
				r.queues[machineID] = queue
				r.mu.Unlock()
				return cloneOffer(offer), nil
			}
			delete(r.queues, machineID)
		}
		waiter := make(chan struct{}, 1)
		r.waiters[machineID] = append(r.waiters[machineID], waiter)
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			r.removeWaiter(machineID, waiter)
			return Offer{}, ctx.Err()
		case <-timer.C:
			r.removeWaiter(machineID, waiter)
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
		delete(r.agents, strings.TrimSpace(in.AgentID))
		return Answer{}, ErrAgentForcedOffline
	}
	if !ok || r.expired(agent) {
		return Answer{}, ErrAgentNotFound
	}
	if agent.MachineID != strings.TrimSpace(in.MachineID) {
		return Answer{}, ErrUnauthorizedAgent
	}
	offer, ok := r.offers[strings.TrimSpace(in.OfferID)]
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
	if strings.TrimSpace(in.SDP) == "" {
		return Answer{}, errors.New("answer sdp is required")
	}
	if err := r.validateSignalingPayload(strings.TrimSpace(in.SDP)); err != nil {
		return Answer{}, err
	}
	if _, exists := r.answers[offer.ID]; exists {
		return Answer{}, ErrAnswerAlreadyExists
	}
	answer := Answer{
		ID:        randomID("answer"),
		OfferID:   offer.ID,
		AgentID:   agent.ID,
		MachineID: agent.MachineID,
		SDP:       in.SDP,
		CreatedAt: r.clock.Now().UTC(),
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
		AppPublicKey:          strings.TrimSpace(in.AppPublicKey),
		RequestedCapabilities: cloneStrings(in.RequestedCapabilities),
		CreatedAt:             r.clock.Now().UTC(),
	}
	if claim.MachineID == "" || claim.PairSessionID == "" || claim.PairSecret == "" ||
		claim.AppDeviceID == "" || claim.AppName == "" || claim.AppPublicKey == "" {
		return PairingClaim{}, errors.New("machine id, pair session, pair secret, app device, app name, and app public key are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(machineID) {
		if r.machineHasActiveForceOfflineLocked(machineID, r.clock.Now().UTC()) {
			return PairingClaim{}, ErrAgentForcedOffline
		}
		return PairingClaim{}, ErrAgentNotFound
	}
	r.pairingClaims[claim.ID] = claim
	r.pairingQueues[machineID] = append(r.pairingQueues[machineID], claim)
	r.notifyPairingLocked(machineID)
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
		if queue := r.pairingQueues[machineID]; len(queue) > 0 {
			for len(queue) > 0 {
				claim := queue[0]
				queue = queue[1:]
				stored, ok := r.pairingClaims[claim.ID]
				if !ok || r.signalingExpired(stored.CreatedAt) {
					delete(r.pairingClaims, claim.ID)
					delete(r.pairingResults, claim.ID)
					continue
				}
				stored.AssignedAgentID = agent.ID
				stored.DeliveredAt = r.clock.Now().UTC()
				r.pairingClaims[stored.ID] = stored
				r.pairingQueues[machineID] = queue
				r.mu.Unlock()
				return clonePairingClaim(stored), nil
			}
			delete(r.pairingQueues, machineID)
		}
		waiter := make(chan struct{}, 1)
		r.pairingWaiters[machineID] = append(r.pairingWaiters[machineID], waiter)
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
		delete(r.agents, agentID)
		return PairingResult{}, ErrAgentForcedOffline
	}
	if !ok || r.expired(agent) {
		return PairingResult{}, ErrAgentNotFound
	}
	if agent.MachineID != machineID {
		return PairingResult{}, ErrUnauthorizedAgent
	}
	claim, ok := r.pairingClaims[strings.TrimSpace(in.ClaimID)]
	if !ok || r.signalingExpired(claim.CreatedAt) {
		return PairingResult{}, ErrPairingClaimNotFound
	}
	if claim.MachineID != agent.MachineID || claim.AssignedAgentID != agent.ID {
		return PairingResult{}, ErrUnauthorizedAgent
	}
	result := PairingResult{
		ClaimID:          claim.ID,
		MachineID:        agent.MachineID,
		MachineName:      strings.TrimSpace(in.MachineName),
		MachinePublicKey: strings.TrimSpace(in.MachinePublicKey),
		AppCertificate:   cloneRawMessage(in.AppCertificate),
		ExpiresAt:        strings.TrimSpace(in.ExpiresAt),
		Error:            strings.TrimSpace(in.Error),
		CreatedAt:        r.clock.Now().UTC(),
	}
	if result.Error == "" && len(result.AppCertificate) == 0 {
		return PairingResult{}, errors.New("pairing result app certificate or error is required")
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
		if claim, ok := r.pairingClaims[claimID]; ok && r.signalingExpired(claim.CreatedAt) {
			delete(r.pairingClaims, claimID)
			delete(r.pairingResults, claimID)
		}
		return PairingResult{}, ErrPairingClaimNotFound
	}
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
	offer, ok := r.offers[answer.OfferID]
	if !ok || r.expireOfferLocked(offer.ID, offer) || r.signalingExpired(answer.CreatedAt) {
		delete(r.answers, answer.OfferID)
		return Answer{}, ErrOfferNotFound
	}
	return answer, nil
}

func (r *Registry) expired(agent Agent) bool {
	return !r.clock.Now().UTC().Before(agent.ExpiresAt)
}

func (r *Registry) signalingExpired(createdAt time.Time) bool {
	return !r.clock.Now().UTC().Before(createdAt.Add(r.signalingTTL))
}

func (r *Registry) machineOnlineLocked(machineID string) bool {
	now := r.clock.Now().UTC()
	for _, agent := range r.agents {
		if agent.MachineID == machineID && !r.expired(agent) && !r.forceOfflineActiveLocked(agent.MachineID, agent.ID, now) {
			return true
		}
	}
	return false
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
	delete(r.queues, machineID)
	delete(r.pairingQueues, machineID)
	for id, offer := range r.offers {
		if offer.MachineID == machineID {
			delete(r.offers, id)
			delete(r.answers, id)
		}
	}
	for id, claim := range r.pairingClaims {
		if claim.MachineID == machineID {
			delete(r.pairingClaims, id)
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
	if !r.signalingExpired(offer.CreatedAt) {
		return false
	}
	delete(r.offers, id)
	delete(r.answers, id)
	return true
}

func (r *Registry) pruneQueuesLocked() {
	for machineID, queue := range r.queues {
		kept := queue[:0]
		for _, queued := range queue {
			offer, ok := r.offers[queued.ID]
			if !ok || offer.AssignedAgentID != "" || r.signalingExpired(offer.CreatedAt) {
				continue
			}
			kept = append(kept, offer)
		}
		if len(kept) == 0 {
			delete(r.queues, machineID)
			continue
		}
		r.queues[machineID] = kept
	}
}

func (r *Registry) prunePairingQueuesLocked() {
	for machineID, queue := range r.pairingQueues {
		kept := queue[:0]
		for _, queued := range queue {
			claim, ok := r.pairingClaims[queued.ID]
			if !ok || claim.AssignedAgentID != "" || r.signalingExpired(claim.CreatedAt) {
				continue
			}
			kept = append(kept, claim)
		}
		if len(kept) == 0 {
			delete(r.pairingQueues, machineID)
			continue
		}
		r.pairingQueues[machineID] = kept
	}
}

func (r *Registry) notifyLocked(machineID string) {
	waiters := r.waiters[machineID]
	delete(r.waiters, machineID)
	for _, waiter := range waiters {
		select {
		case waiter <- struct{}{}:
		default:
		}
	}
}

func (r *Registry) notifyPairingLocked(machineID string) {
	waiters := r.pairingWaiters[machineID]
	delete(r.pairingWaiters, machineID)
	for _, waiter := range waiters {
		select {
		case waiter <- struct{}{}:
		default:
		}
	}
}

func (r *Registry) removeWaiter(machineID string, target chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiters := r.waiters[machineID]
	for i, waiter := range waiters {
		if waiter == target {
			r.waiters[machineID] = append(waiters[:i], waiters[i+1:]...)
			return
		}
	}
}

func (r *Registry) removePairingWaiter(machineID string, target chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiters := r.pairingWaiters[machineID]
	for i, waiter := range waiters {
		if waiter == target {
			r.pairingWaiters[machineID] = append(waiters[:i], waiters[i+1:]...)
			return
		}
	}
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
	offer.AppCertificate = cloneRawMessage(offer.AppCertificate)
	return offer
}

func clonePairingClaim(claim PairingClaim) PairingClaim {
	claim.RequestedCapabilities = cloneStrings(claim.RequestedCapabilities)
	return claim
}

func clonePairingResult(result PairingResult) PairingResult {
	result.AppCertificate = cloneRawMessage(result.AppCertificate)
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

func cloneRawMessage(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
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
