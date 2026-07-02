package history

import "fmt"

// RowOwnerKind 描述 physical row 当前由哪个 terminal/history 领域拥有。
// 它是 ScreenHistoryBuffer 的内部 ownership 语义，不能被 TUI 或 protocol
// 当作 pane/view 状态；后续 projection 只能把它转换成 HistoryWindow 的来源标记。
type RowOwnerKind int

const (
	// RowOwnerUnknown 表示一行尚未被明确内容 owner 接管，通常是新建空行。
	RowOwnerUnknown RowOwnerKind = iota
	// RowOwnerShellStream 表示普通 primary stream 正在写入该 physical row。
	RowOwnerShellStream
	// RowOwnerPrimaryFrame 表示 primary pseudo-TUI/current frame 拥有该行。
	RowOwnerPrimaryFrame
	// RowOwnerAltScreen 表示该行属于 alt-screen transient，不得污染 primary history。
	RowOwnerAltScreen
	// RowOwnerSystem 表示该行由 reset/resize/boundary 等系统语义创建。
	RowOwnerSystem
)

// PhysicalRow 是 screen-backed history 的正文真值单元。
// ID 在 row 生命周期内稳定；Version 只在该 row 被 screen mutation 修改时递增；
// Sealed 表示该 row 已按 scroll-out/boundary/lifecycle 离开 current screen。
// 同一个 ID 不允许 seal 两次，projection 必须使用 ID 做去重而不是比较文本。
type PhysicalRow struct {
	ID        uint64
	Cells     []Cell
	Wrapped   bool
	Continued bool
	Version   uint64
	OwnerSeq  uint64
	OwnerKind RowOwnerKind
	FrameID   uint64
	Sealed    bool
}

// Text 返回 physical row 当前 cells 的 plain text 投影。
// 它只用于测试、诊断和 debug dump；正确性不能用文本相等代替 RowID identity。
func (row PhysicalRow) Text() string {
	return rowText(row.Cells)
}

// CursorState 保存 ScreenHistoryBuffer 的 primary cursor。
// 它属于 core history screen model，不是 vterm live cursor 或 TUI copy cursor。
type CursorState struct {
	X      int
	Y      int
	SavedX int
	SavedY int
}

// MarginState 保存 scroll region 边界。R420 只初始化全屏 margin；
// DECSTBM 和局部滚动语义在 R421 中接入，不能在本阶段被静默当作全屏滚动完成。
type MarginState struct {
	Top    int
	Bottom int
	Left   int
	Right  int
}

// ScreenGrid 保存一组按屏幕行排列的 physical rows。
// Rows 中每个 row 都有稳定 RowID；滚动时移动 row identity，而不是复制文本。
type ScreenGrid struct {
	Rows []PhysicalRow
}

// ScreenHistoryBuffer 是 screen-backed history ingest 的 authoritative state。
// 消息链路是 vterm TerminalSemanticTransaction -> ScreenHistoryBuffer；它只消费
// terminal semantic ops，不读取 raw PTY、live SurfaceTrack、TUI rows 或旧 logical
// history。Committed 保存已经 seal 的 primary physical rows。
type ScreenHistoryBuffer struct {
	Cols int
	Rows int

	Main  *ScreenGrid
	Alt   *ScreenGrid
	InAlt bool

	Cursor  CursorState
	Margins MarginState

	NextRowID  uint64
	AppliedSeq uint64
	Committed  []PhysicalRow

	sealedRowIDs map[uint64]struct{}
}

// NewScreenHistoryBuffer 创建一个独立的 screen-backed history buffer。
// 调用边界：R420 只用于 domain harness；接入 Terminal/HistoryStore 前不得把它
// 和旧 journal/frame reducer 并联成两份 production truth。
func NewScreenHistoryBuffer(cols int, rows int) *ScreenHistoryBuffer {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	buffer := &ScreenHistoryBuffer{
		Cols:         cols,
		Rows:         rows,
		NextRowID:    1,
		sealedRowIDs: make(map[uint64]struct{}),
	}
	buffer.Main = buffer.newGrid(RowOwnerShellStream)
	buffer.Alt = buffer.newGrid(RowOwnerAltScreen)
	buffer.Margins = MarginState{Top: 0, Bottom: rows, Left: 0, Right: cols}
	return buffer
}

func (buffer *ScreenHistoryBuffer) newGrid(owner RowOwnerKind) *ScreenGrid {
	grid := &ScreenGrid{Rows: make([]PhysicalRow, buffer.Rows)}
	for index := range grid.Rows {
		grid.Rows[index] = buffer.newPhysicalRow(owner, 0)
	}
	return grid
}

func (buffer *ScreenHistoryBuffer) newPhysicalRow(owner RowOwnerKind, ownerSeq uint64) PhysicalRow {
	id := buffer.NextRowID
	buffer.NextRowID++
	return PhysicalRow{
		ID:        id,
		Version:   1,
		OwnerSeq:  ownerSeq,
		OwnerKind: owner,
	}
}

// ApplyTransaction 将一个 vterm semantic transaction 应用到 screen buffer。
// Seq 已经处理过时必须 no-op，避免同一 PTY 语义被重复 seal；Seq 为 0 的测试
// transaction 不参与幂等边界。R420 只覆盖基础 op，复杂控制序列由 R421 扩展。
func (buffer *ScreenHistoryBuffer) ApplyTransaction(tx TerminalSemanticTransaction) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	if tx.Seq > 0 && tx.Seq <= buffer.AppliedSeq {
		return nil
	}
	for _, op := range tx.Ops {
		if err := buffer.ApplyOp(op, tx.Seq); err != nil {
			return err
		}
	}
	if tx.Seq > buffer.AppliedSeq {
		buffer.AppliedSeq = tx.Seq
	}
	return nil
}

// ApplyOp 应用一条已排序 terminal semantic op。
// R420 仅支持 printable WriteSpan 与 CR/LF/BS/TAB 基础控制；未支持 op 会返回错误，
// 防止未来 harness 误以为复杂控制序列已经被 screen model 正确消费。
func (buffer *ScreenHistoryBuffer) ApplyOp(op TerminalSemanticOp, seq uint64) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	switch op.Code {
	case vtermScreenOpWriteSpan():
		return buffer.writeSpan(op, seq)
	case vtermScreenOpControl():
		return buffer.applyControl(op, seq)
	default:
		return fmt.Errorf("screen history buffer unsupported op code %d", op.Code)
	}
}

// VisibleRows 返回当前 main screen rows 的副本。
// 调用方只能用于 domain harness 或后续 projection，不能修改返回值来更新 truth。
func (buffer *ScreenHistoryBuffer) VisibleRows() []PhysicalRow {
	if buffer == nil || buffer.Main == nil {
		return nil
	}
	return clonePhysicalRows(buffer.Main.Rows)
}

// CommittedRows 返回已经 seal 的 primary physical rows 副本。
// 它是 screen-backed committed history 的低层 payload，后续 HistoryWindow 仍需
// 通过 projection 暴露 logical rows。
func (buffer *ScreenHistoryBuffer) CommittedRows() []PhysicalRow {
	if buffer == nil {
		return nil
	}
	return clonePhysicalRows(buffer.Committed)
}

func (buffer *ScreenHistoryBuffer) ensure() {
	if buffer.Cols <= 0 {
		buffer.Cols = 80
	}
	if buffer.Rows <= 0 {
		buffer.Rows = 24
	}
	if buffer.sealedRowIDs == nil {
		buffer.sealedRowIDs = make(map[uint64]struct{})
	}
	if buffer.NextRowID == 0 {
		buffer.NextRowID = 1
	}
	if buffer.Main == nil || len(buffer.Main.Rows) != buffer.Rows {
		buffer.Main = buffer.newGrid(RowOwnerShellStream)
	}
	if buffer.Alt == nil || len(buffer.Alt.Rows) != buffer.Rows {
		buffer.Alt = buffer.newGrid(RowOwnerAltScreen)
	}
	if buffer.Margins.Bottom <= 0 || buffer.Margins.Bottom > buffer.Rows {
		buffer.Margins = MarginState{Top: 0, Bottom: buffer.Rows, Left: 0, Right: buffer.Cols}
	}
	buffer.Cursor.X = clampInt(buffer.Cursor.X, 0, maxInt(0, buffer.Cols-1))
	buffer.Cursor.Y = clampInt(buffer.Cursor.Y, 0, maxInt(0, buffer.Rows-1))
}

func clonePhysicalRows(rows []PhysicalRow) []PhysicalRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]PhysicalRow, len(rows))
	for index, row := range rows {
		out[index] = row
		out[index].Cells = cloneHistoryCells(row.Cells)
	}
	return out
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
