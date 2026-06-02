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
	Timestamp time.Time
}

type LineSpan struct {
	StartRow int
	EndRow   int
}

type HistoryWindow struct {
	TerminalID string
	Token      WindowToken
	Op         WindowOp
	Size       protocol.Size
	Rows       []HistoryRow
	Lines      []LineSpan
	HasMore    bool
	Timestamp  time.Time
}

type WindowRequest struct {
	TerminalID string
	Token      WindowToken
	Limit      int
	Cols       int
}

type Source interface {
	LiveSurface(ctx context.Context, terminalID string) (LiveSurface, error)
	LatestHistoryWindow(ctx context.Context, request WindowRequest) (HistoryWindow, error)
	OlderHistoryWindow(ctx context.Context, request WindowRequest) (HistoryWindow, error)
}

type Store interface {
	ApplyLiveSurface(surface LiveSurface)
	ApplyHistoryWindow(window HistoryWindow)
	LiveSurface(terminalID string) (LiveSurface, bool)
	HistoryWindow(terminalID string) (HistoryWindow, bool)
}
