package httpapi_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/connect"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestManagedConnectTicketHTTPFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-managed-connect-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := fixedClock(time.Date(2026, 5, 3, 5, 43, 0, 0, time.UTC))
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-6-managed-http-secret")),
	})
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: clock})
	connectSvc := connect.NewService(connect.Config{DB: db, Clock: clock})
	router := httpapi.NewRouter(httpapi.Config{Accounts: accounts, Machines: machineSvc, Connect: connectSvc})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "managed-http@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, register, &auth)
	boot, err := machineSvc.Bootstrap(ctx, machines.BootstrapInput{MachinePublicKey: "machine-public-key", DisplayName: "HTTP Managed"})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	user, err := accounts.Me(ctx, auth.AccessToken)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if _, err := machineSvc.Claim(ctx, machines.ClaimInput{UserID: user.User.ID, MachineID: boot.Machine.ID, ClaimToken: boot.ClaimToken}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	unauth := postJSON(t, router, "/api/v1/managed/connect-tickets", map[string]string{
		"machine_id":  boot.Machine.ID,
		"terminal_id": "term_1",
	}, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", unauth.Code)
	}
	create := postJSON(t, router, "/api/v1/managed/connect-tickets", map[string]any{
		"machine_id":  boot.Machine.ID,
		"terminal_id": "term_1",
		"ttl_seconds": 60,
	}, auth.AccessToken)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var ticket struct {
		ID         string `json:"id"`
		Path       string `json:"path"`
		MachineID  string `json:"machine_id"`
		TerminalID string `json:"terminal_id"`
		AllowRelay bool   `json:"allow_relay"`
		RelayInUse bool   `json:"relay_in_use"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &ticket); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	if ticket.ID == "" || ticket.Path != "managed" || ticket.MachineID != boot.Machine.ID || ticket.TerminalID != "term_1" {
		t.Fatalf("ticket = %+v", ticket)
	}
	if !ticket.AllowRelay || ticket.RelayInUse {
		t.Fatalf("dev managed ticket relay capability = %+v", ticket)
	}
	lower := strings.ToLower(create.Body.String())
	for _, forbidden := range []string{"turn:", `"path":"relay"`, "relay_path", "terminal_data", "file_data", "api_data", "events_data", "websocket", "http_runtime"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("managed ticket leaked forbidden field %q: %s", forbidden, create.Body.String())
		}
	}

	overflow := postJSON(t, router, "/api/v1/managed/connect-tickets", map[string]any{
		"machine_id":  boot.Machine.ID,
		"terminal_id": "term_1",
		"ttl_seconds": int64(math.MaxInt64),
	}, auth.AccessToken)
	if overflow.Code != http.StatusBadRequest {
		t.Fatalf("overflow status = %d body=%s", overflow.Code, overflow.Body.String())
	}
}
