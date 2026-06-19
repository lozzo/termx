package termxcorev2

import (
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

var terminalHistoryPipelineBeforeIngestHook func()

// terminalHistoryPipeline 串行维护历史 parser 和 HistoryTrack。
// live surface 不走这把锁，避免大批历史写入挡住 attach 的实时快照。
type terminalHistoryPipeline struct {
	mu     sync.Mutex
	track  *history.HistoryTrack
	ingest historyANSIParser
	altCap *live.SurfaceTrack
	cols   int
	rows   int
	// 中文说明：live snapshot 只需要知道 history 已经完成到哪个 generation；
	// 这里用原子投影，避免实时刷新为了读 generation 去等 history parser 锁。
	lastCompletedGeneration atomic.Uint64
}

func newTerminalHistoryPipeline(cols int, rows int) *terminalHistoryPipeline {
	track := history.NewHistoryTrack()
	track.SetPrimaryScreenRows(rows)
	pipeline := &terminalHistoryPipeline{
		track:  track,
		altCap: live.NewSurfaceTrack(live.SurfaceSize{Cols: cols, Rows: rows}),
		cols:   cols,
		rows:   rows,
	}
	pipeline.publishGenerationLocked()
	return pipeline
}

func (pipeline *terminalHistoryPipeline) Ingest(output string) error {
	if terminalHistoryPipelineBeforeIngestHook != nil {
		terminalHistoryPipelineBeforeIngestHook()
	}
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	pipeline.ingest.SetScreenSize(pipeline.cols, pipeline.rows)
	result := pipeline.altCap.WriteWithResult(output)
	for _, writeSegment := range result.Segments {
		if writeSegment.Raw != "" {
			for _, segment := range pipeline.ingest.Parse(writeSegment.Raw) {
				if err := pipeline.applySegment(segment); err != nil {
					return err
				}
			}
		}
		if len(writeSegment.AltScreenExitFrame) > 0 {
			if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventAppendAltScreenFrame, Rows: historyRowsFromVTermRows(writeSegment.AltScreenExitFrame)}); err != nil {
				return err
			}
		}
	}
	pipeline.publishGenerationLocked()
	return nil
}

func (pipeline *terminalHistoryPipeline) Resize(cols int, rows int, event history.HistoryEvent) error {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	pipeline.cols = cols
	pipeline.rows = rows
	pipeline.ingest.SetScreenSize(cols, rows)
	pipeline.altCap.Resize(live.SurfaceSize{Cols: cols, Rows: rows})
	pipeline.track.SetPrimaryScreenRows(rows)
	if err := pipeline.track.Apply(event); err != nil {
		return err
	}
	pipeline.publishGenerationLocked()
	return nil
}

func (pipeline *terminalHistoryPipeline) ResetForRestart() error {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	// 中文说明：restart 是进程边界，不是 terminal history 边界。保留同一
	// HistoryTrack，但必须切断旧进程的 primary screen ownership 和 alt-screen 状态。
	if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		return err
	}
	if pipeline.track.InAltScreen() {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: false}); err != nil {
			return err
		}
	}
	if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: 2}); err != nil {
		return err
	}
	if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventNonHistoryBoundary}); err != nil {
		return err
	}
	pipeline.ingest = historyANSIParser{}
	pipeline.ingest.SetScreenSize(pipeline.cols, pipeline.rows)
	pipeline.altCap = live.NewSurfaceTrack(live.SurfaceSize{Cols: pipeline.cols, Rows: pipeline.rows})
	pipeline.publishGenerationLocked()
	return nil
}

func (pipeline *terminalHistoryPipeline) ForceCommitFrontier() error {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		return err
	}
	pipeline.publishGenerationLocked()
	return nil
}

func (pipeline *terminalHistoryPipeline) AppendSystemLines(lines []string) error {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	// 中文说明：terminal lifecycle marker 是 core 生成的系统输出边界，
	// 不是从 live snapshot 反推历史；每行作为独立 logical line 追加并立即提交。
	if pipeline.track.InAltScreen() {
		// 中文说明：进程退出 marker 属于 primary lifecycle 数据；即使进程死在
		// alt-screen，也不能被普通 primary write 的 alt-screen guard 吞掉。
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: false}); err != nil {
			return err
		}
	}
	for _, line := range lines {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{{
			Text:  line,
			Width: historyCellTextWidthForTerminal(line),
		}}}); err != nil {
			return err
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSealLogicalLine}); err != nil {
			return err
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
			return err
		}
	}
	pipeline.publishGenerationLocked()
	return nil
}

func (pipeline *terminalHistoryPipeline) LatestWindow(cols int, rows int) (history.HistoryWindow, error) {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.LatestWindow(history.HistoryWindowRequest{Cols: cols, Rows: rows})
}

func (pipeline *terminalHistoryPipeline) OlderWindow(cols int, rows int, cursor history.HistoryCursor) (history.HistoryWindow, error) {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.OlderWindow(history.HistoryWindowRequest{Cols: cols, Rows: rows, Cursor: cursor})
}

func (pipeline *terminalHistoryPipeline) CommittedCursorValid(cols int, cursor history.HistoryCursor) bool {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.CommittedCursorValid(cols, cursor)
}

func (pipeline *terminalHistoryPipeline) Generation() history.Generation {
	return history.Generation(pipeline.lastCompletedGeneration.Load())
}

func (pipeline *terminalHistoryPipeline) FreezeSnapshot() history.FrozenSnapshot {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.FreezeSnapshot()
}

func (pipeline *terminalHistoryPipeline) FreezePinnedSnapshot() history.FrozenSnapshot {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.FreezePinnedSnapshot()
}

func (pipeline *terminalHistoryPipeline) FreezePinnedSnapshotAtGeneration(generation history.Generation) history.FrozenSnapshot {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.FreezePinnedSnapshotAtGeneration(generation)
}

func (pipeline *terminalHistoryPipeline) Line(id history.LogicalLineID) (history.LogicalLine, bool) {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.Line(id)
}

func (pipeline *terminalHistoryPipeline) LineIDs() []history.LogicalLineID {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.LineIDs()
}

func (pipeline *terminalHistoryPipeline) RetainedHistoryLineCount() int {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.RetainedLineCount()
}

func (pipeline *terminalHistoryPipeline) CommittedIDs() []history.LogicalLineID {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.CommittedIDs()
}

func (pipeline *terminalHistoryPipeline) FrontierIDs() []history.LogicalLineID {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.FrontierIDs()
}

func (pipeline *terminalHistoryPipeline) CommittableIDs() []history.LogicalLineID {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.CommittableIDs()
}

func (pipeline *terminalHistoryPipeline) applySegment(segment historyOutputSegment) error {
	if len(segment.Cells) > 0 {
		if err := pipeline.track.ApplyOwned(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: segment.Cells}); err != nil {
			return err
		}
	}
	if segment.CarriageReturn {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCarriageReturn}); err != nil {
			return err
		}
	}
	if segment.CursorForward {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorForward, Count: segment.Count}); err != nil {
			return err
		}
	}
	if segment.CursorBackward {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorBackward, Count: segment.Count}); err != nil {
			return err
		}
	}
	if segment.CursorHorizontalAbsolute {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorHorizontalAbsolute, Count: segment.Count}); err != nil {
			return err
		}
	}
	if segment.CursorUp {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorUp, Count: segment.Count}); err != nil {
			return err
		}
	}
	if segment.CursorDown {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorDown, Count: segment.Count}); err != nil {
			return err
		}
	}
	if segment.CursorPosition {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCursorPosition, Row: segment.Row, Column: segment.Column}); err != nil {
			return err
		}
	}
	if segment.EraseInLine {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInLine, EraseMode: segment.EraseMode, EraseCols: pipeline.cols, Style: segment.Style}); err != nil {
			return err
		}
	}
	if segment.EraseInDisplay {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: segment.EraseMode}); err != nil {
			return err
		}
	}
	if segment.SetTailFill {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSetActiveLineTailFill, Style: segment.Style}); err != nil {
			return err
		}
	}
	if segment.SwitchAltScreen {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSwitchAltScreen, EnterAltScreen: segment.EnterAltScreen}); err != nil {
			return err
		}
	}
	if segment.Seal {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventSealLogicalLine}); err != nil {
			return err
		}
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventCommitFrontier}); err != nil {
			return err
		}
	}
	return nil
}

func (pipeline *terminalHistoryPipeline) publishGenerationLocked() {
	pipeline.lastCompletedGeneration.Store(uint64(pipeline.track.Generation()))
}

func historyRowsFromVTermRows(rows [][]vterm.Cell) [][]history.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]history.Cell, len(rows))
	for rowIndex, row := range rows {
		if len(row) == 0 {
			continue
		}
		out[rowIndex] = make([]history.Cell, 0, len(row))
		for _, cell := range row {
			if cell.Content == "" && cell.Width == 0 {
				continue
			}
			text := cell.Content
			if text == "" {
				text = " "
			}
			width := cell.Width
			if width <= 0 {
				width = 1
			}
			out[rowIndex] = append(out[rowIndex], history.Cell{
				Text:       text,
				Width:      width,
				Style:      historyStyleFromVTermStyle(cell.Style),
				LinkURL:    cell.LinkURL,
				LinkParams: cell.LinkParams,
			})
		}
	}
	return out
}

func historyStyleFromVTermStyle(style vterm.CellStyle) history.CellStyle {
	return history.CellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}
