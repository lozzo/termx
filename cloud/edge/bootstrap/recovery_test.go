package bootstrap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

func TestRecoverIdentityGeneratesLocalKeyAndClearsOneTimeToken(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	edgeID := "edge-bootstrap-recovery"
	directory := t.TempDir()
	ca, caKey, caPEM := recoveryTestCA(t, now)
	paths := Resolved{
		IdentityCertificateFile: filepath.Join(directory, "identity-cert.pem"), IdentityPrivateKeyFile: filepath.Join(directory, "identity-key.pem"),
		IdentityCACertificateFile: filepath.Join(directory, "edge-ca.pem"), ManagedIdentityStateFile: filepath.Join(directory, "managed-identity.pem"),
	}
	if err := os.WriteFile(paths.IdentityCACertificateFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/api/install/recover-identity" {
			http.Error(writer, "unexpected request", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		input := &cloudv1.RecoverEdgeIdentityRequest{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, input); err != nil {
			t.Error(err)
			return
		}
		if input.GetEdgeId() != edgeID || input.GetRecoveryToken() != "one-time-recovery-token" || bytes.Contains(body, []byte("PRIVATE KEY")) {
			t.Errorf("recovery request edge=%q token=%q body=%s", input.GetEdgeId(), input.GetRecoveryToken(), body)
			return
		}
		csr := recoveryTestCSR(t, input.GetIdentityCsrPem())
		expectedURI, err := securetransport.EdgeIdentityURI(edgeID)
		if err != nil {
			t.Error(err)
			return
		}
		if len(csr.URIs) != 1 || csr.URIs[0].String() != expectedURI.String() {
			t.Errorf("CSR URIs = %v", csr.URIs)
			return
		}
		notAfter := now.Add(90 * 24 * time.Hour)
		template := &x509.Certificate{
			SerialNumber: big.NewInt(2), Subject: csr.Subject, NotBefore: now.Add(-time.Minute), NotAfter: notAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: csr.URIs,
		}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, csr.PublicKey, caKey)
		if err != nil {
			t.Error(err)
			return
		}
		digest := sha256.Sum256(der)
		payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(&cloudv1.RecoverEdgeIdentityResponse{
			IdentityCertificatePem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), CertificateSha256: digest[:], NotAfter: timestamppb.New(notAfter),
		})
		if err != nil {
			t.Error(err)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	config := FileConfig{ControllerOrigin: server.URL, EdgeID: edgeID, IdentityRecoveryToken: "one-time-recovery-token"}
	configFile := filepath.Join(directory, "config.yaml")
	configPayload, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, configPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverIdentity(context.Background(), configFile, &config, paths, server.Client()); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || config.IdentityRecoveryToken != "" {
		t.Fatalf("requests=%d token=%q", requests, config.IdentityRecoveryToken)
	}
	updatedConfig, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updatedConfig), "identity_recovery_token") || strings.Contains(string(updatedConfig), "one-time-recovery-token") {
		t.Fatalf("recovery token remained in config: %s", updatedConfig)
	}
	managed, err := os.ReadFile(paths.ManagedIdentityStateFile)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(managed, managed)
	if err != nil {
		t.Fatalf("managed recovery credential does not contain a matching key: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != "spiffe://anytty.com/edge/"+edgeID {
		t.Fatalf("managed recovery identity URIs = %v", leaf.URIs)
	}
	info, err := os.Stat(paths.ManagedIdentityStateFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("managed recovery mode=%v", info.Mode().Perm())
	}
}

func recoveryTestCA(t *testing.T, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Recovery Test CA"}, IsCA: true, BasicConstraintsValid: true,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func recoveryTestCSR(t *testing.T, payload []byte) *x509.CertificateRequest {
	t.Helper()
	block, trailing := pem.Decode(payload)
	if block == nil || len(bytes.TrimSpace(trailing)) != 0 {
		t.Fatal("invalid recovery CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatal(err)
	}
	return csr
}
