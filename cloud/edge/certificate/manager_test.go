package certificate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/securetransport"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

func TestManagerHotReloadAndFailureKeepsCurrentCertificate(t *testing.T) {
	now := time.Now().UTC()
	oldCertificate, oldKey, oldLeaf := managedTestPair(t, now, "edge.example.com", 1)
	newCertificate, newKey, newLeaf := managedTestPair(t, now, "edge.example.com", 2)
	wrongCertificate, wrongKey, _ := managedTestPair(t, now, "wrong.example.com", 3)
	directory := t.TempDir()
	certificateFile, keyFile := filepath.Join(directory, "initial-cert.pem"), filepath.Join(directory, "initial-key.pem")
	if err := os.WriteFile(certificateFile, oldCertificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	tlsConfig, loader, err := securetransport.NewReloadableServerTLSConfig(securetransport.ServerOptions{CertificateFile: certificateFile, PrivateKeyFile: keyFile})
	if err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(directory, "managed-certificate.pb")
	manager, err := New(Config{EdgeID: "edge-1", StateFile: stateFile, Now: func() time.Time { return now }}, loader)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(oldLeaf)
	pool.AddCert(newLeaf)
	if serial := handshakeSerial(t, tlsConfig, pool); serial != 1 {
		t.Fatalf("initial handshake serial=%d want=1", serial)
	}
	bundle := &cloudv1.EdgeCertificateBundle{TargetEdgeId: "edge-1", CertificateProfileId: "profile-1", Revision: 2, PublicEndpoint: "edge.example.com:443", CertificateChainPem: newCertificate, PrivateKeyPem: newKey}
	if err := manager.Apply(context.Background(), bundle); err != nil {
		t.Fatal(err)
	}
	if profileID, revision := manager.Current(); profileID != "profile-1" || revision != 2 {
		t.Fatalf("current=%s/%d want profile-1/2", profileID, revision)
	}
	if serial := handshakeSerial(t, tlsConfig, pool); serial != 2 {
		t.Fatalf("reloaded handshake serial=%d want=2", serial)
	}
	stale := &cloudv1.EdgeCertificateBundle{TargetEdgeId: "edge-1", CertificateProfileId: "profile-1", Revision: 1, PublicEndpoint: "edge.example.com:443", CertificateChainPem: oldCertificate, PrivateKeyPem: oldKey}
	if err := manager.Apply(context.Background(), stale); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale certificate revision error=%v want %v", err, ErrStaleRevision)
	}
	if profileID, revision := manager.Current(); profileID != "profile-1" || revision != 2 {
		t.Fatalf("stale apply changed current=%s/%d", profileID, revision)
	}
	if serial := handshakeSerial(t, tlsConfig, pool); serial != 2 {
		t.Fatalf("stale apply changed handshake serial=%d want=2", serial)
	}
	info, err := os.Stat(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("managed state mode=%#o want=0600", info.Mode().Perm())
	}
	bad := &cloudv1.EdgeCertificateBundle{TargetEdgeId: "edge-1", CertificateProfileId: "profile-1", Revision: 3, PublicEndpoint: "edge.example.com:443", CertificateChainPem: wrongCertificate, PrivateKeyPem: wrongKey}
	if err := manager.Apply(context.Background(), bad); err == nil {
		t.Fatal("certificate for wrong DNS name was applied")
	}
	if profileID, revision := manager.Current(); profileID != "profile-1" || revision != 2 {
		t.Fatalf("failed apply changed current=%s/%d", profileID, revision)
	}
	if serial := handshakeSerial(t, tlsConfig, pool); serial != 2 {
		t.Fatalf("failed apply changed handshake serial=%d", serial)
	}
}

func handshakeSerial(t *testing.T, serverConfig *tls.Config, roots *x509.CertPool) int64 {
	t.Helper()
	serverConnection, clientConnection := net.Pipe()
	server := tls.Server(serverConnection, serverConfig.Clone())
	client := tls.Client(clientConnection, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "edge.example.com"})
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- server.Handshake()
	}()
	if err := client.Handshake(); err != nil {
		t.Fatal(err)
	}
	serial := client.ConnectionState().PeerCertificates[0].SerialNumber.Int64()
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
	_ = clientConnection.Close()
	_ = serverConnection.Close()
	return serial
}

func managedTestPair(t *testing.T, now time.Time, dnsName string, serial int64) ([]byte, []byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
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
