package history

import (
	"encoding/binary"
	"sort"
	"sync"
)

// StorageBackend persists logical line payloads and does not define whether a
// line is mutable, committed, or retained.
type StorageBackend interface {
	SaveLine(LogicalLine) error
	LoadLine(LogicalLineID) (LogicalLine, bool)
	DeleteLine(LogicalLineID) bool
	LineIDs() []LogicalLineID
}

type snapshotLineBackend interface {
	LoadSnapshotLine(LogicalLineID) (LogicalLine, bool)
}

type ownedLineBackend interface {
	saveOwnedLine(LogicalLine) error
}

// MemoryStorageBackend is the first in-memory backend used by the domain
// harness before file or mmap persistence exists.
type MemoryStorageBackend struct {
	mu           sync.RWMutex
	lines        map[LogicalLineID]LogicalLine
	compactLines map[LogicalLineID]compactLogicalLine
}

func NewMemoryStorageBackend() *MemoryStorageBackend {
	return &MemoryStorageBackend{
		lines:        make(map[LogicalLineID]LogicalLine),
		compactLines: make(map[LogicalLineID]compactLogicalLine),
	}
}

func (backend *MemoryStorageBackend) SaveLine(line LogicalLine) error {
	line, err := normalizeLine(line)
	if err != nil {
		return err
	}
	return backend.saveNormalizedLine(line)
}

func (backend *MemoryStorageBackend) saveOwnedLine(line LogicalLine) error {
	line, err := normalizeOwnedLine(line)
	if err != nil {
		return err
	}
	return backend.saveNormalizedLine(line)
}

func (backend *MemoryStorageBackend) saveNormalizedLine(line LogicalLine) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if compactLogicalLineEligible(line) {
		backend.compactLines[line.ID] = compactLogicalLineFromLine(line)
		delete(backend.lines, line.ID)
		return nil
	}
	backend.lines[line.ID] = line
	delete(backend.compactLines, line.ID)
	return nil
}

func (backend *MemoryStorageBackend) LoadLine(id LogicalLineID) (LogicalLine, bool) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if line, ok := backend.lines[id]; ok {
		return line.Clone(), true
	}
	line, ok := backend.compactLines[id]
	return line.Line(), ok
}

func (backend *MemoryStorageBackend) LoadSnapshotLine(id LogicalLineID) (LogicalLine, bool) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if line, ok := backend.lines[id]; ok {
		return line, true
	}
	line, ok := backend.compactLines[id]
	return line.Line(), ok
}

func (backend *MemoryStorageBackend) DeleteLine(id LogicalLineID) bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if _, ok := backend.lines[id]; !ok {
		if _, ok := backend.compactLines[id]; !ok {
			return false
		}
	}
	delete(backend.lines, id)
	delete(backend.compactLines, id)
	return true
}

func (backend *MemoryStorageBackend) LineIDs() []LogicalLineID {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	ids := make([]LogicalLineID, 0, len(backend.lines))
	for id := range backend.lines {
		ids = append(ids, id)
	}
	for id := range backend.compactLines {
		ids = append(ids, id)
	}
	sortLogicalLineIDs(ids)
	return ids
}

// 中文说明：compact 只是 sealed clean logical line 的内存编码形态，
// 仍由 LogicalLineStore/StorageBackend 按 ID 管理，不引入第二份 history truth。
type compactLogicalLine struct {
	ID                LogicalLineID
	Generation        Generation
	CreatedGeneration Generation
	ContentGeneration Generation
	Seal              SealState
	TailFill          *RowTailFill
	Residency         Residency
	EncodedCells      []byte
}

func compactLogicalLineEligible(line LogicalLine) bool {
	if line.Seal != SealStateSealed || line.Dirty {
		return false
	}
	if len(line.Cells) == 1 && line.TailFill == nil {
		cell := line.Cells[0]
		if cellStyleZero(cell.Style) && cell.LinkURL == "" && cell.LinkParams == "" {
			return false
		}
	}
	return true
}

func compactLogicalLineFromLine(line LogicalLine) compactLogicalLine {
	compact := compactLogicalLine{
		ID:                line.ID,
		Generation:        line.Generation,
		CreatedGeneration: line.CreatedGeneration,
		ContentGeneration: line.ContentGeneration,
		Seal:              line.Seal,
		TailFill:          cloneRowTailFill(line.TailFill),
		Residency:         line.Residency,
		EncodedCells:      encodeCompactCells(line.Cells),
	}
	return compact
}

func (line compactLogicalLine) Line() LogicalLine {
	cells := decodeCompactCells(line.EncodedCells)
	return LogicalLine{
		ID:                line.ID,
		Generation:        line.Generation,
		CreatedGeneration: line.CreatedGeneration,
		ContentGeneration: line.ContentGeneration,
		Seal:              line.Seal,
		Cells:             cells,
		TailFill:          cloneRowTailFill(line.TailFill),
		Dirty:             false,
		Residency:         line.Residency,
	}
}

func cellStyleZero(style CellStyle) bool {
	return style == CellStyle{}
}

func encodeCompactCells(cells []Cell) []byte {
	out := make([]byte, 0, compactCellsEncodedCapacity(cells))
	out = appendCompactUvarint(out, uint64(len(cells)))
	for _, cell := range cells {
		out = appendCompactString(out, cell.Text)
		out = appendCompactUvarint(out, uint64(cell.Width))
		out = appendCompactCellStyle(out, cell.Style)
		out = appendCompactString(out, cell.LinkURL)
		out = appendCompactString(out, cell.LinkParams)
	}
	return out
}

func compactCellsEncodedCapacity(cells []Cell) int {
	capacity := 1
	for _, cell := range cells {
		capacity += len(cell.Text) + len(cell.LinkURL) + len(cell.LinkParams)
		capacity += len(cell.Style.FG) + len(cell.Style.BG)
		capacity += 10
	}
	return capacity
}

func appendCompactCellStyle(out []byte, style CellStyle) []byte {
	var flags uint64
	if style.Bold {
		flags |= 1 << 0
	}
	if style.Italic {
		flags |= 1 << 1
	}
	if style.Underline {
		flags |= 1 << 2
	}
	if style.Blink {
		flags |= 1 << 3
	}
	if style.Reverse {
		flags |= 1 << 4
	}
	if style.Strikethrough {
		flags |= 1 << 5
	}
	out = appendCompactUvarint(out, flags)
	out = appendCompactString(out, style.FG)
	out = appendCompactString(out, style.BG)
	return out
}

func decodeCompactCells(data []byte) []Cell {
	offset := 0
	count, ok := readCompactUvarint(data, &offset)
	if !ok || count == 0 {
		return nil
	}
	cells := make([]Cell, 0, int(count))
	for i := uint64(0); i < count; i++ {
		text, ok := readCompactString(data, &offset)
		if !ok {
			return cells
		}
		width, ok := readCompactUvarint(data, &offset)
		if !ok {
			return cells
		}
		style, ok := readCompactCellStyle(data, &offset)
		if !ok {
			return cells
		}
		linkURL, ok := readCompactString(data, &offset)
		if !ok {
			return cells
		}
		linkParams, ok := readCompactString(data, &offset)
		if !ok {
			return cells
		}
		cells = append(cells, Cell{
			Text:       text,
			Width:      int(width),
			Style:      style,
			LinkURL:    linkURL,
			LinkParams: linkParams,
		})
	}
	return cells
}

func readCompactCellStyle(data []byte, offset *int) (CellStyle, bool) {
	flags, ok := readCompactUvarint(data, offset)
	if !ok {
		return CellStyle{}, false
	}
	fg, ok := readCompactString(data, offset)
	if !ok {
		return CellStyle{}, false
	}
	bg, ok := readCompactString(data, offset)
	if !ok {
		return CellStyle{}, false
	}
	return CellStyle{
		FG:            fg,
		BG:            bg,
		Bold:          flags&(1<<0) != 0,
		Italic:        flags&(1<<1) != 0,
		Underline:     flags&(1<<2) != 0,
		Blink:         flags&(1<<3) != 0,
		Reverse:       flags&(1<<4) != 0,
		Strikethrough: flags&(1<<5) != 0,
	}, true
}

func appendCompactString(out []byte, value string) []byte {
	out = appendCompactUvarint(out, uint64(len(value)))
	return append(out, value...)
}

func readCompactString(data []byte, offset *int) (string, bool) {
	size, ok := readCompactUvarint(data, offset)
	if !ok {
		return "", false
	}
	end := *offset + int(size)
	if end < *offset || end > len(data) {
		return "", false
	}
	value := string(data[*offset:end])
	*offset = end
	return value, true
}

func appendCompactUvarint(out []byte, value uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], value)
	return append(out, buf[:n]...)
}

func readCompactUvarint(data []byte, offset *int) (uint64, bool) {
	if *offset < 0 || *offset >= len(data) {
		return 0, false
	}
	value, n := binary.Uvarint(data[*offset:])
	if n <= 0 {
		return 0, false
	}
	*offset += n
	return value, true
}

func sortLogicalLineIDs(ids []LogicalLineID) {
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
}
