package devcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestWebEntitlementPublishesUpdatedHubRelayBudget(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "web-entitlement"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	before, err := runtime.state.edgeAuth.RelayBudget(devAccountID)
	if err != nil || before.MaxConcurrency != 2 {
		t.Fatalf("before = (%#v, %v)", before, err)
	}
	payload, _ := json.Marshal(map[string]any{"account_id": devAccountID, "plan_id": "pro", "order_id": "order-1", "valid_until": clock.Now().Add(30 * 24 * time.Hour)})
	request, _ := http.NewRequest(http.MethodPost, runtime.manifest.ControlPlaneURL+"/v1/internal/web/entitlements", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
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
	if err != nil || after.MaxConcurrency != 4 || after.MaxBytes != 256<<20 {
		t.Fatalf("after = (%#v, %v)", after, err)
	}
}
