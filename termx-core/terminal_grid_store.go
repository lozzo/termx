package termx

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-vterm/vterm"
	"google.golang.org/protobuf/proto"
)

const (
	defaultTerminalLiveScrollbackRows = 32
	defaultGridHistoryPageRows        = 100
	maxGridReplayRows                 = 5000
	defaultGridPageMaxBytes           = 4 * 1024 * 1024

	terminalGridStoreVersion = 5
	terminalGridRowCodec     = "compact-line-v2"
	terminalGridIndexCodec   = "fixed20-le-v1"
	terminalGridMetadataName = "grid.meta.pb"
	terminalGridIndexName    = "grid.index"
	terminalGridIndexRecord  = 20
)

type terminalGridStore struct {
	mu              sync.Mutex
	dir             string
	terminalID      string
	current         *os.File
	index           *os.File
	currentSeq      uint32
	currentBytes    int64
	baseRowID       uint64
	generation      uint64
	pageMaxBytes    int64
	rowCount        int
	retentionPolicy terminalGridRetentionPolicy
	closed          bool
	removeOnClose   bool
	writable        bool
}

type terminalGridRetentionPolicy struct {
	maxLogicalLines  int
	maxRetainedBytes int64
	maxAge           time.Duration
}

type terminalGridRowRef struct {
	seq    uint32
	offset int64
	length int64
	flags  uint32
}

const terminalGridRowFlagWrapped uint32 = 1 << 0

type terminalGridMetadata struct {
	StoreVersion  int
	TerminalID    string
	RowCodec      string
	IndexCodec    string
	PageMaxBytes  int64
	RowCount      int
	PageCount     int
	CreatedAtUnix int64
	UpdatedAtUnix int64
	BaseRowID     uint64
	Generation    uint64
}

func newTerminalGridStore(gridRoot, terminalID string) (*terminalGridStore, error) {
	if strings.TrimSpace(gridRoot) == "" {
		dir, err := os.MkdirTemp("", "termx-grid-"+sanitizeGridStoreID(terminalID)+"-*")
		if err != nil {
			return nil, err
		}
		store, err := openTerminalGridStoreDir(dir, terminalID, true, true)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		return store, nil
	}
	if err := removeTerminalGridStore(gridRoot, terminalID); err != nil {
		return nil, err
	}
	return openTerminalGridStoreDir(terminalGridDir(gridRoot, terminalID), terminalID, true, false)
}

func openTerminalGridStoreForReplay(gridRoot, terminalID string) (*terminalGridStore, error) {
	if strings.TrimSpace(gridRoot) == "" {
		return nil, ErrNotFound
	}
	return openTerminalGridStoreDir(terminalGridDir(gridRoot, terminalID), terminalID, false, false)
}

func removeTerminalGridStore(gridRoot, terminalID string) error {
	if strings.TrimSpace(gridRoot) == "" || strings.TrimSpace(terminalID) == "" {
		return nil
	}
	err := os.RemoveAll(terminalGridDir(gridRoot, terminalID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func openTerminalGridStoreDir(dir, terminalID string, create bool, removeOnClose bool) (*terminalGridStore, error) {
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	} else if info, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	} else if !info.IsDir() {
		return nil, ErrNotFound
	}

	metadata, metadataErr := readTerminalGridMetadata(dir)
	if metadataErr == nil && metadata.TerminalID != "" && terminalID != "" && metadata.TerminalID != terminalID {
		return nil, fmt.Errorf("termx grid metadata terminal id mismatch: got %q, want %q", metadata.TerminalID, terminalID)
	}
	if metadataErr != nil {
		if !create {
			metadata = terminalGridMetadata{
				StoreVersion: terminalGridStoreVersion,
				TerminalID:   terminalID,
				RowCodec:     terminalGridRowCodec,
				IndexCodec:   terminalGridIndexCodec,
				PageMaxBytes: defaultGridPageMaxBytes,
				Generation:   terminalGridHistoryGeneration(0),
			}
		} else {
			metadata = terminalGridMetadata{
				StoreVersion:  terminalGridStoreVersion,
				TerminalID:    terminalID,
				RowCodec:      terminalGridRowCodec,
				IndexCodec:    terminalGridIndexCodec,
				PageMaxBytes:  defaultGridPageMaxBytes,
				CreatedAtUnix: time.Now().UTC().Unix(),
				Generation:    terminalGridHistoryGeneration(0),
			}
			if os.IsNotExist(metadataErr) {
				if writeErr := writeTerminalGridMetadata(dir, metadata); writeErr != nil {
					return nil, writeErr
				}
			}
		}
	}
	if metadata.PageMaxBytes <= 0 {
		metadata.PageMaxBytes = defaultGridPageMaxBytes
	}
	if metadata.Generation == 0 {
		metadata.Generation = terminalGridHistoryGeneration(metadata.CreatedAtUnix)
	}

	indexState, err := loadTerminalGridIndexState(dir)
	if err != nil {
		return nil, err
	}
	store := &terminalGridStore{
		dir:           dir,
		terminalID:    terminalID,
		currentSeq:    indexState.lastSeq,
		baseRowID:     metadata.BaseRowID,
		generation:    metadata.Generation,
		pageMaxBytes:  metadata.PageMaxBytes,
		rowCount:      indexState.rowCount,
		removeOnClose: removeOnClose,
		writable:      create,
	}
	if !create {
		return store, nil
	}

	index, err := os.OpenFile(filepath.Join(dir, terminalGridIndexName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	store.index = index
	if err := store.openCurrentPageLocked(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func newMemoryTerminalGridStoreForTest(t testTempDirProvider) *terminalGridStore {
	if t == nil {
		return nil
	}
	store, err := openTerminalGridStoreDir(t.TempDir(), "test", true, false)
	if err != nil {
		panic(err)
	}
	return store
}

func (s *terminalGridStore) SetMaxRows(maxRows int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.retentionPolicy.maxLogicalLines = maxRows
	s.mu.Unlock()
}

func (s *terminalGridStore) SetRetentionPolicy(policy terminalGridRetentionPolicy) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.retentionPolicy = policy
	s.mu.Unlock()
}

type testTempDirProvider interface {
	TempDir() string
}

func (s *terminalGridStore) AppendDamageRows(rows []vterm.DamageOp) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	traceGridDamageOps("core.grid_store.append_damage_rows", s.terminalID, rows)
	return s.appendRowSequence(len(rows), func(i int) terminalGridRow {
		row := rows[i]
		return terminalGridRow{
			cells:     row.Cells,
			runs:      row.Runs,
			timestamp: row.Timestamp,
			rowKind:   row.RowKind,
			wrapped:   row.WrappedSet && row.Wrapped,
		}
	})
}

func (s *terminalGridStore) AppendRows(rows [][]vterm.Cell) error {
	if len(rows) == 0 {
		return nil
	}
	traceGridVTermRows("core.grid_store.append_rows", s.terminalID, rows)
	return s.appendRowSequence(len(rows), func(i int) terminalGridRow {
		return terminalGridRow{cells: rows[i]}
	})
}

type terminalGridRow struct {
	cells     []vterm.Cell
	runs      []vterm.CellRun
	timestamp time.Time
	rowKind   string
	wrapped   bool
}

func (s *terminalGridStore) appendRows(rows []terminalGridRow) error {
	return s.appendRowSequence(len(rows), func(i int) terminalGridRow {
		return rows[i]
	})
}

func (s *terminalGridStore) appendRowSequence(count int, rowAt func(int) terminalGridRow) error {
	if s == nil || count == 0 || rowAt == nil {
		return nil
	}
	finish := perftrace.Measure("terminal.grid.append")
	traceRows := make([]terminalGridRow, 0, minInt(count, 16))

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		finish(0)
		return nil
	}
	batch := make([]byte, 0, minInt(int(s.pageMaxBytes), 64*1024))
	refs := make([]terminalGridRowRef, 0, minInt(count, 1024))
	totalBytes := 0
	appendedRows := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if s.current == nil {
			if err := s.openCurrentPageLocked(); err != nil {
				return err
			}
		}
		n, err := s.current.Write(batch)
		if n > 0 {
			s.currentBytes += int64(n)
		}
		if err != nil {
			return err
		}
		if n != len(batch) {
			return io.ErrShortWrite
		}
		if err := s.appendIndexRowsLocked(refs); err != nil {
			return err
		}
		s.rowCount += len(refs)
		appendedRows += len(refs)
		clear(refs)
		refs = refs[:0]
		clear(batch)
		batch = batch[:0]
		return nil
	}

	for i := 0; i < count; i++ {
		row := rowAt(i)
		if len(traceRows) < cap(traceRows) {
			traceRows = append(traceRows, row)
		}
		payload, err := encodeTerminalGridRow(row)
		if err != nil {
			finish(0)
			return err
		}
		totalBytes += len(payload)
		if s.current == nil {
			if err := s.openCurrentPageLocked(); err != nil {
				finish(0)
				return err
			}
		}
		if len(batch) > 0 && s.currentBytes+int64(len(batch)+len(payload)) > s.pageMaxBytes {
			if err := flush(); err != nil {
				finish(0)
				return err
			}
		}
		if s.currentBytes > 0 && s.currentBytes+int64(len(payload)) > s.pageMaxBytes {
			if err := s.rotateLocked(); err != nil {
				finish(0)
				return err
			}
		}
		var rowFlags uint32
		if row.wrapped {
			rowFlags |= terminalGridRowFlagWrapped
		}
		refs = append(refs, terminalGridRowRef{
			seq:    s.currentSeq,
			offset: s.currentBytes + int64(len(batch)),
			length: int64(len(payload)),
			flags:  rowFlags,
		})
		batch = append(batch, payload...)
		if int64(len(batch)) >= s.pageMaxBytes {
			if err := flush(); err != nil {
				finish(0)
				return err
			}
		}
	}
	if err := flush(); err != nil {
		finish(0)
		return err
	}
	if err := s.enforceMaxRowsLocked(); err != nil {
		finish(0)
		return err
	}

	finish(totalBytes)
	perftrace.Count("terminal.grid.rows", appendedRows)
	perftrace.Count("terminal.grid.bytes", totalBytes)
	traceGridTerminalRows("core.grid_store.append_committed_sample", s.terminalID, traceRows, "requested_rows", count, "appended_rows", appendedRows, "total_rows_after", s.rowCount, "payload_bytes", totalBytes)
	return nil
}

func (s *terminalGridStore) RowCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rowCount
}

func (s *terminalGridStore) LogicalLineCount() int {
	if s == nil {
		return 0
	}
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil {
		return 0
	}
	return terminalGridLogicalLineCountForRefs(refs)
}

func (s *terminalGridStore) coordinatesLocked() (baseRowID uint64, generation uint64, rowCount int) {
	if s == nil {
		return 0, 0, 0
	}
	return s.baseRowID, s.generation, s.rowCount
}

func (s *terminalGridStore) coordinates() (baseRowID uint64, generation uint64, rowCount int) {
	if s == nil {
		return 0, 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coordinatesLocked()
}

func (s *terminalGridStore) Replay(beforeOffset int, limit int) ([]byte, int, bool, error) {
	if s == nil {
		return nil, 0, false, nil
	}
	beforeOffset, limit = sanitizeGridReplayWindow(beforeOffset, limit)

	refs, rows, hasMore, err := s.windowRefs(beforeOffset, limit)
	if err != nil {
		return nil, 0, false, err
	}
	if rows == 0 {
		return nil, 0, false, nil
	}
	finish := perftrace.Measure("terminal.grid.replay")
	gridRows, err := readTerminalGridRows(s.dir, refs)
	if err != nil {
		finish(0)
		return nil, 0, false, err
	}
	replay := encodeGridRowsReplay(gridRows)
	finish(len(replay))
	return replay, rows, hasMore, nil
}

func (s *terminalGridStore) Viewport(beforeOffset int, limit int, cols int) (terminalGridViewport, error) {
	var result terminalGridViewport
	if s == nil {
		return result, nil
	}
	beforeOffset, limit = sanitizeGridViewportWindow(beforeOffset, limit)
	rawLimit := limit
	for {
		refs, rows, hasMore, err := s.windowRefs(beforeOffset, rawLimit)
		if err != nil {
			return result, err
		}
		if rows == 0 {
			return result, nil
		}
		finish := perftrace.Measure("terminal.grid.rows_read")
		gridRows, err := readTerminalGridRows(s.dir, refs)
		finish(len(gridRows))
		if err != nil {
			return result, err
		}
		traceGridTerminalRows("core.grid_store.viewport.raw_rows", s.terminalID, gridRows, "offset", beforeOffset, "limit", limit, "raw_limit", rawLimit, "cols", cols, "window_rows", rows, "has_more", hasMore)
		result.Rows, result.Timestamps, result.RowKinds, result.Wrapped = reflowTerminalGridRows(gridRows, cols)
		result.Ownership = repeatedString(RowOwnershipPersisted, len(result.Rows))
		cropped := false
		if hasMore {
			cropped = trimTerminalGridViewportToTail(&result, limit)
		}
		result.LoadedRows = beforeOffset + rows
		result.HasMore = hasMore || cropped
		result.BeforeOffset = beforeOffset
		result.Limit = limit
		result.Generation, result.FirstRowID, result.LastRowID, result.TotalRows = s.rowWindowCoordinates(beforeOffset, rows)
		result.LogicalTotal = s.LogicalLineCount()
		traceGridVTermRows("core.grid_store.viewport.reflow_rows", s.terminalID, result.Rows, "offset", beforeOffset, "limit", limit, "raw_limit", rawLimit, "cols", cols, "loaded_rows", result.LoadedRows, "total", result.TotalRows, "has_more", result.HasMore, "cropped", cropped, "generation", result.Generation, "first_row_id", result.FirstRowID, "last_row_id", result.LastRowID)
		if len(result.Rows) >= limit || !hasMore {
			return result, nil
		}
		nextRawLimit := rawLimit * 2
		if nextRawLimit <= rawLimit {
			return result, nil
		}
		maxRawRows := result.TotalRows - beforeOffset
		if maxRawRows <= rawLimit {
			return result, nil
		}
		if nextRawLimit > maxRawRows {
			nextRawLimit = maxRawRows
		}
		rawLimit = nextRawLimit
	}
}

type terminalGridViewport struct {
	Rows         [][]vterm.Cell
	Timestamps   []time.Time
	RowKinds     []string
	Wrapped      []bool
	Ownership    []string
	LoadedRows   int
	HasMore      bool
	BeforeOffset int
	Limit        int
	TotalRows    int
	LogicalTotal int
	Generation   uint64
	FirstRowID   uint64
	LastRowID    uint64
}

func trimTerminalGridViewportToTail(result *terminalGridViewport, limit int) bool {
	if result == nil || limit <= 0 || len(result.Rows) <= limit {
		return false
	}
	start := len(result.Rows) - limit
	result.Rows = result.Rows[start:]
	result.Timestamps = trimTimeSliceTail(result.Timestamps, limit)
	result.RowKinds = trimStringSliceTail(result.RowKinds, limit)
	result.Wrapped = trimBoolSliceTail(result.Wrapped, limit)
	result.Ownership = trimStringSliceTail(result.Ownership, limit)
	return true
}

func (s *terminalGridStore) windowRefs(beforeOffset int, limit int) ([]terminalGridRowRef, int, bool, error) {
	s.mu.Lock()
	total := s.rowCount
	indexPath := filepath.Join(s.dir, terminalGridIndexName)
	s.mu.Unlock()

	if total == 0 {
		return nil, 0, false, nil
	}
	if beforeOffset > total {
		beforeOffset = total
	}
	end := total - beforeOffset
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, 0, false, err
	}
	defer file.Close()
	start, err = expandTerminalGridWindowStartToLogicalLine(file, start)
	if err != nil {
		return nil, 0, false, err
	}
	refs, err := readTerminalGridIndexWindowFromFile(file, start, end-start)
	if err != nil {
		return nil, 0, false, err
	}

	if len(refs) == 0 {
		return nil, 0, false, nil
	}
	return refs, len(refs), start > 0, nil
}

func expandTerminalGridWindowStartToLogicalLine(file *os.File, start int) (int, error) {
	for start > 0 {
		ref, err := readTerminalGridIndexRecordFromFile(file, start-1)
		if err != nil {
			return start, err
		}
		if !terminalGridRowContinuesLogicalLine(ref) {
			break
		}
		start--
	}
	return start, nil
}

func terminalGridRowContinuesLogicalLine(ref terminalGridRowRef) bool {
	return ref.flags&terminalGridRowFlagWrapped != 0
}

func (s *terminalGridStore) rowWindowCoordinates(beforeOffset int, loadedRows int) (generation uint64, firstRowID uint64, lastRowID uint64, totalRows int) {
	if s == nil || loadedRows <= 0 {
		return 0, 0, 0, 0
	}
	s.mu.Lock()
	baseRowID, generation, total := s.coordinatesLocked()
	s.mu.Unlock()
	if total <= 0 {
		return generation, 0, 0, total
	}
	if beforeOffset > total {
		beforeOffset = total
	}
	end := total - beforeOffset
	if end < 0 {
		end = 0
	}
	start := end - loadedRows
	if start < 0 {
		start = 0
	}
	if end <= start {
		return generation, 0, 0, total
	}
	return generation, baseRowID + uint64(start), baseRowID + uint64(end-1), total
}

func sanitizeGridReplayWindow(beforeOffset int, limit int) (int, int) {
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = defaultGridHistoryPageRows
	}
	return beforeOffset, limit
}

func sanitizeGridViewportWindow(beforeOffset int, limit int) (int, int) {
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = defaultGridHistoryPageRows
	}
	return beforeOffset, limit
}

func (s *terminalGridStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	current := s.current
	index := s.index
	s.current = nil
	s.index = nil
	dir := s.dir
	removeOnClose := s.removeOnClose
	metadata := terminalGridMetadata{
		StoreVersion: terminalGridStoreVersion,
		TerminalID:   s.terminalID,
		RowCodec:     terminalGridRowCodec,
		IndexCodec:   terminalGridIndexCodec,
		PageMaxBytes: s.pageMaxBytes,
		RowCount:     s.rowCount,
		PageCount:    int(s.currentSeq) + 1,
		BaseRowID:    s.baseRowID,
		Generation:   s.generation,
	}
	if existing, err := readTerminalGridMetadata(dir); err == nil {
		metadata.CreatedAtUnix = existing.CreatedAtUnix
	}
	s.mu.Unlock()

	var err error
	if current != nil {
		err = current.Close()
	}
	if index != nil {
		if indexErr := index.Close(); indexErr != nil && err == nil {
			err = indexErr
		}
	}
	if s.writable && !removeOnClose {
		if metadataErr := writeTerminalGridMetadata(dir, metadata); metadataErr != nil && err == nil {
			err = metadataErr
		}
	}
	if removeOnClose {
		if removeErr := os.RemoveAll(dir); removeErr != nil && err == nil {
			err = removeErr
		}
	}
	return err
}

func (s *terminalGridStore) openCurrentPageLocked() error {
	if s == nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(s.dir, terminalGridPageName(s.currentSeq))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	s.current = file
	s.currentBytes = info.Size()
	return nil
}

func (s *terminalGridStore) rotateLocked() error {
	if s == nil {
		return nil
	}
	if s.current != nil {
		if err := s.current.Close(); err != nil {
			return err
		}
		s.current = nil
	}
	s.currentSeq++
	return s.openCurrentPageLocked()
}

func (s *terminalGridStore) appendIndexRowsLocked(refs []terminalGridRowRef) error {
	if s == nil || len(refs) == 0 {
		return nil
	}
	if s.index == nil {
		index, err := os.OpenFile(filepath.Join(s.dir, terminalGridIndexName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		s.index = index
	}
	buf := make([]byte, len(refs)*terminalGridIndexRecord)
	for i, ref := range refs {
		if ref.length <= 0 || ref.length > int64(^uint32(0)) {
			return fmt.Errorf("termx grid row length out of range: %d", ref.length)
		}
		base := i * terminalGridIndexRecord
		binary.LittleEndian.PutUint32(buf[base:base+4], ref.seq)
		binary.LittleEndian.PutUint64(buf[base+4:base+12], uint64(ref.offset))
		binary.LittleEndian.PutUint32(buf[base+12:base+16], uint32(ref.length))
		binary.LittleEndian.PutUint32(buf[base+16:base+20], ref.flags)
	}
	start, statErr := s.index.Seek(0, io.SeekEnd)
	if statErr != nil {
		return statErr
	}
	n, err := s.index.Write(buf)
	if err != nil {
		_ = s.index.Truncate(start)
		_, _ = s.index.Seek(0, io.SeekEnd)
		return err
	}
	if n != len(buf) {
		_ = s.index.Truncate(start)
		_, _ = s.index.Seek(0, io.SeekEnd)
		return io.ErrShortWrite
	}
	return nil
}

func (s *terminalGridStore) enforceMaxRowsLocked() error {
	return s.enforceMaxRowsLockedAt(time.Now().UTC())
}

func (s *terminalGridStore) enforceMaxRowsLockedAt(now time.Time) error {
	if s == nil {
		return nil
	}
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil {
		return err
	}
	retainRows, err := terminalGridRetentionRetainedRows(s.dir, refs, s.retentionPolicy, now)
	if err != nil {
		return err
	}
	if retainRows >= len(refs) {
		return nil
	}
	drop := len(refs) - retainRows
	if drop <= 0 {
		return nil
	}
	refs = refs[drop:]
	indexPath := filepath.Join(s.dir, terminalGridIndexName)
	tmpPath := indexPath + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := writeTerminalGridIndexRefs(tmp, refs); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if s.index != nil {
		if err := s.index.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		s.index = nil
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	s.rowCount = len(refs)
	s.baseRowID += uint64(drop)
	s.generation++
	if s.generation == 0 {
		s.generation = 1
	}
	if len(refs) > 0 {
		last := refs[len(refs)-1]
		s.currentSeq = last.seq
		if s.current != nil {
			_ = s.current.Close()
			s.current = nil
		}
		if err := s.openCurrentPageLocked(); err != nil {
			return err
		}
		s.currentBytes = last.offset + last.length
	} else {
		s.currentSeq = 0
		s.currentBytes = 0
		if s.current != nil {
			_ = s.current.Close()
			s.current = nil
		}
	}
	return pruneUnreferencedTerminalGridPages(s.dir, refs)
}

func terminalGridRetentionRetainedRows(dir string, refs []terminalGridRowRef, policy terminalGridRetentionPolicy, now time.Time) (int, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	retainRows := len(refs)
	if policy.maxLogicalLines > 0 {
		if rows := terminalGridRetentionRowsForLogicalLineLimit(refs, policy.maxLogicalLines); rows > 0 && rows < retainRows {
			retainRows = rows
		}
	}
	if policy.maxRetainedBytes > 0 {
		if rows := terminalGridRetentionRowsForByteLimit(refs, policy.maxRetainedBytes); rows >= 0 && rows < retainRows {
			retainRows = rows
		}
	}
	if policy.maxAge > 0 {
		rows, err := terminalGridRetentionRowsForAgeLimit(dir, refs, policy.maxAge, now)
		if err != nil {
			return 0, err
		}
		if rows >= 0 && rows < retainRows {
			retainRows = rows
		}
	}
	return retainRows, nil
}

func terminalGridRetentionRowsForLogicalLineLimit(refs []terminalGridRowRef, logicalLines int) int {
	if logicalLines <= 0 || len(refs) == 0 {
		return 0
	}
	retainedLines := 0
	for end := len(refs) - 1; end >= 0; {
		start := end
		for start > 0 && terminalGridRowContinuesLogicalLine(refs[start-1]) {
			start--
		}
		if !terminalGridRowContinuesLogicalLine(refs[end]) || end == len(refs)-1 {
			retainedLines++
		}
		if retainedLines >= logicalLines {
			return len(refs) - start
		}
		end = start - 1
	}
	return len(refs)
}

func terminalGridRetentionRowsForByteLimit(refs []terminalGridRowRef, maxBytes int64) int {
	if maxBytes <= 0 || len(refs) == 0 {
		return 0
	}
	var retainedBytes int64
	for start := len(refs) - 1; start >= 0; start-- {
		retainedBytes += refs[start].length
		if retainedBytes > maxBytes {
			return len(refs) - start - 1
		}
	}
	return len(refs)
}

func terminalGridRetentionRowsForAgeLimit(dir string, refs []terminalGridRowRef, maxAge time.Duration, now time.Time) (int, error) {
	if maxAge <= 0 || len(refs) == 0 {
		return 0, nil
	}
	rows, err := readTerminalGridRows(dir, refs)
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-maxAge)
	retainStart := len(refs)
	for end := len(refs) - 1; end >= 0; {
		start := end
		for start > 0 && terminalGridRowContinuesLogicalLine(refs[start-1]) {
			start--
		}
		newest := time.Time{}
		for i := start; i <= end && i < len(rows); i++ {
			if rows[i].timestamp.After(newest) {
				newest = rows[i].timestamp
			}
		}
		if !newest.IsZero() && newest.Before(cutoff) {
			break
		}
		retainStart = start
		end = start - 1
	}
	return len(refs) - retainStart, nil
}

func terminalGridLogicalLineCountForRefs(refs []terminalGridRowRef) int {
	if len(refs) == 0 {
		return 0
	}
	count := 0
	for i, ref := range refs {
		if !terminalGridRowContinuesLogicalLine(ref) || i == len(refs)-1 {
			count++
		}
	}
	return count
}

func readTerminalGridRows(dir string, refs []terminalGridRowRef) ([]terminalGridRow, error) {
	out := make([]terminalGridRow, 0, len(refs))
	var currentSeq uint32
	var file *os.File
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	for _, ref := range refs {
		if ref.length <= 0 {
			continue
		}
		if file == nil || currentSeq != ref.seq {
			if file != nil {
				if err := file.Close(); err != nil {
					return nil, err
				}
			}
			f, err := os.Open(filepath.Join(dir, terminalGridPageName(ref.seq)))
			if err != nil {
				return nil, err
			}
			file = f
			currentSeq = ref.seq
		}
		payload := make([]byte, ref.length)
		if _, err := file.ReadAt(payload, ref.offset); err != nil {
			return nil, err
		}
		row, err := decodeTerminalGridRow(payload)
		if err != nil {
			return nil, err
		}
		row.wrapped = ref.flags&terminalGridRowFlagWrapped != 0
		out = append(out, row)
	}
	return out, nil
}

func encodeGridRowsReplay(rows []terminalGridRow) []byte {
	if len(rows) == 0 {
		return nil
	}
	plainRows := make([][]vterm.Cell, 0, len(rows))
	wrapped := make([]bool, 0, len(rows))
	for _, row := range rows {
		plainRows = append(plainRows, row.cells)
		wrapped = append(wrapped, row.wrapped)
	}
	return vterm.EncodeHistoryRowsReplayWithWrapped(plainRows, wrapped)
}

func reflowTerminalGridRows(rows []terminalGridRow, cols int) ([][]vterm.Cell, []time.Time, []string, []bool) {
	if len(rows) == 0 {
		return nil, nil, nil, nil
	}
	if cols <= 0 {
		cols = 80
	}
	reflow := terminalGridReflowState{
		cols:       cols,
		rows:       make([][]vterm.Cell, 0, minInt(len(rows), 1024)),
		timestamps: make([]time.Time, 0, minInt(len(rows), 1024)),
		rowKinds:   make([]string, 0, minInt(len(rows), 1024)),
		wrapped:    make([]bool, 0, minInt(len(rows), 1024)),
	}
	var logical []terminalGridReflowCell
	var emptyMeta terminalGridReflowMeta
	for _, row := range rows {
		meta := terminalGridReflowMeta{timestamp: row.timestamp, rowKind: row.rowKind}
		if len(logical) == 0 {
			emptyMeta = meta
		}
		for i, cell := range row.cells {
			logical = append(logical, terminalGridReflowCell{
				cell:      cell,
				meta:      meta,
				continued: row.wrapped && i == len(row.cells)-1,
			})
		}
		if row.wrapped {
			continue
		}
		reflow.emitLogicalLine(logical, emptyMeta)
		clear(logical)
		logical = logical[:0]
		emptyMeta = terminalGridReflowMeta{}
	}
	if len(logical) > 0 {
		reflow.emitLogicalLine(logical, emptyMeta)
	}
	if len(reflow.rows) == 0 {
		reflow.rows = [][]vterm.Cell{make([]vterm.Cell, cols)}
	}
	return reflow.rows, alignGridTimes(reflow.timestamps, len(reflow.rows)), alignGridStrings(reflow.rowKinds, len(reflow.rows)), alignGridBools(reflow.wrapped, len(reflow.rows))
}

type terminalGridReflowMeta struct {
	timestamp time.Time
	rowKind   string
}

type terminalGridReflowCell struct {
	cell      vterm.Cell
	meta      terminalGridReflowMeta
	continued bool
}

type terminalGridReflowState struct {
	cols       int
	rows       [][]vterm.Cell
	timestamps []time.Time
	rowKinds   []string
	wrapped    []bool
}

func (r *terminalGridReflowState) emitLogicalLine(cells []terminalGridReflowCell, emptyMeta terminalGridReflowMeta) {
	if r == nil {
		return
	}
	if len(cells) == 0 {
		r.emitSegment(nil, emptyMeta, false, false)
		return
	}
	start := 0
	width := 0
	segmentIndex := 0
	for i, cell := range cells {
		cellWidth := cell.cell.Width
		if cellWidth <= 0 {
			continue
		}
		if width > 0 && width+cellWidth > r.cols {
			r.emitSegment(cells[start:i], cells[start].meta, true, segmentIndex > 0)
			segmentIndex++
			start = i
			width = 0
		}
		width += cellWidth
	}
	wrapped := false
	if len(cells) > 0 {
		wrapped = cells[len(cells)-1].continued
	}
	r.emitSegment(cells[start:], cells[start].meta, wrapped, segmentIndex > 0)
}

func (r *terminalGridReflowState) emitSegment(segment []terminalGridReflowCell, meta terminalGridReflowMeta, wrapped bool, suppressRowKind bool) {
	if r == nil {
		return
	}
	row := make([]vterm.Cell, len(segment))
	for i, cell := range segment {
		row[i] = cell.cell
	}
	r.rows = append(r.rows, row)
	r.timestamps = append(r.timestamps, meta.timestamp)
	if suppressRowKind {
		r.rowKinds = append(r.rowKinds, "")
	} else {
		r.rowKinds = append(r.rowKinds, meta.rowKind)
	}
	r.wrapped = append(r.wrapped, wrapped)
}

func gridCellRowDisplayWidth(row []vterm.Cell) int {
	width := 0
	for _, cell := range row {
		if cell.Width > 0 {
			width += cell.Width
		}
	}
	return width
}

func alignGridTimes(values []time.Time, size int) []time.Time {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) == size {
		return cloneTimeSlice(values)
	}
	out := make([]time.Time, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func alignGridStrings(values []string, size int) []string {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) == size {
		return cloneStringSlice(values)
	}
	out := make([]string, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func alignGridBools(values []bool, size int) []bool {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) == size {
		return cloneBoolSlice(values)
	}
	out := make([]bool, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func trimTimeSliceTail(values []time.Time, limit int) []time.Time {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return cloneTimeSlice(values)
	}
	return cloneTimeSlice(values[len(values)-limit:])
}

func trimStringSliceTail(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return cloneStringSlice(values)
	}
	return cloneStringSlice(values[len(values)-limit:])
}

func trimBoolSliceTail(values []bool, limit int) []bool {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return cloneBoolSlice(values)
	}
	return cloneBoolSlice(values[len(values)-limit:])
}

func timeAt(values []time.Time, index int) time.Time {
	if index < 0 || index >= len(values) {
		return time.Time{}
	}
	return values[index]
}

func stringAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func boolAt(values []bool, index int) bool {
	return index >= 0 && index < len(values) && values[index]
}

func isBlankGridRow(row []vterm.Cell) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell.Content) != "" {
			return false
		}
		if cell.Style != (vterm.CellStyle{}) {
			return false
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			return false
		}
	}
	return true
}

func terminalGridDir(root, terminalID string) string {
	sum := sha1.Sum([]byte(terminalID))
	return filepath.Join(root, fmt.Sprintf("terminal-grid-v%d-%s-%s", terminalGridStoreVersion, sanitizeGridStoreID(terminalID), hex.EncodeToString(sum[:6])))
}

func terminalGridPageName(seq uint32) string {
	return fmt.Sprintf("grid-%06d.page", seq)
}

func terminalGridPageSeq(name string) (uint32, bool) {
	if !strings.HasPrefix(name, "grid-") || !strings.HasSuffix(name, ".page") {
		return 0, false
	}
	seqText := strings.TrimSuffix(strings.TrimPrefix(name, "grid-"), ".page")
	if len(seqText) != 6 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(seqText, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

func readTerminalGridMetadata(dir string) (terminalGridMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, terminalGridMetadataName))
	if err != nil {
		return terminalGridMetadata{}, err
	}
	var metadata terminalGridMetadata
	var msg wirepb.TerminalGridMetadata
	if err := proto.Unmarshal(data, &msg); err != nil {
		return terminalGridMetadata{}, err
	}
	metadata = terminalGridMetadataFromProto(&msg)
	if metadata.StoreVersion != 0 && metadata.StoreVersion != terminalGridStoreVersion {
		return terminalGridMetadata{}, fmt.Errorf("unsupported terminal grid store version %d", metadata.StoreVersion)
	}
	if metadata.RowCodec != "" && metadata.RowCodec != terminalGridRowCodec {
		return terminalGridMetadata{}, fmt.Errorf("unsupported terminal grid row codec %q", metadata.RowCodec)
	}
	if metadata.IndexCodec != "" && metadata.IndexCodec != terminalGridIndexCodec {
		return terminalGridMetadata{}, fmt.Errorf("unsupported terminal grid index codec %q", metadata.IndexCodec)
	}
	return metadata, nil
}

func writeTerminalGridMetadata(dir string, metadata terminalGridMetadata) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if metadata.StoreVersion == 0 {
		metadata.StoreVersion = terminalGridStoreVersion
	}
	if metadata.RowCodec == "" {
		metadata.RowCodec = terminalGridRowCodec
	}
	if metadata.IndexCodec == "" {
		metadata.IndexCodec = terminalGridIndexCodec
	}
	if metadata.PageMaxBytes <= 0 {
		metadata.PageMaxBytes = defaultGridPageMaxBytes
	}
	now := time.Now().UTC().Unix()
	if metadata.CreatedAtUnix == 0 {
		metadata.CreatedAtUnix = now
	}
	if metadata.Generation == 0 {
		metadata.Generation = terminalGridHistoryGeneration(metadata.CreatedAtUnix)
	}
	metadata.UpdatedAtUnix = now
	data, err := proto.Marshal(terminalGridMetadataToProto(metadata))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, terminalGridMetadataName), data, 0o600)
}

func terminalGridMetadataToProto(metadata terminalGridMetadata) *wirepb.TerminalGridMetadata {
	return &wirepb.TerminalGridMetadata{
		StoreVersion:      int32(metadata.StoreVersion),
		TerminalId:        metadata.TerminalID,
		RowCodec:          metadata.RowCodec,
		IndexCodec:        metadata.IndexCodec,
		PageMaxBytes:      metadata.PageMaxBytes,
		RowCount:          int64(metadata.RowCount),
		PageCount:         int64(metadata.PageCount),
		CreatedAtUnix:     metadata.CreatedAtUnix,
		UpdatedAtUnix:     metadata.UpdatedAtUnix,
		BaseRowId:         metadata.BaseRowID,
		HistoryGeneration: metadata.Generation,
	}
}

func terminalGridMetadataFromProto(msg *wirepb.TerminalGridMetadata) terminalGridMetadata {
	if msg == nil {
		return terminalGridMetadata{}
	}
	return terminalGridMetadata{
		StoreVersion:  int(msg.GetStoreVersion()),
		TerminalID:    msg.GetTerminalId(),
		RowCodec:      msg.GetRowCodec(),
		IndexCodec:    msg.GetIndexCodec(),
		PageMaxBytes:  msg.GetPageMaxBytes(),
		RowCount:      int(msg.GetRowCount()),
		PageCount:     int(msg.GetPageCount()),
		CreatedAtUnix: msg.GetCreatedAtUnix(),
		UpdatedAtUnix: msg.GetUpdatedAtUnix(),
		BaseRowID:     msg.GetBaseRowId(),
		Generation:    msg.GetHistoryGeneration(),
	}
}

func terminalGridHistoryGeneration(createdAtUnix int64) uint64 {
	if createdAtUnix > 0 {
		return uint64(createdAtUnix)
	}
	return uint64(time.Now().UTC().UnixNano())
}

type terminalGridIndexState struct {
	rowCount int
	lastSeq  uint32
}

func loadTerminalGridIndexState(dir string) (terminalGridIndexState, error) {
	indexPath := filepath.Join(dir, terminalGridIndexName)
	file, err := os.OpenFile(indexPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return terminalGridIndexState{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return terminalGridIndexState{}, err
	}
	usableSize := info.Size() - info.Size()%terminalGridIndexRecord
	if usableSize != info.Size() {
		if err := file.Truncate(usableSize); err != nil {
			return terminalGridIndexState{}, err
		}
	}
	rowCount := int(usableSize / terminalGridIndexRecord)
	validRows := 0
	var lastSeq uint32
	var truncatePages []terminalGridRowRef
	for row := 0; row < rowCount; row++ {
		ref, err := readTerminalGridIndexRecordFromFile(file, row)
		if err != nil {
			break
		}
		if err := validateTerminalGridRowRef(dir, ref); err != nil {
			break
		}
		validRows = row + 1
		lastSeq = ref.seq
		truncatePages = append(truncatePages, ref)
	}
	if validRows != rowCount {
		if err := file.Truncate(int64(validRows * terminalGridIndexRecord)); err != nil {
			return terminalGridIndexState{}, err
		}
	}
	if err := truncateTerminalGridPages(dir, truncatePages); err != nil {
		return terminalGridIndexState{}, err
	}
	if validRows <= 0 {
		return terminalGridIndexState{}, nil
	}
	return terminalGridIndexState{rowCount: validRows, lastSeq: lastSeq}, nil
}

func validateTerminalGridRowRef(dir string, ref terminalGridRowRef) error {
	if ref.length <= 0 {
		return fmt.Errorf("termx grid index row length out of range: %d", ref.length)
	}
	pagePath := filepath.Join(dir, terminalGridPageName(ref.seq))
	file, err := os.Open(pagePath)
	if err != nil {
		return err
	}
	defer file.Close()
	payload := make([]byte, ref.length)
	if _, err := file.ReadAt(payload, ref.offset); err != nil {
		return err
	}
	if _, err := decodeTerminalGridRow(payload); err != nil {
		return err
	}
	return nil
}

func writeTerminalGridIndexRefs(file *os.File, refs []terminalGridRowRef) error {
	if file == nil || len(refs) == 0 {
		return nil
	}
	buf := make([]byte, len(refs)*terminalGridIndexRecord)
	for i, ref := range refs {
		if ref.length <= 0 || ref.length > int64(^uint32(0)) {
			return fmt.Errorf("termx grid row length out of range: %d", ref.length)
		}
		base := i * terminalGridIndexRecord
		binary.LittleEndian.PutUint32(buf[base:base+4], ref.seq)
		binary.LittleEndian.PutUint64(buf[base+4:base+12], uint64(ref.offset))
		binary.LittleEndian.PutUint32(buf[base+12:base+16], uint32(ref.length))
		binary.LittleEndian.PutUint32(buf[base+16:base+20], ref.flags)
	}
	n, err := file.Write(buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
}

func truncateTerminalGridPages(dir string, refs []terminalGridRowRef) error {
	keepEnd := make(map[uint32]int64)
	var maxSeq uint32
	haveMax := false
	for _, ref := range refs {
		end := ref.offset + ref.length
		if end > keepEnd[ref.seq] {
			keepEnd[ref.seq] = end
		}
		if !haveMax || ref.seq > maxSeq {
			maxSeq = ref.seq
			haveMax = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seq, ok := terminalGridPageSeq(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		end, keep := keepEnd[seq]
		if !keep {
			if !haveMax || seq > maxSeq {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			continue
		}
		if err := os.Truncate(path, end); err != nil {
			return err
		}
	}
	return nil
}

func pruneUnreferencedTerminalGridPages(dir string, refs []terminalGridRowRef) error {
	keep := make(map[uint32]struct{})
	for _, ref := range refs {
		keep[ref.seq] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seq, ok := terminalGridPageSeq(entry.Name())
		if !ok {
			continue
		}
		if _, ok := keep[seq]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func readTerminalGridIndexRecordFromFile(file *os.File, row int) (terminalGridRowRef, error) {
	if file == nil || row < 0 {
		return terminalGridRowRef{}, ErrNotFound
	}
	rec := make([]byte, terminalGridIndexRecord)
	if _, err := file.ReadAt(rec, int64(row*terminalGridIndexRecord)); err != nil {
		return terminalGridRowRef{}, err
	}
	return decodeTerminalGridIndexRecord(rec)
}

func readTerminalGridIndexRefsFromPath(path string) ([]terminalGridRowRef, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	count := int(info.Size() / terminalGridIndexRecord)
	if count <= 0 {
		return nil, nil
	}
	return readTerminalGridIndexWindowFromFile(file, 0, count)
}

func readTerminalGridIndexWindowFromFile(file *os.File, start int, count int) ([]terminalGridRowRef, error) {
	if file == nil || start < 0 {
		return nil, ErrNotFound
	}
	if count <= 0 {
		return nil, nil
	}
	buf := make([]byte, count*terminalGridIndexRecord)
	if _, err := file.ReadAt(buf, int64(start*terminalGridIndexRecord)); err != nil {
		return nil, err
	}
	refs := make([]terminalGridRowRef, 0, count)
	for offset := 0; offset < len(buf); offset += terminalGridIndexRecord {
		ref, err := decodeTerminalGridIndexRecord(buf[offset : offset+terminalGridIndexRecord])
		if err != nil {
			return nil, err
		}
		if ref.length <= 0 {
			continue
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func decodeTerminalGridIndexRecord(rec []byte) (terminalGridRowRef, error) {
	if len(rec) != terminalGridIndexRecord {
		return terminalGridRowRef{}, io.ErrUnexpectedEOF
	}
	seq := binary.LittleEndian.Uint32(rec[0:4])
	offset := int64(binary.LittleEndian.Uint64(rec[4:12]))
	length := int64(binary.LittleEndian.Uint32(rec[12:16]))
	flags := binary.LittleEndian.Uint32(rec[16:20])
	if length <= 0 {
		return terminalGridRowRef{}, fmt.Errorf("termx grid index row length out of range: %d", length)
	}
	return terminalGridRowRef{seq: seq, offset: offset, length: length, flags: flags}, nil
}

func observeTerminalGridIDs(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metadata, err := readTerminalGridMetadata(filepath.Join(root, entry.Name()))
		if err != nil || metadata.TerminalID == "" {
			continue
		}
		ObserveGeneratedID(metadata.TerminalID)
	}
}

func sanitizeGridStoreID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "terminal"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "terminal"
	}
	return out
}

func closeTerminalGridStore(store *terminalGridStore) error {
	if store == nil {
		return nil
	}
	err := store.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
