package termx

import "time"

type Size struct {
	Cols uint16
	Rows uint16
}

type TerminalState string

const (
	StateStarting TerminalState = "starting"
	StateRunning  TerminalState = "running"
	StateExited   TerminalState = "exited"
)

type StreamMessageType int

const (
	StreamSyncLost StreamMessageType = iota + 1
	StreamClosed
	StreamResize
	StreamBootstrapDone
	StreamScreenUpdate
)

type StreamMessage struct {
	Type         StreamMessageType
	Payload      []byte
	Revision     uint64
	DroppedBytes uint64
	ExitCode     *int
	Cols         uint16
	Rows         uint16
	Latest       func() StreamMessage
}

type TerminalInfo struct {
	ID                         string
	Name                       string
	Command                    []string
	Tags                       map[string]string
	Size                       Size
	State                      TerminalState
	CWD                        string
	LiveCWD                    string
	CreatedAt                  time.Time
	ExitCode                   *int
	ResizeOwnership            *ResizeOwnership
	ResizeOwnerAttachmentCount int
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

type ResizeOwnership struct {
	OwnerAttachmentID string
	OwnerSurfaceID    string
	OwnerViewID       string
	OwnerRemoteAddr   string
	Size              Size
	SizeLocked        bool
	Epoch             uint64
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

type CursorShape string

const (
	CursorBlock     CursorShape = "block"
	CursorUnderline CursorShape = "underline"
	CursorBar       CursorShape = "bar"
)

type CursorState struct {
	Row     int
	Col     int
	Visible bool
	Shape   CursorShape
	Blink   bool
}

type TerminalModes struct {
	AlternateScreen   bool
	AlternateScroll   bool
	MouseTracking     bool
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
	Scrollback             [][]Cell
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

type SnapshotOptions struct {
	ScrollbackOffset int
	ScrollbackLimit  int
}

type GridViewportOptions struct {
	ScrollbackOffset int
	ScrollbackLimit  int
	Cols             int
	Alternate        bool
}

type GridViewport struct {
	TerminalID             string
	Size                   Size
	Rows                   [][]Cell
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

type HistoryReplayOptions struct {
	BeforeOffset int
	Limit        int
	Alternate    bool
}

type HistoryReplayResult struct {
	TerminalID   string
	BeforeOffset int
	Limit        int
	Rows         int
	HasMore      bool
	Replay       string
}

type CreateOptions struct {
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
	KeepAfterExit      time.Duration
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
	RemoteAddr  string
	Mode        string
	SurfaceID   string
	ViewID      string
	ResizeOwner bool
	AttachedAt  time.Time
}
