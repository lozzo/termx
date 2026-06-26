package vterm

// TerminalSemanticSource 是 vterm 对 core-v2 暴露的终端语义事务源。
// domain owner：vterm 解码 PTY bytes；history 只能消费 transaction，不能回放 raw。
type TerminalSemanticSource interface {
	ApplyPTYWrite(raw []byte) (TerminalSemanticTransaction, error)
	Resize(size TerminalSemanticSize) (TerminalSemanticTransaction, error)
}

type TerminalSemanticSize struct {
	Cols int
	Rows int
}

type TerminalSemanticOp = DamageOp
type TerminalSemanticCell = Cell
type TerminalSemanticStyle = CellStyle

// TerminalSemanticCellRun 是 vterm 在 scroll-out proof 中保留的 styled text run。
// domain owner：vterm；core 只能消费它展开历史 payload，不能把 raw PTY 当 fallback。
type TerminalSemanticCellRun = CellRun

// TerminalSemanticScrollOut 是同一个 transaction 内 primary 内容离开可见区域的证明。
// Cells 是 cell 级 payload；Runs 是 vterm 在 raw damage 中保留的 styled text payload。
// core 只能把这些 payload 复制进 logical-line history，不能从 current frame 反推历史。
type TerminalSemanticScrollOut struct {
	Cells      []TerminalSemanticCell
	Runs       []TerminalSemanticCellRun
	Wrapped    bool
	WrappedSet bool
}

type TerminalSemanticFrame struct {
	Rows [][]TerminalSemanticCell
	Cols int
}

// TerminalSemanticTransaction 是一次 PTY write/resize 的 ordered semantic 边界。
// Raw 只用于上层 shadow state，不是 history fallback 输入。
type TerminalSemanticTransaction struct {
	Seq  uint64
	Raw  string
	Size TerminalSemanticSize

	Ops []TerminalSemanticOp

	PrimaryScrollOut []TerminalSemanticScrollOut
	PrimaryFrame     *TerminalSemanticFrame
	AltFrame         *TerminalSemanticFrame
	AltExitFrame     *TerminalSemanticFrame

	AltEntered        bool
	AltExited         bool
	SynchronizedBegin bool
	// SynchronizedActive 表示 transaction 结束后 mode 2026 仍处于开启状态。
	// core classifier 用它识别 begin/end 分片之间的 payload，不回看 raw bytes。
	SynchronizedActive  bool
	SynchronizedEnd     bool
	RequiresFullReplace bool
	FullReplaceReason   string
	// ClearScrollback 表示 PTY 明确发出了清除 terminal scrollback/history 的语义
	// 边界，例如 ED3。它不是 vterm 内部 scrollback 容量淘汰，core 只能在该字段为
	// true 时清 authoritative scrollback/history projection。
	ClearScrollback bool

	SourceDamage WriteDamage
}

type SemanticSource struct {
	vt  *VTerm
	seq uint64
}

func NewSemanticSource(cols int, rows int, scrollbackSize int, onResponse ResponseHandler) *SemanticSource {
	return &SemanticSource{vt: New(cols, rows, scrollbackSize, onResponse)}
}

func NewSemanticSourceFromVTerm(vt *VTerm) *SemanticSource {
	if vt == nil {
		vt = New(80, 24, 0, nil)
	}
	return &SemanticSource{vt: vt}
}

func (source *SemanticSource) VTerm() *VTerm {
	if source == nil {
		return nil
	}
	return source.vt
}

func (source *SemanticSource) ApplyPTYWrite(raw []byte) (TerminalSemanticTransaction, error) {
	if source == nil {
		return TerminalSemanticTransaction{}, nil
	}
	source.ensureVTerm()
	_, err, damage := source.vt.WriteWithDamage(raw)
	source.seq++
	return source.transactionFromDamage(source.seq, string(raw), damage), err
}

func (source *SemanticSource) Resize(size TerminalSemanticSize) (TerminalSemanticTransaction, error) {
	if source == nil || size.Cols <= 0 || size.Rows <= 0 {
		return TerminalSemanticTransaction{}, nil
	}
	source.ensureVTerm()
	damage := source.vt.ResizeWithDamage(size.Cols, size.Rows)
	source.seq++
	tx := source.transactionFromDamage(source.seq, "", damage)
	tx.Size = size
	return tx, nil
}

func (source *SemanticSource) ensureVTerm() {
	if source.vt == nil {
		source.vt = New(80, 24, 0, nil)
	}
}

func (source *SemanticSource) transactionFromDamage(seq uint64, raw string, damage WriteDamage) TerminalSemanticTransaction {
	size := TerminalSemanticSize{Cols: damage.SizeCols, Rows: damage.SizeRows}
	if size.Cols == 0 || size.Rows == 0 {
		size = source.currentSize()
	}
	tx := TerminalSemanticTransaction{
		Seq:                 seq,
		Raw:                 raw,
		Size:                size,
		Ops:                 cloneSemanticOps(semanticOpsForTransactionDamage(damage)),
		RequiresFullReplace: damage.RequiresFullReplace,
		FullReplaceReason:   damage.FullReplaceReason,
		ClearScrollback:     terminalSemanticDamageHasClearScrollback(damage),
		SourceDamage:        damage,
	}
	for _, scrollOut := range damage.ScrollbackAppend {
		tx.PrimaryScrollOut = append(tx.PrimaryScrollOut, TerminalSemanticScrollOut{
			Cells:      cloneCellSlice(scrollOut.Cells),
			Runs:       cloneCellRuns(scrollOut.Runs),
			Wrapped:    scrollOut.Wrapped,
			WrappedSet: scrollOut.WrappedSet,
		})
	}
	tx.AltEntered = terminalSemanticDamageHasAltMode(damage, true)
	tx.AltExited = terminalSemanticDamageHasAltMode(damage, false)
	tx.SynchronizedBegin = terminalSemanticDamageHasSyncMode(damage, true)
	tx.SynchronizedEnd = terminalSemanticDamageHasSyncMode(damage, false)
	if source.vt != nil {
		tx.SynchronizedActive = source.vt.Modes().SynchronizedOutput
		screen := source.vt.UsedScreenContent()
		frame := &TerminalSemanticFrame{Rows: cloneCellRows(screen.Cells), Cols: size.Cols}
		if screen.IsAlternateScreen {
			tx.AltFrame = frame
		} else {
			tx.PrimaryFrame = frame
		}
	}
	return tx
}

func (source *SemanticSource) currentSize() TerminalSemanticSize {
	if source == nil || source.vt == nil {
		return TerminalSemanticSize{}
	}
	source.vt.mu.RLock()
	defer source.vt.mu.RUnlock()
	if source.vt.emu == nil {
		return TerminalSemanticSize{}
	}
	return TerminalSemanticSize{Cols: source.vt.emu.Width(), Rows: source.vt.emu.Height()}
}

func semanticOpsForTransactionDamage(damage WriteDamage) []DamageOp {
	if len(damage.SemanticOps) > 0 {
		return damage.SemanticOps
	}
	return damage.Ops
}

func terminalSemanticDamageHasAltMode(damage WriteDamage, enabled bool) bool {
	for _, op := range semanticOpsForTransactionDamage(damage) {
		if op.Code == ScreenOpModes && op.Private && (op.Mode == 47 || op.Mode == 1047 || op.Mode == 1049) && op.Enabled == enabled {
			return true
		}
	}
	return false
}

func terminalSemanticDamageHasSyncMode(damage WriteDamage, enabled bool) bool {
	for _, op := range semanticOpsForTransactionDamage(damage) {
		if op.Code == ScreenOpModes && op.Private && op.Mode == 2026 && op.Enabled == enabled {
			return true
		}
	}
	return false
}

func cloneSemanticOps(ops []DamageOp) []DamageOp {
	if len(ops) == 0 {
		return nil
	}
	out := make([]DamageOp, len(ops))
	copy(out, ops)
	for i := range out {
		out[i].Cells = cloneCellSlice(out[i].Cells)
		out[i].Runs = cloneCellRuns(out[i].Runs)
		out[i].ScrollOut = cloneScrollbackRowAppends(out[i].ScrollOut)
	}
	return out
}

func terminalSemanticDamageHasClearScrollback(damage WriteDamage) bool {
	for _, op := range semanticOpsForTransactionDamage(damage) {
		if op.Code == ScreenOpControl && op.Control == "ed" && op.Mode == 3 {
			return true
		}
	}
	return false
}

func cloneCellRows(rows [][]Cell) [][]Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneCellSlice(row)
	}
	return out
}

func cloneCellRuns(runs []CellRun) []CellRun {
	if len(runs) == 0 {
		return nil
	}
	out := make([]CellRun, len(runs))
	copy(out, runs)
	return out
}

func cloneScrollbackRowAppends(rows []ScrollbackRowAppend) []ScrollbackRowAppend {
	if len(rows) == 0 {
		return nil
	}
	out := make([]ScrollbackRowAppend, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Cells = cloneCellSlice(row.Cells)
		out[i].Runs = cloneCellRuns(row.Runs)
	}
	return out
}
