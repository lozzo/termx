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

	"github.com/muxvia/muxvia/cloud/controller/apihttp"
	"github.com/muxvia/muxvia/cloud/controller/directory"
	"github.com/muxvia/muxvia/cloud/controller/edgeconfig"
	"github.com/muxvia/muxvia/cloud/controller/install"
	"github.com/muxvia/muxvia/cloud/controller/postgres"
	"github.com/muxvia/muxvia/cloud/edge/bootstrap"
	"github.com/muxvia/muxvia/cloud/securetransport"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

func TestR3EdgeCreateInstallRegisterAndListWithPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("MUXVIA_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MUXVIA_CLOUD_TEST_DATABASE_URL is not set")
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
	artifactFile := writeTestFile(t, temporary, "muxvia-cloud-edge", []byte("r3-test-artifact"))
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
	handler, err := apihttp.NewHandler(apihttp.Config{PublicOrigin: "https://controller.example.com:18444", OperatorUsername: "operator", OperatorPassword: "test-password", Edges: edges, Directory: directoryState, Install: installer})
	if err != nil {
		t.Fatal(err)
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
	if !strings.Contains(scriptRecorder.Body.String(), "install -d -o root -g muxvia-edge -m 0770 /etc/muxvia-cloud-edge") {
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

func doProtoRequest(t *testing.T, handler http.Handler, method, path string, input, output proto.Message, auth bool, expectedStatus int) {
	t.Helper()
	var body []byte
	if input != nil {
		body, _ = protojson.Marshal(input)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if auth {
		request.SetBasicAuth("operator", "test-password")
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

func createTestCSR(t *testing.T, subject pkix.Name, dnsNames []string, uris []*url.URL) []byte {
	t.Helper()
	key := newPrivateKey(t)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: subject, DNSNames: dnsNames, URIs: uris}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}
