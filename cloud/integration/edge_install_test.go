package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/apihttp"
	"github.com/anytty/anytty/cloud/controller/certificate"
	"github.com/anytty/anytty/cloud/controller/commerce"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	"github.com/anytty/anytty/cloud/controller/install"
	operatorservice "github.com/anytty/anytty/cloud/controller/operator"
	"github.com/anytty/anytty/cloud/controller/postgres"
	"github.com/anytty/anytty/cloud/edge/bootstrap"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

func TestR3EdgeCreateInstallRegisterAndListWithPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate R3 schema: %v", err)
	}

	_, configKey, _ := ed25519.GenerateKey(rand.Reader)
	_, artifactKey, _ := ed25519.GenerateKey(rand.Reader)
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: database, SigningKey: configKey, SigningKeyID: "r3-config-test", ClaimTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, caKey := newCertificateAuthority(t)
	temporary := t.TempDir()
	caCertificateFile := writeTestFile(t, temporary, "edge-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate.Raw}))
	caKeyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	caKeyFile := writeTestFile(t, temporary, "edge-ca-key.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKeyDER}))
	artifactFile := writeTestFile(t, temporary, "anytty-cloud-edge", []byte("r3-test-artifact"))
	installer, err := install.NewService(install.Config{
		Edges: edges, PublicOrigin: "https://controller.example.com:18444", ControllerAddress: "controller.example.com:18443", ControllerServerName: "controller.example.com",
		EdgeCACertificateFile: caCertificateFile, EdgeCAPrivateKeyFile: caKeyFile, ControllerCAFile: caCertificateFile,
		ArtifactFile: artifactFile, ArtifactVersion: "r3-test", ArtifactSigningKey: artifactKey, CertificateValidity: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	directoryState, err := directory.New(directory.Config{MailboxSize: 128, GracePeriod: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer directoryState.Close()
	accounts, err := account.New(account.Config{Store: database, AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, RecentAuthenticationTTL: 10 * time.Minute, BcryptCost: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.EnsureBootstrapOperator(ctx, "operator", "test-password"); err != nil {
		t.Fatal(err)
	}
	commercial, err := commerce.New(commerce.Config{Store: database})
	if err != nil {
		t.Fatal(err)
	}
	_, ticketKey, _ := ed25519.GenerateKey(rand.Reader)
	enrollmentService, err := enrollment.NewService(enrollment.Config{Store: database, Edges: edges, Directory: directoryState, Entitlement: commercial, TicketSigningKey: ticketKey, TicketSigningKeyID: "r7-http", EdgeCACertificate: []byte("test-ca"), EnrollmentTTL: 10 * time.Minute, ChallengeTTL: time.Minute, AgentTicketTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	controlService, err := control.NewService(control.Config{ControllerID: "controller-test", ControllerBootID: uuid.NewString(), HeartbeatInterval: time.Second, HeartbeatTimeout: 3 * time.Second, Directory: directoryState})
	if err != nil {
		t.Fatal(err)
	}
	secretStore, err := certificate.NewFileSecretStore(filepath.Join(temporary, "certificate-secrets"))
	if err != nil {
		t.Fatal(err)
	}
	certificateService, err := certificate.New(certificate.Config{Store: database, Secrets: secretStore, Edges: edges, Dispatcher: controlService, Online: func(ctx context.Context, edgeID string) (bool, error) {
		_, found, err := directoryState.Edge(ctx, edgeID)
		return found, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	operatorService, err := operatorservice.New(operatorservice.Config{Store: database, Edges: edges, Enrollment: enrollmentService, Directory: directoryState, Control: controlService, Certificates: certificateService})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := apihttp.NewHandler(apihttp.Config{PublicOrigin: "https://controller.example.com:18444", Edges: edges, Directory: directoryState, Install: installer, Enrollment: enrollmentService, Accounts: accounts, Commerce: commercial, Operator: operatorService, Certificates: certificateService})
	if err != nil {
		t.Fatal(err)
	}
	loginBody, _ := protojson.Marshal(&cloudv1.LoginAccountRequest{Login: "operator", Password: "test-password"})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/account/login", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("operator login status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	r7TestCookies = loginRecorder.Result().Cookies()
	var accessCookie, refreshCookie, csrfCookie *http.Cookie
	for _, cookie := range r7TestCookies {
		switch cookie.Name {
		case "anytty_cloud_access":
			accessCookie = cookie
		case "anytty_cloud_refresh":
			refreshCookie = cookie
		case "anytty_cloud_csrf":
			csrfCookie = cookie
			r7TestCSRF = cookie.Value
		}
	}
	if accessCookie == nil || !accessCookie.Secure || !accessCookie.HttpOnly || accessCookie.SameSite != http.SameSiteStrictMode || accessCookie.Path != "/" {
		t.Fatalf("invalid access cookie: %+v", accessCookie)
	}
	if refreshCookie == nil || !refreshCookie.Secure || !refreshCookie.HttpOnly || refreshCookie.SameSite != http.SameSiteStrictMode || refreshCookie.Path != "/api/account/refresh" {
		t.Fatalf("invalid refresh cookie: %+v", refreshCookie)
	}
	if csrfCookie == nil || !csrfCookie.Secure || csrfCookie.HttpOnly || csrfCookie.SameSite != http.SameSiteStrictMode || csrfCookie.Path != "/" || r7TestCSRF == "" {
		t.Fatalf("invalid CSRF cookie: %+v", csrfCookie)
	}
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/operator/edges", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated operator status=%d", unauthenticated.Code)
	}
	csrfBody, _ := protojson.Marshal(&cloudv1.CreateEdgeRequest{Name: "CSRF", Region: "test", Capacity: 1, PublicEndpoint: "csrf.example.com"})
	csrfRequest := httptest.NewRequest(http.MethodPost, "/api/operator/edges", bytes.NewReader(csrfBody))
	csrfRequest.Header.Set("Content-Type", "application/json")
	for _, cookie := range r7TestCookies {
		csrfRequest.AddCookie(cookie)
	}
	csrfRecorder := httptest.NewRecorder()
	handler.ServeHTTP(csrfRecorder, csrfRequest)
	if csrfRecorder.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", csrfRecorder.Code, csrfRecorder.Body.String())
	}
	spaRequest := httptest.NewRequest(http.MethodGet, "/accounts/11111111-1111-4111-8111-111111111111", nil)
	for _, cookie := range r7TestCookies {
		spaRequest.AddCookie(cookie)
	}
	spaRecorder := httptest.NewRecorder()
	handler.ServeHTTP(spaRecorder, spaRequest)
	if spaRecorder.Code != http.StatusOK || !strings.Contains(spaRecorder.Header().Get("Content-Type"), "text/html") || !strings.Contains(spaRecorder.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("SPA route status=%d content-type=%q", spaRecorder.Code, spaRecorder.Header().Get("Content-Type"))
	}

	create := &cloudv1.CreateEdgeRequest{Name: "测试 Edge", Region: "cn-east", Capacity: 1000, PublicEndpoint: "edge-r3.example.com:41102"}
	createResponse := &cloudv1.CreateEdgeResponse{}
	doProtoRequest(t, handler, http.MethodPost, "/api/operator/edges", create, createResponse, true, http.StatusCreated)
	if createResponse.GetEdge().GetRuntime().GetOnline() || !strings.Contains(createResponse.GetInstallCommand(), "/install/edge/") {
		t.Fatalf("create response = %+v", createResponse)
	}
	installToken := strings.Split(strings.Split(createResponse.GetInstallCommand(), "/install/edge/")[1], " ")[0]
	scriptRequest := httptest.NewRequest(http.MethodGet, "/install/edge/"+installToken, nil)
	scriptRecorder := httptest.NewRecorder()
	handler.ServeHTTP(scriptRecorder, scriptRequest)
	if scriptRecorder.Code != http.StatusOK || !strings.Contains(scriptRecorder.Body.String(), "openssl pkeyutl -verify") {
		t.Fatalf("install script status=%d body=%s", scriptRecorder.Code, scriptRecorder.Body.String())
	}
	if !strings.Contains(scriptRecorder.Body.String(), "install -d -o root -g anytty-edge -m 0770 /etc/anytty-cloud-edge") {
		t.Fatal("install script does not permit the Edge service to atomically replace its bootstrap config")
	}
	syntaxCheck := exec.Command("sh", "-n")
	syntaxCheck.Stdin = strings.NewReader(scriptRecorder.Body.String())
	if output, err := syntaxCheck.CombinedOutput(); err != nil {
		t.Fatalf("install script syntax: %v: %s", err, output)
	}
	reusedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reusedRecorder, httptest.NewRequest(http.MethodGet, "/install/edge/"+installToken, nil))
	if reusedRecorder.Code != http.StatusGone {
		t.Fatalf("reused install token status=%d", reusedRecorder.Code)
	}
	match := regexp.MustCompile(`bootstrap_token: "([^"]+)"`).FindStringSubmatch(scriptRecorder.Body.String())
	if len(match) != 2 {
		t.Fatal("bootstrap token is missing from generated script")
	}

	edgeID := createResponse.GetEdge().GetConfig().GetEdgeId()
	identityURI, _ := securetransport.EdgeIdentityURI(edgeID)
	identityCSR := createTestCSR(t, pkix.Name{CommonName: edgeID}, nil, []*url.URL{identityURI})
	publicCSR := createTestCSR(t, pkix.Name{CommonName: "edge-r3.example.com"}, []string{"edge-r3.example.com"}, nil)
	register := &cloudv1.RegisterEdgeRequest{EdgeId: edgeID, BootstrapToken: match[1], IdentityCsrPem: identityCSR, PublicCsrPem: publicCSR}
	registerResponse := &cloudv1.RegisterEdgeResponse{}
	doProtoRequest(t, handler, http.MethodPost, "/api/install/register", register, registerResponse, false, http.StatusOK)
	if len(registerResponse.GetIdentityCertificatePem()) == 0 || len(registerResponse.GetPublicCertificatePem()) == 0 || len(registerResponse.GetConfigSigningPublicKey()) != ed25519.PublicKeySize {
		t.Fatalf("registration response is incomplete: %+v", registerResponse)
	}
	doProtoRequest(t, handler, http.MethodPost, "/api/install/register", register, &cloudv1.RegisterEdgeResponse{}, false, http.StatusForbidden)

	list := &cloudv1.ListEdgesResponse{}
	doProtoRequest(t, handler, http.MethodGet, "/api/operator/edges", nil, list, true, http.StatusOK)
	var found bool
	for _, listed := range list.GetEdges() {
		if listed.GetConfig().GetEdgeId() == edgeID {
			found = true
			if listed.GetRuntime().GetOnline() {
				t.Fatal("database config was incorrectly reported online")
			}
		}
	}
	if !found {
		t.Fatal("created Edge is absent from operator list")
	}

	t.Run("R8 certificate HTTP API keeps private material out of responses", func(t *testing.T) {
		certificatePEM, privateKeyPEM := issueCertificate(t, caCertificate, caKey, certificateRequest{
			commonName: "edge-r3.example.com", dnsNames: []string{"edge-r3.example.com"}, extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		_, wrongPrivateKeyPEM := issueCertificate(t, caCertificate, caKey, certificateRequest{
			commonName: "edge-r3.example.com", dnsNames: []string{"edge-r3.example.com"}, extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		upload := &cloudv1.UploadCertificateProfileResponse{}
		doProtoRequest(t, handler, http.MethodPost, "/api/operator/certificates", &cloudv1.UploadCertificateProfileRequest{
			Name: "R8 HTTP Certificate", CertificateChainPem: certificatePEM, PrivateKeyPem: privateKeyPEM,
		}, upload, true, http.StatusOK)
		profileID := upload.GetProfile().GetCertificateProfileId()
		if profileID == "" || upload.GetProfile().GetRevision() != 1 {
			t.Fatalf("uploaded HTTP certificate profile=%+v", upload.GetProfile())
		}
		projection, err := protojson.Marshal(upload)
		if err != nil {
			t.Fatal(err)
		}
		assertNoCertificateMaterial(t, "certificate upload HTTP response", projection, certificatePEM, privateKeyPEM)

		binding := &cloudv1.BindCertificateProfileResponse{}
		doProtoRequest(t, handler, http.MethodPost, "/api/operator/edges/"+edgeID+"/certificate", &cloudv1.BindCertificateProfileRequest{
			EdgeId: edgeID, CertificateProfileId: profileID,
		}, binding, true, http.StatusOK)
		if binding.GetBinding().GetSyncState() != cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_PENDING || binding.GetBinding().GetOnline() {
			t.Fatalf("offline HTTP binding=%+v want pending", binding.GetBinding())
		}

		doProtoRequest(t, handler, http.MethodPut, "/api/operator/certificates/"+profileID, &cloudv1.UploadCertificateProfileRequest{
			CertificateProfileId: profileID, ExpectedRevision: 1, Name: "R8 HTTP Certificate", CertificateChainPem: certificatePEM, PrivateKeyPem: wrongPrivateKeyPEM,
		}, nil, true, http.StatusConflict)
		listed := &cloudv1.ListCertificateProfilesResponse{}
		doProtoRequest(t, handler, http.MethodGet, "/api/operator/certificates", nil, listed, true, http.StatusOK)
		var listedProfile *cloudv1.CertificateProfile
		for _, candidate := range listed.GetProfiles() {
			if candidate.GetCertificateProfileId() == profileID {
				listedProfile = candidate
				break
			}
		}
		if listedProfile == nil || listedProfile.GetRevision() != 1 {
			t.Fatalf("failed HTTP replacement changed profile: %+v", listed.GetProfiles())
		}
		projection, err = protojson.Marshal(listed)
		if err != nil {
			t.Fatal(err)
		}
		assertNoCertificateMaterial(t, "certificate list HTTP response", projection, certificatePEM, privateKeyPEM)
	})

	bootstrapEdge, bootstrapInstallToken, _, err := edges.CreateEdge(ctx, edgeconfig.CreateInput{Name: "Bootstrap Edge", Region: "cn-east", Capacity: 500, PublicEndpoint: "edge-bootstrap.example.com:41103"})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapScript, err := installer.InstallScript(ctx, bootstrapInstallToken)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapMatch := regexp.MustCompile(`bootstrap_token: "([^"]+)"`).FindStringSubmatch(bootstrapScript)
	if len(bootstrapMatch) != 2 {
		t.Fatal("second bootstrap token is missing")
	}
	tlsServer := httptest.NewTLSServer(handler)
	defer tlsServer.Close()
	stateDirectory := filepath.Join(temporary, "bootstrap-state")
	configFile := filepath.Join(temporary, "bootstrap-config.yaml")
	fileConfig := bootstrap.FileConfig{ControllerOrigin: tlsServer.URL, RegisterURL: tlsServer.URL + "/api/install/register", EdgeID: bootstrapEdge.ID, BootstrapToken: bootstrapMatch[1], StateDirectory: stateDirectory, PublicEndpoint: bootstrapEdge.PublicEndpoint, ListenOverride: "127.0.0.1:41103", LogLevel: "info"}
	configPayload, _ := yaml.Marshal(fileConfig)
	if err := os.WriteFile(configFile, configPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := bootstrap.Resolve(ctx, configFile, tlsServer.Client())
	if err != nil {
		t.Fatalf("resolve real Edge bootstrap: %v", err)
	}
	if resolved.EdgeID != bootstrapEdge.ID || resolved.ControllerAddress != "controller.example.com:18443" {
		t.Fatalf("resolved bootstrap = %+v", resolved)
	}
	persistedConfig, err := os.ReadFile(configFile)
	if err != nil || bytes.Contains(persistedConfig, []byte(bootstrapMatch[1])) {
		t.Fatalf("bootstrap credential remains in config: err=%v config=%s", err, persistedConfig)
	}
	for _, keyPath := range []string{resolved.IdentityPrivateKeyFile, resolved.PublicPrivateKeyFile} {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("stat private key %s: %v", keyPath, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("private key mode %s = %v", keyPath, info.Mode().Perm())
		}
	}
}

func TestR4DaemonEnrollmentConsumesCodeAndPersistsOnlyIdentity(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, configKey, _ := ed25519.GenerateKey(rand.Reader)
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: database, SigningKey: configKey, SigningKeyID: "r4-config", ClaimTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	directoryState, err := directory.New(directory.Config{MailboxSize: 128, GracePeriod: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer directoryState.Close()
	_, ticketKey, _ := ed25519.GenerateKey(rand.Reader)
	service, err := enrollment.NewService(enrollment.Config{Store: database, Edges: edges, Directory: directoryState, Entitlement: testEntitlementReader{}, TicketSigningKey: ticketKey, TicketSigningKeyID: "r4-ticket", EdgeCACertificate: []byte("test-ca"), EnrollmentTTL: 10 * time.Minute, ChallengeTTL: time.Minute, AgentTicketTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := account.New(account.Config{Store: database, AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, RecentAuthenticationTTL: 10 * time.Minute, BcryptCost: 4})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := accounts.Register(ctx, &cloudv1.RegisterAccountRequest{Email: "r4-" + uuid.NewString() + "@example.com", Password: "r4-test-password", DisplayName: "R4 测试账号"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateEnrollment(ctx, &cloudv1.CreateDaemonEnrollmentRequest{AccountId: registered.GetAccount().GetAccountId(), AccountName: registered.GetAccount().GetDisplayName(), DaemonName: "R4 Daemon"}, "anytty cloud enroll --controller https://controller.test")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.LoadOrCreateLocalIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	begin := &cloudv1.BeginDaemonEnrollmentRequest{EnrollmentCode: created.GetEnrollmentCode(), DeviceId: identity.DeviceID, DeviceFingerprint: identity.Fingerprint, DevicePublicKey: identity.PublicKey}
	challenge, err := service.BeginDaemonEnrollment(ctx, begin)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := remoteauth.SignDeviceIdentityProof(identity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteDaemonEnrollment(ctx, &cloudv1.CompleteDaemonEnrollmentRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof})
	if err != nil {
		t.Fatal(err)
	}
	if completed.GetDaemon().GetAccountId() != created.GetAccountId() || completed.GetDaemon().GetDeviceFingerprint() != identity.Fingerprint {
		t.Fatalf("completed daemon=%+v", completed.GetDaemon())
	}
	replayChallenge, err := service.BeginDaemonEnrollment(ctx, begin)
	if err != nil {
		t.Fatal(err)
	}
	replayProof, _ := remoteauth.SignDeviceIdentityProof(identity, replayChallenge.GetChallenge())
	if _, err := service.CompleteDaemonEnrollment(ctx, &cloudv1.CompleteDaemonEnrollmentRequest{ChallengeId: replayChallenge.GetChallengeId(), DeviceProof: replayProof}); err == nil {
		t.Fatal("consumed enrollment code was accepted again")
	}
	managed, err := service.ListManagedDaemons(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range managed.GetDaemons() {
		if item.GetDaemon().GetDaemonId() == completed.GetDaemon().GetDaemonId() {
			found = true
			if item.GetRuntime().GetOnline() {
				t.Fatal("persistent daemon identity was incorrectly reported online")
			}
		}
	}
	if !found {
		t.Fatal("enrolled daemon is absent from persistent list")
	}
}

func doProtoRequest(t *testing.T, handler http.Handler, method, path string, input, output proto.Message, auth bool, expectedStatus int) {
	t.Helper()
	var body []byte
	if input != nil {
		body, _ = protojson.Marshal(input)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if auth {
		for _, cookie := range r7TestCookies {
			request.AddCookie(cookie)
		}
		if method != http.MethodGet && method != http.MethodHead {
			request.Header.Set("X-AnyTTY-CSRF", r7TestCSRF)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != expectedStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, recorder.Code, expectedStatus, recorder.Body.String())
	}
	if output != nil && recorder.Code < 300 {
		if err := protojson.Unmarshal(recorder.Body.Bytes(), output); err != nil {
			t.Fatalf("decode %s response: %v", path, err)
		}
	}
}

var r7TestCookies []*http.Cookie
var r7TestCSRF string

func createTestCSR(t *testing.T, subject pkix.Name, dnsNames []string, uris []*url.URL) []byte {
	t.Helper()
	key := newPrivateKey(t)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: subject, DNSNames: dnsNames, URIs: uris}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}
