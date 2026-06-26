package vterm

import (
	"strings"
	"testing"
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
	if len(tx.PrimaryScrollOut) == 0 {
		t.Fatalf("transaction must expose primary scroll-out proof: %#v", tx)
	}
	if tx.PrimaryFrame == nil || len(tx.PrimaryFrame.Rows) == 0 || tx.AltFrame != nil {
		t.Fatalf("primary write should publish primary frame only: %#v", tx)
	}
	if tx.SourceDamage.SizeCols != 12 || tx.SourceDamage.SizeRows != 2 {
		t.Fatalf("transaction should keep source damage for current projector adapter: %#v", tx.SourceDamage)
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
