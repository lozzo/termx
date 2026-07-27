package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidatePairAndEndpoint(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.cn.omscd.com")
	metadata, err := ValidatePair(certificatePEM, privateKeyPEM, now)
	if err != nil {
		t.Fatalf("validate matching pair: %v", err)
	}
	profile := Profile{DNSNames: metadata.DNSNames}
	if err := VerifyEndpoint(profile, "edge.cn.omscd.com:443"); err != nil {
		t.Fatalf("verify covered endpoint: %v", err)
	}
	if err := VerifyEndpoint(profile, "other.omscd.com:443"); err == nil {
		t.Fatal("certificate was accepted for an uncovered Edge domain")
	}
	_, otherKey := testPair(t, now, "edge.cn.omscd.com")
	if _, err := ValidatePair(certificatePEM, otherKey, now); err == nil {
		t.Fatal("mismatched private key was accepted")
	}
	if _, err := ValidatePair(certificatePEM, privateKeyPEM, now.Add(48*time.Hour)); err == nil {
		t.Fatal("expired certificate was accepted")
	}
}

func TestFileSecretStorePermissionsAndRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM, privateKeyPEM := testPair(t, time.Now().UTC(), "edge.example.com")
	reference, err := store.Put(certificatePEM, privateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	assertMode(t, root, 0o700)
	assertMode(t, filepath.Join(root, reference), 0o700)
	assertMode(t, filepath.Join(root, reference, certificateFile), 0o600)
	assertMode(t, filepath.Join(root, reference, privateKeyFile), 0o600)
	gotCertificate, gotKey, err := store.Read(reference)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCertificate) != string(certificatePEM) || string(gotKey) != string(privateKeyPEM) {
		t.Fatal("secret round trip changed certificate material")
	}
	if _, _, err := store.Read("../escape"); err == nil {
		t.Fatal("path traversal secret reference was accepted")
	}
	if err := store.Delete(reference); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, reference)); !os.IsNotExist(err) {
		t.Fatalf("secret directory still exists after delete: %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode=%#o want=%#o", path, got, want)
	}
}

func testPair(t *testing.T, now time.Time, dnsName string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
