package history

import "sync"

// MemoryLogicalLineStore keeps logical line truth in a StorageBackend while it
// owns id allocation and line generation updates.
type MemoryLogicalLineStore struct {
	mu              sync.RWMutex
	backend         StorageBackend
	nextID          LogicalLineID
	nextEpoch       ObserverEpoch
	activeObservers map[ObserverEpoch]observerRetention
	retired         map[LogicalLineID][]retainedLineVersion
}

type retainedLineVersion struct {
	line         LogicalLine
	deletedEpoch ObserverEpoch
}

type observerRetention struct {
	refs     int
	first    LogicalLineID
	upper    LogicalLineID
	explicit map[LogicalLineID]struct{}
}

type ObserverLineVisibility struct {
	First LogicalLineID
	Upper LogicalLineID
	IDs   []LogicalLineID
}

type primaryCellsWriteRequest struct {
	Cells             []Cell
	OwnedCells        bool
	ActiveCol         int
	Overwrite         bool
	ContentGeneration Generation
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
		backend:         backend,
		nextID:          nextID,
		nextEpoch:       1,
		activeObservers: make(map[ObserverEpoch]observerRetention),
		retired:         make(map[LogicalLineID][]retainedLineVersion),
	}
}

func (store *MemoryLogicalLineStore) CreateLine(req CreateLineRequest) (LogicalLine, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	seal, err := normalizeSeal(req.Seal)
	if err != nil {
		return LogicalLine{}, err
	}
	residency, err := normalizeResidency(req.Residency)
	if err != nil {
		return LogicalLine{}, err
	}
	cells := req.Cells
	if !req.ownedCells {
		cells = cloneCells(req.Cells)
	}
	line := LogicalLine{
		ID:                store.nextID,
		Generation:        1,
		CreatedGeneration: req.CreatedGeneration,
		ContentGeneration: req.ContentGeneration,
		Seal:              seal,
		Cells:             cells,
		TailFill:          cloneRowTailFill(req.TailFill),
		Dirty:             req.Dirty,
		Residency:         residency,
	}
	if err := store.saveOwnedLineLocked(line); err != nil {
		return LogicalLine{}, err
	}
	store.nextID++
	return line.Clone(), nil
}

func (store *MemoryLogicalLineStore) Line(id LogicalLineID) (LogicalLine, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.backend.LoadLine(id)
}

func (store *MemoryLogicalLineStore) HasLine(id LogicalLineID) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if backend, ok := store.backend.(lineExistsBackend); ok {
		return backend.HasLine(id)
	}
	_, ok := store.backend.LoadLine(id)
	return ok
}

func (store *MemoryLogicalLineStore) SnapshotLine(id LogicalLineID) (LogicalLine, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.snapshotLineLocked(id, 0)
}

func (store *MemoryLogicalLineStore) ObserverSnapshotLine(id LogicalLineID, epoch ObserverEpoch) (LogicalLine, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.snapshotLineLocked(id, epoch)
}

func (store *MemoryLogicalLineStore) snapshotLineLocked(id LogicalLineID, epoch ObserverEpoch) (LogicalLine, bool) {
	backend, ok := store.backend.(snapshotLineBackend)
	if epoch > 0 {
		for _, version := range store.retired[id] {
			// 中文说明：旧 observer 的 epoch 早于删除/替换 epoch 时，只能读当时保留的旧版本。
			if epoch < version.deletedEpoch {
				return version.line.Clone(), true
			}
		}
	}
	if !ok {
		return store.backend.LoadLine(id)
	}
	return backend.LoadSnapshotLine(id)
}

func (store *MemoryLogicalLineStore) ReplaceLine(line LogicalLine) (LogicalLine, error) {
	line.Cells = cloneCells(line.Cells)
	return store.replaceOwnedLine(line)
}

func (store *MemoryLogicalLineStore) replaceOwnedLine(line LogicalLine) (LogicalLine, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateLine(line); err != nil {
		return LogicalLine{}, err
	}
	current, ok := store.loadOwnedLineLocked(line.ID)
	if !ok {
		return LogicalLine{}, ErrUnknownLine
	}
	store.retainCurrentLineLocked(current)
	line.Generation = current.Generation + 1
	if line.CreatedGeneration == 0 {
		line.CreatedGeneration = current.CreatedGeneration
	}
	if line.ContentGeneration == 0 {
		line.ContentGeneration = current.ContentGeneration
	}
	line.TailFill = cloneRowTailFill(line.TailFill)
	if err := store.saveOwnedLineLocked(line); err != nil {
		return LogicalLine{}, err
	}
	store.cleanupRetiredLocked()
	return line.Clone(), nil
}

func (store *MemoryLogicalLineStore) mutateOwnedLine(id LogicalLineID, mutate func(*LogicalLine)) (LogicalLine, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.loadOwnedLineLocked(id)
	if !ok {
		return LogicalLine{}, ErrUnknownLine
	}
	store.retainCurrentLineLocked(current)
	line := current
	mutate(&line)
	return store.saveMutatedLineLocked(current, line)
}

func (store *MemoryLogicalLineStore) writePrimaryCellsOwned(id LogicalLineID, req primaryCellsWriteRequest) (LogicalLine, int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.loadOwnedLineLocked(id)
	if !ok {
		return LogicalLine{}, 0, ErrUnknownLine
	}
	store.retainCurrentLineLocked(current)
	line := current
	lineWidth := logicalLineWidth(line.Cells)
	if !req.Overwrite && req.ActiveCol == lineWidth {
		if req.OwnedCells {
			line.Cells = append(line.Cells, req.Cells...)
		} else {
			line.Cells = append(line.Cells, cloneCells(req.Cells)...)
		}
	} else {
		line.Cells = overwriteLineCellsAtColumn(line.Cells, req.ActiveCol, req.Cells)
		lineWidth = logicalLineWidth(line.Cells)
	}
	line.TailFill = nil
	line.Dirty = true
	if line.CreatedGeneration == 0 {
		line.CreatedGeneration = req.ContentGeneration
	}
	line.ContentGeneration = req.ContentGeneration
	line, err := store.saveMutatedLineLocked(current, line)
	return line, lineWidth, err
}

func (store *MemoryLogicalLineStore) lineCommitState(id LogicalLineID) (SealState, bool, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	line, ok := store.loadOwnedLineLocked(id)
	if !ok {
		return "", false, false
	}
	return line.Seal, line.Dirty, true
}

func (store *MemoryLogicalLineStore) sealLineDirty(id LogicalLineID) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.loadOwnedLineLocked(id)
	if !ok {
		return ErrUnknownLine
	}
	store.retainCurrentLineLocked(current)
	line := current
	line.Seal = SealStateSealed
	line.Dirty = true
	_, err := store.saveMutatedLineLocked(current, line)
	return err
}

func (store *MemoryLogicalLineStore) markLineClean(id LogicalLineID) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.loadOwnedLineLocked(id)
	if !ok {
		return ErrUnknownLine
	}
	store.retainCurrentLineLocked(current)
	line := current
	line.Dirty = false
	_, err := store.saveMutatedLineLocked(current, line)
	return err
}

func (store *MemoryLogicalLineStore) saveMutatedLineLocked(current LogicalLine, line LogicalLine) (LogicalLine, error) {
	if err := validateLine(line); err != nil {
		return LogicalLine{}, err
	}
	if line.ID != current.ID {
		return LogicalLine{}, ErrInvalidLineID
	}
	line.Generation = current.Generation + 1
	if line.CreatedGeneration == 0 {
		line.CreatedGeneration = current.CreatedGeneration
	}
	if line.ContentGeneration == 0 {
		line.ContentGeneration = current.ContentGeneration
	}
	line.TailFill = cloneRowTailFill(line.TailFill)
	if err := store.saveOwnedLineLocked(line); err != nil {
		return LogicalLine{}, err
	}
	store.cleanupRetiredLocked()
	return line, nil
}

func (store *MemoryLogicalLineStore) loadOwnedLineLocked(id LogicalLineID) (LogicalLine, bool) {
	// 中文说明：公开 Line 仍返回 detached copy；store 内部替换/删除/可写 mutation
	// 已经持有锁和 COW observer 边界，可以读取 owned line 来避免热路径整行 clone。
	if backend, ok := store.backend.(snapshotLineBackend); ok {
		return backend.LoadSnapshotLine(id)
	}
	return store.backend.LoadLine(id)
}

func (store *MemoryLogicalLineStore) saveOwnedLineLocked(line LogicalLine) error {
	if backend, ok := store.backend.(ownedLineBackend); ok {
		return backend.saveOwnedLine(line)
	}
	return store.backend.SaveLine(line)
}

func (store *MemoryLogicalLineStore) DeleteLine(id LogicalLineID) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.loadOwnedLineLocked(id)
	if !ok {
		return false
	}
	if store.lineObservedByActiveObserverLocked(id) {
		store.retainLineLocked(current, store.nextObserverEpochLocked())
	}
	deleted := store.backend.DeleteLine(id)
	store.cleanupRetiredLocked()
	return deleted
}

func (store *MemoryLogicalLineStore) LineIDs() []LogicalLineID {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.backend.LineIDs()
}

func (store *MemoryLogicalLineStore) AcquireObserver(visibility ObserverLineVisibility) ObserverEpoch {
	store.mu.Lock()
	defer store.mu.Unlock()
	epoch := store.nextObserverEpochLocked()
	store.activeObservers[epoch] = observerRetentionFromVisibility(visibility)
	return epoch
}

func (store *MemoryLogicalLineStore) ReleaseObserver(epoch ObserverEpoch) {
	if epoch == 0 {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if observer, ok := store.activeObservers[epoch]; ok && observer.refs > 1 {
		observer.refs--
		store.activeObservers[epoch] = observer
	} else {
		delete(store.activeObservers, epoch)
	}
	store.cleanupRetiredLocked()
}

func (store *MemoryLogicalLineStore) RetainedLineCount() int {
	store.mu.RLock()
	defer store.mu.RUnlock()
	total := 0
	for _, versions := range store.retired {
		total += len(versions)
	}
	return total
}

func (store *MemoryLogicalLineStore) retainCurrentLineLocked(line LogicalLine) {
	if !store.lineObservedByActiveObserverLocked(line.ID) {
		return
	}
	// 中文说明：被 active frozen observer 覆盖的 line 不能原地污染，替换前先留旧版本。
	store.retainLineLocked(line, store.nextObserverEpochLocked())
}

func (store *MemoryLogicalLineStore) retainLineLocked(line LogicalLine, deletedEpoch ObserverEpoch) {
	if deletedEpoch == 0 {
		return
	}
	retained := retainedLineVersion{
		line:         line.Clone(),
		deletedEpoch: deletedEpoch,
	}
	store.retired[line.ID] = append(store.retired[line.ID], retained)
}

func (store *MemoryLogicalLineStore) nextObserverEpochLocked() ObserverEpoch {
	epoch := store.nextEpoch
	store.nextEpoch++
	return epoch
}

func (store *MemoryLogicalLineStore) lineObservedByActiveObserverLocked(id LogicalLineID) bool {
	if id == 0 {
		return false
	}
	for _, observer := range store.activeObservers {
		if observer.contains(id) {
			return true
		}
	}
	return false
}

func (store *MemoryLogicalLineStore) cleanupRetiredLocked() {
	if len(store.retired) == 0 {
		return
	}
	for id, versions := range store.retired {
		next := versions[:0]
		for _, version := range versions {
			if store.retainedVersionNeededLocked(id, version.deletedEpoch) {
				next = append(next, version)
			}
		}
		clear(versions[len(next):])
		if len(next) == 0 {
			delete(store.retired, id)
			continue
		}
		store.retired[id] = next
	}
}

func (store *MemoryLogicalLineStore) retainedVersionNeededLocked(id LogicalLineID, deletedEpoch ObserverEpoch) bool {
	versions := store.retired[id]
	for epoch, observer := range store.activeObservers {
		if epoch >= deletedEpoch || !observer.contains(id) {
			continue
		}
		// 中文说明：同一个 observer 只会读到 epoch 之后的第一份 retained 版本；
		// 更晚的中间版本不能因为这个旧 observer 继续占住内存。
		for _, version := range versions {
			if epoch < version.deletedEpoch {
				if version.deletedEpoch == deletedEpoch {
					return true
				}
				break
			}
		}
	}
	return false
}

func observerRetentionFromVisibility(visibility ObserverLineVisibility) observerRetention {
	observer := observerRetention{refs: 1}
	if visibility.First != 0 && visibility.Upper >= visibility.First {
		observer.first = visibility.First
		observer.upper = visibility.Upper
	}
	if len(visibility.IDs) > 0 {
		observer.explicit = make(map[LogicalLineID]struct{}, len(visibility.IDs))
		for _, id := range visibility.IDs {
			if id == 0 {
				continue
			}
			observer.explicit[id] = struct{}{}
		}
	}
	return observer
}

func (observer observerRetention) contains(id LogicalLineID) bool {
	if id == 0 {
		return false
	}
	if observer.first != 0 && id >= observer.first && id <= observer.upper {
		return true
	}
	_, ok := observer.explicit[id]
	return ok
}
