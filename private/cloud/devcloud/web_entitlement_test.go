package devcloud

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestWebSubscriptionPublishesCatalogCapabilityToHub(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "web-entitlement", WebAccountDBPath: filepath.Join(t.TempDir(), "accounts.db"), WebCatalogPath: "../web-controller/config/plans.json", WebStaging: true})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	if before, err := runtime.state.edgeAuth.RelayBudget(devAccountID); err == nil {
		t.Fatalf("included P2P-only plan unexpectedly has Relay budget: %#v", before)
	}
	payload, _ := proto.Marshal(&cloudpb.SubscriptionProjection{SubscriptionId: "subscription-1", AccountId: devAccountID, SourceOrderId: "order-1", PlanId: "team", PlanVersion: 1, Status: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, CurrentPeriodStartUnixMillis: clock.Now().UnixMilli(), CurrentPeriodEndUnixMillis: clock.Now().Add(30 * 24 * time.Hour).UnixMilli(), UpdatedAtUnixMillis: clock.Now().UnixMilli()})
	request, _ := http.NewRequest(http.MethodPost, runtime.manifest.ControlPlaneURL+"/v1/internal/web/entitlements", bytes.NewReader(payload))
	request.Header.Set("Content-Type", httpapi.ProtobufMediaType)
	request.Header.Set("X-TermX-Internal-Service", "web-controller-staging-v1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	after, err := runtime.state.edgeAuth.RelayBudget(devAccountID)
	if err != nil || after.MaxConcurrency != 10 || after.MaxBytes != 1<<30 {
		t.Fatalf("after = (%#v, %v)", after, err)
	}
}

func TestWebEntitlementAcceptsNewRegisteredAccount(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "web-new-account", WebAccountDBPath: filepath.Join(t.TempDir(), "accounts.db"), WebCatalogPath: "../web-controller/config/plans.json", WebStaging: true})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	payload, _ := proto.Marshal(&cloudpb.SubscriptionProjection{SubscriptionId: "subscription-new", AccountId: "account-from-web", SourceOrderId: "order-new", PlanId: "pro", PlanVersion: 1, Status: cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, CurrentPeriodStartUnixMillis: clock.Now().UnixMilli(), CurrentPeriodEndUnixMillis: clock.Now().Add(37 * 24 * time.Hour).UnixMilli(), UpdatedAtUnixMillis: clock.Now().UnixMilli()})
	request, _ := http.NewRequest(http.MethodPost, runtime.manifest.ControlPlaneURL+"/v1/internal/web/entitlements", bytes.NewReader(payload))
	request.Header.Set("Content-Type", httpapi.ProtobufMediaType)
	request.Header.Set("X-TermX-Internal-Service", "web-controller-staging-v1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	budget, err := runtime.state.edgeAuth.RelayBudget("account-from-web")
	if err != nil || budget.MaxConcurrency != 4 || budget.MaxBytes != 256<<20 {
		t.Fatalf("budget = %#v, %v", budget, err)
	}
}
