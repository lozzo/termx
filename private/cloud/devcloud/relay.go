package devcloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
)

const controlRelayUsagePath = "/v1/internal/relay/usage"

type relaySessionState struct {
	signedLease []byte
	claims      servicecredential.RelayLeaseClaims
}

type relayControlState struct {
	mu      sync.Mutex
	usageMu sync.Mutex

	leaseIssuer  servicecredential.RelayLeaseIssuer
	leaseKeyRing *servicecredential.KeyRing
	authority    *cloudrelay.Authority
	usageLedger  *usage.Ledger
	sessions     map[string]relaySessionState
	usageOutbox  *cloudrelay.UsageOutbox
	url          string
}

func (state *serviceState) initializeRelay(now time.Time) error {
	_, leasePrivateKey, err := ed25519.GenerateKey(state.random)
	if err != nil {
		return fmt.Errorf("generate dev Relay lease signing key: %w", err)
	}
	leaseSigner, err := servicecredential.NewSigner("dev-relay-lease-key", leasePrivateKey, now.Add(-time.Minute), now.Add(24*time.Hour))
	clear(leasePrivateKey)
	if err != nil {
		return err
	}
	leaseIssuer, err := servicecredential.NewRelayLeaseIssuer(devRelayLeaseIssuer, leaseSigner)
	if err != nil {
		return err
	}
	leaseKeyRing, err := servicecredential.NewKeyRing(leaseSigner.PublicKey())
	if err != nil {
		return err
	}

	_, usagePrivateKey, err := ed25519.GenerateKey(state.random)
	if err != nil {
		return fmt.Errorf("generate dev Relay usage signing key: %w", err)
	}
	usageSigner, err := servicecredential.NewSigner("dev-relay-usage-key", usagePrivateKey, now.Add(-time.Minute), now.Add(24*time.Hour))
	clear(usagePrivateKey)
	if err != nil {
		return err
	}
	usageKeyRing, err := servicecredential.NewKeyRing(usageSigner.PublicKey())
	if err != nil {
		return err
	}
	usageLedger, err := usage.NewLedger(usageKeyRing, map[string]string{devRelayID: usageSigner.KeyID()}, time.Minute)
	if err != nil {
		return err
	}
	usageOutbox, err := cloudrelay.NewUsageOutbox(state.usageOutboxPath)
	if err != nil {
		return err
	}

	credentialSecret, err := state.randomBytes(32)
	if err != nil {
		return err
	}
	authority, err := cloudrelay.NewAuthority(cloudrelay.Config{
		RelayID: devRelayID, RelayPool: devRelayPool, Region: devRegion, LeaseIssuer: devRelayLeaseIssuer,
		Realm: devRelayRealm, KeyRing: leaseKeyRing,
		Bindings:         cloudrelay.StaticBindings{devRelayBindingID: {devRelayID: {}}},
		CredentialSecret: credentialSecret, UsageSigner: usageSigner, Clock: runtimeClock{now: state.now},
		CredentialTTL: relayLeaseTTL, PendingAuthTTL: 10 * time.Second, NonceReader: state.random,
	})
	clear(credentialSecret)
	if err != nil {
		return err
	}
	state.relayControl = &relayControlState{
		leaseIssuer: leaseIssuer, leaseKeyRing: leaseKeyRing,
		authority: authority, usageLedger: usageLedger, usageOutbox: usageOutbox,
		sessions: make(map[string]relaySessionState),
	}
	return nil
}

func (state *serviceState) acquireSingleRelay(accountID, managedSessionID, clientDeviceID, targetDeviceID string) (relaySessionState, cloudrelay.Activation, error) {
	control := state.relayControl
	if control == nil {
		return relaySessionState{}, cloudrelay.Activation{}, fmt.Errorf("dev Relay is not configured")
	}
	now := state.now().UTC()
	budget, err := state.edgeAuth.RelayBudget(accountID)
	if err != nil {
		return relaySessionState{}, cloudrelay.Activation{}, err
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	for sessionID, existing := range control.sessions {
		if !now.Before(time.Unix(existing.claims.ExpiresAtUnix, 0)) {
			clear(existing.signedLease)
			delete(control.sessions, sessionID)
		}
	}

	// ManagedSession 是两端领取同一签名 lease 的复用真值；client/daemon credential 由 Relay Authority 分别派生。
	current, ok := control.sessions[managedSessionID]
	if ok && !now.Before(time.Unix(current.claims.ExpiresAtUnix, 0)) {
		clear(current.signedLease)
		delete(control.sessions, managedSessionID)
		ok = false
	}
	if !ok {
		if uint32(len(control.sessions)) >= budget.MaxConcurrency {
			return relaySessionState{}, cloudrelay.Activation{}, fmt.Errorf("regional Relay budget exhausted")
		}
		leaseID, err := state.randomID("relay-lease")
		if err != nil {
			return relaySessionState{}, cloudrelay.Activation{}, err
		}
		lease, claims, err := control.leaseIssuer.Issue(servicecredential.RelayLeaseRequest{LeaseID: leaseID, AudienceRelayPool: devRelayPool, AccountID: accountID, ManagedSessionID: managedSessionID, ClientDeviceID: clientDeviceID, TargetDeviceID: targetDeviceID, Region: devRegion, PathKind: servicecredential.RelayPathSingle, TTL: min(budget.MaxLeaseDuration, relayLeaseTTL), MaxBytes: budget.MaxBytes, MaxBitrateKbps: budget.MaxBitrateKbps, MaxConcurrency: budget.MaxConcurrency, CredentialBindingID: devRelayBindingID}, now)
		if err != nil {
			return relaySessionState{}, cloudrelay.Activation{}, err
		}
		current = relaySessionState{signedLease: lease.Bytes(), claims: claims}
		control.sessions[managedSessionID] = current
	}
	activation, err := control.authority.ActivateLease(cloudrelay.ActivationRequest{
		SignedLease: current.signedLease, AccountID: accountID, ManagedSessionID: managedSessionID,
		ClientDeviceID: clientDeviceID, TargetDeviceID: targetDeviceID,
		PathKind: servicecredential.RelayPathSingle,
	})
	if err != nil {
		return relaySessionState{}, cloudrelay.Activation{}, err
	}
	current.signedLease = append([]byte(nil), current.signedLease...)
	return current, activation, nil
}

func (state *serviceState) relayLeaseClaims(leaseID string) (servicecredential.RelayLeaseClaims, bool) {
	control := state.relayControl
	if control == nil {
		return servicecredential.RelayLeaseClaims{}, false
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	for _, current := range control.sessions {
		if current.claims.LeaseID == leaseID {
			return current.claims, true
		}
	}
	return servicecredential.RelayLeaseClaims{}, false
}

func (state *serviceState) relayUsageRecord(event usage.Event) (cloudrelay.UsageRecord, bool) {
	control := state.relayControl
	if control == nil {
		return cloudrelay.UsageRecord{}, false
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	for _, current := range control.sessions {
		if current.claims.LeaseID == event.LeaseID {
			return cloudrelay.UsageRecord{SignedLease: append([]byte(nil), current.signedLease...), Event: event}, true
		}
	}
	return cloudrelay.UsageRecord{}, false
}

func (runtime *Runtime) reportRelayUsage() {
	runtime.waitGroup.Add(1)
	go func() {
		defer runtime.waitGroup.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runtime.usageStop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				_ = runtime.flushRelayUsage(ctx, "")
				cancel()
			}
		}
	}()
}

func (runtime *Runtime) flushRelayUsage(ctx context.Context, terminationReason string) error {
	if runtime == nil || runtime.state == nil || runtime.state.relayControl == nil {
		return nil
	}
	control := runtime.state.relayControl
	control.usageMu.Lock()
	defer control.usageMu.Unlock()
	events, err := control.authority.DrainUsage(terminationReason)
	if err != nil {
		return err
	}
	if len(events) > 0 {
		records := make([]cloudrelay.UsageRecord, 0, len(events))
		for _, event := range events {
			record, ok := runtime.state.relayUsageRecord(event)
			if !ok {
				return fmt.Errorf("Relay usage lease is unavailable")
			}
			records = append(records, record)
		}
		if err := control.usageOutbox.Enqueue(records...); err != nil {
			return err
		}
	}
	// 只有 Control Plane 明确确认后才移除事件；响应丢失时重发同一签名 event，由 ledger 幂等收敛。
	pending, err := control.usageOutbox.Pending()
	if err != nil {
		return err
	}
	for _, record := range pending {
		if err := runtime.postRelayUsage(ctx, record); err != nil {
			return err
		}
		if err := control.usageOutbox.Ack(record.Event.EventID, record.Event.Sequence); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) postRelayUsage(ctx context.Context, record cloudrelay.UsageRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode dev Relay usage: %w", err)
	}
	defer clear(payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, runtime.manifest.ControlPlaneURL+controlRelayUsagePath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create dev Relay usage request: %w", err)
	}
	request.Header.Set("Content-Type", httpapi.JSONMediaType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("report dev Relay usage: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("report dev Relay usage: Control Plane returned status %d", response.StatusCode)
	}
	return nil
}
