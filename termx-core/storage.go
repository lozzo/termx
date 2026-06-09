package termx

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	StorageOpPut    = "put"
	StorageOpDelete = "delete"
)

type StorageGetRequest struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
}

type StoragePutRequest struct {
	AppID           string
	Scope           StorageScope
	OwnerID         string
	Key             string
	Value           []byte
	CheckVersion    bool
	ExpectedVersion uint64
}

type StorageDeleteRequest struct {
	AppID           string
	Scope           StorageScope
	OwnerID         string
	Key             string
	CheckVersion    bool
	ExpectedVersion uint64
}

type StorageDeleteResult struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
	Deleted bool
	Version uint64
}

type StorageListRequest struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Prefix  string
}

type storageRecord struct {
	value     []byte
	version   uint64
	updatedAt time.Time
}

type storageKey struct {
	appID   string
	scope   StorageScope
	ownerID string
	key     string
}

type Storage struct {
	mu      sync.RWMutex
	entries map[storageKey]storageRecord
}

func NewStorage() *Storage {
	return &Storage{entries: make(map[storageKey]storageRecord)}
}

func (s *Storage) Get(req StorageGetRequest) (StorageEntry, error) {
	key, err := normalizeStorageKey(req.AppID, req.Scope, req.OwnerID, req.Key)
	if err != nil {
		return StorageEntry{}, err
	}
	s.mu.RLock()
	record, ok := s.entries[key]
	s.mu.RUnlock()
	if !ok {
		return StorageEntry{}, ErrNotFound
	}
	return storageEntryFromRecord(key, record), nil
}

func (s *Storage) Put(req StoragePutRequest) (StorageEntry, error) {
	key, err := normalizeStorageKey(req.AppID, req.Scope, req.OwnerID, req.Key)
	if err != nil {
		return StorageEntry{}, err
	}
	value := append([]byte(nil), req.Value...)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.entries[key]
	if req.CheckVersion {
		currentVersion := uint64(0)
		if exists {
			currentVersion = current.version
		}
		if currentVersion != req.ExpectedVersion {
			return StorageEntry{}, fmt.Errorf("%w: storage version mismatch: expected %d, got %d", ErrConflict, req.ExpectedVersion, currentVersion)
		}
	}
	version := current.version + 1
	if !exists {
		version = 1
	}
	record := storageRecord{value: value, version: version, updatedAt: now}
	s.entries[key] = record
	return storageEntryFromRecord(key, record), nil
}

func (s *Storage) Delete(req StorageDeleteRequest) (StorageDeleteResult, error) {
	key, err := normalizeStorageKey(req.AppID, req.Scope, req.OwnerID, req.Key)
	if err != nil {
		return StorageDeleteResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.entries[key]
	currentVersion := uint64(0)
	if exists {
		currentVersion = current.version
	}
	if req.CheckVersion && currentVersion != req.ExpectedVersion {
		return StorageDeleteResult{}, fmt.Errorf("%w: storage version mismatch: expected %d, got %d", ErrConflict, req.ExpectedVersion, currentVersion)
	}
	if exists {
		delete(s.entries, key)
		currentVersion++
	}
	return StorageDeleteResult{
		AppID:   key.appID,
		Scope:   key.scope,
		OwnerID: key.ownerID,
		Key:     key.key,
		Deleted: exists,
		Version: currentVersion,
	}, nil
}

func (s *Storage) List(req StorageListRequest) ([]StorageEntry, error) {
	filter, err := normalizeStorageListFilter(req.AppID, req.Scope, req.OwnerID, req.Prefix)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	out := make([]StorageEntry, 0)
	for key, record := range s.entries {
		if key.appID != filter.appID || key.scope != filter.scope || key.ownerID != filter.ownerID {
			continue
		}
		if filter.key != "" && !strings.HasPrefix(key.key, filter.key) {
			continue
		}
		out = append(out, storageEntryFromRecord(key, record))
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func normalizeStorageKey(appID string, scope StorageScope, ownerID string, key string) (storageKey, error) {
	base, err := normalizeStorageListFilter(appID, scope, ownerID, key)
	if err != nil {
		return storageKey{}, err
	}
	if base.key == "" {
		return storageKey{}, fmt.Errorf("%w: storage key is required", ErrInvalidCommand)
	}
	return base, nil
}

func normalizeStorageListFilter(appID string, scope StorageScope, ownerID string, key string) (storageKey, error) {
	appID = strings.TrimSpace(appID)
	ownerID = strings.TrimSpace(ownerID)
	key = strings.TrimSpace(key)
	if appID == "" {
		return storageKey{}, fmt.Errorf("%w: storage app_id is required", ErrInvalidCommand)
	}
	switch scope {
	case StorageScopePublic:
		ownerID = ""
	case StorageScopePrivate:
		if ownerID == "" {
			return storageKey{}, fmt.Errorf("%w: private storage owner_id is required", ErrPermissionDenied)
		}
	default:
		return storageKey{}, fmt.Errorf("%w: storage scope must be public or private", ErrInvalidCommand)
	}
	if strings.Contains(key, "\x00") {
		return storageKey{}, fmt.Errorf("%w: storage key contains NUL", ErrInvalidCommand)
	}
	return storageKey{appID: appID, scope: scope, ownerID: ownerID, key: key}, nil
}

func storageEntryFromRecord(key storageKey, record storageRecord) StorageEntry {
	return StorageEntry{
		AppID:     key.appID,
		Scope:     key.scope,
		OwnerID:   key.ownerID,
		Key:       key.key,
		Value:     append([]byte(nil), record.value...),
		Version:   record.version,
		UpdatedAt: record.updatedAt,
	}
}

func isStorageConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}
