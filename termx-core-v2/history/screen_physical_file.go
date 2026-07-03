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
	fileScreenRowRecordMagic   uint32 = 0x54584852 // "TXHR"
	fileScreenRowRecordVersion uint16 = 1
)

// NewFileScreenPhysicalRowBackend 创建 sealed physical row 的二进制文件 backend。
// 文件只承载 row payload 与 append 顺序索引；screen buffer 仍是 ingest truth，
// HistoryWindow 按 row range 读取该 backend，不能从文件顺序反推 terminal 语义。
func NewFileScreenPhysicalRowBackend(dir string, terminalID string) (ScreenPhysicalRowBackend, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fileName, err := screenPhysicalRowPayloadFileName(terminalID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	backend := &fileScreenPhysicalRowBackend{path: path, file: file}
	if _, err := backend.Recover(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return backend, nil
}

func screenPhysicalRowPayloadFileName(terminalID string) (string, error) {
	terminalID = strings.TrimSpace(terminalID)
	if terminalID == "" {
		return "", os.ErrInvalid
	}
	return url.PathEscape(terminalID) + ".screen-rows.bin", nil
}

type fileScreenPhysicalRowBackend struct {
	path       string
	file       *os.File
	writer     *bufio.Writer
	offsets    []int64
	nextRowID  uint64
	appliedSeq uint64
}

func (backend *fileScreenPhysicalRowBackend) AppendRows(rows []PhysicalRow) error {
	if backend == nil || len(rows) == 0 {
		return nil
	}
	if backend.file == nil {
		return os.ErrInvalid
	}
	currentOffset, err := backend.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if backend.writer == nil {
		backend.writer = bufio.NewWriterSize(backend.file, 256*1024)
	}
	for _, row := range rows {
		if row.ID == 0 || !screenProjectionShouldIncludeRow(row, true) {
			continue
		}
		row.Sealed = true
		payload, err := encodeBinaryScreenPhysicalRow(row)
		if err != nil {
			return err
		}
		if err := writeBinaryScreenRowRecord(backend.writer, row.ID, payload); err != nil {
			return err
		}
		backend.offsets = append(backend.offsets, currentOffset)
		currentOffset += 28 + int64(len(payload))
		if row.ID >= backend.nextRowID {
			backend.nextRowID = row.ID + 1
		}
		if row.OwnerSeq > backend.appliedSeq {
			backend.appliedSeq = row.OwnerSeq
		}
	}
	return backend.writer.Flush()
}

func (backend *fileScreenPhysicalRowBackend) Recover() (RecoveredScreenPhysicalRows, error) {
	if backend == nil || backend.file == nil {
		return RecoveredScreenPhysicalRows{}, nil
	}
	if backend.writer != nil {
		if err := backend.writer.Flush(); err != nil {
			return RecoveredScreenPhysicalRows{}, err
		}
	}
	if _, err := backend.file.Seek(0, io.SeekStart); err != nil {
		return RecoveredScreenPhysicalRows{}, err
	}
	backend.offsets = nil
	backend.nextRowID = 1
	backend.appliedSeq = 0
	for {
		offset, err := backend.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return RecoveredScreenPhysicalRows{}, err
		}
		rowID, payload, err := readBinaryScreenRowRecord(backend.file)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return RecoveredScreenPhysicalRows{}, err
		}
		row, err := decodeBinaryScreenPhysicalRow(payload)
		if err != nil {
			return RecoveredScreenPhysicalRows{}, err
		}
		if row.ID == 0 {
			row.ID = rowID
		}
		backend.offsets = append(backend.offsets, offset)
		if row.ID >= backend.nextRowID {
			backend.nextRowID = row.ID + 1
		}
		if row.OwnerSeq > backend.appliedSeq {
			backend.appliedSeq = row.OwnerSeq
		}
	}
	_, err := backend.file.Seek(0, io.SeekEnd)
	if err != nil {
		return RecoveredScreenPhysicalRows{}, err
	}
	return RecoveredScreenPhysicalRows{
		RowCount:   len(backend.offsets),
		NextRowID:  backend.nextRowID,
		AppliedSeq: backend.appliedSeq,
	}, nil
}

func (backend *fileScreenPhysicalRowBackend) RowCount() int {
	if backend == nil {
		return 0
	}
	return len(backend.offsets)
}

func (backend *fileScreenPhysicalRowBackend) Rows(start int, end int) ([]PhysicalRow, error) {
	if backend == nil || backend.file == nil {
		return nil, nil
	}
	if backend.writer != nil {
		if err := backend.writer.Flush(); err != nil {
			return nil, err
		}
	}
	start = clampInt(start, 0, len(backend.offsets))
	end = clampInt(end, start, len(backend.offsets))
	if start == end {
		return nil, nil
	}
	if _, err := backend.file.Seek(backend.offsets[start], io.SeekStart); err != nil {
		return nil, err
	}
	rows := make([]PhysicalRow, 0, end-start)
	for index := start; index < end; index++ {
		rowID, payload, err := readBinaryScreenRowRecord(backend.file)
		if err != nil {
			return nil, err
		}
		row, err := decodeBinaryScreenPhysicalRow(payload)
		if err != nil {
			return nil, err
		}
		if row.ID == 0 {
			row.ID = rowID
		}
		row.Sealed = true
		rows = append(rows, row)
	}
	return rows, nil
}

func writeBinaryScreenRowRecord(writer io.Writer, rowID uint64, payload []byte) error {
	var header [28]byte
	binary.LittleEndian.PutUint32(header[0:4], fileScreenRowRecordMagic)
	binary.LittleEndian.PutUint16(header[4:6], fileScreenRowRecordVersion)
	binary.LittleEndian.PutUint16(header[6:8], 0)
	binary.LittleEndian.PutUint64(header[8:16], rowID)
	binary.LittleEndian.PutUint64(header[16:24], uint64(len(payload)))
	binary.LittleEndian.PutUint32(header[24:28], 0)
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func readBinaryScreenRowRecord(reader io.Reader) (uint64, []byte, error) {
	var header [28]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, nil, err
	}
	if binary.LittleEndian.Uint32(header[0:4]) != fileScreenRowRecordMagic {
		return 0, nil, errors.New("invalid screen physical row record magic")
	}
	if binary.LittleEndian.Uint16(header[4:6]) != fileScreenRowRecordVersion {
		return 0, nil, errors.New("unsupported screen physical row record version")
	}
	rowID := binary.LittleEndian.Uint64(header[8:16])
	length := binary.LittleEndian.Uint64(header[16:24])
	if length > uint64(int(^uint(0)>>1)) {
		return 0, nil, errors.New("screen physical row record too large")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	return rowID, payload, nil
}

func encodeBinaryScreenPhysicalRow(row PhysicalRow) ([]byte, error) {
	var buf bytes.Buffer
	writer := binaryPayloadBufferWriter(&buf)
	if err := writer.writeUint64(row.ID); err != nil {
		return nil, err
	}
	if err := writer.writeUint64(row.Version); err != nil {
		return nil, err
	}
	if err := writer.writeUint64(row.OwnerSeq); err != nil {
		return nil, err
	}
	if err := writer.writeUint32(uint32(row.OwnerKind)); err != nil {
		return nil, err
	}
	if err := writer.writeUint64(row.FrameID); err != nil {
		return nil, err
	}
	if err := writer.writeBool(row.Sealed); err != nil {
		return nil, err
	}
	if err := writer.writeBool(row.Wrapped); err != nil {
		return nil, err
	}
	if err := writer.writeBool(row.Continued); err != nil {
		return nil, err
	}
	if err := writer.writeUint32(uint32(row.ScreenCols)); err != nil {
		return nil, err
	}
	if err := writer.writeString(string(row.SealSegment)); err != nil {
		return nil, err
	}
	if err := writer.writeCellRuns(row.Cells); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeBinaryScreenPhysicalRow(data []byte) (PhysicalRow, error) {
	reader := bytes.NewReader(data)
	row := PhysicalRow{}
	var err error
	if row.ID, err = readUint64(reader); err != nil {
		return PhysicalRow{}, err
	}
	if row.Version, err = readUint64(reader); err != nil {
		return PhysicalRow{}, err
	}
	if row.OwnerSeq, err = readUint64(reader); err != nil {
		return PhysicalRow{}, err
	}
	ownerKind, err := readUint32(reader)
	if err != nil {
		return PhysicalRow{}, err
	}
	row.OwnerKind = RowOwnerKind(ownerKind)
	if row.FrameID, err = readUint64(reader); err != nil {
		return PhysicalRow{}, err
	}
	if row.Sealed, err = readBool(reader); err != nil {
		return PhysicalRow{}, err
	}
	if row.Wrapped, err = readBool(reader); err != nil {
		return PhysicalRow{}, err
	}
	if row.Continued, err = readBool(reader); err != nil {
		return PhysicalRow{}, err
	}
	screenCols, err := readUint32(reader)
	if err != nil {
		return PhysicalRow{}, err
	}
	row.ScreenCols = int(screenCols)
	segment, err := readString(reader)
	if err != nil {
		return PhysicalRow{}, err
	}
	row.SealSegment = HistorySegment(segment)
	runCount, err := readUint32(reader)
	if err != nil {
		return PhysicalRow{}, err
	}
	for i := 0; i < int(runCount); i++ {
		text, err := readString(reader)
		if err != nil {
			return PhysicalRow{}, err
		}
		rawWidth, err := readUint32(reader)
		if err != nil {
			return PhysicalRow{}, err
		}
		width := int(rawWidth)
		style, err := readCellStyle(reader)
		if err != nil {
			return PhysicalRow{}, err
		}
		linkURL, err := readString(reader)
		if err != nil {
			return PhysicalRow{}, err
		}
		linkParams, err := readString(reader)
		if err != nil {
			return PhysicalRow{}, err
		}
		for _, r := range text {
			row.Cells = append(row.Cells, Cell{
				Text:       string(r),
				Width:      width,
				Style:      style,
				LinkURL:    linkURL,
				LinkParams: linkParams,
			})
		}
	}
	if reader.Len() != 0 {
		return PhysicalRow{}, errors.New("screen physical row payload has trailing bytes")
	}
	return row, nil
}
