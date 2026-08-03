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
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestCompleteDaemonBindingRefreshReturnsBindingForOnlineEdge(t *testing.T) {
	fixture := newBindingRefreshFixture(t)
	response, err := fixture.refresh(fixture.identity)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDaemon().GetDaemonId() != fixture.store.daemon.ID || response.GetDaemon().GetState() != cloudv1.DaemonState_DAEMON_STATE_ACTIVE {
		t.Fatalf("refreshed daemon = %+v", response.GetDaemon())
	}
	if response.GetEdgeLocator().GetEdgeId() != fixture.edge.ID || response.GetEdgeLocator().GetPublicEndpoint() != fixture.edge.PublicEndpoint {
		t.Fatalf("selected Edge = %+v want=%+v", response.GetEdgeLocator(), fixture.edge)
	}
	claims, err := ticket.VerifyDaemonBinding(response.GetDaemonBinding(), ticket.KeySet{fixture.bindingKeyID: fixture.bindingPublicKey}, fixture.edge.ID, fixture.now, 0)
	if err != nil {
		t.Fatalf("verify refreshed binding: %v", err)
	}
	locatorPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(response.GetEdgeLocator())
	if err != nil {
		t.Fatal(err)
	}
	locatorDigest := sha256.Sum256(locatorPayload)
	if claims.GetDaemonId() != fixture.store.daemon.ID || claims.GetAccountId() != fixture.store.daemon.AccountID ||
		claims.GetDeviceId() != fixture.identity.DeviceID || !bytes.Equal(claims.GetDevicePublicKey(), fixture.identity.PublicKey) ||
		!bytes.Equal(claims.GetEdgeLocatorSha256(), locatorDigest[:]) {
		t.Fatalf("refreshed binding claims = %+v", claims)
	}
}

func TestCompleteDaemonBindingRefreshPersistsPreferenceAndMeasurements(t *testing.T) {
	fixture := newBindingRefreshFixture(t)
	challenge := fixture.beginRefresh(t)
	proof, err := remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	response, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), &cloudv1.CompleteDaemonBindingRefreshRequest{
		ChallengeId: challenge.GetChallengeId(), DeviceProof: proof, ChangePreference: true,
		PreferredEdgeId: fixture.edge.ID, ExpectedPreferenceRevision: 1,
		EdgeMeasurements: []*cloudv1.DaemonEdgeMeasurement{{EdgeId: fixture.edge.ID, Reachable: true, ConnectLatencyMs: 23, ConnectionFailureRate: 0, SampleCount: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDaemon().GetPreferredEdgeId() != fixture.edge.ID || response.GetDaemon().GetEdgePreferenceRevision() != 2 {
		t.Fatalf("preference response = %+v", response.GetDaemon())
	}
	candidates := response.GetEdgeSelection().GetCandidates()
	if len(candidates) != 1 || !candidates[0].GetPreferred() || candidates[0].GetMeasurement().GetConnectLatencyMs() != 23 {
		t.Fatalf("Edge selection = %+v", response.GetEdgeSelection())
	}

	challenge = fixture.beginRefresh(t)
	proof, err = remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.CompleteDaemonBindingRefresh(context.Background(), &cloudv1.CompleteDaemonBindingRefreshRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof, ChangePreference: true, PreferredEdgeId: fixture.edge.ID, ExpectedPreferenceRevision: 1})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale preference revision code=%v err=%v", status.Code(err), err)
	}
}

func TestCompleteDaemonBindingRefreshFallsBackFromUnreachablePreference(t *testing.T) {
	fixture := newBindingRefreshFixture(t)
	fallback := edgeconfig.Edge{ID: uuid.NewString(), Name: "Fallback Edge", Region: "fallback", Capacity: 10, PublicEndpoint: "fallback.test.example:41102", Enabled: true, ConfigVersion: 1, Revision: 1}
	fixture.edgeStore.edges = append(fixture.edgeStore.edges, fallback)
	publishEnrollmentTestEdge(t, fixture.directory, fallback.ID)
	fixture.store.daemon.PreferredEdgeID = fixture.edge.ID

	challenge := fixture.beginRefresh(t)
	proof, err := remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	response, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), &cloudv1.CompleteDaemonBindingRefreshRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof, EdgeMeasurements: []*cloudv1.DaemonEdgeMeasurement{
		{EdgeId: fixture.edge.ID, Reachable: false, ConnectionFailureRate: 1, SampleCount: 3},
		{EdgeId: fallback.ID, Reachable: true, ConnectLatencyMs: 80, SampleCount: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetEdgeLocator().GetEdgeId() != fallback.ID || response.GetEdgeSelection().GetPreferredEdgeId() != fixture.edge.ID {
		t.Fatalf("fallback selection = %+v", response.GetEdgeSelection())
	}
}

func TestCompleteDaemonBindingRefreshRejectsWrongProofAndReplay(t *testing.T) {
	fixture := newBindingRefreshFixture(t)
	challenge := fixture.beginRefresh(t)
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherIdentity, err := remoteauth.NewIdentity("other-device", otherPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	wrongProof, err := remoteauth.SignDeviceIdentityProof(otherIdentity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	request := &cloudv1.CompleteDaemonBindingRefreshRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: wrongProof}
	if _, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong proof code=%v err=%v", status.Code(err), err)
	}
	validProof, err := remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	request.DeviceProof = validProof
	if _, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("burned challenge reuse code=%v err=%v", status.Code(err), err)
	}

	challenge = fixture.beginRefresh(t)
	validProof, err = remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	request = &cloudv1.CompleteDaemonBindingRefreshRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: validProof}
	if _, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), request); err != nil {
		t.Fatalf("complete fresh challenge: %v", err)
	}
	if _, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("completed challenge replay code=%v err=%v", status.Code(err), err)
	}
}

func TestCompleteDaemonBindingRefreshUsesLatestLifecycleState(t *testing.T) {
	tests := []struct {
		name         string
		state        cloudv1.DaemonState
		wantMaterial bool
	}{
		{name: "blocked keeps control route", state: cloudv1.DaemonState_DAEMON_STATE_BLOCKED, wantMaterial: true},
		{name: "deleted is terminal", state: cloudv1.DaemonState_DAEMON_STATE_DELETED, wantMaterial: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBindingRefreshFixture(t)
			challenge := fixture.beginRefresh(t)
			fixture.store.daemon.State = test.state
			fixture.store.daemon.StateRevision++
			proof, err := remoteauth.SignDeviceIdentityProof(fixture.identity, challenge.GetChallenge())
			if err != nil {
				t.Fatal(err)
			}
			response, err := fixture.service.CompleteDaemonBindingRefresh(context.Background(), &cloudv1.CompleteDaemonBindingRefreshRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof})
			if err != nil {
				t.Fatal(err)
			}
			if response.GetDaemon().GetState() != test.state || response.GetDaemon().GetStateRevision() != fixture.store.daemon.StateRevision {
				t.Fatalf("refresh lifecycle = %+v", response.GetDaemon())
			}
			hasMaterial := response.GetDaemonBinding() != nil && response.GetEdgeLocator() != nil
			if hasMaterial != test.wantMaterial {
				t.Fatalf("route material present=%v response=%+v", hasMaterial, response)
			}
			if !test.wantMaterial && (response.GetDaemonBinding() != nil || response.GetEdgeLocator() != nil) {
				t.Fatalf("deleted daemon received route material: %+v", response)
			}
		})
	}
}

type bindingRefreshFixture struct {
	service          *Service
	store            *preflightEnrollmentStore
	identity         remoteauth.Identity
	now              time.Time
	edge             edgeconfig.Edge
	edgeStore        *refreshEdgeStore
	directory        *directory.Directory
	bindingKeyID     string
	bindingPublicKey ed25519.PublicKey
}

func newBindingRefreshFixture(t *testing.T) bindingRefreshFixture {
	t.Helper()
	now := time.Unix(30_000, 0).UTC()
	_, identityPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("refresh-device", identityPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	store := &preflightEnrollmentStore{daemon: Daemon{
		ID: uuid.NewString(), AccountID: uuid.NewString(), AccountName: "Refresh account", DisplayName: "Refresh daemon",
		DeviceID: identity.DeviceID, DeviceFingerprint: identity.Fingerprint, DevicePublicKey: append(ed25519.PublicKey(nil), identity.PublicKey...),
		State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 1, EdgePreferenceRevision: 1, EdgePreferenceUpdatedAt: now, CreatedAt: now, UpdatedAt: now,
	}}
	edge := edgeconfig.Edge{ID: uuid.NewString(), Name: "Refresh Edge", Region: "refresh", Capacity: 10, PublicEndpoint: "refresh.test.example:41102", Enabled: true, ConfigVersion: 2, Revision: 2}
	_, edgeSigningKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edgeStore := &refreshEdgeStore{edges: []edgeconfig.Edge{edge}}
	edges, err := edgeconfig.NewService(edgeconfig.Config{Store: edgeStore, SigningKey: edgeSigningKey, SigningKeyID: "edge-refresh-key", ClaimTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	runtimeDirectory, err := directory.New(directory.Config{MailboxSize: 16, GracePeriod: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimeDirectory.Close)
	publishEnrollmentTestEdge(t, runtimeDirectory, edge.ID)
	bindingPublicKey, bindingPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const bindingKeyID = "binding-refresh-key"
	service, err := NewService(Config{
		Store: store, Edges: edges, Directory: runtimeDirectory, Entitlement: &preflightEntitlement{active: true},
		BindingSigningKey: bindingPrivateKey, BindingSigningKeyID: bindingKeyID, EdgeCACertificate: []byte("refresh-test-ca"),
		EnrollmentTTL: time.Minute, ChallengeTTL: time.Minute, BindingTTL: time.Hour, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return bindingRefreshFixture{service: service, store: store, identity: identity, now: now, edge: edge, edgeStore: edgeStore, directory: runtimeDirectory, bindingKeyID: bindingKeyID, bindingPublicKey: bindingPublicKey}
}

func (fixture bindingRefreshFixture) beginRefresh(t *testing.T) *cloudv1.IdentityChallenge {
	t.Helper()
	challenge, err := fixture.service.BeginDaemonBindingRefresh(context.Background(), &cloudv1.BeginDaemonBindingRefreshRequest{DaemonId: fixture.store.daemon.ID})
	if err != nil {
		t.Fatal(err)
	}
	return challenge
}

func (fixture bindingRefreshFixture) refresh(identity remoteauth.Identity) (*cloudv1.RefreshDaemonBindingResponse, error) {
	challenge, err := fixture.service.BeginDaemonBindingRefresh(context.Background(), &cloudv1.BeginDaemonBindingRefreshRequest{DaemonId: fixture.store.daemon.ID})
	if err != nil {
		return nil, err
	}
	proof, err := remoteauth.SignDeviceIdentityProof(identity, challenge.GetChallenge())
	if err != nil {
		return nil, err
	}
	return fixture.service.CompleteDaemonBindingRefresh(context.Background(), &cloudv1.CompleteDaemonBindingRefreshRequest{ChallengeId: challenge.GetChallengeId(), DeviceProof: proof})
}

type refreshEdgeStore struct{ edges []edgeconfig.Edge }

func (store refreshEdgeStore) ListEdges(context.Context) ([]edgeconfig.Edge, error) {
	return append([]edgeconfig.Edge(nil), store.edges...), nil
}
func (store refreshEdgeStore) GetEdge(_ context.Context, edgeID string) (edgeconfig.Edge, error) {
	for _, edge := range store.edges {
		if edge.ID == edgeID {
			return edge, nil
		}
	}
	return edgeconfig.Edge{}, errors.New("not found")
}
func (refreshEdgeStore) CreateEdge(context.Context, edgeconfig.Edge, []byte, time.Time) error {
	return errors.New("unused")
}
func (refreshEdgeStore) UpdateEdge(context.Context, edgeconfig.UpdateInput, edgeconfig.Edge) error {
	return errors.New("unused")
}
func (refreshEdgeStore) DeleteEdge(context.Context, edgeconfig.DeleteInput) error {
	return errors.New("unused")
}
func (refreshEdgeStore) ConsumeInstallClaim(context.Context, []byte, []byte, time.Time) (edgeconfig.Edge, error) {
	return edgeconfig.Edge{}, errors.New("unused")
}
func (refreshEdgeStore) ConsumeBootstrapClaim(context.Context, []byte, string, []byte) (edgeconfig.Edge, error) {
	return edgeconfig.Edge{}, errors.New("unused")
}

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
	measurements []EdgeMeasurement
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
func (store *preflightEnrollmentStore) ListDaemonsByAccount(_ context.Context, accountID string) ([]Daemon, error) {
	if store.daemon.AccountID != accountID {
		return nil, nil
	}
	return []Daemon{store.daemon}, nil
}
func (store *preflightEnrollmentStore) ListDaemonEdgeMeasurements(context.Context, string) ([]EdgeMeasurement, error) {
	return append([]EdgeMeasurement(nil), store.measurements...), nil
}
func (store *preflightEnrollmentStore) UpsertDaemonEdgeMeasurements(_ context.Context, _ string, measurements []EdgeMeasurement) error {
	store.measurements = append([]EdgeMeasurement(nil), measurements...)
	return nil
}
func (store *preflightEnrollmentStore) ChangeDaemonEdgePreference(_ context.Context, accountID, daemonID, edgeID string, expectedRevision uint64, now time.Time) (Daemon, error) {
	if accountID != store.daemon.AccountID || daemonID != store.daemon.ID || expectedRevision != store.daemon.EdgePreferenceRevision {
		return Daemon{}, ErrDaemonUnavailable
	}
	store.daemon.PreferredEdgeID = edgeID
	store.daemon.EdgePreferenceRevision++
	store.daemon.EdgePreferenceUpdatedAt = now
	return store.daemon, nil
}

type preflightEntitlement struct{ active bool }

func (entitlement *preflightEntitlement) EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error) {
	state := cloudv1.EntitlementState_ENTITLEMENT_STATE_SUSPENDED
	if entitlement.active {
		state = cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE
	}
	return &cloudv1.EffectiveEntitlement{State: state, Capability: &cloudv1.CloudCapability{ManagedP2PEnabled: true, CloudDaemonLimit: 10}}, nil
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
func (preflightEdgeStore) DeleteEdge(context.Context, edgeconfig.DeleteInput) error {
	return errors.New("unused")
}
func (store preflightEdgeStore) ConsumeInstallClaim(context.Context, []byte, []byte, time.Time) (edgeconfig.Edge, error) {
	return store.edge, errors.New("unused")
}
func (store preflightEdgeStore) ConsumeBootstrapClaim(context.Context, []byte, string, []byte) (edgeconfig.Edge, error) {
	return store.edge, errors.New("unused")
}
