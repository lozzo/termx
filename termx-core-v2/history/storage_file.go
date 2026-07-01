package history

import (
	"bufio"
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
	writer  *bufio.Writer
	offsets map[LogicalLineID]int64
}

func (backend *fileStorageBackend) Apply(tx StorageTransaction) error {
	if backend == nil {
		return nil
	}
	if backend.offsets == nil {
		backend.offsets = make(map[LogicalLineID]int64)
	}
	currentOffset, err := backend.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if backend.writer == nil {
		backend.writer = bufio.NewWriterSize(backend.file, 256*1024)
	}
	for _, line := range tx.Lines {
		payloadLength := estimatedBinaryLogicalLinePayloadSize(line)
		offset := currentOffset
		if err := writeBinaryLineRecordHeader(backend.writer, line.ID, payloadLength); err != nil {
			return err
		}
		if err := writeBinaryLogicalLine(backend.writer, line); err != nil {
			return err
		}
		backend.offsets[line.ID] = offset
		currentOffset += 28 + int64(payloadLength)
	}
	if err := backend.writer.Flush(); err != nil {
		return err
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
	if backend.writer != nil {
		if err := backend.writer.Flush(); err != nil {
			return LogicalLine{}, false
		}
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
	if err := writeBinaryLineRecordHeader(writer, lineID, len(payload)); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func writeBinaryLineRecordHeader(writer io.Writer, lineID LogicalLineID, payloadLength int) error {
	var header [28]byte
	binary.LittleEndian.PutUint32(header[0:4], fileLineRecordMagic)
	binary.LittleEndian.PutUint16(header[4:6], fileLineRecordVersion)
	binary.LittleEndian.PutUint16(header[6:8], 0)
	binary.LittleEndian.PutUint64(header[8:16], uint64(lineID))
	binary.LittleEndian.PutUint64(header[16:24], uint64(payloadLength))
	binary.LittleEndian.PutUint32(header[24:28], 0)
	_, err := writer.Write(header[:])
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
	buf.Grow(estimatedBinaryLogicalLinePayloadSize(line))
	writer := binaryPayloadBufferWriter(&buf)
	if err := writer.writeLogicalLine(line); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeBinaryLogicalLine(writer *bufio.Writer, line LogicalLine) error {
	payload := binaryPayloadFileWriter(writer)
	return payload.writeLogicalLine(line)
}

type binaryPayloadWriter struct {
	buffer *bytes.Buffer
	file   *bufio.Writer
}

func binaryPayloadBufferWriter(buffer *bytes.Buffer) binaryPayloadWriter {
	return binaryPayloadWriter{buffer: buffer}
}

func binaryPayloadFileWriter(file *bufio.Writer) binaryPayloadWriter {
	return binaryPayloadWriter{file: file}
}

func (writer *binaryPayloadWriter) writeLogicalLine(line LogicalLine) error {
	if err := writer.writeUint64(uint64(line.ID)); err != nil {
		return err
	}
	if err := writer.writeUint64(uint64(line.Generation)); err != nil {
		return err
	}
	if err := writer.writeUint64(uint64(line.CreatedGeneration)); err != nil {
		return err
	}
	if err := writer.writeUint64(uint64(line.ContentGeneration)); err != nil {
		return err
	}
	if err := writer.writeString(string(line.Seal)); err != nil {
		return err
	}
	if err := writer.writeString(line.Kind); err != nil {
		return err
	}
	if err := writer.writeUint32(uint32(line.ScreenCols)); err != nil {
		return err
	}
	if err := writer.writeBool(line.Dirty); err != nil {
		return err
	}
	if err := writer.writeString(string(line.Residency)); err != nil {
		return err
	}
	if err := writer.writeBool(line.TailFill != nil); err != nil {
		return err
	}
	if line.TailFill != nil {
		if err := writer.writeCellStyle(line.TailFill.Style); err != nil {
			return err
		}
	}
	if len(line.Runs) > 0 && len(line.Cells) == 0 {
		return writer.writeCompactRuns(line.Runs)
	}
	return writer.writeCellRuns(line.Cells)
}

func (writer *binaryPayloadWriter) writeByte(value byte) error {
	if writer.file != nil {
		return writer.file.WriteByte(value)
	}
	return writer.buffer.WriteByte(value)
}

func (writer *binaryPayloadWriter) writeRawString(value string) error {
	if writer.file != nil {
		_, err := writer.file.WriteString(value)
		return err
	}
	_, err := writer.buffer.WriteString(value)
	return err
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
		if width == 0 {
			line.Runs = append(line.Runs, CellRun{
				Text:       text,
				Style:      style,
				LinkURL:    linkURL,
				LinkParams: linkParams,
			})
			continue
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

func (writer *binaryPayloadWriter) writeCellRuns(cells []Cell) error {
	if err := writer.writeUint32(uint32(countCellRuns(cells))); err != nil {
		return err
	}
	for start := 0; start < len(cells); {
		end := start + 1
		for end < len(cells) && cellsShareBinaryRun(cells[start], cells[end]) {
			end++
		}
		// 中文说明：file backend 只负责 payload 驻留。这里直接把同 style run
		// 写进二进制 record，避免构造第二份 run slice 或拼接大字符串。
		if err := writer.writeCellRunText(cells[start:end]); err != nil {
			return err
		}
		if err := writer.writeUint32(uint32(cells[start].Width)); err != nil {
			return err
		}
		if err := writer.writeCellStyle(cells[start].Style); err != nil {
			return err
		}
		if err := writer.writeString(cells[start].LinkURL); err != nil {
			return err
		}
		if err := writer.writeString(cells[start].LinkParams); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func (writer *binaryPayloadWriter) writeCompactRuns(runs []CellRun) error {
	if err := writer.writeUint32(uint32(len(runs))); err != nil {
		return err
	}
	for _, run := range runs {
		if err := writer.writeString(run.Text); err != nil {
			return err
		}
		// width=0 marks a logical-line compact run. Older decoded cell-runs
		// always have width > 0, so the record layout stays self-describing.
		if err := writer.writeUint32(0); err != nil {
			return err
		}
		if err := writer.writeCellStyle(run.Style); err != nil {
			return err
		}
		if err := writer.writeString(run.LinkURL); err != nil {
			return err
		}
		if err := writer.writeString(run.LinkParams); err != nil {
			return err
		}
	}
	return nil
}

func countCellRuns(cells []Cell) int {
	if len(cells) == 0 {
		return 0
	}
	runs := 1
	current := cells[0]
	for _, cell := range cells[1:] {
		if cellsShareBinaryRun(current, cell) {
			continue
		}
		runs++
		current = cell
	}
	return runs
}

func cellsShareBinaryRun(left Cell, right Cell) bool {
	return left.Width == right.Width &&
		left.Style == right.Style &&
		left.LinkURL == right.LinkURL &&
		left.LinkParams == right.LinkParams
}

func (writer *binaryPayloadWriter) writeCellRunText(cells []Cell) error {
	length := 0
	for _, cell := range cells {
		length += len(cell.Text)
	}
	if err := writer.writeUint32(uint32(length)); err != nil {
		return err
	}
	for _, cell := range cells {
		if err := writer.writeRawString(cell.Text); err != nil {
			return err
		}
	}
	return nil
}

func estimatedBinaryLogicalLinePayloadSize(line LogicalLine) int {
	size := 32 + 4 + 1 + 1 + 4
	size += encodedStringSize(string(line.Seal))
	size += encodedStringSize(line.Kind)
	size += encodedStringSize(string(line.Residency))
	if line.TailFill != nil {
		size += estimatedCellStyleSize(line.TailFill.Style)
	}
	if len(line.Runs) > 0 && len(line.Cells) == 0 {
		size += estimatedCompactRunsPayloadSize(line.Runs)
	} else {
		size += estimatedCellRunsPayloadSize(line.Cells)
	}
	return size
}

func estimatedCompactRunsPayloadSize(runs []CellRun) int {
	size := 0
	for _, run := range runs {
		size += encodedStringSize(run.Text)
		size += 4 + estimatedCellStyleSize(run.Style)
		size += encodedStringSize(run.LinkURL)
		size += encodedStringSize(run.LinkParams)
	}
	return size
}

func estimatedCellRunsPayloadSize(cells []Cell) int {
	size := 0
	for start := 0; start < len(cells); {
		end := start + 1
		textLength := len(cells[start].Text)
		for end < len(cells) && cellsShareBinaryRun(cells[start], cells[end]) {
			textLength += len(cells[end].Text)
			end++
		}
		size += 4 + textLength
		size += 4 + estimatedCellStyleSize(cells[start].Style)
		size += encodedStringSize(cells[start].LinkURL)
		size += encodedStringSize(cells[start].LinkParams)
		start = end
	}
	return size
}

func estimatedCellStyleSize(style CellStyle) int {
	return encodedStringSize(style.FG) + encodedStringSize(style.BG) + 6
}

func encodedStringSize(value string) int {
	return 4 + len(value)
}

func (writer *binaryPayloadWriter) writeCellStyle(style CellStyle) error {
	if err := writer.writeString(style.FG); err != nil {
		return err
	}
	if err := writer.writeString(style.BG); err != nil {
		return err
	}
	if err := writer.writeBool(style.Bold); err != nil {
		return err
	}
	if err := writer.writeBool(style.Italic); err != nil {
		return err
	}
	if err := writer.writeBool(style.Underline); err != nil {
		return err
	}
	if err := writer.writeBool(style.Blink); err != nil {
		return err
	}
	if err := writer.writeBool(style.Reverse); err != nil {
		return err
	}
	return writer.writeBool(style.Strikethrough)
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

func (writer *binaryPayloadWriter) writeString(value string) error {
	if err := writer.writeUint32(uint32(len(value))); err != nil {
		return err
	}
	return writer.writeRawString(value)
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

func (writer *binaryPayloadWriter) writeBool(value bool) error {
	if value {
		return writer.writeByte(1)
	}
	return writer.writeByte(0)
}

func readBool(reader *bytes.Reader) (bool, error) {
	value, err := reader.ReadByte()
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

func (writer *binaryPayloadWriter) writeUint32(value uint32) error {
	if err := writer.writeByte(byte(value)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 8)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 16)); err != nil {
		return err
	}
	return writer.writeByte(byte(value >> 24))
}

func readUint32(reader *bytes.Reader) (uint32, error) {
	var data [4]byte
	if _, err := io.ReadFull(reader, data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data[:]), nil
}

func (writer *binaryPayloadWriter) writeUint64(value uint64) error {
	if err := writer.writeByte(byte(value)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 8)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 16)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 24)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 32)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 40)); err != nil {
		return err
	}
	if err := writer.writeByte(byte(value >> 48)); err != nil {
		return err
	}
	return writer.writeByte(byte(value >> 56))
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
