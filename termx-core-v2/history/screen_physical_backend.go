package history

// ScreenPhysicalRowBackend 是 screen-backed history sealed physical rows 的
// residency/backend 边界。domain owner 仍是 ScreenHistoryBuffer：backend 只能按
// 已 seal 的 physical row 顺序 append、按 range 读取和恢复 row id/seq 元数据，
// 不能解释 terminal ops、不能决定 row 是否 mutable，也不能成为第二份 history truth。
type ScreenPhysicalRowBackend interface {
	AppendRows(rows []PhysicalRow) error
	Recover() (RecoveredScreenPhysicalRows, error)
	RowCount() int
	Rows(start int, end int) ([]PhysicalRow, error)
}

// RecoveredScreenPhysicalRows 是 screen-backed physical backend 的恢复元数据。
// Rows 不在恢复时整体 materialize；HistoryWindow 只能通过 Rows(start,end) 按
// cursor/window 范围读取，避免无限历史恢复成内存列表。
type RecoveredScreenPhysicalRows struct {
	RowCount   int
	NextRowID  uint64
	AppliedSeq uint64
}
