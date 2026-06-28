package history

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// NewFileStorageBackend 创建最小文件驻留 backend。
// domain boundary：文件只保存 logical-line payload 的最新版本和读取 offset；
// timeline/window/cursor truth 仍由 HistoryStore 持有，不能从文件顺序反推历史顺序。
func NewFileStorageBackend(dir string, terminalID string) (StorageBackend, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, terminalID+".history-lines.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &fileStorageBackend{
		path:    path,
		file:    file,
		offsets: make(map[LogicalLineID]int64),
	}, nil
}

type fileStorageBackend struct {
	path    string
	file    *os.File
	offsets map[LogicalLineID]int64
}

type fileLineRecord struct {
	Line LogicalLine `json:"line"`
}

func (backend *fileStorageBackend) Apply(tx StorageTransaction) error {
	if backend == nil {
		return nil
	}
	if backend.offsets == nil {
		backend.offsets = make(map[LogicalLineID]int64)
	}
	for _, line := range tx.Lines {
		offset, err := backend.file.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(fileLineRecord{Line: cloneLogicalLine(line)})
		if err != nil {
			return err
		}
		if _, err := backend.file.Write(append(payload, '\n')); err != nil {
			return err
		}
		backend.offsets[line.ID] = offset
	}
	for _, id := range tx.Tombstones {
		delete(backend.offsets, id)
	}
	return nil
}

func (backend *fileStorageBackend) Recover() (RecoveredHistoryState, error) {
	if backend == nil {
		return RecoveredHistoryState{}, nil
	}
	return RecoveredHistoryState{}, nil
}

func (backend *fileStorageBackend) Compact(StorageCompactionPolicy) error {
	return nil
}

func (backend *fileStorageBackend) GetLine(id LogicalLineID) (LogicalLine, bool) {
	if backend == nil || backend.file == nil {
		return LogicalLine{}, false
	}
	offset, ok := backend.offsets[id]
	if !ok {
		return LogicalLine{}, false
	}
	if _, err := backend.file.Seek(offset, io.SeekStart); err != nil {
		return LogicalLine{}, false
	}
	decoder := json.NewDecoder(backend.file)
	var record fileLineRecord
	if err := decoder.Decode(&record); err != nil {
		return LogicalLine{}, false
	}
	return cloneLogicalLine(record.Line), true
}

func (backend *fileStorageBackend) GetLines(ids []LogicalLineID) ([]LogicalLine, error) {
	if backend == nil || len(ids) == 0 {
		return nil, nil
	}
	lines := make([]LogicalLine, 0, len(ids))
	for _, id := range ids {
		line, ok := backend.GetLine(id)
		if ok {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
