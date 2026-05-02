package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestOpenStoreFromEnvMigratesSQLite(t *testing.T) {
	t.Setenv("TERMX_WEB_CONTROL_SQLITE_DSN", "file:termx-web-control-main-test?mode=memory&cache=shared")
	db, err := openStoreFromEnv(context.Background())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertTableExists(t, db, "users")
	assertTableExists(t, db, "relay_sessions")
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
	if err != nil {
		t.Fatalf("table %s not found: %v", table, err)
	}
}

func TestRunRendezvousCleanupLoopInvokesCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cleaner := &fakeRendezvousCleaner{}
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})

	go func() {
		runRendezvousCleanupLoop(ctx, cleaner, ticks)
		close(done)
	}()
	ticks <- time.Date(2026, 5, 3, 7, 59, 0, 0, time.UTC)
	eventually(t, func() bool { return cleaner.calls == 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup loop did not stop after context cancellation")
	}
}

func TestRunRendezvousCleanupLoopKeepsRunningAfterError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cleaner := &fakeRendezvousCleaner{err: errors.New("cleanup failed")}
	ticks := make(chan time.Time, 2)
	done := make(chan struct{})

	go func() {
		runRendezvousCleanupLoop(ctx, cleaner, ticks)
		close(done)
	}()
	ticks <- time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	ticks <- time.Date(2026, 5, 3, 8, 1, 0, 0, time.UTC)
	eventually(t, func() bool { return cleaner.calls == 2 })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup loop did not stop after context cancellation")
	}
}

type fakeRendezvousCleaner struct {
	calls int
	err   error
}

func (c *fakeRendezvousCleaner) CleanupExpired(context.Context) (int64, error) {
	c.calls++
	return int64(c.calls), c.err
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met")
}
