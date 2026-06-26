package history

// HistoryMutationKind enumerates the only domain changes a projector may send
// to the authoritative store. Adding a kind requires a harness that proves the
// truth source and cursor semantics.
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

// HistoryMutationEvent is one operation inside a projector transaction. It
// carries ids and payload references owned by history, never TUI rows or live
// surface snapshots.
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
