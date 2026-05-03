package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestDaemonDeviceRegistrationHTTPCompatibility(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-devices-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := fixedClock(time.Date(2026, 5, 3, 11, 50, 0, 0, time.UTC))
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-11-devices-secret")),
	})
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: clock})
	router := httpapi.NewRouter(httpapi.Config{Accounts: accounts, Machines: machineSvc})

	registerUser := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "daemon-device@example.com",
		"password": "valid password",
	}, "")
	if registerUser.Code != http.StatusCreated {
		t.Fatalf("register user status = %d body=%s", registerUser.Code, registerUser.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, registerUser, &auth)

	unauth := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-compat-1",
		"machinePublicKey": "machine-public-key",
	}, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth device register status = %d body=%s", unauth.Code, unauth.Body.String())
	}
	privateKey := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":          "device-compat-1",
		"machinePublicKey":  "machine-public-key",
		"machinePrivateKey": "must-not-upload",
	}, auth.AccessToken)
	if privateKey.Code != http.StatusBadRequest {
		t.Fatalf("private-key register status = %d body=%s", privateKey.Code, privateKey.Body.String())
	}

	resp := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-compat-1",
		"machinePublicKey": "machine-public-key",
		"displayName":      "Compat Agent",
		"hostname":         "agent-host",
		"platform":         "linux/amd64",
		"terminals": []map[string]any{{
			"id":      "term-compat-1",
			"name":    "Shell",
			"command": []string{"bash"},
			"cols":    80,
			"rows":    24,
			"state":   "running",
		}},
	}, auth.AccessToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("device register status = %d body=%s", resp.Code, resp.Body.String())
	}
	var got struct {
		Device struct {
			ID                string `json:"id"`
			OwnerUserID       string `json:"owner_user_id"`
			MachinePublicKey  string `json:"machine_public_key"`
			MachinePrivateKey string `json:"machine_private_key"`
		} `json:"device"`
	}
	decodeJSON(t, resp, &got)
	if got.Device.ID != "device-compat-1" || got.Device.OwnerUserID == "" || got.Device.MachinePublicKey != "machine-public-key" {
		t.Fatalf("device register response = %+v", got)
	}
	if got.Device.MachinePrivateKey != "" {
		t.Fatalf("device register leaked private key field: %+v", got)
	}

	list := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	listReq.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	router.ServeHTTP(list, listReq)
	if list.Code != http.StatusOK {
		t.Fatalf("devices list status = %d body=%s", list.Code, list.Body.String())
	}
	var devices struct {
		Devices []struct {
			ID string `json:"id"`
		} `json:"devices"`
	}
	decodeJSON(t, list, &devices)
	if len(devices.Devices) != 1 || devices.Devices[0].ID != "device-compat-1" {
		t.Fatalf("devices list = %+v", devices)
	}

	terminalsRec := httptest.NewRecorder()
	terminalsReq := httptest.NewRequest(http.MethodGet, "/api/terminals", nil)
	terminalsReq.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	router.ServeHTTP(terminalsRec, terminalsReq)
	if terminalsRec.Code != http.StatusOK {
		t.Fatalf("terminals list status = %d body=%s", terminalsRec.Code, terminalsRec.Body.String())
	}
	var terminals struct {
		Terminals []struct {
			ID        string `json:"id"`
			MachineID string `json:"machine_id"`
			State     string `json:"state"`
		} `json:"terminals"`
	}
	decodeJSON(t, terminalsRec, &terminals)
	if len(terminals.Terminals) != 1 || terminals.Terminals[0].ID != "term-compat-1" ||
		terminals.Terminals[0].MachineID != "device-compat-1" || terminals.Terminals[0].State != "running" {
		t.Fatalf("terminals list = %+v", terminals)
	}
}

func TestDaemonDeviceRegistrationRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-devices-limit-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 14, 30, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-11-devices-limit-secret")),
	})
	router := httpapi.NewRouter(httpapi.Config{
		Accounts:              accounts,
		Machines:              machines.NewService(machines.Config{DB: db}),
		MaxPublicP2PBodyBytes: 256,
	})
	registerUser := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "daemon-device-limit@example.com",
		"password": "valid password",
	}, "")
	if registerUser.Code != http.StatusCreated {
		t.Fatalf("register user status = %d body=%s", registerUser.Code, registerUser.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, registerUser, &auth)
	resp := postJSON(t, router, "/api/devices/register", map[string]any{
		"deviceId":         "device-too-large",
		"machinePublicKey": "machine-public-key",
		"displayName":      strings.Repeat("x", 1024),
	}, auth.AccessToken)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized register status = %d body=%s", resp.Code, resp.Body.String())
	}
}
