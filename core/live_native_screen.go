package core

import (
	"time"

	vterm "github.com/anytty/anytty/vterm/vterm"
)

// LiveRevision 是 core-v2 为单个 terminal native screen 维护的单调版本。
// 它只描述 live display 投影版本，不是 logical-line history generation，也不能作为 history window stale guard。
type LiveRevision uint64

// NativeScreenSize 是 core-v2 live native screen 的尺寸投影。
// truth 来源是 terminal 当前 live SurfaceTrack/vterm size，不来自 TUI pane 或 renderer frame。
type NativeScreenSize struct {
	Cols int
	Rows int
}

// NativeScreenRow 是 core-v2 对外暴露的 native screen 单行 cell matrix。
// 这里保留 vterm cell 语义属性，protocol/TUI 只能把它当实时屏幕 projection，不能当 history truth。
type NativeScreenRow struct {
	Index int
	Cells []vterm.Cell
}

// NativeScreenRowCopy reuses exact rows from the client's base screen. All
// copies are evaluated from the unchanged base before replacement rows apply.
type NativeScreenRowCopy struct {
	SourceRow      int
	DestinationRow int
	Count          int
}

// NativeScreenSnapshot 是 core-v2 当前 native screen 的 latest-only 快照。
// 它由 Terminal 从 live SurfaceTrack 读取；不携带 scrollback、history token 或 TUI view 状态。
type NativeScreenSnapshot struct {
	TerminalID   string
	BaseRevision LiveRevision
	Revision     LiveRevision
	FullReplace  bool
	Size         NativeScreenSize
	RowCopies    []NativeScreenRowCopy
	Rows         []NativeScreenRow
	Cursor       vterm.CursorState
	Modes        vterm.TerminalModes
	AltScreen    bool
	Timestamp    time.Time
}

const (
	liveScreenChangeLogMaxRevisions = 64
	liveScreenChangeLogMaxMarkers   = 4096
)

type liveScreenChange struct {
	BaseRevision LiveRevision
	Revision     LiveRevision
	RowCopies    []NativeScreenRowCopy
	ReplacedRows []int
}

type liveScreenChangeLog struct {
	entries []liveScreenChange
	markers int
}

func (log *liveScreenChangeLog) reset() {
	log.entries = nil
	log.markers = 0
}

// append adds one terminal-global revision and returns the oldest revision that
// can still be used as a delta base after bounded eviction.
func (log *liveScreenChangeLog) append(change liveScreenChange) LiveRevision {
	markers := len(change.RowCopies) + len(change.ReplacedRows)
	if markers > liveScreenChangeLogMaxMarkers {
		log.reset()
		return change.Revision
	}
	log.entries = append(log.entries, change)
	log.markers += markers
	floor := LiveRevision(0)
	for len(log.entries) > liveScreenChangeLogMaxRevisions || log.markers > liveScreenChangeLogMaxMarkers {
		removed := log.entries[0]
		log.entries = log.entries[1:]
		log.markers -= len(removed.RowCopies) + len(removed.ReplacedRows)
		floor = removed.Revision
	}
	return floor
}

func (log *liveScreenChangeLog) compose(observed, current LiveRevision, rows int) ([]NativeScreenRowCopy, []int, bool) {
	if observed == current {
		return nil, nil, true
	}
	first := -1
	for index := range log.entries {
		if log.entries[index].BaseRevision == observed {
			first = index
			break
		}
	}
	if first < 0 {
		return nil, nil, false
	}
	sources := make([]int, rows)
	for row := range sources {
		sources[row] = row
	}
	expected := observed
	for _, change := range log.entries[first:] {
		if change.BaseRevision != expected {
			return nil, nil, false
		}
		previous := append([]int(nil), sources...)
		for _, rowCopy := range change.RowCopies {
			for offset := 0; offset < rowCopy.Count; offset++ {
				sourceRow := rowCopy.SourceRow + offset
				destinationRow := rowCopy.DestinationRow + offset
				if sourceRow < 0 || sourceRow >= rows || destinationRow < 0 || destinationRow >= rows {
					return nil, nil, false
				}
				sources[destinationRow] = previous[sourceRow]
			}
		}
		for _, row := range change.ReplacedRows {
			if row < 0 || row >= rows {
				return nil, nil, false
			}
			sources[row] = -1
		}
		expected = change.Revision
		if expected == current {
			break
		}
	}
	if expected != current {
		return nil, nil, false
	}
	var copies []NativeScreenRowCopy
	var replacements []int
	for destinationRow, sourceRow := range sources {
		if sourceRow < 0 {
			replacements = append(replacements, destinationRow)
			continue
		}
		if sourceRow == destinationRow {
			continue
		}
		if len(copies) > 0 {
			last := &copies[len(copies)-1]
			if last.SourceRow+last.Count == sourceRow && last.DestinationRow+last.Count == destinationRow {
				last.Count++
				continue
			}
		}
		copies = append(copies, NativeScreenRowCopy{SourceRow: sourceRow, DestinationRow: destinationRow, Count: 1})
	}
	return copies, replacements, true
}

// LiveScreenInvalidated 是 core 发给客户端的 live screen 唤醒信号。
// 它只表示 terminal 的 latest native screen 至少已经变到 Revision；客户端应自行拉取最新 snapshot。
type LiveScreenInvalidated struct {
	TerminalID string
	Revision   LiveRevision
}
