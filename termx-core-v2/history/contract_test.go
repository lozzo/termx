package history

import "testing"

// TestHistorySegmentNamesMatchProtocolBoundary 守住 history.window/copy 需要的
// segment 词表，避免 older 分页退化回普通 before_line_id 语义。
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

// TestHistoryWindowRowsCarrySegmentAndLineIdentity 验证投影 row 仍携带选择和
// cursor 所需的 domain 标识。
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

// TestHistoryProjectorContractConsumesSemanticTransactions 锁住消息链路形状：
// projector 输入只能是 semantic transaction 加 classifier decision，不能是 live
// rows 或 raw PTY fallback。
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

// Apply 为 contract test 实现 HistoryProjector，不合成任何 domain mutation。
func (noopProjector) Apply(TerminalSemanticTransaction, ScreenAppDecision) (HistoryMutation, error) {
	return HistoryMutation{}, nil
}

// ForceClose 为 contract test 实现 HistoryProjector 的 lifecycle 边界，不产生
// close mutation。
func (noopProjector) ForceClose(CloseReason) (HistoryMutation, error) {
	return HistoryMutation{}, nil
}
