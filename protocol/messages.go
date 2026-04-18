package protocol

import (
	"encoding/json"
	"errors"
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

type ScrollbackRowAppend struct {
	Cells     []Cell    `json:"cells,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	RowKind   string    `json:"row_kind,omitempty"`
}

type ScreenUpdate struct {
	FullReplace      bool                  `json:"full_replace,omitempty"`
	ResetScrollback  bool                  `json:"reset_scrollback,omitempty"`
	Size             Size                  `json:"size,omitempty"`
	Title            string                `json:"title,omitempty"`
	Screen           ScreenData            `json:"screen,omitempty"`
	ScreenTimestamps []time.Time           `json:"screen_timestamps,omitempty"`
	ScreenRowKinds   []string              `json:"screen_row_kinds,omitempty"`
	ChangedRows      []ScreenRowUpdate     `json:"changed_rows,omitempty"`
	ScrollbackTrim   int                   `json:"scrollback_trim,omitempty"`
	ScrollbackAppend []ScrollbackRowAppend `json:"scrollback_append,omitempty"`
	Cursor           CursorState           `json:"cursor"`
	Modes            TerminalModes         `json:"modes"`
}

type ListResult struct {
	Terminals []TerminalInfo `json:"terminals"`
}

func EncodeScreenUpdatePayload(update ScreenUpdate) ([]byte, error) {
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
	type jsonScreenRowUpdate struct {
		Row       int        `json:"row"`
		Cells     []jsonCell `json:"cells,omitempty"`
		Timestamp time.Time  `json:"timestamp,omitempty"`
		RowKind   string     `json:"row_kind,omitempty"`
	}
	type jsonScrollbackRowAppend struct {
		Cells     []jsonCell `json:"cells,omitempty"`
		Timestamp time.Time  `json:"timestamp,omitempty"`
		RowKind   string     `json:"row_kind,omitempty"`
	}
	type jsonScreenUpdate struct {
		FullReplace      bool                      `json:"full_replace,omitempty"`
		ResetScrollback  bool                      `json:"reset_scrollback,omitempty"`
		Size             Size                      `json:"size,omitempty"`
		Title            string                    `json:"title,omitempty"`
		Screen           jsonScreen                `json:"screen,omitempty"`
		ScreenTimestamps []string                  `json:"screen_timestamps,omitempty"`
		ScreenRowKinds   []string                  `json:"screen_row_kinds,omitempty"`
		ChangedRows      []jsonScreenRowUpdate     `json:"changed_rows,omitempty"`
		ScrollbackTrim   int                       `json:"scrollback_trim,omitempty"`
		ScrollbackAppend []jsonScrollbackRowAppend `json:"scrollback_append,omitempty"`
		Cursor           CursorState               `json:"cursor"`
		Modes            TerminalModes             `json:"modes"`
	}
	encodeRows := func(rows [][]Cell) []jsonRow {
		if len(rows) == 0 {
			return nil
		}
		out := make([]jsonRow, len(rows))
		for i, row := range rows {
			trimmed := trimCellsForScreenUpdateWire(row)
			cells := make([]jsonCell, len(trimmed))
			for j, cell := range trimmed {
				cells[j] = jsonCell{Content: cell.Content, Width: cell.Width}
				if cell.Style != (CellStyle{}) {
					cells[j].Style = &jsonStyle{
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
			out[i] = jsonRow{Cells: cells}
		}
		return out
	}
	encodeCells := func(row []Cell) []jsonCell {
		rows := encodeRows([][]Cell{row})
		if len(rows) == 0 {
			return nil
		}
		return rows[0].Cells
	}
	encodeRowTimestamps := func(values []time.Time) []string {
		if len(values) == 0 {
			return nil
		}
		out := make([]string, len(values))
		for i, value := range values {
			if value.IsZero() {
				continue
			}
			out[i] = value.UTC().Format(time.RFC3339Nano)
		}
		return out
	}
	raw := jsonScreenUpdate{
		FullReplace:      update.FullReplace,
		ResetScrollback:  update.ResetScrollback,
		Size:             update.Size,
		Title:            update.Title,
		Screen:           jsonScreen{IsAlternate: update.Screen.IsAlternateScreen, Rows: encodeRows(update.Screen.Cells)},
		ScreenTimestamps: encodeRowTimestamps(update.ScreenTimestamps),
		ScreenRowKinds:   append([]string(nil), update.ScreenRowKinds...),
		ChangedRows:      make([]jsonScreenRowUpdate, 0, len(update.ChangedRows)),
		ScrollbackTrim:   update.ScrollbackTrim,
		ScrollbackAppend: make([]jsonScrollbackRowAppend, 0, len(update.ScrollbackAppend)),
		Cursor:           update.Cursor,
		Modes:            update.Modes,
	}
	for _, row := range update.ChangedRows {
		raw.ChangedRows = append(raw.ChangedRows, jsonScreenRowUpdate{
			Row:       row.Row,
			Cells:     encodeCells(row.Cells),
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		})
	}
	for _, row := range update.ScrollbackAppend {
		raw.ScrollbackAppend = append(raw.ScrollbackAppend, jsonScrollbackRowAppend{
			Cells:     encodeCells(row.Cells),
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		})
	}
	return json.Marshal(raw)
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
	type jsonCell struct {
		Cells []Cell `json:"cells,omitempty"`
	}
	type jsonScreen struct {
		IsAlternate bool       `json:"is_alternate"`
		Rows        []jsonCell `json:"rows"`
	}
	type jsonScreenRowUpdate struct {
		Row       int       `json:"row"`
		Cells     []Cell    `json:"cells,omitempty"`
		Timestamp time.Time `json:"timestamp,omitempty"`
		RowKind   string    `json:"row_kind,omitempty"`
	}
	type jsonScrollbackRowAppend struct {
		Cells     []Cell    `json:"cells,omitempty"`
		Timestamp time.Time `json:"timestamp,omitempty"`
		RowKind   string    `json:"row_kind,omitempty"`
	}
	type jsonScreenUpdate struct {
		FullReplace      bool                      `json:"full_replace,omitempty"`
		ResetScrollback  bool                      `json:"reset_scrollback,omitempty"`
		Size             Size                      `json:"size,omitempty"`
		Title            string                    `json:"title,omitempty"`
		Screen           jsonScreen                `json:"screen,omitempty"`
		ScreenTimestamps []string                  `json:"screen_timestamps,omitempty"`
		ScreenRowKinds   []string                  `json:"screen_row_kinds,omitempty"`
		ChangedRows      []jsonScreenRowUpdate     `json:"changed_rows,omitempty"`
		ScrollbackTrim   int                       `json:"scrollback_trim,omitempty"`
		ScrollbackAppend []jsonScrollbackRowAppend `json:"scrollback_append,omitempty"`
		Cursor           CursorState               `json:"cursor"`
		Modes            TerminalModes             `json:"modes"`
	}
	var update ScreenUpdate
	if len(payload) == 0 {
		return update, nil
	}
	var raw jsonScreenUpdate
	if err := json.Unmarshal(payload, &raw); err != nil {
		return update, err
	}
	convertRows := func(rows []jsonCell) [][]Cell {
		if len(rows) == 0 {
			return nil
		}
		out := make([][]Cell, len(rows))
		for i, row := range rows {
			out[i] = row.Cells
		}
		return out
	}
	decodeRowTimestamps := func(rawValues []string) []time.Time {
		if len(rawValues) == 0 {
			return nil
		}
		out := make([]time.Time, len(rawValues))
		for i, value := range rawValues {
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
	update = ScreenUpdate{
		FullReplace:      raw.FullReplace,
		ResetScrollback:  raw.ResetScrollback,
		Size:             raw.Size,
		Title:            raw.Title,
		Screen:           ScreenData{Cells: convertRows(raw.Screen.Rows), IsAlternateScreen: raw.Screen.IsAlternate},
		ScreenTimestamps: decodeRowTimestamps(raw.ScreenTimestamps),
		ScreenRowKinds:   append([]string(nil), raw.ScreenRowKinds...),
		ChangedRows:      make([]ScreenRowUpdate, 0, len(raw.ChangedRows)),
		ScrollbackTrim:   raw.ScrollbackTrim,
		ScrollbackAppend: make([]ScrollbackRowAppend, 0, len(raw.ScrollbackAppend)),
		Cursor:           raw.Cursor,
		Modes:            raw.Modes,
	}
	for _, row := range raw.ChangedRows {
		update.ChangedRows = append(update.ChangedRows, ScreenRowUpdate{
			Row:       row.Row,
			Cells:     row.Cells,
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		})
	}
	for _, row := range raw.ScrollbackAppend {
		update.ScrollbackAppend = append(update.ScrollbackAppend, ScrollbackRowAppend{
			Cells:     row.Cells,
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		})
	}
	return update, nil
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
