// Package bindingkeys owns the Controller binding verification keyset revision and bounded publication window.
package bindingkeys

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RevisionStore persists the monotonic revision associated with one canonical binding keyset digest.
type RevisionStore interface {
	ReconcileBindingKeySet(context.Context, []byte, time.Time) (uint64, error)
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
	revision uint64
	keys     []*cloudv1.VerificationKey
	ttl      time.Duration
	now      func() time.Time
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
	keysetPayload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cloudv1.KeyBundle{Revision: 1, IssuedAt: timestamppb.New(time.Unix(1, 0).UTC()), ExpiresAt: timestamppb.New(time.Unix(2, 0).UTC()), Keys: canonical.GetKeys()})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(keysetPayload)
	revision, err := config.Store.ReconcileBindingKeySet(ctx, digest[:], now)
	if err != nil {
		return nil, err
	}
	if revision == 0 {
		return nil, errors.New("binding key revision store returned zero")
	}
	return &Owner{revision: revision, keys: canonical.GetKeys(), ttl: config.TTL, now: config.Now}, nil
}

// Bundle returns a fresh immutable publication window for the current persistent revision.
func (owner *Owner) Bundle() *cloudv1.KeyBundle {
	now := owner.now().UTC()
	return &cloudv1.KeyBundle{
		Revision: owner.revision, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(owner.ttl)),
		Keys: cloneKeys(owner.keys),
	}
}

func cloneKeys(keys []*cloudv1.VerificationKey) []*cloudv1.VerificationKey {
	result := make([]*cloudv1.VerificationKey, len(keys))
	for index, key := range keys {
		result[index] = proto.Clone(key).(*cloudv1.VerificationKey)
	}
	return result
}
