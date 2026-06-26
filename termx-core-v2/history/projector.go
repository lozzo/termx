package history

// HistoryProjector 是 semantic tx 到 history mutation 的唯一转换层。
// 真值来源是 ordered semantic ops、scroll-out proof、frame payload 和 lifecycle reason。
type HistoryProjector interface {
	// Apply 把一个 vterm semantic transaction 和 classifier decision 转成 history
	// mutation；实现不得读取 live surface state 或 raw PTY bytes。
	Apply(tx TerminalSemanticTransaction, decision ScreenAppDecision) (HistoryMutation, error)
	// ForceClose 处理不属于 vterm transaction 的 process/terminal lifecycle
	// boundary，例如 process exit 或 terminal removal。
	ForceClose(reason CloseReason) (HistoryMutation, error)
}
