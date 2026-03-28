package tui

import "testing"

func TestTerminalStoreReturnsSameTerminalForSameID(t *testing.T) {
	store := NewTerminalStore()

	first := store.GetOrCreate("term-1")
	second := store.GetOrCreate("term-1")

	if first == nil || second == nil {
		t.Fatal("expected terminal objects")
	}
	if first != second {
		t.Fatal("expected same pointer for same terminal id")
	}
}

func TestTerminalStoreDeleteRemovesTerminal(t *testing.T) {
	store := NewTerminalStore()
	store.GetOrCreate("term-1")

	store.Delete("term-1")

	if store.Get("term-1") != nil {
		t.Fatal("expected terminal to be removed from store")
	}
}
