package protocol

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	"google.golang.org/protobuf/proto"
)

const (
	rowsBlobMagic = "TXS2"
	rowBlobMagic  = "TXR2"

	rowBlobFlagRuns  uint8 = 1 << 0
	rowBlobFlagCells uint8 = 1 << 1

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

func EncodeSnapshotPayload(snapshot *Snapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("nil snapshot")
	}
	return proto.Marshal(snapshotToWirePB(snapshot))
}

func DecodeSnapshotPayload(payload []byte) (*Snapshot, error) {
	var msg wirepb.Snapshot
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	return snapshotFromWirePB(&msg)
}

func EncodeGridViewportPayload(viewport *GridViewport) ([]byte, error) {
	if viewport == nil {
		return nil, fmt.Errorf("nil grid viewport")
	}
	return proto.Marshal(gridViewportToWirePB(viewport))
}

func DecodeGridViewportPayload(payload []byte) (*GridViewport, error) {
	var msg wirepb.GridViewport
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	return gridViewportFromWirePB(&msg)
}

func snapshotToWirePB(snapshot *Snapshot) *wirepb.Snapshot {
	return &wirepb.Snapshot{
		TerminalId:             snapshot.TerminalID,
		Size:                   sizeToWirePB(snapshot.Size),
		ScreenIsAlternate:      snapshot.Screen.IsAlternateScreen,
		Screen:                 rowSetToWirePB(CompactRowsFromCellsPreserveTrailingBlankRows(snapshot.Screen.Cells), snapshot.ScreenTimestamps, snapshot.ScreenRowKinds, snapshot.ScreenWrapped, snapshot.ScreenOwnership),
		Scrollback:             rowSetToWirePB(snapshot.Scrollback, snapshot.ScrollbackTimestamps, snapshot.ScrollbackRowKinds, snapshot.ScrollbackWrapped, snapshot.ScrollbackOwnership),
		ScrollbackOffset:       int64(snapshot.ScrollbackOffset),
		ScrollbackTotal:        int64(snapshot.ScrollbackTotal),
		ScrollbackLogicalTotal: int64(snapshot.ScrollbackLogicalTotal),
		ScrollbackHasMore:      snapshot.ScrollbackHasMore,
		ScrollbackLoadedRows:   int64(snapshot.ScrollbackLoadedRows),
		HistoryGeneration:      snapshot.HistoryGeneration,
		ScrollbackFirstRowId:   snapshot.ScrollbackFirstRowID,
		ScrollbackLastRowId:    snapshot.ScrollbackLastRowID,
		Cursor:                 cursorToWirePB(snapshot.Cursor),
		Modes:                  modesToWirePB(snapshot.Modes),
		TimestampUnixNano:      timeToUnixNano(snapshot.Timestamp),
	}
}

func snapshotFromWirePB(msg *wirepb.Snapshot) (*Snapshot, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil snapshot payload")
	}
	screenRows, screenTimes, screenKinds, screenWrapped, screenOwnership, err := rowSetFromWirePB(msg.GetScreen())
	if err != nil {
		return nil, err
	}
	scrollbackRows, scrollbackTimes, scrollbackKinds, scrollbackWrapped, scrollbackOwnership, err := rowSetFromWirePB(msg.GetScrollback())
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		TerminalID:             msg.GetTerminalId(),
		Size:                   sizeFromWirePB(msg.GetSize()),
		Screen:                 ScreenData{Cells: CompactRowsToCells(screenRows), IsAlternateScreen: msg.GetScreenIsAlternate()},
		Scrollback:             scrollbackRows,
		ScrollbackOffset:       int(msg.GetScrollbackOffset()),
		ScrollbackTotal:        int(msg.GetScrollbackTotal()),
		ScrollbackLogicalTotal: int(msg.GetScrollbackLogicalTotal()),
		ScrollbackHasMore:      msg.GetScrollbackHasMore(),
		ScrollbackLoadedRows:   int(msg.GetScrollbackLoadedRows()),
		HistoryGeneration:      msg.GetHistoryGeneration(),
		ScrollbackFirstRowID:   msg.GetScrollbackFirstRowId(),
		ScrollbackLastRowID:    msg.GetScrollbackLastRowId(),
		ScreenTimestamps:       screenTimes,
		ScrollbackTimestamps:   scrollbackTimes,
		ScreenRowKinds:         screenKinds,
		ScrollbackRowKinds:     scrollbackKinds,
		ScreenWrapped:          screenWrapped,
		ScrollbackWrapped:      scrollbackWrapped,
		ScreenOwnership:        screenOwnership,
		ScrollbackOwnership:    scrollbackOwnership,
		Cursor:                 cursorFromWirePB(msg.GetCursor()),
		Modes:                  modesFromWirePB(msg.GetModes()),
		Timestamp:              unixNanoToTime(msg.GetTimestampUnixNano()),
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

func historyWindowToWirePB(window *HistoryWindow) *wirepb.HistoryWindow {
	lineStart := make([]int32, len(window.Lines))
	lineEnd := make([]int32, len(window.Lines))
	lineKinds := make([]string, len(window.Lines))
	lineClippedBefore := make([]bool, len(window.Lines))
	lineClippedAfter := make([]bool, len(window.Lines))
	lineLogicalLineIDs := make([]uint64, len(window.Lines))
	lineTimestampStart := make([]int64, len(window.Lines))
	lineTimestampEnd := make([]int64, len(window.Lines))
	for i, span := range window.Lines {
		lineStart[i] = int32(span.StartRow)
		lineEnd[i] = int32(span.EndRow)
		lineKinds[i] = span.RowKind
		lineClippedBefore[i] = span.ClippedBefore
		lineClippedAfter[i] = span.ClippedAfter
		lineLogicalLineIDs[i] = span.LogicalLineID
		lineTimestampStart[i] = timeToUnixNano(span.TimestampStart)
		lineTimestampEnd[i] = timeToUnixNano(span.TimestampEnd)
	}
	return &wirepb.HistoryWindow{
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
		if i < len(timestampStart) {
			span.TimestampStart = unixNanoToTime(timestampStart[i])
		}
		if i < len(timestampEnd) {
			span.TimestampEnd = unixNanoToTime(timestampEnd[i])
		}
		lines = append(lines, span)
	}
	return &HistoryWindow{
		TerminalID:    msg.GetTerminalId(),
		Token:         msg.GetToken(),
		Op:            HistoryWindowOp(msg.GetOp()),
		Size:          sizeFromWirePB(msg.GetSize()),
		Rows:          rows,
		RowTimestamps: timestamps,
		RowKinds:      rowKinds,
		RowWrapped:    wrapped,
		RowOwnership:  ownership,
		Lines:         lines,
		BeforeOffset:  int(msg.GetBeforeOffset()),
		LoadedRows:    int(msg.GetLoadedRows()),
		TotalRows:     int(msg.GetTotalRows()),
		LoadedLines:   int(msg.GetLoadedLines()),
		LogicalTotal:  int(msg.GetLogicalTotal()),
		HasMore:       msg.GetHasMore(),
		Generation:    msg.GetHistoryGeneration(),
		FirstRowID:    msg.GetFirstRowId(),
		LastRowID:     msg.GetLastRowId(),
		FirstLineID:   msg.GetFirstLineId(),
		LastLineID:    msg.GetLastLineId(),
		CursorValid:   msg.GetCursorValid(),
		CursorLineID:  msg.GetCursorBeforeLineId(),
		CursorRow:     int(msg.GetCursorBeforeRowInLine()),
		RowLineIDs:    append([]uint64(nil), msg.GetRowLogicalLineIds()...),
		RowInLine:     decodeWireInt32Slice(msg.GetRowInLine()),
		Timestamp:     unixNanoToTime(msg.GetTimestampUnixNano()),
	}, nil
}

func gridViewportToWirePB(viewport *GridViewport) *wirepb.GridViewport {
	return &wirepb.GridViewport{
		TerminalId:             viewport.TerminalID,
		Size:                   sizeToWirePB(viewport.Size),
		Rows:                   rowSetToWirePB(viewport.Rows, viewport.ScrollbackTimestamps, viewport.ScrollbackRowKinds, viewport.ScrollbackWrapped, viewport.RowOwnership),
		ScrollbackOffset:       int64(viewport.ScrollbackOffset),
		ScrollbackLimit:        int64(viewport.ScrollbackLimit),
		ScrollbackTotal:        int64(viewport.ScrollbackTotal),
		ScrollbackLogicalTotal: int64(viewport.ScrollbackLogicalTotal),
		ScrollbackHasMore:      viewport.ScrollbackHasMore,
		LoadedRows:             int64(viewport.LoadedRows),
		HistoryGeneration:      viewport.HistoryGeneration,
		FirstRowId:             viewport.FirstRowID,
		LastRowId:              viewport.LastRowID,
		TimestampUnixNano:      timeToUnixNano(viewport.Timestamp),
	}
}

func gridViewportFromWirePB(msg *wirepb.GridViewport) (*GridViewport, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil grid viewport payload")
	}
	rows, timestamps, rowKinds, wrapped, ownership, err := rowSetFromWirePB(msg.GetRows())
	if err != nil {
		return nil, err
	}
	return &GridViewport{
		TerminalID:             msg.GetTerminalId(),
		Size:                   sizeFromWirePB(msg.GetSize()),
		Rows:                   rows,
		ScrollbackOffset:       int(msg.GetScrollbackOffset()),
		ScrollbackLimit:        int(msg.GetScrollbackLimit()),
		ScrollbackTotal:        int(msg.GetScrollbackTotal()),
		ScrollbackLogicalTotal: int(msg.GetScrollbackLogicalTotal()),
		ScrollbackHasMore:      msg.GetScrollbackHasMore(),
		LoadedRows:             int(msg.GetLoadedRows()),
		HistoryGeneration:      msg.GetHistoryGeneration(),
		FirstRowID:             msg.GetFirstRowId(),
		LastRowID:              msg.GetLastRowId(),
		ScrollbackTimestamps:   timestamps,
		ScrollbackRowKinds:     rowKinds,
		ScrollbackWrapped:      wrapped,
		RowOwnership:           ownership,
		Timestamp:              unixNanoToTime(msg.GetTimestampUnixNano()),
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
	enc.appendByte(flags)
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
