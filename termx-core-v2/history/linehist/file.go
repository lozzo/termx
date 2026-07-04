package linehist

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

	"github.com/lozzow/termx/termx-core-v2/history"
)

const (
	lineFileRecordMagic   uint32 = 0x54584C4C // "TXLL"
	lineFileRecordVersion uint16 = 1
	lineFileHeaderSize           = 24

	// lineFileRecordKindLine 是 logical line 正文记录。
	// kind=2 预留给 R434 的 boundary 记录（ED3/ClearScrollback 历史分段标记）。
	lineFileRecordKindLine uint8 = 1

	lineFileFlagHardEnd uint8 = 1 << 0
)

// LineFile 是单 terminal 的 logical line append-only 二进制文件。
// 内存只保留 offset 索引（[]int64），恢复时只扫 record header 不 materialize
// 正文，保证真正无限；分页读按 line 绝对序号 Lines(start,end) 随机访问。
type LineFile struct {
	path    string
	file    *os.File
	writer  *bufio.Writer
	offsets []int64
}

// OpenLineFile 打开（或创建）terminal 的 logical line 文件并恢复 offset 索引。
// 崩溃留下的半截尾记录会被截掉；已完整落盘的记录不可变。
func OpenLineFile(dir string, terminalID string) (*LineFile, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}
	terminalID = strings.TrimSpace(terminalID)
	if terminalID == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, url.PathEscape(terminalID)+".logical-lines.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	lf := &LineFile{path: path, file: file}
	if err := lf.recover(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return lf, nil
}

// Path 返回底层文件路径（诊断用）。
func (f *LineFile) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// AppendLines 追加 logical line 记录并 flush。记录一旦完整落盘即不可变
// （seal-on-eviction：滚出屏幕的内容程序无法再修改）。
func (f *LineFile) AppendLines(lines []Line) error {
	if f == nil || len(lines) == 0 {
		return nil
	}
	if f.file == nil {
		return os.ErrInvalid
	}
	offset, err := f.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if f.writer == nil {
		f.writer = bufio.NewWriterSize(f.file, 256*1024)
	}
	for _, line := range lines {
		payload := encodeLineFilePayload(line)
		var header [lineFileHeaderSize]byte
		binary.LittleEndian.PutUint32(header[0:4], lineFileRecordMagic)
		binary.LittleEndian.PutUint16(header[4:6], lineFileRecordVersion)
		header[6] = lineFileRecordKindLine
		if line.HardEnd {
			header[7] |= lineFileFlagHardEnd
		}
		binary.LittleEndian.PutUint64(header[8:16], uint64(len(f.offsets)))
		binary.LittleEndian.PutUint32(header[16:20], uint32(len(payload)))
		binary.LittleEndian.PutUint32(header[20:24], 0)
		if _, err := f.writer.Write(header[:]); err != nil {
			return err
		}
		if _, err := f.writer.Write(payload); err != nil {
			return err
		}
		f.offsets = append(f.offsets, offset)
		offset += int64(lineFileHeaderSize + len(payload))
	}
	return f.writer.Flush()
}

// LineCount 返回已落盘的记录数（含 HardEnd=false 的 chunk 记录）。
func (f *LineFile) LineCount() int {
	if f == nil {
		return 0
	}
	return len(f.offsets)
}

// Lines 按绝对序号读取 [start,end) 区间的记录，越界自动收敛。
func (f *LineFile) Lines(start int, end int) ([]Line, error) {
	if f == nil || f.file == nil {
		return nil, nil
	}
	if f.writer != nil {
		if err := f.writer.Flush(); err != nil {
			return nil, err
		}
	}
	start = clampLineIndex(start, 0, len(f.offsets))
	end = clampLineIndex(end, start, len(f.offsets))
	if start == end {
		return nil, nil
	}
	reader := io.NewSectionReader(f.file, f.offsets[start], int64(1)<<62)
	lines := make([]Line, 0, end-start)
	for index := start; index < end; index++ {
		line, _, err := readLineFileRecord(reader)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// Sync 把已 flush 的数据落到持久存储。
func (f *LineFile) Sync() error {
	if f == nil || f.file == nil {
		return nil
	}
	if f.writer != nil {
		if err := f.writer.Flush(); err != nil {
			return err
		}
	}
	return f.file.Sync()
}

// Close 关闭底层文件。
func (f *LineFile) Close() error {
	if f == nil || f.file == nil {
		return nil
	}
	if f.writer != nil {
		if err := f.writer.Flush(); err != nil {
			return err
		}
		f.writer = nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

// recover 只扫 record header 重建 offset 索引；半截尾记录（崩溃残留）
// 截掉，header 魔数/版本不符按损坏报错，不做静默跳过。
func (f *LineFile) recover() error {
	info, err := f.file.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	f.offsets = nil
	var offset int64
	var header [lineFileHeaderSize]byte
	for offset+lineFileHeaderSize <= size {
		if _, err := f.file.ReadAt(header[:], offset); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(header[0:4]) != lineFileRecordMagic {
			return errors.New("invalid logical line record magic")
		}
		if binary.LittleEndian.Uint16(header[4:6]) != lineFileRecordVersion {
			return errors.New("unsupported logical line record version")
		}
		recordEnd := offset + lineFileHeaderSize + int64(binary.LittleEndian.Uint32(header[16:20]))
		if recordEnd > size {
			break
		}
		f.offsets = append(f.offsets, offset)
		offset = recordEnd
	}
	if offset < size {
		if err := f.file.Truncate(offset); err != nil {
			return err
		}
	}
	_, err = f.file.Seek(0, io.SeekEnd)
	return err
}

func clampLineIndex(value int, low int, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func readLineFileRecord(reader io.Reader) (Line, uint8, error) {
	var header [lineFileHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Line{}, 0, err
	}
	if binary.LittleEndian.Uint32(header[0:4]) != lineFileRecordMagic {
		return Line{}, 0, errors.New("invalid logical line record magic")
	}
	if binary.LittleEndian.Uint16(header[4:6]) != lineFileRecordVersion {
		return Line{}, 0, errors.New("unsupported logical line record version")
	}
	kind := header[6]
	if kind != lineFileRecordKindLine {
		return Line{}, kind, errors.New("unsupported logical line record kind")
	}
	payload := make([]byte, int(binary.LittleEndian.Uint32(header[16:20])))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return Line{}, kind, err
	}
	line, err := decodeLineFilePayload(payload)
	if err != nil {
		return Line{}, kind, err
	}
	line.HardEnd = header[7]&lineFileFlagHardEnd != 0
	return line, kind, nil
}

func encodeLineFilePayload(line Line) []byte {
	var buf bytes.Buffer
	writeLineFileUint32(&buf, uint32(len(line.Runs)))
	for _, run := range line.Runs {
		writeLineFileString(&buf, run.Text)
		writeLineFileString(&buf, run.Style.FG)
		writeLineFileString(&buf, run.Style.BG)
		buf.WriteByte(lineFileStyleFlags(run.Style))
		writeLineFileString(&buf, run.LinkURL)
		writeLineFileString(&buf, run.LinkParams)
	}
	return buf.Bytes()
}

func decodeLineFilePayload(payload []byte) (Line, error) {
	reader := bytes.NewReader(payload)
	runCount, err := readLineFileUint32(reader)
	if err != nil {
		return Line{}, err
	}
	line := Line{}
	for i := 0; i < int(runCount); i++ {
		run := Run{}
		if run.Text, err = readLineFileString(reader); err != nil {
			return Line{}, err
		}
		if run.Style.FG, err = readLineFileString(reader); err != nil {
			return Line{}, err
		}
		if run.Style.BG, err = readLineFileString(reader); err != nil {
			return Line{}, err
		}
		flags, err := reader.ReadByte()
		if err != nil {
			return Line{}, err
		}
		applyLineFileStyleFlags(&run.Style, flags)
		if run.LinkURL, err = readLineFileString(reader); err != nil {
			return Line{}, err
		}
		if run.LinkParams, err = readLineFileString(reader); err != nil {
			return Line{}, err
		}
		line.Runs = append(line.Runs, run)
	}
	if reader.Len() != 0 {
		return Line{}, errors.New("logical line payload has trailing bytes")
	}
	return line, nil
}

func lineFileStyleFlags(style history.CellStyle) uint8 {
	var flags uint8
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

func applyLineFileStyleFlags(style *history.CellStyle, flags uint8) {
	style.Bold = flags&(1<<0) != 0
	style.Italic = flags&(1<<1) != 0
	style.Underline = flags&(1<<2) != 0
	style.Blink = flags&(1<<3) != 0
	style.Reverse = flags&(1<<4) != 0
	style.Strikethrough = flags&(1<<5) != 0
}

func writeLineFileUint32(buf *bytes.Buffer, value uint32) {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	buf.Write(raw[:])
}

func writeLineFileString(buf *bytes.Buffer, value string) {
	writeLineFileUint32(buf, uint32(len(value)))
	buf.WriteString(value)
}

func readLineFileUint32(reader *bytes.Reader) (uint32, error) {
	var raw [4]byte
	if _, err := io.ReadFull(reader, raw[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(raw[:]), nil
}

func readLineFileString(reader *bytes.Reader) (string, error) {
	length, err := readLineFileUint32(reader)
	if err != nil {
		return "", err
	}
	if int(length) > reader.Len() {
		return "", errors.New("logical line string length exceeds payload")
	}
	raw := make([]byte, int(length))
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", err
	}
	return string(raw), nil
}
