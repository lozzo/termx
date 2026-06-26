package history

import "testing"

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
