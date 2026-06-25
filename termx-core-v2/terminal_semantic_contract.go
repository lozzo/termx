package termxcorev2

import vterm "github.com/lozzow/termx/termx-vterm/vterm"

type TerminalSemanticSource = vterm.TerminalSemanticSource
type TerminalSemanticSize = vterm.TerminalSemanticSize
type TerminalSemanticTransaction = vterm.TerminalSemanticTransaction
type TerminalSemanticOp = vterm.TerminalSemanticOp
type TerminalSemanticCell = vterm.TerminalSemanticCell
type TerminalSemanticStyle = vterm.TerminalSemanticStyle
type TerminalSemanticScrollOut = vterm.TerminalSemanticScrollOut
type TerminalSemanticFrame = vterm.TerminalSemanticFrame

func terminalSemanticTransactionFromBatch(batch terminalSemanticBatch) TerminalSemanticTransaction {
	tx := TerminalSemanticTransaction{
		Raw:  batch.Raw,
		Size: TerminalSemanticSize{Cols: batch.Cols, Rows: batch.Rows},
	}
	for index, damage := range batch.Damages {
		tx.Ops = append(tx.Ops, semanticOpsForHistoryDamage(damage)...)
		for _, scrollOut := range damage.ScrollbackAppend {
			tx.PrimaryScrollOut = append(tx.PrimaryScrollOut, TerminalSemanticScrollOut{
				Cells:      cloneVTermCells(scrollOut.Cells),
				Wrapped:    scrollOut.Wrapped,
				WrappedSet: scrollOut.WrappedSet,
			})
		}
		tx.AltEntered = tx.AltEntered || terminalSemanticDamageHasAltMode(damage, true)
		tx.AltExited = tx.AltExited || terminalSemanticDamageHasAltMode(damage, false)
		tx.SynchronizedBegin = tx.SynchronizedBegin || terminalSemanticDamageHasSyncMode(damage, true)
		tx.SynchronizedEnd = tx.SynchronizedEnd || terminalSemanticDamageHasSyncMode(damage, false)
		if damage.RequiresFullReplace {
			tx.RequiresFullReplace = true
			if tx.FullReplaceReason == "" {
				tx.FullReplaceReason = damage.FullReplaceReason
			}
		}
		if index == 0 {
			tx.SourceDamage = damage
		}
	}
	if len(batch.PrimaryScreenRows) > 0 {
		tx.PrimaryFrame = &TerminalSemanticFrame{Rows: cloneVTermCellRows(batch.PrimaryScreenRows), Cols: batch.Cols}
	}
	if len(batch.AltScreenRows) > 0 {
		tx.AltFrame = &TerminalSemanticFrame{Rows: cloneVTermCellRows(batch.AltScreenRows), Cols: batch.Cols}
	}
	if len(batch.AltExitFrame) > 0 {
		tx.AltExitFrame = &TerminalSemanticFrame{Rows: cloneVTermCellRows(batch.AltExitFrame), Cols: batch.Cols}
	}
	return tx
}

func terminalSemanticDamageHasAltMode(damage vterm.WriteDamage, enabled bool) bool {
	for _, op := range semanticOpsForHistoryDamage(damage) {
		if op.Code == vterm.ScreenOpModes && op.Private && (op.Mode == 47 || op.Mode == 1047 || op.Mode == 1049) && op.Enabled == enabled {
			return true
		}
	}
	return false
}

func terminalSemanticDamageHasSyncMode(damage vterm.WriteDamage, enabled bool) bool {
	for _, op := range semanticOpsForHistoryDamage(damage) {
		if op.Code == vterm.ScreenOpModes && op.Private && op.Mode == 2026 && op.Enabled == enabled {
			return true
		}
	}
	return false
}

func cloneVTermCellRows(rows [][]vterm.Cell) [][]vterm.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]vterm.Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneVTermCells(row)
	}
	return out
}

func cloneVTermCells(cells []vterm.Cell) []vterm.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]vterm.Cell, len(cells))
	copy(out, cells)
	return out
}
