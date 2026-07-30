package bindingkeys

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

type revisionStore struct {
	digest   []byte
	revision uint64
}

func (store *revisionStore) ReconcileBindingKeySet(_ context.Context, digest []byte, _ time.Time) (uint64, error) {
	if store.revision == 0 {
		store.revision = 1
		store.digest = append([]byte(nil), digest...)
	} else if !bytes.Equal(store.digest, digest) {
		store.revision++
		store.digest = append(store.digest[:0], digest...)
	}
	return store.revision, nil
}

func TestOwnerPersistsStableRevisionAndIncrementsForKeyChange(t *testing.T) {
	store := &revisionStore{}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	config := Config{Store: store, TTL: time.Hour, Now: func() time.Time { return now }, Keys: []*cloudv1.VerificationKey{{KeyId: "key-a", Algorithm: "Ed25519", PublicKey: make([]byte, ed25519.PublicKeySize)}}}
	first, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if first.Bundle().GetRevision() != 1 || restarted.Bundle().GetRevision() != 1 {
		t.Fatal("unchanged keyset did not retain its persistent revision")
	}
	config.Keys[0] = &cloudv1.VerificationKey{KeyId: "key-b", Algorithm: "Ed25519", PublicKey: append(make([]byte, ed25519.PublicKeySize-1), 1)}
	rotated, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Bundle().GetRevision() != 2 {
		t.Fatalf("rotated revision=%d want=2", rotated.Bundle().GetRevision())
	}
	now = now.Add(10 * time.Minute)
	refreshed := rotated.Bundle()
	if refreshed.GetRevision() != 2 || !refreshed.GetIssuedAt().AsTime().Equal(now) || !refreshed.GetExpiresAt().AsTime().Equal(now.Add(time.Hour)) {
		t.Fatalf("refreshed bundle=%v", refreshed)
	}
}
