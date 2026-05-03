package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestNewRouterFromServicesWiresManagedConnect(t *testing.T) {
	t.Setenv("TERMX_WEB_CONTROL_SQLITE_DSN", "file:termx-web-control-router-test?mode=memory&cache=shared")
	t.Setenv("TERMX_WEB_CONTROL_TOKEN_SECRET", "router-secret")
	t.Setenv("TERMX_WEB_CONTROL_HUB_SECRET", "hub-secret")

	db, err := openStoreFromEnv(context.Background())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	router, err := newRouterFromServices(context.Background(), db)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	register := postMainJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "router@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeMainJSON(t, register, &auth)
	device := postMainJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-router-1",
		"machinePublicKey": "machine-public-key",
		"terminals": []map[string]any{{
			"id":    "term-router-1",
			"state": "running",
		}},
	}, auth.AccessToken)
	if device.Code != http.StatusOK {
		t.Fatalf("device register status = %d body=%s", device.Code, device.Body.String())
	}
	ticket := postMainJSON(t, router, "/api/v1/managed/connect-tickets", map[string]any{
		"machine_id":  "device-router-1",
		"terminal_id": "term-router-1",
	}, auth.AccessToken)
	if ticket.Code != http.StatusCreated {
		t.Fatalf("managed ticket status = %d body=%s", ticket.Code, ticket.Body.String())
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

func postMainJSON(t *testing.T, handler http.Handler, path string, body any, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeMainJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
}
