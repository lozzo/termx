package account

import "time"

const (
	PlanRegisteredFree = "registered_free"
	PlanPro            = "pro"

	SubscriptionActive  = "active"
	SubscriptionPastDue = "past_due"
	SubscriptionExpired = "expired"

	PaymentPending = "pending"
	PaymentPaid    = "paid"
	PaymentFailed  = "failed"
	PaymentExpired = "expired"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type Plan struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	AllowPublicP2P     bool   `json:"allow_public_p2p"`
	AllowRelay         bool   `json:"allow_relay"`
	MonthlyRelayBytes  int64  `json:"monthly_relay_bytes"`
	RelaySessionLimit  int    `json:"relay_session_limit"`
	RelayThrottleBps   int64  `json:"relay_throttle_bps,omitempty"`
	RelayTransferFiles bool   `json:"relay_transfer_files"`
}

type Subscription struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	PlanID          string     `json:"plan_id"`
	Status          string     `json:"status"`
	ProviderOrderID string     `json:"provider_order_id"`
	PeriodStart     *time.Time `json:"current_period_start,omitempty"`
	PeriodEnd       *time.Time `json:"current_period_end,omitempty"`
}

type AuthResult struct {
	User         User          `json:"user"`
	Plan         Plan          `json:"plan"`
	Subscription *Subscription `json:"subscription,omitempty"`
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
}

type RegisterInput struct {
	Email    string
	Password string
}

type LoginInput struct {
	Email    string
	Password string
}

type PaymentOrder struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	PlanID          string `json:"plan_id"`
	ProviderOrderID string `json:"provider_order_id"`
	Status          string `json:"status"`
}

type PaymentSyncResult struct {
	Order        PaymentOrder `json:"order"`
	Plan         Plan         `json:"plan"`
	Subscription Subscription `json:"subscription"`
}
