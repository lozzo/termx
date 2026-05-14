package protocol

import (
	"bytes"
	"testing"
	"time"
)

func TestStorageMethodCodecRoundTrip(t *testing.T) {
	put := StoragePutParams{
		AppID:           "tui",
		Scope:           StorageScopePrivate,
		OwnerID:         "client-a",
		Key:             "viewport",
		Value:           []byte("top=30"),
		CheckVersion:    true,
		ExpectedVersion: 7,
	}
	payload, err := EncodeMethodParams("storage.put", put)
	if err != nil {
		t.Fatalf("encode put: %v", err)
	}
	decoded, err := DecodeMethodParams("storage.put", payload)
	if err != nil {
		t.Fatalf("decode put: %v", err)
	}
	got, ok := decoded.(StoragePutParams)
	if !ok {
		t.Fatalf("decoded put as %T", decoded)
	}
	if got.AppID != put.AppID || got.Scope != put.Scope || got.OwnerID != put.OwnerID || got.Key != put.Key || !bytes.Equal(got.Value, put.Value) || !got.CheckVersion || got.ExpectedVersion != put.ExpectedVersion {
		t.Fatalf("unexpected put params: %#v", got)
	}

	entry := StorageEntry{
		AppID:     "tui",
		Scope:     StorageScopePublic,
		Key:       "locks/copy-mode-owner",
		Value:     []byte("view-a"),
		Version:   3,
		UpdatedAt: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
	}
	result, err := EncodeMethodResult("storage.get", entry)
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	var out StorageEntry
	if err := DecodeMethodResult("storage.get", result, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if out.AppID != entry.AppID || out.Scope != entry.Scope || out.Key != entry.Key || out.Version != entry.Version || !bytes.Equal(out.Value, entry.Value) || !out.UpdatedAt.Equal(entry.UpdatedAt) {
		t.Fatalf("unexpected result: %#v", out)
	}
}

func TestStorageEventCodecRoundTrip(t *testing.T) {
	event := Event{
		Type:      EventStorageChanged,
		Timestamp: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		Storage: &StorageChangedData{
			AppID:   "tui",
			Scope:   StorageScopePublic,
			Key:     "locks/copy-mode-owner",
			Version: 4,
			Op:      "put",
		},
	}
	payload, err := EncodeEventPayload(event)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	got, err := DecodeEventPayload(payload)
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if got.Type != EventStorageChanged || got.Storage == nil {
		t.Fatalf("unexpected decoded event: %#v", got)
	}
	if got.Storage.AppID != "tui" || got.Storage.Scope != StorageScopePublic || got.Storage.Key != "locks/copy-mode-owner" || got.Storage.Version != 4 || got.Storage.Op != "put" {
		t.Fatalf("unexpected storage event: %#v", got.Storage)
	}
}
