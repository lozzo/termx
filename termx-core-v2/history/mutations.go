package history

// HistoryMutationKind 枚举 projector 能发送给 authoritative store 的唯一 domain
// change 集合。新增 kind 必须先补 harness 证明 truth source 和 cursor 语义。
type HistoryMutationKind string

const (
	HistoryMutationOrdinaryCommit      HistoryMutationKind = "ordinary-commit"
	HistoryMutationFrontierMutate      HistoryMutationKind = "frontier-mutate"
	HistoryMutationOpenScreenSession   HistoryMutationKind = "open-screen-session"
	HistoryMutationPublishPrimaryFrame HistoryMutationKind = "publish-primary-frame"
	HistoryMutationArchivePrimaryFrame HistoryMutationKind = "archive-primary-frame"
	HistoryMutationPublishAltFrame     HistoryMutationKind = "publish-alt-frame"
	HistoryMutationCloseScreenSession  HistoryMutationKind = "close-screen-session"
	HistoryMutationCommitFinalFrame    HistoryMutationKind = "commit-final-frame"
	HistoryMutationNonHistoryBoundary  HistoryMutationKind = "non-history-boundary"
)

// HistoryMutation 是 projector 输出到 store 的事务；它只能表达 history domain 变化。
type HistoryMutation struct {
	Seq        uint64
	Generation Generation
	Events     []HistoryMutationEvent
}

// HistoryMutationEvent 表示 projector transaction 内的一步操作。它只能携带
// history 拥有的 id 和 payload 引用，不能携带 TUI rows 或 live surface snapshot。
type HistoryMutationEvent struct {
	Kind          HistoryMutationKind
	LineIDs       []LogicalLineID
	Line          *LogicalLine
	Frame         *ScreenFrame
	SessionID     ScreenSessionID
	FrameID       ScreenFrameID
	ArchiveReason ArchiveReason
	ClosePolicy   ClosePolicy
	CloseReason   CloseReason
	Decision      ScreenAppDecision
}
