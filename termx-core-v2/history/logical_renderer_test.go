package history

import (
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR326LogicalRendererSharesIDsAcrossStreamAndFrameReducers(t *testing.T) {
	renderer := NewHistoryLogicalRenderer(nil, nil)
	batch, err := renderer.Apply(TerminalSemanticTransaction{
		Seq: 1,
		PrimaryScrollOut: []TerminalSemanticScrollOut{{
			Runs: []TerminalSemanticCellRun{{Text: "scroll-out"}},
		}},
		PrimaryFrame: &TerminalSemanticFrame{
			Cols: 12,
			Rows: [][]TerminalSemanticCell{{{Content: "frame", Width: 1}}},
		},
	}, HistoryDecision{Mode: HistoryOutputModePrimaryFrameSession, PublishPrimaryFrame: true, ConsumeScrollOutProof: true})
	if err != nil {
		t.Fatalf("apply renderer transaction: %v", err)
	}

	seen := make(map[LogicalLineID]string)
	for _, mutation := range batch.Mutations {
		switch {
		case mutation.Line != nil:
			if previous := seen[mutation.Line.ID]; previous != "" {
				t.Fatalf("line id collision for sealed stream line id=%d previous=%s new=%s batch=%#v", mutation.Line.ID, previous, lineText(*mutation.Line), batch)
			}
			seen[mutation.Line.ID] = lineText(*mutation.Line)
		case mutation.Mutable != nil:
			for _, row := range mutation.Mutable.Rows {
				if previous := seen[row.Line.ID]; previous != "" {
					t.Fatalf("line id collision for frame line id=%d previous=%s new=%s batch=%#v", row.Line.ID, previous, lineText(row.Line), batch)
				}
				seen[row.Line.ID] = lineText(row.Line)
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected distinct scroll-out and frame logical lines, got %#v batch=%#v", seen, batch)
	}
}

func TestR329LogicalRendererConsumesED2OrderedScrollOutAndClearsCurrentFrame(t *testing.T) {
	renderer := NewHistoryLogicalRenderer(nil, nil)
	if _, err := renderer.Apply(TerminalSemanticTransaction{
		Seq: 1,
		PrimaryFrame: &TerminalSemanticFrame{
			Cols: 12,
			Rows: [][]TerminalSemanticCell{{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}}},
		},
	}, HistoryDecision{Mode: HistoryOutputModePrimaryFrameSession, PublishPrimaryFrame: true}); err != nil {
		t.Fatalf("seed current frame: %v", err)
	}

	batch, err := renderer.Apply(TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{{
			Code:    vterm.ScreenOpControl,
			Control: "ed",
			Mode:    2,
			ScrollOut: []TerminalSemanticScrollbackRowAppend{{
				Runs: []TerminalSemanticCellRun{{Text: "old"}},
			}},
		}},
		PrimaryFrame: &TerminalSemanticFrame{
			Cols: 12,
			Rows: [][]TerminalSemanticCell{{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}}},
		},
	}, HistoryDecision{Mode: HistoryOutputModePrimaryFrameSession, PublishPrimaryFrame: true, ConsumeScrollOutProof: true, ConsumeClearBoundary: true})
	if err != nil {
		t.Fatalf("apply ED2 redraw: %v", err)
	}

	var sealed []string
	clearPrimary := 0
	replacePrimary := 0
	for _, mutation := range batch.Mutations {
		if mutation.Kind == HistoryMutationSealLine && mutation.Line != nil {
			sealed = append(sealed, lineText(*mutation.Line))
		}
		if mutation.Kind == HistoryMutationClearPrimaryFrame {
			clearPrimary++
		}
		if mutation.Kind == HistoryMutationReplacePrimaryFrame {
			replacePrimary++
		}
		if mutation.Kind == HistoryMutationArchivePrimaryFrame {
			t.Fatalf("ED2 scroll-out proof owns old screen history; renderer must not also archive old frame: %#v", batch)
		}
	}
	if len(sealed) != 1 || sealed[0] != "old" {
		t.Fatalf("ED2 ordered scroll-out must be sealed into history, got %v batch=%#v", sealed, batch)
	}
	if clearPrimary != 1 || replacePrimary != 1 {
		t.Fatalf("ED2 redraw should clear old current then publish new frame, clear=%d replace=%d batch=%#v", clearPrimary, replacePrimary, batch)
	}
}

func TestR329LogicalRendererRISClearsFrameFrontiers(t *testing.T) {
	renderer := NewHistoryLogicalRenderer(nil, nil)
	if _, err := renderer.Apply(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{writeOp(0, 0, "open")},
	}, HistoryDecision{Mode: HistoryOutputModeOrdinaryStream}); err != nil {
		t.Fatalf("seed open line: %v", err)
	}
	if _, err := renderer.Apply(TerminalSemanticTransaction{
		Seq: 2,
		PrimaryFrame: &TerminalSemanticFrame{
			Cols: 12,
			Rows: [][]TerminalSemanticCell{{{Content: "f", Width: 1}}},
		},
	}, HistoryDecision{Mode: HistoryOutputModePrimaryFrameSession, PublishPrimaryFrame: true}); err != nil {
		t.Fatalf("seed primary frame: %v", err)
	}

	batch, err := renderer.Apply(TerminalSemanticTransaction{
		Seq: 3,
		Ops: []TerminalSemanticOp{controlOp("ris", 0, 0, 0)},
	}, HistoryDecision{Mode: HistoryOutputModeOrdinaryStream})
	if err != nil {
		t.Fatalf("apply RIS: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(batch.Mutations)); got != "open" {
		t.Fatalf("RIS must seal ordinary open content before reset, got %q batch=%#v", got, batch)
	}
	clearPrimary := 0
	for _, mutation := range batch.Mutations {
		if mutation.Kind == HistoryMutationClearPrimaryFrame {
			clearPrimary++
		}
	}
	if clearPrimary != 1 {
		t.Fatalf("RIS must clear current primary frame ownership, clear=%d batch=%#v", clearPrimary, batch)
	}
}
