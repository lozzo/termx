package history

import "sort"

// CommittedHistoryIndex is the ordered set of logical lines currently counted
// as authoritative committed history.
type CommittedHistoryIndex struct {
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
	if len(index.ids) == 0 {
		return false
	}
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
	return cloneLineIDs(index.ids)
}

func (index *CommittedHistoryIndex) Generation() Generation {
	return index.generation
}

func (index *CommittedHistoryIndex) bumpGeneration() {
	index.generation++
}

func (index *CommittedHistoryIndex) position(id LogicalLineID) (int, bool) {
	if id == 0 || len(index.ids) == 0 {
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

func cloneLineIDs(ids []LogicalLineID) []LogicalLineID {
	if len(ids) == 0 {
		return nil
	}
	cloned := make([]LogicalLineID, len(ids))
	copy(cloned, ids)
	return cloned
}
