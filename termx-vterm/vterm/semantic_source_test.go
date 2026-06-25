package vterm

import "testing"

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
