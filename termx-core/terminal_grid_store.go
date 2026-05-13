package termx

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/vterm"
)

const (
	defaultTerminalLiveScrollbackRows = 128
	defaultGridReplayRows             = 100
	maxGridReplayRows                 = 250
	defaultGridPageMaxBytes           = 4 * 1024 * 1024

	terminalGridStoreVersion = 4
	terminalGridRowCodec     = "compact-line-v1"
	terminalGridIndexCodec   = "fixed20-le-v1"
	terminalGridMetadataName = "grid.meta.json"
	terminalGridIndexName    = "grid.index"
	terminalGridIndexRecord  = 20
)

type terminalGridStore struct {
	mu            sync.Mutex
	dir           string
	terminalID    string
	current       *os.File
	index         *os.File
	currentSeq    uint32
	currentBytes  int64
	pageMaxBytes  int64
	rowCount      int
	closed        bool
	removeOnClose bool
}

type terminalGridRowRef struct {
	seq    uint32
	offset int64
	length int64
	flags  uint32
}

const terminalGridRowFlagWrapped uint32 = 1 << 0

type terminalGridMetadata struct {
	StoreVersion  int    `json:"store_version"`
	TerminalID    string `json:"terminal_id"`
	RowCodec      string `json:"row_codec"`
	IndexCodec    string `json:"index_codec"`
	PageMaxBytes  int64  `json:"page_max_bytes"`
	RowCount      int    `json:"row_count,omitempty"`
	PageCount     int    `json:"page_count,omitempty"`
	CreatedAtUnix int64  `json:"created_at_unix"`
	UpdatedAtUnix int64  `json:"updated_at_unix"`
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
	return openTerminalGridStoreDir(terminalGridDir(gridRoot, terminalID), terminalID, true, false)
}

func openTerminalGridStoreForReplay(gridRoot, terminalID string) (*terminalGridStore, error) {
	if strings.TrimSpace(gridRoot) == "" {
		return nil, ErrNotFound
	}
	return openTerminalGridStoreDir(terminalGridDir(gridRoot, terminalID), terminalID, false, false)
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

	metadata, err := readTerminalGridMetadata(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && metadata.TerminalID != "" && terminalID != "" && metadata.TerminalID != terminalID {
		return nil, fmt.Errorf("termx grid metadata terminal id mismatch: got %q, want %q", metadata.TerminalID, terminalID)
	}
	if os.IsNotExist(err) {
		if !create {
			return nil, ErrNotFound
		}
		metadata = terminalGridMetadata{
			StoreVersion:  terminalGridStoreVersion,
			TerminalID:    terminalID,
			RowCodec:      terminalGridRowCodec,
			IndexCodec:    terminalGridIndexCodec,
			PageMaxBytes:  defaultGridPageMaxBytes,
			CreatedAtUnix: time.Now().UTC().Unix(),
		}
		if writeErr := writeTerminalGridMetadata(dir, metadata); writeErr != nil {
			return nil, writeErr
		}
	}
	if metadata.PageMaxBytes <= 0 {
		metadata.PageMaxBytes = defaultGridPageMaxBytes
	}

	indexState, err := loadTerminalGridIndexState(dir)
	if err != nil {
		return nil, err
	}
	store := &terminalGridStore{
		dir:           dir,
		terminalID:    terminalID,
		currentSeq:    indexState.lastSeq,
		pageMaxBytes:  metadata.PageMaxBytes,
		rowCount:      indexState.rowCount,
		removeOnClose: removeOnClose,
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

type testTempDirProvider interface {
	TempDir() string
}

func (s *terminalGridStore) AppendDamageRows(rows []vterm.DamageOp) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	replayRows := make([]terminalGridRow, 0, len(rows))
	for _, row := range rows {
		replayRows = append(replayRows, terminalGridRow{
			cells:     row.Cells,
			timestamp: row.Timestamp,
			rowKind:   row.RowKind,
			wrapped:   row.WrappedSet && row.Wrapped,
		})
	}
	return s.appendRows(replayRows)
}

func (s *terminalGridStore) AppendRows(rows [][]vterm.Cell) error {
	if len(rows) == 0 {
		return nil
	}
	gridRows := make([]terminalGridRow, 0, len(rows))
	for _, row := range rows {
		gridRows = append(gridRows, terminalGridRow{cells: row})
	}
	return s.appendRows(gridRows)
}

type terminalGridRow struct {
	cells     []vterm.Cell
	timestamp time.Time
	rowKind   string
	wrapped   bool
}

func (s *terminalGridStore) appendRows(rows []terminalGridRow) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	finish := perftrace.Measure("terminal.grid.append")

	encoded := make([][]byte, 0, len(rows))
	flags := make([]uint32, 0, len(rows))
	totalBytes := 0
	for _, row := range rows {
		payload, err := encodeTerminalGridRow(row)
		if err != nil {
			finish(0)
			return err
		}
		encoded = append(encoded, payload)
		var rowFlags uint32
		if row.wrapped {
			rowFlags |= terminalGridRowFlagWrapped
		}
		flags = append(flags, rowFlags)
		totalBytes += len(payload)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		finish(0)
		return nil
	}
	if err := s.appendEncodedRowsLocked(encoded, flags); err != nil {
		finish(0)
		return err
	}
	finish(totalBytes)
	perftrace.Count("terminal.grid.rows", len(encoded))
	perftrace.Count("terminal.grid.bytes", totalBytes)
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
		result.Rows, result.Timestamps, result.RowKinds, result.Wrapped = reflowTerminalGridRows(gridRows, cols)
		cropped := false
		if hasMore {
			cropped = trimTerminalGridViewportToTail(&result, limit)
		}
		result.LoadedRows = rows
		result.HasMore = hasMore || cropped
		result.BeforeOffset = beforeOffset
		result.Limit = limit
		result.TotalRows = s.RowCount()
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
	LoadedRows   int
	HasMore      bool
	BeforeOffset int
	Limit        int
	TotalRows    int
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
	for start > 0 {
		ref, err := readTerminalGridIndexRecordFromFile(file, start-1)
		if err != nil {
			return nil, 0, false, err
		}
		if ref.flags&terminalGridRowFlagWrapped == 0 {
			break
		}
		start--
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

func sanitizeGridReplayWindow(beforeOffset int, limit int) (int, int) {
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = defaultGridReplayRows
	}
	if limit > maxGridReplayRows {
		limit = maxGridReplayRows
	}
	return beforeOffset, limit
}

func sanitizeGridViewportWindow(beforeOffset int, limit int) (int, int) {
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = 500
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
	if !removeOnClose {
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

func (s *terminalGridStore) appendEncodedRowsLocked(payloads [][]byte, flags []uint32) error {
	if s == nil || len(payloads) == 0 {
		return nil
	}
	for index := 0; index < len(payloads); {
		if s.current == nil {
			if err := s.openCurrentPageLocked(); err != nil {
				return err
			}
		}
		next := payloads[index]
		if s.currentBytes > 0 && s.currentBytes+int64(len(next)) > s.pageMaxBytes {
			if err := s.rotateLocked(); err != nil {
				return err
			}
			continue
		}

		seq := s.currentSeq
		offset := s.currentBytes
		var batch bytes.Buffer
		refs := make([]terminalGridRowRef, 0, len(payloads)-index)
		for index < len(payloads) {
			payload := payloads[index]
			projected := s.currentBytes + int64(batch.Len()) + int64(len(payload))
			if batch.Len() > 0 && projected > s.pageMaxBytes {
				break
			}
			refs = append(refs, terminalGridRowRef{
				seq:    seq,
				offset: offset + int64(batch.Len()),
				length: int64(len(payload)),
				flags:  uint32At(flags, index),
			})
			batch.Write(payload)
			index++
			if projected >= s.pageMaxBytes {
				break
			}
		}
		if batch.Len() == 0 {
			continue
		}
		n, err := s.current.Write(batch.Bytes())
		if err != nil {
			return err
		}
		if n != batch.Len() {
			return io.ErrShortWrite
		}
		if err := s.appendIndexRowsLocked(refs); err != nil {
			return err
		}
		s.rowCount += len(refs)
		s.currentBytes += int64(n)
	}
	return nil
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
	n, err := s.index.Write(buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
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

func uint32At(values []uint32, index int) uint32 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
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
	scrollback, timestamps, rowKinds, wrapped := terminalGridRowSlices(rows)
	vt := vterm.New(vtInitialWidth(cols, scrollback), 1, gridReflowScrollbackSize(scrollback, cols), nil)
	vt.LoadSnapshotWithExtendedMetadata(scrollback, timestamps, rowKinds, wrapped, vterm.ScreenData{Cells: [][]vterm.Cell{make([]vterm.Cell, vtInitialWidth(cols, scrollback))}}, nil, nil, nil, vterm.CursorState{Visible: true}, vterm.TerminalModes{AutoWrap: true})
	vt.Resize(cols, 1)
	out := vt.ScrollbackContent()
	outTimestamps := vt.ScrollbackTimestamps()
	outRowKinds := vt.ScrollbackRowKinds()
	outWrapped := vt.ScrollbackWrapped()
	screen := vt.ScreenContent()
	if len(screen.Cells) > 0 && !isBlankGridRow(screen.Cells[0]) {
		out = append(out, screen.Cells[0])
		outTimestamps = append(outTimestamps, timeAt(vt.ScreenTimestamps(), 0))
		outRowKinds = append(outRowKinds, stringAt(vt.ScreenRowKinds(), 0))
		outWrapped = append(outWrapped, boolAt(vt.ScreenWrapped(), 0))
	}
	if len(out) == 0 {
		out = [][]vterm.Cell{make([]vterm.Cell, cols)}
	}
	return out, alignGridTimes(outTimestamps, len(out)), alignGridStrings(outRowKinds, len(out)), alignGridBools(outWrapped, len(out))
}

func terminalGridRowSlices(rows []terminalGridRow) ([][]vterm.Cell, []time.Time, []string, []bool) {
	cells := make([][]vterm.Cell, 0, len(rows))
	timestamps := make([]time.Time, 0, len(rows))
	rowKinds := make([]string, 0, len(rows))
	wrapped := make([]bool, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, row.cells)
		timestamps = append(timestamps, row.timestamp)
		rowKinds = append(rowKinds, row.rowKind)
		wrapped = append(wrapped, row.wrapped)
	}
	return cells, timestamps, rowKinds, wrapped
}

func vtInitialWidth(cols int, rows [][]vterm.Cell) int {
	return maxGridCellRowDisplayWidth(rows)
}

func gridReflowScrollbackSize(rows [][]vterm.Cell, cols int) int {
	if cols <= 0 {
		cols = 80
	}
	needed := len(rows) + 2
	for _, row := range rows {
		width := gridCellRowDisplayWidth(row)
		if width <= cols {
			continue
		}
		needed += (width + cols - 1) / cols
	}
	return maxInt(needed, 32)
}

func maxGridCellRowDisplayWidth(rows [][]vterm.Cell) int {
	width := 1
	for _, row := range rows {
		if rowWidth := gridCellRowDisplayWidth(row); rowWidth > width {
			width = rowWidth
		}
	}
	return width
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

func readTerminalGridMetadata(dir string) (terminalGridMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, terminalGridMetadataName))
	if err != nil {
		return terminalGridMetadata{}, err
	}
	var metadata terminalGridMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return terminalGridMetadata{}, err
	}
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
	metadata.UpdatedAtUnix = now
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, terminalGridMetadataName), data, 0o600)
}

type terminalGridIndexState struct {
	rowCount int
	lastSeq  uint32
}

func loadTerminalGridIndexState(dir string) (terminalGridIndexState, error) {
	file, err := os.Open(filepath.Join(dir, terminalGridIndexName))
	if err != nil {
		if os.IsNotExist(err) {
			return terminalGridIndexState{}, nil
		}
		return terminalGridIndexState{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return terminalGridIndexState{}, err
	}
	rowCount := int(info.Size() / terminalGridIndexRecord)
	if rowCount <= 0 {
		return terminalGridIndexState{}, nil
	}
	ref, err := readTerminalGridIndexRecordFromFile(file, rowCount-1)
	if err != nil {
		return terminalGridIndexState{}, err
	}
	return terminalGridIndexState{rowCount: rowCount, lastSeq: ref.seq}, nil
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

func readTerminalGridIndexWindowFromFile(file *os.File, start int, count int) ([]terminalGridRowRef, error) {
	if file == nil || start < 0 {
		return nil, ErrNotFound
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
