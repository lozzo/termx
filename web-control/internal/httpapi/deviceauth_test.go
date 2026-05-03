package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/deviceauth"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestDeviceAuthHTTPFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-deviceauth-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clock := fixedClock(time.Date(2026, 5, 3, 19, 20, 0, 0, time.UTC))
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  clock,
		Tokens: account.NewHMACTokenIssuer([]byte("slice-17-http-deviceauth-secret")),
	})
	deviceCodes := deviceauth.NewService(deviceauth.Config{DB: db, Accounts: accounts, Clock: clock})
	router := httpapi.NewRouter(httpapi.Config{Accounts: accounts, DeviceAuth: deviceCodes})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "http-device@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, register, &auth)

	create := postJSON(t, router, "/api/v1/auth/device-code", map[string]string{
		"client_name":      "termx cli",
		"verification_uri": "https://control.example.test/device",
	}, "")
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
	}
	decodeJSON(t, create, &created)
	if created.DeviceCode == "" || created.UserCode == "" || created.VerificationURIComplete == "" {
		t.Fatalf("unexpected create response: %+v", created)
	}

	pending := postJSON(t, router, "/api/v1/auth/device-code/token", map[string]string{
		"device_code": created.DeviceCode,
	}, "")
	if pending.Code != http.StatusUnauthorized {
		t.Fatalf("pending poll status=%d body=%s", pending.Code, pending.Body.String())
	}

	unauthConfirm := postJSON(t, router, "/api/v1/auth/device-code/"+created.UserCode+"/confirm", map[string]string{}, "")
	if unauthConfirm.Code != http.StatusUnauthorized {
		t.Fatalf("unauth confirm status=%d body=%s", unauthConfirm.Code, unauthConfirm.Body.String())
	}
	confirm := postJSON(t, router, "/api/v1/auth/device-code/"+created.UserCode+"/confirm", map[string]string{}, auth.AccessToken)
	if confirm.Code != http.StatusNoContent {
		t.Fatalf("confirm status=%d body=%s", confirm.Code, confirm.Body.String())
	}

	token := postJSON(t, router, "/api/v1/auth/device-code/token", map[string]string{
		"device_code": created.DeviceCode,
	}, "")
	if token.Code != http.StatusOK {
		t.Fatalf("approved poll status=%d body=%s", token.Code, token.Body.String())
	}
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	decodeJSON(t, token, &tokenResp)
	if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" || tokenResp.User.Email != "http-device@example.com" {
		t.Fatalf("unexpected token response: %+v", tokenResp)
	}

	rejectCreate := postJSON(t, router, "/api/v1/auth/device-code", map[string]string{"client_name": "termx cli"}, "")
	if rejectCreate.Code != http.StatusCreated {
		t.Fatalf("reject create status=%d body=%s", rejectCreate.Code, rejectCreate.Body.String())
	}
	var rejectCode struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	decodeJSON(t, rejectCreate, &rejectCode)
	reject := postJSON(t, router, "/api/v1/auth/device-code/"+rejectCode.UserCode+"/reject", map[string]string{
		"reason": "not me",
	}, auth.AccessToken)
	if reject.Code != http.StatusNoContent {
		t.Fatalf("reject status=%d body=%s", reject.Code, reject.Body.String())
	}
	rejectedPoll := postJSON(t, router, "/api/v1/auth/device-code/token", map[string]string{
		"device_code": rejectCode.DeviceCode,
	}, "")
	if rejectedPoll.Code != http.StatusForbidden {
		t.Fatalf("rejected poll status=%d body=%s", rejectedPoll.Code, rejectedPoll.Body.String())
	}
}
