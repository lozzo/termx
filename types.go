package termx

import "time"

type Size struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type TerminalState string

const (
	StateStarting TerminalState = "starting"
	StateRunning  TerminalState = "running"
	StateExited   TerminalState = "exited"
)

type StreamMessageType int

const (
	StreamOutput StreamMessageType = iota + 1
	StreamSyncLost
	StreamClosed
	StreamResize
)

type StreamMessage struct {
	Type         StreamMessageType
	Output       []byte
	DroppedBytes uint64
	ExitCode     *int
	Cols         uint16
	Rows         uint16
}

type TerminalInfo struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Command   []string          `json:"command"`
	Tags      map[string]string `json:"tags,omitempty"`
	Size      Size              `json:"size"`
	State     TerminalState     `json:"state"`
	CreatedAt time.Time         `json:"created_at"`
	ExitCode  *int              `json:"exit_code,omitempty"`
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

type CursorShape string

const (
	CursorBlock     CursorShape = "block"
	CursorUnderline CursorShape = "underline"
	CursorBar       CursorShape = "bar"
)

type CursorState struct {
	Row     int         `json:"row"`
	Col     int         `json:"col"`
	Visible bool        `json:"visible"`
	Shape   CursorShape `json:"shape,omitempty"`
	Blink   bool        `json:"blink,omitempty"`
}

type TerminalModes struct {
	AlternateScreen   bool `json:"alternate_screen"`
	AlternateScroll   bool `json:"alternate_scroll,omitempty"`
	MouseTracking     bool `json:"mouse_tracking"`
	BracketedPaste    bool `json:"bracketed_paste"`
	ApplicationCursor bool `json:"application_cursor"`
	AutoWrap          bool `json:"auto_wrap"`
}

type ScreenData struct {
	Cells             [][]Cell `json:"rows"`
	IsAlternateScreen bool     `json:"is_alternate"`
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

type SnapshotOptions struct {
	ScrollbackOffset int
	ScrollbackLimit  int
}

type CreateOptions struct {
	Command        []string
	ID             string
	Name           string
	Tags           map[string]string
	Size           Size
	Dir            string
	Env            []string
	ScrollbackSize int
	KeepAfterExit  time.Duration
}

type ListOptions struct {
	State *TerminalState
	Tags  map[string]string
}

type AttachMode string

const (
	ModeObserver     AttachMode = "observer"
	ModeCollaborator AttachMode = "collaborator"
)

type AttachInfo struct {
	RemoteAddr string    `json:"remote_addr"`
	Mode       string    `json:"mode"`
	AttachedAt time.Time `json:"attached_at"`
}
