package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var compactASCIIStrings = buildCompactASCIIStrings()

func buildCompactASCIIStrings() [128]string {
	var out [128]string
	for i := 0; i < len(out); i++ {
		out[i] = string(byte(i))
	}
	return out
}

type Hello struct {
	Version int
	Client  string
	Server  string
}

type Request struct {
	ID     uint64
	Method string
	Params []byte
}

type Response struct {
	ID     uint64
	Result []byte
}

type ProtocolError struct {
	Code    int
	Message string
}

type ErrorMessage struct {
	ID    uint64
	Error ProtocolError
}

type Size struct {
	Cols uint16
	Rows uint16
}

type TerminalInfo struct {
	ID                         string
	Name                       string
	Command                    []string
	Tags                       map[string]string
	Size                       Size
	State                      string
	CWD                        string
	LiveCWD                    string
	CreatedAt                  time.Time
	ExitCode                   *int
	ResizeOwnership            *ResizeOwnership
	ResizeOwnerAttachmentCount int
}

type CreateParams struct {
	Command            []string
	ID                 string
	Name               string
	Tags               map[string]string
	Size               Size
	Dir                string
	Env                []string
	ScrollbackSize     int
	ScrollbackMaxBytes int64
	ScrollbackMaxAge   time.Duration
}

type CreateResult struct {
	TerminalID string
	State      string
}

type GetParams struct {
	TerminalID string
}

type ResizeParams struct {
	TerminalID string
	Cols       uint16
	Rows       uint16
}

type EnsureResizeParams struct {
	TerminalID   string
	Channel      uint16
	Cols         uint16
	Rows         uint16
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type EnsureResizeResult struct {
	ResizeControl *ResizeControl
	Size          Size
	Resized       bool
}

type SetTagsParams struct {
	TerminalID string
	Tags       map[string]string
}

type SetMetadataParams struct {
	TerminalID string
	Name       string
	Tags       map[string]string
}

type AttachParams struct {
	TerminalID   string
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type AttachResult struct {
	Mode          string
	Channel       uint16
	ResizeControl *ResizeControl
}

type ResizeOwnership struct {
	OwnerAttachmentID string
	OwnerSurfaceID    string
	OwnerViewID       string
	OwnerRemoteAddr   string
	Size              Size
	SizeLocked        bool
	Epoch             uint64
}

const (
	ResizePolicyOwner    = "owner"
	ResizePolicyFollower = "follower"
)

const (
	ResizeControlReasonOwner      = "owner"
	ResizeControlReasonFollower   = "follower"
	ResizeControlReasonObserver   = "observer"
	ResizeControlReasonSizeLocked = "size_locked"
)

type ResizeControl struct {
	CanResize       bool
	Reason          string
	SizeLocked      bool
	SurfaceID       string
	OwnerSurfaceID  string
	OwnerViewID     string
	ResizeOwnership *ResizeOwnership
}

type EventType int

const (
	EventTerminalCreated         EventType = 1
	EventTerminalStateChanged    EventType = 2
	EventTerminalResized         EventType = 3
	EventTerminalRemoved         EventType = 4
	EventCollaboratorsRevoked    EventType = 5
	EventTerminalReadError       EventType = 6
	EventTerminalMetadataChanged EventType = 10
	EventStorageChanged          EventType = 11
)

type TerminalCreatedData struct {
	Name    string
	Command []string
	Size    Size
}

type TerminalStateChangedData struct {
	OldState string
	NewState string
	ExitCode *int
}

type TerminalResizedData struct {
	OldSize Size
	NewSize Size
}

type TerminalRemovedData struct {
	Reason string
}

type CollaboratorsRevokedData struct{}

type TerminalReadErrorData struct {
	Error string
}

type StorageScope string

const (
	StorageScopePublic  StorageScope = "public"
	StorageScopePrivate StorageScope = "private"
)

type StorageEntry struct {
	AppID     string
	Scope     StorageScope
	OwnerID   string
	Key       string
	Value     []byte
	Version   uint64
	UpdatedAt time.Time
}

type StorageGetParams struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
}

type StoragePutParams struct {
	AppID           string
	Scope           StorageScope
	OwnerID         string
	Key             string
	Value           []byte
	CheckVersion    bool
	ExpectedVersion uint64
}

type StorageDeleteParams struct {
	AppID           string
	Scope           StorageScope
	OwnerID         string
	Key             string
	CheckVersion    bool
	ExpectedVersion uint64
}

type StorageDeleteResult struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
	Deleted bool
	Version uint64
}

type StorageListParams struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Prefix  string
}

type StorageListResult struct {
	Entries []StorageEntry
}

type StorageChangedData struct {
	AppID   string
	Scope   StorageScope
	OwnerID string
	Key     string
	Version uint64
	Op      string
}

type Event struct {
	Type                 EventType
	TerminalID           string
	Timestamp            time.Time
	Created              *TerminalCreatedData
	StateChanged         *TerminalStateChangedData
	Resized              *TerminalResizedData
	Removed              *TerminalRemovedData
	CollaboratorsRevoked *CollaboratorsRevokedData
	ReadError            *TerminalReadErrorData
	Storage              *StorageChangedData
}

type DetachParams struct {
	TerminalID string
}

type EventsParams struct {
	TerminalID       string
	Types            []EventType
	StorageAppID     string
	StorageScope     StorageScope
	StorageOwnerID   string
	StorageKeyPrefix string
}

type SnapshotParams struct {
	TerminalID       string
	ScrollbackOffset int
	ScrollbackLimit  int
}

type GridViewportParams struct {
	TerminalID       string
	ScrollbackOffset int
	ScrollbackLimit  int
	Cols             int
}

type HistoryWindowParams struct {
	TerminalID   string
	BeforeOffset int
	Limit        int
	Cols         int
}

type ScreenRect struct {
	X      int
	Y      int
	Width  int
	Height int
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
	Code       ScreenOpCode
	Rect       ScreenRect
	Src        ScreenRect
	DstX       int
	DstY       int
	Dx         int
	Dy         int
	Row        int
	Col        int
	Cells      []Cell
	Cursor     CursorState
	Modes      TerminalModes
	Size       Size
	Title      string
	Timestamp  time.Time
	RowKind    string
	Wrapped    bool
	WrappedSet bool
}

type ScrollbackRowAppend struct {
	Cells      []Cell
	Timestamp  time.Time
	RowKind    string
	Wrapped    bool
	WrappedSet bool
}

type ScreenUpdate struct {
	FullReplace      bool
	ResetScrollback  bool
	Size             Size
	ScreenScroll     int
	Title            string
	Screen           ScreenData
	ScreenTimestamps []time.Time
	ScreenRowKinds   []string
	ScreenWrapped    []bool
	Ops              []ScreenOp
	ScrollbackTrim   int
	ScrollbackAppend []ScrollbackRowAppend
	Cursor           CursorState
	Modes            TerminalModes
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
		normalized.ScreenWrapped = normalizeScreenUpdateBoolSlice(normalized.ScreenWrapped, len(normalized.Screen.Cells))
	} else {
		normalized.Screen.IsAlternateScreen = normalized.Modes.AlternateScreen
	}
	normalized.Ops = normalizeScreenOps(normalized.Ops)
	normalized.ScrollbackAppend = normalizeScrollbackAppendWrapped(normalized.ScrollbackAppend)
	return normalized
}

func ClassifyScreenUpdate(update ScreenUpdate) ScreenUpdateClassification {
	changedRows := len(screenUpdateChangedRowsFromOps(update.Ops)) > 0
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

func normalizeScreenOp(op ScreenOp) ScreenOp {
	op.Wrapped = wrappedSet(op.WrappedSet, op.Wrapped)
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
		op.Wrapped = false
		op.WrappedSet = false
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
		op.Wrapped = false
		op.WrappedSet = false
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
		op.Wrapped = false
		op.WrappedSet = false
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
		op.Wrapped = false
		op.WrappedSet = false
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
		op.Wrapped = false
		op.WrappedSet = false
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
		op.Wrapped = false
		op.WrappedSet = false
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

func normalizeScreenUpdateBoolSlice(values []bool, size int) []bool {
	switch {
	case size <= 0:
		return nil
	case len(values) == size:
		return values
	case len(values) > size:
		return values[:size]
	default:
		normalized := make([]bool, size)
		copy(normalized, values)
		return normalized
	}
}

func isBlankFullReplaceScreenUpdate(update ScreenUpdate) bool {
	if !update.FullReplace || len(update.Ops) > 0 || len(update.ScrollbackAppend) > 0 {
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
	Terminals []TerminalInfo
}

func EncodeScreenUpdatePayload(update ScreenUpdate) ([]byte, error) {
	return encodeScreenUpdatePayloadBinary(NormalizeScreenUpdate(update))
}

func normalizeScrollbackAppendWrapped(rows []ScrollbackRowAppend) []ScrollbackRowAppend {
	for i := range rows {
		rows[i].Wrapped = wrappedSet(rows[i].WrappedSet, rows[i].Wrapped)
	}
	return rows
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
	if cell.LinkURL != "" || cell.LinkParams != "" {
		return true
	}
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
	if len(payload) < len(screenUpdatePayloadMagic) {
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	if string(payload[:len(screenUpdatePayloadMagic)]) != screenUpdatePayloadMagic {
		return ScreenUpdate{}, fmt.Errorf("invalid screen update payload magic")
	}
	update, err := decodeScreenUpdatePayloadBinary(payload)
	if err != nil {
		return ScreenUpdate{}, err
	}
	return NormalizeScreenUpdate(update), nil
}

const (
	screenUpdatePayloadMagic = "TSU7"
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
	return encodeScreenUpdatePayloadBinaryCurrent(update)
}

func encodeScreenUpdatePayloadBinaryCurrent(update ScreenUpdate) ([]byte, error) {
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
		enc.appendRowsPreserveTrailingBlankCells(update.Screen.Cells, styleIndex)
		enc.appendTimeSlice(update.ScreenTimestamps)
		enc.appendStringSlice(update.ScreenRowKinds)
		enc.appendBoolSlice(update.ScreenWrapped)
	}
	enc.appendUvarint(uint64(len(update.Ops)))
	for _, op := range update.Ops {
		op.Wrapped = wrappedSet(op.WrappedSet, op.Wrapped)
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
			enc.appendByte(boolByte(op.WrappedSet))
			if op.WrappedSet {
				enc.appendByte(boolByte(op.Wrapped))
			}
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
			enc.appendByte(boolByte(op.WrappedSet))
			if op.WrappedSet {
				enc.appendByte(boolByte(op.Wrapped))
			}
		case ScreenOpClearToEOL:
			enc.appendUvarint(uint64(op.Row))
			enc.appendUvarint(uint64(op.Col))
			enc.appendTime(op.Timestamp)
			enc.appendString(op.RowKind)
			enc.appendByte(boolByte(op.WrappedSet))
			if op.WrappedSet {
				enc.appendByte(boolByte(op.Wrapped))
			}
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
		row.Wrapped = wrappedSet(row.WrappedSet, row.Wrapped)
		enc.appendTime(row.Timestamp)
		enc.appendString(row.RowKind)
		enc.appendByte(boolByte(row.WrappedSet))
		if row.WrappedSet {
			enc.appendByte(boolByte(row.Wrapped))
		}
		enc.appendCells(row.Cells, styleIndex)
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
	for _, row := range update.Screen.Cells {
		addCells(row)
	}
	for _, op := range update.Ops {
		if op.Code != ScreenOpWriteSpan {
			continue
		}
		addCells(op.Cells)
	}
	for _, row := range update.ScrollbackAppend {
		addCells(row.Cells)
	}
	return styles, index
}

func decodeScreenUpdatePayloadBinary(payload []byte) (ScreenUpdate, error) {
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
		update.ScreenWrapped, err = dec.readBoolSlice()
		if err != nil {
			return ScreenUpdate{}, err
		}
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
			op.WrappedSet, op.Wrapped, err = dec.readWrapped()
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
			op.WrappedSet, op.Wrapped, err = dec.readWrapped()
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
			op.WrappedSet, op.Wrapped, err = dec.readWrapped()
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
		wrappedSet, wrapped, err := dec.readWrapped()
		if err != nil {
			return ScreenUpdate{}, err
		}
		cells, err := dec.readRow(styles)
		if err != nil {
			return ScreenUpdate{}, err
		}
		update.ScrollbackAppend = append(update.ScrollbackAppend, ScrollbackRowAppend{
			Cells:      cells,
			Timestamp:  ts,
			RowKind:    kind,
			Wrapped:    wrapped,
			WrappedSet: wrappedSet,
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

func (e *screenUpdateEncoder) appendBoolSlice(values []bool) {
	e.appendUvarint(uint64(len(values)))
	for _, value := range values {
		e.appendByte(boolByte(value))
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

func (e *screenUpdateEncoder) appendRowsPreserveTrailingBlankCells(rows [][]Cell, styleIndex map[CellStyle]uint64) {
	e.appendUvarint(uint64(len(rows)))
	for _, row := range rows {
		e.appendCells(row, styleIndex)
	}
}

func (e *screenUpdateEncoder) appendCells(cells []Cell, styleIndex map[CellStyle]uint64) {
	e.appendUvarint(uint64(len(cells)))
	for _, cell := range cells {
		e.appendUvarint(styleIndex[cell.Style])
		e.appendUvarint(uint64(cell.Width))
		e.appendString(cell.Content)
		e.appendString(cell.LinkURL)
		e.appendString(cell.LinkParams)
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

func (d *screenUpdateDecoder) readBoolSlice() ([]bool, error) {
	count, err := d.readUvarint()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	out := make([]bool, count)
	for i := range out {
		value, err := d.readByte()
		if err != nil {
			return nil, err
		}
		out[i] = value != 0
	}
	return out, nil
}

func (d *screenUpdateDecoder) readWrapped() (bool, bool, error) {
	rawSet, err := d.readByte()
	if err != nil {
		return false, false, err
	}
	wrappedSet := rawSet != 0
	if !wrappedSet {
		return false, false, nil
	}
	rawWrapped, err := d.readByte()
	if err != nil {
		return false, false, err
	}
	return true, rawWrapped != 0, nil
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
		linkURL, err := d.readString()
		if err != nil {
			return nil, err
		}
		linkParams, err := d.readString()
		if err != nil {
			return nil, err
		}
		out[i] = Cell{
			Content:    content,
			Width:      int(width),
			Style:      styles[styleID],
			LinkURL:    linkURL,
			LinkParams: linkParams,
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

func wrappedSet(wrappedSet bool, wrapped bool) bool {
	return wrappedSet && wrapped
}

type Cell struct {
	Content    string
	Width      int
	Style      CellStyle
	LinkURL    string
	LinkParams string
}

type CellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

type CursorState struct {
	Row     int
	Col     int
	Visible bool
	Shape   string
	Blink   bool
}

type TerminalModes struct {
	AlternateScreen   bool
	AlternateScroll   bool
	MouseTracking     bool
	MouseX10          bool
	MouseNormal       bool
	MouseButtonEvent  bool
	MouseAnyEvent     bool
	MouseSGR          bool
	BracketedPaste    bool
	ApplicationCursor bool
	AutoWrap          bool
}

type ScreenData struct {
	Cells             [][]Cell
	IsAlternateScreen bool
}

const SnapshotRowKindRestart = "restart"

const (
	RowOwnershipPersisted         = "persisted"
	RowOwnershipLiveTailReclaimed = "live-tail-reclaimed"
	RowOwnershipLiveTailLive      = "live-tail-live"
	RowOwnershipScreen            = "screen"
)

type Snapshot struct {
	TerminalID             string
	Size                   Size
	Screen                 ScreenData
	Scrollback             []CompactRow
	ScrollbackOffset       int
	ScrollbackTotal        int
	ScrollbackLogicalTotal int
	ScrollbackHasMore      bool
	ScrollbackLoadedRows   int
	HistoryGeneration      uint64
	ScrollbackFirstRowID   uint64
	ScrollbackLastRowID    uint64
	ScreenTimestamps       []time.Time
	ScrollbackTimestamps   []time.Time
	ScreenRowKinds         []string
	ScrollbackRowKinds     []string
	ScreenWrapped          []bool
	ScrollbackWrapped      []bool
	ScreenOwnership        []string
	ScrollbackOwnership    []string
	Cursor                 CursorState
	Modes                  TerminalModes
	Timestamp              time.Time
}

type GridViewport struct {
	TerminalID             string
	Size                   Size
	Rows                   []CompactRow
	ScrollbackOffset       int
	ScrollbackLimit        int
	ScrollbackTotal        int
	ScrollbackLogicalTotal int
	ScrollbackHasMore      bool
	LoadedRows             int
	HistoryGeneration      uint64
	FirstRowID             uint64
	LastRowID              uint64
	ScrollbackTimestamps   []time.Time
	ScrollbackRowKinds     []string
	ScrollbackWrapped      []bool
	RowOwnership           []string
	Timestamp              time.Time
}

type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
)

type HistoryLineSpan struct {
	StartRow      int
	EndRow        int
	RowKind       string
	ClippedBefore bool
	ClippedAfter  bool
}

type HistoryWindow struct {
	TerminalID    string
	Token         string
	Op            HistoryWindowOp
	Size          Size
	Rows          []CompactRow
	RowTimestamps []time.Time
	RowKinds      []string
	RowWrapped    []bool
	RowOwnership  []string
	Lines         []HistoryLineSpan
	BeforeOffset  int
	LoadedRows    int
	TotalRows     int
	LogicalTotal  int
	HasMore       bool
	Generation    uint64
	FirstRowID    uint64
	LastRowID     uint64
	Timestamp     time.Time
}

func (s CellStyle) isZero() bool {
	return s == CellStyle{}
}

type CompactRowStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

type CompactRowCell struct {
	Content    string
	Width      int
	Style      *CompactRowStyle
	LinkURL    string
	LinkParams string
}

type CompactRowRun struct {
	Text       string
	Style      *CompactRowStyle
	LinkURL    string
	LinkParams string
}

type CompactRow struct {
	Text  string
	Runs  []CompactRowRun
	Cells []CompactRowCell
}

func (r CompactRow) DecodeCells() []Cell {
	return decodeCompactRow(r.Text, r.Runs, r.Cells)
}

func CompactRowsToCells(rows []CompactRow) [][]Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = row.DecodeCells()
	}
	return out
}

func CompactRowsFromCells(rows [][]Cell) []CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CompactRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, CompactRowFromCells(row))
	}
	return out
}

func CompactRowsFromCellsPreserveTrailingBlankRows(rows [][]Cell) []CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CompactRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, CompactRowFromCellsPreserveTrailingBlankCells(row, true))
	}
	return out
}

func CompactRowFromCells(row []Cell) CompactRow {
	return CompactRowFromCellsPreserveTrailingBlankCells(row, false)
}

func CompactRowFromCellsPreserveTrailingBlankCells(row []Cell, preserveTrailingBlankCells bool) CompactRow {
	last := len(row)
	if !preserveTrailingBlankCells {
		for last > 0 {
			cell := row[last-1]
			if cell.Content != "" && strings.TrimSpace(cell.Content) != "" {
				break
			}
			if !cell.Style.isZero() {
				break
			}
			if cell.LinkURL != "" || cell.LinkParams != "" {
				break
			}
			last--
		}
	}
	row = row[:last]
	if len(row) == 0 {
		return CompactRow{}
	}
	var text strings.Builder
	allSimple := true
	allPlain := true
	for _, cell := range row {
		cellText, ok := compactCellText(cell)
		if !ok {
			allSimple = false
			break
		}
		if !cell.Style.isZero() {
			allPlain = false
		}
		if cell.LinkURL != "" || cell.LinkParams != "" {
			allPlain = false
		}
		text.WriteString(cellText)
	}
	if allSimple && allPlain {
		return CompactRow{Text: text.String()}
	}
	if allSimple {
		runs := make([]CompactRowRun, 0, 4)
		var runText strings.Builder
		runStyle := row[0].Style
		runLinkURL := row[0].LinkURL
		runLinkParams := row[0].LinkParams
		flushRun := func() {
			if runText.Len() == 0 {
				return
			}
			runs = append(runs, CompactRowRun{
				Text:       runText.String(),
				Style:      compactRowStyleFromCellStyle(runStyle),
				LinkURL:    runLinkURL,
				LinkParams: runLinkParams,
			})
			runText.Reset()
		}
		for _, cell := range row {
			if cell.Style != runStyle || cell.LinkURL != runLinkURL || cell.LinkParams != runLinkParams {
				flushRun()
				runStyle = cell.Style
				runLinkURL = cell.LinkURL
				runLinkParams = cell.LinkParams
			}
			cellText, _ := compactCellText(cell)
			runText.WriteString(cellText)
		}
		flushRun()
		return CompactRow{Runs: runs}
	}
	cells := make([]CompactRowCell, 0, len(row))
	for _, cell := range row {
		cells = append(cells, CompactRowCell{
			Content:    cell.Content,
			Width:      compactCellWidth(cell),
			Style:      compactRowStyleFromCellStyle(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
	}
	return CompactRow{Cells: cells}
}

func CloneCompactRows(rows []CompactRow) []CompactRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CompactRow, len(rows))
	for i, row := range rows {
		out[i] = CloneCompactRow(row)
	}
	return out
}

func CloneCompactRow(row CompactRow) CompactRow {
	cloned := CompactRow{Text: row.Text}
	if len(row.Runs) > 0 {
		cloned.Runs = make([]CompactRowRun, len(row.Runs))
		for i, run := range row.Runs {
			cloned.Runs[i] = CompactRowRun{Text: run.Text, Style: cloneCompactRowStyle(run.Style), LinkURL: run.LinkURL, LinkParams: run.LinkParams}
		}
	}
	if len(row.Cells) > 0 {
		cloned.Cells = make([]CompactRowCell, len(row.Cells))
		for i, cell := range row.Cells {
			cloned.Cells[i] = CompactRowCell{Content: cell.Content, Width: cell.Width, Style: cloneCompactRowStyle(cell.Style), LinkURL: cell.LinkURL, LinkParams: cell.LinkParams}
		}
	}
	return cloned
}

func CompactRowEqual(left, right CompactRow) bool {
	if left.Text != right.Text || len(left.Runs) != len(right.Runs) || len(left.Cells) != len(right.Cells) {
		return false
	}
	for i := range left.Runs {
		if left.Runs[i].Text != right.Runs[i].Text || left.Runs[i].LinkURL != right.Runs[i].LinkURL || left.Runs[i].LinkParams != right.Runs[i].LinkParams || !compactRowStyleEqual(left.Runs[i].Style, right.Runs[i].Style) {
			return false
		}
	}
	for i := range left.Cells {
		if left.Cells[i].Content != right.Cells[i].Content || left.Cells[i].Width != right.Cells[i].Width || left.Cells[i].LinkURL != right.Cells[i].LinkURL || left.Cells[i].LinkParams != right.Cells[i].LinkParams || !compactRowStyleEqual(left.Cells[i].Style, right.Cells[i].Style) {
			return false
		}
	}
	return true
}

func compactCellText(cell Cell) (string, bool) {
	if cell.Width > 1 {
		return "", false
	}
	if cell.Content == "" {
		return " ", true
	}
	if utf8.RuneCountInString(cell.Content) != 1 {
		return "", false
	}
	return cell.Content, true
}

func compactCellWidth(cell Cell) int {
	if cell.Width > 1 {
		return cell.Width
	}
	return 0
}

func compactRowStyleFromCellStyle(style CellStyle) *CompactRowStyle {
	if style.isZero() {
		return nil
	}
	return &CompactRowStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func cloneCompactRowStyle(style *CompactRowStyle) *CompactRowStyle {
	if style == nil {
		return nil
	}
	cloned := *style
	return &cloned
}

func compactRowStyleEqual(left, right *CompactRowStyle) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func decodeCompactRow(text string, runs []CompactRowRun, cells []CompactRowCell) []Cell {
	if text != "" || (len(runs) == 0 && len(cells) == 0) {
		return decodeCompactRowText(text, CellStyle{})
	}
	if len(runs) > 0 {
		out := make([]Cell, 0, compactRowRunCellCount(runs))
		for _, run := range runs {
			out = append(out, decodeCompactRowTextWithLink(run.Text, compactRowCellStyle(run.Style), run.LinkURL, run.LinkParams)...)
		}
		return out
	}
	out := make([]Cell, len(cells))
	for i, cell := range cells {
		out[i] = Cell{Content: cell.Content, Width: cell.Width, Style: compactRowCellStyle(cell.Style), LinkURL: cell.LinkURL, LinkParams: cell.LinkParams}
	}
	return out
}

func compactRowRunCellCount(runs []CompactRowRun) int {
	total := 0
	for _, run := range runs {
		total += utf8.RuneCountInString(run.Text)
	}
	return total
}

func decodeCompactRowText(text string, style CellStyle) []Cell {
	return decodeCompactRowTextWithLink(text, style, "", "")
}

func decodeCompactRowTextWithLink(text string, style CellStyle, linkURL string, linkParams string) []Cell {
	if text == "" {
		return nil
	}
	out := make([]Cell, 0, utf8.RuneCountInString(text))
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r < utf8.RuneSelf {
			out = append(out, Cell{Content: compactASCIIStrings[byte(r)], Width: 1, Style: style, LinkURL: linkURL, LinkParams: linkParams})
		} else {
			out = append(out, Cell{Content: text[:size], Width: 1, Style: style, LinkURL: linkURL, LinkParams: linkParams})
		}
		text = text[size:]
	}
	return out
}

func compactRowCellStyle(style *CompactRowStyle) CellStyle {
	if style == nil {
		return CellStyle{}
	}
	return CellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
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
