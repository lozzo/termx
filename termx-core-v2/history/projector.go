package history

// HistoryProjector 是 semantic tx 到 history mutation 的唯一转换层。
// truth source：ordered semantic ops、scroll-out proof、frame payload 和 lifecycle reason。
type HistoryProjector interface {
	// Apply converts one vterm semantic transaction plus classifier decision into
	// a history mutation. It must not inspect live surface state or raw PTY bytes.
	Apply(tx TerminalSemanticTransaction, decision ScreenAppDecision) (HistoryMutation, error)
	// ForceClose resolves process/terminal lifecycle boundaries that are not
	// vterm transactions, such as process exit or terminal removal.
	ForceClose(reason CloseReason) (HistoryMutation, error)
}
