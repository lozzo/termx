package history

// HistoryLogicalRenderer 是 terminal semantic transaction 到 history mutation batch
// 的唯一转换层。真值来源只能是 ordered semantic ops、scroll-out proof、semantic
// frame payload 和 lifecycle reason。
type HistoryLogicalRenderer interface {
	// Apply 把一个 vterm semantic transaction 和 classifier decision 转成 history
	// mutation batch；实现不得读取 live surface state、TUI rows 或 raw PTY bytes。
	Apply(tx TerminalSemanticTransaction, decision HistoryDecision) (HistoryMutationBatch, error)
	// Close 处理不属于 vterm transaction 的 terminal/process lifecycle boundary，
	// 例如 process exit、terminal kill 或 daemon cleanup。
	Close(reason CloseReason) (HistoryMutationBatch, error)
}

// StreamLineReducer 负责 ordinary stream 与 primary scroll-out proof 转 logical
// lines。它必须覆盖 cursor、erase、scroll/copy rect 等 ordered ops，不能只按
// append-only 文本处理。
type StreamLineReducer interface {
	// ApplyOp 消费一个有序 terminal op，并返回对 open line/draft 的 mutation。
	ApplyOp(op TerminalSemanticOp) ([]HistoryMutation, error)
	// SealOpenLine 把当前 ordinary open line 按 lifecycle/terminal reason seal。
	SealOpenLine(reason SealReason) ([]HistoryMutation, error)
	// SealScrollOut 把 vterm 提供的 primary scroll-out proof seal 成 logical lines。
	SealScrollOut(proof TerminalSemanticScrollOut) ([]HistoryMutation, error)
}

// FrameReducer 负责 primary current、archived primary 和 alt transient frame
// journal。它只能消费 vterm semantic frame payload，禁止从 write ops 重建 frame。
type FrameReducer interface {
	// ReplacePrimaryCurrent 用同一 transaction 的 primary semantic frame 全量替换
	// current primary frame。
	ReplacePrimaryCurrent(frame TerminalSemanticFrame, reason FrameReason) ([]HistoryMutation, error)
	// ArchivePrimaryCurrent 把 current primary frame 按明确 boundary seal 到 frame journal。
	ArchivePrimaryCurrent(reason SealReason) ([]HistoryMutation, error)
	// ReplaceAltCurrent 用 alt semantic frame 全量替换 transient alt frame。
	ReplaceAltCurrent(frame TerminalSemanticFrame) ([]HistoryMutation, error)
	// ClearAltCurrent 在 alt exit 或 terminal close 时丢弃 transient frame。
	ClearAltCurrent(reason FrameReason) ([]HistoryMutation, error)
	// ApplyNonHistoryBoundary 处理 resize/full-replace 等不应 seal history 的
	// terminal boundary；实现只能推进 frame generation/cursor invalidation mutation。
	ApplyNonHistoryBoundary(reason FrameReason) ([]HistoryMutation, error)
	// ClosePrimaryCurrent 在 terminal/session close 时按 policy seal 或丢弃 current primary。
	ClosePrimaryCurrent(reason SealReason) ([]HistoryMutation, error)
}
