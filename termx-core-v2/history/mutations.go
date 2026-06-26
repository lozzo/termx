package history

// HistoryMutationKind 枚举 HistoryLogicalRenderer 能发送给 authoritative store 的
// 唯一 domain change 集合。新增 kind 必须先补 harness 证明 truth source、ordered
// semantic 覆盖和 cursor 语义。
type HistoryMutationKind string

const (
	HistoryMutationUpsertOpenLine       HistoryMutationKind = "upsert-open-line"
	HistoryMutationSealLine             HistoryMutationKind = "seal-line"
	HistoryMutationAppendTimelineRecord HistoryMutationKind = "append-timeline-record"
	HistoryMutationReplacePrimaryFrame  HistoryMutationKind = "replace-primary-frame"
	HistoryMutationArchivePrimaryFrame  HistoryMutationKind = "archive-primary-frame"
	HistoryMutationReplaceAltFrame      HistoryMutationKind = "replace-alt-frame"
	HistoryMutationClearAltFrame        HistoryMutationKind = "clear-alt-frame"
	HistoryMutationClosePrimaryFrame    HistoryMutationKind = "close-primary-frame"
	HistoryMutationNonHistoryBoundary   HistoryMutationKind = "non-history-boundary"
	HistoryMutationClearScrollback      HistoryMutationKind = "clear-scrollback"
)

// HistoryMutationBatch 是 renderer 输出到 store 的事务边界。一个 batch 对应一个
// terminal semantic transaction 或一个 lifecycle close，store 必须原子应用。
type HistoryMutationBatch struct {
	Seq        uint64
	Generation Generation
	Mutations  []HistoryMutation
}

// HistoryMutation 是 renderer 输出到 store 的单步 domain change；它只能表达
// history-owned logical line、timeline 和 frame journal 变化。
type HistoryMutation struct {
	Kind      HistoryMutationKind
	LineIDs   []LogicalLineID
	Line      *LogicalLine
	OpenLine  *OpenLine
	Record    *HistoryRecord
	Frame     *ScreenFrame
	Mutable   *MutableFrame
	Sealed    *SealedFrame
	Transient *TransientFrame
	SessionID ScreenSessionID
	FrameID   ScreenFrameID
	Reason    SealReason
	Close     CloseReason
	Decision  HistoryDecision
}
