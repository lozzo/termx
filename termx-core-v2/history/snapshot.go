package history

import "fmt"

// FrozenSnapshot 是 copy/history 进入时刻的冻结 logical-line 视图。
// 它不是第二份历史 truth，只是 protocol session 在当下 generation 上
// pin 住的一段只读 logical-line 序列，后续 older 分页只在这份序列内移动。
type FrozenSnapshot struct {
	Token          string
	Generation     Generation
	Lines          []SnapshotLine
	CommittedLines int
}

func (track *HistoryTrack) FreezeSnapshot() FrozenSnapshot {
	return track.freezeSnapshot(true)
}

func (track *HistoryTrack) FreezePinnedSnapshot() FrozenSnapshot {
	return track.freezeSnapshot(false)
}

func (track *HistoryTrack) freezeSnapshot(detach bool) FrozenSnapshot {
	committedIDs := track.committed.IDs()
	ids := cloneLineIDs(committedIDs)
	for _, id := range track.frontier.IDs() {
		if !track.frontier.IsHidden(id) && !containsLineID(ids, id) {
			ids = append(ids, id)
		}
	}
	lines := make([]SnapshotLine, 0, len(ids))
	committedLines := 0
	for _, id := range ids {
		line, ok := snapshotLine(track.store, id, detach)
		if !ok {
			continue
		}
		committed := track.committed.Contains(id)
		if committed {
			committedLines++
		}
		lines = append(lines, SnapshotLine{
			// 中文说明：MemoryStorageBackend 保存时已经 copy-on-write；冻结快照只复制
			// line header，不再重复复制整段 cell payload，避免 protocol 大历史 copy 入口内存翻倍。
			Line:      line,
			Committed: committed,
		})
	}
	return FrozenSnapshot{
		Token:          makeSnapshotToken(track.generation),
		Generation:     track.generation,
		Lines:          lines,
		CommittedLines: committedLines,
	}
}

func makeSnapshotToken(generation Generation) string {
	return fmt.Sprintf("snap:g%d", generation)
}

type snapshotLineStore interface {
	SnapshotLine(LogicalLineID) (LogicalLine, bool)
}

func snapshotLine(store LogicalLineStore, id LogicalLineID, detach bool) (LogicalLine, bool) {
	if !detach {
		if snapshotStore, ok := store.(snapshotLineStore); ok {
			return snapshotStore.SnapshotLine(id)
		}
	}
	if snapshotStore, ok := store.(snapshotLineStore); ok {
		line, ok := snapshotStore.SnapshotLine(id)
		if !ok {
			return LogicalLine{}, false
		}
		return line.Clone(), true
	}
	return store.Line(id)
}
