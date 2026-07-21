package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestInitialMigrationDeclaresControllerTruth(t *testing.T) {
	body, err := os.ReadFile("migrations/0001_controller.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, table := range []string{
		"hub_deployments", "hub_assignments", "control_receive_cursors",
		"cloud_device_ownership", "hub_topology_heads", "presence_topology",
		"managed_peer_topology", "terminal_access_topology", "commerce_accounts",
		"commerce_sessions", "commerce_orders", "commerce_payment_attempts",
		"commerce_payment_events", "commerce_subscriptions", "commerce_entitlements",
		"relay_quota_periods", "relay_lease_reservations", "relay_usage_events",
		"relay_usage_aggregates", "commerce_audit", "management_commands",
		"management_command_children", "management_command_results",
	} {
		if !strings.Contains(sql, "CREATE TABLE "+table+" ") {
			t.Fatalf("initial PostgreSQL migration is missing %s", table)
		}
	}
	for _, sqliteOnly := range []string{"PRAGMA ", " BLOB", "AUTOINCREMENT", "?"} {
		if strings.Contains(sql, sqliteOnly) {
			t.Fatalf("initial PostgreSQL migration contains SQLite-only token %q", sqliteOnly)
		}
	}
}

func TestPostgreSQLPlaceholderRebinding(t *testing.T) {
	// Placeholder conversion is adapter-local，业务 query 不得自行拼接 PostgreSQL 参数编号。
	if got := rebind("SELECT * FROM value WHERE a=? AND b=?"); got != "SELECT * FROM value WHERE a=$1 AND b=$2" {
		t.Fatalf("rebind = %q", got)
	}
}
