package history

// LogicalLineStore 拥有 logical-line payload truth。index、journal record 和
// storage backend 可以引用它的 id，但不能复制成第二份 truth。
type LogicalLineStore interface {
	// Put 把新的 logical line payload 写入 authoritative store。
	Put(line LogicalLine) error
	// Get 在 logical line 仍被保留时返回它的当前 payload。
	Get(id LogicalLineID) (LogicalLine, bool)
	// Update 替换已有 logical line 的 payload；调用方暴露新 history window 前必须
	// bump 对应 generation。
	Update(line LogicalLine) error
	// Delete 只能在 committed index、frontier 和 frame journal 都不再引用 payload
	// 后删除它。
	Delete(id LogicalLineID) error
}

// CommittedHistoryIndex 拥有 ordinary/final-frame ordering。它只保存 id 和 cursor
// boundary，payload 仍留在 LogicalLineStore。
type CommittedHistoryIndex interface {
	// Append 把已经由 store 拥有的 logical line id 加入 committed history order。
	Append(id LogicalLineID) error
	// RemoveRange 为 truncate 或 retention 从 committed order 删除完整 logical line，
	// 但不能修改 active frontier payload。
	RemoveRange(first LogicalLineID, last LogicalLineID) error
	// Boundary 返回当前 segment-aware committed cursor boundary。
	Boundary() HistoryBoundary
}

// MutableFrontier 跟踪仍由当前 primary history semantics 拥有的 logical lines。
// 它是 index/state boundary，不是第二份 payload store。
type MutableFrontier interface {
	// OpenLine 在 terminal semantics 仍拥有 active append line 时返回该 line。
	OpenLine() (LogicalLineID, bool)
	// Reclaim 把完整 committed suffix lines 移回 mutable ownership。
	Reclaim(ids []LogicalLineID) error
	// Hide 把仍可变的 visible lines 移入 hidden frontier ownership。
	Hide(ids []LogicalLineID) error
	// Seal 标记一条 line 不再接受 append write，但它仍可能处于 primary screen
	// ownership 下。
	Seal(id LogicalLineID) error
}

// ScreenFrameJournal 索引 current 和 archived fixed-grid frames。frame payload
// lines 仍然存放在 LogicalLineStore。
type ScreenFrameJournal interface {
	// PublishCurrent 替换对应 session 或 transient alt segment 的 current frame，
	// 不把前一次 repaint ordinary commit。
	PublishCurrent(frame ScreenFrame) error
	// Archive 记录 alt enter 等明确 boundary frame。
	Archive(record FrameRecord) error
	// Current 在 session 存在 active current frame 时返回最新 frame。
	Current(sessionID ScreenSessionID) (ScreenFrame, bool)
	// Older 先遍历 archived frame records，再回到 ordinary committed history；
	// cursor segment 必须保持显式。
	Older(cursor HistoryCursor, limit int) ([]FrameRecord, HistoryCursor, error)
}

// StorageBackend 只负责驻留位置和恢复流程；可变性策略属于 store/projector。
type StorageBackend interface {
	// Apply 为一个 history generation 原子持久化或更新 store/index/journal
	// residency records。
	Apply(tx StorageTransaction) error
	// Recover 重建 logical store、committed index、frontier 和 frame journal metadata，
	// 不能 replay raw PTY bytes，也不能读取 live snapshot。
	Recover() (RecoveredHistoryState, error)
	// Compact 回收不再被任何 authoritative history structure 引用的 storage records。
	Compact(policy StorageCompactionPolicy) error
}

// StorageTransaction 是 history generation update 的 storage-layer 视图；它不能
// 决定 persisted line 是否 mutable。
type StorageTransaction struct {
	Generation Generation
	Lines      []LogicalLine
	Tombstones []LogicalLineID
	Committed  []LogicalLineID
	Frames     []FrameRecord
}

// RecoveredHistoryState 是恢复 history truth 所需的完整状态。只恢复 row payload
// 不足以满足 infinite history 语义。
type RecoveredHistoryState struct {
	Generation Generation
	Lines      []LogicalLine
	Committed  []LogicalLineID
	Frontier   []LogicalLineID
	Frames     []FrameRecord
}

// StorageCompactionPolicy 限制 backend cleanup。它是 storage concern，不决定哪些
// history content 可见。
type StorageCompactionPolicy struct {
	MaxFrames int
	MaxBytes  int64
}

// InfiniteHistoryStore 是权威 history truth 的外部契约。
type InfiniteHistoryStore interface {
	// ApplyMutation 把 projector transaction 作为唯一写路径应用到 authoritative
	// history truth。
	ApplyMutation(mutation HistoryMutation) error
	// ApplyOrdinaryEvent 为 focused harness 应用低层 ordinary event；R303 完成后
	// production 应优先走 projector mutations。
	ApplyOrdinaryEvent(event HistoryEvent) error

	// OpenScreenSession 创建由 history 拥有的 primary screen app session state。
	OpenScreenSession(params ScreenSessionParams) (ScreenSessionID, error)
	// PublishPrimaryFrame 发布 current primary fixed-grid content，但不增加
	// ordinary history depth。
	PublishPrimaryFrame(session ScreenSessionID, frame ScreenFrame) error
	// ArchivePrimaryFrame 记录 alt enter 或 retention policy 等明确 primary frame
	// boundary。
	ArchivePrimaryFrame(session ScreenSessionID, frame ScreenFrame, reason ArchiveReason) error
	// PublishAltFrame 把 current alt-screen content 发布为可选择的 transient state，
	// 不做 ordinary commit。
	PublishAltFrame(frame ScreenFrame) error
	// CloseScreenSession 按 close policy 处理 active session state。
	CloseScreenSession(session ScreenSessionID, policy ClosePolicy) error

	// LatestWindow 返回某个 terminal 的 authoritative latest projection。
	LatestWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// OlderWindow 使用上一响应中的 cursor truth 跨 current/archive/committed segments
	// 分页。
	OlderWindow(req HistoryWindowRequest) (HistoryWindow, error)
	// Freeze 创建 tokenized copy/history boundary，后续 repaint 不能改写它。
	Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error)
	// Copy 从 authoritative history payload 返回选中文本。
	Copy(req HistoryCopyRequest) (string, error)
	// Release 释放 frozen token 或其它 history window resource。
	Release(token HistoryToken) error
}
