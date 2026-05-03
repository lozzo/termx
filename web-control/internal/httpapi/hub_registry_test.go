package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/hubregistry"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestHubReportDiscoverAndForceOfflineHTTP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-hub-registry-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := fixedClock(time.Date(2026, 5, 3, 17, 40, 0, 0, time.UTC))
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-16-hub-registry-secret")),
	})
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: clock})
	hubs := hubregistry.NewService(hubregistry.Config{DB: db, Clock: clock})
	router := httpapi.NewRouter(httpapi.Config{
		Accounts:        accounts,
		Machines:        machineSvc,
		HubRegistry:     hubs,
		HubSharedSecret: "hub-shared-secret",
	})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "hub-registry@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, register, &auth)
	device := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-hub-registry",
		"machinePublicKey": "machine-public-key",
	}, auth.AccessToken)
	if device.Code != http.StatusOK {
		t.Fatalf("device register status = %d body=%s", device.Code, device.Body.String())
	}

	unauthReport := postJSON(t, router, "/api/v1/hub/report", map[string]any{}, "")
	if unauthReport.Code != http.StatusUnauthorized {
		t.Fatalf("unauth hub report status = %d body=%s", unauthReport.Code, unauthReport.Body.String())
	}
	report := postHubJSON(t, router, "/api/v1/hub/report", map[string]any{
		"hub_id":      "hub_1",
		"region":      "iad",
		"http_url":    "https://hub-1.termx.test",
		"status":      "online",
		"capacity":    42,
		"weight":      7,
		"health_json": map[string]any{"ok": true},
		"ttl_seconds": 60,
		"agents": []map[string]any{{
			"machine_id":     "device-hub-registry",
			"agent_id":       "agent-http",
			"status":         "online",
			"terminal_count": 2,
		}},
	}, "hub-shared-secret")
	if report.Code != http.StatusOK {
		t.Fatalf("hub report status = %d body=%s", report.Code, report.Body.String())
	}
	var reported struct {
		AgentPolicies []struct {
			MachineID    string `json:"machine_id"`
			AgentID      string `json:"agent_id"`
			ForceOffline bool   `json:"force_offline"`
		} `json:"agent_policies"`
	}
	decodeJSON(t, report, &reported)
	if len(reported.AgentPolicies) != 1 || reported.AgentPolicies[0].ForceOffline {
		t.Fatalf("initial reported policies = %+v", reported.AgentPolicies)
	}

	discover := getJSON(t, router, "/api/v1/hubs", auth.AccessToken)
	if discover.Code != http.StatusOK {
		t.Fatalf("discover status = %d body=%s", discover.Code, discover.Body.String())
	}
	var discovered struct {
		Hubs []struct {
			ID       string `json:"id"`
			HTTPURL  string `json:"http_url"`
			Capacity int    `json:"capacity"`
			Weight   int    `json:"weight"`
		} `json:"hubs"`
	}
	decodeJSON(t, discover, &discovered)
	if len(discovered.Hubs) != 1 || discovered.Hubs[0].ID != "hub_1" ||
		discovered.Hubs[0].HTTPURL != "https://hub-1.termx.test" ||
		discovered.Hubs[0].Capacity != 42 || discovered.Hubs[0].Weight != 7 {
		t.Fatalf("discovered hubs = %+v", discovered.Hubs)
	}

	force := postJSON(t, router, "/api/v1/machines/device-hub-registry/agents/agent-http/force-offline", map[string]any{
		"reason": "owner requested",
	}, auth.AccessToken)
	if force.Code != http.StatusNoContent {
		t.Fatalf("force offline status = %d body=%s", force.Code, force.Body.String())
	}
	policy := postHubJSON(t, router, "/api/v1/hub/agents/policy", map[string]any{
		"machine_id": "device-hub-registry",
		"agent_id":   "agent-http",
	}, "hub-shared-secret")
	if policy.Code != http.StatusOK {
		t.Fatalf("hub policy status = %d body=%s", policy.Code, policy.Body.String())
	}
	var gotPolicy struct {
		Policy struct {
			ForceOffline bool   `json:"force_offline"`
			Reason       string `json:"reason"`
		} `json:"policy"`
	}
	decodeJSON(t, policy, &gotPolicy)
	if !gotPolicy.Policy.ForceOffline || gotPolicy.Policy.Reason != "owner requested" {
		t.Fatalf("hub policy = %+v", gotPolicy.Policy)
	}
}
