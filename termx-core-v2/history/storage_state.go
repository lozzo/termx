package history

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
)

var (
	ErrInvalidHistoryStoragePath  = errors.New("invalid history storage path")
	ErrInvalidHistoryStorageState = errors.New("invalid history storage state")
)

// HistoryStorageBackend 记录 HistoryTrack 产生的 domain transaction。
// 中文说明：文件只保存 line payload 与 index/frontier/journal 边界，恢复时仍重建
// LogicalLineStore、CommittedHistoryIndex 和 MutableFrontier，不能由文件位置推断可变性。
type HistoryStorageBackend interface {
	ApplyHistoryStorageTransaction(HistoryStorageTransaction) error
	RecoverHistoryStorageSnapshot() (HistoryStorageSnapshot, error)
}

// HistoryStorageSnapshot 是可恢复 backend 的完整 domain 边界快照。
type HistoryStorageSnapshot struct {
	Generation               Generation
	Lines                    []LogicalLine
	CommittedIDs             []LogicalLineID
	FrontierIDs              []LogicalLineID
	HiddenFrontierIDs        []LogicalLineID
	PublishedFrameLineIDs    []LogicalLineID
	ArchivedFrameLineIDs     []LogicalLineID
	TransientFrameLineIDs    []LogicalLineID
	ActiveLine               LogicalLineID
	ActiveCol                int
	Overwrite                bool
	AltScreen                bool
	PrimaryFullscreenIntent  bool
	PrimaryFullscreenFrame   bool
	PrimaryFullscreenModes   []int
	PendingSynchronizedFrame bool
	ScreenRows               int
	ScreenRow                int
	ScreenLineIDs            []LogicalLineID
}

// HistoryStorageTransaction 是 append-only 文件日志中的一条 domain 更新。
type HistoryStorageTransaction struct {
	ReplaceLines  bool
	UpsertLines   []LogicalLine
	DeleteLineIDs []LogicalLineID

	SetGeneration bool
	Generation    Generation

	ReplaceCommittedIndex bool
	CommittedIDs          []LogicalLineID

	ReplaceMutableFrontier bool
	FrontierIDs            []LogicalLineID
	HiddenFrontierIDs      []LogicalLineID

	ReplaceFrameJournal   bool
	PublishedFrameLineIDs []LogicalLineID
	ArchivedFrameLineIDs  []LogicalLineID
	TransientFrameLineIDs []LogicalLineID

	ReplaceTrackState        bool
	ActiveLine               LogicalLineID
	ActiveCol                int
	Overwrite                bool
	AltScreen                bool
	PrimaryFullscreenIntent  bool
	PrimaryFullscreenFrame   bool
	PrimaryFullscreenModes   []int
	PendingSynchronizedFrame bool
	ScreenRows               int
	ScreenRow                int
	ScreenLineIDs            []LogicalLineID
}

func (snapshot HistoryStorageSnapshot) Clone() HistoryStorageSnapshot {
	return HistoryStorageSnapshot{
		Generation:               snapshot.Generation,
		Lines:                    cloneLogicalLines(snapshot.Lines),
		CommittedIDs:             cloneLineIDs(snapshot.CommittedIDs),
		FrontierIDs:              cloneLineIDs(snapshot.FrontierIDs),
		HiddenFrontierIDs:        cloneLineIDs(snapshot.HiddenFrontierIDs),
		PublishedFrameLineIDs:    cloneLineIDs(snapshot.PublishedFrameLineIDs),
		ArchivedFrameLineIDs:     cloneLineIDs(snapshot.ArchivedFrameLineIDs),
		TransientFrameLineIDs:    cloneLineIDs(snapshot.TransientFrameLineIDs),
		ActiveLine:               snapshot.ActiveLine,
		ActiveCol:                snapshot.ActiveCol,
		Overwrite:                snapshot.Overwrite,
		AltScreen:                snapshot.AltScreen,
		PrimaryFullscreenIntent:  snapshot.PrimaryFullscreenIntent,
		PrimaryFullscreenFrame:   snapshot.PrimaryFullscreenFrame,
		PrimaryFullscreenModes:   cloneInts(snapshot.PrimaryFullscreenModes),
		PendingSynchronizedFrame: snapshot.PendingSynchronizedFrame,
		ScreenRows:               snapshot.ScreenRows,
		ScreenRow:                snapshot.ScreenRow,
		ScreenLineIDs:            cloneLineIDs(snapshot.ScreenLineIDs),
	}
}

func (snapshot HistoryStorageSnapshot) FullTransaction() HistoryStorageTransaction {
	return HistoryStorageTransaction{
		ReplaceLines:             true,
		UpsertLines:              cloneLogicalLines(snapshot.Lines),
		SetGeneration:            true,
		Generation:               snapshot.Generation,
		ReplaceCommittedIndex:    true,
		CommittedIDs:             cloneLineIDs(snapshot.CommittedIDs),
		ReplaceMutableFrontier:   true,
		FrontierIDs:              cloneLineIDs(snapshot.FrontierIDs),
		HiddenFrontierIDs:        cloneLineIDs(snapshot.HiddenFrontierIDs),
		ReplaceFrameJournal:      true,
		PublishedFrameLineIDs:    cloneLineIDs(snapshot.PublishedFrameLineIDs),
		ArchivedFrameLineIDs:     cloneLineIDs(snapshot.ArchivedFrameLineIDs),
		TransientFrameLineIDs:    cloneLineIDs(snapshot.TransientFrameLineIDs),
		ReplaceTrackState:        true,
		ActiveLine:               snapshot.ActiveLine,
		ActiveCol:                snapshot.ActiveCol,
		Overwrite:                snapshot.Overwrite,
		AltScreen:                snapshot.AltScreen,
		PrimaryFullscreenIntent:  snapshot.PrimaryFullscreenIntent,
		PrimaryFullscreenFrame:   snapshot.PrimaryFullscreenFrame,
		PrimaryFullscreenModes:   cloneInts(snapshot.PrimaryFullscreenModes),
		PendingSynchronizedFrame: snapshot.PendingSynchronizedFrame,
		ScreenRows:               snapshot.ScreenRows,
		ScreenRow:                snapshot.ScreenRow,
		ScreenLineIDs:            cloneLineIDs(snapshot.ScreenLineIDs),
	}
}

func (track *HistoryTrack) ExportStorageSnapshot() HistoryStorageSnapshot {
	lineIDs := track.store.LineIDs()
	lines := make([]LogicalLine, 0, len(lineIDs))
	for _, id := range lineIDs {
		line, ok := track.store.Line(id)
		if !ok {
			continue
		}
		lines = append(lines, line.Clone())
	}
	return HistoryStorageSnapshot{
		Generation:               track.generation,
		Lines:                    lines,
		CommittedIDs:             track.committed.IDs(),
		FrontierIDs:              track.frontier.IDs(),
		HiddenFrontierIDs:        track.frontier.HiddenIDs(),
		PublishedFrameLineIDs:    cloneLineIDs(track.publishedFrameLineIDs),
		ArchivedFrameLineIDs:     cloneLineIDs(track.archivedFrameLineIDs),
		TransientFrameLineIDs:    cloneLineIDs(track.transientFrameLineIDs),
		ActiveLine:               track.activeLine,
		ActiveCol:                track.activeCol,
		Overwrite:                track.overwrite,
		AltScreen:                track.altScreen,
		PrimaryFullscreenIntent:  track.primaryFullscreenIntent,
		PrimaryFullscreenFrame:   track.primaryFullscreenFrame,
		PrimaryFullscreenModes:   sortedPrimaryFullscreenModes(track.primaryFullscreenModes),
		PendingSynchronizedFrame: track.pendingSynchronizedFrame,
		ScreenRows:               track.screenRows,
		ScreenRow:                track.screenRow,
		ScreenLineIDs:            track.storageScreenLineIDs(),
	}
}

func NewHistoryTrackFromStorageSnapshot(snapshot HistoryStorageSnapshot) (*HistoryTrack, error) {
	if err := validateHistoryStorageSnapshot(snapshot); err != nil {
		return nil, err
	}
	payload := NewMemoryStorageBackend()
	for _, line := range snapshot.Lines {
		if err := payload.SaveLine(line); err != nil {
			return nil, err
		}
	}
	store := NewMemoryLogicalLineStore(payload)
	committed := NewCommittedHistoryIndex()
	for _, id := range snapshot.CommittedIDs {
		if err := committed.Append(id); err != nil {
			return nil, err
		}
	}
	frontier := NewMutableFrontier()
	for _, id := range snapshot.FrontierIDs {
		if err := frontier.Add(id); err != nil {
			return nil, err
		}
	}
	for _, id := range snapshot.HiddenFrontierIDs {
		if err := frontier.Hide(id); err != nil {
			return nil, err
		}
	}
	track := NewHistoryTrackWith(store, committed, frontier)
	track.generation = snapshot.Generation
	track.activeLine = snapshot.ActiveLine
	track.activeCol = snapshot.ActiveCol
	track.overwrite = snapshot.Overwrite
	track.altScreen = snapshot.AltScreen
	track.primaryFullscreenIntent = snapshot.PrimaryFullscreenIntent
	track.primaryFullscreenFrame = snapshot.PrimaryFullscreenFrame
	track.primaryFullscreenModes = primaryFullscreenModeMap(snapshot.PrimaryFullscreenModes)
	track.pendingSynchronizedFrame = snapshot.PendingSynchronizedFrame
	track.publishedFrameLineIDs = cloneLineIDs(snapshot.PublishedFrameLineIDs)
	track.archivedFrameLineIDs = cloneLineIDs(snapshot.ArchivedFrameLineIDs)
	track.transientFrameLineIDs = cloneLineIDs(snapshot.TransientFrameLineIDs)
	track.SetPrimaryScreenRows(snapshot.ScreenRows)
	track.screenRow = snapshot.ScreenRow
	for row, id := range snapshot.ScreenLineIDs {
		if id == 0 {
			continue
		}
		track.screen.set(row, primaryScreenLineOwner{LineID: id})
	}
	return track, nil
}

type MemoryHistoryStorageBackend struct {
	mu       sync.RWMutex
	snapshot HistoryStorageSnapshot
}

func NewMemoryHistoryStorageBackend() *MemoryHistoryStorageBackend {
	return &MemoryHistoryStorageBackend{}
}

func (backend *MemoryHistoryStorageBackend) ApplyHistoryStorageTransaction(tx HistoryStorageTransaction) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	next, err := applyHistoryStorageTransaction(backend.snapshot, tx)
	if err != nil {
		return err
	}
	backend.snapshot = next
	return nil
}

func (backend *MemoryHistoryStorageBackend) RecoverHistoryStorageSnapshot() (HistoryStorageSnapshot, error) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	return backend.snapshot.Clone(), nil
}

type FileHistoryStorageBackend struct {
	mu   sync.Mutex
	path string
}

func NewFileHistoryStorageBackend(path string) (*FileHistoryStorageBackend, error) {
	if path == "" {
		return nil, ErrInvalidHistoryStoragePath
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return &FileHistoryStorageBackend{path: path}, nil
}

func (backend *FileHistoryStorageBackend) ApplyHistoryStorageTransaction(tx HistoryStorageTransaction) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	snapshot, err := backend.recoverLocked()
	if err != nil {
		return err
	}
	if _, err := applyHistoryStorageTransaction(snapshot, tx); err != nil {
		return err
	}
	file, err := os.OpenFile(backend.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encodeErr := json.NewEncoder(file).Encode(tx)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func (backend *FileHistoryStorageBackend) RecoverHistoryStorageSnapshot() (HistoryStorageSnapshot, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.recoverLocked()
}

func (backend *FileHistoryStorageBackend) recoverLocked() (HistoryStorageSnapshot, error) {
	file, err := os.Open(backend.path)
	if errors.Is(err, os.ErrNotExist) {
		return HistoryStorageSnapshot{}, nil
	}
	if err != nil {
		return HistoryStorageSnapshot{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var snapshot HistoryStorageSnapshot
	for {
		var tx HistoryStorageTransaction
		if err := decoder.Decode(&tx); err != nil {
			if errors.Is(err, io.EOF) {
				return snapshot.Clone(), nil
			}
			return HistoryStorageSnapshot{}, err
		}
		next, err := applyHistoryStorageTransaction(snapshot, tx)
		if err != nil {
			return HistoryStorageSnapshot{}, err
		}
		snapshot = next
	}
}

func applyHistoryStorageTransaction(snapshot HistoryStorageSnapshot, tx HistoryStorageTransaction) (HistoryStorageSnapshot, error) {
	if err := validateHistoryStorageTransaction(tx); err != nil {
		return HistoryStorageSnapshot{}, err
	}
	next := snapshot.Clone()
	lines := make(map[LogicalLineID]LogicalLine)
	if !tx.ReplaceLines {
		for _, line := range next.Lines {
			lines[line.ID] = line.Clone()
		}
	}
	for _, line := range tx.UpsertLines {
		line, err := normalizeLine(line)
		if err != nil {
			return HistoryStorageSnapshot{}, err
		}
		lines[line.ID] = line
	}
	for _, id := range tx.DeleteLineIDs {
		delete(lines, id)
	}
	next.Lines = sortedLogicalLines(lines)
	if tx.SetGeneration {
		next.Generation = tx.Generation
	}
	if tx.ReplaceCommittedIndex {
		next.CommittedIDs = cloneLineIDs(tx.CommittedIDs)
	}
	if tx.ReplaceMutableFrontier {
		next.FrontierIDs = cloneLineIDs(tx.FrontierIDs)
		next.HiddenFrontierIDs = cloneLineIDs(tx.HiddenFrontierIDs)
	}
	if tx.ReplaceFrameJournal {
		next.PublishedFrameLineIDs = cloneLineIDs(tx.PublishedFrameLineIDs)
		next.ArchivedFrameLineIDs = cloneLineIDs(tx.ArchivedFrameLineIDs)
		next.TransientFrameLineIDs = cloneLineIDs(tx.TransientFrameLineIDs)
	}
	if tx.ReplaceTrackState {
		next.ActiveLine = tx.ActiveLine
		next.ActiveCol = tx.ActiveCol
		next.Overwrite = tx.Overwrite
		next.AltScreen = tx.AltScreen
		next.PrimaryFullscreenIntent = tx.PrimaryFullscreenIntent
		next.PrimaryFullscreenFrame = tx.PrimaryFullscreenFrame
		next.PrimaryFullscreenModes = cloneInts(tx.PrimaryFullscreenModes)
		next.PendingSynchronizedFrame = tx.PendingSynchronizedFrame
		next.ScreenRows = tx.ScreenRows
		next.ScreenRow = tx.ScreenRow
		next.ScreenLineIDs = cloneLineIDs(tx.ScreenLineIDs)
	}
	if err := validateHistoryStorageSnapshot(next); err != nil {
		return HistoryStorageSnapshot{}, err
	}
	return next, nil
}

func validateHistoryStorageTransaction(tx HistoryStorageTransaction) error {
	for _, line := range tx.UpsertLines {
		if err := validateLine(line); err != nil {
			return err
		}
	}
	if err := validateLineIDList(tx.DeleteLineIDs); err != nil {
		return err
	}
	if tx.ReplaceCommittedIndex {
		if err := validateLineIDList(tx.CommittedIDs); err != nil {
			return err
		}
	}
	if tx.ReplaceMutableFrontier {
		if err := validateLineIDList(tx.FrontierIDs); err != nil {
			return err
		}
		if err := validateLineIDList(tx.HiddenFrontierIDs); err != nil {
			return err
		}
		if err := validateLineIDsSubset(tx.HiddenFrontierIDs, tx.FrontierIDs); err != nil {
			return err
		}
	}
	if tx.ReplaceFrameJournal {
		if err := validateLineIDList(tx.PublishedFrameLineIDs); err != nil {
			return err
		}
		if err := validateLineIDList(tx.ArchivedFrameLineIDs); err != nil {
			return err
		}
		if err := validateLineIDList(tx.TransientFrameLineIDs); err != nil {
			return err
		}
	}
	if tx.ReplaceTrackState {
		if err := validateTrackStorageState(tx.ActiveLine, tx.ActiveCol, tx.ScreenRows, tx.ScreenRow, tx.ScreenLineIDs, tx.PrimaryFullscreenModes); err != nil {
			return err
		}
	}
	return nil
}

func validateHistoryStorageSnapshot(snapshot HistoryStorageSnapshot) error {
	lineSet := make(map[LogicalLineID]struct{}, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		if err := validateLine(line); err != nil {
			return err
		}
		if _, ok := lineSet[line.ID]; ok {
			return ErrDuplicateLineID
		}
		lineSet[line.ID] = struct{}{}
	}
	lists := [][]LogicalLineID{
		snapshot.CommittedIDs,
		snapshot.FrontierIDs,
		snapshot.HiddenFrontierIDs,
		snapshot.PublishedFrameLineIDs,
		snapshot.ArchivedFrameLineIDs,
		snapshot.TransientFrameLineIDs,
	}
	for _, ids := range lists {
		if err := validateLineIDList(ids); err != nil {
			return err
		}
		if err := validateReferencedLineIDs(ids, lineSet); err != nil {
			return err
		}
	}
	if err := validateLineIDsSubset(snapshot.HiddenFrontierIDs, snapshot.FrontierIDs); err != nil {
		return err
	}
	if snapshot.ActiveLine != 0 {
		if _, ok := lineSet[snapshot.ActiveLine]; !ok {
			return ErrUnknownLine
		}
	}
	for _, id := range snapshot.ScreenLineIDs {
		if id == 0 {
			continue
		}
		if _, ok := lineSet[id]; !ok {
			return ErrUnknownLine
		}
	}
	return validateTrackStorageState(snapshot.ActiveLine, snapshot.ActiveCol, snapshot.ScreenRows, snapshot.ScreenRow, snapshot.ScreenLineIDs, snapshot.PrimaryFullscreenModes)
}

func validateTrackStorageState(activeLine LogicalLineID, activeCol int, screenRows int, screenRow int, screenLineIDs []LogicalLineID, primaryModes []int) error {
	if activeCol < 0 || screenRows < 0 || screenRow < 0 {
		return ErrInvalidHistoryStorageState
	}
	if activeLine == 0 && activeCol != 0 {
		return ErrInvalidHistoryStorageState
	}
	if screenRows == 0 {
		if screenRow != 0 || len(screenLineIDs) != 0 {
			return ErrInvalidHistoryStorageState
		}
	} else {
		if screenRow >= screenRows || len(screenLineIDs) != screenRows {
			return ErrInvalidHistoryStorageState
		}
	}
	if err := validateIntsUnique(primaryModes); err != nil {
		return err
	}
	return nil
}

func validateLineIDList(ids []LogicalLineID) error {
	seen := make(map[LogicalLineID]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return ErrInvalidLineID
		}
		if _, ok := seen[id]; ok {
			return ErrDuplicateLineID
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateLineIDsSubset(ids []LogicalLineID, allowed []LogicalLineID) error {
	allowedSet := make(map[LogicalLineID]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := allowedSet[id]; !ok {
			return ErrLineNotMutable
		}
	}
	return nil
}

func validateReferencedLineIDs(ids []LogicalLineID, lineSet map[LogicalLineID]struct{}) error {
	for _, id := range ids {
		if _, ok := lineSet[id]; !ok {
			return ErrUnknownLine
		}
	}
	return nil
}

func validateIntsUnique(values []int) error {
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return ErrInvalidHistoryStorageState
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (track *HistoryTrack) storageScreenLineIDs() []LogicalLineID {
	if track.screenRows == 0 {
		return nil
	}
	ids := make([]LogicalLineID, track.screenRows)
	for row := 0; row < track.screenRows; row++ {
		owner, ok := track.screen.owner(row)
		if ok {
			ids[row] = owner.LineID
		}
	}
	return ids
}

func sortedPrimaryFullscreenModes(modes map[int]struct{}) []int {
	if len(modes) == 0 {
		return nil
	}
	values := make([]int, 0, len(modes))
	for mode := range modes {
		values = append(values, mode)
	}
	sort.Ints(values)
	return values
}

func primaryFullscreenModeMap(values []int) map[int]struct{} {
	if len(values) == 0 {
		return nil
	}
	modes := make(map[int]struct{}, len(values))
	for _, value := range values {
		modes[value] = struct{}{}
	}
	return modes
}

func sortedLogicalLines(lines map[LogicalLineID]LogicalLine) []LogicalLine {
	if len(lines) == 0 {
		return nil
	}
	ids := make([]LogicalLineID, 0, len(lines))
	for id := range lines {
		ids = append(ids, id)
	}
	sortLogicalLineIDs(ids)
	out := make([]LogicalLine, 0, len(ids))
	for _, id := range ids {
		out = append(out, lines[id].Clone())
	}
	return out
}

func cloneLogicalLines(lines []LogicalLine) []LogicalLine {
	if len(lines) == 0 {
		return nil
	}
	cloned := make([]LogicalLine, len(lines))
	for i, line := range lines {
		cloned[i] = line.Clone()
	}
	return cloned
}

func cloneInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]int, len(values))
	copy(cloned, values)
	return cloned
}
