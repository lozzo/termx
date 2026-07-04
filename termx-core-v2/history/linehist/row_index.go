package linehist

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
)

const (
	coldRowIndexMagic      uint32 = 0x54584C52 // "TXLR"
	coldRowIndexVersion    uint16 = 1
	coldRowIndexHeaderSize        = 16
	coldRowIndexEntrySize         = 4
	coldRowIndexBlockSize         = 1024
)

// coldRowIndex 是某个 cols 下的持久 row-count 索引。
// sidecar 保存每条 logical line 投影出的 row 数；内存只保留 block prefix，
// 避免 cold history 很大时为每个 cols 常驻一整份 per-line prefix。
type coldRowIndex struct {
	mu          sync.Mutex
	cols        int
	path        string
	file        *os.File
	writer      *bufio.Writer
	covered     int
	totalRows   int
	blockPrefix []int
}

func openColdRowIndex(path string, cols int, lineCount int) (*coldRowIndex, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	idx := &coldRowIndex{
		cols:        cols,
		path:        path,
		file:        file,
		blockPrefix: []int{0},
	}
	if err := idx.recover(lineCount); err != nil {
		_ = file.Close()
		return nil, err
	}
	return idx, nil
}

func (idx *coldRowIndex) recover(lineCount int) error {
	info, err := idx.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < coldRowIndexHeaderSize {
		return idx.reset()
	}
	var header [coldRowIndexHeaderSize]byte
	if _, err := idx.file.ReadAt(header[:], 0); err != nil {
		return idx.reset()
	}
	if binary.LittleEndian.Uint32(header[0:4]) != coldRowIndexMagic ||
		binary.LittleEndian.Uint16(header[4:6]) != coldRowIndexVersion ||
		binary.LittleEndian.Uint16(header[6:8]) != coldRowIndexEntrySize ||
		int(binary.LittleEndian.Uint32(header[8:12])) != idx.cols ||
		int(binary.LittleEndian.Uint32(header[12:16])) != coldRowIndexBlockSize {
		return idx.reset()
	}
	entriesSize := info.Size() - coldRowIndexHeaderSize
	completeEntriesSize := entriesSize - entriesSize%coldRowIndexEntrySize
	if maxEntriesSize := int64(lineCount) * int64(coldRowIndexEntrySize); completeEntriesSize > maxEntriesSize {
		completeEntriesSize = maxEntriesSize
	}
	if completeEntriesSize != entriesSize {
		if err := idx.file.Truncate(coldRowIndexHeaderSize + completeEntriesSize); err != nil {
			return err
		}
	}
	entryCount := int(completeEntriesSize / coldRowIndexEntrySize)
	idx.covered = 0
	idx.totalRows = 0
	idx.blockPrefix = []int{0}
	if entryCount == 0 {
		return nil
	}
	reader := io.NewSectionReader(idx.file, coldRowIndexHeaderSize, completeEntriesSize)
	const batchEntries = 4096
	raw := make([]byte, batchEntries*coldRowIndexEntrySize)
	for idx.covered < entryCount {
		want := minInt(batchEntries, entryCount-idx.covered)
		buf := raw[:want*coldRowIndexEntrySize]
		if _, err := io.ReadFull(reader, buf); err != nil {
			return err
		}
		for i := 0; i < want; i++ {
			idx.appendCountInMemoryLocked(int(binary.LittleEndian.Uint32(buf[i*coldRowIndexEntrySize : i*coldRowIndexEntrySize+4])))
		}
	}
	return nil
}

func (idx *coldRowIndex) reset() error {
	if idx.writer != nil {
		if err := idx.writer.Flush(); err != nil {
			return err
		}
	}
	if err := idx.file.Truncate(0); err != nil {
		return err
	}
	if _, err := idx.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	idx.covered = 0
	idx.totalRows = 0
	idx.blockPrefix = []int{0}
	if idx.writer == nil {
		idx.writer = bufio.NewWriterSize(idx.file, 64*1024)
	}
	var header [coldRowIndexHeaderSize]byte
	binary.LittleEndian.PutUint32(header[0:4], coldRowIndexMagic)
	binary.LittleEndian.PutUint16(header[4:6], coldRowIndexVersion)
	binary.LittleEndian.PutUint16(header[6:8], coldRowIndexEntrySize)
	binary.LittleEndian.PutUint32(header[8:12], uint32(idx.cols))
	binary.LittleEndian.PutUint32(header[12:16], uint32(coldRowIndexBlockSize))
	if _, err := idx.writer.Write(header[:]); err != nil {
		return err
	}
	return idx.writer.Flush()
}

func (idx *coldRowIndex) ensure(engine *Engine, atLeast int) error {
	if idx == nil || engine == nil {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if atLeast <= idx.covered {
		return nil
	}
	const batch = 1024
	for idx.covered < atLeast {
		end := minInt(atLeast, idx.covered+batch)
		lines, err := engine.Lines(idx.covered, end)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errors.New("linehist: row index cold line range out of range")
		}
		counts := make([]uint32, len(lines))
		for i, line := range lines {
			counts[i] = uint32(countWrappedRows(line.Runs, idx.cols))
		}
		if err := idx.appendCountsLocked(counts); err != nil {
			return err
		}
	}
	return nil
}

func (idx *coldRowIndex) appendCountsLocked(counts []uint32) error {
	if idx == nil || len(counts) == 0 {
		return nil
	}
	if idx.writer == nil {
		idx.writer = bufio.NewWriterSize(idx.file, 64*1024)
	}
	if _, err := idx.file.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	var raw [coldRowIndexEntrySize]byte
	for _, count := range counts {
		binary.LittleEndian.PutUint32(raw[:], count)
		if _, err := idx.writer.Write(raw[:]); err != nil {
			return err
		}
		idx.appendCountInMemoryLocked(int(count))
	}
	return idx.writer.Flush()
}

func (idx *coldRowIndex) appendCountInMemoryLocked(count int) {
	if idx.covered > 0 && idx.covered%coldRowIndexBlockSize == 0 {
		idx.blockPrefix = append(idx.blockPrefix, idx.totalRows)
	}
	idx.covered++
	idx.totalRows += count
}

func (idx *coldRowIndex) rowsBetween(startLine int, endLine int) (int, error) {
	if idx == nil {
		return 0, nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	start, err := idx.rowOffsetForLineLocked(startLine)
	if err != nil {
		return 0, err
	}
	end, err := idx.rowOffsetForLineLocked(endLine)
	if err != nil {
		return 0, err
	}
	return end - start, nil
}

func (idx *coldRowIndex) rowOffsetForLine(line int) (int, error) {
	if idx == nil {
		return 0, nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.rowOffsetForLineLocked(line)
}

func (idx *coldRowIndex) rowOffsetForLineLocked(line int) (int, error) {
	if line <= 0 {
		return 0, nil
	}
	if line >= idx.covered {
		return idx.totalRows, nil
	}
	block := line / coldRowIndexBlockSize
	blockStart := block * coldRowIndexBlockSize
	sum := idx.blockPrefix[block]
	counts, err := idx.readCountsLocked(blockStart, line)
	if err != nil {
		return 0, err
	}
	for _, count := range counts {
		sum += int(count)
	}
	return sum, nil
}

func (idx *coldRowIndex) lineForRow(row int) (int, error) {
	if idx == nil {
		return 0, nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.lineForRowLocked(row)
}

func (idx *coldRowIndex) lineForRowLocked(row int) (int, error) {
	if idx.covered == 0 {
		return 0, nil
	}
	if row < 0 {
		row = 0
	}
	if row >= idx.totalRows {
		row = idx.totalRows - 1
	}
	block := sort.Search(len(idx.blockPrefix), func(i int) bool {
		return idx.blockPrefix[i] > row
	}) - 1
	if block < 0 {
		block = 0
	}
	blockStart := block * coldRowIndexBlockSize
	blockEnd := minInt(idx.covered, blockStart+coldRowIndexBlockSize)
	acc := idx.blockPrefix[block]
	counts, err := idx.readCountsLocked(blockStart, blockEnd)
	if err != nil {
		return 0, err
	}
	for i, count := range counts {
		next := acc + int(count)
		if next > row {
			return blockStart + i, nil
		}
		acc = next
	}
	return blockEnd - 1, nil
}

func (idx *coldRowIndex) readCounts(startLine int, endLine int) ([]uint32, error) {
	if idx == nil {
		return nil, nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.readCountsLocked(startLine, endLine)
}

func (idx *coldRowIndex) readCountsLocked(startLine int, endLine int) ([]uint32, error) {
	if endLine <= startLine {
		return nil, nil
	}
	if idx.writer != nil {
		if err := idx.writer.Flush(); err != nil {
			return nil, err
		}
	}
	startLine = clampInt(startLine, 0, idx.covered)
	endLine = clampInt(endLine, startLine, idx.covered)
	raw := make([]byte, (endLine-startLine)*coldRowIndexEntrySize)
	offset := int64(coldRowIndexHeaderSize + startLine*coldRowIndexEntrySize)
	if _, err := idx.file.ReadAt(raw, offset); err != nil {
		return nil, err
	}
	counts := make([]uint32, endLine-startLine)
	for i := range counts {
		counts[i] = binary.LittleEndian.Uint32(raw[i*coldRowIndexEntrySize : i*coldRowIndexEntrySize+4])
	}
	return counts, nil
}

func (idx *coldRowIndex) close() error {
	if idx == nil {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.file == nil {
		return nil
	}
	if idx.writer != nil {
		if err := idx.writer.Flush(); err != nil {
			return err
		}
		idx.writer = nil
	}
	err := idx.file.Close()
	idx.file = nil
	return err
}
