package history

// MutableFrontier is the ordered set of logical lines still eligible for
// terminal semantic mutation. It stores membership only, never payload copies.
type MutableFrontier struct {
	ids        []LogicalLineID
	present    map[LogicalLineID]struct{}
	generation Generation
}

func NewMutableFrontier() *MutableFrontier {
	return &MutableFrontier{
		present: make(map[LogicalLineID]struct{}),
	}
}

func (frontier *MutableFrontier) Add(id LogicalLineID) error {
	if id == 0 {
		return ErrInvalidLineID
	}
	if _, ok := frontier.present[id]; ok {
		return ErrDuplicateLineID
	}
	frontier.ids = append(frontier.ids, id)
	frontier.present[id] = struct{}{}
	frontier.bumpGeneration()
	return nil
}

func (frontier *MutableFrontier) Remove(id LogicalLineID) bool {
	if _, ok := frontier.present[id]; !ok {
		return false
	}
	delete(frontier.present, id)
	next := frontier.ids[:0]
	for _, existing := range frontier.ids {
		if existing != id {
			next = append(next, existing)
		}
	}
	frontier.ids = next
	frontier.bumpGeneration()
	return true
}

func (frontier *MutableFrontier) Clear() bool {
	if len(frontier.ids) == 0 {
		return false
	}
	frontier.ids = nil
	frontier.present = make(map[LogicalLineID]struct{})
	frontier.bumpGeneration()
	return true
}

func (frontier *MutableFrontier) Contains(id LogicalLineID) bool {
	_, ok := frontier.present[id]
	return ok
}

func (frontier *MutableFrontier) IDs() []LogicalLineID {
	return cloneLineIDs(frontier.ids)
}

func (frontier *MutableFrontier) Generation() Generation {
	return frontier.generation
}

func (frontier *MutableFrontier) bumpGeneration() {
	frontier.generation++
}
