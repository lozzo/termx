package termxcorev2

import (
	"sync"

	"github.com/lozzow/termx/termx-core-v2/history"
)

var terminalHistoryPipelineBeforeIngestHook func()

// terminalHistoryPipeline 串行维护历史 parser 和 HistoryTrack。
// live surface 不走这把锁，避免大批历史写入挡住 attach 的实时快照。
type terminalHistoryPipeline struct {
	mu     sync.Mutex
	track  *history.HistoryTrack
	ingest historyANSIParser
}

func newTerminalHistoryPipeline(rows int) *terminalHistoryPipeline {
	track := history.NewHistoryTrack()
	track.SetPrimaryScreenRows(rows)
	return &terminalHistoryPipeline{track: track}
}

func (pipeline *terminalHistoryPipeline) Ingest(output string) error {
	if terminalHistoryPipelineBeforeIngestHook != nil {
		terminalHistoryPipelineBeforeIngestHook()
	}
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	for _, segment := range pipeline.ingest.Parse(output) {
		if err := pipeline.applySegment(segment); err != nil {
			return err
		}
	}
	return nil
}

func (pipeline *terminalHistoryPipeline) Resize(rows int, event history.HistoryEvent) error {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	pipeline.track.SetPrimaryScreenRows(rows)
	return pipeline.track.Apply(event)
}

func (pipeline *terminalHistoryPipeline) ForceCommitFrontier() error {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier})
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

func (pipeline *terminalHistoryPipeline) FreezeSnapshot() history.FrozenSnapshot {
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	return pipeline.track.FreezeSnapshot()
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
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: segment.Cells}); err != nil {
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
	if segment.EraseInLine {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInLine, EraseMode: segment.EraseMode}); err != nil {
			return err
		}
	}
	if segment.EraseInDisplay {
		if err := pipeline.track.Apply(history.HistoryEvent{Kind: history.EventEraseInDisplay, EraseMode: segment.EraseMode}); err != nil {
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
