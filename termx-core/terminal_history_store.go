package termx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/vterm"
)

const (
	defaultTerminalLiveScrollbackRows = 128
	defaultHistoryReplayRows          = 100
	maxHistoryReplayRows              = 250
	defaultHistoryChunkMaxBytes       = 4 * 1024 * 1024
)

type terminalHistoryStore struct {
	mu            sync.Mutex
	dir           string
	current       *os.File
	currentPath   string
	currentBytes  int64
	chunkMaxBytes int64
	rows          []terminalHistoryRowRef
	closed        bool
}

type terminalHistoryRowRef struct {
	path   string
	offset int64
	length int64
}

func newTerminalHistoryStore(terminalID string) (*terminalHistoryStore, error) {
	dir, err := os.MkdirTemp("", "termx-history-"+sanitizeHistoryStoreID(terminalID)+"-*")
	if err != nil {
		return nil, err
	}
	store := &terminalHistoryStore{
		dir:           dir,
		chunkMaxBytes: defaultHistoryChunkMaxBytes,
	}
	if err := store.rotateLocked(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return store, nil
}

func newMemoryTerminalHistoryStoreForTest(t testTempDirProvider) *terminalHistoryStore {
	if t == nil {
		return nil
	}
	dir := t.TempDir()
	store := &terminalHistoryStore{
		dir:           dir,
		chunkMaxBytes: defaultHistoryChunkMaxBytes,
	}
	if err := store.rotateLocked(); err != nil {
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
	replayRows := make([][]vterm.Cell, 0, len(rows))
	for _, row := range rows {
		replayRows = append(replayRows, row.Cells)
	}
	return s.AppendRows(replayRows)
}

func (s *terminalHistoryStore) AppendRows(rows [][]vterm.Cell) error {
	if s == nil || len(rows) == 0 {
		return nil
	}
	finish := perftrace.Measure("terminal.history.append")

	encoded := make([][]byte, 0, len(rows))
	totalBytes := 0
	for _, row := range rows {
		payload := vterm.EncodeHistoryRowsReplay([][]vterm.Cell{row})
		if payload == nil {
			payload = []byte("\x1b[0m")
		}
		encoded = append(encoded, payload)
		totalBytes += len(payload)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		finish(0)
		return nil
	}
	if err := s.appendEncodedRowsLocked(encoded); err != nil {
		finish(0)
		return err
	}
	finish(totalBytes)
	perftrace.Count("terminal.history.rows", len(encoded))
	perftrace.Count("terminal.history.bytes", totalBytes)
	return nil
}

func (s *terminalHistoryStore) Replay(beforeOffset int, limit int) ([]byte, int, bool, error) {
	if s == nil {
		return nil, 0, false, nil
	}
	beforeOffset, limit = sanitizeHistoryReplayWindow(beforeOffset, limit)

	s.mu.Lock()
	total := len(s.rows)
	if total == 0 {
		s.mu.Unlock()
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
	refs := append([]terminalHistoryRowRef(nil), s.rows[start:end]...)
	s.mu.Unlock()

	if len(refs) == 0 {
		return nil, 0, false, nil
	}
	finish := perftrace.Measure("terminal.history.replay")
	replay, err := readHistoryReplayRows(refs)
	finish(len(replay))
	if err != nil {
		return nil, 0, false, err
	}
	return replay, len(refs), start > 0, nil
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
	s.current = nil
	dir := s.dir
	s.mu.Unlock()

	var err error
	if current != nil {
		err = current.Close()
	}
	if removeErr := os.RemoveAll(dir); removeErr != nil && err == nil {
		err = removeErr
	}
	return err
}

func (s *terminalHistoryStore) appendEncodedRowsLocked(payloads [][]byte) error {
	if s == nil || len(payloads) == 0 {
		return nil
	}
	for index := 0; index < len(payloads); {
		if s.current == nil {
			if err := s.rotateLocked(); err != nil {
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

		path := s.currentPath
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
				path:   path,
				offset: offset + int64(batch.Len()),
				length: int64(len(payload)),
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
		s.rows = append(s.rows, refs...)
		s.currentBytes += int64(n)
	}
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
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(s.dir, fmt.Sprintf("history-%06d.chunk", len(s.rows)))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.current = file
	s.currentPath = path
	s.currentBytes = 0
	return nil
}

func readHistoryReplayRows(refs []terminalHistoryRowRef) ([]byte, error) {
	var out bytes.Buffer
	var currentPath string
	var file *os.File
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	for i, ref := range refs {
		if ref.path == "" || ref.length <= 0 {
			continue
		}
		if currentPath != ref.path {
			if file != nil {
				if err := file.Close(); err != nil {
					return nil, err
				}
			}
			f, err := os.Open(ref.path)
			if err != nil {
				return nil, err
			}
			file = f
			currentPath = ref.path
		}
		payload := make([]byte, ref.length)
		if _, err := file.ReadAt(payload, ref.offset); err != nil {
			return nil, err
		}
		if i > 0 {
			out.WriteString("\r\n")
		}
		out.Write(payload)
	}
	return out.Bytes(), nil
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
