package history

// MutableFrontier is the ordered set of logical lines still eligible for
// terminal semantic mutation. It stores membership only, never payload copies.
type MutableFrontier struct {
	ids        []LogicalLineID
	present    map[LogicalLineID]struct{}
	hidden     map[LogicalLineID]struct{}
	generation Generation
}

func NewMutableFrontier() *MutableFrontier {
	return &MutableFrontier{
		present: make(map[LogicalLineID]struct{}),
		hidden:  make(map[LogicalLineID]struct{}),
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

func (frontier *MutableFrontier) PrependMany(ids []LogicalLineID) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[LogicalLineID]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return ErrInvalidLineID
		}
		if _, ok := frontier.present[id]; ok {
			return ErrDuplicateLineID
		}
		if _, ok := seen[id]; ok {
			return ErrDuplicateLineID
		}
		seen[id] = struct{}{}
	}
	// 中文说明：resize grow 回收的是 committed suffix，必须按原历史顺序插回 frontier。
	next := make([]LogicalLineID, 0, len(ids)+len(frontier.ids))
	next = append(next, ids...)
	next = append(next, frontier.ids...)
	frontier.ids = next
	for _, id := range ids {
		frontier.present[id] = struct{}{}
	}
	frontier.bumpGeneration()
	return nil
}

func (frontier *MutableFrontier) Remove(id LogicalLineID) bool {
	if _, ok := frontier.present[id]; !ok {
		return false
	}
	delete(frontier.present, id)
	delete(frontier.hidden, id)
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
	frontier.hidden = make(map[LogicalLineID]struct{})
	frontier.bumpGeneration()
	return true
}

func (frontier *MutableFrontier) Hide(id LogicalLineID) error {
	if id == 0 {
		return ErrInvalidLineID
	}
	if _, ok := frontier.present[id]; !ok {
		return ErrLineNotMutable
	}
	if _, ok := frontier.hidden[id]; ok {
		return nil
	}
	frontier.hidden[id] = struct{}{}
	frontier.bumpGeneration()
	return nil
}

func (frontier *MutableFrontier) Reveal(id LogicalLineID) bool {
	if _, ok := frontier.hidden[id]; !ok {
		return false
	}
	delete(frontier.hidden, id)
	frontier.bumpGeneration()
	return true
}

func (frontier *MutableFrontier) IsHidden(id LogicalLineID) bool {
	_, ok := frontier.hidden[id]
	return ok
}

func (frontier *MutableFrontier) HiddenIDs() []LogicalLineID {
	hidden := make([]LogicalLineID, 0, len(frontier.hidden))
	for _, id := range frontier.ids {
		if frontier.IsHidden(id) {
			hidden = append(hidden, id)
		}
	}
	return hidden
}

func (frontier *MutableFrontier) Contains(id LogicalLineID) bool {
	_, ok := frontier.present[id]
	return ok
}

func (frontier *MutableFrontier) IDs() []LogicalLineID {
	return cloneLineIDs(frontier.ids)
}

func (frontier *MutableFrontier) Reorder(ids []LogicalLineID) error {
	if len(ids) != len(frontier.ids) {
		return ErrInvalidLineID
	}
	seen := make(map[LogicalLineID]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return ErrInvalidLineID
		}
		if _, ok := frontier.present[id]; !ok {
			return ErrLineNotMutable
		}
		if _, ok := seen[id]; ok {
			return ErrDuplicateLineID
		}
		seen[id] = struct{}{}
	}
	if equalLogicalLineIDs(frontier.ids, ids) {
		return nil
	}
	frontier.ids = cloneLineIDs(ids)
	frontier.bumpGeneration()
	return nil
}

func (frontier *MutableFrontier) Generation() Generation {
	return frontier.generation
}

func (frontier *MutableFrontier) bumpGeneration() {
	frontier.generation++
}

func equalLogicalLineIDs(a []LogicalLineID, b []LogicalLineID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
