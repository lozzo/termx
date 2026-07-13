package devcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func TestControlPlaneOwnsBrowserAccountAPI(t *testing.T) {
	runtime, err := Start(Config{EnrollmentCode: "web-control", WebAccountDBPath: filepath.Join(t.TempDir(), "accounts.db"), WebCatalogPath: "../web-controller/config/plans.json", WebStaging: true})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	origin := runtime.Manifest().ControlPlaneURL
	request, _ := http.NewRequest(http.MethodPost, origin+"/api/auth/password/register", bytes.NewBufferString(`{"email":"control@example.com","password":"secure-password","aff":"TERMXDEV"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d", response.StatusCode)
	}
	cookies := response.Cookies()
	response.Body.Close()
	centerRequest, _ := http.NewRequest(http.MethodGet, origin+"/api/center", nil)
	for _, cookie := range cookies {
		centerRequest.AddCookie(cookie)
	}
	centerResponse, err := http.DefaultClient.Do(centerRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer centerResponse.Body.Close()
	if centerResponse.StatusCode != http.StatusOK {
		t.Fatalf("center = %d", centerResponse.StatusCode)
	}
}

func TestControlPlaneRestoresPaidWebEntitlementAfterRestart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "accounts.db")
	config := Config{EnrollmentCode: "web-restart", WebAccountDBPath: databasePath, WebCatalogPath: "../web-controller/config/plans.json", WebStaging: true}
	runtime, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	origin := runtime.Manifest().ControlPlaneURL
	register, _ := http.NewRequest(http.MethodPost, origin+"/api/auth/password/register", bytes.NewBufferString(`{"email":"restart@example.com","password":"secure-password"}`))
	register.Header.Set("Content-Type", "application/json")
	register.Header.Set("Origin", origin)
	registered, err := http.DefaultClient.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	cookies := registered.Cookies()
	registered.Body.Close()
	centerRequest, _ := http.NewRequest(http.MethodGet, origin+"/api/center", nil)
	for _, cookie := range cookies {
		centerRequest.AddCookie(cookie)
	}
	centerResponse, err := http.DefaultClient.Do(centerRequest)
	if err != nil {
		t.Fatal(err)
	}
	var center struct {
		Profile struct {
			AccountID string `json:"account_id"`
		} `json:"profile"`
	}
	if err = json.NewDecoder(centerResponse.Body).Decode(&center); err != nil {
		t.Fatal(err)
	}
	centerResponse.Body.Close()
	csrf := ""
	for _, cookie := range cookies {
		if cookie.Name == "termx_csrf" {
			csrf = cookie.Value
		}
	}
	checkout, _ := http.NewRequest(http.MethodPost, origin+"/api/checkout", bytes.NewBufferString(`{"plan_id":"pro"}`))
	checkout.Header.Set("Content-Type", "application/json")
	checkout.Header.Set("Origin", origin)
	checkout.Header.Set("X-TermX-CSRF", csrf)
	for _, cookie := range cookies {
		checkout.AddCookie(cookie)
	}
	checkoutResponse, err := http.DefaultClient.Do(checkout)
	if err != nil {
		t.Fatal(err)
	}
	var order struct {
		ID string `json:"id"`
	}
	if err = json.NewDecoder(checkoutResponse.Body).Decode(&order); err != nil {
		t.Fatal(err)
	}
	checkoutResponse.Body.Close()
	confirmBody, _ := json.Marshal(map[string]string{"order_id": order.ID})
	confirm, _ := http.NewRequest(http.MethodPost, origin+"/api/checkout/confirm", bytes.NewReader(confirmBody))
	confirm.Header.Set("Content-Type", "application/json")
	confirm.Header.Set("Origin", origin)
	confirm.Header.Set("X-TermX-CSRF", csrf)
	for _, cookie := range cookies {
		confirm.AddCookie(cookie)
	}
	confirmed, err := http.DefaultClient.Do(confirm)
	if err != nil {
		t.Fatal(err)
	}
	confirmed.Body.Close()
	if confirmed.StatusCode != http.StatusOK {
		t.Fatalf("confirm = %d", confirmed.StatusCode)
	}
	if err = runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	budget, err := restarted.state.edgeAuth.RelayBudget(center.Profile.AccountID)
	if err != nil || budget.MaxConcurrency != 4 {
		t.Fatalf("restored budget = %#v, %v", budget, err)
	}
}
