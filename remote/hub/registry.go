package hub

import (
	"context"
	"sync"
)

type Agent interface {
	HandleOffer(ctx context.Context, offer Offer) (*Answer, error)
}

type Registration struct {
	UserID string
	Agent  Agent
}

type Registry struct {
	mu      sync.RWMutex
	devices map[string]Registration
}

func NewRegistry() *Registry {
	return &Registry{devices: make(map[string]Registration)}
}

func (r *Registry) Register(deviceID, userID string, agent Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[deviceID] = Registration{UserID: userID, Agent: agent}
}

func (r *Registry) Lookup(deviceID string) (Registration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registration, ok := r.devices[deviceID]
	return registration, ok
}
