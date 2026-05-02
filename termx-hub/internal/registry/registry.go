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
	ErrPollTimeout         = errors.New("poll timeout")
	ErrUnauthorizedAgent   = errors.New("unauthorized agent")
	ErrAgentNotFound       = errors.New("agent not found")
	ErrOfferNotFound       = errors.New("offer not found")
	ErrOfferNotAssigned    = errors.New("offer not assigned")
	ErrRuntimePayload      = errors.New("runtime payload is not allowed in signaling")
	ErrPayloadTooLarge     = errors.New("signaling payload too large")
	ErrInvalidSDP          = errors.New("invalid sdp")
	ErrAgentRebound        = errors.New("agent already belongs to another machine")
	ErrUnauthorizedTicket  = errors.New("unauthorized ticket")
	ErrVerifierRequired    = errors.New("authority verifier is required")
	ErrAnswerAlreadyExists = errors.New("answer already exists")
)

type Config struct {
	Clock        Clock
	AgentTTL     time.Duration
	SignalingTTL time.Duration
	MaxSDPBytes  int
	Verifier     AuthorityVerifier
}

type Registry struct {
	mu           sync.Mutex
	clock        Clock
	agentTTL     time.Duration
	signalingTTL time.Duration
	maxSDPBytes  int
	verifier     AuthorityVerifier
	agents       map[string]Agent
	offers       map[string]Offer
	answers      map[string]Answer
	queues       map[string][]Offer
	waiters      map[string][]chan struct{}
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
		clock:        clock,
		agentTTL:     ttl,
		signalingTTL: signalingTTL,
		maxSDPBytes:  maxSDPBytes,
		verifier:     cfg.Verifier,
		agents:       make(map[string]Agent),
		offers:       make(map[string]Offer),
		answers:      make(map[string]Answer),
		queues:       make(map[string][]Offer),
		waiters:      make(map[string][]chan struct{}),
	}
}

func (r *Registry) Register(ctx context.Context, in RegisterInput) (Agent, error) {
	machineID := strings.TrimSpace(in.MachineID)
	agentID := strings.TrimSpace(in.AgentID)
	if machineID == "" || agentID == "" {
		return Agent{}, errors.New("machine id and agent id are required")
	}
	now := r.clock.Now().UTC()
	agent := Agent{
		ID:         agentID,
		MachineID:  machineID,
		Status:     AgentOnline,
		Path:       PathManaged,
		Terminals:  cloneTerminals(in.Terminals),
		LastSeenAt: now,
		ExpiresAt:  now.Add(r.agentTTL),
	}
	if r.verifier == nil {
		return Agent{}, ErrVerifierRequired
	}
	r.mu.Lock()
	existing, exists := r.agents[agent.ID]
	if exists && existing.MachineID != agent.MachineID {
		r.mu.Unlock()
		return Agent{}, ErrAgentRebound
	}
	r.mu.Unlock()
	if err := r.verifier.VerifyAgentRegistration(ctx, AgentRegistration{
		MachineID: agent.MachineID,
		AgentID:   agent.ID,
	}); err != nil {
		return Agent{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, exists = r.agents[agent.ID]
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
	if !ok || r.expired(agent) {
		return Agent{}, false
	}
	return cloneAgent(agent), true
}

func (r *Registry) CleanupExpired(ctx context.Context) int {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for id, agent := range r.agents {
		if r.expired(agent) {
			delete(r.agents, id)
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
	return removed
}

func (r *Registry) SubmitOffer(ctx context.Context, in OfferInput) (Offer, error) {
	machineID := strings.TrimSpace(in.MachineID)
	terminalID := strings.TrimSpace(in.TerminalID)
	ticketID := strings.TrimSpace(in.TicketID)
	sdp := strings.TrimSpace(in.SDP)
	if machineID == "" || terminalID == "" || ticketID == "" || sdp == "" {
		return Offer{}, errors.New("machine id, terminal id, ticket id, and sdp are required")
	}
	if err := r.validateSignalingPayload(sdp); err != nil {
		return Offer{}, err
	}
	if r.verifier == nil {
		return Offer{}, ErrVerifierRequired
	}
	if err := r.verifier.VerifyOfferTicket(ctx, OfferTicket{
		MachineID:  machineID,
		TerminalID: terminalID,
		TicketID:   ticketID,
	}); err != nil {
		return Offer{}, err
	}
	offer := Offer{
		ID:         randomID("offer"),
		MachineID:  machineID,
		TerminalID: terminalID,
		TicketID:   ticketID,
		SDP:        sdp,
		Path:       PathManaged,
		CreatedAt:  r.clock.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.machineOnlineLocked(machineID) {
		return Offer{}, ErrAgentNotFound
	}
	r.offers[offer.ID] = offer
	r.queues[machineID] = append(r.queues[machineID], offer)
	r.notifyLocked(machineID)
	return offer, nil
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
				return offer, nil
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
	answerSDP := strings.TrimSpace(in.SDP)
	if answerSDP == "" {
		return Answer{}, errors.New("answer sdp is required")
	}
	if err := r.validateSignalingPayload(answerSDP); err != nil {
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
		SDP:       answerSDP,
		CreatedAt: r.clock.Now().UTC(),
	}
	r.answers[answer.OfferID] = answer
	return answer, nil
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
	for _, agent := range r.agents {
		if agent.MachineID == machineID && !r.expired(agent) {
			return true
		}
	}
	return false
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

func cloneTerminals(in []Terminal) []Terminal {
	out := make([]Terminal, len(in))
	copy(out, in)
	return out
}

func cloneAgent(agent Agent) Agent {
	agent.Terminals = cloneTerminals(agent.Terminals)
	return agent
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}
