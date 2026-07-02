package history

// LogicalLineStore 拥有 logical-line payload truth。timeline、journal record 和
// storage backend 可以引用它的 id，但不能复制成第二份内容 truth。
type LogicalLineStore interface {
	// Put 把新的 logical line payload 写入 authoritative store。
	Put(line LogicalLine) error
	// Get 在 logical line 仍被保留时返回它的当前 payload。
	Get(id LogicalLineID) (LogicalLine, bool)
	// Update 替换已有 logical line 的 payload；调用方暴露新 history window 前必须
	// bump 对应 generation。
	Update(line LogicalLine) error
	// Delete 只能在 sealed timeline、open line 和 frame journal 都不再引用 payload
	// 后删除它。
	Delete(id LogicalLineID) error
}

// SealedTimelineIndex 拥有 sealed records 的顺序。它只保存 record 引用和
// cursor boundary，payload 仍留在 logical line/frame store。
type SealedTimelineIndex interface {
	// Append 把已经由 store 拥有的 record 加入 sealed timeline order。
	Append(record HistoryRecord) error
	// RemoveRange 为 truncate 或 retention 从 timeline 删除完整 records，但不能
	// 修改 active open line 或 current frame payload。
	RemoveRange(first HistoryRecordID, last HistoryRecordID) error
	// Boundary 返回当前 segment-aware timeline cursor boundary。
	Boundary() HistoryBoundary
}

// OpenLineTracker 跟踪 ordinary stream 当前仍可修改的 logical line。它是 state
// boundary，不是第二份 payload store；screen app frame 不得复用它。
type OpenLineTracker interface {
	// Current 在 ordinary stream 存在 active open line 时返回它。
	Current() (OpenLine, bool)
	// Replace 用 renderer 产出的 draft 替换当前 open line。
	Replace(line OpenLine) error
	// Clear 在 open line seal/drop 后清理 current ownership。
	Clear(reason SealReason) error
}

// ScreenFrameJournal 索引 current 和 archived fixed-grid frames。frame payload
// lines 仍然存放在 LogicalLineStore。
type ScreenFrameJournal interface {
	// ReplacePrimaryCurrent 替换对应 session 的 current primary frame，不把前一次
	// repaint 写成 timeline record。
	ReplacePrimaryCurrent(frame MutableFrame) error
	// ArchivePrimary 记录 alt enter 等明确 boundary frame。
	ArchivePrimary(frame SealedFrame) error
	// ReplaceAltCurrent 替换 transient alt frame，不写 primary timeline。
	ReplaceAltCurrent(frame TransientFrame) error
	// ClearAltCurrent 在 alt exit 或 terminal close 时清 transient frame。
	ClearAltCurrent(reason FrameReason) error
	// CurrentPrimary 在 session 存在 active current frame 时返回最新 frame。
	CurrentPrimary(sessionID ScreenSessionID) (MutableFrame, bool)
	// Older 先遍历 timeline 中的 frame records，再回到 ordinary sealed lines；
	// cursor segment 必须保持显式。
	Older(cursor HistoryCursor, limit int) ([]FrameRecord, HistoryCursor, error)
}

// StorageBackend 只负责驻留位置和恢复流程；可变性策略属于 HistoryState/store。
type StorageBackend interface {
	// Apply 为一个 history generation 原子持久化或更新 store/index/journal
	// residency records。
	Apply(tx StorageTransaction) error
	// Recover 重建 logical store、sealed timeline、open line 和 frame journal metadata，
	// 不能 replay raw PTY bytes，也不能读取 live snapshot。
	Recover() (RecoveredHistoryState, error)
	// Compact 回收不再被任何 authoritative history structure 引用的 storage records。
	Compact(policy StorageCompactionPolicy) error
}

// LinePayloadBackend 是 R341 后 sealed timeline 分页使用的按 id 读取接口。
// domain owner 仍是 HistoryStore：backend 只按 id 返回 payload，不能判断某条
// line 是否 sealed、mutable、可见或属于哪个 window。
type LinePayloadBackend interface {
	// GetLine 返回指定 logical line payload。缺失返回 false，调用方必须按
	// authoritative index 决定是否跳过或报错。
	GetLine(id LogicalLineID) (LogicalLine, bool)
	// GetLines 按输入 id 顺序返回 payload。它服务 cursor window/copy，不得把
	// 整个 backend materialize 成 rows。
	GetLines(ids []LogicalLineID) ([]LogicalLine, error)
}

// StorageTransaction 是 history generation update 的 storage-layer 视图；它不能
// 决定 persisted line 是否 mutable。
type StorageTransaction struct {
	Generation Generation
	Lines      []LogicalLine
	Tombstones []LogicalLineID
	Timeline   []HistoryRecord
	Mutable    []LogicalLineID
	Frames     []FrameRecord
}

// RecoveredHistoryState 是恢复 history truth 所需的完整状态。只恢复 row payload
// 不足以满足 infinite history 语义。
type RecoveredHistoryState struct {
	Generation Generation
	Lines      []LogicalLine
	Timeline   []HistoryRecord
	Mutable    []LogicalLineID
	Frames     []FrameRecord
}

// StorageCompactionPolicy 限制 backend cleanup。它是 storage concern，不决定哪些
// history content 可见。
type StorageCompactionPolicy struct {
	MaxFrames int
	MaxBytes  int64
}

// HistoryStore 是权威 history query/copy 的外部契约。mutation-backed legacy store
// 通过 Apply 接收 renderer batch；screen-backed store 的正文 truth 来自同一
// terminal 的 ScreenHistoryBuffer，Apply 只能作为兼容边界，不能恢复第二份 truth。
type HistoryStore interface {
	// Apply 把 renderer batch 应用到 mutation-backed store；screen-backed store
	// 可以忽略该 batch，因为写 truth 的入口是 ScreenHistoryBuffer physical rows。
	Apply(batch HistoryMutationBatch) error
	// ReadState 返回 classifier 所需的只读 history ownership 边界。调用方只能用它
	// 判断 semantic transaction 的消费路径，不能从中派生 payload 或 UI rows。
	ReadState() HistoryReadState
	// LatestWindow 返回某个 terminal 的 authoritative latest projection。
	LatestWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// OlderWindow 使用上一响应中的 cursor truth 跨 current/archive/sealed segments
	// 分页。
	OlderWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// OldestWindow 从 authoritative projection 的最老 head 直接返回 replace window。
	// 它服务 TUI copy mode `g` 跳转，不能复用 older/prepend 合同，否则 consumer 会把
	// response 当成增量页而不是新的可见窗口 truth。
	OldestWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// NewerWindow 使用 cursor truth 向最新方向分页。
	NewerWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// Freeze 创建 tokenized copy/history boundary，后续 repaint 不能改写它。
	Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error)
	// Copy 从 authoritative history payload 返回选中文本。
	Copy(req HistoryCopyRequest) (string, error)
	// Release 释放 frozen token 或其它 history window resource。
	Release(token HistoryToken) error
}
