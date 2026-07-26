package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/controller/control"
	controllerruntime "github.com/muxvia/muxvia/cloud/controller/runtime"
	"github.com/muxvia/muxvia/cloud/edge/controllerlink"
	edgeruntime "github.com/muxvia/muxvia/cloud/edge/runtime"
	"github.com/muxvia/muxvia/cloud/securetransport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	testEdgeID              = "edge-integration-1"
	testControllerServer    = "controller.test"
	testEdgePublicServer    = "edge.test"
	testControllerID        = "controller-integration-1"
	testControllerBootID    = "controller-boot-integration-1"
	testEdgeBootID          = "edge-boot-integration-1"
	testEdgeSoftwareVersion = "integration-test"
)

type certificateFiles struct {
	rootCA           string
	controllerCert   string
	controllerKey    string
	edgeIdentityCert string
	edgeIdentityKey  string
	edgePublicCert   string
	edgePublicKey    string
	rootPool         *x509.CertPool
}

func TestEdgeControllerHelloWelcomeOverMutualTLS(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startController(t, certificates)
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress:           "127.0.0.1:0",
		PublicCertificateFile:   certificates.edgePublicCert,
		PublicPrivateKeyFile:    certificates.edgePublicKey,
		ControllerAddress:       controllerRuntime.GRPCAddress(),
		ControllerServerName:    testControllerServer,
		ControllerCAFile:        certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert,
		IdentityPrivateKeyFile:  certificates.edgeIdentityKey,
		EdgeID:                  testEdgeID,
		BootID:                  testEdgeBootID,
		SoftwareVersion:         testEdgeSoftwareVersion,
	})
	if err != nil {
		t.Fatalf("start Edge: %v", err)
	}
	t.Cleanup(func() { shutdownEdge(t, edgeRuntime) })

	readyContext, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	if err := edgeRuntime.WaitReady(readyContext); err != nil {
		t.Fatalf("wait for real EdgeHello/EdgeWelcome: %v", err)
	}
	assertHTTPStatus(t, http.DefaultClient, "http://"+controllerRuntime.HealthAddress()+"/readyz", http.StatusOK)

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certificates.rootPool,
		ServerName: testEdgePublicServer,
	}}}
	t.Cleanup(httpClient.CloseIdleConnections)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/healthz", http.StatusOK)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/readyz", http.StatusOK)

	connection, err := grpc.NewClient(edgeRuntime.PublicAddress(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certificates.rootPool,
		ServerName: testEdgePublicServer,
	})))
	if err != nil {
		t.Fatalf("create Edge public gRPC health client: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	checkContext, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	response, err := grpc_health_v1.NewHealthClient(connection).Check(checkContext, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("check Edge public gRPC health: %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("Edge public gRPC health = %s, want SERVING", response.GetStatus())
	}
}

func TestControllerRejectsHelloIdentityMismatch(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	controllerRuntime := startController(t, certificates)
	tlsConfig, err := securetransport.NewClientTLSConfig(securetransport.ClientOptions{
		CertificateFile: certificates.edgeIdentityCert,
		PrivateKeyFile:  certificates.edgeIdentityKey,
		RootCAFile:      certificates.rootCA,
		ServerName:      testControllerServer,
	})
	if err != nil {
		t.Fatalf("load Edge identity TLS: %v", err)
	}
	openContext, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOpen()
	_, err = controllerlink.Open(openContext, controllerlink.Config{
		ControllerAddress: controllerRuntime.GRPCAddress(),
		TLSConfig:         tlsConfig,
		EdgeID:            "edge-does-not-match-certificate",
		BootID:            testEdgeBootID,
		SoftwareVersion:   testEdgeSoftwareVersion,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("mismatched Edge identity code = %s, want InvalidArgument; error: %v", status.Code(err), err)
	}
}

func TestEdgeStaysAliveButNotReadyWhileControllerIsUnavailable(t *testing.T) {
	certificates := newCertificateFiles(t, testEdgeID)
	edgeRuntime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
		ListenAddress:           "127.0.0.1:0",
		PublicCertificateFile:   certificates.edgePublicCert,
		PublicPrivateKeyFile:    certificates.edgePublicKey,
		ControllerAddress:       unavailableAddress(t),
		ControllerServerName:    testControllerServer,
		ControllerCAFile:        certificates.rootCA,
		IdentityCertificateFile: certificates.edgeIdentityCert,
		IdentityPrivateKeyFile:  certificates.edgeIdentityKey,
		EdgeID:                  testEdgeID,
		BootID:                  testEdgeBootID,
		SoftwareVersion:         testEdgeSoftwareVersion,
	})
	if err != nil {
		t.Fatalf("start Edge without Controller: %v", err)
	}
	t.Cleanup(func() { shutdownEdge(t, edgeRuntime) })

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    certificates.rootPool,
		ServerName: testEdgePublicServer,
	}}}
	t.Cleanup(httpClient.CloseIdleConnections)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/healthz", http.StatusOK)
	assertHTTPStatus(t, httpClient, "https://"+edgeRuntime.PublicAddress()+"/readyz", http.StatusServiceUnavailable)
}

func startController(t *testing.T, certificates certificateFiles) *controllerruntime.Runtime {
	t.Helper()
	service, err := control.NewService(control.Config{
		ControllerID:      testControllerID,
		ControllerBootID:  testControllerBootID,
		HeartbeatInterval: time.Second,
		HeartbeatTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("create EdgeControl service: %v", err)
	}
	runtime, err := controllerruntime.Start(controllerruntime.Config{
		GRPCListenAddress:   "127.0.0.1:0",
		HealthListenAddress: "127.0.0.1:0",
		TLSCertificateFile:  certificates.controllerCert,
		TLSPrivateKeyFile:   certificates.controllerKey,
		EdgeCAFile:          certificates.rootCA,
	}, service)
	if err != nil {
		t.Fatalf("start Controller: %v", err)
	}
	t.Cleanup(func() {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if err := runtime.Shutdown(shutdownContext); err != nil {
			t.Errorf("shutdown Controller: %v", err)
		}
	})
	return runtime
}

func shutdownEdge(t *testing.T, runtime *edgeruntime.Runtime) {
	t.Helper()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := runtime.Shutdown(shutdownContext); err != nil {
		t.Errorf("shutdown Edge: %v", err)
	}
}

func assertHTTPStatus(t *testing.T, client *http.Client, target string, expected int) {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != expected {
		t.Fatalf("GET %s status = %d, want %d", target, response.StatusCode, expected)
	}
}

func unavailableAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve unavailable address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release unavailable address: %v", err)
	}
	return address
}

func newCertificateFiles(t *testing.T, edgeID string) certificateFiles {
	t.Helper()
	directory := t.TempDir()
	caCertificate, caKey := newCertificateAuthority(t)
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate.Raw})
	rootPath := writeTestFile(t, directory, "root-ca.pem", rootPEM)
	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(rootPEM) {
		t.Fatal("append generated root CA")
	}
	controllerCert, controllerKey := issueCertificate(t, caCertificate, caKey, certificateRequest{
		commonName:  testControllerServer,
		dnsNames:    []string{testControllerServer},
		extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	edgeIdentityURI, err := securetransport.EdgeIdentityURI(edgeID)
	if err != nil {
		t.Fatalf("create Edge identity URI: %v", err)
	}
	edgeIdentityCert, edgeIdentityKey := issueCertificate(t, caCertificate, caKey, certificateRequest{
		commonName:  edgeID,
		uris:        []*url.URL{edgeIdentityURI},
		extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	edgePublicCert, edgePublicKey := issueCertificate(t, caCertificate, caKey, certificateRequest{
		commonName:  testEdgePublicServer,
		dnsNames:    []string{testEdgePublicServer},
		extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return certificateFiles{
		rootCA:           rootPath,
		controllerCert:   writeTestFile(t, directory, "controller-cert.pem", controllerCert),
		controllerKey:    writeTestFile(t, directory, "controller-key.pem", controllerKey),
		edgeIdentityCert: writeTestFile(t, directory, "edge-identity-cert.pem", edgeIdentityCert),
		edgeIdentityKey:  writeTestFile(t, directory, "edge-identity-key.pem", edgeIdentityKey),
		edgePublicCert:   writeTestFile(t, directory, "edge-public-cert.pem", edgePublicCert),
		edgePublicKey:    writeTestFile(t, directory, "edge-public-key.pem", edgePublicKey),
		rootPool:         rootPool,
	}
}

type certificateRequest struct {
	commonName  string
	dnsNames    []string
	uris        []*url.URL
	extendedUse []x509.ExtKeyUsage
}

func newCertificateAuthority(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key := newPrivateKey(t)
	template := &x509.Certificate{
		SerialNumber:          newSerialNumber(t),
		Subject:               pkix.Name{CommonName: "Muxvia integration root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create root CA: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse root CA: %v", err)
	}
	return certificate, key
}

func issueCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, request certificateRequest) ([]byte, []byte) {
	t.Helper()
	key := newPrivateKey(t)
	template := &x509.Certificate{
		SerialNumber: newSerialNumber(t),
		Subject:      pkix.Name{CommonName: request.commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  request.extendedUse,
		DNSNames:     request.dnsNames,
		URIs:         request.uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("issue %s certificate: %v", request.commonName, err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal %s private key: %v", request.commonName, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func newPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	return key
}

func newSerialNumber(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate certificate serial: %v", err)
	}
	return serial
}

func writeTestFile(t *testing.T, directory, name string, payload []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
