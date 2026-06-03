package history

// CommittedHistoryIndex is the ordered set of logical lines currently counted
// as authoritative committed history.
type CommittedHistoryIndex struct {
	ids        []LogicalLineID
	present    map[LogicalLineID]struct{}
	generation Generation
}

func NewCommittedHistoryIndex() *CommittedHistoryIndex {
	return &CommittedHistoryIndex{
		present: make(map[LogicalLineID]struct{}),
	}
}

func (index *CommittedHistoryIndex) Append(id LogicalLineID) error {
	if id == 0 {
		return ErrInvalidLineID
	}
	if _, ok := index.present[id]; ok {
		return ErrDuplicateLineID
	}
	index.ids = append(index.ids, id)
	index.present[id] = struct{}{}
	index.bumpGeneration()
	return nil
}

func (index *CommittedHistoryIndex) Remove(id LogicalLineID) bool {
	if _, ok := index.present[id]; !ok {
		return false
	}
	delete(index.present, id)
	next := index.ids[:0]
	for _, existing := range index.ids {
		if existing != id {
			next = append(next, existing)
		}
	}
	index.ids = next
	index.bumpGeneration()
	return true
}

func (index *CommittedHistoryIndex) Clear() bool {
	if len(index.ids) == 0 {
		return false
	}
	index.ids = nil
	index.present = make(map[LogicalLineID]struct{})
	index.bumpGeneration()
	return true
}

func (index *CommittedHistoryIndex) Contains(id LogicalLineID) bool {
	_, ok := index.present[id]
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

func cloneLineIDs(ids []LogicalLineID) []LogicalLineID {
	if len(ids) == 0 {
		return nil
	}
	cloned := make([]LogicalLineID, len(ids))
	copy(cloned, ids)
	return cloned
}
