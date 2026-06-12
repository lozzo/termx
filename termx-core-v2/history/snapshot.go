package history

import "fmt"

// FrozenSnapshot 是 copy/history 进入时刻的冻结 logical-line 视图。
// 它不是第二份历史 truth，只是 protocol session 在当下 generation 上
// pin 住的一段只读 logical-line 序列，后续 older 分页只在这份序列内移动。
type FrozenSnapshot struct {
	Token      string
	Generation Generation
	Lines      []SnapshotLine
}

func (track *HistoryTrack) FreezeSnapshot() FrozenSnapshot {
	ids := track.committed.IDs()
	for _, id := range track.frontier.IDs() {
		if !track.frontier.IsHidden(id) && !containsLineID(ids, id) {
			ids = append(ids, id)
		}
	}
	lines := make([]SnapshotLine, 0, len(ids))
	for _, id := range ids {
		line, ok := track.store.Line(id)
		if !ok {
			continue
		}
		lines = append(lines, SnapshotLine{
			Line:      line.Clone(),
			Committed: track.committed.Contains(id),
		})
	}
	return FrozenSnapshot{
		Token:      makeSnapshotToken(track.generation),
		Generation: track.generation,
		Lines:      lines,
	}
}

func makeSnapshotToken(generation Generation) string {
	return fmt.Sprintf("snap:g%d", generation)
}
