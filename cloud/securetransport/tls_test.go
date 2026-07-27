package securetransport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"
)

func TestValidateServerPairRejectsMalformedTrailingCertificate(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testServerPair(t, now, "edge.example.com")
	certificatePEM = append(certificatePEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("malformed trailing certificate")})...)
	if _, err := ValidateServerPair(certificatePEM, privateKeyPEM, "edge.example.com:443", now); err == nil {
		t.Fatal("server pair with malformed trailing certificate was accepted")
	}
}

func TestEdgeIDFromCertificateRequiresCanonicalIdentityURI(t *testing.T) {
	identity, err := EdgeIdentityURI("edge-1")
	if err != nil {
		t.Fatalf("create Edge identity URI: %v", err)
	}
	edgeID, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{identity}})
	if err != nil {
		t.Fatalf("parse canonical Edge identity URI: %v", err)
	}
	if edgeID != "edge-1" {
		t.Fatalf("Edge ID = %q, want edge-1", edgeID)
	}

	queryIdentity := *identity
	queryIdentity.RawQuery = "alternate=true"
	if _, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{&queryIdentity}}); err == nil {
		t.Fatal("Edge identity URI with query was accepted")
	}
	userIdentity := *identity
	userIdentity.User = url.User("unexpected")
	if _, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{&userIdentity}}); err == nil {
		t.Fatal("Edge identity URI with userinfo was accepted")
	}
}

func TestEdgeIDFromCertificateRejectsMissingAndDuplicateIdentity(t *testing.T) {
	if _, err := EdgeIDFromCertificate(&x509.Certificate{}); err == nil {
		t.Fatal("certificate without Edge identity URI was accepted")
	}
	identity, err := EdgeIdentityURI("edge-1")
	if err != nil {
		t.Fatalf("create Edge identity URI: %v", err)
	}
	if _, err := EdgeIDFromCertificate(&x509.Certificate{URIs: []*url.URL{identity, identity}}); err == nil {
		t.Fatal("certificate with duplicate Edge identity URIs was accepted")
	}
}

func testServerPair(t *testing.T, now time.Time, dnsName string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
}
