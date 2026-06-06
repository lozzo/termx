package state

import "testing"

func TestRootAdvance(t *testing.T) {
	next := (Root{}).Advance()
	if next.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", next.Generation)
	}
}

func TestWorkbenchSyncStoreTracksSavedEventAndAppliedVersions(t *testing.T) {
	ref := DefaultWorkbenchStorageRef("").WithVersion(3)
	store := (WorkbenchSyncStore{}).MarkSaved(ref, 3).MarkEvent(4).MarkApplied(4)
	if store.Ref.Version != 3 || store.LastSavedVersion != 3 || store.LastEventVersion != 4 || store.LastAppliedVersion != 4 {
		t.Fatalf("unexpected sync store %#v", store)
	}
	if !store.ShouldIgnoreEvent(3) || store.ShouldIgnoreEvent(4) {
		t.Fatalf("unexpected self-event filtering %#v", store)
	}
}

func TestRootContainsReducerOwnedShellStore(t *testing.T) {
	root := Root{Shell: DefaultShell()}
	if root.Shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("expected root shell active pane, got %#v", root.Shell)
	}
}
