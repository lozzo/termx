package termxcorev2

import (
	"testing"

	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR302CoreConsumesVTermSemanticTransactionContract(t *testing.T) {
	var _ TerminalSemanticSource = vterm.NewSemanticSource(12, 2, 100, nil)

	surface := live.NewSurfaceTrack(live.SurfaceSize{Cols: 12, Rows: 2})
	result := surface.WriteWithResult("one\r\ntwo\r\nthree")
	batches := terminalSemanticBatchesFromSurfaceResult(result, surface.Size())
	if len(batches) == 0 {
		t.Fatal("surface write must produce shared-vterm semantic batches")
	}
	tx := terminalSemanticTransactionFromBatch(batches[0])
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
