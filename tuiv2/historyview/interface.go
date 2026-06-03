package historyview

import (
	"context"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

type SurfaceID string
type WindowToken string

type RowKind string

const (
	RowKindPersisted         RowKind = "persisted"
	RowKindLiveTailReclaimed RowKind = "live-tail-reclaimed"
	RowKindLiveTailLive      RowKind = "live-tail-live"
	RowKindScreen            RowKind = "screen"
)

type WindowOp string

const (
	WindowOpReplace WindowOp = "replace"
	WindowOpPrepend WindowOp = "prepend"
)

type LiveSurface struct {
	TerminalID string
	SurfaceID  SurfaceID
	Size       protocol.Size
	Screen     protocol.ScreenData
	Cursor     protocol.CursorState
	Modes      protocol.TerminalModes
	Timestamp  time.Time
}

type HistoryRow struct {
	Cells     protocol.CompactRow
	Kind      RowKind
	Wrapped   bool
	Timestamp time.Time
}

type LineSpan struct {
	StartRow       int
	EndRow         int
	Kind           RowKind
	LogicalLineID  uint64
	TimestampStart time.Time
	TimestampEnd   time.Time
	ClippedBefore  bool
	ClippedAfter   bool
}

type HistoryWindow struct {
	TerminalID      string
	Token           WindowToken
	Op              WindowOp
	Size            protocol.Size
	Rows            []HistoryRow
	Lines           []LineSpan
	BeforeCursor    int
	LoadedRows      int
	TotalRows       int
	LoadedLines     int
	TotalLines      int
	HasMore         bool
	Generation      uint64
	FirstLineID     uint64
	LastLineID      uint64
	FirstBoundaryID uint64
	LastBoundaryID  uint64
	Timestamp       time.Time
}

type Cursor struct {
	Row           int
	Col           int
	LogicalLineID uint64
	LogicalOffset int
}

type Selection struct {
	Active bool
	Anchor Cursor
	Focus  Cursor
}

type WindowRequest struct {
	TerminalID   string
	Token        WindowToken
	BeforeCursor int
	Limit        int
	Cols         int
}

type Source interface {
	LiveSurface(ctx context.Context, terminalID string) (LiveSurface, error)
	LatestHistoryWindow(ctx context.Context, request WindowRequest) (HistoryWindow, error)
	OlderHistoryWindow(ctx context.Context, request WindowRequest) (HistoryWindow, error)
}

type Store interface {
	ApplyLiveSurface(surface LiveSurface)
	ApplyHistoryWindow(window HistoryWindow) bool
	LiveSurface(terminalID string) (LiveSurface, bool)
	HistoryWindow(terminalID string) (HistoryWindow, bool)
	SetViewportTop(terminalID string, row int)
	ViewportTop(terminalID string) int
	SetCursor(terminalID string, cursor Cursor)
	Cursor(terminalID string) (Cursor, bool)
	SetSelection(terminalID string, selection Selection)
	Selection(terminalID string) (Selection, bool)
	ClearSelection(terminalID string)
	SetPendingRequest(terminalID string, token WindowToken)
	PendingRequest(terminalID string) WindowToken
	ClearPendingRequest(terminalID string, token WindowToken)
}
