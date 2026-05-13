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
	terminalHistoryRowMagic0 byte = 'T'
	terminalHistoryRowMagic1 byte = 'X'
	terminalHistoryRowMagic2 byte = 'H'
	terminalHistoryRowMagic3 byte = 'R'

	terminalHistoryRowCodecVersion byte = 1

	terminalHistoryRowFlagTimestamp uint16 = 1 << 0
	terminalHistoryRowFlagRowKind   uint16 = 1 << 1

	terminalHistoryRunFlagStyle uint8 = 1 << 0
	terminalHistoryRunFlagASCII uint8 = 1 << 1

	terminalHistoryCellFlagContent byte = 1 << 0
	terminalHistoryCellFlagWidth   byte = 1 << 1

	terminalHistoryStyleFlagFG            uint16 = 1 << 0
	terminalHistoryStyleFlagBG            uint16 = 1 << 1
	terminalHistoryStyleFlagBold          uint16 = 1 << 2
	terminalHistoryStyleFlagItalic        uint16 = 1 << 3
	terminalHistoryStyleFlagUnderline     uint16 = 1 << 4
	terminalHistoryStyleFlagBlink         uint16 = 1 << 5
	terminalHistoryStyleFlagReverse       uint16 = 1 << 6
	terminalHistoryStyleFlagStrikethrough uint16 = 1 << 7
)

func encodeTerminalHistoryRow(row terminalHistoryRow) ([]byte, error) {
	cells := trimTerminalHistoryCells(row.cells)
	runs := terminalHistoryRuns(cells)
	size := 8
	if !row.timestamp.IsZero() {
		size += binary.MaxVarintLen64
	}
	rowKind := strings.TrimSpace(row.rowKind)
	if rowKind != "" {
		size += binary.MaxVarintLen64 + len(rowKind)
	}
	for _, run := range runs {
		size += terminalHistoryEncodedRunSize(run)
	}

	out := make([]byte, 0, size)
	out = append(out, terminalHistoryRowMagic0, terminalHistoryRowMagic1, terminalHistoryRowMagic2, terminalHistoryRowMagic3)
	out = append(out, terminalHistoryRowCodecVersion)
	var rowFlags uint16
	if !row.timestamp.IsZero() {
		rowFlags |= terminalHistoryRowFlagTimestamp
	}
	if rowKind != "" {
		rowFlags |= terminalHistoryRowFlagRowKind
	}
	out = appendUvarint(out, uint64(rowFlags))
	if rowFlags&terminalHistoryRowFlagTimestamp != 0 {
		out = appendVarint(out, row.timestamp.UTC().UnixNano())
	}
	if rowFlags&terminalHistoryRowFlagRowKind != 0 {
		out = appendString(out, rowKind)
	}
	out = appendUvarint(out, uint64(len(runs)))
	for _, run := range runs {
		out = appendTerminalHistoryRun(out, run)
	}
	return out, nil
}

func decodeTerminalHistoryRow(data []byte) (terminalHistoryRow, error) {
	reader := terminalHistoryReader{data: data}
	if len(data) < 5 ||
		data[0] != terminalHistoryRowMagic0 ||
		data[1] != terminalHistoryRowMagic1 ||
		data[2] != terminalHistoryRowMagic2 ||
		data[3] != terminalHistoryRowMagic3 {
		return terminalHistoryRow{}, fmt.Errorf("invalid terminal history row magic")
	}
	reader.pos = 4
	version, err := reader.readByte()
	if err != nil {
		return terminalHistoryRow{}, err
	}
	if version != terminalHistoryRowCodecVersion {
		return terminalHistoryRow{}, fmt.Errorf("unsupported terminal history row codec version %d", version)
	}
	rowFlags64, err := reader.readUvarint()
	if err != nil {
		return terminalHistoryRow{}, err
	}
	if rowFlags64 > uint64(^uint16(0)) {
		return terminalHistoryRow{}, fmt.Errorf("terminal history row flags out of range: %d", rowFlags64)
	}
	rowFlags := uint16(rowFlags64)
	row := terminalHistoryRow{}
	if rowFlags&terminalHistoryRowFlagTimestamp != 0 {
		nanos, err := reader.readVarint()
		if err != nil {
			return terminalHistoryRow{}, err
		}
		if nanos != 0 {
			row.timestamp = time.Unix(0, nanos).UTC()
		}
	}
	if rowFlags&terminalHistoryRowFlagRowKind != 0 {
		rowKind, err := reader.readString()
		if err != nil {
			return terminalHistoryRow{}, err
		}
		row.rowKind = rowKind
	}
	runCount64, err := reader.readUvarint()
	if err != nil {
		return terminalHistoryRow{}, err
	}
	if runCount64 > uint64(^uint32(0)) {
		return terminalHistoryRow{}, fmt.Errorf("terminal history row run count out of range: %d", runCount64)
	}
	for i := 0; i < int(runCount64); i++ {
		cells, err := reader.readRun()
		if err != nil {
			return terminalHistoryRow{}, err
		}
		row.cells = append(row.cells, cells...)
	}
	if reader.pos != len(reader.data) {
		return terminalHistoryRow{}, fmt.Errorf("terminal history row has %d trailing bytes", len(reader.data)-reader.pos)
	}
	return row, nil
}

func trimTerminalHistoryCells(cells []vterm.Cell) []vterm.Cell {
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

type terminalHistoryRun struct {
	style vterm.CellStyle
	ascii bool
	cells []vterm.Cell
	text  string
}

func terminalHistoryRuns(cells []vterm.Cell) []terminalHistoryRun {
	if len(cells) == 0 {
		return nil
	}
	runs := make([]terminalHistoryRun, 0, 4)
	for i := 0; i < len(cells); {
		style := cells[i].Style
		ascii := terminalHistoryASCIICompactCell(cells[i])
		start := i
		i++
		for i < len(cells) && cells[i].Style == style && terminalHistoryASCIICompactCell(cells[i]) == ascii {
			i++
		}
		runCells := cells[start:i]
		run := terminalHistoryRun{style: style, ascii: ascii, cells: runCells}
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

func terminalHistoryASCIICompactCell(cell vterm.Cell) bool {
	return cell.Width == 1 && len(cell.Content) == 1 && cell.Content[0] < utf8.RuneSelf
}

func terminalHistoryEncodedRunSize(run terminalHistoryRun) int {
	size := 1
	if run.style != (vterm.CellStyle{}) {
		size += terminalHistoryEncodedStyleSize(run.style)
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

func appendTerminalHistoryRun(out []byte, run terminalHistoryRun) []byte {
	flags := uint8(0)
	if run.style != (vterm.CellStyle{}) {
		flags |= terminalHistoryRunFlagStyle
	}
	if run.ascii {
		flags |= terminalHistoryRunFlagASCII
	}
	out = append(out, flags)
	if flags&terminalHistoryRunFlagStyle != 0 {
		out = appendTerminalHistoryStyle(out, run.style)
	}
	if flags&terminalHistoryRunFlagASCII != 0 {
		return appendString(out, run.text)
	}
	out = appendUvarint(out, uint64(len(run.cells)))
	for _, cell := range run.cells {
		out = appendTerminalHistoryCell(out, cell)
	}
	return out
}

func appendTerminalHistoryCell(out []byte, cell vterm.Cell) []byte {
	flags := byte(0)
	content := cell.Content
	if content != " " || cell.Width == 0 {
		flags |= terminalHistoryCellFlagContent
	}
	if cell.Width != 1 {
		flags |= terminalHistoryCellFlagWidth
	}
	out = append(out, flags)
	if flags&terminalHistoryCellFlagContent != 0 {
		out = appendString(out, content)
	}
	if flags&terminalHistoryCellFlagWidth != 0 {
		out = appendVarint(out, int64(cell.Width))
	}
	return out
}

func appendTerminalHistoryStyle(out []byte, style vterm.CellStyle) []byte {
	flags := terminalHistoryStyleFlags(style)
	out = appendUvarint(out, uint64(flags))
	if flags&terminalHistoryStyleFlagFG != 0 {
		out = appendString(out, style.FG)
	}
	if flags&terminalHistoryStyleFlagBG != 0 {
		out = appendString(out, style.BG)
	}
	return out
}

func terminalHistoryEncodedStyleSize(style vterm.CellStyle) int {
	size := binary.MaxVarintLen64
	if style.FG != "" {
		size += binary.MaxVarintLen64 + len(style.FG)
	}
	if style.BG != "" {
		size += binary.MaxVarintLen64 + len(style.BG)
	}
	return size
}

func terminalHistoryStyleFlags(style vterm.CellStyle) uint16 {
	var flags uint16
	if style.FG != "" {
		flags |= terminalHistoryStyleFlagFG
	}
	if style.BG != "" {
		flags |= terminalHistoryStyleFlagBG
	}
	if style.Bold {
		flags |= terminalHistoryStyleFlagBold
	}
	if style.Italic {
		flags |= terminalHistoryStyleFlagItalic
	}
	if style.Underline {
		flags |= terminalHistoryStyleFlagUnderline
	}
	if style.Blink {
		flags |= terminalHistoryStyleFlagBlink
	}
	if style.Reverse {
		flags |= terminalHistoryStyleFlagReverse
	}
	if style.Strikethrough {
		flags |= terminalHistoryStyleFlagStrikethrough
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

type terminalHistoryReader struct {
	data []byte
	pos  int
}

func (r *terminalHistoryReader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	value := r.data[r.pos]
	r.pos++
	return value, nil
}

func (r *terminalHistoryReader) readUvarint() (uint64, error) {
	value, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.pos += n
	return value, nil
}

func (r *terminalHistoryReader) readVarint() (int64, error) {
	value, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	r.pos += n
	return value, nil
}

func (r *terminalHistoryReader) readString() (string, error) {
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
		return "", fmt.Errorf("terminal history row contains invalid utf-8 string")
	}
	return value, nil
}

func (r *terminalHistoryReader) readRun() ([]vterm.Cell, error) {
	flags, err := r.readByte()
	if err != nil {
		return nil, err
	}
	style := vterm.CellStyle{}
	if flags&terminalHistoryRunFlagStyle != 0 {
		style, err = r.readStyle()
		if err != nil {
			return nil, err
		}
	}
	if flags&terminalHistoryRunFlagASCII != 0 {
		text, err := r.readString()
		if err != nil {
			return nil, err
		}
		cells := make([]vterm.Cell, 0, len(text))
		for i := 0; i < len(text); i++ {
			if text[i] >= utf8.RuneSelf {
				return nil, fmt.Errorf("terminal history ascii run contains non-ascii byte")
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
		return nil, fmt.Errorf("terminal history cell count out of range: %d", cellCount64)
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

func (r *terminalHistoryReader) readCell(style vterm.CellStyle) (vterm.Cell, error) {
	flags, err := r.readByte()
	if err != nil {
		return vterm.Cell{}, err
	}
	cell := vterm.Cell{Content: " ", Width: 1, Style: style}
	if flags&terminalHistoryCellFlagContent != 0 {
		content, err := r.readString()
		if err != nil {
			return vterm.Cell{}, err
		}
		cell.Content = content
	}
	if flags&terminalHistoryCellFlagWidth != 0 {
		width, err := r.readVarint()
		if err != nil {
			return vterm.Cell{}, err
		}
		cell.Width = int(width)
	}
	return cell, nil
}

func (r *terminalHistoryReader) readStyle() (vterm.CellStyle, error) {
	flags64, err := r.readUvarint()
	if err != nil {
		return vterm.CellStyle{}, err
	}
	if flags64 > uint64(^uint16(0)) {
		return vterm.CellStyle{}, fmt.Errorf("terminal history style flags out of range: %d", flags64)
	}
	flags := uint16(flags64)
	style := vterm.CellStyle{
		Bold:          flags&terminalHistoryStyleFlagBold != 0,
		Italic:        flags&terminalHistoryStyleFlagItalic != 0,
		Underline:     flags&terminalHistoryStyleFlagUnderline != 0,
		Blink:         flags&terminalHistoryStyleFlagBlink != 0,
		Reverse:       flags&terminalHistoryStyleFlagReverse != 0,
		Strikethrough: flags&terminalHistoryStyleFlagStrikethrough != 0,
	}
	if flags&terminalHistoryStyleFlagFG != 0 {
		value, err := r.readString()
		if err != nil {
			return vterm.CellStyle{}, err
		}
		style.FG = value
	}
	if flags&terminalHistoryStyleFlagBG != 0 {
		value, err := r.readString()
		if err != nil {
			return vterm.CellStyle{}, err
		}
		style.BG = value
	}
	return style, nil
}
