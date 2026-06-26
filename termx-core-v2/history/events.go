package history

import "time"

// HistoryEventKind 枚举 R303 干净模型接收的低层 history semantic event。这些
// 名字描述 domain transition，不描述 renderer rows、snapshot 或 raw PTY fallback。
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
// 消息链路：TerminalSemanticTransaction -> classifier/projector -> HistoryMutation。
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
