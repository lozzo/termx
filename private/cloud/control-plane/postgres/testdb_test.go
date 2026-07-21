package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var testSchemaSequence atomic.Uint64

func testPostgresDSN(t *testing.T) string {
	t.Helper()
	base := os.Getenv("MUXVIA_TEST_POSTGRES_DSN")
	if base == "" {
		base = "postgres://127.0.0.1:55432/postgres?sslmode=disable"
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		admin.Close()
		t.Skipf("PostgreSQL integration database is unavailable: %v", err)
	}
	schema := fmt.Sprintf("muxvia_test_%d_%d", os.Getpid(), testSchemaSequence.Add(1))
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = admin.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})
	value, err := url.Parse(base)
	if err != nil || value.Scheme == "" {
		t.Fatalf("MUXVIA_TEST_POSTGRES_DSN must be a PostgreSQL URL: %v", err)
	}
	query := value.Query()
	query.Set("search_path", schema)
	value.RawQuery = query.Encode()
	return value.String()
}
