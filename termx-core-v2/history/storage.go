package history

import "sort"

// StorageBackend persists logical line payloads and does not define whether a
// line is mutable, committed, or retained.
type StorageBackend interface {
	SaveLine(LogicalLine) error
	LoadLine(LogicalLineID) (LogicalLine, bool)
	DeleteLine(LogicalLineID) bool
	LineIDs() []LogicalLineID
}

// MemoryStorageBackend is the first in-memory backend used by the domain
// harness before file or mmap persistence exists.
type MemoryStorageBackend struct {
	lines map[LogicalLineID]LogicalLine
}

func NewMemoryStorageBackend() *MemoryStorageBackend {
	return &MemoryStorageBackend{lines: make(map[LogicalLineID]LogicalLine)}
}

func (backend *MemoryStorageBackend) SaveLine(line LogicalLine) error {
	line, err := normalizeLine(line)
	if err != nil {
		return err
	}
	backend.lines[line.ID] = line.Clone()
	return nil
}

func (backend *MemoryStorageBackend) LoadLine(id LogicalLineID) (LogicalLine, bool) {
	line, ok := backend.lines[id]
	if !ok {
		return LogicalLine{}, false
	}
	return line.Clone(), true
}

func (backend *MemoryStorageBackend) DeleteLine(id LogicalLineID) bool {
	if _, ok := backend.lines[id]; !ok {
		return false
	}
	delete(backend.lines, id)
	return true
}

func (backend *MemoryStorageBackend) LineIDs() []LogicalLineID {
	ids := make([]LogicalLineID, 0, len(backend.lines))
	for id := range backend.lines {
		ids = append(ids, id)
	}
	sortLogicalLineIDs(ids)
	return ids
}

func sortLogicalLineIDs(ids []LogicalLineID) {
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
}
