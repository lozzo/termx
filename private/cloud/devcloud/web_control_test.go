package devcloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

func TestWebAccountApprovesClientAndOwnsDaemonEnrollment(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "bootstrap-only", WebAccountDBPath: filepath.Join(t.TempDir(), "accounts.db"), WebCatalogPath: "../web-controller/config/plans.json", WebStaging: true, WebPublicURL: "https://cloud.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	origin := runtime.Manifest().ControlPlaneURL
	cookies, accountID := registerWebAccount(t, origin, "owner@example.com")

	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: origin, HubURL: runtime.Manifest().HubURL, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	credentialStore := &memoryCredentialStore{secrets: make(map[string][]byte)}
	_, clientService := newTestCompanion(t, credentialStore, "web-owner-client", clock, adapter, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
	client := clientService.NewConnection()
	mustHello(t, client, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
	flow, err := client.BeginLogin(context.Background(), &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if err != nil || flow.GetUserCode() == "" || flow.GetVerificationUri() != "https://cloud.example.test/device?code="+flow.GetUserCode() {
		t.Fatalf("login flow = (%v, %v)", flow, err)
	}
	if _, err = client.CompleteLogin(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY) {
		t.Fatalf("unapproved login = %v", err)
	}
	deviceRequest, _ := http.NewRequest(http.MethodGet, origin+"/api/device-login?code="+flow.GetUserCode(), nil)
	addCookies(deviceRequest, cookies)
	deviceResponse, err := http.DefaultClient.Do(deviceRequest)
	if err != nil || deviceResponse.StatusCode != http.StatusOK {
		t.Fatalf("inspect login = (%v, %v)", deviceResponse, err)
	}
	deviceResponse.Body.Close()
	approved := browserMutation(t, origin, "/api/device-login/approve", map[string]string{"user_code": flow.GetUserCode()}, cookies)
	if approved.StatusCode != http.StatusOK {
		t.Fatalf("approve login = %d", approved.StatusCode)
	}
	approved.Body.Close()
	login, err := client.CompleteLogin(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
	if err != nil || login.GetSession().GetAccountId() != accountID || login.GetSession().GetDeviceId() == devClientDeviceID {
		t.Fatalf("completed login = (%v, %v)", login, err)
	}
	if replay := browserMutation(t, origin, "/api/device-login/approve", map[string]string{"user_code": flow.GetUserCode()}, cookies); replay.StatusCode != http.StatusConflict {
		replay.Body.Close()
		t.Fatalf("approval replay = %d", replay.StatusCode)
	} else {
		replay.Body.Close()
	}

	enrollmentResponse := browserMutation(t, origin, "/api/nodes/enrollment", map[string]string{}, cookies)
	if enrollmentResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create enrollment = %d", enrollmentResponse.StatusCode)
	}
	var enrollmentCode struct {
		Code string `json:"code"`
	}
	if err = json.NewDecoder(enrollmentResponse.Body).Decode(&enrollmentCode); err != nil {
		t.Fatal(err)
	}
	enrollmentResponse.Body.Close()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	_, daemonService := newTestCompanion(t, credentialStore, "web-owner-daemon", clock, adapter, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT)
	daemon := daemonService.NewConnection()
	mustHello(t, daemon, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT)
	challenge, err := daemon.BeginDeviceEnrollment(context.Background(), &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: enrollmentCode.Code, DevicePublicKey: publicKey, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Owner workstation", Platform: "test", TermxVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := signEnrollmentProof(t, privateKey, publicKey, "daemon-web-owner", challenge, clock.Now())
	enrolled, err := daemon.CompleteDeviceEnrollment(context.Background(), &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: challenge.GetFlowId(), Proof: proof})
	if err != nil || enrolled.GetSession().GetAccountId() != accountID {
		t.Fatalf("enrollment = (%v, %v)", enrolled, err)
	}
	if device := runtime.state.edgeDevices["daemon-web-owner"]; device.AccountID != accountID {
		t.Fatalf("Hub device account = %q", device.AccountID)
	}
	if _, err = daemon.BeginDeviceEnrollment(context.Background(), &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: enrollmentCode.Code, DevicePublicKey: publicKey, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Replay", Platform: "test", TermxVersion: "test"}}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED) {
		t.Fatalf("enrollment replay = %v", err)
	}
}

func registerWebAccount(t *testing.T, origin, email string) ([]*http.Cookie, string) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, origin+"/api/auth/password/register", bytes.NewBufferString(`{"email":"`+email+`","password":"secure-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusCreated {
		t.Fatalf("register = (%v, %v)", response, err)
	}
	cookies := response.Cookies()
	response.Body.Close()
	center, _ := http.NewRequest(http.MethodGet, origin+"/api/center", nil)
	addCookies(center, cookies)
	centerResponse, err := http.DefaultClient.Do(center)
	if err != nil {
		t.Fatal(err)
	}
	defer centerResponse.Body.Close()
	var value struct {
		Profile struct {
			AccountID string `json:"account_id"`
		} `json:"profile"`
	}
	if err = json.NewDecoder(centerResponse.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return cookies, value.Profile.AccountID
}

func browserMutation(t *testing.T, origin, path string, body any, cookies []*http.Cookie) *http.Response {
	t.Helper()
	encoded, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPost, origin+path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
		if cookie.Name == "termx_csrf" {
			request.Header.Set("X-TermX-CSRF", cookie.Value)
		}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func addCookies(request *http.Request, cookies []*http.Cookie) {
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
}

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
