package history

import "sort"

// CommittedHistoryIndex is the ordered set of logical lines currently counted
// as authoritative committed history.
type CommittedHistoryIndex struct {
	firstID    LogicalLineID
	rangeCount int
	ids        []LogicalLineID
	present    map[LogicalLineID]struct{}
	sorted     bool
	generation Generation
}

func NewCommittedHistoryIndex() *CommittedHistoryIndex {
	return &CommittedHistoryIndex{
		sorted: true,
	}
}

func (index *CommittedHistoryIndex) Append(id LogicalLineID) error {
	if id == 0 {
		return ErrInvalidLineID
	}
	if index.Contains(id) {
		return ErrDuplicateLineID
	}
	if index.isRange() {
		nextID := index.firstID + LogicalLineID(index.rangeCount)
		if id == nextID {
			index.rangeCount++
			index.bumpGeneration()
			return nil
		}
		index.materializeRange()
	}
	if index.rangeCount == 0 && len(index.ids) == 0 {
		// 中文说明：压力日志是严格递增 logical line id，用 range 保存可避免
		// 100 万行历史长期持有一份 8MB+ committed id slice。
		index.firstID = id
		index.rangeCount = 1
		index.sorted = true
		index.bumpGeneration()
		return nil
	}
	if len(index.ids) > 0 && id < index.ids[len(index.ids)-1] {
		// 中文说明：正常 history commit 按 logical line id 单调追加，二分查找即可；
		// 非单调追加只在异常/测试形态下懒建 map，避免 100k 顺序历史长期双份索引。
		index.sorted = false
		index.ensurePresentMap()
	}
	index.ids = append(index.ids, id)
	if index.present != nil {
		index.present[id] = struct{}{}
	}
	index.bumpGeneration()
	return nil
}

func (index *CommittedHistoryIndex) Remove(id LogicalLineID) bool {
	if index.isRange() {
		if !index.rangeContains(id) {
			return false
		}
		if index.rangeCount == 1 {
			index.firstID = 0
			index.rangeCount = 0
			index.bumpGeneration()
			return true
		}
		if id == index.firstID {
			index.firstID++
			index.rangeCount--
			index.bumpGeneration()
			return true
		}
		lastID := index.firstID + LogicalLineID(index.rangeCount-1)
		if id == lastID {
			index.rangeCount--
			index.bumpGeneration()
			return true
		}
		index.materializeRange()
	}
	position, ok := index.position(id)
	if !ok {
		return false
	}
	if index.present != nil {
		delete(index.present, id)
	}
	copy(index.ids[position:], index.ids[position+1:])
	index.ids = index.ids[:len(index.ids)-1]
	index.bumpGeneration()
	return true
}

func (index *CommittedHistoryIndex) Clear() bool {
	if index.Len() == 0 {
		return false
	}
	index.firstID = 0
	index.rangeCount = 0
	index.ids = nil
	index.present = nil
	index.sorted = true
	index.bumpGeneration()
	return true
}

func (index *CommittedHistoryIndex) Contains(id LogicalLineID) bool {
	_, ok := index.position(id)
	return ok
}

func (index *CommittedHistoryIndex) IDs() []LogicalLineID {
	if index.isRange() {
		ids := make([]LogicalLineID, index.rangeCount)
		for i := range ids {
			ids[i] = index.firstID + LogicalLineID(i)
		}
		return ids
	}
	return cloneLineIDs(index.ids)
}

func (index *CommittedHistoryIndex) Len() int {
	if index.isRange() {
		return index.rangeCount
	}
	return len(index.ids)
}

func (index *CommittedHistoryIndex) contiguousBounds() (LogicalLineID, LogicalLineID, int, bool) {
	if index.isRange() {
		return index.firstID, index.firstID + LogicalLineID(index.rangeCount-1), index.rangeCount, true
	}
	if len(index.ids) == 0 || !lineIDsContiguous(index.ids) {
		return 0, 0, 0, false
	}
	return index.ids[0], index.ids[len(index.ids)-1], len(index.ids), true
}

func (index *CommittedHistoryIndex) Generation() Generation {
	return index.generation
}

func (index *CommittedHistoryIndex) bumpGeneration() {
	index.generation++
}

func (index *CommittedHistoryIndex) position(id LogicalLineID) (int, bool) {
	if id == 0 {
		return 0, false
	}
	if index.isRange() {
		if !index.rangeContains(id) {
			return 0, false
		}
		return int(id - index.firstID), true
	}
	if len(index.ids) == 0 {
		return 0, false
	}
	if index.sorted {
		position := sort.Search(len(index.ids), func(i int) bool {
			return index.ids[i] >= id
		})
		return position, position < len(index.ids) && index.ids[position] == id
	}
	if index.present != nil {
		if _, ok := index.present[id]; !ok {
			return 0, false
		}
	}
	for position, existing := range index.ids {
		if existing == id {
			return position, true
		}
	}
	return 0, false
}

func (index *CommittedHistoryIndex) ensurePresentMap() {
	if index.present != nil {
		return
	}
	index.present = make(map[LogicalLineID]struct{}, len(index.ids)+1)
	for _, id := range index.ids {
		index.present[id] = struct{}{}
	}
}

func (index *CommittedHistoryIndex) isRange() bool {
	return index.rangeCount > 0
}

func (index *CommittedHistoryIndex) rangeContains(id LogicalLineID) bool {
	if !index.isRange() || id < index.firstID {
		return false
	}
	return id-index.firstID < LogicalLineID(index.rangeCount)
}

func (index *CommittedHistoryIndex) materializeRange() {
	if !index.isRange() {
		return
	}
	index.ids = make([]LogicalLineID, index.rangeCount)
	for i := range index.ids {
		index.ids[i] = index.firstID + LogicalLineID(i)
	}
	index.firstID = 0
	index.rangeCount = 0
	index.sorted = true
	index.present = nil
}

func cloneLineIDs(ids []LogicalLineID) []LogicalLineID {
	if len(ids) == 0 {
		return nil
	}
	cloned := make([]LogicalLineID, len(ids))
	copy(cloned, ids)
	return cloned
}
