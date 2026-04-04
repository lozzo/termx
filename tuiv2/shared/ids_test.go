package shared

import "testing"

func TestGenerateShortID_NonEmpty(t *testing.T) {
	id := GenerateShortID()
	if id == "" {
		t.Fatal("GenerateShortID returned empty string")
	}
}

func TestGenerateShortID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := GenerateShortID()
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestSequentialIDSource_Prefix(t *testing.T) {
	src := NewSequentialIDSource()
	id1 := src.Next("tab-")
	id2 := src.Next("tab-")
	if id1 == id2 {
		t.Fatalf("expected different IDs, got %q twice", id1)
	}
	if id1 != "tab-1" {
		t.Errorf("expected 'tab-1', got %q", id1)
	}
	if id2 != "tab-2" {
		t.Errorf("expected 'tab-2', got %q", id2)
	}
}

func TestObserveScopedIDAdvancesCounter(t *testing.T) {
	scope := "test-observe"
	ObserveScopedID(scope, "41")
	if got := NextScopedID(scope); got != "42" {
		t.Fatalf("expected observed scope to continue at 42, got %q", got)
	}
}

func TestLessNumericStrings(t *testing.T) {
	if !LessNumericStrings("2", "10") {
		t.Fatal("expected numeric ordering for plain numeric IDs")
	}
	if !LessNumericStrings("pane-2", "pane-10") {
		t.Fatal("expected numeric ordering for legacy prefixed numeric IDs")
	}
	if LessNumericStrings("abc", "10") {
		t.Fatal("expected numeric IDs to sort ahead of non-numeric IDs only when compared from left")
	}
}
