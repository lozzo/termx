package devcloud

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/session"
)

const refreshSessionTTL = 30 * 24 * time.Hour

type refreshRecord struct {
	Hash         []byte       `json:"hash"`
	Kind         session.Kind `json:"kind"`
	AccountID    string       `json:"account_id"`
	AccountLabel string       `json:"account_label,omitempty"`
	DeviceID     string       `json:"device_id"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

type refreshStore struct {
	mu      sync.Mutex
	path    string
	random  io.Reader
	records map[[sha256.Size]byte]refreshRecord
}

func openRefreshStore(path string, random io.Reader, now time.Time) (*refreshStore, error) {
	store := &refreshStore{path: path, random: random, records: make(map[[sha256.Size]byte]refreshRecord)}
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read refresh session store: %w", err)
	}
	defer clear(data)
	var records []refreshRecord
	if json.Unmarshal(data, &records) != nil {
		return nil, fmt.Errorf("refresh session store is invalid")
	}
	for _, record := range records {
		if len(record.Hash) != sha256.Size || record.Kind != session.KindAccount && record.Kind != session.KindDevice || record.AccountID == "" || record.DeviceID == "" || !now.Before(record.ExpiresAt) {
			continue
		}
		var digest [sha256.Size]byte
		copy(digest[:], record.Hash)
		if _, exists := store.records[digest]; exists {
			return nil, fmt.Errorf("refresh session store contains duplicate hash")
		}
		record.Hash = nil
		store.records[digest] = record
	}
	return store, nil
}

func (store *refreshStore) Issue(record refreshRecord, now time.Time) ([]byte, time.Time, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cleanupLocked(now)
	return store.issueLocked(record, now)
}

func (store *refreshStore) Rotate(raw []byte, kind session.Kind, now time.Time) (refreshRecord, []byte, time.Time, error) {
	if len(raw) < 32 {
		return refreshRecord{}, nil, time.Time{}, fmt.Errorf("refresh session rejected")
	}
	digest := sha256.Sum256(raw)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cleanupLocked(now)
	record, ok := store.records[digest]
	if !ok || record.Kind != kind || !now.Before(record.ExpiresAt) {
		return refreshRecord{}, nil, time.Time{}, fmt.Errorf("refresh session rejected")
	}
	delete(store.records, digest)
	secret, expiresAt, err := store.issueLocked(record, now)
	if err != nil {
		store.records[digest] = record
		return refreshRecord{}, nil, time.Time{}, err
	}
	return record, secret, expiresAt, nil
}

func (store *refreshStore) cleanupLocked(now time.Time) {
	for digest, record := range store.records {
		if !now.Before(record.ExpiresAt) {
			delete(store.records, digest)
		}
	}
}

func (store *refreshStore) RevokeDevice(deviceID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	previous := make(map[[sha256.Size]byte]refreshRecord)
	for digest, record := range store.records {
		if record.DeviceID == deviceID {
			previous[digest] = record
			delete(store.records, digest)
		}
	}
	if err := store.persistLocked(); err != nil {
		for digest, record := range previous {
			store.records[digest] = record
		}
		return err
	}
	return nil
}

func (store *refreshStore) issueLocked(record refreshRecord, now time.Time) ([]byte, time.Time, error) {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(store.random, secret); err != nil {
		clear(secret)
		return nil, time.Time{}, err
	}
	digest := sha256.Sum256(secret)
	if _, exists := store.records[digest]; exists {
		clear(secret)
		return nil, time.Time{}, fmt.Errorf("refresh session collision")
	}
	record.Hash = nil
	record.ExpiresAt = now.Add(refreshSessionTTL).UTC()
	store.records[digest] = record
	if err := store.persistLocked(); err != nil {
		delete(store.records, digest)
		clear(secret)
		return nil, time.Time{}, err
	}
	return secret, record.ExpiresAt, nil
}

func (store *refreshStore) persistLocked() error {
	if store.path == "" {
		return nil
	}
	records := make([]refreshRecord, 0, len(store.records))
	for digest, record := range store.records {
		record.Hash = append([]byte(nil), digest[:]...)
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool { return string(records[left].Hash) < string(records[right].Hash) })
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".refresh-sessions-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(records); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, store.path); err != nil {
		return err
	}
	committed = true
	return nil
}
