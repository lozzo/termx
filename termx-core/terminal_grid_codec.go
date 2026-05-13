package termx

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lozzow/termx/termx-core/vterm"
)

const (
	terminalGridRowMagic0 byte = 'T'
	terminalGridRowMagic1 byte = 'X'
	terminalGridRowMagic2 byte = 'G'
	terminalGridRowMagic3 byte = 'R'

	terminalGridRowCodecVersion byte = 1

	terminalGridRowFlagTimestamp uint16 = 1 << 0
	terminalGridRowFlagRowKind   uint16 = 1 << 1

	terminalGridRunFlagStyle uint8 = 1 << 0
	terminalGridRunFlagASCII uint8 = 1 << 1

	terminalGridCellFlagContent byte = 1 << 0
	terminalGridCellFlagWidth   byte = 1 << 1

	terminalGridStyleFlagFG            uint16 = 1 << 0
	terminalGridStyleFlagBG            uint16 = 1 << 1
	terminalGridStyleFlagBold          uint16 = 1 << 2
	terminalGridStyleFlagItalic        uint16 = 1 << 3
	terminalGridStyleFlagUnderline     uint16 = 1 << 4
	terminalGridStyleFlagBlink         uint16 = 1 << 5
	terminalGridStyleFlagReverse       uint16 = 1 << 6
	terminalGridStyleFlagStrikethrough uint16 = 1 << 7
)

func encodeTerminalGridRow(row terminalGridRow) ([]byte, error) {
	cells := trimTerminalGridCells(row.cells)
	runs := terminalGridRuns(cells)
	size := 8
	if !row.timestamp.IsZero() {
		size += binary.MaxVarintLen64
	}
	rowKind := strings.TrimSpace(row.rowKind)
	if rowKind != "" {
		size += binary.MaxVarintLen64 + len(rowKind)
	}
	for _, run := range runs {
		size += terminalGridEncodedRunSize(run)
	}

	out := make([]byte, 0, size)
	out = append(out, terminalGridRowMagic0, terminalGridRowMagic1, terminalGridRowMagic2, terminalGridRowMagic3)
	out = append(out, terminalGridRowCodecVersion)
	var rowFlags uint16
	if !row.timestamp.IsZero() {
		rowFlags |= terminalGridRowFlagTimestamp
	}
	if rowKind != "" {
		rowFlags |= terminalGridRowFlagRowKind
	}
	out = appendUvarint(out, uint64(rowFlags))
	if rowFlags&terminalGridRowFlagTimestamp != 0 {
		out = appendVarint(out, row.timestamp.UTC().UnixNano())
	}
	if rowFlags&terminalGridRowFlagRowKind != 0 {
		out = appendString(out, rowKind)
	}
	out = appendUvarint(out, uint64(len(runs)))
	for _, run := range runs {
		out = appendTerminalGridRun(out, run)
	}
	return out, nil
}

func decodeTerminalGridRow(data []byte) (terminalGridRow, error) {
	reader := terminalGridReader{data: data}
	if len(data) < 5 ||
		data[0] != terminalGridRowMagic0 ||
		data[1] != terminalGridRowMagic1 ||
		data[2] != terminalGridRowMagic2 ||
		data[3] != terminalGridRowMagic3 {
		return terminalGridRow{}, fmt.Errorf("invalid terminal grid row magic")
	}
	reader.pos = 4
	version, err := reader.readByte()
	if err != nil {
		return terminalGridRow{}, err
	}
	if version != terminalGridRowCodecVersion {
		return terminalGridRow{}, fmt.Errorf("unsupported terminal grid row codec version %d", version)
	}
	rowFlags64, err := reader.readUvarint()
	if err != nil {
		return terminalGridRow{}, err
	}
	if rowFlags64 > uint64(^uint16(0)) {
		return terminalGridRow{}, fmt.Errorf("terminal grid row flags out of range: %d", rowFlags64)
	}
	rowFlags := uint16(rowFlags64)
	row := terminalGridRow{}
	if rowFlags&terminalGridRowFlagTimestamp != 0 {
		nanos, err := reader.readVarint()
		if err != nil {
			return terminalGridRow{}, err
		}
		if nanos != 0 {
			row.timestamp = time.Unix(0, nanos).UTC()
		}
	}
	if rowFlags&terminalGridRowFlagRowKind != 0 {
		rowKind, err := reader.readString()
		if err != nil {
			return terminalGridRow{}, err
		}
		row.rowKind = rowKind
	}
	runCount64, err := reader.readUvarint()
	if err != nil {
		return terminalGridRow{}, err
	}
	if runCount64 > uint64(^uint32(0)) {
		return terminalGridRow{}, fmt.Errorf("terminal grid row run count out of range: %d", runCount64)
	}
	for i := 0; i < int(runCount64); i++ {
		cells, err := reader.readRun()
		if err != nil {
			return terminalGridRow{}, err
		}
		row.cells = append(row.cells, cells...)
	}
	if reader.pos != len(reader.data) {
		return terminalGridRow{}, fmt.Errorf("terminal grid row has %d trailing bytes", len(reader.data)-reader.pos)
	}
	return row, nil
}

func trimTerminalGridCells(cells []vterm.Cell) []vterm.Cell {
	last := len(cells)
	for last > 0 {
		cell := cells[last-1]
		if cell.Content != "" && strings.TrimSpace(cell.Content) != "" {
			break
		}
		if cell.Style != (vterm.CellStyle{}) || cell.Width > 1 {
			break
		}
		last--
	}
	return cells[:last]
}

type terminalGridRun struct {
	style vterm.CellStyle
	ascii bool
	cells []vterm.Cell
	text  string
}

func terminalGridRuns(cells []vterm.Cell) []terminalGridRun {
	if len(cells) == 0 {
		return nil
	}
	runs := make([]terminalGridRun, 0, 4)
	for i := 0; i < len(cells); {
		style := cells[i].Style
		ascii := terminalGridASCIICompactCell(cells[i])
		start := i
		i++
		for i < len(cells) && cells[i].Style == style && terminalGridASCIICompactCell(cells[i]) == ascii {
			i++
		}
		runCells := cells[start:i]
		run := terminalGridRun{style: style, ascii: ascii, cells: runCells}
		if ascii {
			var b strings.Builder
			b.Grow(len(runCells))
			for _, cell := range runCells {
				b.WriteString(cell.Content)
			}
			run.text = b.String()
		}
		runs = append(runs, run)
	}
	return runs
}

func terminalGridASCIICompactCell(cell vterm.Cell) bool {
	return cell.Width == 1 && len(cell.Content) == 1 && cell.Content[0] < utf8.RuneSelf
}

func terminalGridEncodedRunSize(run terminalGridRun) int {
	size := 1
	if run.style != (vterm.CellStyle{}) {
		size += terminalGridEncodedStyleSize(run.style)
	}
	if run.ascii {
		return size + binary.MaxVarintLen64 + len(run.text)
	}
	size += binary.MaxVarintLen64
	for _, cell := range run.cells {
		size += 1
		if cell.Content != " " || cell.Width == 0 {
			size += binary.MaxVarintLen64 + len(cell.Content)
		}
		if cell.Width != 1 {
			size += binary.MaxVarintLen64
		}
	}
	return size
}

func appendTerminalGridRun(out []byte, run terminalGridRun) []byte {
	flags := uint8(0)
	if run.style != (vterm.CellStyle{}) {
		flags |= terminalGridRunFlagStyle
	}
	if run.ascii {
		flags |= terminalGridRunFlagASCII
	}
	out = append(out, flags)
	if flags&terminalGridRunFlagStyle != 0 {
		out = appendTerminalGridStyle(out, run.style)
	}
	if flags&terminalGridRunFlagASCII != 0 {
		return appendString(out, run.text)
	}
	out = appendUvarint(out, uint64(len(run.cells)))
	for _, cell := range run.cells {
		out = appendTerminalGridCell(out, cell)
	}
	return out
}

func appendTerminalGridCell(out []byte, cell vterm.Cell) []byte {
	flags := byte(0)
	content := cell.Content
	if content != " " || cell.Width == 0 {
		flags |= terminalGridCellFlagContent
	}
	if cell.Width != 1 {
		flags |= terminalGridCellFlagWidth
	}
	out = append(out, flags)
	if flags&terminalGridCellFlagContent != 0 {
		out = appendString(out, content)
	}
	if flags&terminalGridCellFlagWidth != 0 {
		out = appendVarint(out, int64(cell.Width))
	}
	return out
}

func appendTerminalGridStyle(out []byte, style vterm.CellStyle) []byte {
	flags := terminalGridStyleFlags(style)
	out = appendUvarint(out, uint64(flags))
	if flags&terminalGridStyleFlagFG != 0 {
		out = appendString(out, style.FG)
	}
	if flags&terminalGridStyleFlagBG != 0 {
		out = appendString(out, style.BG)
	}
	return out
}

func terminalGridEncodedStyleSize(style vterm.CellStyle) int {
	size := binary.MaxVarintLen64
	if style.FG != "" {
		size += binary.MaxVarintLen64 + len(style.FG)
	}
	if style.BG != "" {
		size += binary.MaxVarintLen64 + len(style.BG)
	}
	return size
}

func terminalGridStyleFlags(style vterm.CellStyle) uint16 {
	var flags uint16
	if style.FG != "" {
		flags |= terminalGridStyleFlagFG
	}
	if style.BG != "" {
		flags |= terminalGridStyleFlagBG
	}
	if style.Bold {
		flags |= terminalGridStyleFlagBold
	}
	if style.Italic {
		flags |= terminalGridStyleFlagItalic
	}
	if style.Underline {
		flags |= terminalGridStyleFlagUnderline
	}
	if style.Blink {
		flags |= terminalGridStyleFlagBlink
	}
	if style.Reverse {
		flags |= terminalGridStyleFlagReverse
	}
	if style.Strikethrough {
		flags |= terminalGridStyleFlagStrikethrough
	}
	return flags
}

func appendString(out []byte, value string) []byte {
	out = appendUvarint(out, uint64(len(value)))
	return append(out, value...)
}

func appendUvarint(out []byte, value uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], value)
	return append(out, buf[:n]...)
}

func appendVarint(out []byte, value int64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], value)
	return append(out, buf[:n]...)
}

type terminalGridReader struct {
	data []byte
	pos  int
}

func (r *terminalGridReader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	value := r.data[r.pos]
	r.pos++
	return value, nil
}

func (r *terminalGridReader) readUvarint() (uint64, error) {
	value, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.pos += n
	return value, nil
}

func (r *terminalGridReader) readVarint() (int64, error) {
	value, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.pos += n
	return value, nil
}

func (r *terminalGridReader) readString() (string, error) {
	length64, err := r.readUvarint()
	if err != nil {
		return "", err
	}
	if length64 > uint64(len(r.data)-r.pos) {
		return "", io.ErrUnexpectedEOF
	}
	length := int(length64)
	value := string(r.data[r.pos : r.pos+length])
	r.pos += length
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("terminal grid row contains invalid utf-8 string")
	}
	return value, nil
}

func (r *terminalGridReader) readRun() ([]vterm.Cell, error) {
	flags, err := r.readByte()
	if err != nil {
		return nil, err
	}
	style := vterm.CellStyle{}
	if flags&terminalGridRunFlagStyle != 0 {
		style, err = r.readStyle()
		if err != nil {
			return nil, err
		}
	}
	if flags&terminalGridRunFlagASCII != 0 {
		text, err := r.readString()
		if err != nil {
			return nil, err
		}
		cells := make([]vterm.Cell, 0, len(text))
		for i := 0; i < len(text); i++ {
			if text[i] >= utf8.RuneSelf {
				return nil, fmt.Errorf("terminal grid ascii run contains non-ascii byte")
			}
			cells = append(cells, vterm.Cell{Content: string(text[i]), Width: 1, Style: style})
		}
		return cells, nil
	}
	cellCount64, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	if cellCount64 > uint64(^uint32(0)) {
		return nil, fmt.Errorf("terminal grid cell count out of range: %d", cellCount64)
	}
	cells := make([]vterm.Cell, int(cellCount64))
	for i := range cells {
		cell, err := r.readCell(style)
		if err != nil {
			return nil, err
		}
		cells[i] = cell
	}
	return cells, nil
}

func (r *terminalGridReader) readCell(style vterm.CellStyle) (vterm.Cell, error) {
	flags, err := r.readByte()
	if err != nil {
		return vterm.Cell{}, err
	}
	cell := vterm.Cell{Content: " ", Width: 1, Style: style}
	if flags&terminalGridCellFlagContent != 0 {
		content, err := r.readString()
		if err != nil {
			return vterm.Cell{}, err
		}
		cell.Content = content
	}
	if flags&terminalGridCellFlagWidth != 0 {
		width, err := r.readVarint()
		if err != nil {
			return vterm.Cell{}, err
		}
		cell.Width = int(width)
	}
	return cell, nil
}

func (r *terminalGridReader) readStyle() (vterm.CellStyle, error) {
	flags64, err := r.readUvarint()
	if err != nil {
		return vterm.CellStyle{}, err
	}
	if flags64 > uint64(^uint16(0)) {
		return vterm.CellStyle{}, fmt.Errorf("terminal grid style flags out of range: %d", flags64)
	}
	flags := uint16(flags64)
	style := vterm.CellStyle{
		Bold:          flags&terminalGridStyleFlagBold != 0,
		Italic:        flags&terminalGridStyleFlagItalic != 0,
		Underline:     flags&terminalGridStyleFlagUnderline != 0,
		Blink:         flags&terminalGridStyleFlagBlink != 0,
		Reverse:       flags&terminalGridStyleFlagReverse != 0,
		Strikethrough: flags&terminalGridStyleFlagStrikethrough != 0,
	}
	if flags&terminalGridStyleFlagFG != 0 {
		value, err := r.readString()
		if err != nil {
			return vterm.CellStyle{}, err
		}
		style.FG = value
	}
	if flags&terminalGridStyleFlagBG != 0 {
		value, err := r.readString()
		if err != nil {
			return vterm.CellStyle{}, err
		}
		style.BG = value
	}
	return style, nil
}
