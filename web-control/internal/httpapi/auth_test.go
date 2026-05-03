package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestAuthEndpointsRegisterLoginRefreshAndMe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-auth-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := account.NewService(account.Config{
		DB:       db,
		Clock:    fixedClock(time.Date(2026, 5, 3, 3, 13, 0, 0, time.UTC)),
		Tokens:   account.NewHMACTokenIssuer([]byte("slice-2-http-secret")),
		Payments: account.NewMockPaymentProvider(),
	})
	router := httpapi.NewRouter(httpapi.Config{Accounts: svc})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "dana@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var reg struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Plan         struct {
			ID         string `json:"id"`
			AllowRelay bool   `json:"allow_relay"`
		} `json:"plan"`
	}
	decodeJSON(t, register, &reg)
	if reg.Plan.ID != account.PlanRegisteredFree || reg.Plan.AllowRelay {
		t.Fatalf("unexpected default plan: %+v", reg.Plan)
	}

	login := postJSON(t, router, "/api/v1/auth/login", map[string]string{
		"email":    "dana@example.com",
		"password": "valid password",
	}, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}

	refresh := postJSON(t, router, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": reg.RefreshToken,
	}, "")
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", refresh.Code, refresh.Body.String())
	}

	me := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+reg.AccessToken)
	router.ServeHTTP(me, req)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", me.Code, me.Body.String())
	}

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("missing-token me status = %d, want 401", unauthenticated.Code)
	}
}

func postJSON(t *testing.T, handler http.Handler, path string, body any, bearer string) *httptest.ResponseRecorder {
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

func getJSON(t *testing.T, handler http.Handler, path string, bearer string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}
