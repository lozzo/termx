package identity

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersistsAndReusesIdentity(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreate(dir, "alpha")
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if first.DeviceID == "" {
		t.Fatal("expected device id to be generated")
	}
	if first.DisplayName != "alpha" {
		t.Fatalf("expected display name alpha, got %q", first.DisplayName)
	}

	second, err := LoadOrCreate(dir, "")
	if err != nil {
		t.Fatalf("LoadOrCreate second call returned error: %v", err)
	}
	if second.DeviceID != first.DeviceID {
		t.Fatalf("expected same device id, got %q != %q", second.DeviceID, first.DeviceID)
	}
	if second.DisplayName != first.DisplayName {
		t.Fatalf("expected display name to persist, got %q != %q", second.DisplayName, first.DisplayName)
	}
}

func TestLoadOrCreateUpdatesDisplayNameWhenRequested(t *testing.T) {
	dir := t.TempDir()

	original, err := LoadOrCreate(dir, "alpha")
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	updated, err := LoadOrCreate(dir, "beta")
	if err != nil {
		t.Fatalf("LoadOrCreate second call returned error: %v", err)
	}
	if updated.DeviceID != original.DeviceID {
		t.Fatalf("expected same device id, got %q != %q", updated.DeviceID, original.DeviceID)
	}
	if updated.DisplayName != "beta" {
		t.Fatalf("expected updated display name beta, got %q", updated.DisplayName)
	}
	if _, err := LoadOrCreate(filepath.Join(dir, "nested"), "gamma"); err != nil {
		t.Fatalf("expected nested dir creation to succeed: %v", err)
	}
}
