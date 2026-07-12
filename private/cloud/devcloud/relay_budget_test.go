package devcloud

import (
	"context"
	"testing"
	"time"
)

func TestRegionalRelayBudgetRejectsExhaustionAndReleasesExpiredLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "relay-budget", UsageOutboxPath: t.TempDir() + "/usage.outbox"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Close(ctx)
	})
	for _, sessionID := range []string{"relay-budget-1", "relay-budget-2"} {
		if _, _, err := runtime.state.acquireSingleRelay(devAccountID, sessionID, devClientDeviceID, "daemon-budget"); err != nil {
			t.Fatalf("acquire %s = %v", sessionID, err)
		}
	}
	if _, _, err := runtime.state.acquireSingleRelay(devAccountID, "relay-budget-3", devClientDeviceID, "daemon-budget"); err == nil {
		t.Fatal("regional Relay budget allowed a third concurrent lease")
	}
	clock.Advance(relayLeaseTTL + time.Second)
	if _, _, err := runtime.state.acquireSingleRelay(devAccountID, "relay-budget-after-expiry", devClientDeviceID, "daemon-budget"); err != nil {
		t.Fatalf("expired Relay budget was not released: %v", err)
	}
}
