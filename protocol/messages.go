package protocol

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
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

type Event struct {
	Type                 EventType                 `json:"type"`
	TerminalID           string                    `json:"terminal_id"`
	Timestamp            time.Time                 `json:"timestamp"`
	Created              *TerminalCreatedData      `json:"created,omitempty"`
	StateChanged         *TerminalStateChangedData `json:"state_changed,omitempty"`
	Resized              *TerminalResizedData      `json:"resized,omitempty"`
	Removed              *TerminalRemovedData      `json:"removed,omitempty"`
	CollaboratorsRevoked *CollaboratorsRevokedData `json:"collaborators_revoked,omitempty"`
	ReadError            *TerminalReadErrorData    `json:"read_error,omitempty"`
}

type DetachParams struct {
	TerminalID string `json:"terminal_id"`
}

type EventsParams struct {
	TerminalID string      `json:"terminal_id,omitempty"`
	Types      []EventType `json:"types,omitempty"`
}

type SnapshotParams struct {
	TerminalID       string `json:"terminal_id"`
	ScrollbackOffset int    `json:"scrollback_offset,omitempty"`
	ScrollbackLimit  int    `json:"scrollback_limit,omitempty"`
}

type ListResult struct {
	Terminals []TerminalInfo `json:"terminals"`
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
	MouseTracking     bool `json:"mouse_tracking"`
	BracketedPaste    bool `json:"bracketed_paste"`
	ApplicationCursor bool `json:"application_cursor"`
	AutoWrap          bool `json:"auto_wrap"`
}

type ScreenData struct {
	Cells             [][]Cell `json:"-"`
	IsAlternateScreen bool     `json:"-"`
}

type Snapshot struct {
	TerminalID string        `json:"terminal_id"`
	Size       Size          `json:"size"`
	Screen     ScreenData    `json:"screen"`
	Scrollback [][]Cell      `json:"scrollback,omitempty"`
	Cursor     CursorState   `json:"cursor"`
	Modes      TerminalModes `json:"modes"`
	Timestamp  time.Time     `json:"timestamp"`
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
		TerminalID string        `json:"terminal_id"`
		Size       Size          `json:"size"`
		Screen     jsonScreen    `json:"screen"`
		Scrollback []jsonRow     `json:"scrollback"`
		Cursor     CursorState   `json:"cursor"`
		Modes      TerminalModes `json:"modes"`
		Timestamp  time.Time     `json:"timestamp"`
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

	s.TerminalID = raw.TerminalID
	s.Size = raw.Size
	s.Screen = ScreenData{Cells: convertRows(raw.Screen.Rows), IsAlternateScreen: raw.Screen.IsAlternate}
	s.Scrollback = convertRows(raw.Scrollback)
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
