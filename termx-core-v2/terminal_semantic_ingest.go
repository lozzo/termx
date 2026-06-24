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

func resetTerminalSemanticIngestTestHooks() {
	terminalSemanticIngestBatchHook = nil
}

func terminalSemanticBatchesFromSurfaceResult(result live.SurfaceWriteResult, size live.SurfaceSize) []terminalSemanticBatch {
	if len(result.Segments) == 0 {
		return nil
	}
	batches := make([]terminalSemanticBatch, 0, len(result.Segments))
	for _, segment := range result.Segments {
		if segment.Raw == "" && len(segment.AltScreenExitFrame) == 0 {
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
	if batch.Raw == "" && len(batch.AltExitFrame) == 0 {
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
	if batch.Raw != "" {
		var err error
		if batch.FromSharedVTerm {
			// 中文说明：shared vterm 已经给出 alt-screen final-frame 和 mode
			// damage，不能再启动旧 altCap 二次捕获，否则同一 final frame 会进历史两次。
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
	return nil
}
