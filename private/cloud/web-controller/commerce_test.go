package webcontroller_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type entitlementPublisher struct{ calls int }

func (publisher *entitlementPublisher) PublishSubscription(*cloudpb.SubscriptionProjection) error {
	publisher.calls++
	return nil
}

func testCatalog(t *testing.T) webcontroller.Catalog {
	t.Helper()
	catalog, err := webcontroller.LoadCatalog("config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestCommerceSessionAndOrderSurviveDatabaseRestart(t *testing.T) {
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "accounts.db")
	center, err := webcontroller.OpenUserCenterStore(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	service, _ := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), &entitlementPublisher{}, testCatalog(t), func() time.Time { return now })
	service.AttachUserCenter(center)
	session, _ := service.BeginStagingSession("account-dev-local", "user-dev-local", "dev-local@termx.invalid")
	order, err := service.CreateCheckout(session, "pro")
	if err != nil {
		t.Fatal(err)
	}
	_ = center.Close()
	reopened, err := webcontroller.OpenUserCenterStore(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, _ := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), &entitlementPublisher{}, testCatalog(t), func() time.Time { return now })
	restarted.AttachUserCenter(reopened)
	authenticated, err := restarted.Authenticate(session.Token)
	if err != nil {
		t.Fatalf("Authenticate after restart = %v", err)
	}
	view := restarted.AccountView(authenticated)
	if len(view.Orders) != 1 || view.Orders[0].ID != order.ID {
		t.Fatalf("orders after restart = %#v", view.Orders)
	}
}

type recordedEntitlement struct {
	subscription *cloudpb.SubscriptionProjection
}
type recordingPublisher struct{ values []recordedEntitlement }

type failingPublisher struct{}

func (failingPublisher) PublishSubscription(*cloudpb.SubscriptionProjection) error {
	return errors.New("control plane unavailable")
}

func (publisher *recordingPublisher) PublishSubscription(subscription *cloudpb.SubscriptionProjection) error {
	publisher.values = append(publisher.values, recordedEntitlement{subscription: proto.Clone(subscription).(*cloudpb.SubscriptionProjection)})
	return nil
}

func TestCommerceWebhookIsSignedAndIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	publisher := &entitlementPublisher{}
	service, err := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), publisher, testCatalog(t), func() time.Time { return now })
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
	event := webcontroller.PaymentEvent{EventID: "event-1", Type: "payment.succeeded", OrderID: order.ID, AccountID: order.AccountID, PlanID: order.PlanID, PlanVersion: order.PlanVersion}
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

func TestCheckoutUsesCatalogInsteadOfPlanNameBranches(t *testing.T) {
	now := time.Date(2026, 7, 13, 2, 0, 0, 0, time.UTC)
	service, err := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), &entitlementPublisher{}, testCatalog(t), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	session, _ := service.BeginStagingSession("account-1", "user-1", "user@example.test")
	team, err := service.CreateCheckout(session, "team")
	if err != nil || team.PlanID != "team" || team.PlanVersion != 1 {
		t.Fatalf("team checkout = (%#v, %v)", team, err)
	}
	if _, err := service.CreateCheckout(session, "managed-free"); !errors.Is(err, webcontroller.ErrCommerceConflict) {
		t.Fatalf("included checkout error = %v", err)
	}
	if _, err := service.CreateCheckout(session, "missing-plan"); !errors.Is(err, webcontroller.ErrCommerceConflict) {
		t.Fatalf("unknown checkout error = %v", err)
	}
}

func TestFirstPaidReferralExtendsInviteeSubscription(t *testing.T) {
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	center := webcontroller.NewUserCenterStore(func() time.Time { return now })
	defer center.Close()
	_, _, program, _, _ := center.Snapshot("account-dev-local")
	invitee, err := center.RegisterPasswordAccount("invitee@example.com", "secure-password", program.Code)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &recordingPublisher{}
	service, err := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), publisher, testCatalog(t), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	service.AttachUserCenter(center)
	session, _ := service.BeginStagingSession(invitee.AccountID, invitee.UserID, invitee.Email)
	order, _ := service.CreateCheckout(session, "pro")
	paid, err := service.ConfirmStagingPayment(session, order.ID)
	if err != nil || paid.Status != "paid" {
		t.Fatalf("payment = %#v, %v", paid, err)
	}
	view := service.AccountView(session)
	want := now.Add(37 * 24 * time.Hour)
	if view.ValidUntil == nil || !view.ValidUntil.Equal(want) {
		t.Fatalf("valid until = %v, want %v", view.ValidUntil, want)
	}
	if len(publisher.values) != 1 || !time.UnixMilli(publisher.values[0].subscription.GetCurrentPeriodEndUnixMillis()).Equal(want) {
		t.Fatalf("published = %#v", publisher.values)
	}
}

func TestReferralRewardIsNotCommittedBeforeEntitlement(t *testing.T) {
	now := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	center := webcontroller.NewUserCenterStore(func() time.Time { return now })
	defer center.Close()
	_, _, program, _, _ := center.Snapshot("account-dev-local")
	invitee, _ := center.RegisterPasswordAccount("failed@example.com", "secure-password", program.Code)
	service, _ := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), failingPublisher{}, testCatalog(t), func() time.Time { return now })
	service.AttachUserCenter(center)
	session, _ := service.BeginStagingSession(invitee.AccountID, invitee.UserID, invitee.Email)
	order, _ := service.CreateCheckout(session, "pro")
	if _, err := service.ConfirmStagingPayment(session, order.ID); err == nil {
		t.Fatal("payment unexpectedly succeeded")
	}
	_, _, inviteeProgram, _, _ := center.Snapshot(invitee.AccountID)
	if inviteeProgram.RewardDays != 0 {
		t.Fatalf("reward committed before entitlement: %#v", inviteeProgram)
	}
}
