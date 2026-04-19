package shared

import "testing"

func TestIsEastAsianAmbiguousWidthCluster(t *testing.T) {
	if !IsEastAsianAmbiguousWidthCluster("§") {
		t.Fatal("expected section sign to be treated as East Asian ambiguous width")
	}
	if IsEastAsianAmbiguousWidthCluster("A") {
		t.Fatal("expected plain ASCII not to be treated as East Asian ambiguous width")
	}
}

func TestIsHostWidthAmbiguousCluster(t *testing.T) {
	if !IsHostWidthAmbiguousCluster("♻️", 2) {
		t.Fatal("expected FE0F emoji variation sequence to be treated as host-width ambiguous")
	}
	if !IsHostWidthAmbiguousCluster("é", 1) {
		t.Fatal("expected East Asian ambiguous-width text to be treated as host-width ambiguous")
	}
	if IsHostWidthAmbiguousCluster("ok", 2) {
		t.Fatal("expected ordinary ASCII text not to be treated as host-width ambiguous")
	}
}

func TestIsPrintableZeroWidthCluster(t *testing.T) {
	if !IsPrintableZeroWidthCluster("\u00ad") {
		t.Fatal("expected soft hyphen to be treated as a printable zero-width cluster")
	}
	if IsPrintableZeroWidthCluster("A") {
		t.Fatal("expected ASCII not to be treated as a printable zero-width cluster")
	}
}

func TestWidthSafetyForTerminalCell(t *testing.T) {
	decision := WidthSafetyForTerminalCell("é", 1)
	if !decision.HostWidthStabilizer {
		t.Fatalf("expected East Asian ambiguous-width cell to request host width stabilizer, got %#v", decision)
	}
	if decision.AmbiguousCompensation {
		t.Fatalf("expected plain ambiguous-width text not to request FE0F compensation, got %#v", decision)
	}
}

func TestWidthSafetyForDisplayedClusterTracksAmbiguousEmojiCompensation(t *testing.T) {
	decision := WidthSafetyForDisplayedCluster("♻️", 2)
	if !decision.AmbiguousCompensation {
		t.Fatalf("expected FE0F cluster to request ambiguous compensation, got %#v", decision)
	}
	if !decision.HostWidthStabilizer {
		t.Fatalf("expected FE0F cluster to request host width stabilizer, got %#v", decision)
	}
	if !decision.NeedsHiddenCompensation(1) {
		t.Fatalf("expected single-cell erase to preserve hidden ambiguous compensation, got %#v", decision)
	}
	if decision.NeedsHiddenCompensation(2) {
		t.Fatalf("expected multi-cell erase not to mark hidden compensation, got %#v", decision)
	}
}

func TestWidthSafetyTrackerCarriesHostWidthReanchorForward(t *testing.T) {
	var tracker WidthSafetyTracker

	transition := tracker.ObserveDisplayedCluster("é", 1)
	if transition.ReanchorBefore {
		t.Fatalf("expected first visible cluster not to start with reanchor, got %#v", transition)
	}

	transition = tracker.ObserveReanchorBeforeNextCluster()
	if !transition.HostWidthStabilizer {
		t.Fatalf("expected ambiguous-width cluster to mark host-width stabilizer, got %#v", transition)
	}

	transition = tracker.ObserveDisplayedCluster("X", 1)
	if !transition.ReanchorBefore {
		t.Fatalf("expected next visible cluster to inherit reanchor, got %#v", transition)
	}
}

func TestWidthSafetyTrackerMarksSingleCellHiddenCompensation(t *testing.T) {
	var tracker WidthSafetyTracker

	tracker.ObserveDisplayedCluster("♻️", 2)
	transition := tracker.ObserveErase(1)
	if !transition.HiddenCompensation {
		t.Fatalf("expected single-cell erase to preserve hidden compensation, got %#v", transition)
	}
	if transition.ReanchorBefore {
		t.Fatalf("expected erase without prior CHA not to invent reanchor, got %#v", transition)
	}
}

func TestWidthSafetyTrackerDoesNotOvermarkOrdinaryReanchor(t *testing.T) {
	var tracker WidthSafetyTracker

	tracker.ObserveDisplayedCluster("A", 1)
	transition := tracker.ObserveReanchorBeforeNextCluster()
	if transition.HostWidthStabilizer {
		t.Fatalf("expected ordinary reanchor not to be treated as host-width stabilizer, got %#v", transition)
	}

	transition = tracker.ObserveDisplayedCluster("B", 1)
	if !transition.ReanchorBefore {
		t.Fatalf("expected explicit CHA to still reanchor the next visible cluster, got %#v", transition)
	}
}
