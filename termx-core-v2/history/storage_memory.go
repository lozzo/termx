package history

// NewMemoryStorageBackend 创建 R309 的纯内存 StorageBackend。
// domain boundary：backend 只承载 payload/index/journal/recovery 视图，不决定
// logical line 是否 mutable，也不从文件记录反推 history truth。
func NewMemoryStorageBackend() StorageBackend {
	return &memoryStorageBackend{lines: make(map[LogicalLineID]LogicalLine)}
}

type memoryStorageBackend struct {
	generation Generation
	lines      map[LogicalLineID]LogicalLine
	timeline   []HistoryRecord
	mutable    []LogicalLineID
	frames     []FrameRecord
}

func (backend *memoryStorageBackend) Apply(tx StorageTransaction) error {
	if backend == nil {
		return nil
	}
	if backend.lines == nil {
		backend.lines = make(map[LogicalLineID]LogicalLine)
	}
	if tx.Generation > backend.generation {
		backend.generation = tx.Generation
	}
	for _, line := range tx.Lines {
		backend.lines[line.ID] = cloneLogicalLine(line)
	}
	for _, id := range tx.Tombstones {
		delete(backend.lines, id)
		backend.mutable = removeLineID(backend.mutable, id)
	}
	if tx.Timeline != nil {
		backend.timeline = cloneHistoryRecords(tx.Timeline)
	}
	if tx.Mutable != nil {
		backend.mutable = append([]LogicalLineID(nil), tx.Mutable...)
	}
	if tx.Frames != nil {
		backend.frames = cloneFrameRecords(tx.Frames)
	}
	return nil
}

func (backend *memoryStorageBackend) Recover() (RecoveredHistoryState, error) {
	if backend == nil {
		return RecoveredHistoryState{}, nil
	}
	state := RecoveredHistoryState{
		Generation: backend.generation,
		Timeline:   cloneHistoryRecords(backend.timeline),
		Mutable:    append([]LogicalLineID(nil), backend.mutable...),
		Frames:     cloneFrameRecords(backend.frames),
	}
	for _, id := range sortedLineIDs(backend.lines) {
		state.Lines = append(state.Lines, cloneLogicalLine(backend.lines[id]))
	}
	return state, nil
}

func (backend *memoryStorageBackend) Compact(policy StorageCompactionPolicy) error {
	if backend == nil {
		return nil
	}
	if policy.MaxFrames >= 0 && len(backend.frames) > policy.MaxFrames {
		backend.frames = cloneFrameRecords(backend.frames[len(backend.frames)-policy.MaxFrames:])
	}
	referenced := make(map[LogicalLineID]struct{})
	for _, record := range backend.timeline {
		for _, id := range record.LineIDs {
			referenced[id] = struct{}{}
		}
	}
	for _, id := range backend.mutable {
		referenced[id] = struct{}{}
	}
	for _, frame := range backend.frames {
		for _, id := range frame.LineIDs {
			referenced[id] = struct{}{}
		}
	}
	for id := range backend.lines {
		if _, ok := referenced[id]; !ok {
			delete(backend.lines, id)
		}
	}
	return nil
}

func cloneHistoryRecords(records []HistoryRecord) []HistoryRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]HistoryRecord, len(records))
	for i := range records {
		out[i] = cloneHistoryRecord(records[i])
	}
	return out
}

func cloneFrameRecords(records []FrameRecord) []FrameRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]FrameRecord, len(records))
	for i := range records {
		out[i] = cloneFrameRecord(records[i])
	}
	return out
}

func sortedLineIDs(lines map[LogicalLineID]LogicalLine) []LogicalLineID {
	ids := make([]LogicalLineID, 0, len(lines))
	for id := range lines {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}
