package termxcorev2

import (
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// SemanticTapInputKind 描述进入 history semantic tap 的输入类型。
// domain owner 是 core-v2 terminal history ingest；它把 PTY bytes 和 resize 放进
// history 语义序列，避免 history fallback 到 raw parser 或 live snapshot。
type SemanticTapInputKind string

const (
	// SemanticTapInputWrite 表示真实 PTY bytes 输入。truth source 是 child process
	// 输出；history tap 会用 termx-vterm 解码并产出 semantic transaction。
	SemanticTapInputWrite SemanticTapInputKind = "write"
	// SemanticTapInputResize 表示 PTY resize 输入。resize 必须和 PTY bytes 在同一
	// history tap sequence 内排序，不能由 history consumer 自行推断。
	SemanticTapInputResize SemanticTapInputKind = "resize"
)

// SemanticTapInputRecord 是 harness 可观察的 history tap 输入边界。
// 它不保存 history truth；只用于证明 PTY bytes 与 resize 进入同一个 history 语义序列。
type SemanticTapInputRecord struct {
	Seq  uint64
	Kind SemanticTapInputKind
	Size Size
}

// SemanticTapResult 是 history semantic tap 对一次输入的轻量输出。
// Transaction 只给 decision 构建和 harness 拉取完整语义消息副本；生产 history
// apply 只能消费同次 transaction 裁剪出的 compact journal，不能把 full transaction
// renderer 当作 fallback。Revision 只保留 history tap 自身诊断，不再作为 Terminal
// live revision owner。
// R396 后 live.screen.get 从 live SurfaceTrack 读取，不能从 history tap snapshot 取屏幕。
type SemanticTapResult struct {
	terminalID string
	tx         vterm.TerminalSemanticTransaction
	revision   LiveRevision
}

var semanticTapSnapshotBuildHook func()

// SemanticTap 负责单个 terminal 的 history semantic consumer。
// domain owner 是 core-v2 Terminal history ingest；truth source 是 termx-vterm
// semantic pass。消息链路是 PTY bytes/resize -> history tap -> immutable semantic
// tx -> compact journal / renderer。失败条件是 history replay raw PTY、从 live
// snapshot 反推 history、resize 脱离 history 输入序列或让 history tap 回写 response。
type SemanticTap struct {
	mu         sync.Mutex
	terminalID string
	source     *vterm.SemanticSource
	onResponse vterm.ResponseHandler
	revision   LiveRevision
	inputSeq   uint64
	inputs     []SemanticTapInputRecord
}

// NewSemanticTap 创建 history semantic tap。
// 无效 size 会回退到 80x24；R396 生产路径必须传 nil onResponse，让 live
// SurfaceTrack 成为唯一 response 回写 owner。带 onResponse 的用法只留给 tap 自身 harness。
func NewSemanticTap(terminalID string, size Size, onResponse vterm.ResponseHandler) *SemanticTap {
	cols, rows := semanticTapSize(size)
	return &SemanticTap{
		terminalID: terminalID,
		source:     vterm.NewSemanticSource(cols, rows, 0, onResponse),
		onResponse: onResponse,
	}
}

// ApplyPTYWrite 按 tap sequence 消费 PTY bytes。
// 返回的 transaction 已经由 history vterm pass 解码；history consumer 只能消费该
// transaction/journal，不能读取 raw PTY、live SurfaceTrack 或 TUI rows。
func (tap *SemanticTap) ApplyPTYWrite(raw []byte) (SemanticTapResult, error) {
	if tap == nil {
		return SemanticTapResult{}, nil
	}
	tap.mu.Lock()
	defer tap.mu.Unlock()
	tap.ensureSourceLocked()
	tap.inputSeq++
	record := SemanticTapInputRecord{
		Seq:  tap.inputSeq,
		Kind: SemanticTapInputWrite,
	}
	tx, err := tap.source.ApplyPTYWrite(raw)
	// 中文说明：SemanticSource 返回的 tx 已是本次 tap result 独占 payload。
	// 这里不再热路径深拷贝 full transaction；外部 consumer 仍只能通过
	// Transaction/HistoryJournal 拿到各自副本。
	tx.Raw = ""
	tap.revision++
	tap.inputs = append(tap.inputs, record)
	return SemanticTapResult{
		terminalID: tap.terminalID,
		tx:         tx,
		revision:   tap.revision,
	}, err
}

// Resize 按 tap sequence 消费 PTY resize。
// resize transaction 与 PTY bytes 使用同一 vterm state；renderer 只能把它作为
// semantic boundary，不能从 resized snapshot 伪造 sealed history。
func (tap *SemanticTap) Resize(size Size) (SemanticTapResult, error) {
	if tap == nil || !size.Valid() {
		return SemanticTapResult{}, nil
	}
	tap.mu.Lock()
	defer tap.mu.Unlock()
	tap.ensureSourceLocked()
	tap.inputSeq++
	record := SemanticTapInputRecord{
		Seq:  tap.inputSeq,
		Kind: SemanticTapInputResize,
		Size: size,
	}
	tx, err := tap.source.Resize(vterm.TerminalSemanticSize{Cols: int(size.Cols), Rows: int(size.Rows)})
	// 中文说明：resize result 同样保持 tap 内独占 payload，避免无意义 full
	// transaction clone；history 仍只能把它作为 semantic boundary 消费。
	tx.Raw = ""
	tap.revision++
	tap.inputs = append(tap.inputs, record)
	return SemanticTapResult{
		terminalID: tap.terminalID,
		tx:         tx,
		revision:   tap.revision,
	}, err
}

// Transaction 返回 semantic transaction 的 deep copy。
// 这是 R372 的 immutable handoff 边界：history/live fan-out 中任一 consumer
// 修改自己拿到的 slice，都不能污染其他 consumer 或 tap 内部状态。
func (result SemanticTapResult) Transaction() vterm.TerminalSemanticTransaction {
	return cloneSemanticTapTransaction(result.tx)
}

// HistoryJournal 返回 history semantic tap 同次语义 pass 产出的 compact journal 副本。
// 生产 history backlog 应优先消费该命令化 journal；这里从 result 持有的独占
// semantic transaction lazy 裁剪，不在 history write 热路径提前构造，也不
// 读取 raw PTY、live snapshot、TUI rows 或 live SurfaceTrack。
func (result SemanticTapResult) HistoryJournal() history.HistoryJournal {
	return history.HistoryJournalFromTransaction(result.terminalID, result.tx)
}

// Revision 返回本次 tap 输入后的 latest native screen revision。
// R396 后它只是 history tap 诊断值，不能作为 Terminal live revision 或 history generation。
func (result SemanticTapResult) Revision() LiveRevision {
	return result.revision
}

// ResetForRestartPreservingScreen 在 process restart 时重建 tap vterm owner。
// terminal identity 不变，latest visible screen 可作为 live context 保留；外部
// process 已更换，因此旧程序的 alt/mouse/pending parser state 必须丢弃，history
// truth 仍由 store 持有，不能从这个 live snapshot 反推。
func (tap *SemanticTap) ResetForRestartPreservingScreen(size Size) NativeScreenSnapshot {
	if tap == nil {
		return NativeScreenSnapshot{}
	}
	tap.mu.Lock()
	defer tap.mu.Unlock()
	tap.ensureSourceLocked()
	snapshot := tap.snapshotLocked()
	cols, rows := semanticTapSize(size)
	screenRows := make([][]vterm.Cell, rows)
	for _, row := range snapshot.Rows {
		if row.Index < 0 || row.Index >= rows {
			continue
		}
		screenRows[row.Index] = cloneSemanticTapCells(row.Cells)
	}
	cursor := snapshot.Cursor
	if cursor.Row < 0 || cursor.Row >= rows {
		cursor.Row = 0
	}
	if cursor.Col < 0 || cursor.Col >= cols {
		cursor.Col = 0
	}
	vt := vterm.New(cols, rows, 0, tap.onResponse)
	vt.LoadSizedSnapshotWithExtendedMetadata(
		cols,
		rows,
		nil,
		nil,
		nil,
		nil,
		vterm.ScreenData{Cells: screenRows},
		nil,
		nil,
		nil,
		cursor,
		vterm.TerminalModes{AutoWrap: true},
	)
	tap.source = vterm.NewSemanticSourceFromVTerm(vt)
	tap.inputSeq = 0
	tap.inputs = nil
	tap.revision++
	return tap.snapshotLocked()
}

// NativeScreenSnapshot 返回同一 tap vterm state 的 latest native screen。
// 调用方只能把它当 live projection；search/copy/page 必须继续走 authoritative
// history projection。
func (tap *SemanticTap) NativeScreenSnapshot() NativeScreenSnapshot {
	if tap == nil {
		return NativeScreenSnapshot{}
	}
	tap.mu.Lock()
	defer tap.mu.Unlock()
	tap.ensureSourceLocked()
	return cloneSemanticTapSnapshot(tap.snapshotLocked())
}

// LiveRevision 返回 tap latest native screen revision。
// 它只表示 live projection 变化，不是 history generation 或 frozen token。
func (tap *SemanticTap) LiveRevision() LiveRevision {
	if tap == nil {
		return 0
	}
	tap.mu.Lock()
	defer tap.mu.Unlock()
	return tap.revision
}

// InputRecords 返回 R372 harness 用的 tap 输入序列副本。
// 生产 consumer 不应依赖它；R373 后真实 fan-out 应以 transaction/revision 为边界。
func (tap *SemanticTap) InputRecords() []SemanticTapInputRecord {
	if tap == nil {
		return nil
	}
	tap.mu.Lock()
	defer tap.mu.Unlock()
	out := make([]SemanticTapInputRecord, len(tap.inputs))
	copy(out, tap.inputs)
	return out
}

func (tap *SemanticTap) ensureSourceLocked() {
	if tap.source != nil {
		return
	}
	tap.source = vterm.NewSemanticSource(80, 24, 0, tap.onResponse)
}

func (tap *SemanticTap) snapshotLocked() NativeScreenSnapshot {
	if semanticTapSnapshotBuildHook != nil {
		semanticTapSnapshotBuildHook()
	}
	vt := tap.source.VTerm()
	if vt == nil {
		return NativeScreenSnapshot{TerminalID: tap.terminalID, Revision: tap.revision, Timestamp: time.Now().UTC()}
	}
	var rows []NativeScreenRow
	screenInfo := vt.VisitTrimmedScreenRows(func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell) {
		for len(rows) < rowIndex {
			rows = append(rows, NativeScreenRow{Index: len(rows)})
		}
		row := NativeScreenRow{Index: rowIndex}
		if cellCount > 0 {
			row.Cells = make([]vterm.Cell, cellCount)
			for i := 0; i < cellCount; i++ {
				row.Cells[i] = cellAt(i)
			}
		}
		rows = append(rows, row)
	})
	for len(rows) < screenInfo.Rows {
		rows = append(rows, NativeScreenRow{Index: len(rows)})
	}
	return NativeScreenSnapshot{
		TerminalID: tap.terminalID,
		Revision:   tap.revision,
		Size:       NativeScreenSize{Cols: screenInfo.Cols, Rows: screenInfo.Rows},
		Rows:       rows,
		Cursor:     screenInfo.Cursor,
		Modes:      screenInfo.Modes,
		AltScreen:  screenInfo.IsAlternateScreen,
		Timestamp:  time.Now().UTC(),
	}
}

func semanticTapSize(size Size) (int, int) {
	if !size.Valid() {
		return 80, 24
	}
	return int(size.Cols), int(size.Rows)
}

func cloneSemanticTapTransaction(tx vterm.TerminalSemanticTransaction) vterm.TerminalSemanticTransaction {
	cloned := tx
	// 中文说明：tap 之后的 fan-out 合同只暴露 semantic transaction，不能把 raw PTY
	// bytes 继续传给 history queue 或测试 consumer，避免形成第二套 parser owner。
	cloned.Raw = ""
	cloned.Ops = cloneSemanticTapOps(tx.Ops)
	cloned.PrimaryScrollOut = cloneSemanticTapScrollOut(tx.PrimaryScrollOut)
	cloned.EvictedRows = cloneSemanticTapScrollOut(tx.EvictedRows)
	cloned.PrimaryFrame = cloneSemanticTapFrame(tx.PrimaryFrame)
	cloned.PrimaryFrameTouchedRows = cloneSemanticTapInts(tx.PrimaryFrameTouchedRows)
	cloned.AltFrame = cloneSemanticTapFrame(tx.AltFrame)
	cloned.AltExitFrame = cloneSemanticTapFrame(tx.AltExitFrame)
	cloned.SourceDamage = cloneSemanticTapWriteDamage(tx.SourceDamage)
	return cloned
}

func cloneSemanticTapWriteDamage(damage vterm.WriteDamage) vterm.WriteDamage {
	cloned := damage
	cloned.Ops = cloneSemanticTapOps(damage.Ops)
	cloned.SemanticOps = cloneSemanticTapOps(damage.SemanticOps)
	cloned.ScrollbackAppend = cloneSemanticTapOps(damage.ScrollbackAppend)
	cloned.EvictedAppend = cloneSemanticTapOps(damage.EvictedAppend)
	cloned.AlternateAppend = cloneSemanticTapOps(damage.AlternateAppend)
	cloned.DirectDamageTouchedRows = cloneSemanticTapInts(damage.DirectDamageTouchedRows)
	return cloned
}

func cloneSemanticTapOps(ops []vterm.DamageOp) []vterm.DamageOp {
	if len(ops) == 0 {
		return nil
	}
	out := make([]vterm.DamageOp, len(ops))
	for i, op := range ops {
		out[i] = op
		out[i].Cells = cloneSemanticTapCells(op.Cells)
		out[i].Runs = cloneSemanticTapRuns(op.Runs)
		out[i].ScrollOut = cloneSemanticTapScrollbackRows(op.ScrollOut)
		out[i].TailFill = cloneSemanticTapCellStylePointer(op.TailFill)
	}
	return out
}

func cloneSemanticTapScrollbackRows(rows []vterm.ScrollbackRowAppend) []vterm.ScrollbackRowAppend {
	if len(rows) == 0 {
		return nil
	}
	out := make([]vterm.ScrollbackRowAppend, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Cells = cloneSemanticTapCells(row.Cells)
		out[i].Runs = cloneSemanticTapRuns(row.Runs)
	}
	return out
}

func cloneSemanticTapScrollOut(rows []vterm.TerminalSemanticScrollOut) []vterm.TerminalSemanticScrollOut {
	if len(rows) == 0 {
		return nil
	}
	out := make([]vterm.TerminalSemanticScrollOut, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Cells = cloneSemanticTapCells(row.Cells)
		out[i].Runs = cloneSemanticTapRuns(row.Runs)
	}
	return out
}

func cloneSemanticTapFrame(frame *vterm.TerminalSemanticFrame) *vterm.TerminalSemanticFrame {
	if frame == nil {
		return nil
	}
	return &vterm.TerminalSemanticFrame{
		Rows: cloneSemanticTapCellRows(frame.Rows),
		Cols: frame.Cols,
	}
}

func cloneSemanticTapCellRows(rows [][]vterm.Cell) [][]vterm.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]vterm.Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneSemanticTapCells(row)
	}
	return out
}

func cloneSemanticTapCells(cells []vterm.Cell) []vterm.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]vterm.Cell, len(cells))
	copy(out, cells)
	return out
}

func cloneSemanticTapRuns(runs []vterm.CellRun) []vterm.CellRun {
	if len(runs) == 0 {
		return nil
	}
	out := make([]vterm.CellRun, len(runs))
	copy(out, runs)
	return out
}

func cloneSemanticTapInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	copy(out, values)
	return out
}

func cloneSemanticTapCellStylePointer(style *vterm.CellStyle) *vterm.CellStyle {
	if style == nil {
		return nil
	}
	cloned := *style
	return &cloned
}

func cloneSemanticTapSnapshot(snapshot NativeScreenSnapshot) NativeScreenSnapshot {
	cloned := snapshot
	if len(snapshot.Rows) == 0 {
		return cloned
	}
	cloned.Rows = make([]NativeScreenRow, len(snapshot.Rows))
	for i, row := range snapshot.Rows {
		cloned.Rows[i] = row
		cloned.Rows[i].Cells = cloneSemanticTapCells(row.Cells)
	}
	return cloned
}
