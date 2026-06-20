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
	mu               sync.RWMutex
	lines            map[LogicalLineID]LogicalLine
	compactLines     []compactLogicalLine
	compactSparse    map[LogicalLineID]compactLogicalLine
	compactLineCount int
}

func NewMemoryStorageBackend() *MemoryStorageBackend {
	return &MemoryStorageBackend{
		lines: make(map[LogicalLineID]LogicalLine),
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
		backend.saveCompactLine(compactLogicalLineFromLine(line))
		delete(backend.lines, line.ID)
		return nil
	}
	backend.lines[line.ID] = line
	backend.deleteCompactLine(line.ID)
	return nil
}

func (backend *MemoryStorageBackend) LoadLine(id LogicalLineID) (LogicalLine, bool) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if line, ok := backend.lines[id]; ok {
		return line.Clone(), true
	}
	line, ok := backend.compactLine(id)
	return line.Line(), ok
}

func (backend *MemoryStorageBackend) LoadSnapshotLine(id LogicalLineID) (LogicalLine, bool) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if line, ok := backend.lines[id]; ok {
		return line, true
	}
	line, ok := backend.compactLine(id)
	return line.Line(), ok
}

func (backend *MemoryStorageBackend) DeleteLine(id LogicalLineID) bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	_, lineOK := backend.lines[id]
	if lineOK {
		delete(backend.lines, id)
	}
	compactOK := backend.deleteCompactLine(id)
	return lineOK || compactOK
}

func (backend *MemoryStorageBackend) LineIDs() []LogicalLineID {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	ids := make([]LogicalLineID, 0, len(backend.lines)+backend.compactLineCount)
	for id := range backend.lines {
		ids = append(ids, id)
	}
	for _, line := range backend.compactLines {
		if line.ID != 0 {
			ids = append(ids, line.ID)
		}
	}
	for id := range backend.compactSparse {
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
	TailFill          *RowTailFill
	Residency         compactResidency
	EncodedCells      []byte
}

type compactResidency uint8

const (
	compactResidencyMemory compactResidency = iota
	compactResidencyFile
	compactResidencyMmap
	compactResidencyEvicted
)

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
		TailFill:          cloneRowTailFill(line.TailFill),
		Residency:         compactResidencyFrom(line.Residency),
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
		Seal:              SealStateSealed,
		Cells:             cells,
		TailFill:          cloneRowTailFill(line.TailFill),
		Dirty:             false,
		Residency:         line.Residency.Residency(),
	}
}

func compactResidencyFrom(residency Residency) compactResidency {
	switch residency {
	case ResidencyFile:
		return compactResidencyFile
	case ResidencyMmap:
		return compactResidencyMmap
	case ResidencyEvicted:
		return compactResidencyEvicted
	default:
		return compactResidencyMemory
	}
}

func (residency compactResidency) Residency() Residency {
	switch residency {
	case compactResidencyFile:
		return ResidencyFile
	case compactResidencyMmap:
		return ResidencyMmap
	case compactResidencyEvicted:
		return ResidencyEvicted
	default:
		return ResidencyMemory
	}
}

const maxCompactDenseGap = 4096

func (backend *MemoryStorageBackend) saveCompactLine(line compactLogicalLine) {
	if slot, ok := backend.compactLineSlot(line.ID); ok {
		if slot.ID == 0 {
			backend.compactLineCount++
		}
		*slot = line
		if backend.compactSparse != nil {
			delete(backend.compactSparse, line.ID)
		}
		return
	}
	if backend.compactSparse == nil {
		backend.compactSparse = make(map[LogicalLineID]compactLogicalLine)
	}
	if _, ok := backend.compactSparse[line.ID]; !ok {
		backend.compactLineCount++
	}
	backend.compactSparse[line.ID] = line
}

func (backend *MemoryStorageBackend) compactLine(id LogicalLineID) (compactLogicalLine, bool) {
	if id == 0 {
		return compactLogicalLine{}, false
	}
	index := compactDenseIndex(id)
	if index >= 0 && index < len(backend.compactLines) {
		line := backend.compactLines[index]
		if line.ID == id {
			return line, true
		}
	}
	if backend.compactSparse == nil {
		return compactLogicalLine{}, false
	}
	line, ok := backend.compactSparse[id]
	return line, ok
}

func (backend *MemoryStorageBackend) deleteCompactLine(id LogicalLineID) bool {
	index := compactDenseIndex(id)
	if index >= 0 && index < len(backend.compactLines) {
		if backend.compactLines[index].ID == id {
			backend.compactLines[index] = compactLogicalLine{}
			backend.compactLineCount--
			return true
		}
	}
	if backend.compactSparse == nil {
		return false
	}
	if _, ok := backend.compactSparse[id]; !ok {
		return false
	}
	delete(backend.compactSparse, id)
	backend.compactLineCount--
	if len(backend.compactSparse) == 0 {
		backend.compactSparse = nil
	}
	return true
}

func (backend *MemoryStorageBackend) compactLineSlot(id LogicalLineID) (*compactLogicalLine, bool) {
	index := compactDenseIndex(id)
	if index < 0 {
		return nil, false
	}
	if index >= len(backend.compactLines) {
		gap := index - len(backend.compactLines)
		if gap > maxCompactDenseGap {
			return nil, false
		}
		backend.compactLines = append(backend.compactLines, make([]compactLogicalLine, index-len(backend.compactLines)+1)...)
	}
	return &backend.compactLines[index], true
}

func compactDenseIndex(id LogicalLineID) int {
	if id == 0 {
		return -1
	}
	index := uint64(id - 1)
	if index > uint64(int(^uint(0)>>1)) {
		return -1
	}
	return int(index)
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
	capacity := compactUvarintSize(uint64(len(cells)))
	for _, cell := range cells {
		capacity += len(cell.Text) + len(cell.LinkURL) + len(cell.LinkParams)
		capacity += len(cell.Style.FG) + len(cell.Style.BG)
		capacity += compactUvarintSize(uint64(len(cell.Text)))
		capacity += compactUvarintSize(uint64(cell.Width))
		capacity += compactCellStyleEncodedSize(cell.Style)
		capacity += compactUvarintSize(uint64(len(cell.LinkURL)))
		capacity += compactUvarintSize(uint64(len(cell.LinkParams)))
	}
	return capacity
}

func compactCellStyleEncodedSize(style CellStyle) int {
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
	return compactUvarintSize(flags) +
		compactUvarintSize(uint64(len(style.FG))) +
		compactUvarintSize(uint64(len(style.BG)))
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

func compactUvarintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
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
