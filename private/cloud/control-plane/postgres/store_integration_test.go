package postgres_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/persistence"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestDaemonEnrollmentCommitRollsBackEveryDurableWrite(t *testing.T) {
	ctx := context.Background()
	store, err := cloudpostgres.Open(ctx, testPostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	account, accountSession, subscription, entitlement, existingAudit := commerceFixture("account-enrollment", "enrollment@example.com", 0x31, now)
	if err := store.CreateAccount(ctx, account, accountSession, subscription, entitlement, existingAudit); err != nil {
		t.Fatal(err)
	}
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	accessHash := sha256.Sum256([]byte("daemon-access"))
	refreshHash := sha256.Sum256([]byte("daemon-refresh"))
	session := commerce.SessionRecord{
		SessionID: "session-daemon-enrollment", AccountID: account.Projection.GetAccountId(), ClientDeviceID: "daemon-atomic",
		AccessTokenHash: accessHash, RefreshTokenHash: refreshHash, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), Revision: 1,
	}
	input := persistence.DaemonEnrollmentCommit{
		NextOwnership:  cloudtopology.DeviceOwnership{DeviceID: "daemon-atomic", AccountID: account.Projection.GetAccountId(), Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1, PublicKey: publicKey},
		NextAssignment: &cloudpb.HubAssignment{DaemonDeviceId: "daemon-atomic", AccountId: account.Projection.GetAccountId(), HubId: "hub-atomic", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(24 * time.Hour).UnixMilli()},
		Session:        session,
		// 重复 audit_id 故意让事务最后一步失败，以证明此前 ownership、assignment 与 session 均未泄漏。
		Audit: &cloudpb.CommerceAuditProjection{AuditId: existingAudit.GetAuditId(), AccountId: account.Projection.GetAccountId(), ActorId: account.Projection.GetUserId(), Action: "session.device.issue", ResourceId: "daemon-atomic", OccurredAtUnixMillis: now.UnixMilli()},
	}
	input.NextOwnership.AuthEpoch = 2
	if _, err := store.CommitDaemonEnrollment(ctx, input, now); !errors.Is(err, cloudtopology.ErrTopologyRejected) {
		t.Fatalf("commit with stale account auth revision = %v", err)
	}
	input.NextOwnership.AuthEpoch = 1
	if _, err := store.CommitDaemonEnrollment(ctx, input, now); !errors.Is(err, commerce.ErrConflict) {
		t.Fatalf("commit with duplicate audit = %v", err)
	}
	if _, err := store.DeviceOwnership(ctx, "daemon-atomic"); !errors.Is(err, cloudtopology.ErrOwnershipNotFound) {
		t.Fatalf("failed enrollment leaked ownership: %v", err)
	}
	if _, err := store.Assignment(ctx, "daemon-atomic"); !errors.Is(err, hubregistry.ErrAssignmentConflict) {
		t.Fatalf("failed enrollment leaked assignment: %v", err)
	}
	if _, err := store.SessionByRefreshHash(ctx, refreshHash); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("failed enrollment leaked session: %v", err)
	}

	input.Audit.AuditId = "audit-daemon-enrollment"
	assignment, err := store.CommitDaemonEnrollment(ctx, input, now)
	if err != nil {
		t.Fatal(err)
	}
	owner, ownerErr := store.DeviceOwnership(ctx, "daemon-atomic")
	storedSession, sessionErr := store.SessionByRefreshHash(ctx, refreshHash)
	if ownerErr != nil || owner.AccountID != account.Projection.GetAccountId() || assignment.Value.GetHubId() != "hub-atomic" || sessionErr != nil || storedSession.SessionID != session.SessionID {
		t.Fatalf("committed enrollment = owner(%v,%v) assignment(%v) session(%v,%v)", owner, ownerErr, assignment, storedSession, sessionErr)
	}
}

func TestCommerceTransactionRollbackAndRestart(t *testing.T) {
	ctx := context.Background()
	dsn := testPostgresDSN(t)
	store, err := cloudpostgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 20, 0, 0, 0, time.UTC)
	account, session, subscription, entitlement, audit := commerceFixture("account-1", "first@example.com", 0x11, now)
	if err := store.CreateAccount(ctx, account, session, subscription, entitlement, audit); err != nil {
		t.Fatal(err)
	}
	conflicting, conflictingSession, conflictingSubscription, conflictingEntitlement, conflictingAudit := commerceFixture("account-2", "first@example.com", 0x22, now)
	if err := store.CreateAccount(ctx, conflicting, conflictingSession, conflictingSubscription, conflictingEntitlement, conflictingAudit); !errors.Is(err, commerce.ErrConflict) {
		t.Fatalf("duplicate account error = %v", err)
	}
	if _, err := store.SessionByAccessHash(ctx, conflictingSession.AccessTokenHash); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("failed account transaction leaked session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := cloudpostgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.AccountByEmail(ctx, "first@example.com")
	if err != nil || loaded.Projection.GetAccountId() != "account-1" {
		t.Fatalf("restarted account = (%v, %v)", loaded.Projection, err)
	}
	loadedSession, err := reopened.SessionByRefreshHash(ctx, session.RefreshTokenHash)
	if err != nil || loadedSession.SessionID != session.SessionID {
		t.Fatalf("restarted session = (%v, %v)", loadedSession, err)
	}
}

func TestHubGenerationIsSerializedAcrossConcurrentAttach(t *testing.T) {
	ctx := context.Background()
	store, err := cloudpostgres.Open(ctx, testPostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 21, 21, 0, 0, 0, time.UTC)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	relayPublicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", HubId: "hub-1", Region: "local-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(publicKey), RelayId: "relay-1", RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayPublicKey)}
	registry, _ := hubregistry.New(store)
	if err := registry.RegisterDeployment(ctx, hubregistry.Deployment{Metadata: metadata, ControlPublicKey: publicKey, RelayControlPublicKey: relayPublicKey, Enabled: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	const attaches = 8
	generations := make(chan uint64, attaches)
	errorsOut := make(chan error, attaches)
	var group sync.WaitGroup
	for index := 0; index < attaches; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			value, attachErr := registry.AttachHub(ctx, &cloudpb.HubHello{Deployment: metadata}, now)
			if attachErr != nil {
				errorsOut <- attachErr
				return
			}
			generations <- value.ControlGeneration
		}()
	}
	group.Wait()
	close(generations)
	close(errorsOut)
	for attachErr := range errorsOut {
		t.Fatal(attachErr)
	}
	values := make([]int, 0, attaches)
	for generation := range generations {
		values = append(values, int(generation))
	}
	sort.Ints(values)
	for index, generation := range values {
		if generation != index+1 {
			t.Fatalf("serialized generations = %v", values)
		}
	}
}

func TestControlCursorPersists(t *testing.T) {
	ctx := context.Background()
	store, err := cloudpostgres.Open(ctx, testPostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 21, 21, 30, 0, 0, time.UTC)
	if err := store.PutControlCursor(ctx, "hub-1", 1, cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB, 2, []byte("digest"), now); err != nil {
		t.Fatal(err)
	}
	sequence, digest, err := store.ControlCursor(ctx, "hub-1", 1, cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB)
	if err != nil || sequence != 2 || string(digest) != "digest" {
		t.Fatalf("control cursor = (%d, %q, %v)", sequence, digest, err)
	}
}

func commerceFixture(accountID, email string, marker byte, now time.Time) (commerce.AccountRecord, commerce.SessionRecord, *cloudpb.SubscriptionProjection, *cloudpb.EntitlementProjection, *cloudpb.CommerceAuditProjection) {
	account := &cloudpb.AccountProjection{AccountId: accountID, UserId: "user-" + accountID, Email: email, AuthRevision: 1, CreatedAtUnixMillis: now.UnixMilli()}
	accessHash := sha256.Sum256([]byte{marker, 0x01})
	refreshHash := sha256.Sum256([]byte{marker, 0x02})
	session := commerce.SessionRecord{SessionID: "session-" + accountID, AccountID: accountID, AccessTokenHash: accessHash, RefreshTokenHash: refreshHash, AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour), Revision: 1}
	subscription := &cloudpb.SubscriptionProjection{AccountId: accountID, SubscriptionId: "subscription-" + accountID, PlanId: "included", Revision: 1}
	entitlement := &cloudpb.EntitlementProjection{AccountId: accountID, SourceSubscriptionId: subscription.GetSubscriptionId()}
	audit := &cloudpb.CommerceAuditProjection{AuditId: "audit-" + accountID, AccountId: accountID, OccurredAtUnixMillis: now.UnixMilli()}
	return commerce.AccountRecord{Projection: account, PasswordHash: []byte("password-hash")}, session, subscription, entitlement, audit
}
