package termxcorev2

import "github.com/lozzow/termx/termx-core-v2/history"

// ModuleName 是 v2 core module 的稳定标识。
const ModuleName = "termx-core-v2"

// SmokeHistoryWindow 跑通 core-v2 logical line -> HistoryWindow 的最小路径。
func SmokeHistoryWindow() (history.HistoryWindow, error) {
	track := history.NewHistoryTrack()
	if err := track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{{Text: "termx-core-v2"}}}); err != nil {
		return history.HistoryWindow{}, err
	}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		return history.HistoryWindow{}, err
	}
	return track.LatestWindow(history.HistoryWindowRequest{Cols: 80, Rows: 10})
}
