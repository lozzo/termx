package webcontroller

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
)

type proofRecord struct {
	subject         string
	actorKind       cloudpb.ManagementActorKind
	authenticatedAt time.Time
	expiresAt       time.Time
}

type proofStore struct {
	mu      sync.Mutex
	now     func() time.Time
	records map[[sha256.Size]byte]proofRecord
}

func newProofStore(now func() time.Time) *proofStore {
	if now == nil {
		now = time.Now
	}
	return &proofStore{now: now, records: make(map[[sha256.Size]byte]proofRecord)}
}

func (store *proofStore) issue(subject string, kind cloudpb.ManagementActorKind, ttl time.Duration) (string, proofRecord, error) {
	if subject == "" || kind == cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_UNSPECIFIED || ttl <= 0 {
		return "", proofRecord{}, fmt.Errorf("invalid proof")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", proofRecord{}, err
	}
	now := store.now().UTC()
	record := proofRecord{subject: subject, actorKind: kind, authenticatedAt: now, expiresAt: now.Add(ttl)}
	store.mu.Lock()
	store.cleanupLocked(now)
	store.records[sha256.Sum256(token)] = record
	store.mu.Unlock()
	encoded := base64.RawURLEncoding.EncodeToString(token)
	clear(token)
	return encoded, record, nil
}

func (store *proofStore) validate(encoded, subject string) (proofRecord, bool) {
	token, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(token) != 32 {
		return proofRecord{}, false
	}
	digest := sha256.Sum256(token)
	clear(token)
	now := store.now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cleanupLocked(now)
	record, ok := store.records[digest]
	return record, ok && subtle.ConstantTimeCompare([]byte(record.subject), []byte(subject)) == 1 && now.Before(record.expiresAt)
}

func (store *proofStore) revoke(encoded string) {
	token, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return
	}
	digest := sha256.Sum256(token)
	clear(token)
	store.mu.Lock()
	delete(store.records, digest)
	store.mu.Unlock()
}

func (store *proofStore) cleanupLocked(now time.Time) {
	for digest, record := range store.records {
		if !now.Before(record.expiresAt) {
			delete(store.records, digest)
		}
	}
}
