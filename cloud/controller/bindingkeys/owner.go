// Package bindingkeys owns the Controller binding verification keyset revision and bounded publication window.
package bindingkeys

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ErrKeySetReplay is returned when a keyset that is no longer current is presented again.
var ErrKeySetReplay = errors.New("historical binding keyset replay")

// Metadata is the database-owned publication identity and validity window for the current keyset.
type Metadata struct {
	KeySetSHA256 []byte
	Revision     uint64
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

// RevisionStore persists digest history and the complete current bundle metadata.
type RevisionStore interface {
	ReconcileBindingKeySet(context.Context, []byte, time.Time, time.Duration) (Metadata, error)
}

// Config is the complete, narrow Controller keyset owner configuration.
type Config struct {
	Store RevisionStore
	Keys  []*cloudv1.VerificationKey
	TTL   time.Duration
	Now   func() time.Time
}

// Owner publishes fresh bounded bundles while keeping the persistent keyset revision stable.
type Owner struct {
	store  RevisionStore
	digest []byte
	keys   []*cloudv1.VerificationKey
	ttl    time.Duration
	now    func() time.Time

	ownershipLost atomic.Bool
	handlerMu     sync.Mutex
	lostHandler   func()
	notifyOnce    sync.Once
}

// New validates and canonicalizes the keyset before reconciling its persistent revision.
func New(ctx context.Context, config Config) (*Owner, error) {
	if config.Store == nil || config.TTL <= 0 || config.TTL > ticket.MaxKeyBundleTTL {
		return nil, errors.New("binding key revision store and TTL up to 24 hours are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	now := config.Now().UTC()
	prototype := &cloudv1.KeyBundle{Revision: 1, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(config.TTL)), Keys: config.Keys}
	canonical, _, err := ticket.CanonicalKeyBundle(prototype)
	if err != nil {
		return nil, err
	}
	keysetPayload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cloudv1.KeyBundle{Keys: canonical.GetKeys()})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(keysetPayload)
	owner := &Owner{store: config.Store, digest: append([]byte(nil), digest[:]...), keys: canonical.GetKeys(), ttl: config.TTL, now: config.Now}
	if _, err := owner.Bundle(ctx); err != nil {
		return nil, err
	}
	return owner, nil
}

// Bundle verifies that this Owner's digest is still current and returns database-owned metadata.
func (owner *Owner) Bundle(ctx context.Context) (*cloudv1.KeyBundle, error) {
	if owner.ownershipLost.Load() {
		return nil, ErrKeySetReplay
	}
	metadata, err := owner.store.ReconcileBindingKeySet(ctx, owner.digest, owner.now().UTC(), owner.ttl)
	if err != nil {
		if errors.Is(err, ErrKeySetReplay) {
			owner.markOwnershipLost()
		}
		return nil, err
	}
	if owner.ownershipLost.Load() {
		return nil, ErrKeySetReplay
	}
	if !bytes.Equal(metadata.KeySetSHA256, owner.digest) {
		return nil, errors.New("binding key revision store returned a different current digest")
	}
	bundle := &cloudv1.KeyBundle{
		Revision: metadata.Revision, IssuedAt: timestamppb.New(metadata.IssuedAt), ExpiresAt: timestamppb.New(metadata.ExpiresAt),
		Keys: cloneKeys(owner.keys),
	}
	canonical, _, err := ticket.CanonicalKeyBundle(bundle)
	if err != nil {
		return nil, fmt.Errorf("binding key revision store returned invalid metadata: %w", err)
	}
	return canonical, nil
}

// SetOwnershipLostHandler registers the process-level readiness callback.
// A loss detected before registration is delivered synchronously during registration.
func (owner *Owner) SetOwnershipLostHandler(handler func()) {
	if handler == nil {
		return
	}
	owner.handlerMu.Lock()
	if owner.lostHandler == nil {
		owner.lostHandler = handler
	}
	lost := owner.ownershipLost.Load()
	owner.handlerMu.Unlock()
	if lost {
		owner.notifyOwnershipLost()
	}
}

func (owner *Owner) markOwnershipLost() {
	if owner.ownershipLost.CompareAndSwap(false, true) {
		owner.notifyOwnershipLost()
	}
}

func (owner *Owner) notifyOwnershipLost() {
	owner.handlerMu.Lock()
	handler := owner.lostHandler
	owner.handlerMu.Unlock()
	if handler != nil {
		owner.notifyOnce.Do(handler)
	}
}

func cloneKeys(keys []*cloudv1.VerificationKey) []*cloudv1.VerificationKey {
	result := make([]*cloudv1.VerificationKey, len(keys))
	for index, key := range keys {
		result[index] = proto.Clone(key).(*cloudv1.VerificationKey)
	}
	return result
}
