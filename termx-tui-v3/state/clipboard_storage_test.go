package state

import "testing"

func TestClipboardStorageSnapshotRoundTripsEntries(t *testing.T) {
	store := ClipboardStore{}.WithCopiedText("alpha\r\nbeta").WithCopiedText("gamma")
	snapshot := SnapshotClipboardForStorage(store)
	value, err := EncodeClipboardStorageSnapshotValue(snapshot)
	if err != nil {
		t.Fatalf("encode clipboard snapshot: %v", err)
	}
	decoded, err := DecodeClipboardStorageSnapshot(value)
	if err != nil {
		t.Fatalf("decode clipboard snapshot: %v", err)
	}
	next, err := decoded.ToClipboardStore()
	if err != nil {
		t.Fatalf("snapshot to store: %v", err)
	}
	if len(next.Entries) != 2 || next.Entries[0].Text != "gamma" || next.Entries[1].Text != "alpha\nbeta" {
		t.Fatalf("unexpected clipboard entries %#v", next.Entries)
	}
}

func TestClipboardStoreReplaceAndDeleteEntry(t *testing.T) {
	store := ClipboardStore{}.WithCopiedText("alpha").WithCopiedText("beta")
	store = store.ReplaceEntryText(store.Entries[0].ID, "edited\nbeta")
	if store.Entries[0].ID != "clip:edited\nbeta" || store.Entries[0].Text != "edited\nbeta" || store.Entries[0].Title != "edited" || store.Entries[0].Preview != "edited …" {
		t.Fatalf("entry should be replaced with normalized projection, got %#v", store.Entries[0])
	}
	store = store.DeleteEntry(store.Entries[0].ID, store.Entries[0].Text)
	if len(store.Entries) != 1 || store.Entries[0].Text != "alpha" {
		t.Fatalf("entry should be deleted, got %#v", store.Entries)
	}
}
