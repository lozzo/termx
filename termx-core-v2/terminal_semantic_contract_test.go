package termxcorev2

import (
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR302CoreConsumesVTermSemanticTransactionContract(t *testing.T) {
	source := vterm.NewSemanticSource(12, 2, 100, nil)
	var _ TerminalSemanticSource = source

	tx, err := source.ApplyPTYWrite([]byte("one\r\ntwo\r\nthree"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	if tx.Size != (TerminalSemanticSize{Cols: 12, Rows: 2}) {
		t.Fatalf("transaction should preserve PTY size, got %#v", tx.Size)
	}
	if len(tx.Ops) == 0 || len(tx.PrimaryScrollOut) == 0 {
		t.Fatalf("transaction must expose ordered ops and scroll-out proof: %#v", tx)
	}
	if tx.PrimaryFrame == nil || tx.AltFrame != nil {
		t.Fatalf("ordinary primary output should carry primary frame projection only: %#v", tx)
	}
}
