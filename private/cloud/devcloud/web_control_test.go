package devcloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
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
	_, clientService := newTestCompanion(t, credentialStore, "web-owner-client", clock, adapter, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_DIRECTORY)
	client := clientService.NewConnection()
	mustHello(t, client, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_DIRECTORY)
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
	directory, err := client.ListManagedDevices(context.Background(), &cloudpb.ListManagedDevicesRequest{SchemaVersion: 1})
	if err != nil || len(directory.GetDevices()) != 2 {
		t.Fatalf("managed device directory = (%v, %v)", directory, err)
	}
	if err = runtime.state.webCenter.UpsertCloudDevice(accountID, "stale-client", "Old phone", "client", true); err != nil {
		t.Fatal(err)
	}
	staleRevoked := browserMutation(t, origin, "/api/nodes/revoke", map[string]string{"node_id": "stale-client"}, cookies)
	if staleRevoked.StatusCode != http.StatusOK {
		t.Fatalf("revoke stale client = %d", staleRevoked.StatusCode)
	}
	staleRevoked.Body.Close()
	if _, err = daemon.BeginDeviceEnrollment(context.Background(), &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: enrollmentCode.Code, DevicePublicKey: publicKey, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Replay", Platform: "test", TermxVersion: "test"}}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED) {
		t.Fatalf("enrollment replay = %v", err)
	}
	refreshResponse := browserMutation(t, origin, "/api/nodes/enrollment", map[string]string{}, cookies)
	if refreshResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create refresh enrollment = %d", refreshResponse.StatusCode)
	}
	var refreshCode struct {
		Code string `json:"code"`
	}
	if err = json.NewDecoder(refreshResponse.Body).Decode(&refreshCode); err != nil {
		t.Fatal(err)
	}
	refreshResponse.Body.Close()
	refreshChallenge, err := daemon.BeginDeviceEnrollment(context.Background(), &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: refreshCode.Code, DevicePublicKey: publicKey, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Owner workstation", Platform: "test", TermxVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	refreshProof := signEnrollmentProof(t, privateKey, publicKey, "daemon-web-owner", refreshChallenge, clock.Now())
	refreshed, err := daemon.CompleteDeviceEnrollment(context.Background(), &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: refreshChallenge.GetFlowId(), Proof: refreshProof})
	if err != nil || refreshed.GetSession().GetDeviceId() != "daemon-web-owner" {
		t.Fatalf("refresh enrollment = (%v, %v)", refreshed, err)
	}
	revoked := browserMutation(t, origin, "/api/nodes/revoke", map[string]string{"node_id": "daemon-web-owner"}, cookies)
	if revoked.StatusCode != http.StatusOK {
		t.Fatalf("revoke daemon = %d", revoked.StatusCode)
	}
	revoked.Body.Close()
	if device := runtime.state.edgeDevices["daemon-web-owner"]; !device.Revoked {
		t.Fatal("daemon revoke did not reach Hub authorization projection")
	}
	reenrollmentResponse := browserMutation(t, origin, "/api/nodes/enrollment", map[string]string{}, cookies)
	if reenrollmentResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create reenrollment = %d", reenrollmentResponse.StatusCode)
	}
	var reenrollmentCode struct {
		Code string `json:"code"`
	}
	if err = json.NewDecoder(reenrollmentResponse.Body).Decode(&reenrollmentCode); err != nil {
		t.Fatal(err)
	}
	reenrollmentResponse.Body.Close()
	reenrollmentChallenge, err := daemon.BeginDeviceEnrollment(context.Background(), &cloudpb.BeginDeviceEnrollmentRequest{OneTimeCode: strings.ToLower(strings.ReplaceAll(reenrollmentCode.Code, "-", " - ")), DevicePublicKey: publicKey, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Owner workstation", Platform: "test", TermxVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	reenrollmentProof := signEnrollmentProof(t, privateKey, publicKey, "daemon-web-owner", reenrollmentChallenge, clock.Now())
	reenrolled, err := daemon.CompleteDeviceEnrollment(context.Background(), &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: reenrollmentChallenge.GetFlowId(), Proof: reenrollmentProof})
	if err != nil || reenrolled.GetSession().GetAccountId() != accountID || runtime.state.edgeDevices["daemon-web-owner"].Revoked {
		t.Fatalf("reenrollment = (%v, %v, %#v)", reenrolled, err, runtime.state.edgeDevices["daemon-web-owner"])
	}
	clientID := login.GetSession().GetDeviceId()
	revokedClient := browserMutation(t, origin, "/api/nodes/revoke", map[string]string{"node_id": clientID}, cookies)
	if revokedClient.StatusCode != http.StatusOK {
		t.Fatalf("revoke client = %d", revokedClient.StatusCode)
	}
	revokedClient.Body.Close()
	if device := runtime.state.edgeDevices[clientID]; !device.Revoked {
		t.Fatal("client revoke did not reach Hub authorization projection")
	}
	runtime.state.mu.Lock()
	for _, active := range runtime.state.sessions {
		if active.deviceID == clientID {
			runtime.state.mu.Unlock()
			t.Fatal("revoked client cloud session remained active")
		}
	}
	runtime.state.mu.Unlock()
	if _, err := client.ListManagedDevices(context.Background(), &cloudpb.ListManagedDevicesRequest{SchemaVersion: 1}); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED) {
		t.Fatalf("revoked client directory reuse = %v", err)
	}
}

func TestWebCreatesMobileActivationAndApprovesClaimedDevice(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)}
	runtime, err := Start(Config{Now: clock.Now, EnrollmentCode: "bootstrap-only", WebAccountDBPath: filepath.Join(t.TempDir(), "accounts.db"), WebCatalogPath: "../web-controller/config/plans.json", WebStaging: true, WebPublicURL: "https://cloud.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	origin := runtime.Manifest().ControlPlaneURL
	ownerCookies, ownerAccountID := registerWebAccount(t, origin, "mobile-owner@example.com")
	otherCookies, _ := registerWebAccount(t, origin, "other-owner@example.com")

	createdResponse := browserMutation(t, origin, "/api/mobile-activations", map[string]string{}, ownerCookies)
	if createdResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create mobile activation = %d", createdResponse.StatusCode)
	}
	var created struct {
		UserCode  string    `json:"user_code"`
		QRPayload string    `json:"qr_payload"`
		State     string    `json:"state"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err = json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createdResponse.Body.Close()
	if created.UserCode == "" || created.QRPayload != "termx-cloud-activate:v1:"+created.UserCode || created.State != "waiting_for_device" || !created.ExpiresAt.After(clock.Now()) {
		t.Fatalf("created activation = %#v", created)
	}

	inspect := inspectDeviceLogin(t, origin, created.UserCode, ownerCookies)
	if inspect.State != "waiting_for_device" || inspect.ClientLabel != "" {
		t.Fatalf("unclaimed activation = %#v", inspect)
	}

	claimRequest := &cloudpb.ClaimMobileActivationRequest{
		UserCode:       created.UserCode,
		ClientMetadata: &cloudpb.DeviceMetadata{DisplayName: "Huawei JAD-AL00", Platform: "android", TermxVersion: "development"},
	}
	claimMobileActivation(t, origin, &cloudpb.ClaimMobileActivationRequest{UserCode: created.UserCode, ClientMetadata: &cloudpb.DeviceMetadata{DisplayName: strings.Repeat("x", 129), Platform: "android", TermxVersion: "development"}}, http.StatusBadRequest)
	claimMobileActivation(t, origin, &cloudpb.ClaimMobileActivationRequest{UserCode: "ZZZZZ-ZZZZZ", ClientMetadata: claimRequest.ClientMetadata}, http.StatusUnauthorized)
	flow := claimMobileActivation(t, origin, claimRequest, http.StatusOK)
	if flow.GetFlowId() == "" || flow.GetUserCode() != created.UserCode {
		t.Fatalf("claimed flow = %#v", flow)
	}
	inspect = inspectDeviceLogin(t, origin, created.UserCode, ownerCookies)
	if inspect.State != "waiting_for_approval" || inspect.ClientLabel != "Huawei JAD-AL00" || inspect.ClientPlatform != "android" {
		t.Fatalf("claimed activation = %#v", inspect)
	}

	wrongAccount := browserMutation(t, origin, "/api/device-login/approve", map[string]string{"user_code": created.UserCode}, otherCookies)
	if wrongAccount.StatusCode == http.StatusOK {
		wrongAccount.Body.Close()
		t.Fatal("browser-created activation was approved by another account")
	}
	wrongAccount.Body.Close()
	approved := browserMutation(t, origin, "/api/device-login/approve", map[string]string{"user_code": created.UserCode}, ownerCookies)
	if approved.StatusCode != http.StatusOK {
		t.Fatalf("approve mobile activation = %d", approved.StatusCode)
	}
	approved.Body.Close()

	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: origin, HubURL: runtime.Manifest().HubURL, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	credentialStore := &memoryCredentialStore{secrets: make(map[string][]byte)}
	_, clientService := newTestCompanion(t, credentialStore, "mobile-activation-client", clock, adapter, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
	client := clientService.NewConnection()
	mustHello(t, client, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
	completed, err := client.CompleteLogin(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
	if err != nil || completed.GetSession().GetAccountId() != ownerAccountID {
		t.Fatalf("complete mobile activation = (%v, %v)", completed, err)
	}
	claimMobileActivation(t, origin, claimRequest, http.StatusUnauthorized)
}

func TestLoginCodeGeneratorRetriesActiveCollision(t *testing.T) {
	first := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	second := []byte{8, 7, 6, 5, 4, 3, 2, 1}
	state := &serviceState{random: bytes.NewReader(append(append(append([]byte{}, first...), first...), second...)), loginCodes: make(map[string]string)}
	state.mu.Lock()
	code, err := state.newLoginCodeLocked()
	if err != nil {
		t.Fatal(err)
	}
	state.loginCodes[code] = "active-flow"
	retried, err := state.newLoginCodeLocked()
	state.mu.Unlock()
	if err != nil || retried == code || len(retried) != 11 {
		t.Fatalf("collision retry = (%q, %v), first %q", retried, err, code)
	}
}

type inspectedDeviceLogin struct {
	UserCode       string `json:"user_code"`
	State          string `json:"state"`
	ClientLabel    string `json:"client_label"`
	ClientPlatform string `json:"client_platform"`
}

func inspectDeviceLogin(t *testing.T, origin, code string, cookies []*http.Cookie) inspectedDeviceLogin {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, origin+"/api/device-login?code="+code, nil)
	addCookies(request, cookies)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("inspect device login = (%v, %v)", response, err)
	}
	defer response.Body.Close()
	var value inspectedDeviceLogin
	if err = json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func claimMobileActivation(t *testing.T, origin string, payload *cloudpb.ClaimMobileActivationRequest, wantStatus int) *cloudpb.LoginFlow {
	t.Helper()
	encoded, err := proto.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, origin+httpapi.ControlClaimMobileActivationPath, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", httpapi.ProtobufMediaType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("claim mobile activation = %d, want %d", response.StatusCode, wantStatus)
	}
	if wantStatus != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	flow := &cloudpb.LoginFlow{}
	if err = proto.Unmarshal(body, flow); err != nil {
		t.Fatal(err)
	}
	return flow
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
