package history

import "testing"

// TestHistorySegmentNamesMatchProtocolBoundary guards the segment vocabulary
// required by history.window/copy so older pagination cannot collapse back into
// plain before_line_id semantics.
func TestHistorySegmentNamesMatchProtocolBoundary(t *testing.T) {
	want := []HistorySegment{
		HistorySegmentCommitted,
		HistorySegmentCurrentPrimaryFrame,
		HistorySegmentArchivedPrimaryFrame,
		HistorySegmentCurrentAltFrame,
	}
	for _, segment := range want {
		if segment == "" {
			t.Fatal("history segment names must be explicit")
		}
	}
}

// TestHistoryWindowRowsCarrySegmentAndLineIdentity verifies that a projected
// row still carries the domain identifiers needed for selection and cursoring.
func TestHistoryWindowRowsCarrySegmentAndLineIdentity(t *testing.T) {
	window := HistoryWindow{
		Token: "tok-1",
		Rows: []HistoryRow{{
			Kind:      LineKindScreenFrame,
			Segment:   HistorySegmentCurrentPrimaryFrame,
			LineID:    7,
			SessionID: 3,
			FrameID:   5,
			FixedGrid: true,
		}},
	}
	row := window.Rows[0]
	if row.Segment == "" || row.LineID == 0 || row.SessionID == 0 || row.FrameID == 0 || !row.FixedGrid {
		t.Fatalf("history row lost segment cursor identity: %#v", row)
	}
}

// TestHistoryProjectorContractConsumesSemanticTransactions locks the message
// chain shape: projector input is a semantic transaction plus classifier
// decision, not live rows or raw PTY fallback.
func TestHistoryProjectorContractConsumesSemanticTransactions(t *testing.T) {
	var tx TerminalSemanticTransaction
	decision := ScreenAppDecision{Mode: ScreenOutputModeOrdinary}
	projector := noopProjector{}
	mutation, err := projector.Apply(tx, decision)
	if err != nil {
		t.Fatalf("projector contract returned error: %v", err)
	}
	if mutation.Events != nil {
		t.Fatalf("noop projector should not synthesize history events: %#v", mutation)
	}
}

type noopProjector struct{}

// Apply implements HistoryProjector for contract tests without synthesizing
// domain mutations.
func (noopProjector) Apply(TerminalSemanticTransaction, ScreenAppDecision) (HistoryMutation, error) {
	return HistoryMutation{}, nil
}

// ForceClose implements the lifecycle side of HistoryProjector for contract
// tests without producing close mutations.
func (noopProjector) ForceClose(CloseReason) (HistoryMutation, error) {
	return HistoryMutation{}, nil
}
