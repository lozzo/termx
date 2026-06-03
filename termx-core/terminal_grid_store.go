package termx

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	terminalGridLineMetaName = "grid.lines.json"
	terminalGridIndexName    = "grid.index"
	terminalGridIndexRecord  = 20
)

type terminalGridStore struct {
	mu               sync.Mutex
	dir              string
	terminalID       string
	current          *os.File
	index            *os.File
	currentSeq       uint32
	currentBytes     int64
	baseRowID        uint64
	generation       uint64
	pageMaxBytes     int64
	rowCount         int
	retentionPolicy  terminalGridRetentionPolicy
	closed           bool
	removeOnClose    bool
	writable         bool
	lineRecords      []terminalGridLogicalLineRecord
	lineMigrations   map[uint64]uint64
	maxRuntimeLineID uint64
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

type terminalLogicalLineResidency string

const (
	terminalLogicalLineResidencyPersisted terminalLogicalLineResidency = "persisted"
	terminalLogicalLineResidencyLiveTail  terminalLogicalLineResidency = "live-tail"
)

type terminalLogicalLineRecordSource string

const (
	terminalLogicalLineRecordSourceExplicit terminalLogicalLineRecordSource = "explicit"
	terminalLogicalLineRecordSourceFallback terminalLogicalLineRecordSource = "fallback"
)

type terminalGridLogicalLineRecord struct {
	id         uint64
	startRow   int
	endRow     int
	sealed     bool
	origin     terminalLiveTailOrigin
	residency  terminalLogicalLineResidency
	dirty      bool
	generation uint64
	source     terminalLogicalLineRecordSource
}

func terminalGridLogicalLineRecordAuthoritative(record terminalGridLogicalLineRecord) bool {
	return record.source == terminalLogicalLineRecordSourceExplicit
}

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

type terminalGridLineMetadata struct {
	Records     []terminalGridLineRecordMeta `json:"records,omitempty"`
	LiveRecords []terminalGridLineRecordMeta `json:"live_records,omitempty"`
	LiveRows    []terminalGridLineRowMeta    `json:"live_rows,omitempty"`
	Migrations  []terminalGridLineMigration  `json:"migrations,omitempty"`
}

type terminalGridLineRecordMeta struct {
	ID         uint64                          `json:"id"`
	StartRow   int                             `json:"start_row"`
	EndRow     int                             `json:"end_row"`
	RowIDKnown bool                            `json:"row_id_known,omitempty"`
	FirstRowID uint64                          `json:"first_row_id,omitempty"`
	LastRowID  uint64                          `json:"last_row_id,omitempty"`
	Sealed     bool                            `json:"sealed"`
	Origin     terminalLiveTailOrigin          `json:"origin"`
	Residency  terminalLogicalLineResidency    `json:"residency"`
	Dirty      bool                            `json:"dirty"`
	Generation uint64                          `json:"generation"`
	Source     terminalLogicalLineRecordSource `json:"source,omitempty"`
}

type terminalGridLineMigration struct {
	RuntimeID   uint64 `json:"runtime_id"`
	PersistedID uint64 `json:"persisted_id"`
}

type terminalGridLineRowMeta struct {
	Payload []byte `json:"payload"`
	Wrapped bool   `json:"wrapped"`
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
		dir:              dir,
		terminalID:       terminalID,
		currentSeq:       indexState.lastSeq,
		baseRowID:        metadata.BaseRowID,
		generation:       metadata.Generation,
		pageMaxBytes:     metadata.PageMaxBytes,
		rowCount:         indexState.rowCount,
		removeOnClose:    removeOnClose,
		writable:         create,
		maxRuntimeLineID: terminalGridMaxRuntimeLogicalLineIDFromSidecar(dir),
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

func (s *terminalGridStore) truncateTailRows(count int) error {
	if s == nil || count <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if count >= s.rowCount {
		count = s.rowCount
	}
	keepRows := s.rowCount - count
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil {
		return err
	}
	if keepRows < 0 {
		keepRows = 0
	}
	if keepRows > len(refs) {
		keepRows = len(refs)
	}
	refs = refs[:keepRows]
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
	if len(refs) > 0 {
		last := refs[len(refs)-1]
		s.currentSeq = last.seq
		truncateOffset := last.offset + last.length
		if s.current != nil {
			_ = s.current.Close()
			s.current = nil
		}
		if err := os.Truncate(filepath.Join(s.dir, terminalGridPageName(last.seq)), truncateOffset); err != nil {
			return err
		}
		if err := s.openCurrentPageLocked(); err != nil {
			return err
		}
		s.currentBytes = truncateOffset
	} else {
		s.currentSeq = 0
		s.currentBytes = 0
		if s.current != nil {
			_ = s.current.Close()
			s.current = nil
		}
		if err := os.Truncate(filepath.Join(s.dir, terminalGridPageName(0)), 0); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := pruneUnreferencedTerminalGridPages(s.dir, refs); err != nil {
		return err
	}
	s.generation++
	if s.generation == 0 {
		s.generation = 1
	}
	if s.writable && !s.removeOnClose {
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
		if existing, err := readTerminalGridMetadata(s.dir); err == nil {
			metadata.CreatedAtUnix = existing.CreatedAtUnix
		}
		if err := writeTerminalGridMetadata(s.dir, metadata); err != nil {
			return err
		}
		records, err := writeTerminalGridCompletePersistedLineRecordsMetadata(s.dir, s.baseRowID, s.generation)
		s.lineRecords = cloneTerminalGridLogicalLineRecords(records)
		return err
	}
	s.lineRecords = terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, s.baseRowID, s.generation)
	return nil
}

type testTempDirProvider interface {
	TempDir() string
}

func (s *terminalGridStore) AppendDamageRows(rows []vterm.DamageOp) error {
	return s.AppendDamageRowsWithLogicalLineIDs(rows, nil)
}

func (s *terminalGridStore) AppendDamageRowsWithLogicalLineIDs(rows []vterm.DamageOp, logicalLineIDs []uint64) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	traceGridDamageOps("core.grid_store.append_damage_rows", s.terminalID, rows)
	return s.appendRowSequence(len(rows), func(i int) terminalGridRow {
		row := rows[i]
		return terminalGridRow{
			cells:         row.Cells,
			runs:          row.Runs,
			timestamp:     row.Timestamp,
			rowKind:       row.RowKind,
			wrapped:       row.WrappedSet && row.Wrapped,
			logicalLineID: uint64At(logicalLineIDs, i),
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

func (s *terminalGridStore) appendGridRows(rows []terminalGridRow) error {
	if len(rows) == 0 {
		return nil
	}
	return s.appendRowSequence(len(rows), func(i int) terminalGridRow {
		return rows[i]
	})
}

type terminalGridRow struct {
	cells         []vterm.Cell
	runs          []vterm.CellRun
	timestamp     time.Time
	rowKind       string
	wrapped       bool
	logicalLineID uint64
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
	appendStartRow := s.rowCount
	appendedRefs := make([]terminalGridRowRef, 0, count)
	appendedLogicalLineIDs := make([]uint64, 0, count)
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
		appendedRefs = append(appendedRefs, refs...)
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
		appendedLogicalLineIDs = append(appendedLogicalLineIDs, row.logicalLineID)
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
	if hasNonZeroUint64(appendedLogicalLineIDs) && s.lineMigrations == nil {
		s.lineMigrations = make(map[uint64]uint64)
	}
	lineRecordsUpdated := false
	if s.writable && !s.removeOnClose && hasNonZeroUint64(appendedLogicalLineIDs) {
		records, err := writeTerminalGridPersistedLineRecordsMetadataForAppend(s.dir, s.baseRowID, s.generation, appendStartRow, appendedLogicalLineIDs)
		if err != nil {
			records, err = writeTerminalGridCompletePersistedLineRecordsMetadata(s.dir, s.baseRowID, s.generation)
			if err != nil {
				finish(0)
				return err
			}
		}
		s.lineRecords = cloneTerminalGridLogicalLineRecords(records)
		_ = terminalGridExplicitLogicalLineRecordsForAppendedRowsWithMigrations(appendedRefs, s.baseRowID+uint64(appendStartRow), s.generation, 0, appendedLogicalLineIDs, s.lineMigrations)
		lineRecordsUpdated = true
		if err := recordTerminalGridLineMigrationsForAppendedLogicalLineIDs(s.dir, s.generation, s.rowCount, appendStartRow, appendedLogicalLineIDs); err != nil {
			finish(0)
			return err
		}
		if migrationMax := terminalGridMaxRuntimeLogicalLineIDFromSidecar(s.dir); migrationMax > s.maxRuntimeLineID {
			s.maxRuntimeLineID = migrationMax
		}
	}
	if err := s.enforceMaxRowsLocked(); err != nil {
		finish(0)
		return err
	}
	if hasNonZeroUint64(appendedLogicalLineIDs) && !lineRecordsUpdated {
		if drop := len(appendedRefs) - (s.rowCount - appendStartRow); drop > 0 && drop <= len(appendedRefs) {
			appendedRefs = appendedRefs[drop:]
			appendedLogicalLineIDs = trimUint64Prefix(appendedLogicalLineIDs, drop)
			appendStartRow += drop
		}
		records := terminalGridExplicitLogicalLineRecordsForAppendedRowsWithMigrations(appendedRefs, s.baseRowID+uint64(appendStartRow), s.generation, 0, appendedLogicalLineIDs, s.lineMigrations)
		if len(records) > 0 {
			if merged := terminalGridMergeInMemoryPersistedLineRecords(s.lineRecords, records, appendStartRow, s.rowCount); len(merged) > 0 {
				s.lineRecords = merged
			}
		}
	} else if s.writable && !s.removeOnClose && !hasNonZeroUint64(appendedLogicalLineIDs) {
		records, err := writeTerminalGridCompletePersistedLineRecordsMetadata(s.dir, s.baseRowID, s.generation)
		if err != nil {
			finish(0)
			return err
		}
		s.lineRecords = cloneTerminalGridLogicalLineRecords(records)
	}
	if migrationMax := terminalGridLineMigrationMapMaxRuntimeID(s.lineMigrations); migrationMax > s.maxRuntimeLineID {
		s.maxRuntimeLineID = migrationMax
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
	baseRowID, generation, rowCount := s.coordinates()
	if records, ok := s.persistedLogicalLineRecordsFromMetadata(rowCount, generation); ok {
		return len(records)
	}
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil {
		return 0
	}
	if records, ok := s.sealedPersistedLogicalLineRecordPrefixFromMetadata(rowCount, generation); ok {
		if records, ok := terminalGridLogicalLineRecordsFromSealedPrefixAndRefs(records, refs, baseRowID, generation); ok {
			return len(records)
		}
	}
	return len(terminalGridFallbackLogicalLineRecordsForRefs(refs, 0))
}

func (s *terminalGridStore) authoritativeLogicalLineCount() int {
	if s == nil {
		return 0
	}
	_, generation, rowCount := s.coordinates()
	records, ok := s.persistedLogicalLineRecordsFromMetadata(rowCount, generation)
	if !ok {
		return 0
	}
	return terminalGridAuthoritativeLogicalLineRecordCount(records)
}

func (s *terminalGridStore) logicalLineCountForPrefix(rows int) int {
	if s == nil || rows <= 0 {
		return 0
	}
	baseRowID, generation, rowCount := s.coordinates()
	if rows > rowCount {
		rows = rowCount
	}
	if rows <= 0 {
		return 0
	}
	if records, ok := s.persistedLogicalLineRecordsFromMetadata(rowCount, generation); ok {
		return terminalGridLogicalLineRecordPrefixCount(records, rows)
	}
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil {
		return 0
	}
	if records, ok := s.sealedPersistedLogicalLineRecordPrefixFromMetadata(rowCount, generation); ok {
		if records, ok := terminalGridLogicalLineRecordsFromSealedPrefixAndRefs(records, refs, baseRowID, generation); ok {
			return terminalGridLogicalLineRecordPrefixCount(records, rows)
		}
	}
	records := terminalGridFallbackLogicalLineRecordsForRefs(refs, 0)
	return terminalGridLogicalLineRecordPrefixCount(records, rows)
}

func (s *terminalGridStore) authoritativeLogicalLineCountForPrefix(rows int) int {
	if s == nil || rows <= 0 {
		return 0
	}
	_, generation, rowCount := s.coordinates()
	if rows > rowCount {
		rows = rowCount
	}
	if rows <= 0 {
		return 0
	}
	records, ok := s.persistedLogicalLineRecordsFromMetadata(rowCount, generation)
	if !ok {
		return 0
	}
	return terminalGridAuthoritativeLogicalLineRecordPrefixCount(records, rows)
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
	if cols <= 0 {
		cols = 80
	}
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
		generation, firstRowID, lastRowID, totalRows := s.rowWindowCoordinates(beforeOffset, rows)
		lineRecords := s.persistedLogicalLineRecordsForViewport(refs, firstRowID, totalRows, generation)
		rowIDs := terminalGridRowIDs(firstRowID, len(gridRows))
		result.Rows, result.Timestamps, result.RowKinds, result.Wrapped, result.LogicalLineIDs, result.LogicalLineIDAuthoritative, result.RowIDRanges = reflowTerminalGridRowsWithRowIDRanges(gridRows, cols, lineRecords, rowIDs)
		result.Ownership = repeatedString(RowOwnershipPersisted, len(result.Rows))
		result.Generation = generation
		result.FirstRowID = firstRowID
		result.LastRowID = lastRowID
		cropped := false
		if hasMore {
			cropped = trimTerminalGridViewportToTail(&result, limit)
		}
		result.LoadedRows = beforeOffset + rows
		result.HasMore = hasMore || cropped
		result.BeforeOffset = beforeOffset
		result.Limit = limit
		result.Size = Size{Cols: uint16(cols)}
		result.TotalRows = totalRows
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
	Size                       Size
	Rows                       [][]vterm.Cell
	Timestamps                 []time.Time
	RowKinds                   []string
	Wrapped                    []bool
	Ownership                  []string
	LogicalLineIDs             []uint64
	LogicalLineIDAuthoritative []bool
	RowIDRanges                []terminalGridRowIDRange
	LoadedRows                 int
	HasMore                    bool
	BeforeOffset               int
	Limit                      int
	TotalRows                  int
	LogicalTotal               int
	WindowLogicalTotal         int
	FirstLineClippedBefore     bool
	Generation                 uint64
	FirstRowID                 uint64
	LastRowID                  uint64
}

type terminalGridRowIDRange struct {
	First uint64
	Last  uint64
	Known bool
}

func trimTerminalGridViewportToTail(result *terminalGridViewport, limit int) bool {
	if result == nil || limit <= 0 || len(result.Rows) <= limit {
		return false
	}
	start := len(result.Rows) - limit
	inheritedRowKind := clippedViewportLeadingRowKind(result.RowKinds, result.LogicalLineIDs, result.LogicalLineIDAuthoritative, start)
	result.FirstLineClippedBefore = terminalGridViewportTrimClipsLogicalLine(result, start)
	result.Rows = result.Rows[start:]
	result.Timestamps = trimTimeSliceTail(result.Timestamps, limit)
	result.RowKinds = trimStringSliceTail(result.RowKinds, limit)
	if inheritedRowKind != "" && len(result.RowKinds) > 0 && result.RowKinds[0] == "" {
		result.RowKinds[0] = inheritedRowKind
	}
	result.Wrapped = trimBoolSliceTail(result.Wrapped, limit)
	result.Ownership = trimStringSliceTail(result.Ownership, limit)
	result.LogicalLineIDs = trimUint64SliceTail(result.LogicalLineIDs, limit)
	result.LogicalLineIDAuthoritative = trimBoolSliceTail(result.LogicalLineIDAuthoritative, limit)
	result.RowIDRanges = trimTerminalGridRowIDRangesTail(result.RowIDRanges, limit)
	return true
}

func terminalGridViewportRefreshRowIDBoundary(result *terminalGridViewport) {
	if result == nil || len(result.RowIDRanges) == 0 {
		return
	}
	firstRowID, lastRowID, ok := terminalGridRowIDRangeBoundary(result.RowIDRanges)
	if !ok {
		result.FirstRowID = 0
		result.LastRowID = 0
		return
	}
	result.FirstRowID = firstRowID
	result.LastRowID = lastRowID
}

func terminalGridViewportTrimClipsLogicalLine(result *terminalGridViewport, start int) bool {
	if result == nil || start <= 0 || start >= len(result.Rows) {
		return false
	}
	if !terminalGridViewportLogicalLineIDAuthoritative(result.LogicalLineIDAuthoritative, start) || !terminalGridViewportLogicalLineIDAuthoritative(result.LogicalLineIDAuthoritative, start-1) {
		return false
	}
	currentID := uint64At(result.LogicalLineIDs, start)
	previousID := uint64At(result.LogicalLineIDs, start-1)
	return currentID != 0 && previousID != 0 && currentID == previousID
}

func clippedViewportLeadingRowKind(rowKinds []string, logicalLineIDs []uint64, logicalLineIDAuthoritative []bool, start int) string {
	if start <= 0 {
		return ""
	}
	if !terminalGridViewportLogicalLineIDAuthoritative(logicalLineIDAuthoritative, start) || !terminalGridViewportLogicalLineIDAuthoritative(logicalLineIDAuthoritative, start-1) {
		return ""
	}
	currentID := uint64At(logicalLineIDs, start)
	previousID := uint64At(logicalLineIDs, start-1)
	if currentID == 0 || previousID == 0 || currentID != previousID {
		return ""
	}
	for i := start - 1; i >= 0; i-- {
		if uint64At(logicalLineIDs, i) != currentID {
			break
		}
		if !terminalGridViewportLogicalLineIDAuthoritative(logicalLineIDAuthoritative, i) {
			break
		}
		if kind := stringAt(rowKinds, i); kind != "" {
			return kind
		}
	}
	return ""
}

func terminalGridViewportLogicalLineIDAuthoritative(authoritative []bool, row int) bool {
	if row < 0 {
		return false
	}
	if len(authoritative) == 0 {
		return true
	}
	return boolAt(authoritative, row)
}

func (s *terminalGridStore) reclaimViewport(neededRows int, cols int) (terminalGridViewport, error) {
	var result terminalGridViewport
	if s == nil || neededRows <= 0 {
		return result, nil
	}
	baseRowID, generation, totalRows := s.coordinates()
	if totalRows <= 0 {
		return result, nil
	}
	indexPath := filepath.Join(s.dir, terminalGridIndexName)
	if cols <= 0 {
		cols = 80
	}
	limit := neededRows
	for {
		end := totalRows
		start := end - limit
		if start < 0 {
			start = 0
		}
		refs, err := readTerminalGridIndexRefsFromPath(indexPath)
		if err != nil {
			return result, err
		}
		if end > len(refs) {
			end = len(refs)
		}
		if start > end {
			start = end
		}
		startLineRecords := s.persistedLogicalLineRecordsForWindowStart(refs, baseRowID, generation, start)
		start = terminalGridWindowStartForRecords(startLineRecords, start)
		refs = refs[start:end]
		if len(refs) == 0 {
			return result, nil
		}
		gridRows, err := readTerminalGridRows(s.dir, refs)
		if err != nil {
			return result, err
		}
		firstRowID := baseRowID + uint64(start)
		lineRecords := s.persistedLogicalLineRecordsForViewport(refs, firstRowID, totalRows, generation)
		rowIDs := terminalGridRowIDs(firstRowID, len(gridRows))
		result.Rows, result.Timestamps, result.RowKinds, result.Wrapped, result.LogicalLineIDs, result.LogicalLineIDAuthoritative, result.RowIDRanges = reflowTerminalGridRowsWithRowIDRanges(gridRows, cols, lineRecords, rowIDs)
		result.Ownership = repeatedString(RowOwnershipPersisted, len(result.Rows))
		result.Generation = generation
		result.Size = Size{Cols: uint16(cols)}
		result.FirstRowID = baseRowID + uint64(start)
		result.LastRowID = baseRowID + uint64(end-1)
		result.TotalRows = totalRows
		result.LogicalTotal = s.LogicalLineCount()
		result.LoadedRows = end - start
		result.BeforeOffset = 0
		result.Limit = neededRows
		result.HasMore = start > 0
		if len(result.Rows) == 0 {
			return result, nil
		}
		if limit >= totalRows || len(result.Rows) >= neededRows {
			if trimStart := terminalGridViewportReclaimStart(result, neededRows); trimStart > 0 {
				trimTerminalGridViewportPrefix(&result, trimStart)
			}
			return result, nil
		}
		nextLimit := limit * 2
		if nextLimit <= limit {
			return result, nil
		}
		if nextLimit > totalRows {
			nextLimit = totalRows
		}
		limit = nextLimit
	}
}

func terminalGridViewportReclaimStart(viewport terminalGridViewport, neededRows int) int {
	if neededRows <= 0 || len(viewport.Rows) <= neededRows {
		return 0
	}
	start := len(viewport.Rows) - neededRows
	for start > 0 {
		if !terminalGridViewportLogicalLineIDAuthoritative(viewport.LogicalLineIDAuthoritative, start) || !terminalGridViewportLogicalLineIDAuthoritative(viewport.LogicalLineIDAuthoritative, start-1) {
			break
		}
		currentID := uint64At(viewport.LogicalLineIDs, start)
		previousID := uint64At(viewport.LogicalLineIDs, start-1)
		if currentID == 0 || previousID == 0 || currentID != previousID {
			break
		}
		start--
	}
	return start
}

func trimTerminalGridViewportPrefix(result *terminalGridViewport, start int) {
	if result == nil || start <= 0 {
		return
	}
	if start >= len(result.Rows) {
		result.Rows = nil
		result.Timestamps = nil
		result.RowKinds = nil
		result.Wrapped = nil
		result.Ownership = nil
		result.LogicalLineIDs = nil
		result.LogicalLineIDAuthoritative = nil
		result.RowIDRanges = nil
		result.LoadedRows = 0
		result.FirstRowID = 0
		result.LastRowID = 0
		return
	}
	result.Rows = cloneVTermCellRows(result.Rows[start:])
	result.Timestamps = cloneTimeSlice(result.Timestamps[start:])
	result.RowKinds = cloneStringSlice(result.RowKinds[start:])
	result.Wrapped = cloneBoolSlice(result.Wrapped[start:])
	result.Ownership = cloneStringSlice(result.Ownership[start:])
	result.LogicalLineIDs = cloneUint64Slice(result.LogicalLineIDs[start:])
	result.LogicalLineIDAuthoritative = cloneBoolSlice(result.LogicalLineIDAuthoritative[start:])
	result.RowIDRanges = cloneTerminalGridRowIDRanges(result.RowIDRanges[start:])
	result.LoadedRows = len(result.Rows)
	terminalGridViewportRefreshRowIDBoundary(result)
}

func cloneVTermCellRows(rows [][]vterm.Cell) [][]vterm.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]vterm.Cell, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneVTermCells(row))
	}
	return out
}

func (s *terminalGridStore) windowRefs(beforeOffset int, limit int) ([]terminalGridRowRef, int, bool, error) {
	s.mu.Lock()
	total := s.rowCount
	baseRowID := s.baseRowID
	generation := s.generation
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
	refs, err := readTerminalGridIndexRefsFromPath(indexPath)
	if err != nil {
		return nil, 0, false, err
	}
	if end > len(refs) {
		end = len(refs)
	}
	if start > end {
		start = end
	}
	lineRecords := s.persistedLogicalLineRecordsForWindowStart(refs, baseRowID, generation, start)
	start = terminalGridWindowStartForRecords(lineRecords, start)
	refs = refs[start:end]

	if len(refs) == 0 {
		return nil, 0, false, nil
	}
	return refs, len(refs), start > 0, nil
}

func terminalGridFallbackRowContinuesLogicalLine(ref terminalGridRowRef) bool {
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

func (s *terminalGridStore) windowFirstRowID(beforeOffset int, loadedRows int) uint64 {
	_, firstRowID, _, _ := s.rowWindowCoordinates(beforeOffset, loadedRows)
	return firstRowID
}

func (s *terminalGridStore) openPersistedLogicalLineID() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lineRecords) == 0 {
		return s.openPersistedLogicalLineIDFromIndexLocked()
	}
	record := s.lineRecords[len(s.lineRecords)-1]
	if record.sealed || record.id == 0 || record.endRow != s.rowCount-1 {
		return s.openPersistedLogicalLineIDFromIndexLocked()
	}
	return record.id
}

func (s *terminalGridStore) openPersistedLogicalLineIDFromIndexLocked() uint64 {
	if s == nil || s.rowCount <= 0 {
		return 0
	}
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil || len(refs) != s.rowCount {
		return 0
	}
	records := terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, s.baseRowID, s.generation)
	if len(records) == 0 {
		return 0
	}
	record := records[len(records)-1]
	if record.sealed || record.endRow != s.rowCount-1 {
		return 0
	}
	return record.id
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
		if _, lineMetadataErr := writeTerminalGridCompletePersistedLineRecordsMetadata(dir, metadata.BaseRowID, metadata.Generation); lineMetadataErr != nil && err == nil {
			err = lineMetadataErr
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
	lineRecords := s.persistedLogicalLineRecordsForRetention(refs, s.baseRowID, s.generation)
	retainRows, err := terminalGridRetentionRetainedRows(s.dir, refs, lineRecords, s.retentionPolicy, now)
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
	if err := pruneUnreferencedTerminalGridPages(s.dir, refs); err != nil {
		return err
	}
	if s.writable && !s.removeOnClose {
		retainedLineRecords := terminalGridLogicalLineRecordsAfterRowDrop(lineRecords, drop, s.generation)
		if (len(refs) == 0 || len(retainedLineRecords) > 0) && writeTerminalGridPersistedLineRecordsMetadataFromRecords(s.dir, retainedLineRecords, len(refs), s.generation) == nil {
			s.lineRecords = cloneTerminalGridLogicalLineRecords(retainedLineRecords)
			return nil
		}
		records, err := writeTerminalGridCompletePersistedLineRecordsMetadata(s.dir, s.baseRowID, s.generation)
		s.lineRecords = cloneTerminalGridLogicalLineRecords(records)
		return err
	}
	s.lineRecords = cloneTerminalGridLogicalLineRecords(terminalGridLogicalLineRecordsAfterRowDrop(lineRecords, drop, s.generation))
	return nil
}

func terminalGridRetentionRetainedRows(dir string, refs []terminalGridRowRef, lineRecords []terminalGridLogicalLineRecord, policy terminalGridRetentionPolicy, now time.Time) (int, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	lineRecords = terminalGridRetentionLogicalLineRecords(refs, lineRecords)
	retainRows := len(refs)
	if policy.maxLogicalLines > 0 {
		if rows := terminalGridRetentionRowsForLogicalLineLimit(lineRecords, policy.maxLogicalLines); rows > 0 && rows < retainRows {
			retainRows = rows
		}
	}
	if policy.maxRetainedBytes > 0 {
		if rows := terminalGridRetentionRowsForByteLimit(refs, lineRecords, policy.maxRetainedBytes); rows >= 0 && rows < retainRows {
			retainRows = rows
		}
	}
	if policy.maxAge > 0 {
		rows, err := terminalGridRetentionRowsForAgeLimit(dir, refs, lineRecords, policy.maxAge, now)
		if err != nil {
			return 0, err
		}
		if rows >= 0 && rows < retainRows {
			retainRows = rows
		}
	}
	return retainRows, nil
}

func terminalGridRetentionLogicalLineRecords(refs []terminalGridRowRef, lineRecords []terminalGridLogicalLineRecord) []terminalGridLogicalLineRecord {
	if records, ok := terminalGridCompletePersistedLogicalLineRecords(lineRecords, len(refs), 0); ok {
		return records
	}
	return terminalGridFallbackLogicalLineRecordsForRefs(refs, 0)
}

func terminalGridRetentionRowsForLogicalLineLimit(records []terminalGridLogicalLineRecord, logicalLines int) int {
	if logicalLines <= 0 || len(records) == 0 {
		return 0
	}
	if len(records) <= logicalLines {
		return records[len(records)-1].endRow + 1
	}
	lastRow := records[len(records)-1].endRow
	return lastRow - records[len(records)-logicalLines].startRow + 1
}

func terminalGridRetentionRowsForByteLimit(refs []terminalGridRowRef, records []terminalGridLogicalLineRecord, maxBytes int64) int {
	if maxBytes <= 0 || len(refs) == 0 || len(records) == 0 {
		return 0
	}
	var retainedBytes int64
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		var lineBytes int64
		for row := record.startRow; row <= record.endRow && row < len(refs); row++ {
			lineBytes += refs[row].length
		}
		if retainedBytes > 0 && retainedBytes+lineBytes > maxBytes {
			return records[len(records)-1].endRow - records[i+1].startRow + 1
		}
		retainedBytes += lineBytes
		if retainedBytes > maxBytes {
			return record.endRow - record.startRow + 1
		}
	}
	return records[len(records)-1].endRow + 1
}

func terminalGridRetentionRowsForAgeLimit(dir string, refs []terminalGridRowRef, records []terminalGridLogicalLineRecord, maxAge time.Duration, now time.Time) (int, error) {
	if maxAge <= 0 || len(refs) == 0 || len(records) == 0 {
		return 0, nil
	}
	rows, err := readTerminalGridRows(dir, refs)
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-maxAge)
	retainStart := len(refs)
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		newest := time.Time{}
		for row := record.startRow; row <= record.endRow && row < len(rows); row++ {
			if rows[row].timestamp.After(newest) {
				newest = rows[row].timestamp
			}
		}
		if !newest.IsZero() && newest.Before(cutoff) {
			break
		}
		retainStart = record.startRow
	}
	return len(refs) - retainStart, nil
}

func terminalGridFallbackLogicalLineRecordsForRefs(refs []terminalGridRowRef, baseRowID uint64) []terminalGridLogicalLineRecord {
	return terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, baseRowID, 0)
}

func terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs []terminalGridRowRef, baseRowID uint64, generation uint64) []terminalGridLogicalLineRecord {
	if len(refs) == 0 {
		return nil
	}
	records := make([]terminalGridLogicalLineRecord, 0, len(refs))
	start := 0
	for i, ref := range refs {
		if !terminalGridFallbackRowContinuesLogicalLine(ref) || i == len(refs)-1 {
			records = append(records, terminalGridLogicalLineRecord{
				id:         persistedLogicalLineIDFromRowID(baseRowID + uint64(start)),
				startRow:   start,
				endRow:     i,
				sealed:     !terminalGridFallbackRowContinuesLogicalLine(ref),
				origin:     terminalLiveTailOriginReclaimed,
				residency:  terminalLogicalLineResidencyPersisted,
				dirty:      false,
				generation: generation,
				source:     terminalLogicalLineRecordSourceFallback,
			})
			start = i + 1
		}
	}
	return records
}

func terminalGridExplicitLogicalLineRecordsForAppendedRows(refs []terminalGridRowRef, baseRowID uint64, generation uint64, appendedStart int, logicalLineIDs []uint64) []terminalGridLogicalLineRecord {
	return terminalGridExplicitLogicalLineRecordsForAppendedRowsWithMigrations(refs, baseRowID, generation, appendedStart, logicalLineIDs, nil)
}

func terminalGridExplicitLogicalLineRecordsForAppendedRowsWithMigrations(refs []terminalGridRowRef, baseRowID uint64, generation uint64, appendedStart int, logicalLineIDs []uint64, runtimeIDMappings map[uint64]uint64) []terminalGridLogicalLineRecord {
	if len(refs) == 0 || appendedStart < 0 || appendedStart >= len(refs) || len(logicalLineIDs) == 0 {
		return nil
	}
	if appendedStart+len(logicalLineIDs) > len(refs) {
		logicalLineIDs = logicalLineIDs[:len(refs)-appendedStart]
	}
	if !hasNonZeroUint64(logicalLineIDs) {
		return nil
	}
	resolvedIDs := make([]uint64, len(logicalLineIDs))
	explicitIDs := make([]bool, len(logicalLineIDs))
	if runtimeIDMappings == nil {
		runtimeIDMappings = make(map[uint64]uint64)
	}
	for i, rawID := range logicalLineIDs {
		row := appendedStart + i
		if rawID != 0 {
			explicitIDs[i] = true
		}
		if rawID == 0 {
			if i > 0 && terminalGridFallbackRowContinuesLogicalLine(refs[row-1]) {
				rawID = resolvedIDs[i-1]
			} else {
				rawID = persistedLogicalLineIDFromRowID(baseRowID + uint64(row))
			}
		}
		if terminalRuntimeLogicalLineID(rawID) {
			if persistedID := runtimeIDMappings[rawID]; persistedID != 0 {
				rawID = persistedID
			} else {
				persistedID := persistedLogicalLineIDFromRowID(baseRowID + uint64(row))
				runtimeIDMappings[rawID] = persistedID
				rawID = persistedID
			}
		}
		if !terminalPersistedLogicalLineID(rawID) {
			return nil
		}
		if i > 0 && terminalGridFallbackRowContinuesLogicalLine(refs[row-1]) && resolvedIDs[i-1] != rawID {
			return nil
		}
		resolvedIDs[i] = rawID
	}
	records := make([]terminalGridLogicalLineRecord, 0, len(logicalLineIDs))
	completedIDs := make(map[uint64]struct{}, len(logicalLineIDs))
	for start := 0; start < len(resolvedIDs); {
		id := resolvedIDs[start]
		if _, exists := completedIDs[id]; exists {
			return nil
		}
		end := start
		for end+1 < len(resolvedIDs) && resolvedIDs[end+1] == id {
			end++
		}
		hasExplicitID := false
		hasMissingID := false
		for i := start; i <= end; i++ {
			if explicitIDs[i] {
				hasExplicitID = true
			} else {
				hasMissingID = true
			}
		}
		if hasExplicitID && hasMissingID {
			return nil
		}
		row := appendedStart + end
		record := terminalGridLogicalLineRecord{
			id:         id,
			startRow:   appendedStart + start,
			endRow:     row,
			sealed:     !terminalGridFallbackRowContinuesLogicalLine(refs[row]),
			origin:     terminalLiveTailOriginReclaimed,
			residency:  terminalLogicalLineResidencyPersisted,
			dirty:      false,
			generation: generation,
			source:     terminalLogicalLineRecordSourceExplicit,
		}
		records = append(records, record)
		completedIDs[id] = struct{}{}
		start = end + 1
	}
	return records
}

func terminalGridLogicalLineRecordsFromMetadata(records []terminalGridLineRecordMeta) []terminalGridLogicalLineRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	for _, record := range records {
		out = append(out, terminalGridLogicalLineRecord{
			id:         record.ID,
			startRow:   record.StartRow,
			endRow:     record.EndRow,
			sealed:     record.Sealed,
			origin:     record.Origin,
			residency:  record.Residency,
			dirty:      record.Dirty,
			generation: record.Generation,
			source:     record.Source,
		})
	}
	return out
}

func terminalGridCompletePersistedLogicalLineRecords(records []terminalGridLogicalLineRecord, rowCount int, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	if rowCount <= 0 {
		return nil, len(records) == 0
	}
	if len(records) == 0 {
		return nil, false
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	nextStart := 0
	seenIDs := make(map[uint64]struct{}, len(records))
	for _, record := range records {
		if !terminalPersistedLogicalLineID(record.id) || record.residency != terminalLogicalLineResidencyPersisted || record.dirty || record.origin != terminalLiveTailOriginReclaimed || record.startRow != nextStart || record.endRow < record.startRow || record.endRow >= rowCount {
			return nil, false
		}
		if record.source != terminalLogicalLineRecordSourceExplicit && record.source != terminalLogicalLineRecordSourceFallback {
			return nil, false
		}
		if _, exists := seenIDs[record.id]; exists {
			return nil, false
		}
		seenIDs[record.id] = struct{}{}
		if generation != 0 && record.generation != generation {
			return nil, false
		}
		if !record.sealed {
			return nil, false
		}
		out = append(out, record)
		nextStart = record.endRow + 1
	}
	if nextStart != rowCount {
		return nil, false
	}
	return out, true
}

func terminalGridSealedPersistedLogicalLineRecordPrefix(records []terminalGridLogicalLineRecord, rowCount int, generation uint64) []terminalGridLogicalLineRecord {
	if rowCount <= 0 || len(records) == 0 {
		return nil
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	nextStart := 0
	seenIDs := make(map[uint64]struct{}, len(records))
	for _, record := range records {
		if !terminalPersistedLogicalLineID(record.id) || record.residency != terminalLogicalLineResidencyPersisted || record.dirty || record.origin != terminalLiveTailOriginReclaimed || record.startRow != nextStart || record.endRow < record.startRow || record.endRow >= rowCount {
			return nil
		}
		if record.source != terminalLogicalLineRecordSourceExplicit && record.source != terminalLogicalLineRecordSourceFallback {
			return nil
		}
		if _, exists := seenIDs[record.id]; exists {
			return nil
		}
		seenIDs[record.id] = struct{}{}
		if generation != 0 && record.generation != generation {
			return nil
		}
		if !record.sealed {
			break
		}
		out = append(out, record)
		nextStart = record.endRow + 1
	}
	return out
}

func terminalGridLogicalLineRecordsFromSealedPrefixAndRefs(prefix []terminalGridLogicalLineRecord, refs []terminalGridRowRef, baseRowID uint64, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	if len(prefix) == 0 {
		return nil, false
	}
	tailStart := prefix[len(prefix)-1].endRow + 1
	if tailStart < 0 || tailStart > len(refs) {
		return nil, false
	}
	tail := terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs[tailStart:], baseRowID+uint64(tailStart), generation)
	out := make([]terminalGridLogicalLineRecord, 0, len(prefix)+len(tail))
	out = append(out, prefix...)
	for _, record := range tail {
		record.startRow += tailStart
		record.endRow += tailStart
		out = append(out, record)
	}
	return out, true
}

func cloneTerminalGridLogicalLineRecords(records []terminalGridLogicalLineRecord) []terminalGridLogicalLineRecord {
	if len(records) == 0 {
		return nil
	}
	return append([]terminalGridLogicalLineRecord(nil), records...)
}

func terminalGridMergeInMemoryPersistedLineRecords(existing []terminalGridLogicalLineRecord, appended []terminalGridLogicalLineRecord, appendedStart int, rowCount int) []terminalGridLogicalLineRecord {
	if rowCount <= 0 {
		return nil
	}
	if len(appended) == 0 {
		return nil
	}
	records := make([]terminalGridLogicalLineRecord, 0, len(existing)+len(appended))
	for _, record := range existing {
		if record.endRow < appendedStart {
			records = append(records, record)
		}
	}
	for _, record := range appended {
		record.startRow += appendedStart
		record.endRow += appendedStart
		records = append(records, record)
	}
	records = terminalGridMergeOpenPersistedPrefixContinuation(records)
	records = terminalGridCoalesceAdjacentLogicalLineRecords(records)
	if terminalGridInMemoryPersistedLineRecordsValid(records, rowCount) {
		return records
	}
	return nil
}

func terminalGridMergeOpenPersistedPrefixContinuation(records []terminalGridLogicalLineRecord) []terminalGridLogicalLineRecord {
	if len(records) < 2 {
		return records
	}
	out := cloneTerminalGridLogicalLineRecords(records)
	for i := 1; i < len(out); i++ {
		prev := out[i-1]
		if prev.sealed || prev.source != terminalLogicalLineRecordSourceExplicit || out[i].source != terminalLogicalLineRecordSourceExplicit {
			continue
		}
		if prev.endRow+1 != out[i].startRow {
			continue
		}
		continuationID := out[i].id
		out[i].id = prev.id
		for j := i + 1; j < len(out) && out[j].id == continuationID && out[j-1].endRow+1 == out[j].startRow; j++ {
			out[j].id = prev.id
		}
		break
	}
	return out
}

func terminalGridCoalesceAdjacentLogicalLineRecords(records []terminalGridLogicalLineRecord) []terminalGridLogicalLineRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	for _, record := range records {
		if len(out) > 0 {
			prev := &out[len(out)-1]
			if prev.id == record.id && prev.endRow+1 == record.startRow && prev.source == record.source && prev.residency == record.residency && prev.origin == record.origin && prev.dirty == record.dirty && prev.generation == record.generation {
				prev.endRow = record.endRow
				prev.sealed = record.sealed
				continue
			}
		}
		out = append(out, record)
	}
	return out
}

func terminalGridInMemoryPersistedLineRecordsValid(records []terminalGridLogicalLineRecord, rowCount int) bool {
	if rowCount <= 0 {
		return len(records) == 0
	}
	if len(records) == 0 {
		return false
	}
	nextStart := 0
	seenSealedIDs := make(map[uint64]struct{}, len(records))
	for i, record := range records {
		if !terminalPersistedLogicalLineID(record.id) || record.residency != terminalLogicalLineResidencyPersisted || record.dirty || record.origin != terminalLiveTailOriginReclaimed || record.startRow != nextStart || record.endRow < record.startRow || record.endRow >= rowCount || record.source != terminalLogicalLineRecordSourceExplicit {
			return false
		}
		if _, exists := seenSealedIDs[record.id]; exists {
			return false
		}
		if record.sealed {
			seenSealedIDs[record.id] = struct{}{}
		} else if i != len(records)-1 {
			return false
		}
		nextStart = record.endRow + 1
	}
	return nextStart == rowCount
}

func terminalGridLogicalLineRecordsAfterRowDrop(records []terminalGridLogicalLineRecord, droppedRows int, generation uint64) []terminalGridLogicalLineRecord {
	if droppedRows < 0 || len(records) == 0 {
		return nil
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	for _, record := range records {
		if record.endRow < droppedRows {
			continue
		}
		if record.startRow < droppedRows {
			return nil
		}
		record.startRow -= droppedRows
		record.endRow -= droppedRows
		record.generation = generation
		out = append(out, record)
	}
	return out
}

func terminalGridLogicalLineRecordPrefixCount(records []terminalGridLogicalLineRecord, rows int) int {
	if rows <= 0 || len(records) == 0 {
		return 0
	}
	count := 0
	for _, record := range records {
		if record.startRow >= rows {
			break
		}
		count++
		if record.endRow >= rows-1 {
			break
		}
	}
	return count
}

func terminalGridAuthoritativeLogicalLineRecordCount(records []terminalGridLogicalLineRecord) int {
	count := 0
	for _, record := range records {
		if terminalGridLogicalLineRecordAuthoritative(record) {
			count++
		}
	}
	return count
}

func terminalGridAuthoritativeLogicalLineRecordPrefixCount(records []terminalGridLogicalLineRecord, rows int) int {
	if rows <= 0 || len(records) == 0 {
		return 0
	}
	count := 0
	for _, record := range records {
		if record.startRow >= rows {
			break
		}
		if terminalGridLogicalLineRecordAuthoritative(record) {
			count++
		}
		if record.endRow >= rows-1 {
			break
		}
	}
	return count
}

func terminalGridLogicalLineRecordsForWindow(records []terminalGridLogicalLineRecord, start int, end int) []terminalGridLogicalLineRecord {
	if len(records) == 0 || end <= start {
		return nil
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	for _, record := range records {
		if record.endRow < start {
			continue
		}
		if record.startRow >= end {
			break
		}
		windowed := record
		if windowed.startRow < start {
			windowed.startRow = start
		}
		if windowed.endRow >= end {
			windowed.endRow = end - 1
			windowed.sealed = false
		}
		windowed.startRow -= start
		windowed.endRow -= start
		out = append(out, windowed)
	}
	return out
}

func terminalGridWindowStartForRecords(records []terminalGridLogicalLineRecord, start int) int {
	if start <= 0 || len(records) == 0 {
		return maxInt(start, 0)
	}
	for _, record := range records {
		if start >= record.startRow && start <= record.endRow {
			return record.startRow
		}
	}
	return start
}

func (s *terminalGridStore) persistedLogicalLineRecordsFromMetadata(rowCount int, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, false
	}
	memoryRecords, memoryOK := s.persistedLogicalLineRecordsFromMemory(rowCount, generation)
	if memoryOK && terminalGridLogicalLineRecordsAllExplicit(memoryRecords) {
		return memoryRecords, true
	}
	if records, ok := s.persistedLogicalLineRecordsFromSidecar(rowCount, generation); ok {
		return records, true
	}
	return nil, false
}

func (s *terminalGridStore) persistedLogicalLineRecordsFromSidecar(rowCount int, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	metadata, err := readTerminalGridLineMetadata(s.dir)
	if err != nil {
		return nil, false
	}
	if !terminalGridPersistedRecordMetasHaveNoRowIDs(metadata.Records) {
		return nil, false
	}
	records := terminalGridLogicalLineRecordsFromMetadata(metadata.Records)
	terminalGridApplyLineMigrationsToRecords(records, terminalGridLineMigrationMap(metadata.Migrations))
	records, ok := terminalGridCompletePersistedLogicalLineRecords(records, rowCount, generation)
	if !ok {
		return nil, false
	}
	return records, true
}

func terminalGridLogicalLineRecordsAllExplicit(records []terminalGridLogicalLineRecord) bool {
	if len(records) == 0 {
		return false
	}
	for _, record := range records {
		if record.source != terminalLogicalLineRecordSourceExplicit {
			return false
		}
	}
	return true
}

func (s *terminalGridStore) persistedLogicalLineRecordsFromMemory(rowCount int, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	if s == nil || len(s.lineRecords) == 0 {
		return nil, false
	}
	records, ok := terminalGridCompletePersistedLogicalLineRecords(cloneTerminalGridLogicalLineRecords(s.lineRecords), rowCount, generation)
	if !ok {
		return nil, false
	}
	return records, true
}

func (s *terminalGridStore) maxRuntimeLogicalLineIDFromMetadata() uint64 {
	if s == nil {
		return 0
	}
	var maxID uint64
	s.mu.Lock()
	maxID = s.maxRuntimeLineID
	for runtimeID := range s.lineMigrations {
		if terminalRuntimeLogicalLineID(runtimeID) && runtimeID > maxID {
			maxID = runtimeID
		}
	}
	s.mu.Unlock()
	return maxID
}

func terminalGridMaxRuntimeLogicalLineIDFromSidecar(dir string) uint64 {
	if strings.TrimSpace(dir) == "" {
		return 0
	}
	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil {
		return 0
	}
	maxID := terminalGridLineMigrationMapMaxRuntimeID(terminalGridLineMigrationMap(metadata.Migrations))
	for _, record := range metadata.LiveRecords {
		if terminalRuntimeLogicalLineID(record.ID) && record.ID > maxID {
			maxID = record.ID
		}
	}
	return maxID
}

func (s *terminalGridStore) sealedPersistedLogicalLineRecordPrefixFromMetadata(rowCount int, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, false
	}
	metadata, err := readTerminalGridLineMetadata(s.dir)
	if err != nil {
		return nil, false
	}
	if !terminalGridPersistedRecordMetasHaveNoRowIDs(metadata.Records) {
		return nil, false
	}
	records := terminalGridLogicalLineRecordsFromMetadata(metadata.Records)
	terminalGridApplyLineMigrationsToRecords(records, terminalGridLineMigrationMap(metadata.Migrations))
	prefix := terminalGridSealedPersistedLogicalLineRecordPrefix(records, rowCount, generation)
	return prefix, len(prefix) > 0
}

func terminalGridPersistedRecordMetasHaveNoRowIDs(records []terminalGridLineRecordMeta) bool {
	for _, record := range records {
		if terminalGridLineRecordMetaHasRowIDs(record) {
			return false
		}
	}
	return true
}

func terminalGridLineRecordMetaHasRowIDs(record terminalGridLineRecordMeta) bool {
	return record.RowIDKnown || record.FirstRowID != 0 || record.LastRowID != 0
}

func terminalGridLineMigrationMap(migrations []terminalGridLineMigration) map[uint64]uint64 {
	if len(migrations) == 0 {
		return nil
	}
	out := make(map[uint64]uint64, len(migrations))
	for _, migration := range migrations {
		if !terminalRuntimeLogicalLineID(migration.RuntimeID) || !terminalPersistedLogicalLineID(migration.PersistedID) {
			continue
		}
		out[migration.RuntimeID] = migration.PersistedID
	}
	return out
}

func terminalGridLineMigrationMapMaxRuntimeID(migrations map[uint64]uint64) uint64 {
	var maxID uint64
	for runtimeID := range migrations {
		if runtimeID > maxID {
			maxID = runtimeID
		}
	}
	return maxID
}

func terminalGridLineRecordMetasMaxRuntimeID(records []terminalGridLineRecordMeta) uint64 {
	var maxID uint64
	for _, record := range records {
		if terminalRuntimeLogicalLineID(record.ID) && record.ID > maxID {
			maxID = record.ID
		}
	}
	return maxID
}

func terminalGridApplyLineMigrationsToRecords(records []terminalGridLogicalLineRecord, migrations map[uint64]uint64) {
	if len(records) == 0 || len(migrations) == 0 {
		return
	}
	for i := range records {
		if persistedID := migrations[records[i].id]; persistedID != 0 {
			records[i].id = persistedID
			records[i].origin = terminalLiveTailOriginReclaimed
		}
	}
}

func (s *terminalGridStore) persistedLogicalLineRecordsForRetention(refs []terminalGridRowRef, baseRowID uint64, generation uint64) []terminalGridLogicalLineRecord {
	if s == nil {
		return terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, baseRowID, generation)
	}
	if records, ok := s.persistedLogicalLineRecordsFromMetadata(len(refs), generation); ok {
		return records
	}
	if prefix, ok := s.sealedPersistedLogicalLineRecordPrefixFromMetadata(len(refs), generation); ok {
		if records, ok := terminalGridLogicalLineRecordsFromSealedPrefixAndRefs(prefix, refs, baseRowID, generation); ok {
			return records
		}
	}
	return terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, baseRowID, generation)
}

func (s *terminalGridStore) persistedLogicalLineRecordsForWindowStart(refs []terminalGridRowRef, baseRowID uint64, generation uint64, start int) []terminalGridLogicalLineRecord {
	if s == nil {
		return terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, baseRowID, generation)
	}
	if records, ok := s.persistedLogicalLineRecordsFromMetadata(len(refs), generation); ok {
		return records
	}
	if records, ok := s.sealedPersistedLogicalLineRecordPrefixFromMetadata(len(refs), generation); ok && terminalGridRecordsCoverWindow(records, start, start+1) {
		return records
	}
	return terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, baseRowID, generation)
}

func (s *terminalGridStore) persistedLogicalLineRecordsForViewport(refs []terminalGridRowRef, firstRowID uint64, totalRows int, generation uint64) []terminalGridLogicalLineRecord {
	if len(refs) == 0 {
		return nil
	}
	if s != nil {
		baseRowID, _, _ := s.coordinates()
		if firstRowID >= baseRowID {
			start := int(firstRowID - baseRowID)
			end := start + len(refs)
			if records, ok := s.persistedLogicalLineRecordsFromMetadata(totalRows, generation); ok {
				if windowRecords := terminalGridLogicalLineRecordsForWindow(records, start, end); len(windowRecords) > 0 {
					return windowRecords
				}
			}
			if records, ok := s.sealedPersistedLogicalLineRecordPrefixFromMetadata(totalRows, generation); ok && terminalGridRecordsCoverWindow(records, start, end) {
				if windowRecords := terminalGridLogicalLineRecordsForWindow(records, start, end); len(windowRecords) > 0 {
					return windowRecords
				}
			}
		}
	}
	return terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, firstRowID, generation)
}

func terminalGridRecordsCoverWindow(records []terminalGridLogicalLineRecord, start int, end int) bool {
	if len(records) == 0 || end <= start {
		return false
	}
	coveredUntil := start
	for _, record := range records {
		if record.endRow < coveredUntil {
			continue
		}
		if record.startRow > coveredUntil {
			return false
		}
		if record.endRow+1 > coveredUntil {
			coveredUntil = record.endRow + 1
		}
		if coveredUntil >= end {
			return true
		}
	}
	return false
}

func (s *terminalGridStore) recoveredLiveTailFromMetadata() (terminalPrimaryLiveTail, bool) {
	var tail terminalPrimaryLiveTail
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return tail, false
	}
	metadata, err := readTerminalGridLineMetadata(s.dir)
	if err != nil || len(metadata.LiveRecords) == 0 || len(metadata.LiveRows) == 0 {
		return tail, false
	}
	rows, err := terminalGridDamageRowsFromLineRowMetas(metadata.LiveRows)
	if err != nil || len(rows) != len(metadata.LiveRows) {
		return terminalPrimaryLiveTail{}, false
	}
	baseRowID, generation, rowCount := s.coordinates()
	var persistedRecords []terminalGridLogicalLineRecord
	if terminalGridLineRecordMetasContainOrigin(metadata.LiveRecords, terminalLiveTailOriginReclaimed) {
		var ok bool
		persistedRecords, ok = s.persistedLogicalLineRecordsForRecoveredLiveTail(baseRowID, generation)
		if !ok {
			return terminalPrimaryLiveTail{}, false
		}
	}
	migrations := terminalGridLineMigrationMap(metadata.Migrations)
	segments, ok := terminalLiveTailSegmentsFromMetadata(metadata.LiveRecords, rows, baseRowID, generation, rowCount, persistedRecords, migrations)
	if !ok {
		return terminalPrimaryLiveTail{}, false
	}
	return terminalPrimaryLiveTail{
		segments:                 segments,
		wrapPending:              terminalLiveTailSegmentsWrapPending(segments),
		nextRuntimeLogicalLineID: maxUint64(terminalLiveTailSegmentsMaxRuntimeLogicalLineID(segments), terminalGridLineMigrationMapMaxRuntimeID(migrations)),
	}, true
}

func (s *terminalGridStore) persistedLogicalLineRecordsForRecoveredLiveTail(baseRowID uint64, generation uint64) ([]terminalGridLogicalLineRecord, bool) {
	if s == nil {
		return nil, false
	}
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(s.dir, terminalGridIndexName))
	if err != nil {
		return nil, false
	}
	return s.persistedLogicalLineRecordsForRetention(refs, baseRowID, generation), true
}

func terminalGridLineRecordMetasContainOrigin(records []terminalGridLineRecordMeta, origin terminalLiveTailOrigin) bool {
	for _, record := range records {
		if record.Origin == origin {
			return true
		}
	}
	return false
}

func terminalLiveTailSegmentsFromMetadata(records []terminalGridLineRecordMeta, rows []vterm.DamageOp, persistedBaseRowID uint64, persistedGeneration uint64, persistedRowCount int, persistedRecords []terminalGridLogicalLineRecord, migrations map[uint64]uint64) ([]terminalLiveTailSegment, bool) {
	if len(records) == 0 || len(rows) == 0 {
		return nil, false
	}
	if _, ok := terminalGridCompleteLiveTailLogicalLineRecords(terminalGridLogicalLineRecordsFromMetadata(records), len(rows)); !ok {
		return nil, false
	}
	segments := make([]terminalLiveTailSegment, 0, len(records))
	for i, record := range records {
		if record.StartRow < 0 || record.EndRow >= len(rows) || record.EndRow < record.StartRow {
			return nil, false
		}
		if record.Residency != terminalLogicalLineResidencyLiveTail || record.ID == 0 {
			return nil, false
		}
		if !terminalLiveTailOriginKnown(record.Origin) {
			return nil, false
		}
		if record.Origin != terminalLiveTailOriginReclaimed && terminalGridLineRecordMetaHasRowIDs(record) {
			return nil, false
		}
		segmentRows := cloneGridDamageOps(rows[record.StartRow : record.EndRow+1])
		if !terminalLiveTailRecordSealStateMatchesRows(record, segmentRows) {
			return nil, false
		}
		if record.Origin != terminalLiveTailOriginReclaimed && migrations[record.ID] != 0 {
			return nil, false
		}
		lineIDs := make([]uint64, len(segmentRows))
		for row := range lineIDs {
			lineIDs[row] = record.ID
		}
		segment := terminalLiveTailSegment{
			origin:         record.Origin,
			sealState:      terminalLiveTailOpen,
			rows:           segmentRows,
			logicalLineIDs: lineIDs,
			wrapPending:    terminalLiveTailRecordWrapPendingFromMetadata(record, segmentRows),
			generation:     record.Generation,
		}
		if record.Sealed {
			segment.sealState = terminalLiveTailSealed
		}
		if record.Origin == terminalLiveTailOriginReclaimed {
			firstRowID, lastRowID, ok := terminalLiveTailReclaimedRowIDsFromMetadata(record, len(segmentRows))
			if !ok {
				return nil, false
			}
			if !terminalLiveTailReclaimedMetadataMatchesPersistedStore(record, firstRowID, lastRowID, persistedBaseRowID, persistedGeneration, persistedRowCount) {
				return nil, false
			}
			if !terminalLiveTailReclaimedMetadataMatchesPersistedRecord(record, firstRowID, lastRowID, persistedBaseRowID, persistedRecords) {
				return nil, false
			}
			segment.firstRowID = firstRowID
			segment.lastRowID = lastRowID
		}
		if i > 0 && canMergeRecoveredLiveTailSegments(segments[len(segments)-1], segment) {
			merged := &segments[len(segments)-1]
			merged.rows = append(merged.rows, segment.rows...)
			merged.logicalLineIDs = append(merged.logicalLineIDs, segment.logicalLineIDs...)
			merged.sealState = segment.sealState
			merged.wrapPending = segment.wrapPending
			if segment.lastRowID > merged.lastRowID {
				merged.lastRowID = segment.lastRowID
			}
			continue
		}
		segments = append(segments, segment)
	}
	return segments, true
}

func terminalLiveTailRecordSealStateMatchesRows(record terminalGridLineRecordMeta, rows []vterm.DamageOp) bool {
	if len(rows) == 0 {
		return false
	}
	last := rows[len(rows)-1]
	if record.Sealed && last.WrappedSet && last.Wrapped {
		return false
	}
	return true
}

func terminalLiveTailRecordWrapPendingFromMetadata(record terminalGridLineRecordMeta, rows []vterm.DamageOp) bool {
	if record.Sealed || len(rows) == 0 {
		return false
	}
	last := rows[len(rows)-1]
	return last.WrappedSet && last.Wrapped
}

func terminalLiveTailOriginKnown(origin terminalLiveTailOrigin) bool {
	switch origin {
	case terminalLiveTailOriginLive, terminalLiveTailOriginReclaimed, terminalLiveTailOriginResize:
		return true
	default:
		return false
	}
}

func terminalGridCompleteLiveTailLogicalLineRecords(records []terminalGridLogicalLineRecord, rowCount int) ([]terminalGridLogicalLineRecord, bool) {
	if rowCount <= 0 || len(records) == 0 {
		return nil, false
	}
	out := make([]terminalGridLogicalLineRecord, 0, len(records))
	nextStart := 0
	seenIDs := make(map[uint64]struct{}, len(records))
	for i, record := range records {
		if record.id == 0 || record.residency != terminalLogicalLineResidencyLiveTail || !terminalLiveTailOriginKnown(record.origin) || record.dirty != terminalLiveTailOriginDirty(record.origin) || record.startRow != nextStart || record.endRow < record.startRow || record.endRow >= rowCount {
			return nil, false
		}
		if _, exists := seenIDs[record.id]; exists {
			return nil, false
		}
		seenIDs[record.id] = struct{}{}
		if !terminalLiveTailRecordIDMatchesOrigin(record.id, record.origin) {
			return nil, false
		}
		if record.origin != terminalLiveTailOriginReclaimed && record.generation != 0 {
			return nil, false
		}
		if record.origin == terminalLiveTailOriginReclaimed && !record.sealed {
			return nil, false
		}
		if record.origin != terminalLiveTailOriginReclaimed && !record.sealed && i != len(records)-1 {
			return nil, false
		}
		if record.origin == terminalLiveTailOriginReclaimed && record.generation == 0 {
			return nil, false
		}
		out = append(out, record)
		nextStart = record.endRow + 1
	}
	if nextStart != rowCount {
		return nil, false
	}
	return out, true
}

func terminalLiveTailOriginDirty(origin terminalLiveTailOrigin) bool {
	return origin != terminalLiveTailOriginReclaimed
}

func terminalLiveTailRecordIDMatchesOrigin(id uint64, origin terminalLiveTailOrigin) bool {
	if origin == terminalLiveTailOriginReclaimed {
		return terminalPersistedLogicalLineID(id)
	}
	return terminalRuntimeLogicalLineID(id)
}

func terminalPersistedLogicalLineID(id uint64) bool {
	return id > 0 && id < terminalLiveTailLogicalLineIDBase
}

func terminalRuntimeLogicalLineID(id uint64) bool {
	return id >= terminalLiveTailLogicalLineIDBase
}

func canMergeRecoveredLiveTailSegments(left terminalLiveTailSegment, right terminalLiveTailSegment) bool {
	if left.origin != right.origin || left.generation != right.generation {
		return false
	}
	if left.origin == terminalLiveTailOriginReclaimed {
		return left.lastRowID+1 == right.firstRowID
	}
	return true
}

func terminalLiveTailSegmentsWrapPending(segments []terminalLiveTailSegment) bool {
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i].origin == terminalLiveTailOriginReclaimed {
			continue
		}
		return segments[i].wrapPending
	}
	return false
}

func terminalLiveTailSegmentsMaxRuntimeLogicalLineID(segments []terminalLiveTailSegment) uint64 {
	var maxID uint64
	for _, segment := range segments {
		for _, id := range segment.logicalLineIDs {
			if terminalRuntimeLogicalLineID(id) && id > maxID {
				maxID = id
			}
		}
	}
	return maxID
}

func terminalLiveTailReclaimedRowIDsFromMetadata(record terminalGridLineRecordMeta, rowCount int) (uint64, uint64, bool) {
	if !record.RowIDKnown || rowCount <= 0 || record.LastRowID < record.FirstRowID {
		return 0, 0, false
	}
	if int(record.LastRowID-record.FirstRowID)+1 != rowCount {
		return 0, 0, false
	}
	return record.FirstRowID, record.LastRowID, true
}

func terminalLiveTailReclaimedMetadataMatchesPersistedStore(record terminalGridLineRecordMeta, firstRowID uint64, lastRowID uint64, persistedBaseRowID uint64, persistedGeneration uint64, persistedRowCount int) bool {
	if persistedRowCount <= 0 {
		return false
	}
	if record.Generation == 0 || persistedGeneration == 0 || record.Generation != persistedGeneration {
		return false
	}
	if firstRowID < persistedBaseRowID {
		return false
	}
	lastPersistedRowID := persistedBaseRowID + uint64(persistedRowCount) - 1
	return lastRowID <= lastPersistedRowID
}

func terminalLiveTailReclaimedMetadataMatchesPersistedRecord(record terminalGridLineRecordMeta, firstRowID uint64, lastRowID uint64, persistedBaseRowID uint64, persistedRecords []terminalGridLogicalLineRecord) bool {
	if len(persistedRecords) == 0 || firstRowID < persistedBaseRowID || lastRowID < firstRowID {
		return false
	}
	start := int(firstRowID - persistedBaseRowID)
	end := int(lastRowID - persistedBaseRowID)
	for _, persisted := range persistedRecords {
		if persisted.endRow < start {
			continue
		}
		if persisted.startRow > start {
			return false
		}
		return persisted.id == record.ID && persisted.startRow <= start && persisted.endRow >= end && persisted.sealed
	}
	return false
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

func reflowTerminalGridRows(rows []terminalGridRow, cols int, lineRecords []terminalGridLogicalLineRecord) ([][]vterm.Cell, []time.Time, []string, []bool, []uint64) {
	projected, timestamps, rowKinds, wrapped, lineIDs, _ := reflowTerminalGridRowsWithAuthority(rows, cols, lineRecords)
	return projected, timestamps, rowKinds, wrapped, lineIDs
}

func reflowTerminalGridRowsWithAuthority(rows []terminalGridRow, cols int, lineRecords []terminalGridLogicalLineRecord) ([][]vterm.Cell, []time.Time, []string, []bool, []uint64, []bool) {
	projected, timestamps, rowKinds, wrapped, lineIDs, authoritative, _ := reflowTerminalGridRowsWithRowIDRanges(rows, cols, lineRecords, nil)
	return projected, timestamps, rowKinds, wrapped, lineIDs, authoritative
}

func reflowTerminalGridRowsWithRowIDRanges(rows []terminalGridRow, cols int, lineRecords []terminalGridLogicalLineRecord, rowIDs []uint64) ([][]vterm.Cell, []time.Time, []string, []bool, []uint64, []bool, []terminalGridRowIDRange) {
	if len(rows) == 0 {
		return nil, nil, nil, nil, nil, nil, nil
	}
	if cols <= 0 {
		cols = 80
	}
	trackRowIDRanges := len(rowIDs) >= len(rows)
	reflow := terminalGridReflowState{
		cols:                cols,
		rows:                make([][]vterm.Cell, 0, minInt(len(rows), 1024)),
		timestamps:          make([]time.Time, 0, minInt(len(rows), 1024)),
		rowKinds:            make([]string, 0, minInt(len(rows), 1024)),
		wrapped:             make([]bool, 0, minInt(len(rows), 1024)),
		lineIDs:             make([]uint64, 0, minInt(len(rows), 1024)),
		lineIDAuthoritative: make([]bool, 0, minInt(len(rows), 1024)),
		trackRowIDRanges:    trackRowIDRanges,
	}
	if trackRowIDRanges {
		reflow.rowIDRanges = make([]terminalGridRowIDRange, 0, minInt(len(rows), 1024))
	}
	var logical []terminalGridReflowCell
	var emptyMeta terminalGridReflowMeta
	recordIndex := 0
	for i, row := range rows {
		for recordIndex < len(lineRecords) && i > lineRecords[recordIndex].endRow {
			recordIndex++
		}
		logicalLineID := uint64(0)
		recordEnd := i
		recordSealed := false
		hasRecord := false
		lineIDAuthoritative := false
		if recordIndex < len(lineRecords) && i >= lineRecords[recordIndex].startRow && i <= lineRecords[recordIndex].endRow {
			hasRecord = true
			logicalLineID = lineRecords[recordIndex].id
			recordEnd = lineRecords[recordIndex].endRow
			recordSealed = lineRecords[recordIndex].sealed
			lineIDAuthoritative = terminalGridLogicalLineRecordAuthoritative(lineRecords[recordIndex])
		}
		continuesLogicalLine := row.wrapped
		if hasRecord {
			continuesLogicalLine = i < recordEnd || !recordSealed
		}
		meta := terminalGridReflowMeta{
			timestamp:                  row.timestamp,
			rowKind:                    row.rowKind,
			logicalLineID:              logicalLineID,
			logicalLineIDAuthoritative: lineIDAuthoritative,
		}
		if trackRowIDRanges {
			meta.rowID = rowIDs[i]
			meta.rowIDKnown = true
		}
		if len(logical) == 0 {
			emptyMeta = meta
		}
		for i, cell := range row.cells {
			cellMeta := meta
			cellMeta.logicalLineID = emptyMeta.logicalLineID
			cellMeta.logicalLineIDAuthoritative = emptyMeta.logicalLineIDAuthoritative
			logical = append(logical, terminalGridReflowCell{
				cell:      cell,
				meta:      cellMeta,
				continued: continuesLogicalLine && i == len(row.cells)-1,
			})
		}
		if continuesLogicalLine {
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
		if trackRowIDRanges && len(reflow.rowIDRanges) == 0 {
			reflow.rowIDRanges = []terminalGridRowIDRange{{First: rowIDs[0], Last: rowIDs[0], Known: true}}
		}
	}
	return reflow.rows,
		alignGridTimes(reflow.timestamps, len(reflow.rows)),
		alignGridStrings(reflow.rowKinds, len(reflow.rows)),
		alignGridBools(reflow.wrapped, len(reflow.rows)),
		alignGridUint64s(reflow.lineIDs, len(reflow.rows)),
		alignGridBools(reflow.lineIDAuthoritative, len(reflow.rows)),
		alignGridRowIDRanges(reflow.rowIDRanges, len(reflow.rows))
}

type terminalGridReflowMeta struct {
	timestamp                  time.Time
	rowKind                    string
	logicalLineID              uint64
	logicalLineIDAuthoritative bool
	rowID                      uint64
	rowIDKnown                 bool
}

type terminalGridReflowCell struct {
	cell      vterm.Cell
	meta      terminalGridReflowMeta
	continued bool
}

type terminalGridReflowState struct {
	cols                int
	rows                [][]vterm.Cell
	timestamps          []time.Time
	rowKinds            []string
	wrapped             []bool
	lineIDs             []uint64
	lineIDAuthoritative []bool
	rowIDRanges         []terminalGridRowIDRange
	trackRowIDRanges    bool
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
	r.lineIDs = append(r.lineIDs, meta.logicalLineID)
	r.lineIDAuthoritative = append(r.lineIDAuthoritative, meta.logicalLineIDAuthoritative)
	if r.trackRowIDRanges {
		r.rowIDRanges = append(r.rowIDRanges, terminalGridRowIDRangeForSegment(segment, meta))
	}
}

func terminalGridRowIDRangeForSegment(segment []terminalGridReflowCell, meta terminalGridReflowMeta) terminalGridRowIDRange {
	if len(segment) == 0 {
		if !meta.rowIDKnown {
			return terminalGridRowIDRange{}
		}
		return terminalGridRowIDRange{First: meta.rowID, Last: meta.rowID, Known: true}
	}
	var out terminalGridRowIDRange
	for _, cell := range segment {
		if !cell.meta.rowIDKnown {
			continue
		}
		if !out.Known {
			out = terminalGridRowIDRange{First: cell.meta.rowID, Last: cell.meta.rowID, Known: true}
			continue
		}
		if cell.meta.rowID < out.First {
			out.First = cell.meta.rowID
		}
		if cell.meta.rowID > out.Last {
			out.Last = cell.meta.rowID
		}
	}
	return out
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

func alignGridUint64s(values []uint64, size int) []uint64 {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) == size {
		return cloneUint64Slice(values)
	}
	out := make([]uint64, size)
	if len(values) > size {
		values = values[len(values)-size:]
	}
	copy(out[size-len(values):], values)
	return out
}

func alignGridRowIDRanges(values []terminalGridRowIDRange, size int) []terminalGridRowIDRange {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) == size {
		return cloneTerminalGridRowIDRanges(values)
	}
	out := make([]terminalGridRowIDRange, size)
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

func trimUint64SliceTail(values []uint64, limit int) []uint64 {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return cloneUint64Slice(values)
	}
	return cloneUint64Slice(values[len(values)-limit:])
}

func trimTerminalGridRowIDRangesTail(values []terminalGridRowIDRange, limit int) []terminalGridRowIDRange {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return cloneTerminalGridRowIDRanges(values)
	}
	return cloneTerminalGridRowIDRanges(values[len(values)-limit:])
}

func trimUint64Prefix(values []uint64, start int) []uint64 {
	if start <= 0 {
		return cloneUint64Slice(values)
	}
	if start >= len(values) {
		return nil
	}
	return cloneUint64Slice(values[start:])
}

func cloneUint64Slice(values []uint64) []uint64 {
	if len(values) == 0 {
		return nil
	}
	return append([]uint64(nil), values...)
}

func cloneTerminalGridRowIDRanges(values []terminalGridRowIDRange) []terminalGridRowIDRange {
	if len(values) == 0 {
		return nil
	}
	return append([]terminalGridRowIDRange(nil), values...)
}

func terminalGridRowIDs(firstRowID uint64, count int) []uint64 {
	if count <= 0 {
		return nil
	}
	out := make([]uint64, count)
	for i := range out {
		out[i] = firstRowID + uint64(i)
	}
	return out
}

func uint64At(values []uint64, index int) uint64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func persistedLogicalLineIDFromRowID(rowID uint64) uint64 {
	return rowID + 1
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

func readTerminalGridLineMetadata(dir string) (terminalGridLineMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, terminalGridLineMetaName))
	if err != nil {
		return terminalGridLineMetadata{}, err
	}
	var metadata terminalGridLineMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return terminalGridLineMetadata{}, err
	}
	return metadata, nil
}

func writeTerminalGridLineMetadata(dir string, metadata terminalGridLineMetadata) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sort.Slice(metadata.Migrations, func(i, j int) bool {
		return metadata.Migrations[i].RuntimeID < metadata.Migrations[j].RuntimeID
	})
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, terminalGridLineMetaName), data, 0o600)
}

func writeTerminalGridCompletePersistedLineRecordsMetadata(dir string, baseRowID uint64, generation uint64) ([]terminalGridLogicalLineRecord, error) {
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(dir, terminalGridIndexName))
	if err != nil {
		return nil, err
	}
	if metadata, err := readTerminalGridLineMetadata(dir); err == nil && terminalGridPersistedRecordMetasHaveNoRowIDs(metadata.Records) {
		records := terminalGridLogicalLineRecordsFromMetadata(metadata.Records)
		terminalGridApplyLineMigrationsToRecords(records, terminalGridLineMigrationMap(metadata.Migrations))
		if complete, ok := terminalGridCompletePersistedLogicalLineRecords(records, len(refs), generation); ok {
			return complete, writeTerminalGridPersistedLineRecordsMetadataFromRecords(dir, complete, len(refs), generation)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	records := terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs, baseRowID, generation)
	return records, writeTerminalGridPersistedLineRecordsMetadataFromRecords(dir, records, len(refs), generation)
}

func writeTerminalGridPersistedLineRecordsMetadataForAppend(dir string, baseRowID uint64, generation uint64, appendedStart int, logicalLineIDs []uint64) ([]terminalGridLogicalLineRecord, error) {
	refs, err := readTerminalGridIndexRefsFromPath(filepath.Join(dir, terminalGridIndexName))
	if err != nil {
		return nil, err
	}
	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	records := terminalGridLogicalLineRecordsFromMetadata(metadata.Records)
	terminalGridApplyLineMigrationsToRecords(records, terminalGridLineMigrationMap(metadata.Migrations))
	if prefix, ok := terminalGridCompletePersistedLogicalLineRecords(records, appendedStart, generation); ok {
		records = prefix
	} else if prefix := terminalGridSealedPersistedLogicalLineRecordPrefix(records, appendedStart, generation); len(prefix) > 0 && prefix[len(prefix)-1].endRow+1 == appendedStart {
		records = prefix
	} else if appendedStart == 0 {
		records = nil
	} else {
		records = terminalGridFallbackLogicalLineRecordsForRefsWithGeneration(refs[:appendedStart], baseRowID, generation)
		if complete, ok := terminalGridCompletePersistedLogicalLineRecords(records, appendedStart, generation); ok {
			records = complete
		} else {
			return nil, fmt.Errorf("terminal grid persisted line metadata prefix does not cover append start")
		}
	}
	appendedRecords := terminalGridExplicitLogicalLineRecordsForAppendedRows(refs, baseRowID, generation, appendedStart, logicalLineIDs)
	if len(appendedRecords) == 0 {
		return nil, fmt.Errorf("terminal grid appended logical line metadata is incomplete")
	}
	records = append(records, appendedRecords...)
	return records, writeTerminalGridPersistedLineRecordsMetadataFromRecords(dir, records, len(refs), generation)
}

func recordTerminalGridLineMigrationsForAppendedLogicalLineIDs(dir string, generation uint64, rowCount int, appendedStart int, logicalLineIDs []uint64) error {
	if len(logicalLineIDs) == 0 {
		return nil
	}
	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	records, ok := terminalGridCompletePersistedLogicalLineRecords(terminalGridLogicalLineRecordsFromMetadata(metadata.Records), rowCount, generation)
	if !ok {
		return nil
	}
	migrations := make(map[uint64]uint64)
	seen := make(map[uint64]struct{})
	for start := 0; start < len(logicalLineIDs); {
		runtimeID := uint64At(logicalLineIDs, start)
		end := start
		for end+1 < len(logicalLineIDs) && uint64At(logicalLineIDs, end+1) == runtimeID {
			end++
		}
		if terminalRuntimeLogicalLineID(runtimeID) {
			if _, exists := seen[runtimeID]; exists {
				return nil
			}
			seen[runtimeID] = struct{}{}
			if record, ok := terminalGridLogicalLineRecordCoveringWindow(records, appendedStart+start, appendedStart+end+1); ok {
				migrations[runtimeID] = record.id
			}
		}
		start = end + 1
	}
	return recordTerminalGridLineMigrations(dir, migrations)
}

func terminalGridLogicalLineRecordCoveringWindow(records []terminalGridLogicalLineRecord, start int, end int) (terminalGridLogicalLineRecord, bool) {
	if end <= start {
		return terminalGridLogicalLineRecord{}, false
	}
	for _, record := range records {
		if record.startRow <= start && record.endRow >= end-1 {
			return record, terminalPersistedLogicalLineID(record.id)
		}
	}
	return terminalGridLogicalLineRecord{}, false
}

func writeTerminalGridPersistedLineRecordsMetadataFromRecords(dir string, records []terminalGridLogicalLineRecord, rowCount int, generation uint64) error {
	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if complete, ok := terminalGridCompletePersistedLogicalLineRecords(records, rowCount, generation); ok {
		metadata.Records = terminalGridLineRecordMetasFromRecords(complete)
	} else if sealedPrefix := terminalGridSealedPersistedLogicalLineRecordPrefix(records, rowCount, generation); len(sealedPrefix) > 0 {
		metadata.Records = terminalGridLineRecordMetasFromRecords(sealedPrefix)
	} else {
		metadata.Records = nil
	}
	metadata.Migrations = terminalGridLineMigrationsForPersistedRecords(metadata.Migrations, metadata.Records)
	return writeTerminalGridLineMetadata(dir, metadata)
}

func terminalGridLineMigrationsForPersistedRecords(migrations []terminalGridLineMigration, records []terminalGridLineRecordMeta) []terminalGridLineMigration {
	if len(migrations) == 0 || len(records) == 0 {
		return nil
	}
	persistedIDs := make(map[uint64]struct{}, len(records))
	for _, record := range records {
		if terminalPersistedLogicalLineID(record.ID) {
			persistedIDs[record.ID] = struct{}{}
		}
	}
	if len(persistedIDs) == 0 {
		return nil
	}
	out := make([]terminalGridLineMigration, 0, len(migrations))
	for _, migration := range migrations {
		if !terminalRuntimeLogicalLineID(migration.RuntimeID) || !terminalPersistedLogicalLineID(migration.PersistedID) {
			continue
		}
		if _, ok := persistedIDs[migration.PersistedID]; !ok {
			continue
		}
		out = append(out, migration)
	}
	return out
}

func terminalGridLineRecordMetasFromRecords(records []terminalGridLogicalLineRecord) []terminalGridLineRecordMeta {
	if len(records) == 0 {
		return nil
	}
	out := make([]terminalGridLineRecordMeta, 0, len(records))
	for _, record := range records {
		out = append(out, terminalGridLineRecordMeta{
			ID:         record.id,
			StartRow:   record.startRow,
			EndRow:     record.endRow,
			Sealed:     record.sealed,
			Origin:     record.origin,
			Residency:  record.residency,
			Dirty:      record.dirty,
			Generation: record.generation,
			Source:     record.source,
		})
	}
	return out
}

func terminalGridLineRecordMetasFromLiveTailRecords(records []terminalLiveTailLogicalLineRecord) []terminalGridLineRecordMeta {
	if len(records) == 0 {
		return nil
	}
	out := make([]terminalGridLineRecordMeta, 0, len(records))
	for _, record := range records {
		meta := terminalGridLineRecordMeta{
			ID:         record.id,
			StartRow:   record.startRow,
			EndRow:     record.endRow,
			Sealed:     record.sealState == terminalLiveTailSealed,
			Origin:     record.origin,
			Residency:  record.residency,
			Dirty:      record.dirty,
			Generation: record.generation,
		}
		if record.origin == terminalLiveTailOriginReclaimed && record.rowIDKnown {
			meta.RowIDKnown = true
			meta.FirstRowID = record.firstRowID
			meta.LastRowID = record.lastRowID
		}
		out = append(out, meta)
	}
	return out
}

func terminalLiveTailRecordsValidForLineState(records []terminalLiveTailLogicalLineRecord, rows []vterm.DamageOp) bool {
	if len(records) == 0 || len(rows) == 0 {
		return len(records) == 0 && len(rows) == 0
	}
	nextStart := 0
	seenIDs := make(map[uint64]struct{}, len(records))
	for i, record := range records {
		if record.id == 0 || record.residency != terminalLogicalLineResidencyLiveTail || !terminalLiveTailOriginKnown(record.origin) || record.dirty != terminalLiveTailOriginDirty(record.origin) || record.startRow != nextStart || record.endRow < record.startRow || record.endRow >= len(rows) {
			return false
		}
		if _, exists := seenIDs[record.id]; exists {
			return false
		}
		seenIDs[record.id] = struct{}{}
		if !terminalLiveTailRecordIDMatchesOrigin(record.id, record.origin) {
			return false
		}
		sealed := record.sealState == terminalLiveTailSealed
		if record.origin != terminalLiveTailOriginReclaimed {
			if record.generation != 0 || record.rowIDKnown || record.firstRowID != 0 || record.lastRowID != 0 {
				return false
			}
			if !sealed && i != len(records)-1 {
				return false
			}
		} else {
			if !sealed || !record.rowIDKnown || record.lastRowID < record.firstRowID || record.generation == 0 {
				return false
			}
			if int(record.lastRowID-record.firstRowID)+1 != record.endRow-record.startRow+1 {
				return false
			}
		}
		meta := terminalGridLineRecordMeta{
			Sealed: sealed,
			Origin: record.origin,
		}
		if !terminalLiveTailRecordSealStateMatchesRows(meta, rows[record.startRow:record.endRow+1]) {
			return false
		}
		nextStart = record.endRow + 1
	}
	return nextStart == len(rows)
}

func terminalGridLineRowMetasFromDamageRows(rows []vterm.DamageOp) ([]terminalGridLineRowMeta, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]terminalGridLineRowMeta, 0, len(rows))
	for _, row := range rows {
		payload, err := encodeTerminalGridRow(terminalGridRow{
			cells:     damageOpCells(row),
			runs:      row.Runs,
			timestamp: row.Timestamp,
			rowKind:   row.RowKind,
			wrapped:   row.WrappedSet && row.Wrapped,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, terminalGridLineRowMeta{
			Payload: payload,
			Wrapped: row.WrappedSet && row.Wrapped,
		})
	}
	return out, nil
}

func terminalGridDamageRowsFromLineRowMetas(rows []terminalGridLineRowMeta) ([]vterm.DamageOp, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]vterm.DamageOp, 0, len(rows))
	for _, row := range rows {
		if len(row.Payload) == 0 {
			return nil, fmt.Errorf("terminal grid live tail row payload is empty")
		}
		decoded, err := decodeTerminalGridRow(row.Payload)
		if err != nil {
			return nil, err
		}
		out = append(out, vterm.DamageOp{
			Cells:      cloneVTermCells(decoded.cells),
			Timestamp:  decoded.timestamp,
			RowKind:    decoded.rowKind,
			WrappedSet: true,
			Wrapped:    row.Wrapped,
		})
	}
	return out, nil
}

func (s *terminalGridStore) recordLiveTailLineState(records []terminalLiveTailLogicalLineRecord, rows []vterm.DamageOp) error {
	if s == nil {
		return nil
	}
	dir, ok := s.lineMetadataWritableDir()
	if !ok {
		return nil
	}
	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !terminalLiveTailRecordsValidForLineState(records, rows) {
		metadata.LiveRecords = nil
		metadata.LiveRows = nil
		return writeTerminalGridLineMetadata(dir, metadata)
	}
	metadata.LiveRecords = terminalGridLineRecordMetasFromLiveTailRecords(records)
	rowMetas, err := terminalGridLineRowMetasFromDamageRows(rows)
	if err != nil {
		return err
	}
	metadata.LiveRows = rowMetas
	if err := writeTerminalGridLineMetadata(dir, metadata); err != nil {
		return err
	}
	s.mu.Lock()
	if liveMax := terminalGridLineRecordMetasMaxRuntimeID(metadata.LiveRecords); liveMax > s.maxRuntimeLineID {
		s.maxRuntimeLineID = liveMax
	}
	s.mu.Unlock()
	return nil
}

func (s *terminalGridStore) recordLineMigrations(migrations map[uint64]uint64) error {
	if s == nil || len(migrations) == 0 {
		return nil
	}
	dir, ok := s.lineMetadataWritableDir()
	if !ok {
		return nil
	}
	return recordTerminalGridLineMigrations(dir, migrations)
}

func recordTerminalGridLineMigrations(dir string, migrations map[uint64]uint64) error {
	if len(migrations) == 0 {
		return nil
	}
	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	merged := make(map[uint64]uint64, len(metadata.Migrations)+len(migrations))
	for _, migration := range metadata.Migrations {
		if !terminalRuntimeLogicalLineID(migration.RuntimeID) || !terminalPersistedLogicalLineID(migration.PersistedID) {
			continue
		}
		merged[migration.RuntimeID] = migration.PersistedID
	}
	for runtimeID, persistedID := range migrations {
		if !terminalRuntimeLogicalLineID(runtimeID) || !terminalPersistedLogicalLineID(persistedID) {
			continue
		}
		merged[runtimeID] = persistedID
	}
	metadata.Migrations = metadata.Migrations[:0]
	for runtimeID, persistedID := range merged {
		metadata.Migrations = append(metadata.Migrations, terminalGridLineMigration{
			RuntimeID:   runtimeID,
			PersistedID: persistedID,
		})
	}
	return writeTerminalGridLineMetadata(dir, metadata)
}

func (s *terminalGridStore) lineMetadataWritableDir() (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.removeOnClose || strings.TrimSpace(s.dir) == "" {
		return "", false
	}
	return s.dir, true
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
