package history

// MemoryLogicalLineStore keeps logical line truth in a StorageBackend while it
// owns id allocation and line generation updates.
type MemoryLogicalLineStore struct {
	backend StorageBackend
	nextID  LogicalLineID
}

func NewMemoryLogicalLineStore(backend StorageBackend) *MemoryLogicalLineStore {
	if backend == nil {
		backend = NewMemoryStorageBackend()
	}
	nextID := LogicalLineID(1)
	for _, id := range backend.LineIDs() {
		if id >= nextID {
			nextID = id + 1
		}
	}
	return &MemoryLogicalLineStore{
		backend: backend,
		nextID:  nextID,
	}
}

func (store *MemoryLogicalLineStore) CreateLine(req CreateLineRequest) (LogicalLine, error) {
	seal, err := normalizeSeal(req.Seal)
	if err != nil {
		return LogicalLine{}, err
	}
	residency, err := normalizeResidency(req.Residency)
	if err != nil {
		return LogicalLine{}, err
	}
	line := LogicalLine{
		ID:         store.nextID,
		Generation: 1,
		Seal:       seal,
		Cells:      cloneCells(req.Cells),
		TailFill:   cloneRowTailFill(req.TailFill),
		Dirty:      req.Dirty,
		Residency:  residency,
	}
	if err := store.backend.SaveLine(line); err != nil {
		return LogicalLine{}, err
	}
	store.nextID++
	return line.Clone(), nil
}

func (store *MemoryLogicalLineStore) Line(id LogicalLineID) (LogicalLine, bool) {
	return store.backend.LoadLine(id)
}

func (store *MemoryLogicalLineStore) ReplaceLine(line LogicalLine) (LogicalLine, error) {
	if err := validateLine(line); err != nil {
		return LogicalLine{}, err
	}
	current, ok := store.backend.LoadLine(line.ID)
	if !ok {
		return LogicalLine{}, ErrUnknownLine
	}
	line.Generation = current.Generation + 1
	line.Cells = cloneCells(line.Cells)
	line.TailFill = cloneRowTailFill(line.TailFill)
	if err := store.backend.SaveLine(line); err != nil {
		return LogicalLine{}, err
	}
	return line.Clone(), nil
}

func (store *MemoryLogicalLineStore) DeleteLine(id LogicalLineID) bool {
	return store.backend.DeleteLine(id)
}

func (store *MemoryLogicalLineStore) LineIDs() []LogicalLineID {
	return store.backend.LineIDs()
}
