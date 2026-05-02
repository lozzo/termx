package httpapi_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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

func TestMachineHTTPAPIsRequireAuthAndDoNotLeakPrivateKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-machines-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 5, 8, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-3-http-secret")),
	})
	machineSvc := machines.NewService(machines.Config{
		DB:    db,
		Clock: fixedClock(time.Date(2026, 5, 3, 5, 8, 0, 0, time.UTC)),
	})
	router := httpapi.NewRouter(httpapi.Config{Accounts: accounts, Machines: machineSvc})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "owner@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, register, &auth)

	rejectedBootstrap := postJSON(t, router, "/api/v1/agent/bootstrap", map[string]string{
		"machine_public_key":  "machine-public-key",
		"machine_private_key": "must-not-leak",
		"display_name":        "HTTP Machine",
		"hostname":            "http.local",
		"platform":            "linux/amd64",
	}, "")
	if rejectedBootstrap.Code != http.StatusBadRequest {
		t.Fatalf("private-key bootstrap status = %d body=%s", rejectedBootstrap.Code, rejectedBootstrap.Body.String())
	}
	machinePublic, machinePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate machine key: %v", err)
	}
	bootstrap := postJSON(t, router, "/api/v1/agent/bootstrap", map[string]string{
		"machine_public_key": base64.RawURLEncoding.EncodeToString(machinePublic),
		"display_name":       "HTTP Machine",
		"hostname":           "http.local",
		"platform":           "linux/amd64",
	}, "")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d body=%s", bootstrap.Code, bootstrap.Body.String())
	}
	assertBodyHasNoPrivateMaterial(t, bootstrap.Body.String())
	var boot struct {
		Machine struct {
			ID string `json:"id"`
		} `json:"machine"`
		ClaimToken string `json:"claim_token"`
	}
	decodeJSON(t, bootstrap, &boot)
	if boot.Machine.ID == "" {
		t.Fatal("bootstrap did not return machine id")
	}
	if boot.ClaimToken == "" {
		t.Fatal("bootstrap did not return claim token")
	}

	claimUnauth := postJSON(t, router, "/api/v1/machines/claim", map[string]string{"machine_id": boot.Machine.ID}, "")
	if claimUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth claim status = %d", claimUnauth.Code)
	}
	claimMissingToken := postJSON(t, router, "/api/v1/machines/claim", map[string]string{"machine_id": boot.Machine.ID}, auth.AccessToken)
	if claimMissingToken.Code != http.StatusForbidden {
		t.Fatalf("claim without token status = %d body=%s", claimMissingToken.Code, claimMissingToken.Body.String())
	}
	claim := postJSON(t, router, "/api/v1/machines/claim", map[string]string{"machine_id": boot.Machine.ID, "claim_token": boot.ClaimToken}, auth.AccessToken)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status = %d body=%s", claim.Code, claim.Body.String())
	}
	assertBodyHasNoPrivateMaterial(t, claim.Body.String())

	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	router.ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	assertBodyHasNoPrivateMaterial(t, list.Body.String())

	appPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	payload := signedHTTPPayload(t, machinePrivate, map[string]any{
		"cert_id":        "cert_http",
		"machine_id":     boot.Machine.ID,
		"app_public_key": base64.RawURLEncoding.EncodeToString(appPublic),
		"expires_at":     "2026-05-03T06:08:00Z",
	})
	rejectedCert := postJSON(t, router, "/api/v1/machines/"+boot.Machine.ID+"/app-certificates", map[string]string{
		"app_public_key":        base64.RawURLEncoding.EncodeToString(appPublic),
		"app_private_key":       "must-not-leak",
		"app_display_name":      "Alice Phone",
		"certificate_payload":   payload.Body,
		"certificate_signature": payload.Signature,
		"expires_at":            "2026-05-03T06:08:00Z",
	}, auth.AccessToken)
	if rejectedCert.Code != http.StatusForbidden {
		t.Fatalf("private-key cert status = %d body=%s", rejectedCert.Code, rejectedCert.Body.String())
	}
	cert := postJSON(t, router, "/api/v1/machines/"+boot.Machine.ID+"/app-certificates", map[string]string{
		"app_public_key":        base64.RawURLEncoding.EncodeToString(appPublic),
		"app_display_name":      "Alice Phone",
		"certificate_payload":   payload.Body,
		"certificate_signature": payload.Signature,
		"expires_at":            "2026-05-03T06:08:00Z",
	}, auth.AccessToken)
	if cert.Code != http.StatusCreated {
		t.Fatalf("cert status = %d body=%s", cert.Code, cert.Body.String())
	}
	assertBodyHasNoPrivateMaterial(t, cert.Body.String())
	var createdCert struct {
		ID string `json:"id"`
	}
	decodeJSON(t, cert, &createdCert)
	if createdCert.ID == "" {
		t.Fatal("cert did not return id")
	}

	revoke := requestJSON(t, router, http.MethodDelete, "/api/v1/machines/"+boot.Machine.ID+"/app-certificates/"+createdCert.ID, nil, auth.AccessToken)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d body=%s", revoke.Code, revoke.Body.String())
	}
}

type signedHTTPCertificatePayload struct {
	Body      string
	Signature string
}

func signedHTTPPayload(t *testing.T, privateKey ed25519.PrivateKey, body map[string]any) signedHTTPCertificatePayload {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signature := ed25519.Sign(privateKey, encoded)
	return signedHTTPCertificatePayload{
		Body:      string(encoded),
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}
}

func assertBodyHasNoPrivateMaterial(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	if strings.Contains(lower, "private_key") || strings.Contains(lower, "privatekey") || strings.Contains(lower, "must-not-leak") {
		t.Fatalf("response leaked private material: %s", body)
	}
}

func requestJSON(t *testing.T, handler http.Handler, method string, path string, body any, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		payload = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, payload)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
