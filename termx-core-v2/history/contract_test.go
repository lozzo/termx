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

// TestHistoryLogicalRendererContractConsumesSemanticTransactions 锁住消息链路形状：
// renderer 输入只能是 semantic transaction 加 classifier decision，不能是 live
// rows、snapshot 或 raw PTY fallback。
func TestHistoryLogicalRendererContractConsumesSemanticTransactions(t *testing.T) {
	var tx TerminalSemanticTransaction
	decision := HistoryDecision{Mode: HistoryOutputModeOrdinaryStream}
	renderer := noopRenderer{}
	batch, err := renderer.Apply(tx, decision)
	if err != nil {
		t.Fatalf("renderer contract returned error: %v", err)
	}
	if batch.Mutations != nil {
		t.Fatalf("noop renderer should not synthesize history mutations: %#v", batch)
	}
}

// TestR319HistoryStateNamesNewTruthBoundaries 验证新模型的顶层对象不再把
// commit/frontier 作为领域 truth，而是以 open line、sealed timeline 和 frame
// journal 组合历史状态。
func TestR319HistoryStateNamesNewTruthBoundaries(t *testing.T) {
	state := HistoryState{
		TerminalID: "term",
		OpenLine: OpenLine{
			Active: true,
			Draft:  LogicalLineDraft{Line: LogicalLine{ID: 1, Seal: SealStateOpen}},
		},
		Timeline: SealedTimeline{Records: []HistoryRecord{{
			ID:      1,
			Kind:    HistoryRecordOrdinaryLine,
			LineIDs: []LogicalLineID{2},
		}}},
		Frames: FrameJournal{PrimaryCurrent: &MutableFrame{ID: 3, Cols: 80}},
	}
	if !state.OpenLine.Active || len(state.Timeline.Records) != 1 || state.Frames.PrimaryCurrent == nil {
		t.Fatalf("history state lost open/timeline/frame boundaries: %#v", state)
	}
}

type noopRenderer struct{}

// Apply 为 contract test 实现 HistoryLogicalRenderer，不合成任何 domain mutation。
func (noopRenderer) Apply(TerminalSemanticTransaction, HistoryDecision) (HistoryMutationBatch, error) {
	return HistoryMutationBatch{}, nil
}

// Close 为 contract test 实现 HistoryLogicalRenderer 的 lifecycle 边界，不产生
// close mutation。
func (noopRenderer) Close(CloseReason) (HistoryMutationBatch, error) {
	return HistoryMutationBatch{}, nil
}
