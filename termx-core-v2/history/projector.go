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
	// FlushOpenLine 在 transaction 末尾把仍可见的 ordinary open line 发布给 store。
	// 它只同步 renderer-owned mutable frontier，不 seal 历史；普通高压输出不能在
	// 每个 span 都 clone 整条 open line。
	FlushOpenLine() ([]HistoryMutation, error)
	// SealScrollOut 把 vterm 提供的 primary scroll-out proof seal 成 logical lines。
	SealScrollOut(proof TerminalSemanticScrollOut) ([]HistoryMutation, error)
	// ClearScreenOwnership 只清理 stream reducer 当前屏幕 ownership，不把已按 LF
	// seal 的普通行再次写入 history；ED2 clear-time proof 负责表达真正未封存内容。
	ClearScreenOwnership() ([]HistoryMutation, error)
	// ResetForClearScrollback 在 ED3 等明确 clear-scrollback boundary 后丢弃
	// renderer-owned ordinary mutable state。调用方仍需通过 mutation 清 store。
	ResetForClearScrollback()
}

// FrameReducer 负责 primary current、archived primary 和 alt transient frame
// journal。它只能消费 vterm semantic frame payload，禁止从 write ops 重建 frame。
type FrameReducer interface {
	// ReplacePrimaryCurrent 用同一 transaction 的 primary semantic frame 全量替换
	// current primary frame。
	ReplacePrimaryCurrent(frame TerminalSemanticFrame, reason FrameReason) ([]HistoryMutation, error)
	// ReplacePrimaryTouchedRows 只用本 transaction 明确触达的 rows 更新 current
	// primary frame。调用方必须从 ordered semantic ops 派生 touched rows；不能用
	// live snapshot 文本或程序名过滤，否则会把 shell tail 误当成 screen app truth。
	ReplacePrimaryTouchedRows(frame TerminalSemanticFrame, rows []int, reason FrameReason) ([]HistoryMutation, error)
	// ClosePrimaryCurrentFromFrameExcludingRows 使用普通输出 transaction 结束后的
	// vterm 当前屏幕 proof 收束 primary current frame，但只更新 current frame 已拥有的
	// rows，并排除本次普通 stream 触达的 rows。domain owner：history frame reducer；
	// truth source 是同一 terminal semantic transaction，失败条件是不能把 prompt 行或
	// 已 sealed shell tail 复制进 final screen-frame。
	ClosePrimaryCurrentFromFrameExcludingRows(frame TerminalSemanticFrame, excludedRows []int, reason SealReason) ([]HistoryMutation, error)
	// ArchivePrimaryCurrent 把 current primary frame 按明确 boundary seal 到 frame journal。
	ArchivePrimaryCurrent(reason SealReason) ([]HistoryMutation, error)
	// ClearPrimaryCurrent 丢弃 current primary frame ownership。它只用于 vterm 已经
	// 用 scroll-out proof 或 reset/clear boundary 表达内容去向时，避免把旧 frame
	// 再作为 archived truth 重复写入 timeline。
	ClearPrimaryCurrent(reason FrameReason) ([]HistoryMutation, error)
	// FilterPrimaryScrollOutRows 只保留属于当前 primary frame ownership 的 scroll-out
	// proof。它用于 ED2 clear-time proof：vterm 会给出整屏离开 viewport 的行，
	// 但其中可能混有已 sealed 的普通 shell 行，history 只能把 current frame
	// 拥有的 rows 写入 timeline。
	FilterPrimaryScrollOutRows(proofs []TerminalSemanticScrollOut) []TerminalSemanticScrollOut
	// ReplaceAltCurrent 用 alt semantic frame 全量替换 transient alt frame。
	ReplaceAltCurrent(frame TerminalSemanticFrame) ([]HistoryMutation, error)
	// ClearAltCurrent 在 alt exit 或 terminal close 时丢弃 transient frame。
	ClearAltCurrent(reason FrameReason) ([]HistoryMutation, error)
	// ApplyNonHistoryBoundary 处理 resize/full-replace 等不应 seal history 的
	// terminal boundary；实现只能推进 frame generation/cursor invalidation mutation。
	ApplyNonHistoryBoundary(reason FrameReason) ([]HistoryMutation, error)
	// ClosePrimaryCurrent 在 terminal/session close 时按 policy seal 或丢弃 current primary。
	ClosePrimaryCurrent(reason SealReason) ([]HistoryMutation, error)
	// ResetForClearScrollback 在 ED3 等明确 clear-scrollback boundary 后丢弃
	// renderer-owned frame journal 和 session state，避免下一次 redraw 沿用旧 session。
	ResetForClearScrollback()
}
