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

type TerminalSemanticScrollOut struct {
	Cells      []TerminalSemanticCell
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

	AltEntered          bool
	AltExited           bool
	SynchronizedBegin   bool
	SynchronizedEnd     bool
	RequiresFullReplace bool
	FullReplaceReason   string

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
		SourceDamage:        damage,
	}
	for _, scrollOut := range damage.ScrollbackAppend {
		tx.PrimaryScrollOut = append(tx.PrimaryScrollOut, TerminalSemanticScrollOut{
			Cells:      cloneCellSlice(scrollOut.Cells),
			Wrapped:    scrollOut.Wrapped,
			WrappedSet: scrollOut.WrappedSet,
		})
	}
	tx.AltEntered = terminalSemanticDamageHasAltMode(damage, true)
	tx.AltExited = terminalSemanticDamageHasAltMode(damage, false)
	tx.SynchronizedBegin = terminalSemanticDamageHasSyncMode(damage, true)
	tx.SynchronizedEnd = terminalSemanticDamageHasSyncMode(damage, false)
	if source.vt != nil {
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
	return out
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
