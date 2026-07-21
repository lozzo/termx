package remoteauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/shared/filelock"
)

func TestCredentialStoreKeepsEndpointIdentityStableAcrossLostResponseRecovery(t *testing.T) {
	dir := t.TempDir()
	store := NewCredentialStore(dir)
	first, err := store.LoadOrCreateIdentity("lab-access", "endpoint-lab", bytes.NewReader(bytes.Repeat([]byte{0x31}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOrCreateIdentity("lab-access", "endpoint-lab", bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity.Fingerprint != second.Identity.Fingerprint || first.Ready() || second.Ready() {
		t.Fatalf("pending credential identity changed: first=%#v second=%#v", first, second)
	}
	if _, err := store.LoadOrCreateIdentity("lab-access", "endpoint-other", rand.Reader); err == nil {
		t.Fatal("credential ref was rebound to another endpoint")
	}
	info, err := os.Stat(filepath.Join(dir, credentialFileName("lab-access")))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions: info=%v err=%v", info, err)
	}
}

func TestCredentialStoreBindsOnlyGrantForStoredSubject(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	credential, err := store.LoadOrCreateIdentity("lab-access", "endpoint-lab", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonPublic, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	issue := func(subject string) string {
		grant, issueErr := Issue(daemonPrivate, Claims{
			GrantID: "grant-1", IssuerDeviceID: "device-1", SubjectKeyFingerprint: subject,
			Scope: Scope{AllowDaemon: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		return grant
	}
	other, _ := GenerateClientAccessIdentity("endpoint-lab", rand.Reader)
	if _, err := store.BindGrant("lab-access", issue(other.Fingerprint), Fingerprint(daemonPublic), now, BindGrantOptions{}); !errors.Is(err, ErrGrantSubjectMismatch) {
		t.Fatalf("wrong subject bind error = %v", err)
	}
	bound, err := store.BindGrant("lab-access", issue(credential.Identity.Fingerprint), Fingerprint(daemonPublic), now, BindGrantOptions{})
	if err != nil || !bound.Ready() {
		t.Fatalf("BindGrant: credential=%#v err=%v", bound, err)
	}
	resolved, err := store.Resolve("lab-access")
	if err != nil || resolved.Identity.Fingerprint != credential.Identity.Fingerprint || !resolved.Ready() {
		t.Fatalf("Resolve: credential=%#v err=%v", resolved, err)
	}
}

func TestCredentialStoreSerializesCrossInstanceIdentityCreation(t *testing.T) {
	dir := t.TempDir()
	stores := []*CredentialStore{NewCredentialStore(dir), NewCredentialStore(dir)}
	results := make(chan ClientAccessCredential, len(stores))
	errorsCh := make(chan error, len(stores))
	var wait sync.WaitGroup
	for index, store := range stores {
		wait.Add(1)
		go func(index int, store *CredentialStore) {
			defer wait.Done()
			credential, err := store.LoadOrCreateIdentity("shared-ref", "endpoint-shared", bytes.NewReader(bytes.Repeat([]byte{byte(index + 1)}, 64)))
			results <- credential
			errorsCh <- err
		}(index, store)
	}
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var fingerprint string
	for credential := range results {
		if fingerprint == "" {
			fingerprint = credential.Identity.Fingerprint
		} else if credential.Identity.Fingerprint != fingerprint {
			t.Fatalf("cross-instance credential creation split keys: %q != %q", credential.Identity.Fingerprint, fingerprint)
		}
	}
}

func TestCredentialStoreRequiresExplicitScopeExpansion(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	credential, err := store.LoadOrCreateIdentity("scope-ref", "endpoint-scope", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	issue := func(grantID string, scope Scope) string {
		grant, issueErr := Issue(privateKey, Claims{
			GrantID: grantID, IssuerDeviceID: "device-scope", SubjectKeyFingerprint: credential.Identity.Fingerprint,
			Scope: scope, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		return grant
	}
	fingerprint := Fingerprint(publicKey)
	if _, err := store.BindGrant("scope-ref", issue("grant-narrow", Scope{AllowDaemon: true}), fingerprint, now, BindGrantOptions{}); err != nil {
		t.Fatal(err)
	}
	expanded := issue("grant-expanded", Scope{AllowDaemon: true, FileReadMetadata: true})
	if _, err := store.BindGrant("scope-ref", expanded, fingerprint, now, BindGrantOptions{}); !errors.Is(err, ErrGrantScopeExpansion) {
		t.Fatalf("silent scope expansion error = %v", err)
	}
	if _, err := store.BindGrant("scope-ref", expanded, fingerprint, now, BindGrantOptions{AllowScopeExpansion: true}); err != nil {
		t.Fatalf("confirmed scope expansion failed: %v", err)
	}
}

func TestCredentialStoreChecksScopeBeforeExchangeAndRecoversByBundleDigest(t *testing.T) {
	_, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-pair-client", daemonPrivate)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	accessStore, err := LoadAccessStore(t.TempDir(), identity, AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accessStore.Close() })
	credentialStore := NewCredentialStore(t.TempDir())
	issue := func(scope Scope) []byte {
		bundle, _, issueErr := accessStore.IssuePairingBundle(PairingIssueOptions{
			Scope: scope, TicketTTL: time.Hour, GrantLifetime: 48 * time.Hour, Now: now,
		})
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		payload, encodeErr := EncodePairingBundle(bundle)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return payload
	}
	redeem := func(payload []byte, label string) func(ClientAccessIdentity) (PairingExchangeResult, error) {
		return func(client ClientAccessIdentity) (PairingExchangeResult, error) {
			return accessStore.RedeemPairingBundle(payload, client.PublicKey, label, now)
		}
	}
	narrowPayload := issue(Scope{TerminalID: "term-1"})
	if _, err := credentialStore.PairAndBind(
		context.Background(), "pair-ref", "endpoint-1", narrowPayload, fixedNow(now), rand.Reader, BindGrantOptions{}, redeem(narrowPayload, "narrow"),
	); err != nil {
		t.Fatal(err)
	}
	expandedPayload := issue(Scope{AllowDaemon: true})
	_, expandedClaims, err := ParsePairingBundleForExchange(expandedPayload)
	if err != nil {
		t.Fatal(err)
	}
	exchangeCalls := 0
	_, err = credentialStore.PairAndBind(
		context.Background(), "pair-ref", "endpoint-1", expandedPayload, fixedNow(now), rand.Reader, BindGrantOptions{},
		func(client ClientAccessIdentity) (PairingExchangeResult, error) {
			exchangeCalls++
			return accessStore.RedeemPairingBundle(expandedPayload, client.PublicKey, "expanded", now)
		},
	)
	if !errors.Is(err, ErrGrantScopeExpansion) || exchangeCalls != 0 {
		t.Fatalf("scope expansion reached daemon: calls=%d err=%v", exchangeCalls, err)
	}
	if accessStore.tickets[expandedClaims.TicketID].GrantID != "" {
		t.Fatal("unconfirmed scope expansion consumed the PairingTicket")
	}
	bound, err := credentialStore.PairAndBind(
		context.Background(), "pair-ref", "endpoint-1", expandedPayload, fixedNow(now), rand.Reader, BindGrantOptions{AllowScopeExpansion: true},
		func(client ClientAccessIdentity) (PairingExchangeResult, error) {
			exchangeCalls++
			return accessStore.RedeemPairingBundle(expandedPayload, client.PublicKey, "expanded", now)
		},
	)
	if err != nil || exchangeCalls != 1 || bound.LastPairingBundleDigest == "" {
		t.Fatalf("confirmed scope expansion = credential %#v calls=%d err=%v", bound, exchangeCalls, err)
	}
	recovered, err := credentialStore.PairAndBind(
		context.Background(), "pair-ref", "endpoint-1", expandedPayload, fixedNow(now.Add(defaultDeliveryGrace+time.Hour)), rand.Reader, BindGrantOptions{},
		func(ClientAccessIdentity) (PairingExchangeResult, error) {
			return PairingExchangeResult{}, errors.New("same bundle should recover locally after delivery grace")
		},
	)
	if err != nil || recovered.CapabilityGrant != bound.CapabilityGrant {
		t.Fatalf("bundle digest recovery = credential %#v err=%v", recovered, err)
	}
	shortBundle, _, err := accessStore.IssuePairingBundle(PairingIssueOptions{
		Scope: Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Second, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	shortPayload, _ := EncodePairingBundle(shortBundle)
	_, err = credentialStore.PairAndBind(
		context.Background(), "pair-ref", "endpoint-1", shortPayload, fixedNow(now.Add(time.Second)), rand.Reader, BindGrantOptions{},
		func(client ClientAccessIdentity) (PairingExchangeResult, error) {
			return accessStore.RedeemPairingBundle(shortPayload, client.PublicKey, "short", now)
		},
	)
	if !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("expired exchange result error = %v", err)
	}
	unchanged, err := credentialStore.Resolve("pair-ref")
	if err != nil || unchanged.CapabilityGrant != bound.CapabilityGrant || unchanged.LastPairingBundleDigest != bound.LastPairingBundleDigest {
		t.Fatalf("expired exchange replaced existing credential: credential=%#v err=%v", unchanged, err)
	}
}

func TestAccessStoreEnforcesSingleProcessOwner(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-owner", privateKey)
	dir := t.TempDir()
	first, err := LoadAccessStore(dir, identity, AccessStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccessStore(dir, identity, AccessStoreOptions{}); !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("second AccessStore owner error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := LoadAccessStore(dir, identity, AccessStoreOptions{})
	if err != nil {
		t.Fatalf("owner lock was not released: %v", err)
	}
	_ = second.Close()
}

func TestAccessStoreCompactsDeliveredTicketsOnlyOnLaterMutation(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-compaction", privateKey)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	store, err := LoadAccessStore(t.TempDir(), identity, AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, claims, err := store.IssuePairingBundle(PairingIssueOptions{Scope: Scope{AllowDaemon: true}, TicketTTL: time.Minute, GrantLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodePairingBundle(bundle)
	client, _ := GenerateClientAccessIdentity("endpoint-compaction", rand.Reader)
	if _, err := store.RedeemPairingBundle(payload, client.PublicKey, "compaction", now); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.tickets[claims.TicketID]; !exists {
		t.Fatal("consumed ticket disappeared before delivery grace")
	}
	now = now.Add(defaultDeliveryGrace)
	if _, _, err := store.IssuePairingBundle(PairingIssueOptions{Scope: Scope{MachineEventsOnly: true}, TicketTTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.tickets[claims.TicketID]; exists {
		t.Fatal("expired delivery record was not compacted by later low-frequency mutation")
	}
}

func TestAccessStoreKeepsPublishedStateInMemoryAfterDurabilityError(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-published-state", privateKey)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	store, err := LoadAccessStore(t.TempDir(), identity, AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, claims, err := store.IssuePairingBundle(PairingIssueOptions{Scope: Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodePairingBundle(bundle)
	client, _ := GenerateClientAccessIdentity("endpoint-published", rand.Reader)
	originalWriter := store.writeFile
	injected := errors.New("injected post-rename durability failure")
	failNextPublishedWrite := func() {
		failed := false
		store.writeFile = func(path string, payload []byte) error {
			if !failed {
				failed = true
				return writePrivateFileWithPostPublish(path, payload, func(string) error { return injected })
			}
			return originalWriter(path, payload)
		}
	}
	failNextPublishedWrite()
	if _, err := store.RedeemPairingBundle(payload, client.PublicKey, "published", now); !errors.Is(err, injected) {
		t.Fatalf("published redemption error = %v", err)
	}
	grantID := store.tickets[claims.TicketID].GrantID
	if grantID == "" || store.Revoked(grantID) {
		t.Fatalf("published redemption rolled back memory: ticket=%#v", store.tickets[claims.TicketID])
	}
	result, err := store.RedeemPairingBundle(payload, client.PublicKey, "published", now)
	if err != nil || result.GrantID != grantID {
		t.Fatalf("published redemption was not recoverable: result=%#v err=%v", result, err)
	}
	failNextPublishedWrite()
	if _, err := store.RevokeGrant(grantID); !errors.Is(err, injected) {
		t.Fatalf("published revoke error = %v", err)
	}
	if !store.Revoked(grantID) {
		t.Fatal("published revoke rolled back in-memory revocation truth")
	}
	snapshot, err := LoadAccessSnapshot(filepath.Dir(store.path), identity)
	if err != nil || !snapshot.Revoked(grantID) {
		t.Fatalf("published revoke was not visible on disk: snapshot=%#v err=%v", snapshot, err)
	}
	if record, err := store.RevokeGrant(grantID); err != nil || record.RevokedAt.IsZero() {
		t.Fatalf("published revoke idempotent retry = record %#v err=%v", record, err)
	}
}

func TestAccessStoreConcurrentRedeemIdempotencyRevokeAndRestart(t *testing.T) {
	_, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-1", daemonPrivate)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := LoadAccessStore(dir, identity, AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, _, err := store.IssuePairingBundle(PairingIssueOptions{Scope: FullDaemonScope(), TicketTTL: time.Hour, GrantLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	bundlePayload, err := EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	clientA, _ := GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	clientB, _ := GenerateClientAccessIdentity("endpoint-1", rand.Reader)
	type redemption struct {
		result PairingExchangeResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan redemption, 2)
	var wg sync.WaitGroup
	for _, client := range []ClientAccessIdentity{clientA, clientB} {
		wg.Add(1)
		go func(client ClientAccessIdentity) {
			defer wg.Done()
			<-start
			result, redeemErr := store.RedeemPairingBundle(bundlePayload, client.PublicKey, client.Fingerprint, now)
			results <- redemption{result: result, err: redeemErr}
		}(client)
	}
	close(start)
	wg.Wait()
	close(results)
	var winner redemption
	consumed := 0
	for result := range results {
		if result.err == nil {
			winner = result
		} else if errors.Is(result.err, ErrPairingTicketConsumed) {
			consumed++
		} else {
			t.Fatalf("unexpected redemption error: %v", result.err)
		}
	}
	if winner.result.Grant == "" || consumed != 1 {
		t.Fatalf("concurrent redemption winner=%#v consumed=%d", winner, consumed)
	}
	winnerPublic := clientA.PublicKey
	if winner.result.SubjectKeyFingerprint == clientB.Fingerprint {
		winnerPublic = clientB.PublicKey
	}
	retry, err := store.RedeemPairingBundle(bundlePayload, winnerPublic, "renamed client", now)
	if err != nil || retry.Grant != winner.result.Grant || retry.DeliveryReceipt != winner.result.DeliveryReceipt {
		t.Fatalf("idempotent retry changed result: retry=%#v err=%v winner=%#v", retry, err, winner.result)
	}
	expiredRetry, err := store.RedeemPairingBundle(bundlePayload, winnerPublic, "renamed again", now.Add(2*time.Hour))
	if err != nil || expiredRetry.Grant != winner.result.Grant || expiredRetry.DeliveryReceipt != winner.result.DeliveryReceipt {
		t.Fatalf("lost-response recovery after ticket expiry changed result: retry=%#v err=%v winner=%#v", expiredRetry, err, winner.result)
	}
	loserPublic := clientB.PublicKey
	if winner.result.SubjectKeyFingerprint == clientB.Fingerprint {
		loserPublic = clientA.PublicKey
	}
	if _, err := store.RedeemPairingBundle(bundlePayload, loserPublic, "loser", now.Add(2*time.Hour)); !errors.Is(err, ErrPairingTicketConsumed) {
		t.Fatalf("expired ticket bound to another key error = %v", err)
	}
	if _, err := store.RedeemPairingBundle(bundlePayload, winnerPublic, "winner", now.Add(defaultDeliveryGrace)); !errors.Is(err, ErrPairingTicketConsumed) {
		t.Fatalf("delivery grace exact expiry error = %v", err)
	}
	beforeReadOnly, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(winner.result.Grant, identity.Fingerprint, now.Add(time.Minute), store); err != nil {
		t.Fatalf("verify active grant: %v", err)
	}
	if len(store.ListClientAccess()) != 1 {
		t.Fatal("read-only access projection lost grant")
	}
	afterReadOnly, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeReadOnly, afterReadOnly) {
		t.Fatal("ordinary capability verification or access listing rewrote AccessStore")
	}
	storedPayload, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(storedPayload, []byte(winner.result.Grant)) || bytes.Contains(storedPayload, []byte(winner.result.DeliveryReceipt)) ||
		bytes.Contains(storedPayload, []byte("termx-grant-v2")) || bytes.Contains(storedPayload, bundlePayload) {
		t.Fatal("AccessStore persisted raw pairing bundle, grant, or delivery receipt")
	}
	unknownGrant, err := Issue(identity.PrivateKey, Claims{
		GrantID: "grant-outside-store", IssuerDeviceID: identity.DeviceID, SubjectKeyFingerprint: winner.result.SubjectKeyFingerprint,
		Scope: Scope{AllowDaemon: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(unknownGrant, identity.Fingerprint, now.Add(time.Minute), store); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("grant absent from AccessStore must fail closed, got %v", err)
	}
	expiredBundle, _, err := store.IssuePairingBundle(PairingIssueOptions{Scope: Scope{TerminalID: "term-expired"}, TicketTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	expiredPayload, err := EncodePairingBundle(expiredBundle)
	if err != nil {
		t.Fatal(err)
	}
	unusedClient, _ := GenerateClientAccessIdentity("endpoint-expired", rand.Reader)
	if _, err := store.RedeemPairingBundle(expiredPayload, unusedClient.PublicKey, "expired", now.Add(2*time.Minute)); !errors.Is(err, ErrPairingTicketExpired) {
		t.Fatalf("unconsumed expired ticket error = %v", err)
	}
	beforeRevokeRevision := store.AccessProjectionRevision()
	if beforeRevokeRevision == 0 {
		t.Fatal("grant creation did not advance access projection revision")
	}
	if _, err := store.RevokeGrant(winner.result.GrantID); err != nil {
		t.Fatal(err)
	}
	if store.AccessProjectionRevision() != beforeRevokeRevision+1 {
		t.Fatalf("revoke revision = %d, want %d", store.AccessProjectionRevision(), beforeRevokeRevision+1)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadAccessStore(dir, identity, AccessStoreOptions{Now: func() time.Time { return now.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })
	if !reloaded.Revoked(winner.result.GrantID) || len(reloaded.ListClientAccess()) != 1 {
		t.Fatalf("access truth did not survive restart: %#v", reloaded.ListClientAccess())
	}
	if reloaded.AccessProjectionRevision() != beforeRevokeRevision+1 {
		t.Fatalf("reloaded access projection revision = %d", reloaded.AccessProjectionRevision())
	}
	if _, err := Verify(winner.result.Grant, identity.Fingerprint, now.Add(time.Minute), reloaded); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("reloaded revocation error = %v", err)
	}
}

func TestCredentialStoreRejectsUnsafeOrMissingRef(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	for _, ref := range []string{"", "../escape", "with space", "/absolute"} {
		if _, err := store.LoadOrCreateIdentity(ref, "endpoint-1", rand.Reader); err == nil {
			t.Fatalf("unsafe ref %q was accepted", ref)
		}
	}
	if _, err := store.Resolve("missing"); err == nil {
		t.Fatal("missing credential ref must fail")
	}
}

func TestAccessStoreRejectsBrokenTicketGrantLinkOnRestart(t *testing.T) {
	_, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-corrupt", daemonPrivate)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := LoadAccessStore(dir, identity, AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, claims, err := store.IssuePairingBundle(PairingIssueOptions{Scope: FullDaemonScope(), TicketTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := GenerateClientAccessIdentity("endpoint-corrupt", rand.Reader)
	bundlePayload, err := EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RedeemPairingBundle(bundlePayload, client.PublicKey, "original", now); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var state storedAccessState
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatal(err)
	}
	record := state.Tickets[claims.TicketID]
	record.ClientLabel = "tampered"
	state.Tickets[claims.TicketID] = record
	payload, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccessStore(dir, identity, AccessStoreOptions{}); err == nil {
		t.Fatal("corrupt ticket/grant linkage was accepted after restart")
	}
}
