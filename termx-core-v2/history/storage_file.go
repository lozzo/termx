package history

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	fileLineRecordMagic   uint32 = 0x5458484c // "TXHL"
	fileLineRecordVersion uint16 = 1
)

// NewFileStorageBackend 创建二进制 append-only payload backend。
// domain boundary：文件只保存 logical-line payload 的最新 record offset；
// timeline/window/cursor truth 仍由 HistoryStore 持有，不能从文件顺序反推历史顺序。
func NewFileStorageBackend(dir string, terminalID string) (StorageBackend, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fileName, err := historyLinePayloadFileName(terminalID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fileName)
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

func historyLinePayloadFileName(terminalID string) (string, error) {
	terminalID = strings.TrimSpace(terminalID)
	if terminalID == "" {
		return "", os.ErrInvalid
	}
	return url.PathEscape(terminalID) + ".history-lines.bin", nil
}

type fileStorageBackend struct {
	path    string
	file    *os.File
	offsets map[LogicalLineID]int64
}

func (backend *fileStorageBackend) Apply(tx StorageTransaction) error {
	if backend == nil {
		return nil
	}
	if backend.offsets == nil {
		backend.offsets = make(map[LogicalLineID]int64)
	}
	for _, line := range tx.Lines {
		payload, err := encodeBinaryLogicalLine(line)
		if err != nil {
			return err
		}
		offset, err := backend.file.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		if err := writeBinaryLineRecord(backend.file, line.ID, payload); err != nil {
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
	lineID, payload, err := readBinaryLineRecord(backend.file)
	if err != nil || lineID != id {
		return LogicalLine{}, false
	}
	line, err := decodeBinaryLogicalLine(payload)
	if err != nil {
		return LogicalLine{}, false
	}
	return line, true
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

func writeBinaryLineRecord(writer io.Writer, lineID LogicalLineID, payload []byte) error {
	var header [28]byte
	binary.LittleEndian.PutUint32(header[0:4], fileLineRecordMagic)
	binary.LittleEndian.PutUint16(header[4:6], fileLineRecordVersion)
	binary.LittleEndian.PutUint16(header[6:8], 0)
	binary.LittleEndian.PutUint64(header[8:16], uint64(lineID))
	binary.LittleEndian.PutUint64(header[16:24], uint64(len(payload)))
	binary.LittleEndian.PutUint32(header[24:28], 0)
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func readBinaryLineRecord(reader io.Reader) (LogicalLineID, []byte, error) {
	var header [28]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, nil, err
	}
	if binary.LittleEndian.Uint32(header[0:4]) != fileLineRecordMagic {
		return 0, nil, errors.New("invalid history line record magic")
	}
	if binary.LittleEndian.Uint16(header[4:6]) != fileLineRecordVersion {
		return 0, nil, errors.New("unsupported history line record version")
	}
	lineID := LogicalLineID(binary.LittleEndian.Uint64(header[8:16]))
	length := binary.LittleEndian.Uint64(header[16:24])
	if length > uint64(int(^uint(0)>>1)) {
		return 0, nil, errors.New("history line record too large")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	return lineID, payload, nil
}

func encodeBinaryLogicalLine(line LogicalLine) ([]byte, error) {
	var buf bytes.Buffer
	writeUint64(&buf, uint64(line.ID))
	writeUint64(&buf, uint64(line.Generation))
	writeUint64(&buf, uint64(line.CreatedGeneration))
	writeUint64(&buf, uint64(line.ContentGeneration))
	writeString(&buf, string(line.Seal))
	writeString(&buf, line.Kind)
	writeUint32(&buf, uint32(line.ScreenCols))
	writeBool(&buf, line.Dirty)
	writeString(&buf, string(line.Residency))
	writeBool(&buf, line.TailFill != nil)
	if line.TailFill != nil {
		writeCellStyle(&buf, line.TailFill.Style)
	}
	runs := compactCellRuns(line.Cells)
	writeUint32(&buf, uint32(len(runs)))
	for _, run := range runs {
		writeString(&buf, run.Text)
		writeUint32(&buf, uint32(run.Width))
		writeCellStyle(&buf, run.Style)
		writeString(&buf, run.LinkURL)
		writeString(&buf, run.LinkParams)
	}
	return buf.Bytes(), nil
}

func decodeBinaryLogicalLine(data []byte) (LogicalLine, error) {
	reader := bytes.NewReader(data)
	line := LogicalLine{}
	var err error
	if line.ID, err = readLogicalLineID(reader); err != nil {
		return LogicalLine{}, err
	}
	if line.Generation, err = readGeneration(reader); err != nil {
		return LogicalLine{}, err
	}
	if line.CreatedGeneration, err = readGeneration(reader); err != nil {
		return LogicalLine{}, err
	}
	if line.ContentGeneration, err = readGeneration(reader); err != nil {
		return LogicalLine{}, err
	}
	seal, err := readString(reader)
	if err != nil {
		return LogicalLine{}, err
	}
	line.Seal = SealState(seal)
	if line.Kind, err = readString(reader); err != nil {
		return LogicalLine{}, err
	}
	screenCols, err := readUint32(reader)
	if err != nil {
		return LogicalLine{}, err
	}
	line.ScreenCols = int(screenCols)
	if line.Dirty, err = readBool(reader); err != nil {
		return LogicalLine{}, err
	}
	residency, err := readString(reader)
	if err != nil {
		return LogicalLine{}, err
	}
	line.Residency = Residency(residency)
	hasTailFill, err := readBool(reader)
	if err != nil {
		return LogicalLine{}, err
	}
	if hasTailFill {
		style, err := readCellStyle(reader)
		if err != nil {
			return LogicalLine{}, err
		}
		line.TailFill = &RowTailFill{Style: style}
	}
	rawRunCount, err := readUint32(reader)
	if err != nil {
		return LogicalLine{}, err
	}
	runCount := int(rawRunCount)
	for i := 0; i < runCount; i++ {
		text, err := readString(reader)
		if err != nil {
			return LogicalLine{}, err
		}
		rawWidth, err := readUint32(reader)
		if err != nil {
			return LogicalLine{}, err
		}
		width := int(rawWidth)
		style, err := readCellStyle(reader)
		if err != nil {
			return LogicalLine{}, err
		}
		linkURL, err := readString(reader)
		if err != nil {
			return LogicalLine{}, err
		}
		linkParams, err := readString(reader)
		if err != nil {
			return LogicalLine{}, err
		}
		for _, r := range text {
			line.Cells = append(line.Cells, Cell{
				Text:       string(r),
				Width:      width,
				Style:      style,
				LinkURL:    linkURL,
				LinkParams: linkParams,
			})
		}
	}
	if reader.Len() != 0 {
		return LogicalLine{}, errors.New("history line payload has trailing bytes")
	}
	return line, nil
}

type binaryCellRun struct {
	Text       string
	Width      int
	Style      CellStyle
	LinkURL    string
	LinkParams string
}

func compactCellRuns(cells []Cell) []binaryCellRun {
	if len(cells) == 0 {
		return nil
	}
	runs := make([]binaryCellRun, 0, 1)
	current := binaryCellRun{
		Text:       cells[0].Text,
		Width:      cells[0].Width,
		Style:      cells[0].Style,
		LinkURL:    cells[0].LinkURL,
		LinkParams: cells[0].LinkParams,
	}
	for _, cell := range cells[1:] {
		if cell.Width == current.Width && cell.Style == current.Style && cell.LinkURL == current.LinkURL && cell.LinkParams == current.LinkParams {
			current.Text += cell.Text
			continue
		}
		runs = append(runs, current)
		current = binaryCellRun{
			Text:       cell.Text,
			Width:      cell.Width,
			Style:      cell.Style,
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
	}
	runs = append(runs, current)
	return runs
}

func writeCellStyle(buf *bytes.Buffer, style CellStyle) {
	writeString(buf, style.FG)
	writeString(buf, style.BG)
	writeBool(buf, style.Bold)
	writeBool(buf, style.Italic)
	writeBool(buf, style.Underline)
	writeBool(buf, style.Blink)
	writeBool(buf, style.Reverse)
	writeBool(buf, style.Strikethrough)
}

func readCellStyle(reader *bytes.Reader) (CellStyle, error) {
	fg, err := readString(reader)
	if err != nil {
		return CellStyle{}, err
	}
	bg, err := readString(reader)
	if err != nil {
		return CellStyle{}, err
	}
	bold, err := readBool(reader)
	if err != nil {
		return CellStyle{}, err
	}
	italic, err := readBool(reader)
	if err != nil {
		return CellStyle{}, err
	}
	underline, err := readBool(reader)
	if err != nil {
		return CellStyle{}, err
	}
	blink, err := readBool(reader)
	if err != nil {
		return CellStyle{}, err
	}
	reverse, err := readBool(reader)
	if err != nil {
		return CellStyle{}, err
	}
	strikethrough, err := readBool(reader)
	if err != nil {
		return CellStyle{}, err
	}
	return CellStyle{
		FG:            fg,
		BG:            bg,
		Bold:          bold,
		Italic:        italic,
		Underline:     underline,
		Blink:         blink,
		Reverse:       reverse,
		Strikethrough: strikethrough,
	}, nil
}

func writeString(buf *bytes.Buffer, value string) {
	writeUint32(buf, uint32(len(value)))
	buf.WriteString(value)
}

func readString(reader *bytes.Reader) (string, error) {
	rawLength, err := readUint32(reader)
	if err != nil {
		return "", err
	}
	length := int(rawLength)
	if length <= 0 {
		return "", nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func writeBool(buf *bytes.Buffer, value bool) {
	if value {
		buf.WriteByte(1)
		return
	}
	buf.WriteByte(0)
}

func readBool(reader *bytes.Reader) (bool, error) {
	value, err := reader.ReadByte()
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

func writeUint32(buf *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	buf.Write(data[:])
}

func readUint32(reader *bytes.Reader) (uint32, error) {
	var data [4]byte
	if _, err := io.ReadFull(reader, data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data[:]), nil
}

func writeUint64(buf *bytes.Buffer, value uint64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], value)
	buf.Write(data[:])
}

func readUint64(reader *bytes.Reader) (uint64, error) {
	var data [8]byte
	if _, err := io.ReadFull(reader, data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(data[:]), nil
}

func readLogicalLineID(reader *bytes.Reader) (LogicalLineID, error) {
	value, err := readUint64(reader)
	return LogicalLineID(value), err
}

func readGeneration(reader *bytes.Reader) (Generation, error) {
	value, err := readUint64(reader)
	return Generation(value), err
}
