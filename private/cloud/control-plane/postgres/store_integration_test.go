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
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

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
