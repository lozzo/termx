package history

import (
	"encoding/binary"
	"sort"
	"strconv"
	"strings"
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

type lineExistsBackend interface {
	HasLine(LogicalLineID) bool
}

type ownedLineBackend interface {
	saveOwnedLine(LogicalLine) error
}

// MemoryStorageBackend is the first in-memory backend used by the domain
// harness before file or mmap persistence exists.
type MemoryStorageBackend struct {
	mu               sync.RWMutex
	lines            map[LogicalLineID]LogicalLine
	compactLines     [][]byte
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
		backend.saveCompactLogicalLine(line)
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
	return backend.loadCompactLine(id)
}

func (backend *MemoryStorageBackend) LoadSnapshotLine(id LogicalLineID) (LogicalLine, bool) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if line, ok := backend.lines[id]; ok {
		return line, true
	}
	return backend.loadCompactLine(id)
}

func (backend *MemoryStorageBackend) HasLine(id LogicalLineID) bool {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	if _, ok := backend.lines[id]; ok {
		return true
	}
	return backend.hasCompactLine(id)
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
	for index, encodedLine := range backend.compactLines {
		if len(encodedLine) > 0 {
			ids = append(ids, LogicalLineID(index+1))
		}
	}
	for id := range backend.compactSparse {
		ids = append(ids, id)
	}
	sortLogicalLineIDs(ids)
	return ids
}

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

const (
	compactLineHeaderCreatedDifferent uint64 = 1 << iota
	compactLineHeaderContentDifferent
	compactLineHeaderResidencyDifferent
	compactLineHeaderTailFill
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
	compact := compactLogicalLineMetadataFromLine(line)
	compact.EncodedCells = encodeCompactCells(line.Cells)
	return compact
}

func compactLogicalLineMetadataFromLine(line LogicalLine) compactLogicalLine {
	return compactLogicalLine{
		ID:                line.ID,
		Generation:        line.Generation,
		CreatedGeneration: line.CreatedGeneration,
		ContentGeneration: line.ContentGeneration,
		TailFill:          cloneRowTailFill(line.TailFill),
		Residency:         compactResidencyFrom(line.Residency),
	}
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

func decodeCompactLine(id LogicalLineID, data []byte) (LogicalLine, bool) {
	line, _, ok := decodeCompactLineParts(id, data)
	return line, ok
}

func decodeCompactLineParts(id LogicalLineID, data []byte) (LogicalLine, []byte, bool) {
	offset := 0
	flags, ok := readCompactUvarint(data, &offset)
	if !ok {
		return LogicalLine{}, nil, false
	}
	generation, ok := readCompactUvarint(data, &offset)
	if !ok {
		return LogicalLine{}, nil, false
	}
	createdGeneration := generation
	if flags&compactLineHeaderCreatedDifferent != 0 {
		createdGeneration, ok = readCompactUvarint(data, &offset)
		if !ok {
			return LogicalLine{}, nil, false
		}
	}
	contentGeneration := generation
	if flags&compactLineHeaderContentDifferent != 0 {
		contentGeneration, ok = readCompactUvarint(data, &offset)
		if !ok {
			return LogicalLine{}, nil, false
		}
	}
	residency := uint64(compactResidencyMemory)
	if flags&compactLineHeaderResidencyDifferent != 0 {
		residency, ok = readCompactUvarint(data, &offset)
		if !ok {
			return LogicalLine{}, nil, false
		}
	}
	var tailFill *RowTailFill
	if flags&compactLineHeaderTailFill != 0 {
		style, ok := readCompactCellStyle(data, &offset)
		if !ok {
			return LogicalLine{}, nil, false
		}
		tailFill = &RowTailFill{Style: style}
	}
	encodedCells := data[offset:]
	return LogicalLine{
		ID:                id,
		Generation:        Generation(generation),
		CreatedGeneration: Generation(createdGeneration),
		ContentGeneration: Generation(contentGeneration),
		Seal:              SealStateSealed,
		Cells:             decodeCompactCells(encodedCells),
		TailFill:          tailFill,
		Dirty:             false,
		Residency:         compactResidency(residency).Residency(),
	}, encodedCells, true
}

func compactLogicalLineFromEncodedLine(id LogicalLineID, data []byte) (compactLogicalLine, bool) {
	line, encodedCells, ok := decodeCompactLineParts(id, data)
	if !ok {
		return compactLogicalLine{}, false
	}
	return compactLogicalLine{
		ID:                line.ID,
		Generation:        line.Generation,
		CreatedGeneration: line.CreatedGeneration,
		ContentGeneration: line.ContentGeneration,
		TailFill:          cloneRowTailFill(line.TailFill),
		Residency:         compactResidencyFrom(line.Residency),
		EncodedCells:      encodedCells,
	}, true
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

func (backend *MemoryStorageBackend) saveCompactLogicalLine(line LogicalLine) {
	if slot, ok := backend.compactLineSlot(line.ID); ok {
		if len(*slot) == 0 {
			backend.compactLineCount++
		}
		*slot = encodeCompactLogicalLineFromLine(line)
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
	backend.compactSparse[line.ID] = compactLogicalLineFromLine(line)
}

func (backend *MemoryStorageBackend) saveCompactLine(line compactLogicalLine) {
	if slot, ok := backend.compactLineSlot(line.ID); ok {
		if len(*slot) == 0 {
			backend.compactLineCount++
		}
		*slot = encodeCompactLogicalLine(line)
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
		encodedLine := backend.compactLines[index]
		if len(encodedLine) > 0 {
			return compactLogicalLineFromEncodedLine(id, encodedLine)
		}
	}
	if backend.compactSparse == nil {
		return compactLogicalLine{}, false
	}
	line, ok := backend.compactSparse[id]
	return line, ok
}

func (backend *MemoryStorageBackend) hasCompactLine(id LogicalLineID) bool {
	if id == 0 {
		return false
	}
	index := compactDenseIndex(id)
	if index >= 0 && index < len(backend.compactLines) {
		return len(backend.compactLines[index]) > 0
	}
	if backend.compactSparse == nil {
		return false
	}
	_, ok := backend.compactSparse[id]
	return ok
}

func (backend *MemoryStorageBackend) deleteCompactLine(id LogicalLineID) bool {
	index := compactDenseIndex(id)
	if index >= 0 && index < len(backend.compactLines) {
		if len(backend.compactLines[index]) > 0 {
			backend.compactLines[index] = nil
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

func (backend *MemoryStorageBackend) compactLineSlot(id LogicalLineID) (*[]byte, bool) {
	index := compactDenseIndex(id)
	if index < 0 {
		return nil, false
	}
	if index >= len(backend.compactLines) {
		gap := index - len(backend.compactLines)
		if gap > maxCompactDenseGap {
			return nil, false
		}
		backend.compactLines = append(backend.compactLines, make([][]byte, index-len(backend.compactLines)+1)...)
	}
	return &backend.compactLines[index], true
}

func (backend *MemoryStorageBackend) loadCompactLine(id LogicalLineID) (LogicalLine, bool) {
	if id == 0 {
		return LogicalLine{}, false
	}
	index := compactDenseIndex(id)
	if index >= 0 && index < len(backend.compactLines) {
		encodedLine := backend.compactLines[index]
		if len(encodedLine) > 0 {
			return decodeCompactLine(id, encodedLine)
		}
	}
	if backend.compactSparse == nil {
		return LogicalLine{}, false
	}
	line, ok := backend.compactSparse[id]
	return line.Line(), ok
}

func encodeCompactLogicalLine(line compactLogicalLine) []byte {
	flags := compactLogicalLineHeaderFlags(line)
	out := make([]byte, 0, compactLogicalLineEncodedCapacity(line, flags))
	return appendCompactLogicalLine(out, line, flags)
}

func encodeCompactLogicalLineFromLine(line LogicalLine) []byte {
	compact := compactLogicalLineMetadataFromLine(line)
	flags := compactLogicalLineHeaderFlags(compact)
	out := make([]byte, 0, compactLogicalLineHeaderEncodedCapacity(compact, flags)+compactCellsEncodedCapacity(line.Cells))
	out = appendCompactLogicalLineHeader(out, compact, flags)
	return appendCompactCells(out, line.Cells)
}

func appendCompactLogicalLine(out []byte, line compactLogicalLine, flags uint64) []byte {
	out = appendCompactLogicalLineHeader(out, line, flags)
	return append(out, line.EncodedCells...)
}

func appendCompactLogicalLineHeader(out []byte, line compactLogicalLine, flags uint64) []byte {
	// 中文说明：header 只写非默认元数据；ID 由 dense slot 持有，cells 仍是唯一历史 payload。
	out = appendCompactUvarint(out, flags)
	out = appendCompactUvarint(out, uint64(line.Generation))
	if flags&compactLineHeaderCreatedDifferent != 0 {
		out = appendCompactUvarint(out, uint64(line.CreatedGeneration))
	}
	if flags&compactLineHeaderContentDifferent != 0 {
		out = appendCompactUvarint(out, uint64(line.ContentGeneration))
	}
	if flags&compactLineHeaderResidencyDifferent != 0 {
		out = appendCompactUvarint(out, uint64(line.Residency))
	}
	if flags&compactLineHeaderTailFill != 0 {
		out = appendCompactCellStyle(out, line.TailFill.Style)
	}
	return out
}

func compactLogicalLineHeaderFlags(line compactLogicalLine) uint64 {
	var flags uint64
	if line.CreatedGeneration != line.Generation {
		flags |= compactLineHeaderCreatedDifferent
	}
	if line.ContentGeneration != line.Generation {
		flags |= compactLineHeaderContentDifferent
	}
	if line.Residency != compactResidencyMemory {
		flags |= compactLineHeaderResidencyDifferent
	}
	if line.TailFill != nil {
		flags |= compactLineHeaderTailFill
	}
	return flags
}

func compactLogicalLineEncodedCapacity(line compactLogicalLine, flags uint64) int {
	capacity := compactLogicalLineHeaderEncodedCapacity(line, flags)
	capacity += len(line.EncodedCells)
	return capacity
}

func compactLogicalLineHeaderEncodedCapacity(line compactLogicalLine, flags uint64) int {
	capacity := compactUvarintSize(flags)
	capacity += compactUvarintSize(uint64(line.Generation))
	if flags&compactLineHeaderCreatedDifferent != 0 {
		capacity += compactUvarintSize(uint64(line.CreatedGeneration))
	}
	if flags&compactLineHeaderContentDifferent != 0 {
		capacity += compactUvarintSize(uint64(line.ContentGeneration))
	}
	if flags&compactLineHeaderResidencyDifferent != 0 {
		capacity += compactUvarintSize(uint64(line.Residency))
	}
	if flags&compactLineHeaderTailFill != 0 {
		capacity += compactCellStyleEncodedSize(line.TailFill.Style)
		capacity += len(line.TailFill.Style.FG) + len(line.TailFill.Style.BG)
	}
	return capacity
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
	return appendCompactCells(out, cells)
}

const (
	compactCellsRunEncodingMarker uint64 = 0
	compactCellsRunEncodingV1     uint64 = 1
)

const (
	compactCellRunWidthsExplicit uint64 = 1 << iota
	compactCellRunLinkURL
	compactCellRunLinkParams
)

func appendCompactCells(out []byte, cells []Cell) []byte {
	out = appendCompactUvarint(out, compactCellsRunEncodingMarker)
	out = appendCompactUvarint(out, compactCellsRunEncodingV1)
	out = appendCompactUvarint(out, uint64(len(cells)))
	for start := 0; start < len(cells); {
		end, flags := compactCellRunEnd(cells, start)
		first := cells[start]
		out = appendCompactUvarint(out, uint64(end-start))
		out = appendCompactUvarint(out, flags)
		out = appendCompactCellStyleV1(out, first.Style)
		if flags&compactCellRunLinkURL != 0 {
			out = appendCompactString(out, first.LinkURL)
		}
		if flags&compactCellRunLinkParams != 0 {
			out = appendCompactString(out, first.LinkParams)
		}
		for i := start; i < end; i++ {
			out = appendCompactString(out, cells[i].Text)
			if flags&compactCellRunWidthsExplicit != 0 {
				out = appendCompactUvarint(out, uint64(cells[i].Width))
			}
		}
		start = end
	}
	return out
}

func compactCellsEncodedCapacity(cells []Cell) int {
	capacity := compactUvarintSize(compactCellsRunEncodingMarker)
	capacity += compactUvarintSize(compactCellsRunEncodingV1)
	capacity += compactUvarintSize(uint64(len(cells)))
	for start := 0; start < len(cells); {
		end, flags := compactCellRunEnd(cells, start)
		first := cells[start]
		capacity += compactUvarintSize(uint64(end - start))
		capacity += compactUvarintSize(flags)
		capacity += compactCellStyleV1EncodedSize(first.Style)
		if flags&compactCellRunLinkURL != 0 {
			capacity += compactStringEncodedSize(first.LinkURL)
		}
		if flags&compactCellRunLinkParams != 0 {
			capacity += compactStringEncodedSize(first.LinkParams)
		}
		for i := start; i < end; i++ {
			capacity += compactStringEncodedSize(cells[i].Text)
			if flags&compactCellRunWidthsExplicit != 0 {
				capacity += compactUvarintSize(uint64(cells[i].Width))
			}
		}
		start = end
	}
	return capacity
}

func compactCellRunEnd(cells []Cell, start int) (int, uint64) {
	first := cells[start]
	widthsExplicit := !compactCellWidthInferred(first)
	end := start + 1
	for end < len(cells) {
		next := cells[end]
		if next.Style != first.Style || next.LinkURL != first.LinkURL || next.LinkParams != first.LinkParams {
			break
		}
		if !compactCellWidthInferred(next) != widthsExplicit {
			break
		}
		end++
	}
	var flags uint64
	if widthsExplicit {
		flags |= compactCellRunWidthsExplicit
	}
	if first.LinkURL != "" {
		flags |= compactCellRunLinkURL
	}
	if first.LinkParams != "" {
		flags |= compactCellRunLinkParams
	}
	return end, flags
}

func compactCellWidthInferred(cell Cell) bool {
	width, ok := compactASCIITextWidth(cell.Text)
	return ok && cell.Width == width
}

func compactASCIITextWidth(text string) (int, bool) {
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] >= 0x7f {
			return 0, false
		}
	}
	return len(text), true
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
	out = appendCompactUvarint(out, compactCellStyleAttrFlags(style))
	out = appendCompactString(out, style.FG)
	out = appendCompactString(out, style.BG)
	return out
}

func compactCellStyleAttrFlags(style CellStyle) uint64 {
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
	return flags
}

func appendCompactCellStyleV1(out []byte, style CellStyle) []byte {
	out = appendCompactUvarint(out, compactCellStyleAttrFlags(style))
	out = appendCompactColor(out, style.FG)
	out = appendCompactColor(out, style.BG)
	return out
}

func compactCellStyleV1EncodedSize(style CellStyle) int {
	return compactUvarintSize(compactCellStyleAttrFlags(style)) +
		compactColorEncodedSize(style.FG) +
		compactColorEncodedSize(style.BG)
}

func decodeCompactCells(data []byte) []Cell {
	offset := 0
	marker, ok := readCompactUvarint(data, &offset)
	if !ok || marker != compactCellsRunEncodingMarker {
		return nil
	}
	return decodeCompactCellsV1(data, &offset)
}

func decodeCompactCellsV1(data []byte, offset *int) []Cell {
	version, ok := readCompactUvarint(data, offset)
	if !ok || version != compactCellsRunEncodingV1 {
		return nil
	}
	count, ok := readCompactUvarint(data, offset)
	if !ok || count > uint64(maxInt()) {
		return nil
	}
	if count == 0 {
		return []Cell{}
	}
	cells := make([]Cell, 0, int(count))
	for uint64(len(cells)) < count {
		runLen, ok := readCompactUvarint(data, offset)
		if !ok || runLen == 0 || runLen > count-uint64(len(cells)) {
			return cells
		}
		flags, ok := readCompactUvarint(data, offset)
		if !ok {
			return cells
		}
		style, ok := readCompactCellStyleV1(data, offset)
		if !ok {
			return cells
		}
		linkURL := ""
		if flags&compactCellRunLinkURL != 0 {
			linkURL, ok = readCompactString(data, offset)
			if !ok {
				return cells
			}
		}
		linkParams := ""
		if flags&compactCellRunLinkParams != 0 {
			linkParams, ok = readCompactString(data, offset)
			if !ok {
				return cells
			}
		}
		for i := uint64(0); i < runLen; i++ {
			text, ok := readCompactString(data, offset)
			if !ok {
				return cells
			}
			width := len(text)
			if flags&compactCellRunWidthsExplicit != 0 {
				rawWidth, ok := readCompactUvarint(data, offset)
				if !ok {
					return cells
				}
				width = int(rawWidth)
			}
			cells = append(cells, Cell{
				Text:       text,
				Width:      width,
				Style:      style,
				LinkURL:    linkURL,
				LinkParams: linkParams,
			})
		}
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

func readCompactCellStyleV1(data []byte, offset *int) (CellStyle, bool) {
	flags, ok := readCompactUvarint(data, offset)
	if !ok {
		return CellStyle{}, false
	}
	fg, ok := readCompactColor(data, offset)
	if !ok {
		return CellStyle{}, false
	}
	bg, ok := readCompactColor(data, offset)
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

type compactColorKind uint64

const (
	compactColorNone compactColorKind = iota
	compactColorANSI
	compactColorIndex
	compactColorRGB
	compactColorLiteral
)

type compactColorEncoding struct {
	kind    compactColorKind
	number  uint64
	rgb     [3]byte
	literal string
}

func compactColorEncodingFor(value string) compactColorEncoding {
	if value == "" {
		return compactColorEncoding{kind: compactColorNone}
	}
	if number, ok := compactCanonicalDecimalSuffix(value, "ansi:"); ok {
		return compactColorEncoding{kind: compactColorANSI, number: number}
	}
	if number, ok := compactCanonicalDecimalSuffix(value, "idx:"); ok {
		return compactColorEncoding{kind: compactColorIndex, number: number}
	}
	if rgb, ok := compactCanonicalRGB(value); ok {
		return compactColorEncoding{kind: compactColorRGB, rgb: rgb}
	}
	return compactColorEncoding{kind: compactColorLiteral, literal: value}
}

func appendCompactColor(out []byte, value string) []byte {
	encoded := compactColorEncodingFor(value)
	out = appendCompactUvarint(out, uint64(encoded.kind))
	switch encoded.kind {
	case compactColorANSI, compactColorIndex:
		out = appendCompactUvarint(out, encoded.number)
	case compactColorRGB:
		out = append(out, encoded.rgb[:]...)
	case compactColorLiteral:
		out = appendCompactString(out, encoded.literal)
	}
	return out
}

func compactColorEncodedSize(value string) int {
	encoded := compactColorEncodingFor(value)
	size := compactUvarintSize(uint64(encoded.kind))
	switch encoded.kind {
	case compactColorANSI, compactColorIndex:
		size += compactUvarintSize(encoded.number)
	case compactColorRGB:
		size += len(encoded.rgb)
	case compactColorLiteral:
		size += compactStringEncodedSize(encoded.literal)
	}
	return size
}

func readCompactColor(data []byte, offset *int) (string, bool) {
	rawKind, ok := readCompactUvarint(data, offset)
	if !ok {
		return "", false
	}
	switch compactColorKind(rawKind) {
	case compactColorNone:
		return "", true
	case compactColorANSI:
		number, ok := readCompactUvarint(data, offset)
		if !ok {
			return "", false
		}
		return "ansi:" + strconv.FormatUint(number, 10), true
	case compactColorIndex:
		number, ok := readCompactUvarint(data, offset)
		if !ok {
			return "", false
		}
		return "idx:" + strconv.FormatUint(number, 10), true
	case compactColorRGB:
		if *offset+3 > len(data) {
			return "", false
		}
		rgb := data[*offset : *offset+3]
		*offset += 3
		return "#" + compactHexByte(rgb[0]) + compactHexByte(rgb[1]) + compactHexByte(rgb[2]), true
	case compactColorLiteral:
		return readCompactString(data, offset)
	default:
		return "", false
	}
}

func compactCanonicalDecimalSuffix(value string, prefix string) (uint64, bool) {
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}
	digits := value[len(prefix):]
	if digits == "" || (len(digits) > 1 && digits[0] == '0') {
		return 0, false
	}
	number, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	if strconv.FormatUint(number, 10) != digits {
		return 0, false
	}
	return number, true
}

func compactCanonicalRGB(value string) ([3]byte, bool) {
	var rgb [3]byte
	if len(value) != 7 || value[0] != '#' {
		return rgb, false
	}
	for i := 0; i < 3; i++ {
		high, ok := compactHexNibble(value[1+i*2])
		if !ok {
			return rgb, false
		}
		low, ok := compactHexNibble(value[2+i*2])
		if !ok {
			return rgb, false
		}
		rgb[i] = high<<4 | low
	}
	if "#"+compactHexByte(rgb[0])+compactHexByte(rgb[1])+compactHexByte(rgb[2]) != value {
		return rgb, false
	}
	return rgb, true
}

func compactHexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	default:
		return 0, false
	}
}

func compactHexByte(value byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[value>>4], digits[value&0x0f]})
}

func appendCompactString(out []byte, value string) []byte {
	out = appendCompactUvarint(out, uint64(len(value)))
	return append(out, value...)
}

func compactStringEncodedSize(value string) int {
	return compactUvarintSize(uint64(len(value))) + len(value)
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

func maxInt() int {
	return int(^uint(0) >> 1)
}
