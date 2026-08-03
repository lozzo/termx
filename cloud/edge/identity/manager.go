// Package identity owns the EdgeIdentity client certificate, private-key
// rotation, and the atomic credential used by future EdgeControl handshakes.
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
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/securetransport"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const DefaultRenewBefore = 30 * 24 * time.Hour

type Config struct {
	EdgeID                   string
	BootstrapCertificateFile string
	BootstrapPrivateKeyFile  string
	ManagedStateFile         string
	CACertificateFile        string
	RenewBefore              time.Duration
	Now                      func() time.Time
}

type Metadata struct {
	SHA256   []byte
	NotAfter time.Time
}

type pendingRenewal struct {
	requestID  string
	privateKey *ecdsa.PrivateKey
}

// Manager keeps the currently usable key pair in memory. A managed state file
// contains the certificate and private key in one atomic payload, so a crash
// cannot publish a certificate from one generation with another generation's key.
type Manager struct {
	config          Config
	roots           *x509.CertPool
	current         atomic.Pointer[tls.Certificate]
	mu              sync.Mutex
	leaf            *x509.Certificate
	pending         *pendingRenewal
	legacyBootstrap bool
}

func New(config Config) (*Manager, error) {
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.BootstrapCertificateFile = filepath.Clean(strings.TrimSpace(config.BootstrapCertificateFile))
	config.BootstrapPrivateKeyFile = filepath.Clean(strings.TrimSpace(config.BootstrapPrivateKeyFile))
	config.ManagedStateFile = filepath.Clean(strings.TrimSpace(config.ManagedStateFile))
	config.CACertificateFile = filepath.Clean(strings.TrimSpace(config.CACertificateFile))
	if config.RenewBefore <= 0 {
		config.RenewBefore = DefaultRenewBefore
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.EdgeID == "" || config.BootstrapCertificateFile == "." || config.BootstrapPrivateKeyFile == "." || config.ManagedStateFile == "." || config.CACertificateFile == "." {
		return nil, errors.New("Edge identity, bootstrap credential, managed state, and CA are required")
	}
	roots, err := loadRoots(config.CACertificateFile)
	if err != nil {
		return nil, err
	}
	manager := &Manager{config: config, roots: roots}
	pair, leaf, legacyBootstrap, err := manager.loadCurrent(config.Now().UTC())
	if err != nil {
		return nil, err
	}
	manager.current.Store(pair)
	manager.leaf = leaf
	manager.legacyBootstrap = legacyBootstrap
	return manager, nil
}

// PersistRecoveredCredential validates a recovery response against the local
// Edge CA and identity before atomically publishing its certificate and key.
func PersistRecoveredCredential(config Config, certificatePEM, privateKeyPEM []byte, expected Metadata) (Metadata, error) {
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.ManagedStateFile = filepath.Clean(strings.TrimSpace(config.ManagedStateFile))
	config.CACertificateFile = filepath.Clean(strings.TrimSpace(config.CACertificateFile))
	if config.RenewBefore <= 0 {
		config.RenewBefore = DefaultRenewBefore
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.EdgeID == "" || config.ManagedStateFile == "." || config.CACertificateFile == "." || len(expected.SHA256) != sha256.Size || expected.NotAfter.IsZero() {
		return Metadata{}, errors.New("recovered EdgeIdentity credential metadata is incomplete")
	}
	roots, err := loadRoots(config.CACertificateFile)
	if err != nil {
		return Metadata{}, err
	}
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return Metadata{}, fmt.Errorf("load recovered EdgeIdentity credential: %w", err)
	}
	manager := &Manager{config: config, roots: roots}
	now := config.Now().UTC()
	leaf, err := manager.validate(&pair, now, false)
	if err != nil {
		return Metadata{}, fmt.Errorf("validate recovered EdgeIdentity credential: %w", err)
	}
	value := metadata(leaf)
	if !bytes.Equal(value.SHA256, expected.SHA256) || !value.NotAfter.Equal(expected.NotAfter.UTC()) {
		return Metadata{}, errors.New("recovered EdgeIdentity metadata does not match the certificate")
	}
	if !value.NotAfter.After(now.Add(config.RenewBefore)) {
		return Metadata{}, errors.New("recovered EdgeIdentity certificate does not extend beyond the renewal window")
	}
	state := append(append([]byte(nil), certificatePEM...), privateKeyPEM...)
	if err := atomicWrite(config.ManagedStateFile, state, 0o600); err != nil {
		return Metadata{}, fmt.Errorf("persist recovered EdgeIdentity credential: %w", err)
	}
	return value, nil
}

func loadRoots(path string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read EdgeIdentity CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("EdgeIdentity CA certificate is invalid")
	}
	return roots, nil
}

func (manager *Manager) loadCurrent(now time.Time) (*tls.Certificate, *x509.Certificate, bool, error) {
	managed, err := os.ReadFile(manager.config.ManagedStateFile)
	if err == nil {
		pair, pairErr := tls.X509KeyPair(managed, managed)
		if pairErr != nil {
			return nil, nil, false, fmt.Errorf("load managed EdgeIdentity credential: %w", pairErr)
		}
		leaf, validateErr := manager.validate(&pair, now, false)
		if validateErr != nil {
			return nil, nil, false, fmt.Errorf("validate managed EdgeIdentity credential: %w", validateErr)
		}
		return &pair, leaf, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, fmt.Errorf("read managed EdgeIdentity credential: %w", err)
	}
	pair, err := tls.LoadX509KeyPair(manager.config.BootstrapCertificateFile, manager.config.BootstrapPrivateKeyFile)
	if err != nil {
		return nil, nil, false, fmt.Errorf("load bootstrap EdgeIdentity credential: %w", err)
	}
	leaf, err := manager.validate(&pair, now, true)
	if err != nil {
		return nil, nil, false, fmt.Errorf("validate bootstrap EdgeIdentity credential: %w", err)
	}
	return &pair, leaf, len(leaf.ExtKeyUsage) == 0, nil
}

// GetClientCertificate supplies the current credential to future TLS handshakes.
// Existing EdgeControl connections retain the certificate used by their handshake.
func (manager *Manager) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	if manager == nil {
		return nil, errors.New("EdgeIdentity manager is unavailable")
	}
	pair := manager.current.Load()
	if pair == nil {
		return nil, errors.New("EdgeIdentity credential is unavailable")
	}
	return pair, nil
}

func (manager *Manager) Current() Metadata {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return metadata(manager.leaf)
}

func (manager *Manager) RenewalDelay(now time.Time) time.Duration {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.legacyBootstrap {
		return 0
	}
	if now.IsZero() {
		now = manager.config.Now().UTC()
	}
	deadline := manager.leaf.NotAfter.Add(-manager.config.RenewBefore)
	if !now.Before(deadline) {
		return 0
	}
	return deadline.Sub(now)
}

// BeginRenewal rotates the private key before creating the CSR. Only one
// request may be pending, which bounds private material retained in memory.
func (manager *Manager) BeginRenewal(ctx context.Context) (*cloudv1.EdgeIdentityRenewRequest, error) {
	if manager == nil {
		return nil, errors.New("EdgeIdentity manager is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.pending != nil {
		return nil, errors.New("EdgeIdentity renewal is already pending")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate EdgeIdentity renewal key: %w", err)
	}
	identityURI, err := securetransport.EdgeIdentityURI(manager.config.EdgeID)
	if err != nil {
		return nil, err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: manager.config.EdgeID}, URIs: []*url.URL{identityURI},
	}, key)
	if err != nil {
		return nil, fmt.Errorf("create EdgeIdentity renewal CSR: %w", err)
	}
	requestID := uuid.NewString()
	manager.pending = &pendingRenewal{requestID: requestID, privateKey: key}
	current := metadata(manager.leaf)
	now := manager.config.Now().UTC()
	return &cloudv1.EdgeIdentityRenewRequest{
		RequestId: requestID, CsrPem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
		CurrentCertificateSha256: current.SHA256, RequestedAt: timestamppb.New(now),
	}, nil
}

func (manager *Manager) CancelRenewal(requestID string) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	if manager.pending != nil && manager.pending.requestID == strings.TrimSpace(requestID) {
		manager.pending = nil
	}
	manager.mu.Unlock()
}

func (manager *Manager) Apply(ctx context.Context, response *cloudv1.EdgeIdentityRenewResponse) (Metadata, error) {
	if manager == nil || response == nil {
		return Metadata{}, errors.New("EdgeIdentity renewal response is required")
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.pending == nil || strings.TrimSpace(response.GetRequestId()) != manager.pending.requestID {
		return Metadata{}, errors.New("EdgeIdentity renewal response does not match the pending request")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(manager.pending.privateKey)
	if err != nil {
		return Metadata{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(response.GetCertificatePem(), keyPEM)
	if err != nil {
		return Metadata{}, fmt.Errorf("load renewed EdgeIdentity credential: %w", err)
	}
	now := manager.config.Now().UTC()
	leaf, err := manager.validate(&pair, now, false)
	if err != nil {
		return Metadata{}, fmt.Errorf("validate renewed EdgeIdentity credential: %w", err)
	}
	value := metadata(leaf)
	if response.GetNotAfter() == nil || response.GetNotAfter().CheckValid() != nil || !response.GetNotAfter().AsTime().Equal(value.NotAfter) || !bytes.Equal(response.GetCertificateSha256(), value.SHA256) {
		return Metadata{}, errors.New("renewed EdgeIdentity metadata does not match the certificate")
	}
	if !value.NotAfter.After(now.Add(manager.config.RenewBefore)) {
		return Metadata{}, errors.New("renewed EdgeIdentity certificate does not extend beyond the renewal window")
	}
	state := append(append([]byte(nil), response.GetCertificatePem()...), keyPEM...)
	if err := atomicWrite(manager.config.ManagedStateFile, state, 0o600); err != nil {
		return Metadata{}, fmt.Errorf("persist renewed EdgeIdentity credential: %w", err)
	}
	manager.current.Store(&pair)
	manager.leaf = leaf
	manager.legacyBootstrap = false
	manager.pending = nil
	return value, nil
}

func (manager *Manager) validate(pair *tls.Certificate, now time.Time, allowLegacyBootstrap bool) (*x509.Certificate, error) {
	if pair == nil || len(pair.Certificate) == 0 {
		return nil, errors.New("EdgeIdentity certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	intermediates := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		certificate, parseErr := x509.ParseCertificate(raw)
		if parseErr != nil {
			return nil, parseErr
		}
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: manager.roots, Intermediates: intermediates, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, err
	}
	publicKey, p256 := leaf.PublicKey.(*ecdsa.PublicKey)
	clientAuthOnly := len(leaf.ExtKeyUsage) == 1 && leaf.ExtKeyUsage[0] == x509.ExtKeyUsageClientAuth
	legacyPurpose := allowLegacyBootstrap && len(leaf.ExtKeyUsage) == 0
	if !p256 || publicKey.Curve != elliptic.P256() || leaf.IsCA || len(leaf.UnknownExtKeyUsage) != 0 || (!clientAuthOnly && !legacyPurpose) {
		return nil, errors.New("EdgeIdentity certificate purpose or public key is invalid")
	}
	edgeID, err := securetransport.EdgeIDFromCertificate(leaf)
	if err != nil || edgeID != manager.config.EdgeID || len(leaf.URIs) != 1 || len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 0 || len(leaf.EmailAddresses) != 0 {
		return nil, errors.New("EdgeIdentity certificate does not contain the exact expected URI identity")
	}
	pair.Leaf = leaf
	return leaf, nil
}

func metadata(leaf *x509.Certificate) Metadata {
	if leaf == nil {
		return Metadata{}
	}
	digest := sha256.Sum256(leaf.Raw)
	return Metadata{SHA256: append([]byte(nil), digest[:]...), NotAfter: leaf.NotAfter.UTC()}
}

func atomicWrite(path string, payload []byte, mode os.FileMode) error {
	if len(payload) == 0 {
		return errors.New("refuse to persist an empty EdgeIdentity credential")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".managed-identity-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}
