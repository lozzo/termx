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

func TestCatalogAndOverrideMigrationDeclaresVersionedTruth(t *testing.T) {
	body, err := os.ReadFile("migrations/0002_catalog_and_entitlement_overrides.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, table := range []string{"plan_catalog_releases", "plan_catalog_head", "plan_definitions", "plan_catalog_release_plans", "entitlement_overrides", "operator_mutation_audit"} {
		if !strings.Contains(sql, "CREATE TABLE "+table+" ") {
			t.Fatalf("catalog migration is missing %s", table)
		}
	}
}

func TestCommerceOperationsMigrationDeclaresPromotionAndAdjustmentTruth(t *testing.T) {
	body, err := os.ReadFile("migrations/0003_commerce_operations.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, table := range []string{"promotions", "promotion_redemptions", "subscription_adjustments"} {
		if !strings.Contains(sql, "CREATE TABLE "+table+" ") {
			t.Fatalf("commerce operations migration is missing %s", table)
		}
	}
}

func TestPostgreSQLPlaceholderRebinding(t *testing.T) {
	// Placeholder conversion is adapter-local，业务 query 不得自行拼接 PostgreSQL 参数编号。
	if got := rebind("SELECT * FROM value WHERE a=? AND b=?"); got != "SELECT * FROM value WHERE a=$1 AND b=$2" {
		t.Fatalf("rebind = %q", got)
	}
}

func TestValidateDSNRequiresRemoteTLS(t *testing.T) {
	for _, test := range []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{name: "local development", dsn: "postgres://127.0.0.1:55432/postgres?sslmode=disable"},
		{name: "Supabase direct TLS", dsn: "postgresql://postgres:secret@db.example.supabase.co:5432/postgres?sslmode=require"},
		{name: "Supavisor session TLS", dsn: "postgres://postgres.project:secret@region.pooler.supabase.com:5432/postgres?sslmode=verify-full"},
		{name: "remote TLS missing", dsn: "postgres://db.example.com/postgres", wantErr: true},
		{name: "remote TLS disabled", dsn: "postgres://db.example.com/postgres?sslmode=disable", wantErr: true},
		{name: "keyword DSN", dsn: "host=db.example.com dbname=postgres sslmode=require", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDSN(test.dsn)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateDSN() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateDSNDoesNotLeakCredential(t *testing.T) {
	const secret = "do-not-log-this-password"
	err := ValidateDSN("postgres://postgres:" + secret + "@db.example.com/postgres?sslmode=disable")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("ValidateDSN credential handling error = %v", err)
	}
}
