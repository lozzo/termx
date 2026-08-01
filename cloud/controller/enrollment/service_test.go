package enrollment

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/runtimesnapshot"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCompleteDaemonEnrollmentPrecheckFailureLeavesTokenRetryable(t *testing.T) {
	tests := []struct {
		name         string
		entitled     bool
		edgeOnline   bool
		expectedCode codes.Code
	}{
		{name: "entitlement unavailable", entitled: false, edgeOnline: true, expectedCode: codes.PermissionDenied},
		{name: "edge offline", entitled: true, edgeOnline: false, expectedCode: codes.Unavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEnrollmentPreflightFixture(t, test.entitled, test.edgeOnline)
			if _, err := fixture.complete(); status.Code(err) != test.expectedCode {
				t.Fatalf("precheck code=%v err=%v", status.Code(err), err)
			}
			if fixture.store.consumeCalls != 0 || fixture.store.consumed {
				t.Fatalf("failed precheck consumed enrollment: calls=%d consumed=%v", fixture.store.consumeCalls, fixture.store.consumed)
			}

			fixture.entitlement.active = true
			fixture.makeEdgeOnline()
			completed, err := fixture.complete()
			if err != nil {
				t.Fatalf("retry enrollment: %v", err)
			}
			if fixture.store.consumeCalls != 1 || !fixture.store.consumed || completed.GetDaemon().GetDaemonId() != fixture.store.daemon.ID {
				t.Fatalf("retry result=%+v calls=%d consumed=%v", completed, fixture.store.consumeCalls, fixture.store.consumed)
			}
		})
	}
}

type enrollmentPreflightFixture struct {
	service        *Service
	store          *preflightEnrollmentStore
	entitlement    *preflightEntitlement
	identity       remoteauth.Identity
	code           string
	makeEdgeOnline func()
}

func newEnrollmentPreflightFixture(t *testing.T, entitled, edgeOnline bool) enrollmentPreflightFixture {
	t.Helper()
	now := time.Unix(20_000, 0).UTC()
	code := "mxe_preflight_retry"
	digest := sha256.Sum256([]byte(code))
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("preflight-device", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	accountID := uuid.NewString()
	store := &preflightEnrollmentStore{
		digest: digest[:], accountID: accountID,
		daemon: Daemon{ID: uuid.NewString(), AccountID: accountID, AccountName: "Test account", DisplayName: "Test daemon", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1, CreatedAt: now, UpdatedAt: now},
	}
	edge := edgeconfig.Edge{ID: uuid.NewString(), Name: "Test Edge", Region: "test", Capacity: 10, PublicEndpoint: "edge.test.example:41102", Enabled: true, ConfigVersion: 1, Revision: 1}
	_, edgeSigningKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: preflightEdgeStore{edge: edge}, SigningKey: edgeSigningKey, SigningKeyID: "edge-test-key", ClaimTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	runtimeDirectory, err := directory.New(directory.Config{MailboxSize: 16, GracePeriod: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimeDirectory.Close)
	edgePublished := false
	makeEdgeOnline := func() {
		if edgePublished {
			return
		}
		edgePublished = true
		publishEnrollmentTestEdge(t, runtimeDirectory, edge.ID)
	}
	if edgeOnline {
		makeEdgeOnline()
	}
	entitlement := &preflightEntitlement{active: entitled}
	_, bindingSigningKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		Store: store, Edges: edges, Directory: runtimeDirectory, Entitlement: entitlement,
		BindingSigningKey: bindingSigningKey, BindingSigningKeyID: "binding-test-key", EdgeCACertificate: []byte("test-ca"),
		EnrollmentTTL: time.Minute, ChallengeTTL: time.Minute, BindingTTL: time.Hour, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return enrollmentPreflightFixture{service: service, store: store, entitlement: entitlement, identity: identity, code: code, makeEdgeOnline: makeEdgeOnline}
}

func (fixture enrollmentPreflightFixture) complete() (*cloudv1.CompleteDaemonEnrollmentResponse, error) {
	challenge, err := fixture.service.BeginDaemonEnrollment(context.Background(), &cloudv1.BeginDaemonEnrollmentRequest{
		EnrollmentCode: fixture.code, DeviceId: fixture.identity.DeviceID, DeviceFingerprint: fixture.identity.Fingerprint, DevicePublicKey: fixture.identity.PublicKey,
	})
	if err != nil {
		return nil, err
	}
	proof, err := remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
	if err != nil {
		return nil, err
	}
	return fixture.service.CompleteDaemonEnrollment(context.Background(), &cloudv1.CompleteDaemonEnrollmentRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof})
}

func publishEnrollmentTestEdge(t *testing.T, runtimeDirectory *directory.Directory, edgeID string) {
	t.Helper()
	ctx := context.Background()
	connectionID := "connection-" + edgeID
	if err := runtimeDirectory.Attach(ctx, directory.Attachment{EdgeID: edgeID, BootID: "boot-test", ConnectionID: connectionID, SoftwareVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	snapshot := &cloudv1.RuntimeSnapshot{Revision: 1}
	digest, err := runtimesnapshot.Digest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeDirectory.BeginSnapshot(ctx, connectionID, &cloudv1.SnapshotBegin{SnapshotId: "snapshot-test", Revision: snapshot.GetRevision()}); err != nil {
		t.Fatal(err)
	}
	if err := runtimeDirectory.CommitSnapshot(ctx, connectionID, &cloudv1.SnapshotEnd{SnapshotId: "snapshot-test", Revision: snapshot.GetRevision(), Digest: digest}); err != nil {
		t.Fatal(err)
	}
}

type preflightEnrollmentStore struct {
	digest       []byte
	accountID    string
	daemon       Daemon
	consumeCalls int
	consumed     bool
}

func (*preflightEnrollmentStore) CreateDaemonEnrollment(context.Context, string, string, string, []byte, time.Time, time.Time) (string, error) {
	return "", errors.New("unused")
}
func (store *preflightEnrollmentStore) GetDaemonEnrollmentAccount(_ context.Context, digest []byte, _ time.Time) (string, error) {
	if store.consumed || !bytes.Equal(store.digest, digest) {
		return "", ErrEnrollmentInvalid
	}
	return store.accountID, nil
}
func (store *preflightEnrollmentStore) ConsumeDaemonEnrollment(_ context.Context, digest []byte, deviceID, fingerprint string, publicKey ed25519.PublicKey, _ time.Time) (Daemon, error) {
	store.consumeCalls++
	if store.consumed || !bytes.Equal(store.digest, digest) {
		return Daemon{}, ErrEnrollmentInvalid
	}
	store.consumed = true
	store.daemon.DeviceID = deviceID
	store.daemon.DeviceFingerprint = fingerprint
	store.daemon.DevicePublicKey = append(ed25519.PublicKey(nil), publicKey...)
	return store.daemon, nil
}
func (store *preflightEnrollmentStore) GetDaemon(_ context.Context, daemonID string) (Daemon, error) {
	if daemonID != store.daemon.ID {
		return Daemon{}, ErrDaemonUnavailable
	}
	return store.daemon, nil
}
func (store *preflightEnrollmentStore) ListDaemons(context.Context) ([]Daemon, error) {
	return []Daemon{store.daemon}, nil
}

type preflightEntitlement struct{ active bool }

func (entitlement *preflightEntitlement) EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error) {
	state := cloudv1.EntitlementState_ENTITLEMENT_STATE_SUSPENDED
	if entitlement.active {
		state = cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE
	}
	return &cloudv1.EffectiveEntitlement{State: state, Capability: &cloudv1.CloudCapability{ManagedP2PEnabled: true}}, nil
}

type preflightEdgeStore struct{ edge edgeconfig.Edge }

func (store preflightEdgeStore) ListEdges(context.Context) ([]edgeconfig.Edge, error) {
	return []edgeconfig.Edge{store.edge}, nil
}
func (store preflightEdgeStore) GetEdge(_ context.Context, edgeID string) (edgeconfig.Edge, error) {
	if edgeID != store.edge.ID {
		return edgeconfig.Edge{}, errors.New("not found")
	}
	return store.edge, nil
}
func (preflightEdgeStore) CreateEdge(context.Context, edgeconfig.Edge, []byte, time.Time) error {
	return errors.New("unused")
}
func (preflightEdgeStore) UpdateEdge(context.Context, edgeconfig.UpdateInput, edgeconfig.Edge) error {
	return errors.New("unused")
}
func (store preflightEdgeStore) ConsumeInstallClaim(context.Context, []byte, []byte, time.Time) (edgeconfig.Edge, error) {
	return store.edge, errors.New("unused")
}
func (store preflightEdgeStore) ConsumeBootstrapClaim(context.Context, []byte, string, []byte) (edgeconfig.Edge, error) {
	return store.edge, errors.New("unused")
}
