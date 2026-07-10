package relay_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/usage"
	"github.com/lozzow/termx/private/termx-cloud/relay"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func TestAuthorityRequiresSignedLeaseAndEnforcesConcurrencyQuota(t *testing.T) {
	fixture := newRelayFixture(t, 2, 5_000, 8)
	if _, ok := fixture.authority.AuthenticateTURN("unknown", "termx-relay", "source-0"); ok {
		t.Fatal("TURN auth succeeded without active lease")
	}
	activation, err := fixture.authority.ActivateLease(fixture.activationRequest)
	if err != nil {
		t.Fatal(err)
	}
	if activation.ClientCredential.Username == activation.DaemonCredential.Username || activation.ClientCredential.Password == activation.DaemonCredential.Password {
		t.Fatal("client and daemon received shared TURN credential")
	}
	if strings.Contains(activation.ClientCredential.String(), activation.ClientCredential.Password) {
		t.Fatal("Credential.String leaked password")
	}
	if _, ok := fixture.authority.AuthenticateTURN(activation.ClientCredential.Username, "wrong-realm", "source-client"); ok {
		t.Fatal("TURN auth accepted wrong realm")
	}
	if key, ok := fixture.authority.AuthenticateTURN(activation.ClientCredential.Username, "termx-relay", "source-client"); !ok || len(key) == 0 {
		t.Fatal("client TURN auth failed")
	}
	if err := fixture.authority.ConfirmAllocation("source-client", "allocation-client", activation.ClientCredential.Username); err != nil {
		t.Fatal(err)
	}
	if _, ok := fixture.authority.AuthenticateTURN(activation.DaemonCredential.Username, "termx-relay", "source-daemon"); !ok {
		t.Fatal("daemon TURN auth failed")
	}
	if err := fixture.authority.ConfirmAllocation("source-daemon", "allocation-daemon", activation.DaemonCredential.Username); err != nil {
		t.Fatal(err)
	}
	if _, ok := fixture.authority.AuthenticateTURN(activation.ClientCredential.Username, "termx-relay", "source-third"); ok {
		t.Fatal("TURN auth exceeded lease concurrency")
	}
	if err := fixture.authority.RecordTraffic("allocation-client", 400, 500); err != nil {
		t.Fatal(err)
	}
	if err := fixture.authority.RecordTraffic("allocation-client", 601, 0); !errors.Is(err, relay.ErrQuota) {
		t.Fatalf("bitrate quota error = %v", err)
	}
	fixture.clock.Advance(2 * time.Second)
	if err := fixture.authority.RecordTraffic("allocation-client", 600, 500); err != nil {
		t.Fatal(err)
	}
	if err := fixture.authority.RecordTraffic("missing", 1, 1); !errors.Is(err, relay.ErrAllocationNotFound) {
		t.Fatalf("missing allocation error = %v", err)
	}
	fixture.authority.ReleaseAllocation("allocation-client")
	if _, ok := fixture.authority.AuthenticateTURN(activation.ClientCredential.Username, "termx-relay", "source-third"); !ok {
		t.Fatal("released allocation did not free concurrency")
	}
}

func TestAuthoritySignsUsageThatControlPlaneSettlesOnce(t *testing.T) {
	fixture := newRelayFixture(t, 2, 10_000, 100)
	activation, err := fixture.authority.ActivateLease(fixture.activationRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fixture.authority.AuthenticateTURN(activation.ClientCredential.Username, "termx-relay", "source")
	if err := fixture.authority.ConfirmAllocation("source", "allocation", activation.ClientCredential.Username); err != nil {
		t.Fatal(err)
	}
	if err := fixture.authority.RecordTraffic("allocation", 700, 900); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(5 * time.Second)
	events, err := fixture.authority.DrainUsage("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].BytesUp != 700 || events[0].BytesDown != 900 || events[0].Sequence != 1 {
		t.Fatalf("usage events = %#v", events)
	}
	usageRing, _ := servicecredential.NewKeyRing(fixture.usageSigner.PublicKey())
	ledger, err := usage.NewLedger(usageRing, map[string]string{"relay-eu-1": fixture.usageSigner.KeyID()}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ledger.Apply(activation.Claims, events[0], fixture.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Aggregate.BytesUp != 700 || result.Aggregate.BytesDown != 900 {
		t.Fatalf("settled usage = %#v", result.Aggregate)
	}
	duplicate, err := ledger.Apply(activation.Claims, events[0], fixture.clock.Now())
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate settlement = %#v, %v", duplicate, err)
	}
	if next, err := fixture.authority.DrainUsage(""); err != nil || len(next) != 0 {
		t.Fatalf("empty drain = %#v, %v", next, err)
	}
}

func TestAuthorityRejectsWrongRelayBinding(t *testing.T) {
	fixture := newRelayFixture(t, 2, 10_000, 100)
	fixture.activationRequest.RouteID = "wrong-route"
	if _, err := fixture.authority.ActivateLease(fixture.activationRequest); !errors.Is(err, relay.ErrLeaseRejected) {
		t.Fatalf("wrong route error = %v", err)
	}
}

type relayFixture struct {
	clock             *fakeClock
	authority         *relay.Authority
	activationRequest relay.ActivationRequest
	usageSigner       servicecredential.Signer
}

func newRelayFixture(t *testing.T, maxConcurrency uint32, maxBytes uint64, maxBitrateKbps uint32) relayFixture {
	t.Helper()
	now := time.Date(2026, 7, 11, 11, 0, 0, 0, time.UTC)
	cpPrivate := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	cpSigner, err := servicecredential.NewSigner("cp-key", cpPrivate, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	leaseIssuer, _ := servicecredential.NewRelayLeaseIssuer("control-plane.test", cpSigner)
	lease, _, err := leaseIssuer.Issue(servicecredential.RelayLeaseRequest{
		LeaseID: "lease-1", AudienceRelayPool: "pool-eu", AccountID: "account-1", ManagedSessionID: "managed-1",
		ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", Region: "eu-west", PathKind: servicecredential.RelayPathSingle,
		TTL: 5 * time.Minute, MaxBytes: maxBytes, MaxBitrateKbps: maxBitrateKbps, MaxConcurrency: maxConcurrency, CredentialBindingID: "binding-1",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	usageSeed := bytes.Repeat([]byte{0x44}, ed25519.SeedSize)
	usageSigner, err := servicecredential.NewSigner("relay-usage-key", ed25519.NewKeyFromSeed(usageSeed), now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	keyRing, _ := servicecredential.NewKeyRing(cpSigner.PublicKey())
	clock := &fakeClock{now: now}
	authority, err := relay.NewAuthority(relay.Config{
		RelayID: "relay-eu-1", RelayPool: "pool-eu", Region: "eu-west", LeaseIssuer: "control-plane.test", Realm: "termx-relay",
		KeyRing: keyRing, Bindings: relay.StaticBindings{"binding-1": {"relay-eu-1": {}}}, CredentialSecret: bytes.Repeat([]byte{0x22}, 32),
		UsageSigner: usageSigner, Clock: clock, CredentialTTL: 3 * time.Minute, PendingAuthTTL: 5 * time.Second,
		NonceReader: bytes.NewReader(bytes.Repeat([]byte{0x33}, 1024)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return relayFixture{
		clock: clock, authority: authority, usageSigner: usageSigner,
		activationRequest: relay.ActivationRequest{SignedLease: lease.Bytes(), AccountID: "account-1", ManagedSessionID: "managed-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", PathKind: servicecredential.RelayPathSingle},
	}
}
