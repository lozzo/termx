package port

import (
	"context"
	"time"

	"github.com/anytty/anytty/tui/state"
)

// NativeScreenSource 是 TUI live render loop 拉取 core latest native screen 的唯一接口。
// 它不返回 history token、scrollback 或 lifecycle truth；调用方只能把结果用于当前实时显示。
type NativeScreenSource interface {
	LiveSurface(context.Context, TerminalSurfaceRequest) (TerminalSurfaceResult, error)
}

// LiveInvalidationSource 提供 one-shot latest live screen 请求。
// 调用方每次 arm 最多得到一份基于 observed revision 的 latest 行变化，不排队保存中间帧。
type LiveInvalidationSource interface {
	ArmLiveInvalidation(context.Context, TerminalLiveEventRequest) (TerminalLiveEvent, error)
}

// TerminalSurfaceResult 返回当前 live native screen projection，不包含 committed history truth。
type TerminalSurfaceResult struct {
	Snapshot state.LiveSurfaceSnapshot
	Ready    bool
	// 中文说明：只标记这一次 service result 来自 core lifecycle 查询；不要写进 TUI store。
	LifecycleKnown bool
}

// TerminalSurfaceRequest 请求 owning daemon 的最新 live surface snapshot。
type TerminalSurfaceRequest struct {
	EndpointID       state.EndpointID
	TerminalID       string
	Cols             int
	Rows             int
	ObservedRevision uint64
}

// TerminalLiveEventRequest 是 TUI service 层 one-shot live wake 请求。
// ObservedRevision 来自 core native screen/wake 的已观察版本，只用于补 arm 间隙；
// 它不是 FrameSink 写出进度，不能把 TUI 渲染状态反传成 core truth。
type TerminalLiveEventRequest struct {
	EndpointID       state.EndpointID
	TerminalID       string
	Cols             int
	Rows             int
	ObservedRevision uint64
}

// TerminalLiveEvent 是 daemon one-shot live 请求进入 TUI effect 的投影。
// Ready 事件直接携带自 ObservedRevision 以来的 latest native screen 行变化。
type TerminalLiveEvent struct {
	EndpointID state.EndpointID
	TerminalID string
	Snapshot   state.LiveSurfaceSnapshot
	Refresh    bool
	// 中文说明：只标记这一次 event 承载 core lifecycle 变化；reducer 用完即丢。
	LifecycleKnown       bool
	Exited               bool
	ExitCode             int
	ExitedAt             time.Time
	Command              []string
	Reason               string
	Tags                 map[string]string
	Metadata             bool
	AttachmentProjection bool
	AttachmentCount      int
	OwnerSurfaceID       string
	OwnerViewID          string
	ResizeEpoch          uint64
	SizeLocked           bool
	Err                  error
	Ready                bool
}
