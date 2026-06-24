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
	DamageBatches      int
	WriteSpanOps       int
	ClearToEOLOps      int
	EraseDisplayOps    int
	ModeOps            int
	ControlOps         int
	ScrollbackAppends  int
	AltExitFrames      int
	FullReplaceOnly    int
	SemanticProjectors int
	RawFallbacks       int
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
			case vterm.ScreenOpControl:
				stats.ControlOps++
				if op.Control == "el" {
					stats.ClearToEOLOps++
				} else if op.Control == "ed" {
					stats.EraseDisplayOps++
				}
			case vterm.ScreenOpModes:
				stats.ModeOps++
			}
		}
		stats.ScrollbackAppends += len(damage.ScrollbackAppend)
	}
	if len(batch.AltExitFrame) > 0 {
		stats.AltExitFrames++
	}
	fullReplaceOnly := sharedBatchFullReplaceOnly(batch.Damages)
	if batch.FromSharedVTerm && (batch.Raw == "" || rawSharedBatchCanUseSemanticOps(batch.Damages)) && semanticBatchHasHistoryOps(batch.Damages) {
		stats.SemanticProjectors++
		if batch.Raw != "" {
			pipeline.shadowParsePrimaryOutputLocked(batch.Raw)
		}
		_, err := pipeline.applyVTermDamageEventsLocked(batch.Damages)
		if err != nil {
			return err
		}
	} else if batch.Raw != "" && !(batch.FromSharedVTerm && fullReplaceOnly) {
		stats.RawFallbacks++
		var err error
		if batch.FromSharedVTerm {
			// 中文说明：shared vterm 已经给出 alt-screen final-frame 和 damage；
			// 只有 vterm 暂未产出可消费语义、且不是 full-replace/stale 边界时
			// 才允许 raw parser 作为迁移辅助。
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

func sharedBatchFullReplaceOnly(damages []vterm.WriteDamage) bool {
	hasFullReplace := false
	for _, damage := range damages {
		if damage.RequiresFullReplace {
			hasFullReplace = true
		}
		if len(semanticOpsForHistoryDamage(damage)) > 0 || len(damage.ScrollbackAppend) > 0 || len(damage.AlternateAppend) > 0 {
			return false
		}
	}
	return hasFullReplace
}

func rawSharedBatchCanUseSemanticOps(damages []vterm.WriteDamage) bool {
	hasOps := false
	linefeedControls := 0
	scrollbackAppends := 0
	for _, damage := range damages {
		if damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
			return false
		}
		lowRows := damage.SizeRows > 0 && damage.SizeRows <= 2
		tallPlainScrollOut := rawSharedTallPlainScrollOutDamageCanUseSemanticOps(damage)
		tallASCIIText := rawSharedTallASCIITextDamageCanUseSemanticOps(damage)
		if len(damage.ScrollbackAppend) > 0 {
			if !lowRows && !tallPlainScrollOut {
				return false
			}
			scrollbackAppends += len(damage.ScrollbackAppend)
			hasOps = true
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
				if op.Control == "lf" || op.Control == "ind" || op.Control == "soft-wrap" {
					if !lowRows && !tallPlainScrollOut && !tallASCIIText {
						return false
					}
					linefeedControls++
				}
				hasOps = true
			case vterm.ScreenOpModes:
				if !rawSharedModeCanUseSemanticOp(op) {
					return false
				}
				hasOps = true
			case vterm.ScreenOpScrollRect:
				if op.Dx != 0 || op.Dy == 0 {
					return false
				}
				if op.Dy < 0 {
					if len(damage.ScrollbackAppend) == 0 {
						return false
					}
					if !lowRows && !tallPlainScrollOut {
						return false
					}
					hasOps = true
					continue
				}
				if len(damage.ScrollbackAppend) > 0 || !semanticOpsContainControl(damage.SemanticOps, "ri") {
					return false
				}
				hasOps = true
			default:
				return false
			}
		}
	}
	if scrollbackAppends > 1 && linefeedControls <= 1 {
		return false
	}
	return hasOps
}

func rawSharedControlCanUseSemanticOp(control string) bool {
	switch control {
	case "cr", "bs", "ht", "cuf", "cub", "cha", "cup", "vpa", "el", "ed", "lf", "ind", "soft-wrap", "ri", "decstbm":
		return true
	default:
		return false
	}
}

func rawSharedModeCanUseSemanticOp(op vterm.DamageOp) bool {
	if !op.Private {
		return false
	}
	switch op.Mode {
	case 25, 47, 1047, 1049, 1000, 1002, 1003, 1004, 1006, 2026:
		return true
	default:
		return false
	}
}

func rawSharedTallPlainScrollOutDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：R201Z 只放开大高度普通 primary scroll-out；
	// styled/link/宽字符 footprint 仍留给后续语义切片，不能被这里抢走。
	if damage.SizeRows <= 2 || len(damage.ScrollbackAppend) == 0 || damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
		return false
	}
	for _, op := range damage.ScrollbackAppend {
		for _, cell := range op.Cells {
			if !rawSharedPlainHistoryCell(cell) {
				return false
			}
		}
		for _, run := range op.Runs {
			if run.Style != (vterm.CellStyle{}) || containsNonSingleWidthRune(run.Text) {
				return false
			}
		}
	}
	for _, op := range damage.SemanticOps {
		if op.Code != vterm.ScreenOpWriteSpan {
			continue
		}
		for _, cell := range op.Cells {
			if !rawSharedPlainHistoryCell(cell) {
				return false
			}
		}
	}
	return true
}

func rawSharedTallASCIITextDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：R201AA 只接管无 scroll-out 的 ASCII SGR/OSC8 文本；
	// 宽字符、combining 和 styled tail footprint 仍保留给后续切片。
	if damage.SizeRows <= 2 || len(damage.ScrollbackAppend) > 0 || damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
		return false
	}
	hasText := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedASCIITextCell(cell) {
					return false
				}
			}
			hasText = true
		case vterm.ScreenOpControl:
			if op.Control != "lf" && op.Control != "ind" && op.Control != "cr" {
				return false
			}
		default:
			return false
		}
	}
	return hasText
}

func rawSharedPlainHistoryCell(cell vterm.Cell) bool {
	return cell.Style == (vterm.CellStyle{}) &&
		cell.LinkURL == "" &&
		cell.LinkParams == "" &&
		(cell.Width == 0 || cell.Width == 1) &&
		!containsNonSingleWidthRune(cell.Content)
}

func rawSharedASCIITextCell(cell vterm.Cell) bool {
	return (cell.Width == 0 || cell.Width == 1) && !containsNonSingleWidthRune(cell.Content)
}

func containsNonSingleWidthRune(text string) bool {
	for _, r := range text {
		if r == '\t' || r == '\n' || r == '\r' {
			return true
		}
		if r < 0x20 || r >= 0x7f {
			return true
		}
	}
	return false
}

func (pipeline *terminalHistoryPipeline) applyVTermDamageEventsLocked(damages []vterm.WriteDamage) (bool, error) {
	applied := false
	for _, damage := range damages {
		ops := semanticOpsForHistoryDamage(damage)
		if len(ops) == 0 && len(damage.ScrollbackAppend) == 0 {
			continue
		}
		scrollbackApplied := 0
		controlLinefeedInDamage := semanticOpsContainLinefeedControl(ops)
		reverseIndexScrollDowns := semanticOpsReverseIndexScrollDownCount(ops)
		softWrapContinuation := false
		lastWriteRow := -1
		lastWriteEndCol := -1
		for _, op := range ops {
			switch op.Code {
			case vterm.ScreenOpWriteSpan:
				cells := historyCellsFromVTermCells(op.Cells)
				if len(cells) == 0 {
					continue
				}
				if softWrapContinuation {
					softWrapContinuation = false
				} else if op.Row != lastWriteRow || op.Col != lastWriteEndCol {
					if err := pipeline.applyCursorPositionFromVTerm(op.Row, op.Col); err != nil {
						return applied, err
					}
				}
				if err := pipeline.track.ApplyOwned(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: cells}); err != nil {
					return applied, err
				}
				lastWriteRow = op.Row
				lastWriteEndCol = op.Col + historyCellsDisplayWidth(cells)
				applied = true
			case vterm.ScreenOpClearToEOL:
				lastWriteRow = -1
				lastWriteEndCol = -1
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
				lastWriteRow = -1
				lastWriteEndCol = -1
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
				if controlLinefeedInDamage {
					continue
				}
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
				if op.Control != "soft-wrap" {
					lastWriteRow = -1
					lastWriteEndCol = -1
				}
				if op.Control == "ri" && reverseIndexScrollDowns > 0 {
					reverseIndexScrollDowns--
					applied = true
					continue
				}
				if err := pipeline.applyVTermControlEventLocked(op); err != nil {
					return applied, err
				}
				if op.Control == "lf" || op.Control == "ind" || op.Control == "soft-wrap" {
					if scrollbackApplied < len(damage.ScrollbackAppend) {
						scrollbackApplied++
					}
					softWrapContinuation = op.Control == "soft-wrap"
				}
				applied = true
			case vterm.ScreenOpModes:
				lastWriteRow = -1
				lastWriteEndCol = -1
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

func semanticOpsContainLinefeedControl(ops []vterm.DamageOp) bool {
	return semanticOpsContainControl(ops, "lf") ||
		semanticOpsContainControl(ops, "ind") ||
		semanticOpsContainControl(ops, "soft-wrap")
}

func semanticOpsContainControl(ops []vterm.DamageOp, control string) bool {
	for _, op := range ops {
		if op.Code == vterm.ScreenOpControl && op.Control == control {
			return true
		}
	}
	return false
}

func semanticOpsReverseIndexScrollDownCount(ops []vterm.DamageOp) int {
	if !semanticOpsContainControl(ops, "ri") {
		return 0
	}
	count := 0
	for _, op := range ops {
		if op.Code == vterm.ScreenOpScrollRect && op.Dy > 0 {
			count += op.Dy
		}
	}
	return count
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
	case "soft-wrap":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSoftWrapLine})
	case "lf", "ind":
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSealLogicalLine}); err != nil {
			return err
		}
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCommitFrontier})
	case "ri":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorUp, Count: 1})
	case "decstbm":
		// 中文说明：DECSTBM 只改变 vterm 的滚动区域；history 不保存第二份
		// scroll-region truth，后续 cursor/write/scroll-out 语义会显式投影。
		return nil
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
			Style:     historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
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
		next := history.Cell{
			Text:       text,
			Width:      width,
			Style:      historyStyleFromVTermStyle(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
		if len(out) > 0 && canMergeVTermTextHistoryCells(out[len(out)-1], next) {
			out[len(out)-1].Text += next.Text
			out[len(out)-1].Width += next.Width
			continue
		}
		out = append(out, next)
	}
	return out
}

func canMergeVTermTextHistoryCells(left history.Cell, right history.Cell) bool {
	return left.Style == right.Style &&
		left.LinkURL == right.LinkURL &&
		left.LinkParams == right.LinkParams &&
		left.Width > 0 &&
		right.Width > 0 &&
		!containsNonSingleWidthRune(left.Text) &&
		!containsNonSingleWidthRune(right.Text)
}

func historyCellsDisplayWidth(cells []history.Cell) int {
	width := 0
	for _, cell := range cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		width += len([]rune(cell.Text))
	}
	return width
}

func opStyleFromClearToEOL(op vterm.DamageOp) vterm.CellStyle {
	if len(op.Cells) == 0 {
		return vterm.CellStyle{}
	}
	return op.Cells[0].Style
}
