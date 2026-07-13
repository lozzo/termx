package webcontroller_test

import (
	"encoding/json"
	"testing"
	"time"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

type entitlementPublisher struct{ calls int }

func (publisher *entitlementPublisher) Activate(accountID, planID, orderID string, validUntil time.Time) error {
	publisher.calls++
	return nil
}

func TestCommerceWebhookIsSignedAndIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	publisher := &entitlementPublisher{}
	service, err := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), publisher, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	session, _ := service.BeginStagingSession("account-1", "user-1", "user@example.test")
	authenticated, err := service.Authenticate(session.Token)
	if err != nil {
		t.Fatal(err)
	}
	order, err := service.CreateCheckout(authenticated, "pro")
	if err != nil || publisher.calls != 0 {
		t.Fatalf("checkout = (%#v, %v), publisher=%d", order, err, publisher.calls)
	}
	event := webcontroller.PaymentEvent{EventID: "event-1", Type: "payment.succeeded", OrderID: order.ID, AccountID: order.AccountID, PlanID: order.PlanID}
	body, _ := json.Marshal(event)
	if _, err := service.ApplyWebhook(body, "invalid"); err == nil {
		t.Fatal("unsigned payment webhook accepted")
	}
	paid, err := service.ApplyWebhook(body, service.SignStagingEvent(body))
	if err != nil || paid.Status != "paid" || publisher.calls != 1 {
		t.Fatalf("paid = (%#v, %v), publisher=%d", paid, err, publisher.calls)
	}
	if _, err := service.ApplyWebhook(body, service.SignStagingEvent(body)); err != nil || publisher.calls != 1 {
		t.Fatalf("duplicate webhook = %v, publisher=%d", err, publisher.calls)
	}
}
