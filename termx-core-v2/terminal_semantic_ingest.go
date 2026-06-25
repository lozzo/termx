package termxcorev2

import (
	"unicode"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

type terminalSemanticBatch struct {
	Raw               string
	Damages           []vterm.WriteDamage
	PrimaryScreenRows [][]vterm.Cell
	AltScreenRows     [][]vterm.Cell
	AltExitFrame      [][]vterm.Cell
	Cols              int
	Rows              int
	FromSharedVTerm   bool
}

var terminalSemanticIngestBatchHook func(terminalSemanticBatch)
var terminalSemanticProjectorHook func(terminalSemanticProjectorStats)

type terminalSemanticProjectorStats struct {
	DamageBatches             int
	WriteSpanOps              int
	ClearToEOLOps             int
	EraseDisplayOps           int
	ModeOps                   int
	ControlOps                int
	ScrollbackAppends         int
	AlternateAppends          int
	AltExitFrames             int
	FullReplaceOnly           int
	SemanticProjectors        int
	PrimaryScreenTransactions int
	AltScreenTransactions     int
	RawFallbacks              int
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
		if segment.Raw == "" && len(segment.Damages) == 0 && len(segment.AltScreenRows) == 0 && len(segment.AltScreenExitFrame) == 0 {
			continue
		}
		batch := terminalSemanticBatch{
			Raw:               segment.Raw,
			Damages:           segment.Damages,
			PrimaryScreenRows: segment.PrimaryScreenRows,
			AltScreenRows:     segment.AltScreenRows,
			AltExitFrame:      segment.AltScreenExitFrame,
			Cols:              size.Cols,
			Rows:              size.Rows,
			FromSharedVTerm:   true,
		}
		batches = append(batches, batch)
	}
	return batches
}

func (pipeline *terminalHistoryPipeline) IngestSemanticBatch(batch terminalSemanticBatch) error {
	if terminalSemanticIngestBatchHook != nil {
		terminalSemanticIngestBatchHook(batch)
	}
	if batch.Raw == "" && len(batch.Damages) == 0 && len(batch.AltScreenRows) == 0 && len(batch.AltExitFrame) == 0 {
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
				if op.Control == "el" || op.Control == "ech" {
					stats.ClearToEOLOps++
				} else if op.Control == "ed" {
					stats.EraseDisplayOps++
				}
			case vterm.ScreenOpModes:
				stats.ModeOps++
			}
		}
		stats.ScrollbackAppends += len(damage.ScrollbackAppend)
		stats.AlternateAppends += len(damage.AlternateAppend)
	}
	if len(batch.AltScreenRows) > 0 || len(batch.AltExitFrame) > 0 {
		stats.AltExitFrames++
	}
	tx := terminalSemanticTransactionFromBatch(batch)
	decision := SimpleScreenAppClassifier{}.Classify(tx, ScreenSessionState{
		InAltScreen:             pipeline.track.InAltScreen(),
		HasActivePrimarySession: false,
	})
	publishSynchronizedFrame := false
	handledByHistoryProjector := false
	if batch.FromSharedVTerm && (batch.Raw == "" || rawSharedBatchCanUseSemanticOps(batch.Damages)) && semanticBatchHasHistoryOps(batch.Damages) {
		stats.SemanticProjectors++
		if batch.Raw != "" {
			pipeline.shadowParsePrimaryOutputLocked(batch.Raw)
		}
		if terminalSemanticTransactionCanUseOrdinaryProjector(tx, decision) {
			// 中文说明：ordinary stdout 的 truth 来自 vterm ordered transaction，
			// 由 HistoryTrackProjector 直接提交完整 logical line；复杂 screen app
			// 语义继续留给后续切片和既有 damage projector。
			if _, err := NewHistoryTrackProjector(pipeline.track).Apply(tx, decision); err != nil {
				return err
			}
			handledByHistoryProjector = true
		} else if terminalSemanticTransactionCanUsePrimaryScreenProjector(tx, decision) {
			stats.PrimaryScreenTransactions++
			// 中文说明：primary screen app session 的 truth 是 vterm transaction +
			// 当前 primary frame；repaint 只替换 current frame，不增加 ordinary depth。
			if _, err := NewHistoryTrackProjector(pipeline.track).Apply(tx, decision); err != nil {
				return err
			}
			handledByHistoryProjector = true
		} else if terminalSemanticTransactionCanUseAltScreenProjector(tx, decision) {
			stats.AltScreenTransactions++
			// 中文说明：alt-screen 是 transient current frame；进入 alt 前由
			// projector 归档/隐藏 primary current，alt frame 不写 primary history。
			if _, err := NewHistoryTrackProjector(pipeline.track).Apply(tx, decision); err != nil {
				return err
			}
			handledByHistoryProjector = true
		} else {
			var err error
			publishSynchronizedFrame, err = pipeline.applyVTermDamageEventsLocked(batch.Damages)
			if err != nil {
				return err
			}
		}
	} else if batch.Raw != "" && batch.FromSharedVTerm {
		// 中文说明：真实 PTY raw 已经被 shared vterm 解码一次；即使本批语义
		// 暂不可消费，也不能回退 historyANSIParser 重放终端控制。
		pipeline.shadowParsePrimaryOutputLocked(batch.Raw)
	}
	// 中文说明：alt-screen 不写 committed primary history，但运行中/退出时的
	// 当前可见帧需要进入 latest/frozen 作为 transient current frame，避免 history 模式丢内容。
	if len(batch.AltScreenRows) > 0 && !handledByHistoryProjector {
		if err := pipeline.track.Apply(history.HistoryEvent{
			Kind: history.EventAppendAltScreenFrame,
			Rows: historyRowsFromVTermRows(batch.AltScreenRows),
		}); err != nil {
			return err
		}
	}
	if len(batch.AltExitFrame) > 0 && !handledByHistoryProjector {
		if err := pipeline.track.Apply(history.HistoryEvent{
			Kind: history.EventAppendAltScreenFrame,
			Rows: historyRowsFromVTermRows(batch.AltExitFrame),
		}); err != nil {
			return err
		}
	}
	if publishSynchronizedFrame {
		if err := pipeline.track.Apply(history.HistoryEvent{
			Kind: history.EventReplacePrimaryFrame,
			Rows: historyRowsFromVTermRows(batch.PrimaryScreenRows),
		}); err != nil {
			return err
		}
	}
	if terminalSemanticProjectorHook != nil {
		terminalSemanticProjectorHook(stats)
	}
	return nil
}

func terminalSemanticTransactionCanUseOrdinaryProjector(tx TerminalSemanticTransaction, decision ScreenAppDecision) bool {
	if decision.Mode != ScreenOutputModeOrdinary || tx.RequiresFullReplace || tx.AltEntered || tx.AltExited || tx.SynchronizedBegin || tx.SynchronizedEnd {
		return false
	}
	if tx.PrimaryFrame == nil || tx.AltFrame != nil || tx.AltExitFrame != nil {
		return false
	}
	if transactionHasAltMode(tx, true) || transactionHasAltMode(tx, false) || transactionHasSyncMode(tx, true) || transactionHasSyncMode(tx, false) {
		return false
	}
	hasHistoryOp := false
	hasHardLineEnd := false
	for _, op := range tx.Ops {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) > 0 {
				if !terminalSemanticOrdinaryProjectorCellsAllowed(op.Cells) {
					return false
				}
				hasHistoryOp = true
			}
		case vterm.ScreenOpControl:
			if !terminalSemanticOrdinaryProjectorControlAllowed(op.Control) {
				return false
			}
			if op.Control == "lf" || op.Control == "ind" {
				hasHardLineEnd = true
			}
			hasHistoryOp = true
		case vterm.ScreenOpScrollRect:
			if op.Dx != 0 || op.Dy >= 0 || len(tx.PrimaryScrollOut) == 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasHistoryOp && hasHardLineEnd
}

func terminalSemanticTransactionCanUsePrimaryScreenProjector(tx TerminalSemanticTransaction, decision ScreenAppDecision) bool {
	if decision.Mode != ScreenOutputModePrimaryScreen || !decision.PublishFrame || tx.PrimaryFrame == nil {
		return false
	}
	if tx.AltFrame != nil || tx.AltExitFrame != nil || tx.AltEntered || tx.AltExited || tx.RequiresFullReplace {
		return false
	}
	return tx.SynchronizedEnd || transactionHasSyncMode(tx, false)
}

func terminalSemanticTransactionCanUseAltScreenProjector(tx TerminalSemanticTransaction, decision ScreenAppDecision) bool {
	hasAltBoundary := tx.AltEntered || tx.AltExited || transactionHasAltMode(tx, true) || transactionHasAltMode(tx, false)
	hasAltFrame := tx.AltFrame != nil || tx.AltExitFrame != nil
	if !hasAltBoundary && !hasAltFrame && decision.Mode != ScreenOutputModeAltTransient {
		return false
	}
	return true
}

func terminalSemanticOrdinaryProjectorControlAllowed(control string) bool {
	switch control {
	case "cr", "lf", "ind", "soft-wrap":
		return true
	default:
		return false
	}
}

func terminalSemanticOrdinaryProjectorCellsAllowed(cells []vterm.Cell) bool {
	for _, cell := range cells {
		if !rawSharedPlainHistoryCell(cell) {
			return false
		}
	}
	return true
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
		if len(semanticOpsForHistoryDamage(damage)) > 0 || len(damage.ScrollbackAppend) > 0 || len(damage.AlternateAppend) > 0 {
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
	lineOperationScrollback := false
	for _, damage := range damages {
		altScreenRunning := rawSharedAltScreenRunningDamageCanUseSemanticOps(damage)
		if len(damage.AlternateAppend) > 0 && !altScreenRunning {
			return false
		}
		fullReplaceSemantic := rawSharedFullReplaceDamageCanUseSemanticOps(damage)
		fullReplaceScrollOutSemantic := rawSharedFullReplaceScrollOutDamageCanUseSemanticOps(damage)
		if damage.RequiresFullReplace && !fullReplaceSemantic {
			if !fullReplaceScrollOutSemantic {
				return false
			}
		}
		lowRows := damage.SizeRows > 0 && damage.SizeRows <= 2
		tallPlainScrollOut := rawSharedTallPlainScrollOutDamageCanUseSemanticOps(damage)
		tallStyledScrollOut := rawSharedTallStyledScrollOutDamageCanUseSemanticOps(damage)
		tallLinkScrollOut := rawSharedTallLinkScrollOutDamageCanUseSemanticOps(damage)
		tallASCIIText := rawSharedTallASCIITextDamageCanUseSemanticOps(damage)
		tallGraphemeText := rawSharedTallGraphemeTextDamageCanUseSemanticOps(damage)
		tallLinefeedNewlineText := rawSharedTallLinefeedNewlineTextDamageCanUseSemanticOps(damage)
		directPrimaryText := rawSharedDirectPrimaryTextDamageCanUseSemanticOps(damage)
		eraseDisplaySemantic := semanticOpsContainControl(damage.SemanticOps, "ed")
		if directPrimaryText {
			hasOps = true
			continue
		}
		if len(damage.ScrollbackAppend) > 0 {
			if !lowRows && !tallPlainScrollOut && !tallStyledScrollOut && !tallLinkScrollOut {
				if !semanticOpsContainControl(damage.SemanticOps, "su") {
					if !eraseDisplaySemantic {
						return false
					}
					// 中文说明：ED2 page-break 会让 live vterm 伴随 scrollback append；
					// history truth 只能来自 ordered ed control，不能把 append row 当输出历史。
				} else {
					// 中文说明：SU 会让 vterm 同批产生真实 scrollback append；
					// history 仍由 ordered su control 提交 screen ownership，不把 append 行当 truth。
					lineOperationScrollback = true
				}
			}
			if !eraseDisplaySemantic {
				scrollbackAppends += len(damage.ScrollbackAppend)
			}
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
			case vterm.ScreenOpClearToEOL, vterm.ScreenOpClearRect:
				if semanticOpsContainAnyControl(damage.SemanticOps, "il", "dl", "su", "sd") {
					// 中文说明：行级操作的 blank fill 只是 live diff；
					// history 按同批 ordered line-control 更新 ownership。
					hasOps = true
					continue
				}
				return false
			case vterm.ScreenOpControl:
				if !rawSharedControlCanUseSemanticOp(op.Control) {
					return false
				}
				if op.Control == "lf" || op.Control == "ind" || op.Control == "soft-wrap" {
					if !lowRows && !tallPlainScrollOut && !tallStyledScrollOut && !tallLinkScrollOut && !tallASCIIText && !tallGraphemeText && !tallLinefeedNewlineText && !fullReplaceSemantic && !fullReplaceScrollOutSemantic && !altScreenRunning && !eraseDisplaySemantic {
						return false
					}
					// 中文说明：ED 后的文本可能从清屏前 cursor 继续写并 soft-wrap；
					// page-break truth 仍由同批 ordered ed control 驱动，不能退回 parser。
					linefeedControls++
				}
				hasOps = true
			case vterm.ScreenOpModes:
				if !rawSharedModeCanUseSemanticOp(op) {
					return false
				}
				hasOps = true
			case vterm.ScreenOpScrollRect:
				if op.Dx != 0 {
					if op.Dy == 0 && op.Dx < 0 && semanticOpsContainControl(damage.SemanticOps, "dch") {
						// 中文说明：DCH 的横向 scroll rect 只是 live screen diff；
						// history 只消费同批 ordered dch control，不能从这里推断删除语义。
						hasOps = true
						continue
					}
					if op.Dy == 0 && op.Dx > 0 && semanticOpsContainControl(damage.SemanticOps, "ich") {
						// 中文说明：ICH 的横向 scroll rect 只是 live screen diff；
						// history 只消费同批 ordered ich control，不能从这里推断插入语义。
						hasOps = true
						continue
					}
					return false
				}
				if op.Dy == 0 {
					return false
				}
				if op.Dy < 0 {
					if len(damage.ScrollbackAppend) == 0 {
						if semanticOpsContainAnyControl(damage.SemanticOps, "dl", "su") {
							// 中文说明：DL/SU 的垂直 scroll rect 只是 live screen diff；
							// history 只消费同批 ordered line-control，不能从这里推断整行删除。
							hasOps = true
							continue
						}
						if altScreenRunning {
							hasOps = true
							continue
						}
						return false
					}
					if !lowRows && !tallPlainScrollOut && !tallStyledScrollOut && !tallLinkScrollOut {
						if semanticOpsContainControl(damage.SemanticOps, "su") {
							// 中文说明：SU 自带真实 scrollback append，但 history
							// 提交由 ordered su control 更新 ownership，不能回退 parser。
							hasOps = true
							continue
						}
						return false
					}
					hasOps = true
					continue
				}
				if len(damage.ScrollbackAppend) > 0 || !semanticOpsContainAnyControl(damage.SemanticOps, "ri", "il", "sd") {
					return false
				}
				hasOps = true
			default:
				return false
			}
		}
	}
	if scrollbackAppends > 1 && linefeedControls <= 1 && !lineOperationScrollback {
		return false
	}
	return hasOps
}

func rawSharedAltScreenRunningDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：AlternateAppend 只作为“这批结束后仍在 alt-screen”的边界证据；
	// history 仍只消费同批 ordered SemanticOps，不能把 alt append 行当 primary truth。
	if len(damage.AlternateAppend) == 0 || len(damage.ScrollbackAppend) > 0 || damage.RequiresFullReplace {
		return false
	}
	hasSemantic := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
			}
			hasSemantic = true
		case vterm.ScreenOpControl:
			if !rawSharedControlCanUseSemanticOp(op.Control) {
				return false
			}
			hasSemantic = true
		case vterm.ScreenOpModes:
			if !rawSharedModeCanUseSemanticOp(op) {
				return false
			}
			hasSemantic = true
		case vterm.ScreenOpScrollRect:
			if op.Dx != 0 || op.Dy == 0 {
				return false
			}
			hasSemantic = true
		default:
			return false
		}
	}
	return hasSemantic
}

func rawSharedControlCanUseSemanticOp(control string) bool {
	switch control {
	case "cr", "bs", "ht", "cbt", "hts", "tbc", "decst8c", "scs", "decscusr", "dsr", "decxcpr", "decrqm", "da", "da2", "cuu", "cud", "cuf", "cub", "cha", "cup", "vpa", "ech", "dch", "ich", "il", "dl", "su", "sd", "el", "ed", "lf", "ind", "soft-wrap", "ri", "decstbm", "decslrm", "ris":
		return true
	default:
		return false
	}
}

func rawSharedModeCanUseSemanticOp(op vterm.DamageOp) bool {
	if !op.Private {
		return op.Mode == 20
	}
	switch op.Mode {
	case 1, 6, 7, 9, 25, 47, 66, 67, 69, 1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007, 1015, 1016, 1047, 1048, 1049, 2004, 2026, 2027, 2031, 2048, 9001:
		return true
	default:
		// 中文说明：未知 private mode 已由 shared vterm 解码；history 不保存
		// 第二份 mode truth，也不能因此回退 raw parser 重放同批终端语义。
		return true
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
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			for _, cell := range op.Cells {
				if !rawSharedPlainHistoryCell(cell) {
					return false
				}
			}
		case vterm.ScreenOpControl:
			if op.Control != "lf" && op.Control != "ind" && op.Control != "soft-wrap" && op.Control != "cr" {
				return false
			}
		case vterm.ScreenOpScrollRect:
			if op.Dx != 0 || op.Dy >= 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func rawSharedTallStyledScrollOutDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：R201AC 只放开大高度 styled primary scroll-out；
	// vterm 必须在行结束 control 上显式给出 TailFill，core 不从 scrollback row 反推。
	if damage.SizeRows <= 2 || len(damage.ScrollbackAppend) == 0 || damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
		return false
	}
	hasTailFill := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
			}
		case vterm.ScreenOpControl:
			if op.Control != "lf" && op.Control != "ind" && op.Control != "soft-wrap" && op.Control != "cr" {
				return false
			}
			if op.TailFill != nil {
				if *op.TailFill == (vterm.CellStyle{}) || op.TailFill.BG == "" {
					return false
				}
				hasTailFill = true
			}
		case vterm.ScreenOpScrollRect:
			if op.Dx != 0 || op.Dy >= 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasTailFill
}

func rawSharedTallLinkScrollOutDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：OSC8 scroll-out 只信任 ordered SemanticOps 中的 link cell；
	// ScrollbackAppend 只证明 primary screen ownership 离开，不提供 history payload。
	if damage.SizeRows <= 2 || len(damage.ScrollbackAppend) == 0 || damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
		return false
	}
	for _, op := range damage.ScrollbackAppend {
		if len(op.Runs) > 0 {
			return false
		}
		for _, cell := range op.Cells {
			if !rawSharedGraphemeTextCell(cell) {
				return false
			}
		}
	}
	hasLink := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
				if cell.LinkURL != "" || cell.LinkParams != "" {
					hasLink = true
				}
			}
		case vterm.ScreenOpControl:
			if op.Control != "lf" && op.Control != "ind" && op.Control != "soft-wrap" && op.Control != "cr" {
				return false
			}
			if op.TailFill != nil {
				return false
			}
		case vterm.ScreenOpScrollRect:
			if op.Dx != 0 || op.Dy >= 0 {
				return false
			}
		default:
			return false
		}
	}
	return hasLink
}

func rawSharedFullReplaceDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：RequiresFullReplace 只表示 live/stale 边界；若同批已经有
	// ordered semantic ops，history 只能消费这些 ops，不能退回 raw parser 或 screen diff。
	if !damage.RequiresFullReplace || len(damage.ScrollbackAppend) > 0 || len(damage.AlternateAppend) > 0 {
		return false
	}
	hasSemantic := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
			}
			hasSemantic = true
		case vterm.ScreenOpControl:
			if !rawSharedControlCanUseSemanticOp(op.Control) {
				return false
			}
			hasSemantic = true
		case vterm.ScreenOpModes:
			if !rawSharedModeCanUseSemanticOp(op) {
				return false
			}
			hasSemantic = true
		default:
			return false
		}
	}
	return hasSemantic
}

func rawSharedFullReplaceScrollOutDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：full-replace + scrollback append 仍可能携带完整 ordered semantic ops；
	// append row 只作为真实 primary scroll-out 证据，history payload 仍来自 semantic ops。
	if !damage.RequiresFullReplace || len(damage.ScrollbackAppend) == 0 || len(damage.AlternateAppend) > 0 {
		return false
	}
	hasSemantic := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
			}
			hasSemantic = true
		case vterm.ScreenOpControl:
			if !rawSharedControlCanUseSemanticOp(op.Control) {
				return false
			}
			hasSemantic = true
		case vterm.ScreenOpModes:
			if !rawSharedModeCanUseSemanticOp(op) {
				return false
			}
			hasSemantic = true
		case vterm.ScreenOpScrollRect:
			if op.Dx != 0 || op.Dy >= 0 {
				return false
			}
			hasSemantic = true
		default:
			return false
		}
	}
	return hasSemantic
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

func rawSharedTallGraphemeTextDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：R201AB 只接管无 scroll-out 的真实 grapheme 文本；
	// scrollback 和 full-replace 的 ownership 边界仍由独立切片证明。
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
				if !rawSharedGraphemeTextCell(cell) {
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

func rawSharedTallLinefeedNewlineTextDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	// 中文说明：ANSI LNM mode 20 只影响同批 vterm 如何把 LF 解码为 LF/CR；
	// history 不保存 mode truth，只消费 vterm 已输出的 ordered control/text 语义。
	if damage.SizeRows <= 2 || len(damage.ScrollbackAppend) > 0 || damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
		return false
	}
	hasMode20 := false
	hasText := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
			}
			hasText = true
		case vterm.ScreenOpControl:
			if op.Control != "lf" && op.Control != "ind" && op.Control != "soft-wrap" && op.Control != "cr" {
				return false
			}
		case vterm.ScreenOpModes:
			if op.Private || op.Mode != 20 {
				return false
			}
			hasMode20 = true
		default:
			return false
		}
	}
	return hasMode20 && hasText
}

func rawSharedDirectPrimaryTextDamageCanUseSemanticOps(damage vterm.WriteDamage) bool {
	if len(damage.SemanticOps) == 0 || damage.RequiresFullReplace || len(damage.AlternateAppend) > 0 {
		return false
	}
	hasText := false
	hasControlOrMode := false
	for _, op := range damage.SemanticOps {
		switch op.Code {
		case vterm.ScreenOpWriteSpan:
			if len(op.Cells) == 0 {
				continue
			}
			for _, cell := range op.Cells {
				if !rawSharedGraphemeTextCell(cell) {
					return false
				}
			}
			hasText = true
		case vterm.ScreenOpControl:
			if !rawSharedControlCanUseSemanticOp(op.Control) {
				return false
			}
			hasControlOrMode = true
		case vterm.ScreenOpModes:
			if !rawSharedModeCanUseSemanticOp(op) {
				return false
			}
			hasControlOrMode = true
		case vterm.ScreenOpScrollRect:
			if len(damage.ScrollbackAppend) == 0 || op.Dx != 0 || op.Dy >= 0 {
				return false
			}
			hasControlOrMode = true
		default:
			// 中文说明：这条捷径只允许 vterm print path 的 ordered 文本/控制；
			// screen diff clear/copy 或无 scroll-out 证据的 scroll 不能被提升为 history 语义。
			return false
		}
	}
	return hasText || hasControlOrMode
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

func rawSharedGraphemeTextCell(cell vterm.Cell) bool {
	if cell.Content == "" && cell.Width == 0 {
		return true
	}
	if cell.Content == "" {
		return false
	}
	for _, r := range cell.Content {
		if r == '\t' || r == '\n' || r == '\r' || r < 0x20 {
			return false
		}
	}
	return cell.Width >= 0
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
	publishSynchronizedFrame := false
	for _, damage := range damages {
		ops := semanticOpsForHistoryDamage(damage)
		if len(ops) == 0 && len(damage.ScrollbackAppend) == 0 {
			continue
		}
		scrollbackApplied := 0
		controlLinefeedInDamage := semanticOpsContainLinefeedControl(ops)
		lineOperationInDamage := semanticOpsContainAnyControl(ops, "il", "dl", "su", "sd")
		eraseDisplayInDamage := semanticOpsContainControl(ops, "ed")
		reverseIndexScrollDowns := semanticOpsReverseIndexScrollDownCount(ops)
		softWrapContinuation := false
		hardLineBreakPending := false
		nextWritePreserveLeadingColumns := false
		nextWriteUseActiveLogicalColumn := false
		logicalColumnOffsetActive := false
		logicalColumnOffsetRow := -1
		logicalColumnOffset := 0
		lastWriteRow := -1
		lastWriteEndCol := -1
		var pendingWriteCells []history.Cell
		pendingWriteRow := -1
		pendingWriteCol := -1
		pendingWritePreserveLeadingColumns := false
		flushPendingWrite := func() error {
			if len(pendingWriteCells) == 0 {
				return nil
			}
			targetCol := pendingWriteCol
			if hardLineBreakPending && !pendingWritePreserveLeadingColumns {
				// 中文说明：shared vterm 的 Row/Col 是当前屏幕坐标；普通 LF 后的
				// 下一条 logical line 必须从历史列 0 开始，不能继承上一行屏幕列。
				if pendingWriteCol > 0 {
					logicalColumnOffsetActive = true
					logicalColumnOffsetRow = pendingWriteRow
					logicalColumnOffset = pendingWriteCol
				}
				targetCol = 0
			} else if !pendingWritePreserveLeadingColumns && logicalColumnOffsetActive && pendingWriteRow == logicalColumnOffsetRow {
				targetCol = pendingWriteCol - logicalColumnOffset
				if targetCol < 0 {
					targetCol = 0
				}
			}
			if pendingWritePreserveLeadingColumns && pipeline.track.ActiveLineID() == 0 && targetCol > 0 {
				// 中文说明：vterm semantic write span 携带的是真实屏幕列；
				// 只有显式 cursor control 后的新行定位需要保留前导空白，普通换行不能借此继承屏幕列。
				padding := make([]history.Cell, targetCol)
				for i := range padding {
					padding[i] = history.Cell{Text: " ", Width: 1}
				}
				pendingWriteCells = append(padding, pendingWriteCells...)
				targetCol = 0
			}
			if softWrapContinuation {
				softWrapContinuation = false
			} else if !nextWriteUseActiveLogicalColumn && (pendingWriteRow != lastWriteRow || targetCol != lastWriteEndCol) {
				if err := pipeline.applyCursorPositionFromVTerm(pendingWriteRow, targetCol); err != nil {
					return err
				}
			}
			hardLineBreakPending = false
			nextWriteUseActiveLogicalColumn = false
			if err := pipeline.track.ApplyOwned(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: pendingWriteCells}); err != nil {
				return err
			}
			lastWriteRow = pendingWriteRow
			lastWriteEndCol = targetCol + historyCellsDisplayWidth(pendingWriteCells)
			pendingWriteCells = nil
			pendingWriteRow = -1
			pendingWriteCol = -1
			pendingWritePreserveLeadingColumns = false
			return nil
		}
		for _, op := range ops {
			switch op.Code {
			case vterm.ScreenOpWriteSpan:
				cells := historyCellsFromVTermCells(op.Cells)
				if len(cells) == 0 {
					continue
				}
				if len(pendingWriteCells) > 0 && op.Row == pendingWriteRow && op.Col == pendingWriteCol+historyCellsDisplayWidth(pendingWriteCells) {
					pendingWriteCells = appendMergedHistoryCells(pendingWriteCells, cells)
					continue
				}
				if err := flushPendingWrite(); err != nil {
					return publishSynchronizedFrame, err
				}
				pendingWriteCells = cells
				pendingWriteRow = op.Row
				pendingWriteCol = op.Col
				pendingWritePreserveLeadingColumns = nextWritePreserveLeadingColumns
				nextWritePreserveLeadingColumns = false
			case vterm.ScreenOpClearToEOL:
				if err := flushPendingWrite(); err != nil {
					return publishSynchronizedFrame, err
				}
				nextWritePreserveLeadingColumns = false
				if lineOperationInDamage {
					continue
				}
				lastWriteRow = -1
				lastWriteEndCol = -1
				if err := pipeline.applyCursorPositionFromVTerm(op.Row, op.Col); err != nil {
					return publishSynchronizedFrame, err
				}
				if err := pipeline.track.Apply(history.HistoryEvent{
					Kind:      history.EventEraseInLine,
					EraseMode: 0,
					EraseCols: pipeline.cols,
					Style:     historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
				}); err != nil {
					return publishSynchronizedFrame, err
				}
			case vterm.ScreenOpClearRect:
				if err := flushPendingWrite(); err != nil {
					return publishSynchronizedFrame, err
				}
				nextWritePreserveLeadingColumns = false
				if lineOperationInDamage {
					continue
				}
				lastWriteRow = -1
				lastWriteEndCol = -1
				if op.Rect.Width <= 0 || op.Rect.Height <= 0 {
					continue
				}
				if op.Rect.X == 0 && op.Rect.Width >= pipeline.cols {
					if err := pipeline.applyCursorPositionFromVTerm(op.Rect.Y, op.Rect.X); err != nil {
						return publishSynchronizedFrame, err
					}
					if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: 0}); err != nil {
						return publishSynchronizedFrame, err
					}
				}
			case vterm.ScreenOpScrollRect:
				if err := flushPendingWrite(); err != nil {
					return publishSynchronizedFrame, err
				}
				nextWritePreserveLeadingColumns = false
				if lineOperationInDamage {
					// 中文说明：IL/DL/SU/SD 已作为 ordered control 投影；
					// accompanying scroll rect 只是 live diff，不能再当 scroll-out 语义。
					continue
				}
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
					return publishSynchronizedFrame, err
				}
				scrollbackApplied += count
			case vterm.ScreenOpControl:
				if err := flushPendingWrite(); err != nil {
					return publishSynchronizedFrame, err
				}
				nextWritePreserveLeadingColumns = false
				nextWriteUseActiveLogicalColumn = false
				if vtermControlPreservesExplicitColumnForNextWrite(op.Control) || op.Control == "cuf" || op.Control == "cub" || op.Control == "bs" || op.Control == "cr" {
					hardLineBreakPending = false
				}
				if logicalColumnOffsetActive && op.Row == logicalColumnOffsetRow {
					if handled, err := pipeline.applyOffsetVTermControlEventLocked(op, logicalColumnOffset, &lastWriteEndCol, &nextWriteUseActiveLogicalColumn); err != nil {
						return publishSynchronizedFrame, err
					} else if handled {
						continue
					}
				}
				if op.Control != "soft-wrap" {
					lastWriteRow = -1
					lastWriteEndCol = -1
				}
				if op.Control == "ri" && reverseIndexScrollDowns > 0 {
					reverseIndexScrollDowns--
					continue
				}
				if op.TailFill != nil {
					if err := pipeline.track.Apply(history.HistoryEvent{
						Kind:  history.EventSetActiveLineTailFill,
						Style: historyStyleFromVTermStyle(*op.TailFill),
					}); err != nil {
						return publishSynchronizedFrame, err
					}
				}
				if err := pipeline.applyVTermControlEventLocked(op); err != nil {
					return publishSynchronizedFrame, err
				}
				nextWritePreserveLeadingColumns = vtermControlPreservesExplicitColumnForNextWrite(op.Control)
				if op.Control == "lf" || op.Control == "ind" || op.Control == "soft-wrap" {
					if scrollbackApplied < len(damage.ScrollbackAppend) {
						scrollbackApplied++
					}
					softWrapContinuation = op.Control == "soft-wrap"
					hardLineBreakPending = op.Control != "soft-wrap"
					if hardLineBreakPending {
						logicalColumnOffsetActive = false
						logicalColumnOffsetRow = -1
						logicalColumnOffset = 0
					}
				}
			case vterm.ScreenOpModes:
				if err := flushPendingWrite(); err != nil {
					return publishSynchronizedFrame, err
				}
				lastWriteRow = -1
				lastWriteEndCol = -1
				nextWritePreserveLeadingColumns = false
				nextWriteUseActiveLogicalColumn = false
				if op.Private && op.Mode == 2026 {
					if op.Enabled {
						if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventBeginSynchronizedFrame}); err != nil {
							return publishSynchronizedFrame, err
						}
					} else {
						if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEndSynchronizedFrame}); err != nil {
							return publishSynchronizedFrame, err
						}
						publishSynchronizedFrame = true
					}
				} else if err := pipeline.applyVTermModeEventLocked(op); err != nil {
					return publishSynchronizedFrame, err
				}
			}
		}
		if err := flushPendingWrite(); err != nil {
			return publishSynchronizedFrame, err
		}
		if eraseDisplayInDamage {
			// 中文说明：ED2 page-break / ED3 soft boundary 的 history 语义只由
			// ordered ed control 承接；vterm 伴随 scrollback append 只是 live surface 信号。
			continue
		}
		for ; scrollbackApplied < len(damage.ScrollbackAppend); scrollbackApplied++ {
			if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventPrimaryScrollOut, Count: 1}); err != nil {
				return publishSynchronizedFrame, err
			}
		}
	}
	return publishSynchronizedFrame, nil
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

func semanticOpsContainAnyControl(ops []vterm.DamageOp, controls ...string) bool {
	for _, control := range controls {
		if semanticOpsContainControl(ops, control) {
			return true
		}
	}
	return false
}

func vtermControlPreservesExplicitColumnForNextWrite(control string) bool {
	switch control {
	case "cup", "cha", "vpa", "ht", "cbt":
		return true
	default:
		return false
	}
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
	case "hts", "tbc", "decst8c", "scs", "decscusr", "dsr", "decxcpr", "decrqm", "da", "da2", "decstbm", "decslrm":
		// 中文说明：这些控制只改变 shared vterm 内部状态；history 不保存第二份
		// tab/charset/cursor-style/report/device-attributes/margin truth，后续
		// resolved cursor/write/scroll-out 语义会显式投影。
		return nil
	case "ris":
		// 中文说明：RIS 丢弃当前 primary mutable frontier，但不能从清屏后的
		// vterm screen 反推 committed history。
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventResetFrontier})
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
	case "cha", "cbt":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorHorizontalAbsolute, Count: op.Col + 1})
	case "cup", "vpa":
		return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorPosition, Row: op.Row + 1, Column: op.Col + 1})
	case "ech":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:  history.EventEraseCharacters,
			Count: op.Mode,
			Style: historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "dch":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:  history.EventDeleteCharacters,
			Count: op.Mode,
			Style: historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "ich":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:  history.EventInsertCharacters,
			Count: op.Mode,
			Style: historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "il":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:   history.EventInsertLines,
			Count:  op.Mode,
			Row:    op.Row,
			Bottom: op.Bottom,
			Style:  historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "dl":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:   history.EventDeleteLines,
			Count:  op.Mode,
			Row:    op.Row,
			Bottom: op.Bottom,
			Style:  historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "su":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:   history.EventScrollUpLines,
			Count:  op.Mode,
			Row:    op.Row,
			Bottom: op.Bottom,
			Style:  historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
	case "sd":
		return pipeline.track.Apply(history.HistoryEvent{
			Kind:   history.EventScrollDownLines,
			Count:  op.Mode,
			Row:    op.Row,
			Bottom: op.Bottom,
			Style:  historyStyleFromVTermStyle(opStyleFromClearToEOL(op)),
		})
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

func (pipeline *terminalHistoryPipeline) applyOffsetVTermControlEventLocked(op vterm.DamageOp, offset int, lastWriteEndCol *int, nextWriteUseActiveLogicalColumn *bool) (bool, error) {
	if lastWriteEndCol == nil || nextWriteUseActiveLogicalColumn == nil || *lastWriteEndCol < 0 {
		return false, nil
	}
	switch op.Control {
	case "cuf":
		count := op.Mode
		if count <= 0 {
			count = 1
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorForward, Count: count}); err != nil {
			return true, err
		}
		*lastWriteEndCol += count
		*nextWriteUseActiveLogicalColumn = true
		return true, nil
	case "cub":
		count := op.Mode
		if count <= 0 {
			count = 1
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorBackward, Count: count}); err != nil {
			return true, err
		}
		*lastWriteEndCol -= count
		if *lastWriteEndCol < 0 {
			*lastWriteEndCol = 0
		}
		*nextWriteUseActiveLogicalColumn = true
		return true, nil
	case "bs":
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorBackward, Count: 1}); err != nil {
			return true, err
		}
		if *lastWriteEndCol > 0 {
			*lastWriteEndCol--
		}
		*nextWriteUseActiveLogicalColumn = true
		return true, nil
	case "ht", "cha", "cbt":
		targetCol := op.Col - offset
		if targetCol < 0 {
			targetCol = 0
		}
		if op.Control == "ht" {
			// 中文说明：LF 后 history logical line 从 0 列开始，但真实 vterm
			// tabstop 仍按屏幕绝对列计算；offset 映射后若落在 logical tabstop
			// 之前，需要对齐到 history line 自身的下一个默认 tabstop。
			defaultTabCol := ((*lastWriteEndCol / 8) + 1) * 8
			if targetCol > *lastWriteEndCol && targetCol < defaultTabCol {
				targetCol = defaultTabCol
			}
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorHorizontalAbsolute, Count: targetCol + 1}); err != nil {
			return true, err
		}
		*lastWriteEndCol = targetCol
		*nextWriteUseActiveLogicalColumn = true
		return true, nil
	case "cup", "vpa":
		targetCol := op.Col - offset
		if targetCol < 0 {
			targetCol = 0
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorPosition, Row: op.Row + 1, Column: targetCol + 1}); err != nil {
			return true, err
		}
		*lastWriteEndCol = targetCol
		*nextWriteUseActiveLogicalColumn = true
		return true, nil
	default:
		return false, nil
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
	case 9, 1000, 1001, 1002, 1003, 1004, 1006, 2026:
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
		if cell.Width == 0 && len(out) > 0 && cell.Content != "" {
			next := history.Cell{
				Text:       cell.Content,
				Style:      historyStyleFromVTermStyle(cell.Style),
				LinkURL:    cell.LinkURL,
				LinkParams: cell.LinkParams,
			}
			if canMergeVTermCombiningHistoryCell(out[len(out)-1], next) {
				out[len(out)-1].Text += next.Text
				continue
			}
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

func appendMergedHistoryCells(left []history.Cell, right []history.Cell) []history.Cell {
	if len(left) == 0 {
		return right
	}
	for _, cell := range right {
		if canMergeVTermCombiningHistoryCell(left[len(left)-1], cell) {
			left[len(left)-1].Text += cell.Text
			continue
		}
		if canMergeVTermTextHistoryCells(left[len(left)-1], cell) {
			left[len(left)-1].Text += cell.Text
			left[len(left)-1].Width += cell.Width
			continue
		}
		left = append(left, cell)
	}
	return left
}

func canMergeVTermCombiningHistoryCell(left history.Cell, right history.Cell) bool {
	return left.Style == right.Style &&
		left.LinkURL == right.LinkURL &&
		left.LinkParams == right.LinkParams &&
		left.Text != "" &&
		right.Text != "" &&
		right.Width <= 1 &&
		containsOnlyCombiningMarks(right.Text)
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

func containsOnlyCombiningMarks(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.Is(unicode.Mn, r) && !unicode.Is(unicode.Me, r) && !unicode.Is(unicode.Mc, r) {
			return false
		}
	}
	return true
}

func opStyleFromClearToEOL(op vterm.DamageOp) vterm.CellStyle {
	if len(op.Cells) == 0 {
		return vterm.CellStyle{}
	}
	return op.Cells[0].Style
}
