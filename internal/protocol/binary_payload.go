package protocol

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	rowsBlobMagic = "TXS2"
	rowBlobMagic  = "TXR2"

	rowBlobFlagRuns  uint8 = 1 << 0
	rowBlobFlagCells uint8 = 1 << 1
	rowBlobFlagTail  uint8 = 1 << 2

	rowBlobRunFlagStyle  uint8 = 1 << 0
	rowBlobRunFlagLink   uint8 = 1 << 1
	rowBlobCellFlagStyle uint8 = 1 << 0
	rowBlobCellFlagLink  uint8 = 1 << 1
)

func EncodeBinaryResponsePayload(id uint64, result []byte) ([]byte, error) {
	return proto.Marshal(&wirepb.ResponseEnvelope{Id: id, Result: result})
}

func DecodeBinaryResponsePayload(payload []byte) (uint64, []byte, error) {
	var envelope wirepb.ResponseEnvelope
	if err := proto.Unmarshal(payload, &envelope); err != nil {
		return 0, nil, err
	}
	return envelope.GetId(), envelope.GetResult(), nil
}

// EncodeNativeScreenSnapshotPayload 编码 realtime native screen 专用 payload。
// wire 暂时复用 Snapshot 的 screen RowSet/cursor/modes 字段，但 live revision 写入独立 unknown field，
// 不借用 history_generation，避免 live display contract 污染 authoritative history 语义。
func EncodeNativeScreenSnapshotPayload(snapshot *NativeScreenSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("nil native screen snapshot")
	}
	msg := &wirepb.Snapshot{
		TerminalId:        snapshot.TerminalID,
		Size:              sizeToWirePB(snapshot.Size),
		ScreenIsAlternate: snapshot.AltScreen,
		Screen:            rowSetToWirePB(snapshot.Rows, nil, nil, nil, nil),
		Cursor:            cursorToWirePB(snapshot.Cursor),
		Modes:             modesToWirePB(snapshot.Modes),
		TimestampUnixNano: timeToUnixNano(snapshot.Timestamp),
	}
	setUint64UnknownField(msg, nativeScreenLiveRevisionFieldNumber, snapshot.Revision)
	return proto.Marshal(msg)
}

// DecodeNativeScreenSnapshotPayload 解码 live.screen.get 的专用 payload。
// 旧 Snapshot scrollback/history 字段会被忽略，调用方只能把结果当 realtime native screen。
func DecodeNativeScreenSnapshotPayload(payload []byte) (*NativeScreenSnapshot, error) {
	var msg wirepb.Snapshot
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	screenRows, _, _, _, _, err := rowSetFromWirePB(msg.GetScreen())
	if err != nil {
		return nil, err
	}
	return &NativeScreenSnapshot{
		TerminalID: msg.GetTerminalId(),
		Revision:   uint64UnknownField(&msg, nativeScreenLiveRevisionFieldNumber),
		Size:       sizeFromWirePB(msg.GetSize()),
		Rows:       screenRows,
		AltScreen:  msg.GetScreenIsAlternate(),
		Cursor:     cursorFromWirePB(msg.GetCursor()),
		Modes:      modesFromWirePB(msg.GetModes()),
		Timestamp:  unixNanoToTime(msg.GetTimestampUnixNano()),
	}, nil
}

func EncodeHistoryWindowPayload(window *HistoryWindow) ([]byte, error) {
	if window == nil {
		return nil, fmt.Errorf("nil history window")
	}
	return proto.Marshal(historyWindowToWirePB(window))
}

func DecodeHistoryWindowPayload(payload []byte) (*HistoryWindow, error) {
	var msg wirepb.HistoryWindow
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	return historyWindowFromWirePB(&msg)
}

func EncodeCopyEntryProjectionPayload(projection *CopyEntryProjection) ([]byte, error) {
	if projection == nil {
		return nil, fmt.Errorf("nil copy entry projection")
	}
	msg := historyWindowToWirePB(&projection.Window)
	// 中文说明：R376 copy-entry 结果暂时复用 HistoryWindow protobuf，并把
	// materialized projection 元数据写入保留 field number；这些元数据不是
	// history window truth，也不能让 window token 伪装成 frozen snapshot。
	setUint64UnknownField(msg, copyEntryNativeColsFieldNumber, uint64(projection.NativeCols))
	setUint64UnknownField(msg, copyEntryAppliedHistorySeqFieldNumber, projection.AppliedHistorySeq)
	setUint64UnknownField(msg, copyEntryTargetHistorySeqFieldNumber, projection.TargetHistorySeq)
	setBoolUnknownField(msg, copyEntryCatchupPendingFieldNumber, projection.CatchupPending)
	setBoolUnknownField(msg, copyEntrySelectableFieldNumber, projection.Capabilities.Selectable)
	setBoolUnknownField(msg, copyEntryCopyableFieldNumber, projection.Capabilities.Copyable)
	setBoolUnknownField(msg, copyEntrySearchableFieldNumber, projection.Capabilities.Searchable)
	setBoolUnknownField(msg, copyEntryPageableFieldNumber, projection.Capabilities.Pageable)
	setInt64UnknownField(msg, copyEntryTimestampFieldNumber, timeToUnixNano(projection.Timestamp))
	return proto.Marshal(msg)
}

func DecodeCopyEntryProjectionPayload(payload []byte) (*CopyEntryProjection, error) {
	var msg wirepb.HistoryWindow
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	window, err := historyWindowFromWirePB(&msg)
	if err != nil {
		return nil, err
	}
	return &CopyEntryProjection{
		TerminalID:        window.TerminalID,
		NativeCols:        int(uint64UnknownField(&msg, copyEntryNativeColsFieldNumber)),
		Generation:        window.Generation,
		Window:            *window,
		AppliedHistorySeq: uint64UnknownField(&msg, copyEntryAppliedHistorySeqFieldNumber),
		TargetHistorySeq:  uint64UnknownField(&msg, copyEntryTargetHistorySeqFieldNumber),
		CatchupPending:    boolUnknownField(&msg, copyEntryCatchupPendingFieldNumber),
		Capabilities: CopyEntryCapabilityBits{
			Selectable: boolUnknownField(&msg, copyEntrySelectableFieldNumber),
			Copyable:   boolUnknownField(&msg, copyEntryCopyableFieldNumber),
			Searchable: boolUnknownField(&msg, copyEntrySearchableFieldNumber),
			Pageable:   boolUnknownField(&msg, copyEntryPageableFieldNumber),
		},
		Timestamp: unixNanoToTime(int64UnknownField(&msg, copyEntryTimestampFieldNumber)),
	}, nil
}

func historyWindowToWirePB(window *HistoryWindow) *wirepb.HistoryWindow {
	lineStart := make([]int32, len(window.Lines))
	lineEnd := make([]int32, len(window.Lines))
	lineKinds := make([]string, len(window.Lines))
	lineClippedBefore := make([]bool, len(window.Lines))
	lineClippedAfter := make([]bool, len(window.Lines))
	lineLogicalLineIDs := make([]uint64, len(window.Lines))
	lineSessionIDs := make([]uint64, len(window.Lines))
	lineFrameIDs := make([]uint64, len(window.Lines))
	lineFixedGrid := make([]bool, len(window.Lines))
	lineScreenCols := make([]int, len(window.Lines))
	lineTimestampStart := make([]int64, len(window.Lines))
	lineTimestampEnd := make([]int64, len(window.Lines))
	for i, span := range window.Lines {
		lineStart[i] = int32(span.StartRow)
		lineEnd[i] = int32(span.EndRow)
		lineKinds[i] = span.RowKind
		lineClippedBefore[i] = span.ClippedBefore
		lineClippedAfter[i] = span.ClippedAfter
		lineLogicalLineIDs[i] = span.LogicalLineID
		lineSessionIDs[i] = span.SessionID
		lineFrameIDs[i] = span.FrameID
		lineFixedGrid[i] = span.FixedGrid
		lineScreenCols[i] = span.ScreenCols
		lineTimestampStart[i] = timeToUnixNano(span.TimestampStart)
		lineTimestampEnd[i] = timeToUnixNano(span.TimestampEnd)
	}
	msg := &wirepb.HistoryWindow{
		TerminalId:                 window.TerminalID,
		Token:                      window.Token,
		Op:                         string(window.Op),
		Size:                       sizeToWirePB(window.Size),
		Rows:                       rowSetToWirePB(window.Rows, window.RowTimestamps, window.RowKinds, window.RowWrapped, window.RowOwnership),
		LineStartRows:              lineStart,
		LineEndRows:                lineEnd,
		LineRowKinds:               lineKinds,
		LineClippedBefore:          lineClippedBefore,
		LineClippedAfter:           lineClippedAfter,
		LineLogicalLineIds:         lineLogicalLineIDs,
		LineTimestampStartUnixNano: lineTimestampStart,
		LineTimestampEndUnixNano:   lineTimestampEnd,
		BeforeOffset:               int64(window.BeforeOffset),
		LoadedRows:                 int64(window.LoadedRows),
		TotalRows:                  int64(window.TotalRows),
		LoadedLines:                int64(window.LoadedLines),
		LogicalTotal:               int64(window.LogicalTotal),
		HasMore:                    window.HasMore,
		HistoryGeneration:          window.Generation,
		FirstRowId:                 window.FirstRowID,
		LastRowId:                  window.LastRowID,
		FirstLineId:                window.FirstLineID,
		LastLineId:                 window.LastLineID,
		CursorValid:                window.CursorValid,
		CursorBeforeLineId:         window.CursorLineID,
		CursorBeforeRowInLine:      int32(window.CursorRow),
		RowLogicalLineIds:          append([]uint64(nil), window.RowLineIDs...),
		RowInLine:                  encodeWireInt32Slice(window.RowInLine),
		TimestampUnixNano:          timeToUnixNano(window.Timestamp),
	}
	// 中文说明：cursor segment 是 history.window 的 truth-source 边界字段；
	// pb 生成物未同步时先走正式 field number 的 unknown field，避免 TUI 猜测行段或分页 offset。
	setStringProtoFieldOrUnknown(msg, historyWindowResponseCursorSegmentFieldNumber, window.CursorSegment)
	setStringSliceUnknownField(msg, historyWindowResponseRowSegmentsFieldNumber, window.RowSegments)
	setInt32UnknownField(msg, historyWindowResponseCursorRowIndexFieldNumber, int32(window.CursorRowIndex))
	setUint64SliceUnknownField(msg, historyWindowResponseRowSessionIDsFieldNumber, window.RowSessionIDs)
	setUint64SliceUnknownField(msg, historyWindowResponseRowFrameIDsFieldNumber, window.RowFrameIDs)
	setBoolSliceUnknownField(msg, historyWindowResponseRowFixedGridFieldNumber, window.RowFixedGrid)
	setIntSliceUnknownField(msg, historyWindowResponseRowScreenColsFieldNumber, window.RowScreenCols)
	setIntSliceUnknownField(msg, historyWindowResponseRowIndexesFieldNumber, window.RowIndexes)
	setUint64SliceUnknownField(msg, historyWindowLineSessionIDsFieldNumber, lineSessionIDs)
	setUint64SliceUnknownField(msg, historyWindowLineFrameIDsFieldNumber, lineFrameIDs)
	setBoolSliceUnknownField(msg, historyWindowLineFixedGridFieldNumber, lineFixedGrid)
	setIntSliceUnknownField(msg, historyWindowLineScreenColsFieldNumber, lineScreenCols)
	return msg
}

func historyWindowFromWirePB(msg *wirepb.HistoryWindow) (*HistoryWindow, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil history window payload")
	}
	rows, timestamps, rowKinds, wrapped, ownership, err := rowSetFromWirePB(msg.GetRows())
	if err != nil {
		return nil, err
	}
	starts := msg.GetLineStartRows()
	ends := msg.GetLineEndRows()
	kinds := msg.GetLineRowKinds()
	clippedBefore := msg.GetLineClippedBefore()
	clippedAfter := msg.GetLineClippedAfter()
	logicalLineIDs := msg.GetLineLogicalLineIds()
	lineSessionIDs := uint64SliceUnknownField(msg, historyWindowLineSessionIDsFieldNumber)
	lineFrameIDs := uint64SliceUnknownField(msg, historyWindowLineFrameIDsFieldNumber)
	lineFixedGrid := boolSliceUnknownField(msg, historyWindowLineFixedGridFieldNumber)
	lineScreenCols := intSliceUnknownField(msg, historyWindowLineScreenColsFieldNumber)
	timestampStart := msg.GetLineTimestampStartUnixNano()
	timestampEnd := msg.GetLineTimestampEndUnixNano()
	lines := make([]HistoryLineSpan, 0, len(starts))
	for i := range starts {
		span := HistoryLineSpan{StartRow: int(starts[i])}
		if i < len(ends) {
			span.EndRow = int(ends[i])
		}
		if i < len(kinds) {
			span.RowKind = kinds[i]
		}
		if i < len(clippedBefore) {
			span.ClippedBefore = clippedBefore[i]
		}
		if i < len(clippedAfter) {
			span.ClippedAfter = clippedAfter[i]
		}
		if i < len(logicalLineIDs) {
			span.LogicalLineID = logicalLineIDs[i]
		}
		if i < len(lineSessionIDs) {
			span.SessionID = lineSessionIDs[i]
		}
		if i < len(lineFrameIDs) {
			span.FrameID = lineFrameIDs[i]
		}
		if i < len(lineFixedGrid) {
			span.FixedGrid = lineFixedGrid[i]
		}
		if i < len(lineScreenCols) {
			span.ScreenCols = lineScreenCols[i]
		}
		if i < len(timestampStart) {
			span.TimestampStart = unixNanoToTime(timestampStart[i])
		}
		if i < len(timestampEnd) {
			span.TimestampEnd = unixNanoToTime(timestampEnd[i])
		}
		lines = append(lines, span)
	}
	cursorRowIndex := int(int32ProtoFieldOrUnknown(msg, historyWindowResponseCursorRowIndexFieldNumber))
	if cursorRowIndex == 0 && msg.GetCursorBeforeRowInLine() > 0 {
		// 中文说明：旧 history.window response 曾把 projection absolute offset
		// 放在 cursor_before_row_in_line；新 response 使用 cursor_row_index。
		cursorRowIndex = int(msg.GetCursorBeforeRowInLine())
	}
	return &HistoryWindow{
		TerminalID:     msg.GetTerminalId(),
		Token:          msg.GetToken(),
		Op:             HistoryWindowOp(msg.GetOp()),
		Size:           sizeFromWirePB(msg.GetSize()),
		Rows:           rows,
		RowTimestamps:  timestamps,
		RowKinds:       rowKinds,
		RowWrapped:     wrapped,
		RowOwnership:   ownership,
		RowSegments:    stringSliceUnknownField(msg, historyWindowResponseRowSegmentsFieldNumber),
		RowSessionIDs:  uint64SliceUnknownField(msg, historyWindowResponseRowSessionIDsFieldNumber),
		RowFrameIDs:    uint64SliceUnknownField(msg, historyWindowResponseRowFrameIDsFieldNumber),
		RowFixedGrid:   boolSliceUnknownField(msg, historyWindowResponseRowFixedGridFieldNumber),
		RowScreenCols:  intSliceUnknownField(msg, historyWindowResponseRowScreenColsFieldNumber),
		RowIndexes:     intSliceUnknownField(msg, historyWindowResponseRowIndexesFieldNumber),
		Lines:          lines,
		BeforeOffset:   int(msg.GetBeforeOffset()),
		LoadedRows:     int(msg.GetLoadedRows()),
		TotalRows:      int(msg.GetTotalRows()),
		LoadedLines:    int(msg.GetLoadedLines()),
		LogicalTotal:   int(msg.GetLogicalTotal()),
		HasMore:        msg.GetHasMore(),
		Generation:     msg.GetHistoryGeneration(),
		FirstRowID:     msg.GetFirstRowId(),
		LastRowID:      msg.GetLastRowId(),
		FirstLineID:    msg.GetFirstLineId(),
		LastLineID:     msg.GetLastLineId(),
		CursorValid:    msg.GetCursorValid(),
		CursorLineID:   msg.GetCursorBeforeLineId(),
		CursorRow:      int(msg.GetCursorBeforeRowInLine()),
		CursorRowIndex: cursorRowIndex,
		CursorSegment:  stringProtoFieldOrUnknown(msg, historyWindowResponseCursorSegmentFieldNumber),
		RowLineIDs:     append([]uint64(nil), msg.GetRowLogicalLineIds()...),
		RowInLine:      decodeWireInt32Slice(msg.GetRowInLine()),
		Timestamp:      unixNanoToTime(msg.GetTimestampUnixNano()),
	}, nil
}

func rowSetToWirePB(rows []CompactRow, timestamps []time.Time, rowKinds []string, wrapped []bool, ownership []string) *wirepb.RowSet {
	return &wirepb.RowSet{
		RowsBlob:           encodeCompactRowsBlob(rows),
		TimestampsUnixNano: encodeTimeSliceUnixNano(timestamps),
		RowKinds:           encodeWireStringSlice(rowKinds),
		Wrapped:            encodeWireBoolSlice(wrapped),
		Ownership:          encodeWireStringSlice(ownership),
	}
}

func rowSetFromWirePB(msg *wirepb.RowSet) ([]CompactRow, []time.Time, []string, []bool, []string, error) {
	if msg == nil {
		return nil, nil, nil, nil, nil, nil
	}
	rows, err := decodeCompactRowsBlob(msg.GetRowsBlob())
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return rows,
		decodeTimeSliceUnixNano(msg.GetTimestampsUnixNano()),
		append([]string(nil), msg.GetRowKinds()...),
		append([]bool(nil), msg.GetWrapped()...),
		append([]string(nil), msg.GetOwnership()...),
		nil
}

func encodeCompactRowsBlob(rows []CompactRow) []byte {
	if len(rows) == 0 {
		return nil
	}
	enc := binaryEncoder{buf: make([]byte, 0, compactRowsBlobSize(rows))}
	enc.appendBytes([]byte(rowsBlobMagic))
	enc.appendUvarint(uint64(len(rows)))
	for _, row := range rows {
		rowSize := compactRowBlobSize(row)
		enc.appendUvarint(uint64(rowSize))
		encodeCompactRowBlobInto(&enc, row)
	}
	return enc.buf
}

func decodeCompactRowsBlob(blob []byte) ([]CompactRow, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	dec := binaryDecoder{data: blob}
	if !dec.consumeMagic(rowsBlobMagic) {
		return nil, fmt.Errorf("invalid compact rows blob magic")
	}
	count, err := dec.readUvarint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(blob)) {
		return nil, fmt.Errorf("invalid compact rows count")
	}
	out := make([]CompactRow, 0, int(count))
	for i := uint64(0); i < count; i++ {
		size, err := dec.readUvarint()
		if err != nil {
			return nil, err
		}
		if uint64(len(dec.data)-dec.off) < size {
			return nil, fmt.Errorf("unexpected EOF")
		}
		row, err := decodeCompactRowBlob(dec.data[dec.off : dec.off+int(size)])
		if err != nil {
			return nil, err
		}
		dec.off += int(size)
		out = append(out, row)
	}
	if dec.off != len(dec.data) {
		return nil, fmt.Errorf("trailing compact rows blob data")
	}
	return out, nil
}

func compactRowsBlobSize(rows []CompactRow) int {
	size := len(rowsBlobMagic) + uvarintSize(uint64(len(rows)))
	for _, row := range rows {
		rowSize := compactRowBlobSize(row)
		size += uvarintSize(uint64(rowSize)) + rowSize
	}
	return size
}

func encodeCompactRowBlob(row CompactRow) []byte {
	enc := binaryEncoder{buf: make([]byte, 0, compactRowBlobSize(row))}
	encodeCompactRowBlobInto(&enc, row)
	return enc.buf
}

func encodeCompactRowBlobInto(enc *binaryEncoder, row CompactRow) {
	enc.appendBytes([]byte(rowBlobMagic))
	flags := uint8(0)
	if len(row.Runs) > 0 {
		flags |= rowBlobFlagRuns
	}
	if len(row.Cells) > 0 {
		flags |= rowBlobFlagCells
	}
	if row.TailFill != nil {
		flags |= rowBlobFlagTail
	}
	enc.appendByte(flags)
	if flags&rowBlobFlagTail != 0 {
		enc.appendCompactRowStyle(row.TailFill, rowBlobRunFlagStyle)
	}
	switch {
	case flags&rowBlobFlagRuns != 0:
		enc.appendUvarint(uint64(len(row.Runs)))
		for _, run := range row.Runs {
			enc.appendString(run.Text)
			enc.appendCompactRowStyle(run.Style, rowBlobRunFlagStyle)
			enc.appendCompactRowLink(run.LinkURL, run.LinkParams, rowBlobRunFlagLink)
		}
	case flags&rowBlobFlagCells != 0:
		enc.appendUvarint(uint64(len(row.Cells)))
		for _, cell := range row.Cells {
			enc.appendString(cell.Content)
			enc.appendUvarint(uint64(maxInt(0, cell.Width)))
			enc.appendCompactRowStyle(cell.Style, rowBlobCellFlagStyle)
			enc.appendCompactRowLink(cell.LinkURL, cell.LinkParams, rowBlobCellFlagLink)
		}
	default:
		enc.appendString(row.Text)
	}
}

func decodeCompactRowBlob(blob []byte) (CompactRow, error) {
	dec := binaryDecoder{data: blob}
	if !dec.consumeMagic(rowBlobMagic) {
		return CompactRow{}, fmt.Errorf("invalid compact row blob magic")
	}
	flags, err := dec.readByte()
	if err != nil {
		return CompactRow{}, err
	}
	var row CompactRow
	if flags&rowBlobFlagTail != 0 {
		row.TailFill, err = dec.readCompactRowStyle(rowBlobRunFlagStyle)
		if err != nil {
			return CompactRow{}, err
		}
	}
	switch {
	case flags&rowBlobFlagRuns != 0:
		count, err := dec.readUvarint()
		if err != nil {
			return CompactRow{}, err
		}
		row.Runs = make([]CompactRowRun, 0, int(count))
		for i := uint64(0); i < count; i++ {
			text, err := dec.readString()
			if err != nil {
				return CompactRow{}, err
			}
			style, err := dec.readCompactRowStyle(rowBlobRunFlagStyle)
			if err != nil {
				return CompactRow{}, err
			}
			linkURL, linkParams, err := dec.readCompactRowLink(rowBlobRunFlagLink)
			if err != nil {
				return CompactRow{}, err
			}
			row.Runs = append(row.Runs, CompactRowRun{Text: text, Style: style, LinkURL: linkURL, LinkParams: linkParams})
		}
	case flags&rowBlobFlagCells != 0:
		count, err := dec.readUvarint()
		if err != nil {
			return CompactRow{}, err
		}
		row.Cells = make([]CompactRowCell, 0, int(count))
		for i := uint64(0); i < count; i++ {
			content, err := dec.readString()
			if err != nil {
				return CompactRow{}, err
			}
			width, err := dec.readUvarint()
			if err != nil {
				return CompactRow{}, err
			}
			style, err := dec.readCompactRowStyle(rowBlobCellFlagStyle)
			if err != nil {
				return CompactRow{}, err
			}
			linkURL, linkParams, err := dec.readCompactRowLink(rowBlobCellFlagLink)
			if err != nil {
				return CompactRow{}, err
			}
			row.Cells = append(row.Cells, CompactRowCell{Content: content, Width: int(width), Style: style, LinkURL: linkURL, LinkParams: linkParams})
		}
	default:
		row.Text, err = dec.readString()
		if err != nil {
			return CompactRow{}, err
		}
	}
	if dec.off != len(dec.data) {
		return CompactRow{}, fmt.Errorf("compact row blob has %d trailing bytes", len(dec.data)-dec.off)
	}
	return row, nil
}

func compactRowBlobSize(row CompactRow) int {
	size := len(rowBlobMagic) + 1
	if row.TailFill != nil {
		size += compactRowStyleBlobSize(row.TailFill)
	}
	switch {
	case len(row.Runs) > 0:
		size += uvarintSize(uint64(len(row.Runs)))
		for _, run := range row.Runs {
			size += stringBlobSize(run.Text) + compactRowStyleBlobSize(run.Style) + compactRowLinkBlobSize(run.LinkURL, run.LinkParams)
		}
	case len(row.Cells) > 0:
		size += uvarintSize(uint64(len(row.Cells)))
		for _, cell := range row.Cells {
			size += stringBlobSize(cell.Content) + uvarintSize(uint64(maxInt(0, cell.Width))) + compactRowStyleBlobSize(cell.Style) + compactRowLinkBlobSize(cell.LinkURL, cell.LinkParams)
		}
	default:
		size += stringBlobSize(row.Text)
	}
	return size
}

func compactRowStyleBlobSize(style *CompactRowStyle) int {
	if style == nil {
		return 1
	}
	return 1 + stringBlobSize(style.FG) + stringBlobSize(style.BG) + 1
}

func compactRowLinkBlobSize(linkURL, linkParams string) int {
	if linkURL == "" && linkParams == "" {
		return 1
	}
	return 1 + stringBlobSize(linkURL) + stringBlobSize(linkParams)
}

func stringBlobSize(value string) int {
	return uvarintSize(uint64(len(value))) + len(value)
}

func uvarintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
}

func encodeTimeSliceUnixNano(values []time.Time) []int64 {
	if len(values) == 0 {
		return nil
	}
	out := make([]int64, len(values))
	nonEmpty := false
	for i, value := range values {
		if value.IsZero() {
			continue
		}
		out[i] = value.UTC().UnixNano()
		nonEmpty = true
	}
	if !nonEmpty {
		return nil
	}
	return out
}

func decodeTimeSliceUnixNano(values []int64) []time.Time {
	if len(values) == 0 {
		return nil
	}
	out := make([]time.Time, len(values))
	for i, value := range values {
		if value != 0 {
			out[i] = time.Unix(0, value).UTC()
		}
	}
	return out
}

func encodeWireStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	nonEmpty := false
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[i] = value
		nonEmpty = true
	}
	if !nonEmpty {
		return nil
	}
	return out
}

func setStringSliceUnknownField(msg proto.Message, field protowire.Number, values []string) {
	if msg == nil || len(values) == 0 {
		return
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		setStringUnknownField(msg, field, value)
	}
}

func stringSliceUnknownField(msg proto.Message, field protowire.Number) []string {
	if msg == nil {
		return nil
	}
	unknown := msg.ProtoReflect().GetUnknown()
	var values []string
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return nil
		}
		unknown = unknown[n:]
		valueStart := unknown
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return nil
		}
		if num == field && typ == protowire.BytesType {
			value, consumed := protowire.ConsumeBytes(valueStart)
			if consumed >= 0 {
				values = append(values, string(value))
			}
		}
		unknown = unknown[n:]
	}
	return values
}

func setUint64SliceUnknownField(msg proto.Message, field protowire.Number, values []uint64) {
	if msg == nil || len(values) == 0 {
		return
	}
	nonZero := false
	for _, value := range values {
		if value != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		return
	}
	appendUnknownField(msg, field, protowire.BytesType, func(out []byte) []byte {
		var packed []byte
		for _, value := range values {
			packed = protowire.AppendVarint(packed, value)
		}
		out = protowire.AppendVarint(out, uint64(len(packed)))
		return append(out, packed...)
	})
}

func uint64SliceUnknownField(msg proto.Message, field protowire.Number) []uint64 {
	if msg == nil {
		return nil
	}
	unknown := msg.ProtoReflect().GetUnknown()
	var values []uint64
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return nil
		}
		unknown = unknown[n:]
		valueStart := unknown
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return nil
		}
		if num == field {
			switch typ {
			case protowire.VarintType:
				value, consumed := protowire.ConsumeVarint(valueStart)
				if consumed >= 0 {
					values = append(values, value)
				}
			case protowire.BytesType:
				payload, consumed := protowire.ConsumeBytes(valueStart)
				if consumed >= 0 {
					values = append(values, consumePackedUint64(payload)...)
				}
			}
		}
		unknown = unknown[n:]
	}
	return values
}

func setIntSliceUnknownField(msg proto.Message, field protowire.Number, values []int) {
	if len(values) == 0 {
		return
	}
	out := make([]uint64, len(values))
	for i, value := range values {
		if value > 0 {
			out[i] = uint64(value)
		}
	}
	setUint64SliceUnknownField(msg, field, out)
}

func intSliceUnknownField(msg proto.Message, field protowire.Number) []int {
	values := uint64SliceUnknownField(msg, field)
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	for i, value := range values {
		out[i] = int(value)
	}
	return out
}

func setBoolSliceUnknownField(msg proto.Message, field protowire.Number, values []bool) {
	if msg == nil || len(values) == 0 {
		return
	}
	nonZero := false
	out := make([]uint64, len(values))
	for i, value := range values {
		if value {
			out[i] = 1
			nonZero = true
		}
	}
	if !nonZero {
		return
	}
	setUint64SliceUnknownField(msg, field, out)
}

func boolSliceUnknownField(msg proto.Message, field protowire.Number) []bool {
	values := uint64SliceUnknownField(msg, field)
	if len(values) == 0 {
		return nil
	}
	out := make([]bool, len(values))
	for i, value := range values {
		out[i] = value != 0
	}
	return out
}

func consumePackedUint64(payload []byte) []uint64 {
	var values []uint64
	for len(payload) > 0 {
		value, consumed := protowire.ConsumeVarint(payload)
		if consumed < 0 {
			return nil
		}
		values = append(values, value)
		payload = payload[consumed:]
	}
	return values
}

func encodeWireBoolSlice(values []bool) []bool {
	if len(values) == 0 {
		return nil
	}
	out := make([]bool, len(values))
	nonEmpty := false
	for i, value := range values {
		if value {
			out[i] = true
			nonEmpty = true
		}
	}
	if !nonEmpty {
		return nil
	}
	return out
}

func encodeWireInt32Slice(values []int) []int32 {
	if len(values) == 0 {
		return nil
	}
	out := make([]int32, len(values))
	for i, value := range values {
		out[i] = int32(value)
	}
	return out
}

func decodeWireInt32Slice(values []int32) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	for i, value := range values {
		out[i] = int(value)
	}
	return out
}

func sizeToWirePB(size Size) *wirepb.Size {
	return &wirepb.Size{Cols: uint32(size.Cols), Rows: uint32(size.Rows)}
}

func sizeFromWirePB(size *wirepb.Size) Size {
	if size == nil {
		return Size{}
	}
	return Size{Cols: uint16(size.GetCols()), Rows: uint16(size.GetRows())}
}

func cursorToWirePB(cursor CursorState) *wirepb.CursorState {
	return &wirepb.CursorState{
		Row:     int32(cursor.Row),
		Col:     int32(cursor.Col),
		Visible: cursor.Visible,
		Shape:   uint32(encodeCursorShape(cursor.Shape)),
		Blink:   cursor.Blink,
	}
}

func cursorFromWirePB(cursor *wirepb.CursorState) CursorState {
	if cursor == nil {
		return CursorState{Visible: true, Shape: "block"}
	}
	return CursorState{
		Row:     int(cursor.GetRow()),
		Col:     int(cursor.GetCol()),
		Visible: cursor.GetVisible(),
		Shape:   decodeCursorShape(byte(cursor.GetShape())),
		Blink:   cursor.GetBlink(),
	}
}

func modesToWirePB(modes TerminalModes) *wirepb.TerminalModes {
	return &wirepb.TerminalModes{Mask: uint32(encodeTerminalModesMask(modes))}
}

func modesFromWirePB(modes *wirepb.TerminalModes) TerminalModes {
	if modes == nil {
		return TerminalModes{}
	}
	return decodeTerminalModesMask(uint16(modes.GetMask()))
}

func timeToUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}

func unixNanoToTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}

type binaryEncoder struct {
	buf []byte
}

func (e *binaryEncoder) appendBytes(value []byte) {
	e.buf = append(e.buf, value...)
}

func (e *binaryEncoder) appendByte(value byte) {
	e.buf = append(e.buf, value)
}

func (e *binaryEncoder) appendUvarint(value uint64) {
	var raw [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(raw[:], value)
	e.appendBytes(raw[:n])
}

func (e *binaryEncoder) appendString(value string) {
	e.appendUvarint(uint64(len(value)))
	e.buf = append(e.buf, value...)
}

func (e *binaryEncoder) appendCompactRowStyle(style *CompactRowStyle, styleFlag uint8) {
	flags := uint8(0)
	if style != nil {
		flags |= styleFlag
	}
	e.appendByte(flags)
	if style == nil {
		return
	}
	e.appendString(style.FG)
	e.appendString(style.BG)
	e.appendByte(compactRowStyleMask(style))
}

func (e *binaryEncoder) appendCompactRowLink(linkURL, linkParams string, linkFlag uint8) {
	flags := uint8(0)
	if linkURL != "" || linkParams != "" {
		flags |= linkFlag
	}
	e.appendByte(flags)
	if flags&linkFlag == 0 {
		return
	}
	e.appendString(linkURL)
	e.appendString(linkParams)
}

func compactRowStyleMask(style *CompactRowStyle) uint8 {
	var mask uint8
	if style.Bold {
		mask |= 1 << 0
	}
	if style.Italic {
		mask |= 1 << 1
	}
	if style.Underline {
		mask |= 1 << 2
	}
	if style.Blink {
		mask |= 1 << 3
	}
	if style.Reverse {
		mask |= 1 << 4
	}
	if style.Strikethrough {
		mask |= 1 << 5
	}
	return mask
}

type binaryDecoder struct {
	data []byte
	off  int
}

func (d *binaryDecoder) consumeMagic(magic string) bool {
	if len(d.data)-d.off < len(magic) {
		return false
	}
	if string(d.data[d.off:d.off+len(magic)]) != magic {
		return false
	}
	d.off += len(magic)
	return true
}

func (d *binaryDecoder) readByte() (byte, error) {
	if d.off >= len(d.data) {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := d.data[d.off]
	d.off++
	return value, nil
}

func (d *binaryDecoder) readUvarint() (uint64, error) {
	value, n := binary.Uvarint(d.data[d.off:])
	if n <= 0 {
		return 0, fmt.Errorf("invalid varint")
	}
	d.off += n
	return value, nil
}

func (d *binaryDecoder) readString() (string, error) {
	size, err := d.readUvarint()
	if err != nil {
		return "", err
	}
	if uint64(len(d.data)-d.off) < size {
		return "", fmt.Errorf("unexpected EOF")
	}
	value := string(d.data[d.off : d.off+int(size)])
	d.off += int(size)
	return value, nil
}

func (d *binaryDecoder) readCompactRowStyle(styleFlag uint8) (*CompactRowStyle, error) {
	flags, err := d.readByte()
	if err != nil {
		return nil, err
	}
	if flags&styleFlag == 0 {
		return nil, nil
	}
	fg, err := d.readString()
	if err != nil {
		return nil, err
	}
	bg, err := d.readString()
	if err != nil {
		return nil, err
	}
	mask, err := d.readByte()
	if err != nil {
		return nil, err
	}
	return &CompactRowStyle{
		FG:            fg,
		BG:            bg,
		Bold:          mask&(1<<0) != 0,
		Italic:        mask&(1<<1) != 0,
		Underline:     mask&(1<<2) != 0,
		Blink:         mask&(1<<3) != 0,
		Reverse:       mask&(1<<4) != 0,
		Strikethrough: mask&(1<<5) != 0,
	}, nil
}

func (d *binaryDecoder) readCompactRowLink(linkFlag uint8) (string, string, error) {
	flags, err := d.readByte()
	if err != nil {
		return "", "", err
	}
	if flags&linkFlag == 0 {
		return "", "", nil
	}
	linkURL, err := d.readString()
	if err != nil {
		return "", "", err
	}
	linkParams, err := d.readString()
	if err != nil {
		return "", "", err
	}
	return linkURL, linkParams, nil
}
