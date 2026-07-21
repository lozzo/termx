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

	"github.com/muxvia/muxvia/core/history"
)

const (
	lineFileRecordMagic       uint32 = 0x54584C4C // "TXLL"
	lineFileRecordVersion     uint16 = 1
	lineFileHeaderSize               = 24
	lineFileIndexMagic        uint32 = 0x54584C49 // "TXLI"
	lineFileIndexVersion      uint16 = 1
	lineFileIndexHeaderSize          = 16
	lineFileIndexEntrySize           = 24
	lineFilePayloadWriterSize        = 256 * 1024
	lineFileIndexWriterSize          = 64 * 1024

	// lineFileRecordKindLine 是 logical line 正文记录。
	lineFileRecordKindLine uint8 = 1
	// lineFileRecordKindBoundary 是 ED3/ClearScrollback 历史分段标记：
	// 它只表达软页边界，不裁剪 authoritative logical-line 历史。
	lineFileRecordKindBoundary uint8 = 2

	lineFileFlagHardEnd uint8 = 1 << 0
)

// LineFile 是单 terminal 的 logical line append-only 二进制文件。
// 正文 truth 在 path；indexPath 只是可重建 sidecar，保存 line record 的
// offset/len/flags。恢复优先读 sidecar，缺失或损坏时才扫描 payload header。
// 分页读按 line 绝对序号 Lines(start,end) 随机访问。
type LineFile struct {
	path        string
	indexPath   string
	file        *os.File
	indexFile   *os.File
	writer      *bufio.Writer
	indexWriter *bufio.Writer
	writeOffset int64
	headerBuf   [lineFileHeaderSize]byte
	indexBuf    [lineFileIndexEntrySize]byte
	uintBuf     [8]byte
	offsets     []int64
	payloadLens []uint32
	lineFlags   []uint8
	base        int
}

type lineFileIndexEntry struct {
	offset     int64
	payloadLen uint32
	flags      uint8
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
	lf := &LineFile{path: path, indexPath: path + ".idx", file: file}
	if err := lf.recover(); err != nil {
		_ = file.Close()
		if lf.indexFile != nil {
			_ = lf.indexFile.Close()
		}
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

// IndexPath 返回 payload metadata sidecar 路径（诊断与测试用）。sidecar
// 不是正文 truth，损坏时可由 payload header 重建。
func (f *LineFile) IndexPath() string {
	if f == nil {
		return ""
	}
	return f.indexPath
}

// AppendLines 追加 logical line 记录到 buffered append stream。调用返回后，
// 本进程内的 offset 索引即可被 Lines 读取；Lines/Sync/Close 会负责 flush
// payload writer。sidecar index 只是可重建加速索引，同样按批缓冲写入，
// 避免高压 seal-on-eviction 路径被每行 fs buffer flush 拖慢。
func (f *LineFile) AppendLines(lines []Line) error {
	if f == nil || len(lines) == 0 {
		return nil
	}
	if f.file == nil {
		return os.ErrInvalid
	}
	if f.writer == nil {
		f.writer = bufio.NewWriterSize(f.file, lineFilePayloadWriterSize)
	}
	offset := f.writeOffset
	for _, line := range lines {
		payloadLen, err := lineFilePayloadLen(line)
		if err != nil {
			return err
		}
		clear(f.headerBuf[:])
		header := f.headerBuf[:]
		binary.LittleEndian.PutUint32(header[0:4], lineFileRecordMagic)
		binary.LittleEndian.PutUint16(header[4:6], lineFileRecordVersion)
		header[6] = lineFileRecordKindLine
		if line.HardEnd {
			header[7] |= lineFileFlagHardEnd
		}
		binary.LittleEndian.PutUint64(header[8:16], uint64(len(f.offsets)))
		binary.LittleEndian.PutUint32(header[16:20], payloadLen)
		binary.LittleEndian.PutUint32(header[20:24], 0)
		if _, err := f.writer.Write(header); err != nil {
			return err
		}
		if err := f.writeLineFilePayload(line); err != nil {
			return err
		}
		entry := lineFileIndexEntry{
			offset:     offset,
			payloadLen: payloadLen,
			flags:      header[7],
		}
		offset += int64(lineFileHeaderSize) + int64(payloadLen)
		f.writeOffset = offset
		f.offsets = append(f.offsets, entry.offset)
		f.payloadLens = append(f.payloadLens, entry.payloadLen)
		f.lineFlags = append(f.lineFlags, entry.flags)
		if err := f.appendIndexEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

// AppendBoundary 追加一条 ED3/ClearScrollback 软页边界记录。边界只让上层
// generation/cursor 失效，不改变 Lines 可见范围；历史真值仍是全部已追加
// logical lines，payload writer 由后续 Lines/Sync/Close 统一 flush。
func (f *LineFile) AppendBoundary() error {
	if f == nil {
		return nil
	}
	if f.file == nil {
		return os.ErrInvalid
	}
	if f.writer == nil {
		f.writer = bufio.NewWriterSize(f.file, lineFilePayloadWriterSize)
	}
	clear(f.headerBuf[:])
	header := f.headerBuf[:]
	binary.LittleEndian.PutUint32(header[0:4], lineFileRecordMagic)
	binary.LittleEndian.PutUint16(header[4:6], lineFileRecordVersion)
	header[6] = lineFileRecordKindBoundary
	binary.LittleEndian.PutUint64(header[8:16], uint64(len(f.offsets)))
	if _, err := f.writer.Write(header); err != nil {
		return err
	}
	f.writeOffset += lineFileHeaderSize
	return nil
}

// LineCount 返回已追加的记录数（含 HardEnd=false 的 chunk 记录），绝对域。
func (f *LineFile) LineCount() int {
	if f == nil {
		return 0
	}
	return len(f.offsets)
}

// Base 返回当前可见冷历史的起点。R439 后 ClearScrollback 不隐藏历史，
// 所以 production linehist 始终从 0 开始；该方法仅保留给视图代码统一
// 使用绝对域计算。
func (f *LineFile) Base() int {
	if f == nil {
		return 0
	}
	return f.base
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

// Sync 把 buffered payload 与可重建 sidecar index 落到持久存储。payload 先
// sync，保证 sidecar 不会比正文 truth 更持久。
func (f *LineFile) Sync() error {
	if f == nil || f.file == nil {
		return nil
	}
	if f.writer != nil {
		if err := f.writer.Flush(); err != nil {
			return err
		}
	}
	if err := f.file.Sync(); err != nil {
		return err
	}
	if f.indexWriter != nil {
		if err := f.indexWriter.Flush(); err != nil {
			return err
		}
	}
	if f.indexFile != nil {
		if err := f.indexFile.Sync(); err != nil {
			return err
		}
	}
	return nil
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
	if f.indexWriter != nil {
		if err := f.indexWriter.Flush(); err != nil {
			_ = f.file.Close()
			return err
		}
		f.indexWriter = nil
	}
	var err error
	if f.indexFile != nil {
		err = f.indexFile.Close()
		f.indexFile = nil
	}
	if closeErr := f.file.Close(); err == nil {
		err = closeErr
	}
	f.file = nil
	return err
}

// recover 优先从 sidecar 恢复 line offset 索引；sidecar 缺失或损坏时，
// 才扫描 payload header 重建。半截尾记录（崩溃残留）会被截掉。
func (f *LineFile) recover() error {
	info, err := f.file.Stat()
	if err != nil {
		return err
	}
	indexFile, err := os.OpenFile(f.indexPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	f.indexFile = indexFile
	size := info.Size()
	f.offsets = nil
	f.payloadLens = nil
	f.lineFlags = nil
	f.base = 0
	startOffset, ok := f.loadIndex(size)
	if !ok {
		if err := f.resetIndexFile(); err != nil {
			return err
		}
		startOffset = 0
	}
	if _, err := f.indexFile.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	offset, err := f.scanPayloadIndexFrom(startOffset, size)
	if err != nil {
		return err
	}
	if offset < size {
		if err := f.file.Truncate(offset); err != nil {
			return err
		}
	}
	if f.indexWriter != nil {
		if err := f.indexWriter.Flush(); err != nil {
			return err
		}
	}
	end, err := f.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	f.writeOffset = end
	return nil
}

func (f *LineFile) loadIndex(payloadSize int64) (int64, bool) {
	info, err := f.indexFile.Stat()
	if err != nil || info.Size() < lineFileIndexHeaderSize {
		return 0, false
	}
	var idxHeader [lineFileIndexHeaderSize]byte
	if _, err := f.indexFile.ReadAt(idxHeader[:], 0); err != nil {
		return 0, false
	}
	if binary.LittleEndian.Uint32(idxHeader[0:4]) != lineFileIndexMagic ||
		binary.LittleEndian.Uint16(idxHeader[4:6]) != lineFileIndexVersion ||
		binary.LittleEndian.Uint16(idxHeader[6:8]) != lineFileIndexEntrySize {
		return 0, false
	}
	entriesSize := info.Size() - lineFileIndexHeaderSize
	if entriesSize < 0 {
		return 0, false
	}
	completeEntriesSize := entriesSize - entriesSize%lineFileIndexEntrySize
	if completeEntriesSize != entriesSize {
		if err := f.indexFile.Truncate(lineFileIndexHeaderSize + completeEntriesSize); err != nil {
			return 0, false
		}
	}
	entryCount := int(completeEntriesSize / lineFileIndexEntrySize)
	if entryCount == 0 {
		return 0, true
	}
	reader := io.NewSectionReader(f.indexFile, lineFileIndexHeaderSize, completeEntriesSize)
	f.offsets = make([]int64, 0, entryCount)
	f.payloadLens = make([]uint32, 0, entryCount)
	f.lineFlags = make([]uint8, 0, entryCount)
	var lastOffset int64 = -1
	for i := 0; i < entryCount; i++ {
		var raw [lineFileIndexEntrySize]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return 0, false
		}
		offset := int64(binary.LittleEndian.Uint64(raw[0:8]))
		payloadLen := binary.LittleEndian.Uint32(raw[8:12])
		flags := raw[12]
		if offset <= lastOffset || offset < 0 {
			return 0, false
		}
		f.offsets = append(f.offsets, offset)
		f.payloadLens = append(f.payloadLens, payloadLen)
		f.lineFlags = append(f.lineFlags, flags)
		lastOffset = offset
	}
	last := lineFileIndexEntry{
		offset:     f.offsets[len(f.offsets)-1],
		payloadLen: f.payloadLens[len(f.payloadLens)-1],
		flags:      f.lineFlags[len(f.lineFlags)-1],
	}
	if !f.validateIndexEntry(last, payloadSize) {
		f.offsets = nil
		f.payloadLens = nil
		f.lineFlags = nil
		return 0, false
	}
	return last.offset + lineFileHeaderSize + int64(last.payloadLen), true
}

func (f *LineFile) validateIndexEntry(entry lineFileIndexEntry, payloadSize int64) bool {
	if entry.offset < 0 || entry.offset+lineFileHeaderSize+int64(entry.payloadLen) > payloadSize {
		return false
	}
	var header [lineFileHeaderSize]byte
	if _, err := f.file.ReadAt(header[:], entry.offset); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(header[0:4]) == lineFileRecordMagic &&
		binary.LittleEndian.Uint16(header[4:6]) == lineFileRecordVersion &&
		header[6] == lineFileRecordKindLine &&
		header[7] == entry.flags &&
		binary.LittleEndian.Uint32(header[16:20]) == entry.payloadLen
}

func (f *LineFile) resetIndexFile() error {
	if f.indexWriter != nil {
		if err := f.indexWriter.Flush(); err != nil {
			return err
		}
	}
	if err := f.indexFile.Truncate(0); err != nil {
		return err
	}
	if _, err := f.indexFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	f.offsets = nil
	f.payloadLens = nil
	f.lineFlags = nil
	if f.indexWriter == nil {
		f.indexWriter = bufio.NewWriterSize(f.indexFile, lineFileIndexWriterSize)
	}
	var header [lineFileIndexHeaderSize]byte
	binary.LittleEndian.PutUint32(header[0:4], lineFileIndexMagic)
	binary.LittleEndian.PutUint16(header[4:6], lineFileIndexVersion)
	binary.LittleEndian.PutUint16(header[6:8], lineFileIndexEntrySize)
	if _, err := f.indexWriter.Write(header[:]); err != nil {
		return err
	}
	return f.indexWriter.Flush()
}

func (f *LineFile) scanPayloadIndexFrom(startOffset int64, payloadSize int64) (int64, error) {
	offset := startOffset
	var header [lineFileHeaderSize]byte
	var pending []lineFileIndexEntry
	flushPending := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := f.appendIndexEntries(pending); err != nil {
			return err
		}
		pending = pending[:0]
		return nil
	}
	for offset+lineFileHeaderSize <= payloadSize {
		if _, err := f.file.ReadAt(header[:], offset); err != nil {
			return offset, err
		}
		if binary.LittleEndian.Uint32(header[0:4]) != lineFileRecordMagic {
			return offset, errors.New("invalid logical line record magic")
		}
		if binary.LittleEndian.Uint16(header[4:6]) != lineFileRecordVersion {
			return offset, errors.New("unsupported logical line record version")
		}
		payloadLen := binary.LittleEndian.Uint32(header[16:20])
		recordEnd := offset + lineFileHeaderSize + int64(payloadLen)
		if recordEnd > payloadSize {
			break
		}
		switch header[6] {
		case lineFileRecordKindLine:
			entry := lineFileIndexEntry{offset: offset, payloadLen: payloadLen, flags: header[7]}
			f.offsets = append(f.offsets, offset)
			f.payloadLens = append(f.payloadLens, payloadLen)
			f.lineFlags = append(f.lineFlags, header[7])
			pending = append(pending, entry)
			if len(pending) >= 4096 {
				if err := flushPending(); err != nil {
					return offset, err
				}
			}
		case lineFileRecordKindBoundary:
		default:
			return offset, errors.New("unsupported logical line record kind")
		}
		offset = recordEnd
	}
	if err := flushPending(); err != nil {
		return offset, err
	}
	return offset, nil
}

func (f *LineFile) appendIndexEntries(entries []lineFileIndexEntry) error {
	if f == nil || len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if err := f.appendIndexEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func (f *LineFile) appendIndexEntry(entry lineFileIndexEntry) error {
	if f == nil {
		return nil
	}
	if f.indexFile == nil {
		return os.ErrInvalid
	}
	if f.indexWriter == nil {
		f.indexWriter = bufio.NewWriterSize(f.indexFile, lineFileIndexWriterSize)
	}
	clear(f.indexBuf[:])
	binary.LittleEndian.PutUint64(f.indexBuf[0:8], uint64(entry.offset))
	binary.LittleEndian.PutUint32(f.indexBuf[8:12], entry.payloadLen)
	f.indexBuf[12] = entry.flags
	if _, err := f.indexWriter.Write(f.indexBuf[:]); err != nil {
		return err
	}
	return nil
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
	for {
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
		payloadLen := int64(binary.LittleEndian.Uint32(header[16:20]))
		// boundary 记录不占 offset 序号，顺序读时跳过即可（offsets 里的
		// start 定位到正文记录，中间可能穿插 clear 分段标记）。
		if kind == lineFileRecordKindBoundary {
			if _, err := io.CopyN(io.Discard, reader, payloadLen); err != nil {
				return Line{}, kind, err
			}
			continue
		}
		if kind != lineFileRecordKindLine {
			return Line{}, kind, errors.New("unsupported logical line record kind")
		}
		payload := make([]byte, int(payloadLen))
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
}

func lineFilePayloadLen(line Line) (uint32, error) {
	const maxUint32 = int64(^uint32(0))
	size := int64(4)
	for _, run := range line.Runs {
		size += int64(4 + len(run.Text))
		size += int64(4 + len(run.Style.FG))
		size += int64(4 + len(run.Style.BG))
		size += 1
		size += int64(4 + len(run.LinkURL))
		size += int64(4 + len(run.LinkParams))
		if size > maxUint32 {
			return 0, errors.New("logical line payload too large")
		}
	}
	return uint32(size), nil
}

func (f *LineFile) writeLineFilePayload(line Line) error {
	if err := f.writeLineFileUint32(f.writer, uint32(len(line.Runs))); err != nil {
		return err
	}
	for _, run := range line.Runs {
		if err := f.writeLineFileString(f.writer, run.Text); err != nil {
			return err
		}
		if err := f.writeLineFileString(f.writer, run.Style.FG); err != nil {
			return err
		}
		if err := f.writeLineFileString(f.writer, run.Style.BG); err != nil {
			return err
		}
		if err := f.writer.WriteByte(lineFileStyleFlags(run.Style)); err != nil {
			return err
		}
		if err := f.writeLineFileString(f.writer, run.LinkURL); err != nil {
			return err
		}
		if err := f.writeLineFileString(f.writer, run.LinkParams); err != nil {
			return err
		}
	}
	return nil
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

func (f *LineFile) writeLineFileUint32(writer *bufio.Writer, value uint32) error {
	binary.LittleEndian.PutUint32(f.uintBuf[:4], value)
	_, err := writer.Write(f.uintBuf[:4])
	return err
}

func (f *LineFile) writeLineFileString(writer *bufio.Writer, value string) error {
	if err := f.writeLineFileUint32(writer, uint32(len(value))); err != nil {
		return err
	}
	_, err := writer.WriteString(value)
	return err
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
