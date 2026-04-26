package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/workbenchdoc"
	"github.com/lozzow/termx/workbenchops"
)

type Hello struct {
	Version      int      `json:"version"`
	Client       string   `json:"client,omitempty"`
	Server       string   `json:"server,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
}

type ProtocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ErrorMessage struct {
	ID    uint64        `json:"id"`
	Error ProtocolError `json:"error"`
}

type Size struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type TerminalInfo struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Command   []string          `json:"command"`
	Tags      map[string]string `json:"tags,omitempty"`
	Size      Size              `json:"size"`
	State     string            `json:"state"`
	CreatedAt time.Time         `json:"created_at"`
	ExitCode  *int              `json:"exit_code,omitempty"`
}

type CreateParams struct {
	Command        []string          `json:"command"`
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	Size           Size              `json:"size,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	Env            []string          `json:"env,omitempty"`
	ScrollbackSize int               `json:"scrollback_size,omitempty"`
}

type CreateResult struct {
	TerminalID string `json:"terminal_id"`
	State      string `json:"state"`
}

type GetParams struct {
	TerminalID string `json:"terminal_id"`
}

type ResizeParams struct {
	TerminalID string `json:"terminal_id"`
	Cols       uint16 `json:"cols"`
	Rows       uint16 `json:"rows"`
}

type SetTagsParams struct {
	TerminalID string            `json:"terminal_id"`
	Tags       map[string]string `json:"tags"`
}

type SetMetadataParams struct {
	TerminalID string            `json:"terminal_id"`
	Name       string            `json:"name,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

type AttachParams struct {
	TerminalID string `json:"terminal_id"`
	Mode       string `json:"mode"`
}

type AttachResult struct {
	Mode    string `json:"mode"`
	Channel uint16 `json:"channel"`
}

type EventType int

const (
	EventTerminalCreated EventType = iota + 1
	EventTerminalStateChanged
	EventTerminalResized
	EventTerminalRemoved
	EventCollaboratorsRevoked
	EventTerminalReadError
	EventSessionCreated
	EventSessionUpdated
	EventSessionDeleted
)

type TerminalCreatedData struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Size    Size     `json:"size"`
}

type TerminalStateChangedData struct {
	OldState string `json:"old_state"`
	NewState string `json:"new_state"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

type TerminalResizedData struct {
	OldSize Size `json:"old_size"`
	NewSize Size `json:"new_size"`
}

type TerminalRemovedData struct {
	Reason string `json:"reason"`
}

type CollaboratorsRevokedData struct{}

type TerminalReadErrorData struct {
	Error string `json:"error"`
}

type SessionEventData struct {
	Revision uint64 `json:"revision,omitempty"`
	ViewID   string `json:"view_id,omitempty"`
}

type Event struct {
	Type                 EventType                 `json:"type"`
	TerminalID           string                    `json:"terminal_id"`
	SessionID            string                    `json:"session_id,omitempty"`
	Timestamp            time.Time                 `json:"timestamp"`
	Created              *TerminalCreatedData      `json:"created,omitempty"`
	StateChanged         *TerminalStateChangedData `json:"state_changed,omitempty"`
	Resized              *TerminalResizedData      `json:"resized,omitempty"`
	Removed              *TerminalRemovedData      `json:"removed,omitempty"`
	CollaboratorsRevoked *CollaboratorsRevokedData `json:"collaborators_revoked,omitempty"`
	ReadError            *TerminalReadErrorData    `json:"read_error,omitempty"`
	Session              *SessionEventData         `json:"session,omitempty"`
}

type DetachParams struct {
	TerminalID string `json:"terminal_id"`
}

type EventsParams struct {
	TerminalID string      `json:"terminal_id,omitempty"`
	SessionID  string      `json:"session_id,omitempty"`
	Types      []EventType `json:"types,omitempty"`
}

type SnapshotParams struct {
	TerminalID       string `json:"terminal_id"`
	ScrollbackOffset int    `json:"scrollback_offset,omitempty"`
	ScrollbackLimit  int    `json:"scrollback_limit,omitempty"`
}

type ScreenRowUpdate struct {
	Row       int       `json:"row"`
	Cells     []Cell    `json:"cells,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	RowKind   string    `json:"row_kind,omitempty"`
}

type ScreenSpanOp uint8

const (
	ScreenSpanOpWrite ScreenSpanOp = iota
	ScreenSpanOpClearToEOL
	ScreenSpanOpReplaceRow
)

type ScreenSpanUpdate struct {
	Row       int          `json:"row"`
	ColStart  int          `json:"col_start,omitempty"`
	Cells     []Cell       `json:"cells,omitempty"`
	Op        ScreenSpanOp `json:"op,omitempty"`
	Timestamp time.Time    `json:"timestamp,omitempty"`
	RowKind   string       `json:"row_kind,omitempty"`
}

type ScreenRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ScreenOpCode uint8

const (
	ScreenOpWriteSpan ScreenOpCode = iota
	ScreenOpScrollRect
	ScreenOpCopyRect
	ScreenOpClearRect
	ScreenOpClearToEOL
	ScreenOpCursor
	ScreenOpModes
	ScreenOpResize
	ScreenOpTitle
)

type ScreenOp struct {
	Code      ScreenOpCode  `json:"code"`
	Rect      ScreenRect    `json:"rect,omitempty"`
	Src       ScreenRect    `json:"src,omitempty"`
	DstX      int           `json:"dst_x,omitempty"`
	DstY      int           `json:"dst_y,omitempty"`
	Dx        int           `json:"dx,omitempty"`
	Dy        int           `json:"dy,omitempty"`
	Row       int           `json:"row,omitempty"`
	Col       int           `json:"col,omitempty"`
	Cells     []Cell        `json:"cells,omitempty"`
	Cursor    CursorState   `json:"cursor,omitempty"`
	Modes     TerminalModes `json:"modes,omitempty"`
	Size      Size          `json:"size,omitempty"`
	Title     string        `json:"title,omitempty"`
	Timestamp time.Time     `json:"timestamp,omitempty"`
	RowKind   string        `json:"row_kind,omitempty"`
}

type ScrollbackRowAppend struct {
	Cells     []Cell    `json:"cells,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	RowKind   string    `json:"row_kind,omitempty"`
}

type ScreenUpdate struct {
	FullReplace      bool                  `json:"full_replace,omitempty"`
	ResetScrollback  bool                  `json:"reset_scrollback,omitempty"`
	Size             Size                  `json:"size,omitempty"`
	ScreenScroll     int                   `json:"screen_scroll,omitempty"`
	Title            string                `json:"title,omitempty"`
	Screen           ScreenData            `json:"screen,omitempty"`
	ScreenTimestamps []time.Time           `json:"screen_timestamps,omitempty"`
	ScreenRowKinds   []string              `json:"screen_row_kinds,omitempty"`
	ChangedRows      []ScreenRowUpdate     `json:"changed_rows,omitempty"` // legacy TSU2 input; NormalizeScreenUpdate folds this into ChangedSpans
	ChangedSpans     []ScreenSpanUpdate    `json:"changed_spans,omitempty"`
	Ops              []ScreenOp            `json:"ops,omitempty"`
	ScrollbackTrim   int                   `json:"scrollback_trim,omitempty"`
	ScrollbackAppend []ScrollbackRowAppend `json:"scrollback_append,omitempty"`
	Cursor           CursorState           `json:"cursor"`
	Modes            TerminalModes         `json:"modes"`
}

type ScreenUpdateClassification struct {
	FullReplace         bool
	BlankFullReplace    bool
	HasContentChange    bool
	HasChangedRows      bool
	HasScreenScroll     bool
	HasScrollbackChange bool
	HasTitle            bool
}

func NormalizeScreenUpdate(update ScreenUpdate) ScreenUpdate {
	normalized := update
	if normalized.ScrollbackTrim < 0 {
		normalized.ScrollbackTrim = 0
	}
	if normalized.FullReplace {
		normalized.ScreenTimestamps = normalizeScreenUpdateTimeSlice(normalized.ScreenTimestamps, len(normalized.Screen.Cells))
		normalized.ScreenRowKinds = normalizeScreenUpdateStringSlice(normalized.ScreenRowKinds, len(normalized.Screen.Cells))
	} else {
		normalized.Screen.IsAlternateScreen = normalized.Modes.AlternateScreen
	}
	normalized.ChangedRows = normalizeChangedScreenRows(normalized.ChangedRows)
	normalized.ChangedSpans = normalizeChangedScreenSpans(normalized.ChangedRows, normalized.ChangedSpans)
	normalized.Ops = normalizeScreenOps(normalized.Ops)
	if len(normalized.ChangedRows) == 0 {
		normalized.ChangedRows = legacyScreenRowsFromSpans(normalized.ChangedSpans)
	}
	return normalized
}

func ClassifyScreenUpdate(update ScreenUpdate) ScreenUpdateClassification {
	changedRows := len(update.ChangedSpans) > 0 || len(update.ChangedRows) > 0 || len(screenUpdateChangedRowsFromOps(update.Ops)) > 0
	return ScreenUpdateClassification{
		FullReplace:         update.FullReplace,
		BlankFullReplace:    isBlankFullReplaceScreenUpdate(update),
		HasContentChange:    screenUpdateHasContentChange(update),
		HasChangedRows:      changedRows,
		HasScreenScroll:     screenUpdateHasScroll(update),
		HasScrollbackChange: update.ResetScrollback || update.ScrollbackTrim > 0 || len(update.ScrollbackAppend) > 0,
		HasTitle:            update.Title != "",
	}
}

func normalizeChangedScreenRows(rows []ScreenRowUpdate) []ScreenRowUpdate {
	if len(rows) <= 1 {
		return rows
	}
	lastIndex := make(map[int]int, len(rows))
	for index, row := range rows {
		lastIndex[row.Row] = index
	}
	normalized := make([]ScreenRowUpdate, 0, len(lastIndex))
	for index, row := range rows {
		if lastIndex[row.Row] != index {
			continue
		}
		normalized = append(normalized, row)
	}
	if len(normalized) == len(rows) {
		return rows
	}
	return normalized
}

func normalizeChangedScreenSpans(rows []ScreenRowUpdate, spans []ScreenSpanUpdate) []ScreenSpanUpdate {
	if len(rows) == 0 && len(spans) == 0 {
		return nil
	}
	normalized := make([]ScreenSpanUpdate, 0, len(rows)+len(spans))
	for _, row := range rows {
		normalized = append(normalized, normalizeScreenSpanUpdate(ScreenSpanUpdate{
			Row:       row.Row,
			Op:        ScreenSpanOpReplaceRow,
			Cells:     row.Cells,
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		}))
	}
	for _, span := range spans {
		normalized = append(normalized, normalizeScreenSpanUpdate(span))
	}
	return normalized
}

func legacyScreenRowsFromSpans(spans []ScreenSpanUpdate) []ScreenRowUpdate {
	if len(spans) == 0 {
		return nil
	}
	rows := make([]ScreenRowUpdate, 0, len(spans))
	for _, span := range spans {
		if span.Op != ScreenSpanOpReplaceRow {
			return nil
		}
		rows = append(rows, ScreenRowUpdate{
			Row:       span.Row,
			Cells:     span.Cells,
			Timestamp: span.Timestamp,
			RowKind:   span.RowKind,
		})
	}
	return rows
}

func normalizeScreenSpanUpdate(span ScreenSpanUpdate) ScreenSpanUpdate {
	switch span.Op {
	case ScreenSpanOpClearToEOL:
		span.Cells = nil
	case ScreenSpanOpReplaceRow:
		span.ColStart = 0
	}
	return span
}

func normalizeScreenOp(op ScreenOp) ScreenOp {
	switch op.Code {
	case ScreenOpWriteSpan:
		op.Rect = ScreenRect{}
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
	case ScreenOpScrollRect:
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Cursor = CursorState{}
		op.Modes = TerminalModes{}
		op.Size = Size{}
		op.Title = ""
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Rect = normalizeScreenRect(op.Rect)
	case ScreenOpCopyRect:
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Cursor = CursorState{}
		op.Modes = TerminalModes{}
		op.Size = Size{}
		op.Title = ""
		op.Dx = 0
		op.Dy = 0
		op.Src = normalizeScreenRect(op.Src)
	case ScreenOpClearRect:
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Cursor = CursorState{}
		op.Modes = TerminalModes{}
		op.Size = Size{}
		op.Title = ""
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
		op.Rect = normalizeScreenRect(op.Rect)
	case ScreenOpClearToEOL:
		op.Rect = ScreenRect{}
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
		op.Cells = nil
	case ScreenOpCursor:
		op.Rect = ScreenRect{}
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Modes = TerminalModes{}
		op.Size = Size{}
		op.Title = ""
	case ScreenOpModes:
		op.Rect = ScreenRect{}
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Cursor = CursorState{}
		op.Size = Size{}
		op.Title = ""
	case ScreenOpResize:
		op.Rect = ScreenRect{}
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Cursor = CursorState{}
		op.Modes = TerminalModes{}
		op.Title = ""
	case ScreenOpTitle:
		op.Rect = ScreenRect{}
		op.Src = ScreenRect{}
		op.DstX = 0
		op.DstY = 0
		op.Dx = 0
		op.Dy = 0
		op.Row = 0
		op.Col = 0
		op.Cells = nil
		op.Cursor = CursorState{}
		op.Modes = TerminalModes{}
		op.Size = Size{}
	}
	return op
}

func normalizeScreenRect(rect ScreenRect) ScreenRect {
	if rect.Width < 0 {
		rect.Width = 0
	}
	if rect.Height < 0 {
		rect.Height = 0
	}
	return rect
}

func normalizeScreenOps(ops []ScreenOp) []ScreenOp {
	if len(ops) == 0 {
		return nil
	}
	normalized := make([]ScreenOp, 0, len(ops))
	for _, op := range ops {
		if !isValidScreenOpCode(op.Code) {
			continue
		}
		normalized = append(normalized, normalizeScreenOp(op))
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func isValidScreenSpanOp(op ScreenSpanOp) bool {
	switch op {
	case ScreenSpanOpWrite, ScreenSpanOpClearToEOL, ScreenSpanOpReplaceRow:
		return true
	default:
		return false
	}
}

func screenSpanOpHasCells(op ScreenSpanOp) bool {
	return op != ScreenSpanOpClearToEOL
}

func isValidScreenOpCode(code ScreenOpCode) bool {
	switch code {
	case ScreenOpWriteSpan,
		ScreenOpScrollRect,
		ScreenOpCopyRect,
		ScreenOpClearRect,
		ScreenOpClearToEOL,
		ScreenOpCursor,
		ScreenOpModes,
		ScreenOpResize,
		ScreenOpTitle:
		return true
	default:
		return false
	}
}

func normalizeScreenUpdateTimeSlice(values []time.Time, size int) []time.Time {
	switch {
	case size <= 0:
		return nil
	case len(values) == size:
		return values
	case len(values) > size:
		return values[:size]
	default:
		normalized := make([]time.Time, size)
		copy(normalized, values)
		return normalized
	}
}

func normalizeScreenUpdateStringSlice(values []string, size int) []string {
	switch {
	case size <= 0:
		return nil
	case len(values) == size:
		return values
	case len(values) > size:
		return values[:size]
	default:
		normalized := make([]string, size)
		copy(normalized, values)
		return normalized
	}
}

func isBlankFullReplaceScreenUpdate(update ScreenUpdate) bool {
	if !update.FullReplace || len(update.ChangedRows) > 0 || len(update.ChangedSpans) > 0 || len(update.Ops) > 0 || len(update.ScrollbackAppend) > 0 {
		return false
	}
	for _, row := range update.Screen.Cells {
		for _, cell := range row {
			if strings.TrimSpace(cell.Content) != "" {
				return false
			}
		}
	}
	return true
}

func screenUpdateHasContentChange(update ScreenUpdate) bool {
	return update.FullReplace ||
		len(update.ChangedRows) > 0 ||
		len(update.ChangedSpans) > 0 ||
		update.ScreenScroll != 0 ||
		screenUpdateHasContentOps(update.Ops) ||
		update.ResetScrollback ||
		update.ScrollbackTrim > 0 ||
		len(update.ScrollbackAppend) > 0
}

func screenUpdateHasContentOps(ops []ScreenOp) bool {
	for _, op := range ops {
		switch op.Code {
		case ScreenOpWriteSpan, ScreenOpScrollRect, ScreenOpCopyRect, ScreenOpClearRect, ScreenOpClearToEOL, ScreenOpResize:
			return true
		}
	}
	return false
}

func screenUpdateHasScroll(update ScreenUpdate) bool {
	if update.ScreenScroll != 0 {
		return true
	}
	for _, op := range update.Ops {
		if op.Code == ScreenOpScrollRect && op.Dy != 0 {
			return true
		}
	}
	return false
}

func screenUpdateChangedRowsFromOps(ops []ScreenOp) []int {
	if len(ops) == 0 {
		return nil
	}
	rows := make([]int, 0, len(ops))
	seen := make(map[int]struct{}, len(ops))
	addRange := func(start, end int) {
		for row := start; row < end; row++ {
			if row < 0 {
				continue
			}
			if _, ok := seen[row]; ok {
				continue
			}
			seen[row] = struct{}{}
			rows = append(rows, row)
		}
	}
	for _, op := range ops {
		switch op.Code {
		case ScreenOpWriteSpan, ScreenOpClearToEOL:
			addRange(op.Row, op.Row+1)
		case ScreenOpScrollRect, ScreenOpClearRect:
			addRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
		case ScreenOpCopyRect:
			addRange(op.DstY, op.DstY+op.Src.Height)
		case ScreenOpResize:
			addRange(0, int(op.Size.Rows))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

type ListResult struct {
	Terminals []TerminalInfo `json:"terminals"`
}

func EncodeScreenUpdatePayload(update ScreenUpdate) ([]byte, error) {
	return encodeScreenUpdatePayloadBinary(NormalizeScreenUpdate(update))
}

func trimCellsForScreenUpdateWire(row []Cell) []Cell {
	if len(row) == 0 {
		return nil
	}
	last := -1
	for i, cell := range row {
		if cellNeedsWireEncoding(cell) {
			last = i
			if cell.Width > 1 {
				last = maxInt(last, minInt(len(row)-1, i+cell.Width-1))
			}
		}
	}
	if last < 0 {
		return nil
	}
	return row[:last+1]
}

func cellNeedsWireEncoding(cell Cell) bool {
	if cell.Style != (CellStyle{}) {
		return true
	}
	if cell.Width > 1 {
		return true
	}
	if cell.Content == "" {
		return false
	}
	return strings.TrimSpace(cell.Content) != ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func DecodeScreenUpdatePayload(payload []byte) (ScreenUpdate, error) {
	if len(payload) == 0 {
		return ScreenUpdate{}, nil
	}
	if len(payload) < len(screenUpdatePayloadMagicV2) {
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	var (
		update ScreenUpdate
		err    error
	)
	switch string(payload[:len(screenUpdatePayloadMagicV2)]) {
	case screenUpdatePayloadMagicV2:
		update, err = decodeScreenUpdatePayloadBinaryV2(payload)
	case screenUpdatePayloadMagicV3:
		update, err = decodeScreenUpdatePayloadBinaryV3(payload)
	case screenUpdatePayloadMagicV4:
		update, err = decodeScreenUpdatePayloadBinaryV4(payload)
	default:
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	if err != nil {
		return ScreenUpdate{}, err
	}
	return NormalizeScreenUpdate(update), nil
}

const (
	screenUpdatePayloadMagicV2 = "TSU2"
	screenUpdatePayloadMagicV3 = "TSU3"
	screenUpdatePayloadMagicV4 = "TSU4"
	screenUpdatePayloadMagic   = screenUpdatePayloadMagicV3
)

const (
	screenUpdateFlagFullReplace uint8 = 1 << iota
	screenUpdateFlagResetScrollback
	screenUpdateFlagHasTitle
	screenUpdateFlagHasScreenScroll
)

type screenUpdateEncoder struct {
	buf []byte
}

func encodeScreenUpdatePayloadBinary(update ScreenUpdate) ([]byte, error) {
	if !update.FullReplace && len(update.Ops) > 0 {
		v4Payload, err := encodeScreenUpdatePayloadBinaryV4(update)
		if err != nil {
			return nil, err
		}
		if !screenUpdateCanFallbackToV3(update) {
			return v4Payload, nil
		}
		v3Payload, err := encodeScreenUpdatePayloadBinaryV3(update)
		if err != nil {
			return nil, err
		}
		if len(v3Payload) <= len(v4Payload) {
			return v3Payload, nil
		}
		return v4Payload, nil
	}
	return encodeScreenUpdatePayloadBinaryV3(update)
}

func encodeScreenUpdatePayloadBinaryV3(update ScreenUpdate) ([]byte, error) {
	styles, styleIndex := collectScreenUpdateStyles(update)
	enc := screenUpdateEncoder{buf: make([]byte, 0, 256)}
	enc.appendBytes([]byte(screenUpdatePayloadMagic))
	flags := uint8(0)
	if update.FullReplace {
		flags |= screenUpdateFlagFullReplace
	}
	if update.ResetScrollback {
		flags |= screenUpdateFlagResetScrollback
	}
	if update.Title != "" {
		flags |= screenUpdateFlagHasTitle
	}
	if update.ScreenScroll != 0 {
		flags |= screenUpdateFlagHasScreenScroll
	}
	enc.appendByte(flags)
	enc.appendUint16(update.Size.Cols)
	enc.appendUint16(update.Size.Rows)
	if flags&screenUpdateFlagHasScreenScroll != 0 {
		enc.appendInt32(int32(update.ScreenScroll))
	}
	if flags&screenUpdateFlagHasTitle != 0 {
		enc.appendString(update.Title)
	}
	enc.appendUint16(encodeTerminalModesMask(update.Modes))
	enc.appendInt32(int32(update.Cursor.Row))
	enc.appendInt32(int32(update.Cursor.Col))
	enc.appendByte(boolByte(update.Cursor.Visible))
	enc.appendByte(encodeCursorShape(update.Cursor.Shape))
	enc.appendByte(boolByte(update.Cursor.Blink))
	enc.appendUvarint(uint64(maxInt(0, len(styles)-1)))
	for _, style := range styles[1:] {
		enc.appendCellStyle(style)
	}
	if update.FullReplace {
		enc.appendByte(boolByte(update.Screen.IsAlternateScreen))
		enc.appendRows(update.Screen.Cells, styleIndex)
		enc.appendTimeSlice(update.ScreenTimestamps)
		enc.appendStringSlice(update.ScreenRowKinds)
	}
	enc.appendUvarint(uint64(len(update.ChangedSpans)))
	for _, span := range update.ChangedSpans {
		if !isValidScreenSpanOp(span.Op) {
			return nil, fmt.Errorf("invalid screen span op %d", span.Op)
		}
		enc.appendUvarint(uint64(span.Row))
		enc.appendByte(byte(span.Op))
		enc.appendUvarint(uint64(span.ColStart))
		enc.appendTime(span.Timestamp)
		enc.appendString(span.RowKind)
		if !screenSpanOpHasCells(span.Op) {
			continue
		}
		if span.Op == ScreenSpanOpReplaceRow {
			enc.appendRow(span.Cells, styleIndex)
			continue
		}
		enc.appendCells(span.Cells, styleIndex)
	}
	enc.appendUvarint(uint64(maxInt(0, update.ScrollbackTrim)))
	enc.appendUvarint(uint64(len(update.ScrollbackAppend)))
	for _, row := range update.ScrollbackAppend {
		enc.appendTime(row.Timestamp)
		enc.appendString(row.RowKind)
		enc.appendRow(row.Cells, styleIndex)
	}
	return enc.buf, nil
}

func encodeScreenUpdatePayloadBinaryV4(update ScreenUpdate) ([]byte, error) {
	styles, styleIndex := collectScreenUpdateStyles(update)
	enc := screenUpdateEncoder{buf: make([]byte, 0, 256)}
	enc.appendBytes([]byte(screenUpdatePayloadMagicV4))
	flags := uint8(0)
	if update.FullReplace {
		flags |= screenUpdateFlagFullReplace
	}
	if update.ResetScrollback {
		flags |= screenUpdateFlagResetScrollback
	}
	if update.Title != "" {
		flags |= screenUpdateFlagHasTitle
	}
	if update.ScreenScroll != 0 {
		flags |= screenUpdateFlagHasScreenScroll
	}
	enc.appendByte(flags)
	enc.appendUint16(update.Size.Cols)
	enc.appendUint16(update.Size.Rows)
	if flags&screenUpdateFlagHasScreenScroll != 0 {
		enc.appendInt32(int32(update.ScreenScroll))
	}
	if flags&screenUpdateFlagHasTitle != 0 {
		enc.appendString(update.Title)
	}
	enc.appendUint16(encodeTerminalModesMask(update.Modes))
	enc.appendInt32(int32(update.Cursor.Row))
	enc.appendInt32(int32(update.Cursor.Col))
	enc.appendByte(boolByte(update.Cursor.Visible))
	enc.appendByte(encodeCursorShape(update.Cursor.Shape))
	enc.appendByte(boolByte(update.Cursor.Blink))
	enc.appendUvarint(uint64(maxInt(0, len(styles)-1)))
	for _, style := range styles[1:] {
		enc.appendCellStyle(style)
	}
	enc.appendUvarint(uint64(len(update.Ops)))
	for _, op := range update.Ops {
		if !isValidScreenOpCode(op.Code) {
			return nil, fmt.Errorf("invalid screen op %d", op.Code)
		}
		enc.appendByte(byte(op.Code))
		switch op.Code {
		case ScreenOpWriteSpan:
			enc.appendUvarint(uint64(op.Row))
			enc.appendUvarint(uint64(op.Col))
			enc.appendTime(op.Timestamp)
			enc.appendString(op.RowKind)
			enc.appendCells(op.Cells, styleIndex)
		case ScreenOpScrollRect:
			enc.appendScreenRect(op.Rect)
			enc.appendInt32(int32(op.Dx))
			enc.appendInt32(int32(op.Dy))
		case ScreenOpCopyRect:
			enc.appendScreenRect(op.Src)
			enc.appendInt32(int32(op.DstX))
			enc.appendInt32(int32(op.DstY))
		case ScreenOpClearRect:
			enc.appendScreenRect(op.Rect)
			enc.appendTime(op.Timestamp)
			enc.appendString(op.RowKind)
		case ScreenOpClearToEOL:
			enc.appendUvarint(uint64(op.Row))
			enc.appendUvarint(uint64(op.Col))
			enc.appendTime(op.Timestamp)
			enc.appendString(op.RowKind)
		case ScreenOpCursor:
			enc.appendInt32(int32(op.Cursor.Row))
			enc.appendInt32(int32(op.Cursor.Col))
			enc.appendByte(boolByte(op.Cursor.Visible))
			enc.appendByte(encodeCursorShape(op.Cursor.Shape))
			enc.appendByte(boolByte(op.Cursor.Blink))
		case ScreenOpModes:
			enc.appendUint16(encodeTerminalModesMask(op.Modes))
		case ScreenOpResize:
			enc.appendUint16(op.Size.Cols)
			enc.appendUint16(op.Size.Rows)
		case ScreenOpTitle:
			enc.appendString(op.Title)
		}
	}
	enc.appendUvarint(uint64(maxInt(0, update.ScrollbackTrim)))
	enc.appendUvarint(uint64(len(update.ScrollbackAppend)))
	for _, row := range update.ScrollbackAppend {
		enc.appendTime(row.Timestamp)
		enc.appendString(row.RowKind)
		enc.appendRow(row.Cells, styleIndex)
	}
	return enc.buf, nil
}

func screenUpdateCanFallbackToV3(update ScreenUpdate) bool {
	if update.FullReplace {
		return true
	}
	hasSpanFallback := len(update.ChangedSpans) > 0 || len(update.ChangedRows) > 0
	for _, op := range update.Ops {
		switch op.Code {
		case ScreenOpCursor, ScreenOpModes, ScreenOpResize, ScreenOpTitle:
			continue
		case ScreenOpScrollRect:
			if screenUpdateScrollRectRepresentableInV3(update, op) {
				continue
			}
			if hasSpanFallback {
				continue
			}
			return false
		default:
			if hasSpanFallback {
				continue
			}
			return false
		}
	}
	return true
}

func screenUpdateScrollRectRepresentableInV3(update ScreenUpdate, op ScreenOp) bool {
	if op.Code != ScreenOpScrollRect || op.Dx != 0 || update.ScreenScroll != -op.Dy {
		return false
	}
	if update.Size.Cols == 0 || update.Size.Rows == 0 {
		return false
	}
	return op.Rect.X == 0 &&
		op.Rect.Y == 0 &&
		op.Rect.Width >= int(update.Size.Cols) &&
		op.Rect.Height >= int(update.Size.Rows)
}

func encodeScreenUpdatePayloadBinaryV2(update ScreenUpdate) ([]byte, error) {
	styles, styleIndex := collectScreenUpdateStyles(update)
	enc := screenUpdateEncoder{buf: make([]byte, 0, 256)}
	enc.appendBytes([]byte(screenUpdatePayloadMagicV2))
	flags := uint8(0)
	if update.FullReplace {
		flags |= screenUpdateFlagFullReplace
	}
	if update.ResetScrollback {
		flags |= screenUpdateFlagResetScrollback
	}
	if update.Title != "" {
		flags |= screenUpdateFlagHasTitle
	}
	if update.ScreenScroll != 0 {
		flags |= screenUpdateFlagHasScreenScroll
	}
	enc.appendByte(flags)
	enc.appendUint16(update.Size.Cols)
	enc.appendUint16(update.Size.Rows)
	if flags&screenUpdateFlagHasScreenScroll != 0 {
		enc.appendInt32(int32(update.ScreenScroll))
	}
	if flags&screenUpdateFlagHasTitle != 0 {
		enc.appendString(update.Title)
	}
	enc.appendUint16(encodeTerminalModesMask(update.Modes))
	enc.appendInt32(int32(update.Cursor.Row))
	enc.appendInt32(int32(update.Cursor.Col))
	enc.appendByte(boolByte(update.Cursor.Visible))
	enc.appendByte(encodeCursorShape(update.Cursor.Shape))
	enc.appendByte(boolByte(update.Cursor.Blink))
	enc.appendUvarint(uint64(maxInt(0, len(styles)-1)))
	for _, style := range styles[1:] {
		enc.appendCellStyle(style)
	}
	if update.FullReplace {
		enc.appendByte(boolByte(update.Screen.IsAlternateScreen))
		enc.appendRows(update.Screen.Cells, styleIndex)
		enc.appendTimeSlice(update.ScreenTimestamps)
		enc.appendStringSlice(update.ScreenRowKinds)
	}
	enc.appendUvarint(uint64(len(update.ChangedRows)))
	for _, row := range update.ChangedRows {
		enc.appendUvarint(uint64(row.Row))
		enc.appendTime(row.Timestamp)
		enc.appendString(row.RowKind)
		enc.appendRow(row.Cells, styleIndex)
	}
	enc.appendUvarint(uint64(maxInt(0, update.ScrollbackTrim)))
	enc.appendUvarint(uint64(len(update.ScrollbackAppend)))
	for _, row := range update.ScrollbackAppend {
		enc.appendTime(row.Timestamp)
		enc.appendString(row.RowKind)
		enc.appendRow(row.Cells, styleIndex)
	}
	return enc.buf, nil
}

func collectScreenUpdateStyles(update ScreenUpdate) ([]CellStyle, map[CellStyle]uint64) {
	styles := []CellStyle{{}}
	index := map[CellStyle]uint64{{}: 0}
	addCells := func(cells []Cell) {
		for _, cell := range cells {
			if _, ok := index[cell.Style]; ok {
				continue
			}
			index[cell.Style] = uint64(len(styles))
			styles = append(styles, cell.Style)
		}
	}
	addRow := func(row []Cell) {
		addCells(trimCellsForScreenUpdateWire(row))
	}
	for _, row := range update.Screen.Cells {
		addRow(row)
	}
	for _, span := range update.ChangedSpans {
		if !screenSpanOpHasCells(span.Op) {
			continue
		}
		if span.Op == ScreenSpanOpReplaceRow {
			addRow(span.Cells)
			continue
		}
		addCells(span.Cells)
	}
	for _, op := range update.Ops {
		if op.Code != ScreenOpWriteSpan {
			continue
		}
		addCells(op.Cells)
	}
	for _, row := range update.ChangedRows {
		addRow(row.Cells)
	}
	for _, row := range update.ScrollbackAppend {
		addRow(row.Cells)
	}
	return styles, index
}

func decodeScreenUpdatePayloadBinaryV3(payload []byte) (ScreenUpdate, error) {
	dec := screenUpdateDecoder{data: payload}
	if !dec.consumeMagic(screenUpdatePayloadMagic) {
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	flags, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cols, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	rows, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update := ScreenUpdate{
		FullReplace:     flags&screenUpdateFlagFullReplace != 0,
		ResetScrollback: flags&screenUpdateFlagResetScrollback != 0,
		Size:            Size{Cols: cols, Rows: rows},
	}
	if flags&screenUpdateFlagHasScreenScroll != 0 {
		scroll, err := dec.readInt32()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenScroll = int(scroll)
	}
	if flags&screenUpdateFlagHasTitle != 0 {
		update.Title, err = dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
	}
	modeMask, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorRow, err := dec.readInt32()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorCol, err := dec.readInt32()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorVisible, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorShape, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorBlink, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.Modes = decodeTerminalModesMask(modeMask)
	update.Cursor = CursorState{
		Row:     int(cursorRow),
		Col:     int(cursorCol),
		Visible: cursorVisible != 0,
		Shape:   decodeCursorShape(cursorShape),
		Blink:   cursorBlink != 0,
	}
	styleCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	styles := make([]CellStyle, 1, int(styleCount)+1)
	styles[0] = CellStyle{}
	for i := uint64(0); i < styleCount; i++ {
		style, err := dec.readCellStyle()
		if err != nil {
			return ScreenUpdate{}, err
		}
		styles = append(styles, style)
	}
	if update.FullReplace {
		screenAlt, err := dec.readByte()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.Screen.IsAlternateScreen = screenAlt != 0
		update.Screen.Cells, err = dec.readRows(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenTimestamps, err = dec.readTimeSlice()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenRowKinds, err = dec.readStringSlice()
		if err != nil {
			return ScreenUpdate{}, err
		}
	}
	changedCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ChangedSpans = make([]ScreenSpanUpdate, 0, int(changedCount))
	for i := uint64(0); i < changedCount; i++ {
		rowIndex, err := dec.readUvarint()
		if err != nil {
			return ScreenUpdate{}, err
		}
		opRaw, err := dec.readByte()
		if err != nil {
			return ScreenUpdate{}, err
		}
		op := ScreenSpanOp(opRaw)
		if !isValidScreenSpanOp(op) {
			return ScreenUpdate{}, fmt.Errorf("invalid screen span op %d", opRaw)
		}
		colStart, err := dec.readUvarint()
		if err != nil {
			return ScreenUpdate{}, err
		}
		ts, err := dec.readTime()
		if err != nil {
			return ScreenUpdate{}, err
		}
		kind, err := dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
		var cells []Cell
		if screenSpanOpHasCells(op) {
			if op == ScreenSpanOpReplaceRow {
				cells, err = dec.readRow(styles)
			} else {
				cells, err = dec.readCells(styles)
			}
			if err != nil {
				return ScreenUpdate{}, err
			}
		}
		update.ChangedSpans = append(update.ChangedSpans, ScreenSpanUpdate{
			Row:       int(rowIndex),
			ColStart:  int(colStart),
			Cells:     cells,
			Op:        op,
			Timestamp: ts,
			RowKind:   kind,
		})
	}
	scrollbackTrim, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ScrollbackTrim = int(scrollbackTrim)
	appendCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ScrollbackAppend = make([]ScrollbackRowAppend, 0, int(appendCount))
	for i := uint64(0); i < appendCount; i++ {
		ts, err := dec.readTime()
		if err != nil {
			return ScreenUpdate{}, err
		}
		kind, err := dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
		cells, err := dec.readRow(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScrollbackAppend = append(update.ScrollbackAppend, ScrollbackRowAppend{
			Cells:     cells,
			Timestamp: ts,
			RowKind:   kind,
		})
	}
	if dec.off != len(dec.data) {
		return ScreenUpdate{}, fmt.Errorf("trailing bytes in screen update payload")
	}
	if !update.FullReplace {
		update.Screen.IsAlternateScreen = update.Modes.AlternateScreen
	}
	return update, nil
}

func decodeScreenUpdatePayloadBinaryV4(payload []byte) (ScreenUpdate, error) {
	dec := screenUpdateDecoder{data: payload}
	if !dec.consumeMagic(screenUpdatePayloadMagicV4) {
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	flags, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cols, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	rows, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update := ScreenUpdate{
		FullReplace:     flags&screenUpdateFlagFullReplace != 0,
		ResetScrollback: flags&screenUpdateFlagResetScrollback != 0,
		Size:            Size{Cols: cols, Rows: rows},
	}
	if flags&screenUpdateFlagHasScreenScroll != 0 {
		scroll, err := dec.readInt32()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenScroll = int(scroll)
	}
	if flags&screenUpdateFlagHasTitle != 0 {
		update.Title, err = dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
	}
	modeMask, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorRow, err := dec.readInt32()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorCol, err := dec.readInt32()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorVisible, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorShape, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorBlink, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.Modes = decodeTerminalModesMask(modeMask)
	update.Cursor = CursorState{
		Row:     int(cursorRow),
		Col:     int(cursorCol),
		Visible: cursorVisible != 0,
		Shape:   decodeCursorShape(cursorShape),
		Blink:   cursorBlink != 0,
	}
	styleCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	styles := make([]CellStyle, 1, int(styleCount)+1)
	styles[0] = CellStyle{}
	for i := uint64(0); i < styleCount; i++ {
		style, err := dec.readCellStyle()
		if err != nil {
			return ScreenUpdate{}, err
		}
		styles = append(styles, style)
	}
	opCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.Ops = make([]ScreenOp, 0, int(opCount))
	for i := uint64(0); i < opCount; i++ {
		opCodeRaw, err := dec.readByte()
		if err != nil {
			return ScreenUpdate{}, err
		}
		opCode := ScreenOpCode(opCodeRaw)
		if !isValidScreenOpCode(opCode) {
			return ScreenUpdate{}, fmt.Errorf("invalid screen op %d", opCodeRaw)
		}
		op := ScreenOp{Code: opCode}
		switch opCode {
		case ScreenOpWriteSpan:
			rowIndex, err := dec.readUvarint()
			if err != nil {
				return ScreenUpdate{}, err
			}
			colIndex, err := dec.readUvarint()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Row = int(rowIndex)
			op.Col = int(colIndex)
			op.Timestamp, err = dec.readTime()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.RowKind, err = dec.readString()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Cells, err = dec.readCells(styles)
			if err != nil {
				return ScreenUpdate{}, err
			}
		case ScreenOpScrollRect:
			op.Rect, err = dec.readScreenRect()
			if err != nil {
				return ScreenUpdate{}, err
			}
			dx, err := dec.readInt32()
			if err != nil {
				return ScreenUpdate{}, err
			}
			dy, err := dec.readInt32()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Dx = int(dx)
			op.Dy = int(dy)
		case ScreenOpCopyRect:
			op.Src, err = dec.readScreenRect()
			if err != nil {
				return ScreenUpdate{}, err
			}
			dstX, err := dec.readInt32()
			if err != nil {
				return ScreenUpdate{}, err
			}
			dstY, err := dec.readInt32()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.DstX = int(dstX)
			op.DstY = int(dstY)
		case ScreenOpClearRect:
			op.Rect, err = dec.readScreenRect()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Timestamp, err = dec.readTime()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.RowKind, err = dec.readString()
			if err != nil {
				return ScreenUpdate{}, err
			}
		case ScreenOpClearToEOL:
			rowIndex, err := dec.readUvarint()
			if err != nil {
				return ScreenUpdate{}, err
			}
			colIndex, err := dec.readUvarint()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Row = int(rowIndex)
			op.Col = int(colIndex)
			op.Timestamp, err = dec.readTime()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.RowKind, err = dec.readString()
			if err != nil {
				return ScreenUpdate{}, err
			}
		case ScreenOpCursor:
			row, err := dec.readInt32()
			if err != nil {
				return ScreenUpdate{}, err
			}
			col, err := dec.readInt32()
			if err != nil {
				return ScreenUpdate{}, err
			}
			visible, err := dec.readByte()
			if err != nil {
				return ScreenUpdate{}, err
			}
			shape, err := dec.readByte()
			if err != nil {
				return ScreenUpdate{}, err
			}
			blink, err := dec.readByte()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Cursor = CursorState{
				Row:     int(row),
				Col:     int(col),
				Visible: visible != 0,
				Shape:   decodeCursorShape(shape),
				Blink:   blink != 0,
			}
		case ScreenOpModes:
			mask, err := dec.readUint16()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Modes = decodeTerminalModesMask(mask)
		case ScreenOpResize:
			cols, err := dec.readUint16()
			if err != nil {
				return ScreenUpdate{}, err
			}
			rows, err := dec.readUint16()
			if err != nil {
				return ScreenUpdate{}, err
			}
			op.Size = Size{Cols: cols, Rows: rows}
		case ScreenOpTitle:
			op.Title, err = dec.readString()
			if err != nil {
				return ScreenUpdate{}, err
			}
		}
		update.Ops = append(update.Ops, op)
	}
	scrollbackTrim, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ScrollbackTrim = int(scrollbackTrim)
	appendCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ScrollbackAppend = make([]ScrollbackRowAppend, 0, int(appendCount))
	for i := uint64(0); i < appendCount; i++ {
		ts, err := dec.readTime()
		if err != nil {
			return ScreenUpdate{}, err
		}
		kind, err := dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
		cells, err := dec.readRow(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScrollbackAppend = append(update.ScrollbackAppend, ScrollbackRowAppend{
			Cells:     cells,
			Timestamp: ts,
			RowKind:   kind,
		})
	}
	if dec.off != len(dec.data) {
		return ScreenUpdate{}, fmt.Errorf("trailing bytes in screen update payload")
	}
	if !update.FullReplace {
		update.Screen.IsAlternateScreen = update.Modes.AlternateScreen
	}
	return update, nil
}

func decodeScreenUpdatePayloadBinaryV2(payload []byte) (ScreenUpdate, error) {
	dec := screenUpdateDecoder{data: payload}
	if !dec.consumeMagic(screenUpdatePayloadMagicV2) {
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	flags, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cols, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	rows, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update := ScreenUpdate{
		FullReplace:     flags&screenUpdateFlagFullReplace != 0,
		ResetScrollback: flags&screenUpdateFlagResetScrollback != 0,
		Size:            Size{Cols: cols, Rows: rows},
	}
	if flags&screenUpdateFlagHasScreenScroll != 0 {
		scroll, err := dec.readInt32()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenScroll = int(scroll)
	}
	if flags&screenUpdateFlagHasTitle != 0 {
		update.Title, err = dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
	}
	modeMask, err := dec.readUint16()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorRow, err := dec.readInt32()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorCol, err := dec.readInt32()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorVisible, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorShape, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	cursorBlink, err := dec.readByte()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.Modes = decodeTerminalModesMask(modeMask)
	update.Cursor = CursorState{
		Row:     int(cursorRow),
		Col:     int(cursorCol),
		Visible: cursorVisible != 0,
		Shape:   decodeCursorShape(cursorShape),
		Blink:   cursorBlink != 0,
	}
	styleCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	styles := make([]CellStyle, 1, int(styleCount)+1)
	styles[0] = CellStyle{}
	for i := uint64(0); i < styleCount; i++ {
		style, err := dec.readCellStyle()
		if err != nil {
			return ScreenUpdate{}, err
		}
		styles = append(styles, style)
	}
	if update.FullReplace {
		screenAlt, err := dec.readByte()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.Screen.IsAlternateScreen = screenAlt != 0
		update.Screen.Cells, err = dec.readRows(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenTimestamps, err = dec.readTimeSlice()
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScreenRowKinds, err = dec.readStringSlice()
		if err != nil {
			return ScreenUpdate{}, err
		}
	}
	changedCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ChangedRows = make([]ScreenRowUpdate, 0, int(changedCount))
	for i := uint64(0); i < changedCount; i++ {
		rowIndex, err := dec.readUvarint()
		if err != nil {
			return ScreenUpdate{}, err
		}
		ts, err := dec.readTime()
		if err != nil {
			return ScreenUpdate{}, err
		}
		kind, err := dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
		cells, err := dec.readRow(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ChangedRows = append(update.ChangedRows, ScreenRowUpdate{
			Row:       int(rowIndex),
			Cells:     cells,
			Timestamp: ts,
			RowKind:   kind,
		})
	}
	scrollbackTrim, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ScrollbackTrim = int(scrollbackTrim)
	appendCount, err := dec.readUvarint()
	if err != nil {
		return ScreenUpdate{}, err
	}
	update.ScrollbackAppend = make([]ScrollbackRowAppend, 0, int(appendCount))
	for i := uint64(0); i < appendCount; i++ {
		ts, err := dec.readTime()
		if err != nil {
			return ScreenUpdate{}, err
		}
		kind, err := dec.readString()
		if err != nil {
			return ScreenUpdate{}, err
		}
		cells, err := dec.readRow(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScrollbackAppend = append(update.ScrollbackAppend, ScrollbackRowAppend{
			Cells:     cells,
			Timestamp: ts,
			RowKind:   kind,
		})
	}
	if dec.off != len(dec.data) {
		return ScreenUpdate{}, fmt.Errorf("trailing bytes in screen update payload")
	}
	if !update.FullReplace {
		update.Screen.IsAlternateScreen = update.Modes.AlternateScreen
	}
	return update, nil
}

type screenUpdateDecoder struct {
	data []byte
	off  int
}

func (e *screenUpdateEncoder) appendBytes(value []byte) {
	e.buf = append(e.buf, value...)
}

func (e *screenUpdateEncoder) appendByte(value byte) {
	e.buf = append(e.buf, value)
}

func (e *screenUpdateEncoder) appendUint16(value uint16) {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	e.appendBytes(raw[:])
}

func (e *screenUpdateEncoder) appendInt32(value int32) {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], uint32(value))
	e.appendBytes(raw[:])
}

func (e *screenUpdateEncoder) appendInt64(value int64) {
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], uint64(value))
	e.appendBytes(raw[:])
}

func (e *screenUpdateEncoder) appendUvarint(value uint64) {
	var raw [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(raw[:], value)
	e.appendBytes(raw[:n])
}

func (e *screenUpdateEncoder) appendString(value string) {
	e.appendUvarint(uint64(len(value)))
	e.appendBytes([]byte(value))
}

func (e *screenUpdateEncoder) appendTime(value time.Time) {
	if value.IsZero() {
		e.appendInt64(0)
		return
	}
	e.appendInt64(value.UTC().UnixNano())
}

func (e *screenUpdateEncoder) appendTimeSlice(values []time.Time) {
	e.appendUvarint(uint64(len(values)))
	for _, value := range values {
		e.appendTime(value)
	}
}

func (e *screenUpdateEncoder) appendStringSlice(values []string) {
	e.appendUvarint(uint64(len(values)))
	for _, value := range values {
		e.appendString(value)
	}
}

func (e *screenUpdateEncoder) appendScreenRect(rect ScreenRect) {
	e.appendInt32(int32(rect.X))
	e.appendInt32(int32(rect.Y))
	e.appendInt32(int32(rect.Width))
	e.appendInt32(int32(rect.Height))
}

func (e *screenUpdateEncoder) appendCellStyle(style CellStyle) {
	e.appendString(style.FG)
	e.appendString(style.BG)
	mask := uint8(0)
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
	e.appendByte(mask)
}

func (e *screenUpdateEncoder) appendRows(rows [][]Cell, styleIndex map[CellStyle]uint64) {
	e.appendUvarint(uint64(len(rows)))
	for _, row := range rows {
		e.appendRow(row, styleIndex)
	}
}

func (e *screenUpdateEncoder) appendCells(cells []Cell, styleIndex map[CellStyle]uint64) {
	e.appendUvarint(uint64(len(cells)))
	for _, cell := range cells {
		e.appendUvarint(styleIndex[cell.Style])
		e.appendUvarint(uint64(cell.Width))
		e.appendString(cell.Content)
	}
}

func (e *screenUpdateEncoder) appendRow(row []Cell, styleIndex map[CellStyle]uint64) {
	e.appendCells(trimCellsForScreenUpdateWire(row), styleIndex)
}

func (d *screenUpdateDecoder) consumeMagic(magic string) bool {
	if len(d.data)-d.off < len(magic) {
		return false
	}
	if string(d.data[d.off:d.off+len(magic)]) != magic {
		return false
	}
	d.off += len(magic)
	return true
}

func (d *screenUpdateDecoder) readByte() (byte, error) {
	if d.off >= len(d.data) {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := d.data[d.off]
	d.off++
	return value, nil
}

func (d *screenUpdateDecoder) readUint16() (uint16, error) {
	if len(d.data)-d.off < 2 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := binary.LittleEndian.Uint16(d.data[d.off : d.off+2])
	d.off += 2
	return value, nil
}

func (d *screenUpdateDecoder) readInt32() (int32, error) {
	if len(d.data)-d.off < 4 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := int32(binary.LittleEndian.Uint32(d.data[d.off : d.off+4]))
	d.off += 4
	return value, nil
}

func (d *screenUpdateDecoder) readInt64() (int64, error) {
	if len(d.data)-d.off < 8 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	value := int64(binary.LittleEndian.Uint64(d.data[d.off : d.off+8]))
	d.off += 8
	return value, nil
}

func (d *screenUpdateDecoder) readUvarint() (uint64, error) {
	value, n := binary.Uvarint(d.data[d.off:])
	if n <= 0 {
		return 0, fmt.Errorf("invalid varint")
	}
	d.off += n
	return value, nil
}

func (d *screenUpdateDecoder) readString() (string, error) {
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

func (d *screenUpdateDecoder) readTime() (time.Time, error) {
	raw, err := d.readInt64()
	if err != nil {
		return time.Time{}, err
	}
	if raw == 0 {
		return time.Time{}, nil
	}
	return time.Unix(0, raw).UTC(), nil
}

func (d *screenUpdateDecoder) readTimeSlice() ([]time.Time, error) {
	count, err := d.readUvarint()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	out := make([]time.Time, count)
	for i := range out {
		out[i], err = d.readTime()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *screenUpdateDecoder) readStringSlice() ([]string, error) {
	count, err := d.readUvarint()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	out := make([]string, count)
	for i := range out {
		out[i], err = d.readString()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *screenUpdateDecoder) readScreenRect() (ScreenRect, error) {
	x, err := d.readInt32()
	if err != nil {
		return ScreenRect{}, err
	}
	y, err := d.readInt32()
	if err != nil {
		return ScreenRect{}, err
	}
	width, err := d.readInt32()
	if err != nil {
		return ScreenRect{}, err
	}
	height, err := d.readInt32()
	if err != nil {
		return ScreenRect{}, err
	}
	return ScreenRect{
		X:      int(x),
		Y:      int(y),
		Width:  int(width),
		Height: int(height),
	}, nil
}

func (d *screenUpdateDecoder) readCellStyle() (CellStyle, error) {
	fg, err := d.readString()
	if err != nil {
		return CellStyle{}, err
	}
	bg, err := d.readString()
	if err != nil {
		return CellStyle{}, err
	}
	mask, err := d.readByte()
	if err != nil {
		return CellStyle{}, err
	}
	return CellStyle{
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

func (d *screenUpdateDecoder) readRows(styles []CellStyle) ([][]Cell, error) {
	count, err := d.readUvarint()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	out := make([][]Cell, count)
	for i := range out {
		out[i], err = d.readRow(styles)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *screenUpdateDecoder) readCells(styles []CellStyle) ([]Cell, error) {
	count, err := d.readUvarint()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	out := make([]Cell, count)
	for i := range out {
		styleID, err := d.readUvarint()
		if err != nil {
			return nil, err
		}
		if styleID >= uint64(len(styles)) {
			return nil, fmt.Errorf("invalid style id %d", styleID)
		}
		width, err := d.readUvarint()
		if err != nil {
			return nil, err
		}
		content, err := d.readString()
		if err != nil {
			return nil, err
		}
		out[i] = Cell{
			Content: content,
			Width:   int(width),
			Style:   styles[styleID],
		}
	}
	return out, nil
}

func (d *screenUpdateDecoder) readRow(styles []CellStyle) ([]Cell, error) {
	return d.readCells(styles)
}

func encodeTerminalModesMask(modes TerminalModes) uint16 {
	var mask uint16
	if modes.AlternateScreen {
		mask |= 1 << 0
	}
	if modes.AlternateScroll {
		mask |= 1 << 1
	}
	if modes.MouseTracking {
		mask |= 1 << 2
	}
	if modes.MouseX10 {
		mask |= 1 << 3
	}
	if modes.MouseNormal {
		mask |= 1 << 4
	}
	if modes.MouseButtonEvent {
		mask |= 1 << 5
	}
	if modes.MouseAnyEvent {
		mask |= 1 << 6
	}
	if modes.MouseSGR {
		mask |= 1 << 7
	}
	if modes.BracketedPaste {
		mask |= 1 << 8
	}
	if modes.ApplicationCursor {
		mask |= 1 << 9
	}
	if modes.AutoWrap {
		mask |= 1 << 10
	}
	return mask
}

func decodeTerminalModesMask(mask uint16) TerminalModes {
	return TerminalModes{
		AlternateScreen:   mask&(1<<0) != 0,
		AlternateScroll:   mask&(1<<1) != 0,
		MouseTracking:     mask&(1<<2) != 0,
		MouseX10:          mask&(1<<3) != 0,
		MouseNormal:       mask&(1<<4) != 0,
		MouseButtonEvent:  mask&(1<<5) != 0,
		MouseAnyEvent:     mask&(1<<6) != 0,
		MouseSGR:          mask&(1<<7) != 0,
		BracketedPaste:    mask&(1<<8) != 0,
		ApplicationCursor: mask&(1<<9) != 0,
		AutoWrap:          mask&(1<<10) != 0,
	}
}

func encodeCursorShape(shape string) byte {
	switch shape {
	case "underline":
		return 1
	case "bar":
		return 2
	default:
		return 0
	}
}

func decodeCursorShape(shape byte) string {
	switch shape {
	case 1:
		return "underline"
	case 2:
		return "bar"
	default:
		return "block"
	}
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

type Cell struct {
	Content string    `json:"r,omitempty"`
	Width   int       `json:"w,omitempty"`
	Style   CellStyle `json:"s,omitempty"`
}

type CellStyle struct {
	FG            string `json:"fg,omitempty"`
	BG            string `json:"bg,omitempty"`
	Bold          bool   `json:"b,omitempty"`
	Italic        bool   `json:"i,omitempty"`
	Underline     bool   `json:"u,omitempty"`
	Blink         bool   `json:"k,omitempty"`
	Reverse       bool   `json:"rv,omitempty"`
	Strikethrough bool   `json:"st,omitempty"`
}

type CursorState struct {
	Row     int    `json:"row"`
	Col     int    `json:"col"`
	Visible bool   `json:"visible"`
	Shape   string `json:"shape,omitempty"`
	Blink   bool   `json:"blink,omitempty"`
}

type TerminalModes struct {
	AlternateScreen   bool `json:"alternate_screen"`
	AlternateScroll   bool `json:"alternate_scroll,omitempty"`
	MouseTracking     bool `json:"mouse_tracking"`
	MouseX10          bool `json:"mouse_x10,omitempty"`
	MouseNormal       bool `json:"mouse_normal,omitempty"`
	MouseButtonEvent  bool `json:"mouse_button_event,omitempty"`
	MouseAnyEvent     bool `json:"mouse_any_event,omitempty"`
	MouseSGR          bool `json:"mouse_sgr,omitempty"`
	BracketedPaste    bool `json:"bracketed_paste"`
	ApplicationCursor bool `json:"application_cursor"`
	AutoWrap          bool `json:"auto_wrap"`
}

type ScreenData struct {
	Cells             [][]Cell `json:"-"`
	IsAlternateScreen bool     `json:"-"`
}

const SnapshotRowKindRestart = "restart"
const SnapshotRowKindWrapped = "wrapped"

type Snapshot struct {
	TerminalID           string        `json:"terminal_id"`
	Size                 Size          `json:"size"`
	Screen               ScreenData    `json:"screen"`
	Scrollback           [][]Cell      `json:"scrollback,omitempty"`
	ScreenTimestamps     []time.Time   `json:"screen_timestamps,omitempty"`
	ScrollbackTimestamps []time.Time   `json:"scrollback_timestamps,omitempty"`
	ScreenRowKinds       []string      `json:"screen_row_kinds,omitempty"`
	ScrollbackRowKinds   []string      `json:"scrollback_row_kinds,omitempty"`
	Cursor               CursorState   `json:"cursor"`
	Modes                TerminalModes `json:"modes"`
	Timestamp            time.Time     `json:"timestamp"`
}

type SessionOp = workbenchops.Op

type SessionInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Revision  uint64    `json:"revision"`
}

type ViewInfo struct {
	ViewID              string    `json:"view_id"`
	SessionID           string    `json:"session_id"`
	ClientID            string    `json:"client_id"`
	ActiveWorkspaceName string    `json:"active_workspace_name,omitempty"`
	ActiveTabID         string    `json:"active_tab_id,omitempty"`
	FocusedPaneID       string    `json:"focused_pane_id,omitempty"`
	WindowCols          uint16    `json:"window_cols,omitempty"`
	WindowRows          uint16    `json:"window_rows,omitempty"`
	AttachedAt          time.Time `json:"attached_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type LeaseInfo struct {
	TerminalID string    `json:"terminal_id"`
	SessionID  string    `json:"session_id"`
	ViewID     string    `json:"view_id"`
	PaneID     string    `json:"pane_id"`
	AcquiredAt time.Time `json:"acquired_at"`
}

type SessionSnapshot struct {
	Session   SessionInfo       `json:"session"`
	Workbench *workbenchdoc.Doc `json:"workbench,omitempty"`
	View      *ViewInfo         `json:"view,omitempty"`
	Leases    []LeaseInfo       `json:"leases,omitempty"`
}

type CreateSessionParams struct {
	SessionID string `json:"session_id,omitempty"`
	Name      string `json:"name,omitempty"`
}

type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

type GetSessionParams struct {
	SessionID string `json:"session_id"`
}

type AttachSessionParams struct {
	SessionID  string `json:"session_id"`
	ClientID   string `json:"client_id,omitempty"`
	WindowCols uint16 `json:"window_cols,omitempty"`
	WindowRows uint16 `json:"window_rows,omitempty"`
}

type DetachSessionParams struct {
	SessionID string `json:"session_id"`
	ViewID    string `json:"view_id"`
}

type ApplySessionParams struct {
	SessionID    string      `json:"session_id"`
	ViewID       string      `json:"view_id,omitempty"`
	BaseRevision uint64      `json:"base_revision"`
	Ops          []SessionOp `json:"ops"`
}

type ReplaceSessionParams struct {
	SessionID    string            `json:"session_id"`
	ViewID       string            `json:"view_id,omitempty"`
	BaseRevision uint64            `json:"base_revision"`
	Workbench    *workbenchdoc.Doc `json:"workbench,omitempty"`
}

type UpdateSessionViewPatch struct {
	ActiveWorkspaceName string `json:"active_workspace_name,omitempty"`
	ActiveTabID         string `json:"active_tab_id,omitempty"`
	FocusedPaneID       string `json:"focused_pane_id,omitempty"`
	WindowCols          uint16 `json:"window_cols,omitempty"`
	WindowRows          uint16 `json:"window_rows,omitempty"`
}

type UpdateSessionViewParams struct {
	SessionID string                 `json:"session_id"`
	ViewID    string                 `json:"view_id"`
	View      UpdateSessionViewPatch `json:"view"`
}

type AcquireSessionLeaseParams struct {
	SessionID  string `json:"session_id"`
	ViewID     string `json:"view_id"`
	PaneID     string `json:"pane_id"`
	TerminalID string `json:"terminal_id"`
}

type ReleaseSessionLeaseParams struct {
	SessionID  string `json:"session_id"`
	ViewID     string `json:"view_id"`
	TerminalID string `json:"terminal_id"`
}

func (s *Snapshot) UnmarshalJSON(data []byte) error {
	type jsonStyle struct {
		FG            string `json:"fg,omitempty"`
		BG            string `json:"bg,omitempty"`
		Bold          bool   `json:"b,omitempty"`
		Italic        bool   `json:"i,omitempty"`
		Underline     bool   `json:"u,omitempty"`
		Blink         bool   `json:"k,omitempty"`
		Reverse       bool   `json:"rv,omitempty"`
		Strikethrough bool   `json:"st,omitempty"`
	}
	type jsonCell struct {
		Content string     `json:"r,omitempty"`
		Width   int        `json:"w,omitempty"`
		Style   *jsonStyle `json:"s,omitempty"`
	}
	type jsonRow struct {
		Cells []jsonCell `json:"cells,omitempty"`
	}
	type jsonScreen struct {
		IsAlternate bool      `json:"is_alternate"`
		Rows        []jsonRow `json:"rows"`
	}
	type jsonSnapshot struct {
		TerminalID           string        `json:"terminal_id"`
		Size                 Size          `json:"size"`
		Screen               jsonScreen    `json:"screen"`
		Scrollback           []jsonRow     `json:"scrollback"`
		ScreenTimestamps     []string      `json:"screen_timestamps,omitempty"`
		ScrollbackTimestamps []string      `json:"scrollback_timestamps,omitempty"`
		ScreenRowKinds       []string      `json:"screen_row_kinds,omitempty"`
		ScrollbackRowKinds   []string      `json:"scrollback_row_kinds,omitempty"`
		Cursor               CursorState   `json:"cursor"`
		Modes                TerminalModes `json:"modes"`
		Timestamp            time.Time     `json:"timestamp"`
	}

	var raw jsonSnapshot
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	convertRows := func(rows []jsonRow) [][]Cell {
		out := make([][]Cell, len(rows))
		for i, row := range rows {
			cells := make([]Cell, len(row.Cells))
			for j, cell := range row.Cells {
				cells[j] = Cell{Content: cell.Content, Width: cell.Width}
				if cell.Style != nil {
					cells[j].Style = CellStyle{
						FG:            cell.Style.FG,
						BG:            cell.Style.BG,
						Bold:          cell.Style.Bold,
						Italic:        cell.Style.Italic,
						Underline:     cell.Style.Underline,
						Blink:         cell.Style.Blink,
						Reverse:       cell.Style.Reverse,
						Strikethrough: cell.Style.Strikethrough,
					}
				}
			}
			out[i] = cells
		}
		return out
	}
	decodeRowTimestamps := func(raw []string) []time.Time {
		if len(raw) == 0 {
			return nil
		}
		out := make([]time.Time, len(raw))
		for i, value := range raw {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				continue
			}
			out[i] = parsed
		}
		return out
	}

	s.TerminalID = raw.TerminalID
	s.Size = raw.Size
	s.Screen = ScreenData{Cells: convertRows(raw.Screen.Rows), IsAlternateScreen: raw.Screen.IsAlternate}
	s.Scrollback = convertRows(raw.Scrollback)
	s.ScreenTimestamps = decodeRowTimestamps(raw.ScreenTimestamps)
	s.ScrollbackTimestamps = decodeRowTimestamps(raw.ScrollbackTimestamps)
	s.ScreenRowKinds = append([]string(nil), raw.ScreenRowKinds...)
	s.ScrollbackRowKinds = append([]string(nil), raw.ScrollbackRowKinds...)
	s.Cursor = raw.Cursor
	s.Modes = raw.Modes
	s.Timestamp = raw.Timestamp
	return nil
}

type ChannelAllocator struct {
	mu       sync.Mutex
	next     uint16
	freeList []uint16
}

func NewChannelAllocator() *ChannelAllocator {
	return &ChannelAllocator{}
}

func (a *ChannelAllocator) Alloc() (uint16, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n := len(a.freeList); n > 0 {
		ch := a.freeList[n-1]
		a.freeList = a.freeList[:n-1]
		return ch, nil
	}
	if a.next == ^uint16(0) {
		return 0, errors.New("protocol: no channels available")
	}
	a.next++
	if a.next == 0 {
		a.next++
	}
	return a.next, nil
}

func (a *ChannelAllocator) Free(ch uint16) {
	if ch == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.freeList = append(a.freeList, ch)
}
