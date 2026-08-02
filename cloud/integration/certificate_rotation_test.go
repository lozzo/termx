package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	controllercertificate "github.com/anytty/anytty/cloud/controller/certificate"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/postgres"
	controllerruntime "github.com/anytty/anytty/cloud/controller/runtime"
	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/securefs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestR8CertificateAutoUpdateAcrossOnlineAndReconnectWithPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate R8 schema: %v", err)
	}

	accounts, err := newIntegrationAccountService(account.Config{Store: database, AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, RecentAuthenticationTTL: 10 * time.Minute, SetupTTL: time.Hour, BcryptCost: 4})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := accounts.EnsureBootstrapOperator(ctx, "r8-"+uuid.NewString()+"@example.com", "r8-test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, configKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: database, SigningKey: configKey, SigningKeyID: "r8-config-test", ClaimTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	edge, _, _, err := edges.CreateEdge(ctx, edgeconfig.CreateInput{Name: "R8 Certificate Edge", Region: "test", Capacity: 10, PublicEndpoint: testEdgePublicServer + ":41102"})
	if err != nil {
		t.Fatal(err)
	}

	certificates := newCertificateFiles(t, edge.ID)
	directoryState, err := directory.New(directory.Config{MailboxSize: 1024, GracePeriod: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer directoryState.Close()
	var certificateService *controllercertificate.Service
	var dropNextCertificateApplied atomic.Bool
	controlService, err := control.NewService(control.Config{
		ControllerID: testControllerID, ControllerBootID: uuid.NewString(), HeartbeatInterval: 100 * time.Millisecond, HeartbeatTimeout: time.Second, Directory: directoryState,
		BindingKeyBundle: testBindingKeyBundleProvider(), EdgeEnabled: integrationEdgeEnabled,
		DaemonStateSnapshot: integrationDaemonStateSnapshot, ResolveDaemonState: integrationDaemonStateResolver,
		DesiredCertificate: func(ctx context.Context, edgeID string) (*cloudv1.EdgeCertificateBundle, error) {
			return certificateService.BundleForEdge(ctx, edgeID)
		},
		CertificateApplied: func(ctx context.Context, edgeID string, applied *cloudv1.CertificateApplied) error {
			if dropNextCertificateApplied.CompareAndSwap(true, false) {
				return nil
			}
			return certificateService.RecordApplied(ctx, edgeID, applied)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := securefs.SecureDirectory(temporary); err != nil {
		t.Fatal(err)
	}
	secretRoot := filepath.Join(temporary, "controller-certificates")
	secrets, err := controllercertificate.NewFileSecretStore(secretRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer secrets.Close()
	certificateService, err = controllercertificate.New(controllercertificate.Config{
		Store: database, Secrets: secrets, Edges: edges, Dispatcher: controlService,
		Online: func(ctx context.Context, edgeID string) (bool, error) {
			_, found, err := directoryState.Edge(ctx, edgeID)
			return found, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	controllerRuntime, err := controllerruntime.Start(controllerruntime.Config{
		GRPCListenAddress: "127.0.0.1:0", HealthListenAddress: "127.0.0.1:0",
		TLSCertificateFile: certificates.controllerCert, TLSPrivateKeyFile: certificates.controllerKey, EdgeCAFile: certificates.rootCA,
	}, controlService)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := controllerRuntime.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown R8 Controller: %v", err)
		}
	}()

	stateFile := filepath.Join(t.TempDir(), "edge", "managed-certificate.pb")
	startEdge := func(bootID string) *edgeruntime.Runtime {
		t.Helper()
		runtime, err := edgeruntime.Start(context.Background(), edgeruntime.Config{
			ListenAddress: "127.0.0.1:0", PublicCertificateFile: certificates.edgePublicCert, PublicPrivateKeyFile: certificates.edgePublicKey,
			ControllerAddress: controllerRuntime.GRPCAddress(), ControllerServerName: testControllerServer, ControllerCAFile: certificates.rootCA,
			IdentityCertificateFile: certificates.edgeIdentityCert, IdentityPrivateKeyFile: certificates.edgeIdentityKey,
			BindingKeyBundleCacheFile: testBindingKeyCacheFile(t),
			EdgeID:                    edge.ID, BootID: bootID, SoftwareVersion: "r8-integration", ManagedCertificateStateFile: stateFile,
		})
		if err != nil {
			t.Fatalf("start R8 Edge: %v", err)
		}
		readyCtx, readyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer readyCancel()
		if err := runtime.WaitReady(readyCtx); err != nil {
			shutdownEdge(t, runtime)
			t.Fatalf("wait R8 Edge ready: %v", err)
		}
		return runtime
	}

	edgeRuntime := startEdge("r8-edge-boot-1")
	initialFingerprint := peerCertificateFingerprint(t, edgeRuntime.PublicAddress(), certificates.rootPool)
	managedCA, managedCAKey := newCertificateAuthority(t)
	managedRoots := certificates.rootPool
	managedRoots.AddCert(managedCA)
	pair := func() ([]byte, []byte, string) {
		certificatePEM, privateKeyPEM := issueCertificate(t, managedCA, managedCAKey, certificateRequest{
			commonName: testEdgePublicServer, dnsNames: []string{testEdgePublicServer}, extendedUse: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		block, _ := pem.Decode(certificatePEM)
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(leaf.Raw)
		return certificatePEM, privateKeyPEM, strings.ToUpper(hex.EncodeToString(digest[:]))
	}

	certificate1, privateKey1, fingerprint1 := pair()
	upload1, err := certificateService.UploadProfile(ctx, &cloudv1.UploadCertificateProfileRequest{Name: "R8 Edge Certificate", CertificateChainPem: certificate1, PrivateKeyPem: privateKey1}, actor.GetAccountId())
	if err != nil {
		t.Fatal(err)
	}
	profileID := upload1.GetProfile().GetCertificateProfileId()
	if profileID == "" || upload1.GetProfile().GetRevision() != 1 || upload1.GetProfile().GetSha256Fingerprint() != fingerprint1 {
		t.Fatalf("unexpected uploaded profile: %+v", upload1.GetProfile())
	}
	staleProfile, err := database.GetCertificateProfile(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	staleEdge := edge
	dropNextCertificateApplied.Store(true)
	if _, err := certificateService.BindProfile(ctx, &cloudv1.BindCertificateProfileRequest{EdgeId: edge.ID, CertificateProfileId: profileID}, actor.GetAccountId()); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, func() bool {
		return !dropNextCertificateApplied.Load() && peerCertificateFingerprint(t, edgeRuntime.PublicAddress(), managedRoots) == fingerprint1
	})
	pending, err := certificateService.BindingForEdge(ctx, edge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.GetSyncState() != cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_PENDING || pending.GetAppliedRevision() != 0 {
		t.Fatalf("dropped applied receipt binding=%+v want pending revision 0", pending)
	}
	shutdownEdge(t, edgeRuntime)
	eventually(t, 2*time.Second, func() bool {
		_, found, lookupErr := directoryState.Edge(context.Background(), edge.ID)
		return lookupErr == nil && !found
	})
	edgeRuntime = startEdge("r8-edge-boot-applied-reconcile")
	waitCertificateApplied(t, certificateService, edge.ID, profileID, 1)
	if got := peerCertificateFingerprint(t, edgeRuntime.PublicAddress(), managedRoots); got != fingerprint1 || got == initialFingerprint {
		t.Fatalf("online TLS fingerprint=%s want new=%s initial=%s", got, fingerprint1, initialFingerprint)
	}
	assertFileMode(t, stateFile, 0o600)
	assertFileMode(t, secretRoot, 0o700)

	certificate2, privateKey2, fingerprint2 := pair()
	if _, err := certificateService.UploadProfile(ctx, &cloudv1.UploadCertificateProfileRequest{
		CertificateProfileId: profileID, ExpectedRevision: 1, Name: "R8 Edge Certificate", CertificateChainPem: certificate1, PrivateKeyPem: privateKey2,
	}, actor.GetAccountId()); err == nil {
		t.Fatal("mismatched replacement private key was accepted")
	}
	current, err := certificateService.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var currentProfileRevision uint64
	for _, profile := range current.GetProfiles() {
		if profile.GetCertificateProfileId() == profileID {
			currentProfileRevision = profile.GetRevision()
			break
		}
	}
	if currentProfileRevision != 1 || peerCertificateFingerprint(t, edgeRuntime.PublicAddress(), managedRoots) != fingerprint1 {
		t.Fatal("failed replacement changed the profile revision or active TLS certificate")
	}
	upload2, err := certificateService.UploadProfile(ctx, &cloudv1.UploadCertificateProfileRequest{
		CertificateProfileId: profileID, ExpectedRevision: 1, Name: "R8 Edge Certificate", CertificateChainPem: certificate2, PrivateKeyPem: privateKey2,
	}, actor.GetAccountId())
	if err != nil {
		t.Fatal(err)
	}
	if upload2.GetProfile().GetRevision() != 2 {
		t.Fatalf("replacement revision=%d want=2", upload2.GetProfile().GetRevision())
	}
	waitCertificateApplied(t, certificateService, edge.ID, profileID, 2)
	if got := peerCertificateFingerprint(t, edgeRuntime.PublicAddress(), managedRoots); got != fingerprint2 {
		t.Fatalf("second online TLS fingerprint=%s want=%s", got, fingerprint2)
	}

	shutdownEdge(t, edgeRuntime)
	eventually(t, 2*time.Second, func() bool {
		_, found, lookupErr := directoryState.Edge(context.Background(), edge.ID)
		return lookupErr == nil && !found
	})
	certificate3, privateKey3, fingerprint3 := pair()
	upload3, err := certificateService.UploadProfile(ctx, &cloudv1.UploadCertificateProfileRequest{
		CertificateProfileId: profileID, ExpectedRevision: 2, Name: "R8 Edge Certificate", CertificateChainPem: certificate3, PrivateKeyPem: privateKey3,
	}, actor.GetAccountId())
	if err != nil {
		t.Fatal(err)
	}
	if binding := upload3.GetProfile().GetBindings()[0]; binding.GetSyncState() != cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_PENDING || binding.GetOnline() {
		t.Fatalf("offline replacement binding=%+v want offline pending", binding)
	}
	edgeRuntime = startEdge("r8-edge-boot-2")
	defer shutdownEdge(t, edgeRuntime)
	waitCertificateApplied(t, certificateService, edge.ID, profileID, 3)
	if got := peerCertificateFingerprint(t, edgeRuntime.PublicAddress(), managedRoots); got != fingerprint3 {
		t.Fatalf("reconnected TLS fingerprint=%s want=%s", got, fingerprint3)
	}

	if _, err := database.BindCertificateProfile(ctx, edge, staleProfile, 1, actor.GetAccountId(), time.Now().UTC()); !errors.Is(err, controllercertificate.ErrRevisionConflict) {
		t.Fatalf("bind with stale certificate profile error=%v want revision conflict", err)
	}
	currentProfile, err := database.GetCertificateProfile(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	currentEdge, err := edges.GetEdge(ctx, edge.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedEdge, err := edges.UpdateEdge(ctx, edgeconfig.UpdateInput{
		EdgeID: currentEdge.ID, ExpectedRevision: currentEdge.Revision, Name: currentEdge.Name + " updated", Region: currentEdge.Region,
		Capacity: currentEdge.Capacity, PublicEndpoint: currentEdge.PublicEndpoint, Enabled: currentEdge.Enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.BindCertificateProfile(ctx, staleEdge, currentProfile, 1, actor.GetAccountId(), time.Now().UTC()); !errors.Is(err, controllercertificate.ErrRevisionConflict) {
		t.Fatalf("bind with stale Edge error=%v want revision conflict", err)
	}
	if _, err := edges.UpdateEdge(ctx, edgeconfig.UpdateInput{
		EdgeID: updatedEdge.ID, ExpectedRevision: updatedEdge.Revision, Name: updatedEdge.Name, Region: updatedEdge.Region,
		Capacity: updatedEdge.Capacity, PublicEndpoint: "uncovered.example.invalid:41102", Enabled: updatedEdge.Enabled,
	}); err == nil {
		t.Fatal("bound Edge accepted a public endpoint outside the current certificate DNS SAN")
	}
	persistedEdge, err := edges.GetEdge(ctx, edge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedEdge.PublicEndpoint != updatedEdge.PublicEndpoint || persistedEdge.Revision != updatedEdge.Revision {
		t.Fatalf("rejected endpoint update changed Edge: %+v", persistedEdge)
	}

	profiles, err := certificateService.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := protojson.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCertificateMaterial(t, "operator API projection", projection, certificate3, privateKey3)
	assertPostgresHasNoCertificateMaterial(t, databaseURL, profileID, certificate3, privateKey3)
}

func waitCertificateApplied(t *testing.T, service *controllercertificate.Service, edgeID, profileID string, revision uint64) {
	t.Helper()
	eventually(t, 5*time.Second, func() bool {
		binding, err := service.BindingForEdge(context.Background(), edgeID)
		return err == nil && binding.GetCertificateProfileId() == profileID && binding.GetDesiredRevision() == revision && binding.GetAppliedRevision() == revision && binding.GetSyncState() == cloudv1.CertificateSyncState_CERTIFICATE_SYNC_STATE_APPLIED
	})
}

func peerCertificateFingerprint(t *testing.T, address string, roots *x509.CertPool) string {
	t.Helper()
	connection, err := tls.Dial("tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, ServerName: testEdgePublicServer, RootCAs: roots})
	if err != nil {
		t.Fatalf("handshake with Edge public listener: %v", err)
	}
	defer connection.Close()
	certificates := connection.ConnectionState().PeerCertificates
	if len(certificates) == 0 {
		t.Fatal("Edge public listener did not provide a certificate")
	}
	digest := sha256.Sum256(certificates[0].Raw)
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode=%#o want=%#o", path, got, want)
	}
}

func assertPostgresHasNoCertificateMaterial(t *testing.T, databaseURL, profileID string, certificatePEM, privateKeyPEM []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	var profileJSON, bindingJSON string
	if err := connection.QueryRow(ctx, `SELECT to_jsonb(profile)::text FROM certificate_profiles profile WHERE certificate_profile_id=$1`, profileID).Scan(&profileJSON); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `SELECT to_jsonb(binding)::text FROM edge_certificate_bindings binding WHERE certificate_profile_id=$1`, profileID).Scan(&bindingJSON); err != nil {
		t.Fatal(err)
	}
	rows, err := connection.Query(ctx, `SELECT to_jsonb(audit)::text FROM operator_audit_events audit WHERE resource_id=$1 OR reason LIKE '%'||$1||'%'`, profileID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var databaseProjection strings.Builder
	databaseProjection.WriteString(profileJSON)
	databaseProjection.WriteString(bindingJSON)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		databaseProjection.WriteString(value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	assertNoCertificateMaterial(t, "PostgreSQL metadata and audit", []byte(databaseProjection.String()), certificatePEM, privateKeyPEM)
}

func assertNoCertificateMaterial(t *testing.T, owner string, projection, certificatePEM, privateKeyPEM []byte) {
	t.Helper()
	for name, material := range map[string][]byte{"certificate": certificatePEM, "private key": privateKeyPEM} {
		if bytes.Contains(projection, material) || bytes.Contains(projection, bytes.TrimSpace(material)) {
			t.Fatalf("%s contains %s PEM", owner, name)
		}
		if strings.Contains(string(projection), base64.StdEncoding.EncodeToString(material)) || strings.Contains(string(projection), base64.StdEncoding.EncodeToString(bytes.TrimSpace(material))) {
			t.Fatalf("%s contains Base64-encoded %s PEM", owner, name)
		}
	}
	for _, marker := range []string{"BEGIN CERTIFICATE", "BEGIN PRIVATE KEY", "BEGIN EC PRIVATE KEY", "BEGIN RSA PRIVATE KEY"} {
		if strings.Contains(string(projection), marker) {
			t.Fatalf("%s contains PEM marker %q", owner, marker)
		}
	}
	if strings.Contains(string(projection), fmt.Sprintf("%x", privateKeyPEM)) {
		t.Fatalf("%s contains hex-encoded private key", owner)
	}
}
