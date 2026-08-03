package identity

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
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestManagerRotatesKeyAndAtomicallyLoadsRenewedCredential(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	edgeID := "edge-identity-test"
	directory := t.TempDir()
	ca, caKey, caPEM := testCA(t, now)
	bootstrapCertificate, bootstrapKey, bootstrapLeaf := testIdentityCredential(t, ca, caKey, edgeID, now, now.Add(31*24*time.Hour), nil)
	caFile := filepath.Join(directory, "edge-ca.pem")
	certificateFile := filepath.Join(directory, "identity-cert.pem")
	keyFile := filepath.Join(directory, "identity-key.pem")
	managedFile := filepath.Join(directory, "managed-identity.pem")
	writeTestFile(t, caFile, caPEM)
	writeTestFile(t, certificateFile, bootstrapCertificate)
	writeTestFile(t, keyFile, bootstrapKey)

	config := Config{
		EdgeID: edgeID, BootstrapCertificateFile: certificateFile, BootstrapPrivateKeyFile: keyFile,
		ManagedStateFile: managedFile, CACertificateFile: caFile, Now: func() time.Time { return now },
	}
	manager, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.RenewalDelay(now); got != 24*time.Hour {
		t.Fatalf("renewal delay = %v, want 24h", got)
	}
	request, err := manager.BeginRenewal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	csr := parseTestCSR(t, request.GetCsrPem())
	renewedPEM, _, renewedLeaf := testIdentityCredential(t, ca, caKey, edgeID, now, now.Add(90*24*time.Hour), csr)
	digest := sha256.Sum256(renewedLeaf.Raw)
	metadata, err := manager.Apply(context.Background(), &cloudv1.EdgeIdentityRenewResponse{
		RequestId: request.GetRequestId(), CertificatePem: renewedPEM, CertificateSha256: digest[:], NotAfter: timestamppb.New(renewedLeaf.NotAfter),
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(metadata.SHA256, certificateDigest(bootstrapLeaf)) || !bytes.Equal(metadata.SHA256, digest[:]) {
		t.Fatalf("renewed fingerprint = %x", metadata.SHA256)
	}
	bootstrapPublic := bootstrapLeaf.PublicKey.(*ecdsa.PublicKey)
	renewedPublic := renewedLeaf.PublicKey.(*ecdsa.PublicKey)
	if bootstrapPublic.X.Cmp(renewedPublic.X) == 0 && bootstrapPublic.Y.Cmp(renewedPublic.Y) == 0 {
		t.Fatal("renewal reused the bootstrap private key")
	}
	pair, err := manager.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil || pair.Leaf == nil || !bytes.Equal(pair.Leaf.Raw, renewedLeaf.Raw) {
		t.Fatalf("dynamic TLS credential was not switched: leaf=%v err=%v", pair.Leaf, err)
	}
	info, err := os.Stat(managedFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("managed credential mode = %o", info.Mode().Perm())
	}
	reloaded, err := New(config)
	if err != nil {
		t.Fatalf("reload managed credential: %v", err)
	}
	if !bytes.Equal(reloaded.Current().SHA256, digest[:]) {
		t.Fatalf("reloaded fingerprint = %x", reloaded.Current().SHA256)
	}
}

func TestManagerRejectsWrongIdentityWithoutReplacingCurrentCredential(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	edgeID := "edge-identity-test"
	directory := t.TempDir()
	ca, caKey, caPEM := testCA(t, now)
	bootstrapCertificate, bootstrapKey, bootstrapLeaf := testIdentityCredential(t, ca, caKey, edgeID, now, now.Add(31*24*time.Hour), nil)
	config := Config{
		EdgeID: edgeID, BootstrapCertificateFile: filepath.Join(directory, "identity-cert.pem"), BootstrapPrivateKeyFile: filepath.Join(directory, "identity-key.pem"),
		ManagedStateFile: filepath.Join(directory, "managed-identity.pem"), CACertificateFile: filepath.Join(directory, "edge-ca.pem"), Now: func() time.Time { return now },
	}
	writeTestFile(t, config.CACertificateFile, caPEM)
	writeTestFile(t, config.BootstrapCertificateFile, bootstrapCertificate)
	writeTestFile(t, config.BootstrapPrivateKeyFile, bootstrapKey)
	manager, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := manager.BeginRenewal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	csr := parseTestCSR(t, request.GetCsrPem())
	wrongURI, err := securetransport.EdgeIdentityURI("another-edge")
	if err != nil {
		t.Fatal(err)
	}
	wrongPEM, _, wrongLeaf := testIdentityCredential(t, ca, caKey, edgeID, now, now.Add(90*24*time.Hour), csr, wrongURI)
	digest := sha256.Sum256(wrongLeaf.Raw)
	if _, err := manager.Apply(context.Background(), &cloudv1.EdgeIdentityRenewResponse{
		RequestId: request.GetRequestId(), CertificatePem: wrongPEM, CertificateSha256: digest[:], NotAfter: timestamppb.New(wrongLeaf.NotAfter),
	}); err == nil {
		t.Fatal("accepted a certificate for another Edge identity")
	}
	if !bytes.Equal(manager.Current().SHA256, certificateDigest(bootstrapLeaf)) {
		t.Fatal("rejected renewal replaced the active credential")
	}
	if _, err := os.Stat(config.ManagedStateFile); !os.IsNotExist(err) {
		t.Fatalf("rejected renewal wrote managed state: %v", err)
	}
}

func TestManagerImmediatelyRotatesLegacyBootstrapWithoutEKU(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	edgeID := "legacy-edge-identity-test"
	directory := t.TempDir()
	ca, caKey, caPEM := testCA(t, now)
	bootstrapCertificate, bootstrapKey, _ := testIdentityCredentialWithUsages(t, ca, caKey, edgeID, now, now.Add(90*24*time.Hour), nil, nil)
	config := Config{
		EdgeID: edgeID, BootstrapCertificateFile: filepath.Join(directory, "identity-cert.pem"), BootstrapPrivateKeyFile: filepath.Join(directory, "identity-key.pem"),
		ManagedStateFile: filepath.Join(directory, "managed-identity.pem"), CACertificateFile: filepath.Join(directory, "edge-ca.pem"), Now: func() time.Time { return now },
	}
	writeTestFile(t, config.CACertificateFile, caPEM)
	writeTestFile(t, config.BootstrapCertificateFile, bootstrapCertificate)
	writeTestFile(t, config.BootstrapPrivateKeyFile, bootstrapKey)

	manager, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if delay := manager.RenewalDelay(now); delay != 0 {
		t.Fatalf("legacy bootstrap renewal delay = %v, want immediate renewal", delay)
	}
	request, err := manager.BeginRenewal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	csr := parseTestCSR(t, request.GetCsrPem())
	renewedPEM, _, renewedLeaf := testIdentityCredential(t, ca, caKey, edgeID, now, now.Add(90*24*time.Hour), csr)
	digest := sha256.Sum256(renewedLeaf.Raw)
	if _, err := manager.Apply(context.Background(), &cloudv1.EdgeIdentityRenewResponse{
		RequestId: request.GetRequestId(), CertificatePem: renewedPEM, CertificateSha256: digest[:], NotAfter: timestamppb.New(renewedLeaf.NotAfter),
	}); err != nil {
		t.Fatal(err)
	}
	if delay := manager.RenewalDelay(now); delay != 60*24*time.Hour {
		t.Fatalf("renewed credential delay = %v, want 60d", delay)
	}
	if _, err := New(config); err != nil {
		t.Fatalf("reload strictly validated managed credential: %v", err)
	}
}

func TestManagerRejectsLegacyCredentialFromManagedState(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	edgeID := "legacy-managed-identity-test"
	directory := t.TempDir()
	ca, caKey, caPEM := testCA(t, now)
	certificatePEM, keyPEM, _ := testIdentityCredentialWithUsages(t, ca, caKey, edgeID, now, now.Add(90*24*time.Hour), nil, nil)
	config := Config{
		EdgeID: edgeID, BootstrapCertificateFile: filepath.Join(directory, "identity-cert.pem"), BootstrapPrivateKeyFile: filepath.Join(directory, "identity-key.pem"),
		ManagedStateFile: filepath.Join(directory, "managed-identity.pem"), CACertificateFile: filepath.Join(directory, "edge-ca.pem"), Now: func() time.Time { return now },
	}
	writeTestFile(t, config.CACertificateFile, caPEM)
	writeTestFile(t, config.ManagedStateFile, append(certificatePEM, keyPEM...))
	if _, err := New(config); err == nil {
		t.Fatal("accepted a managed EdgeIdentity credential without clientAuth EKU")
	}
}

func testCA(t *testing.T, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Edge Identity Test CA"}, IsCA: true, BasicConstraintsValid: true,
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

func testIdentityCredential(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, edgeID string, notBefore, notAfter time.Time, csr *x509.CertificateRequest, overrideURI ...*url.URL) ([]byte, []byte, *x509.Certificate) {
	t.Helper()
	return testIdentityCredentialWithUsages(t, ca, caKey, edgeID, notBefore, notAfter, csr, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, overrideURI...)
}

func testIdentityCredentialWithUsages(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, edgeID string, notBefore, notAfter time.Time, csr *x509.CertificateRequest, usages []x509.ExtKeyUsage, overrideURI ...*url.URL) ([]byte, []byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := any(&key.PublicKey)
	if csr != nil {
		publicKey = csr.PublicKey
	}
	identityURI, err := securetransport.EdgeIdentityURI(edgeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrideURI) != 0 {
		identityURI = overrideURI[0]
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(notAfter.UnixNano()), Subject: pkix.Name{CommonName: edgeID}, NotBefore: notBefore.Add(-time.Minute), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usages, URIs: []*url.URL{identityURI},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, publicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), leaf
}

func parseTestCSR(t *testing.T, payload []byte) *x509.CertificateRequest {
	t.Helper()
	block, trailing := pem.Decode(payload)
	if block == nil || len(bytes.TrimSpace(trailing)) != 0 {
		t.Fatal("invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return csr
}

func certificateDigest(certificate *x509.Certificate) []byte {
	digest := sha256.Sum256(certificate.Raw)
	return digest[:]
}

func writeTestFile(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
