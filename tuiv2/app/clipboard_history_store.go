package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
)

const (
	clipboardHistoryStorageAppID  = "termx.clipboard"
	clipboardHistoryStoragePrefix = "history/"
	clipboardHistoryRecordVersion = 1
)

type clipboardHistoryStore interface {
	List(ctx context.Context) ([]clipboardHistoryEntry, error)
	Get(ctx context.Context, id string) (clipboardHistoryEntry, error)
	Put(ctx context.Context, entry clipboardHistoryEntry) error
	Delete(ctx context.Context, id string) error
}

type storageClipboardHistoryStore struct {
	client bridge.StorageClient
}

type clipboardHistoryRecord struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Text          string    `json:"text"`
	Preview       string    `json:"preview,omitempty"`
	PaneID        string    `json:"pane_id,omitempty"`
	SourceApp     string    `json:"source_app,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func newClipboardHistoryStoreFromClient(client any) clipboardHistoryStore {
	storageClient, ok := client.(bridge.StorageClient)
	if !ok || storageClient == nil {
		return nil
	}
	return &storageClipboardHistoryStore{client: storageClient}
}

func (s *storageClipboardHistoryStore) List(ctx context.Context) ([]clipboardHistoryEntry, error) {
	entries, err := s.loadAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(entries) > clipboardHistoryLimit {
		entries = entries[:clipboardHistoryLimit]
	}
	return entries, nil
}

func (s *storageClipboardHistoryStore) Get(ctx context.Context, id string) (clipboardHistoryEntry, error) {
	if s == nil || s.client == nil {
		return clipboardHistoryEntry{}, nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return clipboardHistoryEntry{}, nil
	}
	stored, err := s.client.StorageGet(ctx, protocol.StorageGetParams{
		AppID: clipboardHistoryStorageAppID,
		Scope: protocol.StorageScopePublic,
		Key:   clipboardHistoryStorageKey(id),
	})
	if err != nil {
		return clipboardHistoryEntry{}, err
	}
	if stored == nil {
		return clipboardHistoryEntry{}, fmt.Errorf("clipboard history entry %q not found", id)
	}
	entry, ok := decodeClipboardHistoryEntry(*stored)
	if !ok {
		return clipboardHistoryEntry{}, fmt.Errorf("clipboard history entry %q is invalid", id)
	}
	return entry, nil
}

func (s *storageClipboardHistoryStore) Put(ctx context.Context, entry clipboardHistoryEntry) error {
	if s == nil || s.client == nil {
		return nil
	}
	entry = normalizeClipboardHistoryEntry(entry)
	if entry.ID == "" || entry.Text == "" {
		return nil
	}
	value, err := json.Marshal(clipboardHistoryRecord{
		SchemaVersion: clipboardHistoryRecordVersion,
		ID:            entry.ID,
		Text:          entry.Text,
		Preview:       entry.Preview,
		PaneID:        entry.PaneID,
		SourceApp:     "tuiv2",
		CreatedAt:     entry.CreatedAt,
	})
	if err != nil {
		return err
	}
	_, err = s.client.StoragePut(ctx, protocol.StoragePutParams{
		AppID: clipboardHistoryStorageAppID,
		Scope: protocol.StorageScopePublic,
		Key:   clipboardHistoryStorageKey(entry.ID),
		Value: value,
	})
	if err != nil {
		return err
	}
	s.prune(ctx)
	return nil
}

func (s *storageClipboardHistoryStore) Delete(ctx context.Context, id string) error {
	if s == nil || s.client == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	_, err := s.client.StorageDelete(ctx, protocol.StorageDeleteParams{
		AppID: clipboardHistoryStorageAppID,
		Scope: protocol.StorageScopePublic,
		Key:   clipboardHistoryStorageKey(id),
	})
	return err
}

func (s *storageClipboardHistoryStore) prune(ctx context.Context) {
	entries, err := s.loadAll(ctx)
	if err != nil || len(entries) <= clipboardHistoryLimit {
		return
	}
	for _, entry := range entries[clipboardHistoryLimit:] {
		if entry.ID == "" {
			continue
		}
		_, _ = s.client.StorageDelete(ctx, protocol.StorageDeleteParams{
			AppID: clipboardHistoryStorageAppID,
			Scope: protocol.StorageScopePublic,
			Key:   clipboardHistoryStorageKey(entry.ID),
		})
	}
}

func (s *storageClipboardHistoryStore) loadAll(ctx context.Context) ([]clipboardHistoryEntry, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	result, err := s.client.StorageList(ctx, protocol.StorageListParams{
		AppID:  clipboardHistoryStorageAppID,
		Scope:  protocol.StorageScopePublic,
		Prefix: clipboardHistoryStoragePrefix,
	})
	if err != nil {
		return nil, err
	}
	entries := make([]clipboardHistoryEntry, 0, len(result.Entries))
	for _, stored := range result.Entries {
		entry, ok := decodeClipboardHistoryEntry(stored)
		if ok {
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return entries, nil
}

func decodeClipboardHistoryEntry(stored protocol.StorageEntry) (clipboardHistoryEntry, bool) {
	var record clipboardHistoryRecord
	if err := json.Unmarshal(stored.Value, &record); err != nil {
		return clipboardHistoryEntry{}, false
	}
	if record.SchemaVersion != clipboardHistoryRecordVersion || record.Text == "" {
		return clipboardHistoryEntry{}, false
	}
	id := strings.TrimSpace(record.ID)
	if id == "" {
		id = strings.TrimPrefix(stored.Key, clipboardHistoryStoragePrefix)
	}
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = stored.UpdatedAt
	}
	entry := normalizeClipboardHistoryEntry(clipboardHistoryEntry{
		ID:        id,
		Text:      record.Text,
		Preview:   record.Preview,
		PaneID:    record.PaneID,
		CreatedAt: createdAt,
	})
	return entry, entry.ID != "" && entry.Text != ""
}

func clipboardHistoryStorageKey(id string) string {
	return clipboardHistoryStoragePrefix + strings.TrimSpace(id)
}
