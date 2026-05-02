package account

import (
	"context"
	"errors"
	"sync"
)

type PaymentProvider interface {
	CreateOrder(ctx context.Context, userID string, planID string) (ProviderOrder, error)
	GetOrder(ctx context.Context, providerOrderID string) (ProviderOrder, error)
}

type ProviderOrder struct {
	ID     string
	UserID string
	PlanID string
	Status string
}

type MockPaymentProvider struct {
	mu     sync.Mutex
	orders map[string]ProviderOrder
}

func NewMockPaymentProvider() *MockPaymentProvider {
	return &MockPaymentProvider{orders: map[string]ProviderOrder{}}
}

func (p *MockPaymentProvider) CreateOrder(ctx context.Context, userID string, planID string) (ProviderOrder, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	order := ProviderOrder{
		ID:     randomID("pay"),
		UserID: userID,
		PlanID: planID,
		Status: PaymentPending,
	}
	p.orders[order.ID] = order
	return order, nil
}

func (p *MockPaymentProvider) GetOrder(ctx context.Context, providerOrderID string) (ProviderOrder, error) {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	order, ok := p.orders[providerOrderID]
	if !ok {
		return ProviderOrder{}, errors.New("payment order not found")
	}
	return order, nil
}

func (p *MockPaymentProvider) SimulateSuccess(ctx context.Context, providerOrderID string) error {
	return p.setStatus(ctx, providerOrderID, PaymentPaid)
}

func (p *MockPaymentProvider) SimulateFailure(ctx context.Context, providerOrderID string) error {
	return p.setStatus(ctx, providerOrderID, PaymentFailed)
}

func (p *MockPaymentProvider) SimulateExpiry(ctx context.Context, providerOrderID string) error {
	return p.setStatus(ctx, providerOrderID, PaymentExpired)
}

func (p *MockPaymentProvider) setStatus(ctx context.Context, providerOrderID string, status string) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	order, ok := p.orders[providerOrderID]
	if !ok {
		return errors.New("payment order not found")
	}
	order.Status = status
	p.orders[providerOrderID] = order
	return nil
}

func (p *MockPaymentProvider) OverrideOrder(ctx context.Context, providerOrderID string, order ProviderOrder) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.orders[providerOrderID]; !ok {
		return errors.New("payment order not found")
	}
	if order.ID == "" {
		order.ID = providerOrderID
	}
	p.orders[providerOrderID] = order
	return nil
}
