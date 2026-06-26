package history

import "time"

// HistoryEventKind enumerates the low-level history semantic events accepted by
// the clean R303 model. These names describe domain transitions, not renderer
// rows, snapshots, or raw PTY fallback states.
type HistoryEventKind string

const (
	HistoryEventWritePrimaryCells        HistoryEventKind = "write-primary-cells"
	HistoryEventSealLogicalLine          HistoryEventKind = "seal-logical-line"
	HistoryEventMutateFrontier           HistoryEventKind = "mutate-frontier"
	HistoryEventResetFrontier            HistoryEventKind = "reset-frontier"
	HistoryEventCommitFrontier           HistoryEventKind = "commit-frontier"
	HistoryEventForceCommitFrontier      HistoryEventKind = "force-commit-frontier"
	HistoryEventReclaimCommittedSuffix   HistoryEventKind = "reclaim-committed-suffix"
	HistoryEventHideFrontier             HistoryEventKind = "hide-frontier"
	HistoryEventTruncateCommittedHistory HistoryEventKind = "truncate-committed-history"
	HistoryEventSwitchAltScreen          HistoryEventKind = "switch-alt-screen"
	HistoryEventNonHistoryBoundary       HistoryEventKind = "non-history-boundary"
)

// HistoryEvent 是进入 history projector/store 的唯一输入形状。
// message chain：TerminalSemanticTransaction -> classifier/projector -> HistoryMutation。
type HistoryEvent struct {
	Seq       uint64
	Kind      HistoryEventKind
	Time      time.Time
	LineID    LogicalLineID
	Cells     []Cell
	Frame     *ScreenFrame
	Size      TerminalSemanticSize
	Reason    string
	Operation TerminalSemanticOp
}
