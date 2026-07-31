package core

import (
	"time"

	vterm "github.com/anytty/anytty/vterm/vterm"
)

// LiveRevision 是 core-v2 为单个 terminal native screen 维护的单调版本。
// 它只描述 live display 投影版本，不是 logical-line history generation，也不能作为 history window stale guard。
type LiveRevision uint64

// NativeScreenSize 是 core-v2 live native screen 的尺寸投影。
// truth 来源是 terminal 当前 live SurfaceTrack/vterm size，不来自 TUI pane 或 renderer frame。
type NativeScreenSize struct {
	Cols int
	Rows int
}

// NativeScreenRow 是 core-v2 对外暴露的 native screen 单行 cell matrix。
// 这里保留 vterm cell 语义属性，protocol/TUI 只能把它当实时屏幕 projection，不能当 history truth。
type NativeScreenRow struct {
	Index int
	Cells []vterm.Cell
}

// NativeScreenSnapshot 是 core-v2 当前 native screen 的 latest-only 快照。
// 它由 Terminal 从 live SurfaceTrack 读取；不携带 scrollback、history token 或 TUI view 状态。
type NativeScreenSnapshot struct {
	TerminalID   string
	BaseRevision LiveRevision
	Revision     LiveRevision
	FullReplace  bool
	Size         NativeScreenSize
	Rows         []NativeScreenRow
	Cursor       vterm.CursorState
	Modes        vterm.TerminalModes
	AltScreen    bool
	Timestamp    time.Time
}

// LiveScreenInvalidated 是 core 发给客户端的 live screen 唤醒信号。
// 它只表示 terminal 的 latest native screen 至少已经变到 Revision；客户端应自行拉取最新 snapshot。
type LiveScreenInvalidated struct {
	TerminalID string
	Revision   LiveRevision
}
