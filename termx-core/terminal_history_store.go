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
	defaultHistoryReplayRows          = 100
	maxHistoryReplayRows              = 250
	defaultHistoryChunkMaxBytes       = 4 * 1024 * 1024

	terminalHistoryFormatVersion = 4
	terminalHistoryCodec         = "compact-line-v1"
	terminalHistoryManifestName  = "manifest.json"
	terminalHistoryIndexName     = "history.index"
	terminalHistoryIndexRecord   = 20
)

type terminalHistoryStore struct {
	mu            sync.Mutex
	dir           string
	terminalID    string
	current       *os.File
	index         *os.File
	currentSeq    uint32
	currentBytes  int64
	chunkMaxBytes int64
	rows          []terminalHistoryRowRef
	closed        bool
	removeOnClose bool
}

type terminalHistoryRowRef struct {
	seq    uint32
	offset int64
	length int64
	flags  uint32
}

const terminalHistoryRowFlagWrapped uint32 = 1 << 0

type terminalHistoryManifest struct {
	FormatVersion int    `json:"format_version"`
	TerminalID    string `json:"terminal_id"`
	Codec         string `json:"codec"`
	ChunkMaxBytes int64  `json:"chunk_max_bytes"`
	RowCount      int    `json:"row_count,omitempty"`
	ChunkCount    int    `json:"chunk_count,omitempty"`
	CreatedAtUnix int64  `json:"created_at_unix"`
	UpdatedAtUnix int64  `json:"updated_at_unix"`
}

func newTerminalHistoryStore(historyRoot, terminalID string) (*terminalHistoryStore, error) {
	if strings.TrimSpace(historyRoot) == "" {
		dir, err := os.MkdirTemp("", "termx-history-"+sanitizeHistoryStoreID(terminalID)+"-*")
		if err != nil {
			return nil, err
		}
		store, err := openTerminalHistoryStoreDir(dir, terminalID, true, true)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		return store, nil
	}
	return openTerminalHistoryStoreDir(terminalHistoryDir(historyRoot, terminalID), terminalID, true, false)
}

func openTerminalHistoryStoreForReplay(historyRoot, terminalID string) (*terminalHistoryStore, error) {
	if strings.TrimSpace(historyRoot) == "" {
		return nil, ErrNotFound
	}
	return openTerminalHistoryStoreDir(terminalHistoryDir(historyRoot, terminalID), terminalID, false, false)
}

func openTerminalHistoryStoreDir(dir, terminalID string, create bool, removeOnClose bool) (*terminalHistoryStore, error) {
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

	manifest, err := readTerminalHistoryManifest(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && manifest.TerminalID != "" && terminalID != "" && manifest.TerminalID != terminalID {
		return nil, fmt.Errorf("termx history manifest terminal id mismatch: got %q, want %q", manifest.TerminalID, terminalID)
	}
	if os.IsNotExist(err) {
		if !create {
			return nil, ErrNotFound
		}
		manifest = terminalHistoryManifest{
			FormatVersion: terminalHistoryFormatVersion,
			TerminalID:    terminalID,
			Codec:         terminalHistoryCodec,
			ChunkMaxBytes: defaultHistoryChunkMaxBytes,
			CreatedAtUnix: time.Now().UTC().Unix(),
		}
		if writeErr := writeTerminalHistoryManifest(dir, manifest); writeErr != nil {
			return nil, writeErr
		}
	}
	if manifest.ChunkMaxBytes <= 0 {
		manifest.ChunkMaxBytes = defaultHistoryChunkMaxBytes
	}

	rows, lastSeq, err := loadTerminalHistoryIndex(dir)
	if err != nil {
		return nil, err
	}
	store := &terminalHistoryStore{
		dir:           dir,
		terminalID:    terminalID,
		currentSeq:    lastSeq,
		chunkMaxBytes: manifest.ChunkMaxBytes,
		rows:          rows,
		removeOnClose: removeOnClose,
	}
	if !create {
		return store, nil
	}

	index, err := os.OpenFile(filepath.Join(dir, terminalHistoryIndexName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	store.index = index
	if err := store.openCurrentChunkLocked(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func newMemoryTerminalHistoryStoreForTest(t testTempDirProvider) *terminalHistoryStore {
	if t == nil {
		return nil
	}
	store, err := openTerminalHistoryStoreDir(t.TempDir(), "test", true, false)
	if err != nil {
		panic(err)
	}
	return store
}

type testTempDirProvider interface {
	TempDir() string
}

func (s *terminalHistoryStore) AppendDamageRows(rows []vterm.DamageOp) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	replayRows := make([]terminalHistoryRow, 0, len(rows))
	for _, row := range rows {
		replayRows = append(replayRows, terminalHistoryRow{
			cells:     row.Cells,
			timestamp: row.Timestamp,
			rowKind:   row.RowKind,
			wrapped:   row.WrappedSet && row.Wrapped,
		})
	}
	return s.appendRows(replayRows)
}

func (s *terminalHistoryStore) AppendRows(rows [][]vterm.Cell) error {
	if len(rows) == 0 {
		return nil
	}
	historyRows := make([]terminalHistoryRow, 0, len(rows))
	for _, row := range rows {
		historyRows = append(historyRows, terminalHistoryRow{cells: row})
	}
	return s.appendRows(historyRows)
}

type terminalHistoryRow struct {
	cells     []vterm.Cell
	timestamp time.Time
	rowKind   string
	wrapped   bool
}

func (s *terminalHistoryStore) appendRows(rows []terminalHistoryRow) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	finish := perftrace.Measure("terminal.history.append")

	encoded := make([][]byte, 0, len(rows))
	flags := make([]uint32, 0, len(rows))
	totalBytes := 0
	for _, row := range rows {
		payload, err := encodeTerminalHistoryRow(row)
		if err != nil {
			finish(0)
			return err
		}
		encoded = append(encoded, payload)
		var rowFlags uint32
		if row.wrapped {
			rowFlags |= terminalHistoryRowFlagWrapped
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
	perftrace.Count("terminal.history.rows", len(encoded))
	perftrace.Count("terminal.history.bytes", totalBytes)
	return nil
}

func (s *terminalHistoryStore) RowCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func (s *terminalHistoryStore) Replay(beforeOffset int, limit int) ([]byte, int, bool, error) {
	if s == nil {
		return nil, 0, false, nil
	}
	beforeOffset, limit = sanitizeHistoryReplayWindow(beforeOffset, limit)

	refs, rows, hasMore := s.windowRefs(beforeOffset, limit)
	if rows == 0 {
		return nil, 0, false, nil
	}
	finish := perftrace.Measure("terminal.history.replay")
	historyRows, err := readTerminalHistoryRows(s.dir, refs)
	if err != nil {
		finish(0)
		return nil, 0, false, err
	}
	replay := encodeHistoryRowsReplay(historyRows)
	finish(len(replay))
	return replay, rows, hasMore, nil
}

func (s *terminalHistoryStore) Rows(beforeOffset int, limit int, cols int) (terminalHistoryRowsResult, error) {
	var result terminalHistoryRowsResult
	if s == nil {
		return result, nil
	}
	beforeOffset, limit = sanitizeHistorySnapshotWindow(beforeOffset, limit)
	rawLimit := limit
	for {
		refs, rows, hasMore := s.windowRefs(beforeOffset, rawLimit)
		if rows == 0 {
			return result, nil
		}
		finish := perftrace.Measure("terminal.history.rows_read")
		historyRows, err := readTerminalHistoryRows(s.dir, refs)
		finish(len(historyRows))
		if err != nil {
			return result, err
		}
		result.Rows, result.Timestamps, result.RowKinds, result.Wrapped = reflowTerminalHistoryRows(historyRows, cols)
		cropped := false
		if hasMore {
			cropped = trimTerminalHistoryRowsResultToTail(&result, limit)
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

type terminalHistoryRowsResult struct {
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

func trimTerminalHistoryRowsResultToTail(result *terminalHistoryRowsResult, limit int) bool {
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

func (s *terminalHistoryStore) windowRefs(beforeOffset int, limit int) ([]terminalHistoryRowRef, int, bool) {
	s.mu.Lock()
	total := len(s.rows)
	if total == 0 {
		s.mu.Unlock()
		return nil, 0, false
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
	for start > 0 && s.rows[start-1].flags&terminalHistoryRowFlagWrapped != 0 {
		start--
	}
	refs := append([]terminalHistoryRowRef(nil), s.rows[start:end]...)
	s.mu.Unlock()

	if len(refs) == 0 {
		return nil, 0, false
	}
	return refs, len(refs), start > 0
}

func sanitizeHistoryReplayWindow(beforeOffset int, limit int) (int, int) {
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = defaultHistoryReplayRows
	}
	if limit > maxHistoryReplayRows {
		limit = maxHistoryReplayRows
	}
	return beforeOffset, limit
}

func sanitizeHistorySnapshotWindow(beforeOffset int, limit int) (int, int) {
	if beforeOffset < 0 {
		beforeOffset = 0
	}
	if limit <= 0 {
		limit = 500
	}
	return beforeOffset, limit
}

func (s *terminalHistoryStore) Close() error {
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
	manifest := terminalHistoryManifest{
		FormatVersion: terminalHistoryFormatVersion,
		TerminalID:    s.terminalID,
		Codec:         terminalHistoryCodec,
		ChunkMaxBytes: s.chunkMaxBytes,
		RowCount:      len(s.rows),
		ChunkCount:    int(s.currentSeq) + 1,
	}
	if existing, err := readTerminalHistoryManifest(dir); err == nil {
		manifest.CreatedAtUnix = existing.CreatedAtUnix
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
		if manifestErr := writeTerminalHistoryManifest(dir, manifest); manifestErr != nil && err == nil {
			err = manifestErr
		}
	}
	if removeOnClose {
		if removeErr := os.RemoveAll(dir); removeErr != nil && err == nil {
			err = removeErr
		}
	}
	return err
}

func (s *terminalHistoryStore) appendEncodedRowsLocked(payloads [][]byte, flags []uint32) error {
	if s == nil || len(payloads) == 0 {
		return nil
	}
	for index := 0; index < len(payloads); {
		if s.current == nil {
			if err := s.openCurrentChunkLocked(); err != nil {
				return err
			}
		}
		next := payloads[index]
		if s.currentBytes > 0 && s.currentBytes+int64(len(next)) > s.chunkMaxBytes {
			if err := s.rotateLocked(); err != nil {
				return err
			}
			continue
		}

		seq := s.currentSeq
		offset := s.currentBytes
		var batch bytes.Buffer
		refs := make([]terminalHistoryRowRef, 0, len(payloads)-index)
		for index < len(payloads) {
			payload := payloads[index]
			projected := s.currentBytes + int64(batch.Len()) + int64(len(payload))
			if batch.Len() > 0 && projected > s.chunkMaxBytes {
				break
			}
			refs = append(refs, terminalHistoryRowRef{
				seq:    seq,
				offset: offset + int64(batch.Len()),
				length: int64(len(payload)),
				flags:  uint32At(flags, index),
			})
			batch.Write(payload)
			index++
			if projected >= s.chunkMaxBytes {
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
		s.rows = append(s.rows, refs...)
		s.currentBytes += int64(n)
	}
	return nil
}

func (s *terminalHistoryStore) openCurrentChunkLocked() error {
	if s == nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(s.dir, terminalHistoryChunkName(s.currentSeq))
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

func (s *terminalHistoryStore) rotateLocked() error {
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
	return s.openCurrentChunkLocked()
}

func (s *terminalHistoryStore) appendIndexRowsLocked(refs []terminalHistoryRowRef) error {
	if s == nil || len(refs) == 0 {
		return nil
	}
	if s.index == nil {
		index, err := os.OpenFile(filepath.Join(s.dir, terminalHistoryIndexName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		s.index = index
	}
	buf := make([]byte, len(refs)*terminalHistoryIndexRecord)
	for i, ref := range refs {
		if ref.length <= 0 || ref.length > int64(^uint32(0)) {
			return fmt.Errorf("termx history row length out of range: %d", ref.length)
		}
		base := i * terminalHistoryIndexRecord
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

func readTerminalHistoryRows(dir string, refs []terminalHistoryRowRef) ([]terminalHistoryRow, error) {
	out := make([]terminalHistoryRow, 0, len(refs))
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
			f, err := os.Open(filepath.Join(dir, terminalHistoryChunkName(ref.seq)))
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
		row, err := decodeTerminalHistoryRow(payload)
		if err != nil {
			return nil, err
		}
		row.wrapped = ref.flags&terminalHistoryRowFlagWrapped != 0
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

func encodeHistoryRowsReplay(rows []terminalHistoryRow) []byte {
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

func reflowTerminalHistoryRows(rows []terminalHistoryRow, cols int) ([][]vterm.Cell, []time.Time, []string, []bool) {
	if len(rows) == 0 {
		return nil, nil, nil, nil
	}
	if cols <= 0 {
		cols = 80
	}
	scrollback, timestamps, rowKinds, wrapped := terminalHistoryRowSlices(rows)
	vt := vterm.New(vtInitialWidth(cols, scrollback), 1, historyReflowScrollbackSize(scrollback, cols), nil)
	vt.LoadSnapshotWithExtendedMetadata(scrollback, timestamps, rowKinds, wrapped, vterm.ScreenData{Cells: [][]vterm.Cell{make([]vterm.Cell, vtInitialWidth(cols, scrollback))}}, nil, nil, nil, vterm.CursorState{Visible: true}, vterm.TerminalModes{AutoWrap: true})
	vt.Resize(cols, 1)
	out := vt.ScrollbackContent()
	outTimestamps := vt.ScrollbackTimestamps()
	outRowKinds := vt.ScrollbackRowKinds()
	outWrapped := vt.ScrollbackWrapped()
	screen := vt.ScreenContent()
	if len(screen.Cells) > 0 && !isBlankHistoryRow(screen.Cells[0]) {
		out = append(out, screen.Cells[0])
		outTimestamps = append(outTimestamps, timeAt(vt.ScreenTimestamps(), 0))
		outRowKinds = append(outRowKinds, stringAt(vt.ScreenRowKinds(), 0))
		outWrapped = append(outWrapped, boolAt(vt.ScreenWrapped(), 0))
	}
	if len(out) == 0 {
		out = [][]vterm.Cell{make([]vterm.Cell, cols)}
	}
	return out, alignHistoryTimes(outTimestamps, len(out)), alignHistoryStrings(outRowKinds, len(out)), alignHistoryBools(outWrapped, len(out))
}

func terminalHistoryRowSlices(rows []terminalHistoryRow) ([][]vterm.Cell, []time.Time, []string, []bool) {
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
	return maxHistoryCellRowDisplayWidth(rows)
}

func historyReflowScrollbackSize(rows [][]vterm.Cell, cols int) int {
	if cols <= 0 {
		cols = 80
	}
	needed := len(rows) + 2
	for _, row := range rows {
		width := historyCellRowDisplayWidth(row)
		if width <= cols {
			continue
		}
		needed += (width + cols - 1) / cols
	}
	return maxInt(needed, 32)
}

func maxHistoryCellRowDisplayWidth(rows [][]vterm.Cell) int {
	width := 1
	for _, row := range rows {
		if rowWidth := historyCellRowDisplayWidth(row); rowWidth > width {
			width = rowWidth
		}
	}
	return width
}

func historyCellRowDisplayWidth(row []vterm.Cell) int {
	width := 0
	for _, cell := range row {
		if cell.Width > 0 {
			width += cell.Width
		}
	}
	return width
}

func alignHistoryTimes(values []time.Time, size int) []time.Time {
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

func alignHistoryStrings(values []string, size int) []string {
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

func alignHistoryBools(values []bool, size int) []bool {
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

func isBlankHistoryRow(row []vterm.Cell) bool {
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

func terminalHistoryDir(root, terminalID string) string {
	sum := sha1.Sum([]byte(terminalID))
	return filepath.Join(root, fmt.Sprintf("terminal-v%d-%s-%s", terminalHistoryFormatVersion, sanitizeHistoryStoreID(terminalID), hex.EncodeToString(sum[:6])))
}

func terminalHistoryChunkName(seq uint32) string {
	return fmt.Sprintf("history-%06d.chunk", seq)
}

func readTerminalHistoryManifest(dir string) (terminalHistoryManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, terminalHistoryManifestName))
	if err != nil {
		return terminalHistoryManifest{}, err
	}
	var manifest terminalHistoryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return terminalHistoryManifest{}, err
	}
	if manifest.FormatVersion != 0 && manifest.FormatVersion != terminalHistoryFormatVersion {
		return terminalHistoryManifest{}, fmt.Errorf("unsupported terminal history format version %d", manifest.FormatVersion)
	}
	if manifest.Codec != "" && manifest.Codec != terminalHistoryCodec {
		return terminalHistoryManifest{}, fmt.Errorf("unsupported terminal history codec %q", manifest.Codec)
	}
	return manifest, nil
}

func writeTerminalHistoryManifest(dir string, manifest terminalHistoryManifest) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if manifest.FormatVersion == 0 {
		manifest.FormatVersion = terminalHistoryFormatVersion
	}
	if manifest.Codec == "" {
		manifest.Codec = terminalHistoryCodec
	}
	if manifest.ChunkMaxBytes <= 0 {
		manifest.ChunkMaxBytes = defaultHistoryChunkMaxBytes
	}
	now := time.Now().UTC().Unix()
	if manifest.CreatedAtUnix == 0 {
		manifest.CreatedAtUnix = now
	}
	manifest.UpdatedAtUnix = now
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, terminalHistoryManifestName), data, 0o600)
}

func loadTerminalHistoryIndex(dir string) ([]terminalHistoryRowRef, uint32, error) {
	file, err := os.Open(filepath.Join(dir, terminalHistoryIndexName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer file.Close()

	var (
		rows []terminalHistoryRowRef
		last uint32
	)
	rec := make([]byte, terminalHistoryIndexRecord)
	for {
		if _, err := io.ReadFull(file, rec); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, 0, err
		}
		seq := binary.LittleEndian.Uint32(rec[0:4])
		offset := int64(binary.LittleEndian.Uint64(rec[4:12]))
		length := int64(binary.LittleEndian.Uint32(rec[12:16]))
		flags := binary.LittleEndian.Uint32(rec[16:20])
		if length <= 0 {
			continue
		}
		rows = append(rows, terminalHistoryRowRef{seq: seq, offset: offset, length: length, flags: flags})
		last = seq
	}
	return rows, last, nil
}

func observeTerminalHistoryIDs(root string) {
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
		manifest, err := readTerminalHistoryManifest(filepath.Join(root, entry.Name()))
		if err != nil || manifest.TerminalID == "" {
			continue
		}
		ObserveGeneratedID(manifest.TerminalID)
	}
}

func sanitizeHistoryStoreID(id string) string {
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

func closeTerminalHistoryStore(store *terminalHistoryStore) error {
	if store == nil {
		return nil
	}
	err := store.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
