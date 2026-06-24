package termxcorev2

import (
	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type terminalSemanticBatch struct {
	Raw             string
	Damages         []vterm.WriteDamage
	AltExitFrame    [][]vterm.Cell
	Cols            int
	Rows            int
	FromSharedVTerm bool
}

var terminalSemanticIngestBatchHook func(terminalSemanticBatch)
var terminalSemanticProjectorHook func(terminalSemanticProjectorStats)

type terminalSemanticProjectorStats struct {
	DamageBatches     int
	WriteSpanOps      int
	ClearToEOLOps     int
	ScrollbackAppends int
	FullReplaceOnly   int
}

func resetTerminalSemanticIngestTestHooks() {
	terminalSemanticIngestBatchHook = nil
	terminalSemanticProjectorHook = nil
}

func terminalSemanticBatchesFromSurfaceResult(result live.SurfaceWriteResult, size live.SurfaceSize) []terminalSemanticBatch {
	if len(result.Segments) == 0 {
		return nil
	}
	batches := make([]terminalSemanticBatch, 0, len(result.Segments))
	for _, segment := range result.Segments {
		if segment.Raw == "" && len(segment.Damages) == 0 && len(segment.AltScreenExitFrame) == 0 {
			continue
		}
		batch := terminalSemanticBatch{
			Raw:             segment.Raw,
			Damages:         segment.Damages,
			AltExitFrame:    segment.AltScreenExitFrame,
			Cols:            size.Cols,
			Rows:            size.Rows,
			FromSharedVTerm: true,
		}
		batches = append(batches, batch)
	}
	return batches
}

func (pipeline *terminalHistoryPipeline) IngestSemanticBatch(batch terminalSemanticBatch) error {
	if terminalSemanticIngestBatchHook != nil {
		terminalSemanticIngestBatchHook(batch)
	}
	if batch.Raw == "" && len(batch.Damages) == 0 && len(batch.AltExitFrame) == 0 {
		return nil
	}
	if terminalHistoryPipelineBeforeIngestHook != nil {
		terminalHistoryPipelineBeforeIngestHook()
	}
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	if batch.Cols > 0 && batch.Rows > 0 {
		pipeline.cols = batch.Cols
		pipeline.rows = batch.Rows
		pipeline.track.SetPrimaryScreenRows(batch.Rows)
	}
	pipeline.ingest.SetScreenSize(pipeline.cols, pipeline.rows)
	if err := pipeline.projectSemanticBatchLocked(batch); err != nil {
		return err
	}
	pipeline.publishGenerationLocked()
	return nil
}

func (pipeline *terminalHistoryPipeline) projectSemanticBatchLocked(batch terminalSemanticBatch) error {
	// 中文说明：第一阶段让 vterm batch 成为唯一终端语义事务来源；
	// raw parser 临时只负责把文本/SGR/OSC8 辅助投影成 HistoryEvent cells。
	stats := terminalSemanticProjectorStats{}
	for _, damage := range batch.Damages {
		ops := semanticOpsForHistoryDamage(damage)
		if damage.SizeCols > 0 || damage.SizeRows > 0 || len(ops) > 0 || len(damage.ScrollbackAppend) > 0 || len(damage.AlternateAppend) > 0 || damage.RequiresFullReplace {
			stats.DamageBatches++
		}
		if damage.RequiresFullReplace && len(ops) == 0 && len(damage.ScrollbackAppend) == 0 && len(damage.AlternateAppend) == 0 {
			stats.FullReplaceOnly++
		}
		for _, op := range ops {
			switch op.Code {
			case vterm.ScreenOpWriteSpan:
				stats.WriteSpanOps++
			case vterm.ScreenOpClearToEOL:
				stats.ClearToEOLOps++
			}
		}
		stats.ScrollbackAppends += len(damage.ScrollbackAppend)
	}
	if batch.FromSharedVTerm && (batch.Raw == "" || rawSharedBatchCanUseSemanticOps(batch.Damages)) && semanticBatchHasHistoryOps(batch.Damages) {
		if batch.Raw != "" {
			pipeline.shadowParsePrimaryOutputLocked(batch.Raw)
		}
		_, err := pipeline.applyVTermDamageEventsLocked(batch.Damages)
		if err != nil {
			return err
		}
	} else if batch.Raw != "" {
		var err error
		if batch.FromSharedVTerm {
			// 中文说明：shared vterm 已经给出 alt-screen final-frame 和 damage；
			// 只有 vterm 暂未产出可消费语义时才允许 raw parser 作为迁移辅助。
			err = pipeline.ingestPrimaryOutputLocked(batch.Raw)
		} else {
			err = pipeline.ingestOutputLocked(batch.Raw)
		}
		if err != nil {
			return err
		}
	}
	if len(batch.AltExitFrame) > 0 {
		if err := pipeline.track.Apply(history.HistoryEvent{
			Kind: history.EventAppendAltScreenFrame,
			Rows: historyRowsFromVTermRows(batch.AltExitFrame),
		}); err != nil {
			return err
		}
	}
	if terminalSemanticProjectorHook != nil {
		terminalSemanticProjectorHook(stats)
	}
	return nil
}

func (pipeline *terminalHistoryPipeline) shadowParsePrimaryOutputLocked(output string) {
	if output == "" {
		return
	}
	// 中文说明：vterm 已经提供可消费语义时，parser 只能维护 pending/style/OSC8
	// 辅助状态，不能再把 raw 文本或控制事件写入 HistoryTrack。
	_ = pipeline.ingest.Parse(output)
	pipeline.ingest.clearSegments()
}

func semanticBatchHasHistoryOps(damages []vterm.WriteDamage) bool {
	for _, damage := range damages {
		if len(semanticOpsForHistoryDamage(damage)) > 0 || len(damage.ScrollbackAppend) > 0 {
			return true
		}
	}
	return false
}

func rawSharedBatchCanUseSemanticOps(damages []vterm.WriteDamage) bool {
	hasOps := false
	for _, damage := range damages {
		if damage.RequiresFullReplace || len(damage.ScrollbackAppend) > 0 || len(damage.AlternateAppend) > 0 {
			return false
		}
		if len(damage.SemanticOps) == 0 {
			continue
		}
		for _, op := range damage.SemanticOps {
			switch op.Code {
			case vterm.ScreenOpWriteSpan:
				if len(op.Cells) == 0 {
					continue
				}
				hasOps = true
			case vterm.ScreenOpControl:
				if !rawSharedControlCanUseSemanticOp(op.Control) {
					return false
				}
				hasOps = true
			default:
				return false
			}
		}
	}
	return hasOps
}

func rawSharedControlCanUseSemanticOp(control string) bool {
	switch control {
	case "cr", "bs", "ht", "cuf", "cub", "cha", "cup", "vpa":
		return true
	default:
		return false
	}
}

func (pipeline *terminalHistoryPipeline) applyVTermDamageEventsLocked(damages []vterm.WriteDamage) (bool, error) {
	applied := false
	for _, damage := range damages {
		ops := semanticOpsForHistoryDamage(damage)
		if len(ops) == 0 && len(damage.ScrollbackAppend) == 0 {
			continue
		}
		scrollbackApplied := 0
		for _, op := range ops {
			switch op.Code {
			case vterm.ScreenOpWriteSpan:
				cells := historyCellsFromVTermCells(op.Cells)
				if len(cells) == 0 {
					continue
				}
				if err := pipeline.applyCursorPositionFromVTerm(op.Row, op.Col); err != nil {
					return applied, err
				}
				if err := pipeline.track.ApplyOwned(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: cells}); err != nil {
					return applied, err
				}
				applied = true
			case vterm.ScreenOpClearToEOL:
				if err := pipeline.applyCursorPositionFromVTerm(op.Row, op.Col); err != nil {
					return applied, err
				}
				if err := pipeline.track.Apply(history.HistoryEvent{
					Kind:      history.EventEraseInLine,
					EraseMode: 0,
					EraseCols: pipeline.cols,
					Style:     historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
				}); err != nil {
					return applied, err
				}
				applied = true
			case vterm.ScreenOpClearRect:
				if op.Rect.Width <= 0 || op.Rect.Height <= 0 {
					continue
				}
				if op.Rect.X == 0 && op.Rect.Width >= pipeline.cols {
					if err := pipeline.applyCursorPositionFromVTerm(op.Rect.Y, op.Rect.X); err != nil {
						return applied, err
					}
					if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: 0}); err != nil {
						return applied, err
					}
					applied = true
				}
			case vterm.ScreenOpScrollRect:
				// 中文说明：screen scroll rect 本身只是屏幕移动；只有同批
				// primary scrollback append 证明有行离开 ownership 时才提交历史。
				count := primaryScrollOutCountForDamageOp(op, len(damage.ScrollbackAppend)-scrollbackApplied)
				if count <= 0 {
					continue
				}
				if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventPrimaryScrollOut, Count: count}); err != nil {
					return applied, err
				}
				scrollbackApplied += count
				applied = true
			case vterm.ScreenOpControl:
				if err := pipeline.applyVTermControlEventLocked(op); err != nil {
					return applied, err
				}
				applied = true
			case vterm.ScreenOpModes:
				if err := pipeline.applyVTermModeEventLocked(op); err != nil {
					return applied, err
				}
				applied = true
			}
		}
		for ; scrollbackApplied < len(damage.ScrollbackAppend); scrollbackApplied++ {
			if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventPrimaryScrollOut, Count: 1}); err != nil {
				return applied, err
			}
			applied = true
		}
	}
	return applied, nil
}

func semanticOpsForHistoryDamage(damage vterm.WriteDamage) []vterm.DamageOp {
	if len(damage.SemanticOps) > 0 {
		return damage.SemanticOps
	}
	return damage.Ops
}

func primaryScrollOutCountForDamageOp(op vterm.DamageOp, maxCount int) int {
	if maxCount <= 0 || op.Code != vterm.ScreenOpScrollRect || op.Dy >= 0 {
		return 0
	}
	count := -op.Dy
	if op.Rect.Height > 0 && count > op.Rect.Height {
		count = op.Rect.Height
	}
	if count > maxCount {
		count = maxCount
	}
	return count
}

func (pipeline *terminalHistoryPipeline) applyVTermControlEventLocked(op vterm.DamageOp) error {
	switch op.Control {
	case "cr":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCarriageReturn})
	case "lf", "ind":
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSealLogicalLine}); err != nil {
			return err
		}
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCommitFrontier})
	case "ri":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorUp, Count: 1})
	case "cuu":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorUp, Count: op.Mode})
	case "cud":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorDown, Count: op.Mode})
	case "cuf":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorForward, Count: op.Mode})
	case "cub":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorBackward, Count: op.Mode})
	case "bs":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorBackward, Count: 1})
	case "ht":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorHorizontalAbsolute, Count: op.Col + 1})
	case "cha":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorHorizontalAbsolute, Count: op.Col + 1})
	case "cup", "vpa":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorPosition, Row: op.Row + 1, Column: op.Col + 1})
	case "el":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:      history.EventEraseInLine,
			EraseMode: op.Mode,
			EraseCols: pipeline.cols,
		})
	case "ed":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: op.Mode})
	default:
		return nil
	}
}

func (pipeline *terminalHistoryPipeline) applyVTermModeEventLocked(op vterm.DamageOp) error {
	if !op.Private {
		return nil
	}
	switch op.Mode {
	case 47, 1047, 1049:
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: op.Enabled})
	case 25:
		if !op.Enabled {
			return pipeline.track.Apply(history.HistoryEvent{
				Kind:        history.EventEnterPrimaryFullscreen,
				PrimaryMode: op.Mode,
			})
		}
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:        history.EventExitPrimaryFullscreen,
			PrimaryMode: op.Mode,
		})
	case 1000, 1002, 1003, 1004, 1006, 2026:
		if op.Enabled {
			return pipeline.track.Apply(history.HistoryEvent{
				Kind:        history.EventEnterPrimaryFullscreen,
				PrimaryMode: op.Mode,
			})
		}
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:        history.EventExitPrimaryFullscreen,
			PrimaryMode: op.Mode,
		})
	default:
		return nil
	}
}

func (pipeline *terminalHistoryPipeline) applyCursorPositionFromVTerm(row int, col int) error {
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorPosition, Row: row + 1, Column: col + 1})
}

func historyCellsFromVTermCells(cells []vterm.Cell) []history.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(cells))
	for _, cell := range cells {
		if cell.Content == "" && cell.Width == 0 {
			continue
		}
		text := cell.Content
		width := cell.Width
		if width <= 0 {
			width = 1
		}
		if text == "" {
			text = " "
		}
		out = append(out, history.Cell{
			Text:       text,
			Width:      width,
			Style:      historyStyleFromVTermStyle(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
	}
	return out
}

func opStyleFromClearToEOL(op vterm.DamageOp) vterm.CellStyle {
	if len(op.Cells) == 0 {
		return vterm.CellStyle{}
	}
	return op.Cells[0].Style
}
