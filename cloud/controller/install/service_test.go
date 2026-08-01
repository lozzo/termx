package install

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestRegisterValidatesIPAddressCSRBeforeConsumingBootstrapClaim(t *testing.T) {
	now := time.Unix(30_000, 0).UTC()
	edge := edgeconfig.Edge{ID: "edge-ip-test", Name: "IP Edge", Region: "test", Capacity: 10, PublicEndpoint: "155.94.155.192:41102", Enabled: true, ConfigVersion: 1, Revision: 1}
	edgeStore := &registerEdgeStore{edge: edge}
	_, configSigningKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: edgeStore, SigningKey: configSigningKey, SigningKeyID: "config-test-key", ClaimTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, caKey := registerTestCA(t, now)
	service := &Service{
		edges: edges, caCertificate: caCertificate, caCertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate.Raw}), caKey: caKey,
		controllerCAPEM: []byte("controller-ca"), certificateValidity: time.Hour, now: func() time.Time { return now },
	}
	identityURI, err := securetransport.EdgeIdentityURI(edge.ID)
	if err != nil {
		t.Fatal(err)
	}
	identityCSR := registerTestCSR(t, pkix.Name{CommonName: edge.ID}, nil, nil, []*url.URL{identityURI})
	wrongPublicCSR := registerTestCSR(t, pkix.Name{CommonName: "155.94.155.192"}, []string{"155.94.155.192"}, nil, nil)
	request := &cloudv1.RegisterEdgeRequest{EdgeId: edge.ID, BootstrapToken: "same-bootstrap-token", IdentityCsrPem: identityCSR, PublicCsrPem: wrongPublicCSR}
	if _, err := service.Register(context.Background(), request); err == nil {
		t.Fatal("IP endpoint accepted a DNS SAN")
	}
	if edgeStore.consumeCalls != 0 || edgeStore.consumed {
		t.Fatalf("invalid public CSR consumed bootstrap claim: calls=%d consumed=%v", edgeStore.consumeCalls, edgeStore.consumed)
	}

	request.PublicCsrPem = registerTestCSR(t, pkix.Name{CommonName: "155.94.155.192"}, nil, []net.IP{net.ParseIP("155.94.155.192")}, nil)
	response, err := service.Register(context.Background(), request)
	if err != nil {
		t.Fatalf("retry IP registration: %v", err)
	}
	if edgeStore.consumeCalls != 1 || !edgeStore.consumed {
		t.Fatalf("valid retry did not consume bootstrap claim once: calls=%d consumed=%v", edgeStore.consumeCalls, edgeStore.consumed)
	}
	block, _ := pem.Decode(response.GetPublicCertificatePem())
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(certificate.IPAddresses) != 1 || !certificate.IPAddresses[0].Equal(net.ParseIP("155.94.155.192")) || len(certificate.DNSNames) != 0 {
		t.Fatalf("issued public SANs DNS=%v IP=%v", certificate.DNSNames, certificate.IPAddresses)
	}
}

func registerTestCSR(t *testing.T, subject pkix.Name, dnsNames []string, ipAddresses []net.IP, uris []*url.URL) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: subject, DNSNames: dnsNames, IPAddresses: ipAddresses, URIs: uris}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func registerTestCA(t *testing.T, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Install Test CA"}, IsCA: true, BasicConstraintsValid: true,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key
}

type registerEdgeStore struct {
	edge         edgeconfig.Edge
	consumeCalls int
	consumed     bool
}

func (store *registerEdgeStore) ListEdges(context.Context) ([]edgeconfig.Edge, error) {
	return []edgeconfig.Edge{store.edge}, nil
}
func (store *registerEdgeStore) GetEdge(_ context.Context, edgeID string) (edgeconfig.Edge, error) {
	if edgeID != store.edge.ID {
		return edgeconfig.Edge{}, errors.New("not found")
	}
	return store.edge, nil
}
func (*registerEdgeStore) CreateEdge(context.Context, edgeconfig.Edge, []byte, time.Time) error {
	return errors.New("unused")
}
func (*registerEdgeStore) UpdateEdge(context.Context, edgeconfig.UpdateInput, edgeconfig.Edge) error {
	return errors.New("unused")
}
func (store *registerEdgeStore) ConsumeInstallClaim(context.Context, []byte, []byte, time.Time) (edgeconfig.Edge, error) {
	return store.edge, errors.New("unused")
}
func (store *registerEdgeStore) ConsumeBootstrapClaim(_ context.Context, _ []byte, edgeID string, _ []byte) (edgeconfig.Edge, error) {
	store.consumeCalls++
	if store.consumed || edgeID != store.edge.ID {
		return edgeconfig.Edge{}, edgeconfig.ErrClaimInvalid
	}
	store.consumed = true
	return store.edge, nil
}
