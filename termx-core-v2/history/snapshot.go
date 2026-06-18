package history

import (
	"fmt"
	"sort"
)

// FrozenSnapshot 是 copy/history 进入时刻的冻结 logical-line 视图。
// 它不是第二份历史 truth，只是 protocol session 在当下 generation 上
// pin 住的一段只读 logical-line 序列，后续 older 分页只在这份序列内移动。
type FrozenSnapshot struct {
	Token          string
	Generation     Generation
	ObserverEpoch  ObserverEpoch
	Lines          []SnapshotLine
	CommittedLines int
	CommittedFirst LogicalLineID
	CommittedIDs   []LogicalLineID
	CommittedUpper LogicalLineID
	FrozenFrontier []SnapshotLine
	store          LogicalLineStore
	detached       bool
}

func (track *HistoryTrack) FreezeSnapshot() FrozenSnapshot {
	return track.freezeSnapshot(true)
}

func (track *HistoryTrack) FreezePinnedSnapshot() FrozenSnapshot {
	return track.freezeSnapshot(false)
}

func (track *HistoryTrack) FreezePinnedSnapshotAtGeneration(generation Generation) FrozenSnapshot {
	return track.freezeSnapshotAtGeneration(false, generation)
}

func NewDetachedFrozenSnapshot(token string, generation Generation, lines []SnapshotLine) FrozenSnapshot {
	cloned := make([]SnapshotLine, 0, len(lines))
	committed := 0
	for _, line := range lines {
		line.Line = line.Line.Clone()
		if line.Committed {
			committed++
		}
		cloned = append(cloned, line)
	}
	return FrozenSnapshot{
		Token:          token,
		Generation:     generation,
		Lines:          cloned,
		CommittedLines: committed,
		detached:       true,
	}
}

func (track *HistoryTrack) freezeSnapshot(detach bool) FrozenSnapshot {
	return track.freezeSnapshotAtGeneration(detach, 0)
}

func (track *HistoryTrack) freezeSnapshotAtGeneration(detach bool, generation Generation) FrozenSnapshot {
	committedIDs := track.committed.IDs()
	if generation > 0 {
		committedIDs = track.lineIDsAtOrBeforeGeneration(committedIDs, generation)
	}
	frontierIDs := make([]LogicalLineID, 0)
	for _, id := range track.frontier.IDs() {
		if !track.frontier.IsHidden(id) && !containsLineID(committedIDs, id) && track.lineAtOrBeforeGeneration(id, generation) {
			frontierIDs = append(frontierIDs, id)
		}
	}
	observerEpoch := ObserverEpoch(0)
	if !detach {
		observerEpoch = track.acquireObserver(committedIDs, frontierIDs)
	}
	snapshot := FrozenSnapshot{
		Token:          makeSnapshotToken(track.generation),
		Generation:     track.generation,
		ObserverEpoch:  observerEpoch,
		CommittedLines: len(committedIDs),
		store:          track.store,
		detached:       detach,
	}
	if len(committedIDs) > 0 {
		snapshot.CommittedFirst = committedIDs[0]
		snapshot.CommittedUpper = committedIDs[len(committedIDs)-1]
		if !lineIDsContiguous(committedIDs) {
			snapshot.CommittedIDs = cloneLineIDs(committedIDs)
		}
	}
	if detach && len(snapshot.CommittedIDs) == 0 {
		snapshot.CommittedIDs = cloneLineIDs(committedIDs)
	}
	for _, id := range frontierIDs {
		line, ok := snapshotLine(track.store, id, detach, observerEpoch)
		if !ok {
			continue
		}
		snapshot.FrozenFrontier = append(snapshot.FrozenFrontier, SnapshotLine{
			// 中文说明：只有 still-mutable frontier 需要冻结 payload；committed history
			// 已经在 store 中稳定存在，protocol pin 只记录 ID 边界，分页时按需取。
			Line:      line,
			Committed: false,
		})
	}
	if detach {
		snapshot.materializeDetachedLines()
	}
	return snapshot
}

func (track *HistoryTrack) lineIDsAtOrBeforeGeneration(ids []LogicalLineID, generation Generation) []LogicalLineID {
	if generation == 0 || len(ids) == 0 {
		return ids
	}
	filtered := make([]LogicalLineID, 0, len(ids))
	for _, id := range ids {
		if track.lineAtOrBeforeGeneration(id, generation) {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func (track *HistoryTrack) lineAtOrBeforeGeneration(id LogicalLineID, generation Generation) bool {
	if generation == 0 {
		return true
	}
	line, ok := track.store.Line(id)
	if !ok {
		return false
	}
	contentGeneration := line.ContentGeneration
	if contentGeneration == 0 {
		contentGeneration = line.CreatedGeneration
	}
	if contentGeneration == 0 {
		return true
	}
	return contentGeneration <= generation
}

func (snapshot *FrozenSnapshot) materializeDetachedLines() {
	if len(snapshot.Lines) > 0 {
		return
	}
	lines := make([]SnapshotLine, 0, len(snapshot.CommittedIDs)+len(snapshot.FrozenFrontier))
	for _, id := range snapshot.CommittedIDs {
		line, ok := snapshotLine(snapshot.store, id, true, snapshot.ObserverEpoch)
		if !ok {
			continue
		}
		lines = append(lines, SnapshotLine{Line: line, Committed: true})
	}
	lines = append(lines, snapshot.FrozenFrontier...)
	snapshot.Lines = lines
	snapshot.store = nil
	snapshot.ObserverEpoch = 0
}

func (snapshot FrozenSnapshot) VisibleLineCount() int {
	if len(snapshot.Lines) > 0 {
		return len(snapshot.Lines)
	}
	return snapshot.CommittedLines + len(snapshot.FrozenFrontier)
}

func (snapshot FrozenSnapshot) LineAt(index int) (SnapshotLine, bool) {
	if index < 0 {
		return SnapshotLine{}, false
	}
	if len(snapshot.Lines) > 0 {
		if index >= len(snapshot.Lines) {
			return SnapshotLine{}, false
		}
		return snapshot.Lines[index], true
	}
	if index < snapshot.CommittedLines {
		id, ok := snapshot.committedIDAt(index)
		if !ok {
			return SnapshotLine{}, false
		}
		line, ok := snapshotLine(snapshot.store, id, false, snapshot.ObserverEpoch)
		if !ok {
			return SnapshotLine{}, false
		}
		return SnapshotLine{Line: line, Committed: true}, true
	}
	frontierIndex := index - snapshot.CommittedLines
	if frontierIndex < 0 || frontierIndex >= len(snapshot.FrozenFrontier) {
		return SnapshotLine{}, false
	}
	return snapshot.FrozenFrontier[frontierIndex], true
}

func (snapshot FrozenSnapshot) LineIndex(id LogicalLineID) (int, bool) {
	if id == 0 {
		return -1, false
	}
	if len(snapshot.Lines) > 0 {
		for index, line := range snapshot.Lines {
			if line.Line.ID == id {
				return index, true
			}
		}
		return -1, false
	}
	if index, ok := snapshot.committedLineIndex(id); ok {
		return index, true
	}
	for index, line := range snapshot.FrozenFrontier {
		if line.Line.ID == id {
			return snapshot.CommittedLines + index, true
		}
	}
	return -1, false
}

func (snapshot FrozenSnapshot) CommittedLineIDs() []LogicalLineID {
	if len(snapshot.Lines) > 0 {
		ids := make([]LogicalLineID, 0, snapshot.CommittedLines)
		for _, line := range snapshot.Lines {
			if line.Committed {
				ids = append(ids, line.Line.ID)
			}
		}
		return ids
	}
	if len(snapshot.CommittedIDs) > 0 {
		return cloneLineIDs(snapshot.CommittedIDs)
	}
	if snapshot.CommittedFirst == 0 || snapshot.CommittedLines <= 0 {
		return nil
	}
	ids := make([]LogicalLineID, snapshot.CommittedLines)
	for i := range ids {
		ids[i] = snapshot.CommittedFirst + LogicalLineID(i)
	}
	return ids
}

func (snapshot FrozenSnapshot) committedIDAt(index int) (LogicalLineID, bool) {
	if index < 0 || index >= snapshot.CommittedLines {
		return 0, false
	}
	if len(snapshot.CommittedIDs) > 0 {
		if index >= len(snapshot.CommittedIDs) {
			return 0, false
		}
		return snapshot.CommittedIDs[index], true
	}
	if snapshot.CommittedFirst == 0 {
		return 0, false
	}
	return snapshot.CommittedFirst + LogicalLineID(index), true
}

func (snapshot FrozenSnapshot) committedLineIndex(id LogicalLineID) (int, bool) {
	if id == 0 || snapshot.CommittedLines <= 0 {
		return -1, false
	}
	if len(snapshot.CommittedIDs) > 0 {
		index := sort.Search(len(snapshot.CommittedIDs), func(i int) bool {
			return snapshot.CommittedIDs[i] >= id
		})
		if index < len(snapshot.CommittedIDs) && snapshot.CommittedIDs[index] == id {
			return index, true
		}
		return -1, false
	}
	if snapshot.CommittedFirst == 0 || id < snapshot.CommittedFirst || id > snapshot.CommittedUpper {
		return -1, false
	}
	index := int(id - snapshot.CommittedFirst)
	if index < 0 || index >= snapshot.CommittedLines {
		return -1, false
	}
	return index, true
}

func (snapshot FrozenSnapshot) ReleaseObserver() {
	if snapshot.ObserverEpoch == 0 || snapshot.store == nil {
		return
	}
	if store, ok := snapshot.store.(observerLineStore); ok {
		store.ReleaseObserver(snapshot.ObserverEpoch)
	}
}

func lineIDsContiguous(ids []LogicalLineID) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[i-1]+1 {
			return false
		}
	}
	return true
}

func makeSnapshotToken(generation Generation) string {
	return fmt.Sprintf("snap:g%d", generation)
}

type snapshotLineStore interface {
	SnapshotLine(LogicalLineID) (LogicalLine, bool)
}

type observerLineStore interface {
	AcquireObserver(ObserverLineVisibility) ObserverEpoch
	ReleaseObserver(ObserverEpoch)
	ObserverSnapshotLine(LogicalLineID, ObserverEpoch) (LogicalLine, bool)
}

func (track *HistoryTrack) acquireObserver(committedIDs []LogicalLineID, frontierIDs []LogicalLineID) ObserverEpoch {
	if store, ok := track.store.(observerLineStore); ok {
		visibility := ObserverLineVisibility{IDs: frontierIDs}
		if len(committedIDs) > 0 {
			visibility.First = committedIDs[0]
			visibility.Upper = committedIDs[len(committedIDs)-1]
		}
		return store.AcquireObserver(visibility)
	}
	return 0
}

func (track *HistoryTrack) ReleaseObserver(epoch ObserverEpoch) {
	if epoch == 0 {
		return
	}
	if store, ok := track.store.(observerLineStore); ok {
		store.ReleaseObserver(epoch)
	}
}

func snapshotLine(store LogicalLineStore, id LogicalLineID, detach bool, observerEpoch ObserverEpoch) (LogicalLine, bool) {
	if observerEpoch > 0 {
		if observerStore, ok := store.(observerLineStore); ok {
			line, ok := observerStore.ObserverSnapshotLine(id, observerEpoch)
			if !ok {
				return LogicalLine{}, false
			}
			if detach {
				return line.Clone(), true
			}
			return line, true
		}
	}
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
