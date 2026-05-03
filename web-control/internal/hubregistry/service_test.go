package hubregistry_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/hubregistry"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestHubReportDiscoverForceOfflineAndCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openHubRegistryDB(t, "file:termx-hub-registry-service?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 17, 25, 0, 0, time.UTC)}
	svc := hubregistry.NewService(hubregistry.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_1", "mach_1")

	report, err := svc.ReportHub(ctx, hubregistry.ReportHubInput{
		HubID:    "hub_1",
		Region:   "iad",
		HTTPURL:  "https://hub-1.termx.test",
		Status:   hubregistry.HubOnline,
		Capacity: 42,
		Health:   `{"cpu":0.20,"ok":true}`,
		TTL:      time.Minute,
		Agents: []hubregistry.AgentReport{{
			MachineID:     "mach_1",
			AgentID:       "agent_1",
			Status:        hubregistry.AgentOnline,
			TerminalCount: 3,
		}},
	})
	if err != nil {
		t.Fatalf("report hub: %v", err)
	}
	if len(report.AgentPolicies) != 1 || report.AgentPolicies[0].ForceOffline {
		t.Fatalf("initial agent policies = %+v", report.AgentPolicies)
	}

	hubs, err := svc.DiscoverHubs(ctx, hubregistry.DiscoverHubsInput{Now: clock.Now()})
	if err != nil {
		t.Fatalf("discover hubs: %v", err)
	}
	if len(hubs) != 1 || hubs[0].ID != "hub_1" || hubs[0].HTTPURL != "https://hub-1.termx.test" || hubs[0].Capacity != 42 {
		t.Fatalf("discovered hubs = %+v", hubs)
	}

	if err := svc.ForceOfflineAgent(ctx, hubregistry.ForceOfflineInput{
		UserID:    "usr_1",
		MachineID: "mach_1",
		AgentID:   "agent_1",
		Reason:    "owner requested",
	}); err != nil {
		t.Fatalf("force offline: %v", err)
	}
	policy, err := svc.GetAgentPolicy(ctx, hubregistry.AgentPolicyInput{
		MachineID: "mach_1",
		AgentID:   "agent_1",
	})
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if !policy.ForceOffline || policy.Reason != "owner requested" {
		t.Fatalf("force policy = %+v", policy)
	}
	denied, err := svc.ReportHub(ctx, hubregistry.ReportHubInput{
		HubID:   "hub_1",
		Region:  "iad",
		HTTPURL: "https://hub-1.termx.test",
		Status:  hubregistry.HubOnline,
		TTL:     time.Minute,
		Agents: []hubregistry.AgentReport{{
			MachineID: "mach_1",
			AgentID:   "agent_1",
			Status:    hubregistry.AgentOnline,
		}},
	})
	if err != nil {
		t.Fatalf("report after force offline: %v", err)
	}
	if len(denied.AgentPolicies) != 1 || !denied.AgentPolicies[0].ForceOffline {
		t.Fatalf("forced report policy = %+v", denied.AgentPolicies)
	}
	var forcedStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT status FROM hub_agents WHERE machine_id = ? AND agent_id = ?
	`, "mach_1", "agent_1").Scan(&forcedStatus); err != nil {
		t.Fatalf("query forced status: %v", err)
	}
	if forcedStatus != hubregistry.AgentOffline {
		t.Fatalf("forced agent durable status = %q", forcedStatus)
	}

	longTTL, err := svc.ReportHub(ctx, hubregistry.ReportHubInput{
		HubID:   "hub_long",
		Region:  "iad",
		HTTPURL: "https://hub-long.termx.test",
		Status:  hubregistry.HubOnline,
		TTL:     time.Hour,
	})
	if err != nil {
		t.Fatalf("report long ttl: %v", err)
	}
	if longTTL.Hub.ExpiresAt.After(clock.Now().Add(5*time.Minute + time.Second)) {
		t.Fatalf("hub ttl was not capped: expires_at=%s now=%s", longTTL.Hub.ExpiresAt, clock.Now())
	}

	clock.value = clock.value.Add(6 * time.Minute)
	removed, err := svc.CleanupExpired(ctx, hubregistry.CleanupInput{Now: clock.Now()})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed == 0 {
		t.Fatal("cleanup removed no expired hub/agent state")
	}
	hubs, err = svc.DiscoverHubs(ctx, hubregistry.DiscoverHubsInput{Now: clock.Now()})
	if err != nil {
		t.Fatalf("discover after cleanup: %v", err)
	}
	if len(hubs) != 0 {
		t.Fatalf("expired hub still discoverable: %+v", hubs)
	}
}

func TestHubReportRejectsOversizedAgentBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openHubRegistryDB(t, "file:termx-hub-registry-agent-cap?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 18, 22, 0, 0, time.UTC)}
	svc := hubregistry.NewService(hubregistry.Config{DB: db, Clock: clock})
	agents := make([]hubregistry.AgentReport, 1025)
	for i := range agents {
		agents[i] = hubregistry.AgentReport{MachineID: "mach_1", AgentID: "agent"}
	}
	if _, err := svc.ReportHub(ctx, hubregistry.ReportHubInput{
		HubID:   "hub_1",
		Region:  "iad",
		HTTPURL: "https://hub-1.termx.test",
		Agents:  agents,
	}); err == nil {
		t.Fatal("oversized agent report was accepted")
	}
}

func TestForceOfflineRequiresMachineOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openHubRegistryDB(t, "file:termx-hub-registry-owner?mode=memory&cache=shared")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 17, 30, 0, 0, time.UTC)}
	svc := hubregistry.NewService(hubregistry.Config{DB: db, Clock: clock})
	seedUserAndMachine(t, ctx, db, "usr_owner", "mach_1")

	if err := svc.ForceOfflineAgent(ctx, hubregistry.ForceOfflineInput{
		UserID:    "usr_other",
		MachineID: "mach_1",
		AgentID:   "agent_1",
		Reason:    "not owner",
	}); !errors.Is(err, hubregistry.ErrMachineNotOwned) {
		t.Fatalf("wrong owner force err = %v", err)
	}
}

func openHubRegistryDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedUserAndMachine(t *testing.T, ctx context.Context, db *sql.DB, userID string, machineID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users(id, email, password_hash)
		VALUES (?, ?, 'hash')
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO machines(id, owner_user_id, machine_public_key, display_name)
		VALUES (?, ?, 'machine-public-key', 'machine')
	`, machineID, userID); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}
