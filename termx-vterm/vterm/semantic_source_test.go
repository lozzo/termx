package vterm

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	charmvt "github.com/lozzow/termx/termx-vterm/internal/vt"
)

func TestSemanticSourceApplyPTYWriteEmitsOrderedTransaction(t *testing.T) {
	source := NewSemanticSource(12, 2, 100, nil)
	tx, err := source.ApplyPTYWrite([]byte("one\r\ntwo\r\nthree"))
	if err != nil {
		t.Fatalf("apply pty write: %v", err)
	}
	if tx.Seq != 1 || tx.Size != (TerminalSemanticSize{Cols: 12, Rows: 2}) {
		t.Fatalf("unexpected transaction header: %#v", tx)
	}
	if len(tx.Ops) == 0 {
		t.Fatalf("transaction must expose ordered semantic ops: %#v", tx)
	}
	if len(tx.PrimaryScrollOut) != 0 {
		t.Fatalf("ordinary primary write should keep scroll-out payload in ordered ops only: %#v", tx.PrimaryScrollOut)
	}
	if tx.PrimaryFrame != nil || tx.AltFrame != nil {
		t.Fatalf("ordinary primary write should not carry frame side proof: %#v", tx)
	}
	if tx.SourceDamage.SizeCols != 12 || tx.SourceDamage.SizeRows != 2 {
		t.Fatalf("transaction should keep source damage summary for diagnostics: %#v", tx.SourceDamage)
	}
	if len(tx.SourceDamage.Ops) != 0 || len(tx.SourceDamage.ScrollbackAppend) != 0 {
		t.Fatalf("ordinary transaction must not duplicate source damage payload: %#v", tx.SourceDamage)
	}
}

func TestR390SemanticSourceTransfersDamageOpsWithoutCrossWriteAlias(t *testing.T) {
	source := NewSemanticSource(24, 4, 100, nil)
	first, err := source.ApplyPTYWrite([]byte("first line\r\n"))
	if err != nil {
		t.Fatalf("apply first write: %v", err)
	}
	if len(first.Ops) == 0 || (len(first.Ops[0].Cells) == 0 && len(first.Ops[0].Runs) == 0) {
		t.Fatalf("expected first write to expose ordered ops, got %#v", first)
	}
	if len(first.Ops[0].Runs) > 0 {
		first.Ops[0].Runs[0].Text = "X"
	} else {
		first.Ops[0].Cells[0].Content = "X"
	}

	second, err := source.ApplyPTYWrite([]byte("second line\r\n"))
	if err != nil {
		t.Fatalf("apply second write: %v", err)
	}
	if len(second.Ops) == 0 || (len(second.Ops[0].Cells) == 0 && len(second.Ops[0].Runs) == 0) {
		t.Fatalf("expected second write to expose ordered ops, got %#v", second)
	}
	if got := semanticOpTextForSourceTest(second.Ops[0]); got == "X" {
		t.Fatalf("mutating one transaction must not alias future semantic damage payload, got %#v", second.Ops[0])
	}
}

func TestSemanticSourceApplyPTYWriteEmitsModeBoundaries(t *testing.T) {
	source := NewSemanticSource(24, 4, 100, nil)
	syncTx, err := source.ApplyPTYWrite([]byte("\x1b[?2026hhello\x1b[?2026l"))
	if err != nil {
		t.Fatalf("apply sync write: %v", err)
	}
	if !syncTx.SynchronizedBegin || !syncTx.SynchronizedEnd {
		t.Fatalf("transaction must expose synchronized output boundaries: %#v", syncTx)
	}

	altTx, err := source.ApplyPTYWrite([]byte("\x1b[?1049halt\x1b[?1049l"))
	if err != nil {
		t.Fatalf("apply alt write: %v", err)
	}
	if !altTx.AltEntered || !altTx.AltExited {
		t.Fatalf("transaction must expose alt enter/exit boundaries: %#v", altTx)
	}
}

func TestSemanticSourceSynchronizedActiveSpansSplitTransactions(t *testing.T) {
	source := NewSemanticSource(12, 3, 100, nil)
	begin, err := source.ApplyPTYWrite([]byte("\x1b[?2026h"))
	if err != nil {
		t.Fatalf("apply sync begin: %v", err)
	}
	payload, err := source.ApplyPTYWrite([]byte("payload"))
	if err != nil {
		t.Fatalf("apply sync payload: %v", err)
	}
	end, err := source.ApplyPTYWrite([]byte("\x1b[?2026l"))
	if err != nil {
		t.Fatalf("apply sync end: %v", err)
	}
	if !begin.SynchronizedBegin || !begin.SynchronizedActive {
		t.Fatalf("begin transaction should expose active synchronized mode, got %#v", begin)
	}
	if payload.SynchronizedBegin || payload.SynchronizedEnd || !payload.SynchronizedActive {
		t.Fatalf("payload transaction should remain inside synchronized mode, got %#v", payload)
	}
	if !end.SynchronizedEnd || end.SynchronizedActive {
		t.Fatalf("end transaction should close synchronized mode, got %#v", end)
	}
}

func TestSemanticSourceSynchronizedScrollOutKeepsPayloadRuns(t *testing.T) {
	source := NewSemanticSource(12, 3, 100, nil)
	raw := "\x1b[?2026h"
	for i := 1; i <= 8; i++ {
		raw += "line0" + string(rune('0'+i)) + "\r\n"
	}
	raw += "\x1b[?2026l"

	tx, err := source.ApplyPTYWrite([]byte(raw))
	if err != nil {
		t.Fatalf("apply synchronized output: %v", err)
	}
	if !tx.SynchronizedBegin || !tx.SynchronizedEnd {
		t.Fatalf("expected synchronized boundaries, got %#v", tx)
	}
	if len(tx.PrimaryScrollOut) < 6 {
		t.Fatalf("expected scroll-out proof for lines beyond screen, got %#v", tx.PrimaryScrollOut)
	}
	if got := semanticScrollOutText(tx.PrimaryScrollOut[0]); got != "line01" {
		t.Fatalf("scroll-out proof must preserve text payload, got %q in %#v", got, tx.PrimaryScrollOut[0])
	}
	if tx.PrimaryFrame == nil || len(tx.PrimaryFrame.Rows) < 2 || cellsTextForSemanticTest(tx.PrimaryFrame.Rows[0]) != "line07" || cellsTextForSemanticTest(tx.PrimaryFrame.Rows[1]) != "line08" {
		t.Fatalf("expected latest frame to remain final screen, got %#v", tx.PrimaryFrame)
	}
}

func TestR335SemanticSourceExposesFullReplaceTouchedRows(t *testing.T) {
	source := NewSemanticSource(80, 24, 100, nil)
	prevWithDamage := safeEmulatorWriteWithSemanticDamage
	safeEmulatorWriteWithSemanticDamage = func(emu *charmvt.SafeEmulator, data []byte) (int, error, []charmvt.Damage, bool) {
		n, err := emu.Write(data)
		cells := make([]uv.Cell, emu.Width())
		for col := range cells {
			cells[col] = uv.Cell{Content: "x", Width: 1}
		}
		damages := make([]charmvt.Damage, emu.Height())
		for row := 0; row < emu.Height(); row++ {
			damages[row] = charmvt.SpanDamage{X: 0, Y: row, Cells: cells}
		}
		return n, err, damages, true
	}
	t.Cleanup(func() {
		safeEmulatorWriteWithSemanticDamage = prevWithDamage
	})

	tx, err := source.ApplyPTYWrite([]byte("x"))
	if err != nil {
		t.Fatalf("apply broad direct damage: %v", err)
	}
	if !tx.RequiresFullReplace || tx.FullReplaceReason != "broad_direct_cell_damage" {
		t.Fatalf("expected broad full replace transaction, got %#v", tx)
	}
	if len(tx.PrimaryFrameTouchedRows) != 24 || tx.PrimaryFrameTouchedRows[0] != 0 || tx.PrimaryFrameTouchedRows[23] != 23 {
		t.Fatalf("semantic transaction must carry primary frame touched rows, got %#v", tx.PrimaryFrameTouchedRows)
	}
}

func TestR417SemanticSourceAttachesFrameForPrimaryPseudoTUIEdit(t *testing.T) {
	source := NewSemanticSource(20, 6, 100, nil)
	if _, err := source.ApplyPTYWrite([]byte("shell prompt\r\n")); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	tx, err := source.ApplyPTYWrite([]byte("\r\x1b[Kcodex edit"))
	if err != nil {
		t.Fatalf("apply CR+EL primary edit: %v", err)
	}
	if tx.PrimaryFrame == nil {
		t.Fatalf("primary edit transaction must attach primary frame proof, got %#v", tx)
	}
	if len(tx.PrimaryFrameTouchedRows) == 0 {
		t.Fatalf("primary edit transaction must expose touched rows, got %#v", tx)
	}
	if got := cellsTextForSemanticTest(tx.PrimaryFrame.Rows[1]); got != "codex edit" {
		t.Fatalf("primary frame should contain edited current line, got %q tx=%#v", got, tx)
	}
}

func TestR417SemanticSourceFrameAttachCoversPrimaryEditOps(t *testing.T) {
	cases := []struct {
		name string
		op   DamageOp
	}{
		{name: "clear to eol", op: DamageOp{Code: ScreenOpClearToEOL, Row: 1, Col: 0}},
		{name: "clear rect", op: DamageOp{Code: ScreenOpClearRect, Rect: DamageRect{X: 0, Y: 1, Width: 10, Height: 1}}},
		{name: "scroll rect", op: DamageOp{Code: ScreenOpScrollRect, Rect: DamageRect{X: 0, Y: 1, Width: 10, Height: 3}, Dy: -1}},
		{name: "copy rect", op: DamageOp{Code: ScreenOpCopyRect, Src: DamageRect{X: 0, Y: 1, Width: 10, Height: 1}, DstX: 0, DstY: 3}},
		{name: "relative cursor", op: DamageOp{Code: ScreenOpControl, Control: "cuu", Mode: 1}},
		{name: "line edit", op: DamageOp{Code: ScreenOpControl, Control: "dch", Row: 2, Col: 4, Mode: 1}},
		{name: "line insert", op: DamageOp{Code: ScreenOpControl, Control: "il", Row: 2, Mode: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !terminalSemanticShouldAttachFrame(WriteDamage{SemanticOps: []DamageOp{tc.op}}, TerminalSemanticTransaction{}) {
				t.Fatalf("semantic source should attach frame for %s op %#v", tc.name, tc.op)
			}
		})
	}
}

func TestR328SemanticSourceDistinguishesEraseDisplayFromClearScrollback(t *testing.T) {
	ed2 := NewSemanticSource(12, 3, 100, nil)
	if _, err := ed2.ApplyPTYWrite([]byte("old1\r\nold2")); err != nil {
		t.Fatalf("seed ED2 source: %v", err)
	}
	ed2Tx, err := ed2.ApplyPTYWrite([]byte("\x1b[2Jnew"))
	if err != nil {
		t.Fatalf("apply ED2: %v", err)
	}
	if ed2Tx.ClearScrollback {
		t.Fatalf("ED2 saves visible screen to scrollback; it must not clear authoritative history: %#v", ed2Tx)
	}
	gotED2Proof := ""
	for _, proof := range ed2Tx.PrimaryScrollOut {
		gotED2Proof += semanticScrollOutText(proof)
	}
	if gotED2Proof != "old1old2" {
		t.Fatalf("ED2 should expose clear-time scrollback proof for every visible row, got %q in %#v", gotED2Proof, ed2Tx.PrimaryScrollOut)
	}
	if len(ed2Tx.PrimaryScrollOut) != 2 || !ed2Tx.PrimaryScrollOut[0].RowSet || ed2Tx.PrimaryScrollOut[0].Row != 0 || !ed2Tx.PrimaryScrollOut[1].RowSet || ed2Tx.PrimaryScrollOut[1].Row != 1 {
		t.Fatalf("ED2 clear-time proof must preserve original viewport rows, got %#v", ed2Tx.PrimaryScrollOut)
	}

	ed3 := NewSemanticSource(12, 3, 100, nil)
	if _, err := ed3.ApplyPTYWrite([]byte("old1\r\nold2")); err != nil {
		t.Fatalf("seed ED3 source: %v", err)
	}
	ed3Tx, err := ed3.ApplyPTYWrite([]byte("\x1b[3Jnew"))
	if err != nil {
		t.Fatalf("apply ED3: %v", err)
	}
	if !ed3Tx.ClearScrollback {
		t.Fatalf("ED3 must expose explicit clear-scrollback boundary to core history: %#v", ed3Tx)
	}
}

func TestSemanticSourceResizeEmitsTransaction(t *testing.T) {
	source := NewSemanticSource(8, 2, 100, nil)
	if _, err := source.ApplyPTYWrite([]byte("abcdef\n")); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	tx, err := source.Resize(TerminalSemanticSize{Cols: 4, Rows: 3})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if tx.Seq != 2 || tx.Size != (TerminalSemanticSize{Cols: 4, Rows: 3}) {
		t.Fatalf("unexpected resize transaction header: %#v", tx)
	}
	if !tx.RequiresFullReplace || tx.FullReplaceReason != "resize" {
		t.Fatalf("resize transaction should mark full replace boundary: %#v", tx)
	}
	if tx.PrimaryFrame == nil || tx.PrimaryFrame.Cols != 4 {
		t.Fatalf("resize transaction should include current primary frame projection: %#v", tx)
	}
}

func semanticScrollOutText(row TerminalSemanticScrollOut) string {
	var out string
	for _, cell := range row.Cells {
		out += cell.Content
	}
	for _, run := range row.Runs {
		out += run.Text
	}
	return strings.TrimRight(out, " ")
}

func cellsTextForSemanticTest(cells []TerminalSemanticCell) string {
	var out string
	for _, cell := range cells {
		out += cell.Content
	}
	return out
}

func semanticOpTextForSourceTest(op DamageOp) string {
	var out string
	for _, run := range op.Runs {
		out += run.Text
	}
	for _, cell := range op.Cells {
		out += cell.Content
	}
	return strings.TrimRight(out, " ")
}
