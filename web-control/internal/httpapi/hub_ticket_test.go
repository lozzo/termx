package httpapi_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/connect"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestHubManagedTicketCheckAndConsumeHTTP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-hub-ticket-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := fixedClock(time.Date(2026, 5, 3, 11, 55, 0, 0, time.UTC))
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-11-hub-ticket-secret")),
	})
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: clock})
	connectSvc := connect.NewService(connect.Config{DB: db, Clock: clock})
	router := httpapi.NewRouter(httpapi.Config{
		Accounts:        accounts,
		Machines:        machineSvc,
		Connect:         connectSvc,
		HubSharedSecret: "hub-shared-secret",
	})

	user := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "hub-ticket@example.com",
		"password": "valid password",
	}, "")
	if user.Code != http.StatusCreated {
		t.Fatalf("register user status = %d body=%s", user.Code, user.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, user, &auth)
	device := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-hub-ticket",
		"machinePublicKey": "machine-public-key",
		"terminals": []map[string]any{{
			"id":    "term-hub-ticket",
			"state": "running",
		}},
	}, auth.AccessToken)
	if device.Code != http.StatusOK {
		t.Fatalf("device register status = %d body=%s", device.Code, device.Body.String())
	}
	ticketResp := postJSON(t, router, "/api/v1/managed/connect-tickets", map[string]any{
		"machine_id":  "device-hub-ticket",
		"terminal_id": "term-hub-ticket",
		"ttl_seconds": 60,
	}, auth.AccessToken)
	if ticketResp.Code != http.StatusCreated {
		t.Fatalf("create ticket status = %d body=%s", ticketResp.Code, ticketResp.Body.String())
	}
	var ticket struct {
		ID string `json:"id"`
	}
	decodeJSON(t, ticketResp, &ticket)

	unauthCheck := postJSON(t, router, "/api/v1/hub/managed-tickets/check", map[string]any{
		"ticket_id":   ticket.ID,
		"machine_id":  "device-hub-ticket",
		"terminal_id": "term-hub-ticket",
	}, "")
	if unauthCheck.Code != http.StatusUnauthorized {
		t.Fatalf("unauth hub check status = %d body=%s", unauthCheck.Code, unauthCheck.Body.String())
	}
	check := postHubJSON(t, router, "/api/v1/hub/managed-tickets/check", map[string]any{
		"ticket_id":   ticket.ID,
		"machine_id":  "device-hub-ticket",
		"terminal_id": "term-hub-ticket",
	}, "hub-shared-secret")
	if check.Code != http.StatusOK {
		t.Fatalf("check ticket status = %d body=%s", check.Code, check.Body.String())
	}
	var checked struct {
		Ticket struct {
			ID         string `json:"id"`
			Path       string `json:"path"`
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id"`
			AllowRelay bool   `json:"allow_relay"`
		} `json:"ticket"`
	}
	decodeJSON(t, check, &checked)
	if checked.Ticket.ID != ticket.ID || checked.Ticket.Path != "managed" ||
		checked.Ticket.MachineID != "device-hub-ticket" || checked.Ticket.TerminalID != "term-hub-ticket" ||
		checked.Ticket.AllowRelay {
		t.Fatalf("checked ticket = %+v", checked)
	}
	consume := postHubJSON(t, router, "/api/v1/hub/managed-tickets/consume", map[string]any{
		"ticket_id":   ticket.ID,
		"machine_id":  "device-hub-ticket",
		"terminal_id": "term-hub-ticket",
	}, "hub-shared-secret")
	if consume.Code != http.StatusOK {
		t.Fatalf("consume ticket status = %d body=%s", consume.Code, consume.Body.String())
	}
	secondConsume := postHubJSON(t, router, "/api/v1/hub/managed-tickets/consume", map[string]any{
		"ticket_id":   ticket.ID,
		"machine_id":  "device-hub-ticket",
		"terminal_id": "term-hub-ticket",
	}, "hub-shared-secret")
	if secondConsume.Code != http.StatusForbidden {
		t.Fatalf("second consume status = %d body=%s", secondConsume.Code, secondConsume.Body.String())
	}
}

func TestHubAgentRegistrationVerifierHTTP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-hub-agent-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := fixedClock(time.Date(2026, 5, 3, 14, 26, 0, 0, time.UTC))
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-11-agent-secret")),
	})
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: clock})
	router := httpapi.NewRouter(httpapi.Config{
		Accounts:        accounts,
		Machines:        machineSvc,
		HubSharedSecret: "hub-shared-secret",
	})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "hub-agent@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register user status = %d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, register, &auth)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate machine key: %v", err)
	}
	device := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-hub-agent",
		"machinePublicKey": base64.RawURLEncoding.EncodeToString(publicKey),
	}, auth.AccessToken)
	if device.Code != http.StatusOK {
		t.Fatalf("device register status = %d body=%s", device.Code, device.Body.String())
	}
	nonce := "nonce-http-agent"
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, testAgentRegistrationMessage(testAgentRegistrationFields{
		MachineID: "device-hub-agent",
		AgentID:   "agent-http",
		Nonce:     nonce,
		Timestamp: clock.Now(),
	})))

	unauth := postJSON(t, router, "/api/v1/hub/agents/verify-registration", map[string]any{}, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth agent verify status = %d body=%s", unauth.Code, unauth.Body.String())
	}
	ok := postHubJSON(t, router, "/api/v1/hub/agents/verify-registration", map[string]any{
		"machine_id": "device-hub-agent",
		"agent_id":   "agent-http",
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     nonce,
			"timestamp": clock.Now().Unix(),
			"value":     signature,
		},
	}, "hub-shared-secret")
	if ok.Code != http.StatusNoContent {
		t.Fatalf("agent verify status = %d body=%s", ok.Code, ok.Body.String())
	}
	replay := postHubJSON(t, router, "/api/v1/hub/agents/verify-registration", map[string]any{
		"machine_id": "device-hub-agent",
		"agent_id":   "agent-http",
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     nonce,
			"timestamp": clock.Now().Unix(),
			"value":     signature,
		},
	}, "hub-shared-secret")
	if replay.Code != http.StatusForbidden {
		t.Fatalf("replay status = %d body=%s", replay.Code, replay.Body.String())
	}
}

type testAgentRegistrationFields struct {
	MachineID string
	AgentID   string
	Nonce     string
	Timestamp time.Time
}

func testAgentRegistrationMessage(fields testAgentRegistrationFields) []byte {
	machineHash := sha256.Sum256([]byte(strings.TrimSpace(fields.MachineID)))
	agentHash := sha256.Sum256([]byte(strings.TrimSpace(fields.AgentID)))
	return []byte(strings.Join([]string{
		"termx-agent-registration-v1:",
		"sha256(machine_id):" + hex.EncodeToString(machineHash[:]),
		"sha256(agent_id):" + hex.EncodeToString(agentHash[:]),
		"nonce:" + strings.TrimSpace(fields.Nonce),
		fmt.Sprintf("timestamp:%d", fields.Timestamp.UTC().Unix()),
	}, "\n"))
}

func postHubJSON(t *testing.T, handler http.Handler, path string, body any, secret string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TermX-Hub-Secret", secret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
