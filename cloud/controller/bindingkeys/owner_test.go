package bindingkeys

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

type revisionStore struct {
	mu      sync.Mutex
	current Metadata
	history map[string]uint64
}

func (store *revisionStore) ReconcileBindingKeySet(_ context.Context, digest []byte, now time.Time, ttl time.Duration) (Metadata, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.history == nil {
		store.history = make(map[string]uint64)
	}
	if store.current.Revision == 0 {
		store.current = Metadata{KeySetSHA256: append([]byte(nil), digest...), Revision: 1, IssuedAt: now, ExpiresAt: now.Add(ttl)}
		store.history[string(digest)] = 1
	} else if bytes.Equal(store.current.KeySetSHA256, digest) {
		if !now.Before(store.current.IssuedAt.Add(store.current.ExpiresAt.Sub(store.current.IssuedAt) / 2)) {
			store.current.IssuedAt = now
			if candidate := now.Add(ttl); candidate.After(store.current.ExpiresAt) {
				store.current.ExpiresAt = candidate
			}
		}
	} else if _, exists := store.history[string(digest)]; exists {
		return Metadata{}, ErrKeySetReplay
	} else {
		store.current = Metadata{KeySetSHA256: append([]byte(nil), digest...), Revision: store.current.Revision + 1, IssuedAt: now, ExpiresAt: now.Add(ttl)}
		store.history[string(digest)] = store.current.Revision
	}
	result := store.current
	result.KeySetSHA256 = append([]byte(nil), result.KeySetSHA256...)
	return result, nil
}

func TestOwnerUsesDatabaseMetadataAndRejectsStaleKeyset(t *testing.T) {
	ctx := context.Background()
	store := &revisionStore{}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	config := Config{Store: store, TTL: time.Hour, Now: func() time.Time { return now }, Keys: []*cloudv1.VerificationKey{{KeyId: "key-a", Algorithm: "Ed25519", PublicKey: make([]byte, ed25519.PublicKeySize)}}}
	first, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	firstBundle, err := first.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restartedBundle, err := restarted.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(firstBundle, restartedBundle) || firstBundle.GetRevision() != 1 {
		t.Fatalf("same digest bundles differ: first=%v restarted=%v", firstBundle, restartedBundle)
	}

	config.Keys = []*cloudv1.VerificationKey{{KeyId: "key-b", Algorithm: "Ed25519", PublicKey: append(make([]byte, ed25519.PublicKeySize-1), 1)}}
	rotated, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	rotatedBundle, err := rotated.Bundle(ctx)
	if err != nil || rotatedBundle.GetRevision() != 2 {
		t.Fatalf("rotated bundle=%v err=%v", rotatedBundle, err)
	}
	if _, err := first.Bundle(ctx); !errors.Is(err, ErrKeySetReplay) {
		t.Fatalf("stale owner error=%v want replay", err)
	}
	if _, err := New(ctx, Config{Store: store, TTL: time.Hour, Now: func() time.Time { return now }, Keys: firstBundle.GetKeys()}); !errors.Is(err, ErrKeySetReplay) {
		t.Fatalf("A->B->A error=%v want replay", err)
	}
}

func TestOwnerRefreshesSameRevisionExpiryAtHalfLife(t *testing.T) {
	ctx := context.Background()
	store := &revisionStore{}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	config := Config{Store: store, TTL: time.Hour, Now: func() time.Time { return now }, Keys: []*cloudv1.VerificationKey{{KeyId: "key-a", Algorithm: "Ed25519", PublicKey: make([]byte, ed25519.PublicKeySize)}}}
	first, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	original, err := first.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30*time.Minute - time.Nanosecond)
	beforeBoundary, err := first.Bundle(ctx)
	if err != nil || !proto.Equal(original, beforeBoundary) {
		t.Fatalf("metadata changed before half-life: bundle=%v err=%v", beforeBoundary, err)
	}
	now = now.Add(time.Nanosecond)
	refreshed, err := first.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.GetRevision() != 1 || !refreshed.GetIssuedAt().AsTime().Equal(now) || !refreshed.GetExpiresAt().AsTime().Equal(now.Add(time.Hour)) {
		t.Fatalf("refreshed bundle=%v", refreshed)
	}
	second, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, err := second.Bundle(ctx)
	if err != nil || !proto.Equal(refreshed, secondBundle) {
		t.Fatalf("same digest metadata differs after refresh: second=%v err=%v", secondBundle, err)
	}
}

func TestOwnerNeverMovesSameRevisionExpiryBackward(t *testing.T) {
	ctx := context.Background()
	store := &revisionStore{}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	config := Config{Store: store, TTL: 2 * time.Hour, Now: func() time.Time { return now }, Keys: []*cloudv1.VerificationKey{{KeyId: "key-a", Algorithm: "Ed25519", PublicKey: make([]byte, ed25519.PublicKeySize)}}}
	owner, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	original, err := owner.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	owner.ttl = 15 * time.Minute
	refreshed, err := owner.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.GetRevision() != original.GetRevision() || refreshed.GetExpiresAt().AsTime().Before(original.GetExpiresAt().AsTime()) {
		t.Fatalf("same revision expiry moved backward: original=%v refreshed=%v", original, refreshed)
	}
}
