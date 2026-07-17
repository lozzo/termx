package protocoladapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/shared/perftrace"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

// ProtocolTerminalClient 是 tui-v3 service adapter 依赖的 core-v2 protocol 边界。
// live invalidation 方法只透传 observed native revision 补 wake 边沿；adapter
// 不把 renderer/FrameSink 状态写回 core，也不从 protocol 侧推断 history truth。
type ProtocolTerminalClient interface {
	clientruntime.ProtoApplicationExecutor
	ApplicationAttachmentChannel(*apipb.ResourceHandle) (uint16, bool)
	ApplicationAttachment(uint16) (*apipb.ResourceHandle, bool)
	Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error)
	LiveScreen(context.Context, string) (*protocol.NativeScreenSnapshot, error)
	NextLiveInvalidation(context.Context, string, uint64) (*protocol.Event, error)
}

// ProtocolTerminalServiceAdapter 把 TUI-v3 terminal service 契约映射到 termx protocol。
type ProtocolTerminalServiceAdapter struct {
	Client      ProtocolTerminalClient
	Application *clientruntime.ApplicationSession
}

// NewProtocolTerminalServiceAdapter 把一个已完成 Hello/auth 的 protocol client 绑定到不可变 runtime generation。
// 构造失败时调用方必须放弃该 connection，不能用空 application session 继续运行或 fallback 到旧 method。
func NewProtocolTerminalServiceAdapter(client ProtocolTerminalClient, stamp clientruntime.EndpointSessionStamp) (ProtocolTerminalServiceAdapter, error) {
	if client == nil {
		return ProtocolTerminalServiceAdapter{}, port.ErrMissingTerminalClient
	}
	application, err := clientruntime.NewApplicationSession(stamp, client)
	if err != nil {
		return ProtocolTerminalServiceAdapter{}, err
	}
	return ProtocolTerminalServiceAdapter{Client: client, Application: application}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Attach(ctx context.Context, req port.TerminalAttachRequest) (port.TerminalAttachResult, error) {
	if adapter.Client == nil || adapter.Application == nil {
		return port.TerminalAttachResult{}, port.ErrMissingTerminalClient
	}
	finishRPC := perftrace.Measure("tui.protocol.terminal_attach.rpc")
	result, err := adapter.Application.TerminalAttach(ctx, &apipb.TerminalAttachCommand{
		Terminal: &apipb.TerminalRef{EndpointId: string(req.EndpointID), TerminalId: req.TerminalID},
		Mode:     attachmentModeToProto(req.Mode), ResizePolicy: resizePolicyToProto(req.ResizePolicy), SurfaceId: req.SurfaceID, ViewId: req.ViewID,
	})
	finishRPC(0)
	if err != nil {
		return port.TerminalAttachResult{}, err
	}
	channel, ok := adapter.Client.ApplicationAttachmentChannel(result.GetAttachment().GetResource())
	if !ok {
		return port.TerminalAttachResult{}, fmt.Errorf("terminal attachment has no protocol stream binding")
	}
	out := port.TerminalAttachResult{
		EndpointID:   req.EndpointID,
		TerminalID:   req.TerminalID,
		Channel:      channel,
		Cols:         int(result.GetSize().GetCols()),
		Rows:         int(result.GetSize().GetRows()),
		CanResize:    true,
		ResizePolicy: resizePolicyFromProto(result.GetResizePolicy()),
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	}
	if result.GetResizeControl() != nil {
		applyProtoResizeControlToAttach(&out, result.GetResizeControl())
	}
	return out, nil
}

func (adapter ProtocolTerminalServiceAdapter) Detach(ctx context.Context, req port.TerminalDetachRequest) error {
	if adapter.Client == nil || adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	resource, ok := adapter.Client.ApplicationAttachment(req.Channel)
	if !ok {
		return fmt.Errorf("terminal detach requires active attachment resource")
	}
	return adapter.Application.TerminalDetach(ctx, &apipb.TerminalDetachCommand{Attachment: resource})
}

func (adapter ProtocolTerminalServiceAdapter) List(ctx context.Context, req port.TerminalListRequest) (port.TerminalListResult, error) {
	if adapter.Application == nil {
		return port.TerminalListResult{}, port.ErrMissingTerminalClient
	}
	finishRPC := perftrace.Measure("tui.protocol.terminal_list.rpc")
	result, err := adapter.Application.TerminalList(ctx, &apipb.TerminalListCommand{})
	finishRPC(0)
	if err != nil {
		return port.TerminalListResult{}, err
	}
	finishConvert := perftrace.Measure("tui.protocol.terminal_list.convert")
	items := make([]port.TerminalPoolItem, 0, len(result.GetTerminals()))
	for _, terminal := range result.GetTerminals() {
		items = append(items, port.TerminalPoolItem{
			EndpointID: req.EndpointID, TerminalID: terminal.GetRef().GetTerminalId(), Title: terminalTitleFromProto(terminal), State: terminalStateFromProto(terminal.GetState()), CWD: terminal.GetCwd(),
			Command: append([]string(nil), terminal.GetCommand()...), Tags: cloneStringMap(terminal.GetTags()), ExitCode: int32PointerToInt(terminal.ExitCode), ExitedAt: unixNanoTime(terminal.GetExitedAtUnixNano()),
			Cols: int(terminal.GetSize().GetCols()), Rows: int(terminal.GetSize().GetRows()), AttachmentCount: int(terminal.GetAttachmentCount()),
			Resources: port.TerminalResourceUsage{
				PID: int(terminal.GetResources().GetPid()), CPUPercentX100: int(terminal.GetResources().GetCpuPercentX100()), MemoryBytes: terminal.GetResources().GetMemoryBytes(), SampledAt: unixNanoTime(terminal.GetResources().GetSampledAtUnixNano()),
			},
		})
	}
	finishConvert(len(items))
	return port.TerminalListResult{Items: items}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Create(ctx context.Context, req port.TerminalCreateRequest) (port.TerminalCreateResult, error) {
	if adapter.Application == nil {
		return port.TerminalCreateResult{}, port.ErrMissingTerminalClient
	}
	command := append([]string(nil), req.Command...)
	if len(command) == 0 {
		// 中文说明：默认 command 属于目标 daemon endpoint；adapter 这里没有
		// endpoint 默认值 truth，不能退回 TUI 进程本地 SHELL。
		return port.TerminalCreateResult{}, fmt.Errorf("terminal create command is required")
	}
	finishRPC := perftrace.Measure("tui.protocol.terminal_create.rpc")
	result, err := adapter.Application.TerminalCreate(ctx, &apipb.TerminalCreateCommand{Terminal: &apipb.TerminalCreateSpec{
		TerminalId: req.TerminalID, Name: req.Title, Command: command, Tags: cloneStringMap(req.Tags), Cwd: req.CWD, Size: &apipb.TerminalSize{Cols: uint32(req.Cols), Rows: uint32(req.Rows)},
	}})
	finishRPC(0)
	if err != nil {
		return port.TerminalCreateResult{}, err
	}
	terminalID := result.GetTerminal().GetRef().GetTerminalId()
	if terminalID == "" {
		terminalID = req.TerminalID
	}
	return port.TerminalCreateResult{EndpointID: req.EndpointID, TerminalID: terminalID, State: terminalStateFromProto(result.GetTerminal().GetState())}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Restart(ctx context.Context, req port.TerminalRestartRequest) error {
	if adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	return adapter.Application.TerminalRestart(ctx, &apipb.TerminalRestartCommand{Terminal: terminalRef(req.EndpointID, req.TerminalID)})
}

func (adapter ProtocolTerminalServiceAdapter) Reconnect(ctx context.Context, req port.TerminalReconnectRequest) (port.TerminalAttachResult, error) {
	return adapter.Attach(ctx, port.TerminalAttachRequest{
		EndpointID:   req.EndpointID,
		TerminalID:   req.TerminalID,
		Cols:         req.Cols,
		Rows:         req.Rows,
		Mode:         req.Mode,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	})
}

func (adapter ProtocolTerminalServiceAdapter) Kill(ctx context.Context, req port.TerminalKillRequest) error {
	if adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	return adapter.Application.TerminalKill(ctx, &apipb.TerminalKillCommand{Terminal: terminalRef(req.EndpointID, req.TerminalID)})
}

func (adapter ProtocolTerminalServiceAdapter) Remove(ctx context.Context, req port.TerminalRemoveRequest) error {
	if adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	return adapter.Application.TerminalRemove(ctx, &apipb.TerminalRemoveCommand{Terminal: terminalRef(req.EndpointID, req.TerminalID)})
}

func (adapter ProtocolTerminalServiceAdapter) EditMetadata(ctx context.Context, req port.TerminalEditMetadataRequest) error {
	if adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	return adapter.Application.TerminalSetMetadata(ctx, &apipb.TerminalSetMetadataCommand{Terminal: terminalRef(req.EndpointID, req.TerminalID), Name: req.Title, Tags: cloneStringMap(req.Tags)})
}

func (adapter ProtocolTerminalServiceAdapter) EditTags(ctx context.Context, req port.TerminalEditTagsRequest) error {
	if adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	return adapter.Application.TerminalSetTags(ctx, &apipb.TerminalSetTagsCommand{Terminal: terminalRef(req.EndpointID, req.TerminalID), Tags: cloneStringMap(req.Tags)})
}

func (adapter ProtocolTerminalServiceAdapter) SendInput(ctx context.Context, req port.TerminalInputRequest) error {
	if adapter.Client == nil || adapter.Application == nil {
		return port.ErrMissingTerminalClient
	}
	if req.Channel == 0 {
		return fmt.Errorf("terminal input requires attached channel")
	}
	resource, ok := adapter.Client.ApplicationAttachment(req.Channel)
	if !ok {
		return fmt.Errorf("terminal input requires active attachment resource")
	}
	return adapter.Application.TerminalInput(ctx, &apipb.TerminalInputCommand{Attachment: resource, Data: append([]byte(nil), req.Bytes...)})
}

func (adapter ProtocolTerminalServiceAdapter) Resize(ctx context.Context, req port.TerminalResizeRequest) (port.TerminalResizeResult, error) {
	if adapter.Client == nil || adapter.Application == nil {
		return port.TerminalResizeResult{}, port.ErrMissingTerminalClient
	}
	if req.Channel == 0 {
		return port.TerminalResizeResult{}, fmt.Errorf("terminal resize requires attached channel")
	}
	resource, ok := adapter.Client.ApplicationAttachment(req.Channel)
	if !ok {
		return port.TerminalResizeResult{}, fmt.Errorf("terminal resize requires active attachment resource")
	}
	result, err := adapter.Application.TerminalResize(ctx, &apipb.TerminalResizeCommand{Attachment: resource, Size: &apipb.TerminalSize{Cols: uint32(req.Cols), Rows: uint32(req.Rows)}, ResizePolicy: resizePolicyToProto(req.ResizePolicy)})
	if err != nil {
		return port.TerminalResizeResult{}, err
	}
	return terminalResizeResultFromProto(req, result), nil
}

func applyProtoResizeControlToAttach(out *port.TerminalAttachResult, control *apipb.ResizeControl) {
	if out == nil || control == nil {
		return
	}
	out.CanResize = control.CanResize
	out.SizeLocked = control.SizeLocked
	out.ControlReason = resizeControlReasonFromProto(control.GetReason())
	out.ResizePolicy = resizePolicyFromProtoControl(control, out.ResizePolicy)
	out.OwnerSurfaceID = control.GetOwnerSurfaceId()
	out.OwnerViewID = control.GetOwnerViewId()
	if ownership := control.GetOwnership(); ownership != nil {
		out.SizeLocked = ownership.GetSizeLocked()
		out.ResizeEpoch = ownership.GetEpoch()
		if ownership.GetSize() != nil {
			out.Cols = int(ownership.GetSize().GetCols())
			out.Rows = int(ownership.GetSize().GetRows())
		}
	}
}

func terminalResizeResultFromProto(req port.TerminalResizeRequest, result *apipb.TerminalResizeResult) port.TerminalResizeResult {
	out := port.TerminalResizeResult{TerminalID: req.TerminalID, Cols: req.Cols, Rows: req.Rows, ResizePolicy: req.ResizePolicy, SurfaceID: req.SurfaceID, ViewID: req.ViewID}
	if result == nil {
		return out
	}
	out.Resized = result.GetResized()
	out.Cols = int(result.GetSize().GetCols())
	out.Rows = int(result.GetSize().GetRows())
	if control := result.GetResizeControl(); control != nil {
		out.CanResize = control.GetCanResize()
		out.SizeLocked = control.GetSizeLocked()
		out.ControlReason = resizeControlReasonFromProto(control.GetReason())
		out.ResizePolicy = resizePolicyFromProtoControl(control, out.ResizePolicy)
		out.OwnerSurfaceID = control.GetOwnerSurfaceId()
		out.OwnerViewID = control.GetOwnerViewId()
		if ownership := control.GetOwnership(); ownership != nil {
			out.SizeLocked = ownership.GetSizeLocked()
			out.ResizeEpoch = ownership.GetEpoch()
			if ownership.GetSize() != nil {
				out.Cols = int(ownership.GetSize().GetCols())
				out.Rows = int(ownership.GetSize().GetRows())
			}
		}
	}
	return out
}

func resizePolicyFromProtoControl(control *apipb.ResizeControl, fallback string) string {
	if control == nil {
		return fallback
	}
	// core-v2 的 ResizeControl.Reason 是 attachment 这次 attach/ensure 后的权威角色；
	// 不能继续沿用请求里的 ResizePolicy，否则 UI 会显示半旧的 owner/follower 状态。
	switch control.GetReason() {
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_OWNER, apipb.ResizeControlReason_RESIZE_CONTROL_REASON_SIZE_LOCKED:
		return state.TerminalResizeRoleOwner
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_OBSERVER:
		return state.TerminalResizeRoleObserver
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_FOLLOWER:
		return state.TerminalResizeRoleFollower
	default:
		if control.CanResize {
			return state.TerminalResizeRoleOwner
		}
		return fallback
	}
}

func (adapter ProtocolTerminalServiceAdapter) LiveSurface(ctx context.Context, req port.TerminalSurfaceRequest) (port.TerminalSurfaceResult, error) {
	if adapter.Client == nil {
		return port.TerminalSurfaceResult{}, port.ErrMissingTerminalClient
	}
	finishRPC := perftrace.Measure("tui.protocol.live_surface.rpc")
	snapshot, err := adapter.Client.LiveScreen(ctx, req.TerminalID)
	finishRPC(0)
	if err != nil {
		return port.TerminalSurfaceResult{}, err
	}
	finishConvert := perftrace.Measure("tui.protocol.live_surface_convert")
	// 中文说明：v3 live display 的 screen truth 只能来自 core latest native screen；
	// lifecycle 由 list/state event 单独承载，不能再混入 snapshot RPC。
	liveSnapshot := liveSurfaceSnapshotFromProtocol(req.TerminalID, snapshot)
	finishConvert(liveSnapshotApproxBytes(liveSnapshot))
	return port.TerminalSurfaceResult{
		Ready:          true,
		Snapshot:       liveSnapshot,
		LifecycleKnown: false,
	}, nil
}

func (adapter ProtocolTerminalServiceAdapter) ArmLiveInvalidation(ctx context.Context, req port.TerminalLiveEventRequest) (port.TerminalLiveEvent, error) {
	if adapter.Client == nil {
		return port.TerminalLiveEvent{}, port.ErrMissingTerminalClient
	}
	finishRPC := perftrace.Measure("tui.protocol.live_invalidation.rpc")
	event, err := adapter.Client.NextLiveInvalidation(ctx, req.TerminalID, req.ObservedRevision)
	finishRPC(0)
	if err != nil {
		return port.TerminalLiveEvent{}, err
	}
	if event == nil {
		return port.TerminalLiveEvent{}, context.Canceled
	}
	liveEvent := liveInvalidationFromProtocol(req, *event)
	perftrace.Count("tui.protocol.live_event", protocolLiveEventApproxBytes(liveEvent))
	return liveEvent, nil
}

func protocolLiveEventApproxBytes(event port.TerminalLiveEvent) int {
	if !event.Ready {
		return 0
	}
	total := 0
	for _, line := range event.Snapshot.Lines {
		total += len(line)
	}
	for _, row := range event.Snapshot.Screen {
		for _, cell := range row {
			total += len(cell.Text)
		}
	}
	return total
}

func liveSnapshotApproxBytes(snapshot state.LiveSurfaceSnapshot) int {
	total := 0
	for _, line := range snapshot.Lines {
		total += len(line)
	}
	for _, row := range snapshot.Screen {
		for _, cell := range row {
			total += len(cell.Text)
		}
	}
	return total
}

func liveInvalidationFromProtocol(req port.TerminalLiveEventRequest, event protocol.Event) port.TerminalLiveEvent {
	out := port.TerminalLiveEvent{TerminalID: req.TerminalID}
	if event.TerminalID != "" {
		out.TerminalID = event.TerminalID
	}
	if event.Type != protocol.EventTerminalLiveInvalidated {
		out.Err = fmt.Errorf("unexpected live invalidation event type: %v", event.Type)
		return out
	}
	out.Refresh = true
	if event.LiveInvalidated != nil {
		out.Snapshot.Revision = event.LiveInvalidated.Revision
	}
	return out
}

func liveSurfaceSnapshotFromProtocol(terminalID string, snapshot *protocol.NativeScreenSnapshot) state.LiveSurfaceSnapshot {
	if snapshot == nil {
		return state.LiveSurfaceSnapshot{TerminalID: terminalID}
	}
	return state.LiveSurfaceSnapshot{
		TerminalID: terminalID,
		Revision:   snapshot.Revision,
		Cols:       int(snapshot.Size.Cols),
		Rows:       int(snapshot.Size.Rows),
		Screen:     liveSurfaceScreenFromCompactRows(snapshot.Rows),
		Cursor: state.LiveCursor{
			Visible: snapshot.Cursor.Visible,
			Row:     snapshot.Cursor.Row,
			Col:     snapshot.Cursor.Col,
			Shape:   snapshot.Cursor.Shape,
		},
		Modes: liveSurfaceModesFromProtocol(snapshot.Modes),
	}
}

func liveSurfaceScreenFromCompactRows(rows []protocol.CompactRow) [][]state.LiveCell {
	if len(rows) == 0 {
		return nil
	}
	screen := make([][]state.LiveCell, len(rows))
	for rowIndex, row := range rows {
		screen[rowIndex] = liveSurfaceCellsFromCompactRow(row)
	}
	return screen
}

func liveSurfaceModesFromProtocol(modes protocol.TerminalModes) state.LiveTerminalModes {
	return state.LiveTerminalModes{
		MouseTracking:  modes.MouseTracking,
		MouseX10:       modes.MouseX10,
		MouseNormal:    modes.MouseNormal,
		MouseButton:    modes.MouseButtonEvent,
		MouseAny:       modes.MouseAnyEvent,
		MouseSGR:       modes.MouseSGR,
		BracketedPaste: modes.BracketedPaste,
	}
}

func liveSurfaceCellsFromCompactRow(row protocol.CompactRow) []state.LiveCell {
	if row.Text != "" {
		return []state.LiveCell{{Text: row.Text, Width: liveSurfaceTextWidth(row.Text)}}
	}
	if len(row.Runs) > 0 {
		out := make([]state.LiveCell, 0, len(row.Runs))
		for _, run := range row.Runs {
			cell, ok := liveSurfaceCellFromCompactRun(run)
			if ok {
				out = append(out, cell)
			}
		}
		return out
	}
	if len(row.Cells) == 0 {
		return nil
	}
	return liveSurfaceCellsFromCompactCells(row.Cells)
}

func liveSurfaceCellFromCompactRun(run protocol.CompactRowRun) (state.LiveCell, bool) {
	if run.Text == "" {
		return state.LiveCell{}, false
	}
	style := liveSurfaceStyleFromCompact(run.Style)
	return state.LiveCell{
		Text:          run.Text,
		Width:         liveSurfaceTextWidth(run.Text),
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
		LinkURL:       run.LinkURL,
		LinkParams:    run.LinkParams,
	}, true
}

func liveSurfaceCellsFromCompactCells(cells []protocol.CompactRowCell) []state.LiveCell {
	out := make([]state.LiveCell, 0, liveSurfaceRunCapacity(len(cells)))
	for index := 0; index < len(cells); {
		next, ok := liveSurfaceCellFromCompactCell(cells[index])
		if !ok {
			index++
			continue
		}
		if !liveSurfaceCellIsSingleWidthASCII(next) {
			out = append(out, next)
			index++
			continue
		}
		runEnd := index + 1
		runWidth := next.Width
		runBytes := len(next.Text)
		runCells := 1
		for runEnd < len(cells) {
			runCell, ok := liveSurfaceCellFromCompactCell(cells[runEnd])
			if !ok {
				runEnd++
				continue
			}
			if !canMergeLiveSurfaceCellRun(next, runCell) {
				break
			}
			runWidth += runCell.Width
			runBytes += len(runCell.Text)
			runCells++
			runEnd++
		}
		if runCells == 1 {
			out = append(out, next)
			index = runEnd
			continue
		}
		var builder strings.Builder
		builder.Grow(runBytes)
		for runIndex := index; runIndex < runEnd; runIndex++ {
			if runCell, ok := liveSurfaceCellFromCompactCell(cells[runIndex]); ok {
				builder.WriteString(runCell.Text)
			}
		}
		next.Text = builder.String()
		next.Width = runWidth
		out = append(out, next)
		index = runEnd
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func liveSurfaceTextWidth(text string) int {
	return xansi.StringWidth(strings.ReplaceAll(text, "\n", " "))
}

func liveSurfaceCellFromCompactCell(cell protocol.CompactRowCell) (state.LiveCell, bool) {
	width := cell.Width
	if width < 0 {
		width = 0
	}
	if width == 0 && cell.Content == "" {
		return state.LiveCell{}, false
	}
	if width == 0 {
		width = 1
	}
	style := liveSurfaceStyleFromCompact(cell.Style)
	return state.LiveCell{
		Text:          cell.Content,
		Width:         width,
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
		LinkURL:       cell.LinkURL,
		LinkParams:    cell.LinkParams,
	}, true
}

func liveSurfaceStyleFromCompact(style *protocol.CompactRowStyle) protocol.CellStyle {
	if style == nil {
		return protocol.CellStyle{}
	}
	return protocol.CellStyle{
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

func liveSurfaceCellsFromProtocol(cells []protocol.Cell) []state.LiveCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]state.LiveCell, 0, liveSurfaceRunCapacity(len(cells)))
	for index := 0; index < len(cells); {
		next, ok := liveSurfaceCellFromProtocol(cells[index])
		if !ok {
			index++
			continue
		}
		if !liveSurfaceCellIsSingleWidthASCII(next) {
			out = append(out, next)
			index++
			continue
		}
		runEnd := index + 1
		runWidth := next.Width
		runBytes := len(next.Text)
		runCells := 1
		for runEnd < len(cells) {
			runCell, ok := liveSurfaceCellFromProtocol(cells[runEnd])
			if !ok {
				runEnd++
				continue
			}
			if !canMergeLiveSurfaceCellRun(next, runCell) {
				break
			}
			runWidth += runCell.Width
			runBytes += len(runCell.Text)
			runCells++
			runEnd++
		}
		if runCells == 1 {
			out = append(out, next)
			index = runEnd
			continue
		}
		// 中文说明：高频 live 行合并时先算准 run 容量，再一次构造字符串，
		// 避免逐 cell 拼接或小容量 builder 增长制造 alloc churn。
		var builder strings.Builder
		builder.Grow(runBytes)
		for runIndex := index; runIndex < runEnd; runIndex++ {
			if runCell, ok := liveSurfaceCellFromProtocol(cells[runIndex]); ok {
				builder.WriteString(runCell.Text)
			}
		}
		next.Text = builder.String()
		next.Width = runWidth
		out = append(out, next)
		index = runEnd
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func liveSurfaceRunCapacity(cellCount int) int {
	if cellCount <= 0 {
		return 0
	}
	if cellCount < 16 {
		return cellCount
	}
	return 16
}

func liveSurfaceCellFromProtocol(cell protocol.Cell) (state.LiveCell, bool) {
	width := cell.Width
	if width < 0 {
		width = 0
	}
	if width == 0 && cell.Content == "" {
		// 中文说明：protocol/vterm 会把 wide cell continuation 暴露成零宽空占位。
		// live surface 只需要保留真实 terminal footprint 起点；continuation 交给渲染侧按前一格 width 物化。
		// 否则会把同一个 FE0F footprint 额外展开成一格可见空白，导致 follower dots 被提前推左。
		return state.LiveCell{}, false
	}
	if width == 0 {
		width = 1
	}
	return state.LiveCell{
		Text:          cell.Content,
		Width:         width,
		FG:            cell.Style.FG,
		BG:            cell.Style.BG,
		Bold:          cell.Style.Bold,
		Italic:        cell.Style.Italic,
		Underline:     cell.Style.Underline,
		Blink:         cell.Style.Blink,
		Reverse:       cell.Style.Reverse,
		Strikethrough: cell.Style.Strikethrough,
		LinkURL:       cell.LinkURL,
		LinkParams:    cell.LinkParams,
	}, true
}

func canMergeLiveSurfaceCellRun(left state.LiveCell, right state.LiveCell) bool {
	return left.FG == right.FG &&
		left.BG == right.BG &&
		left.Bold == right.Bold &&
		left.Italic == right.Italic &&
		left.Underline == right.Underline &&
		left.Blink == right.Blink &&
		left.Reverse == right.Reverse &&
		left.Strikethrough == right.Strikethrough &&
		left.LinkURL == "" &&
		right.LinkURL == "" &&
		left.LinkParams == "" &&
		right.LinkParams == "" &&
		liveSurfaceCellIsSingleWidthASCII(left) &&
		liveSurfaceCellIsSingleWidthASCII(right)
}

func liveSurfaceCellIsSingleWidthASCII(cell state.LiveCell) bool {
	if cell.Width != len(cell.Text) {
		return false
	}
	for index := 0; index < len(cell.Text); index++ {
		if cell.Text[index] < 0x20 || cell.Text[index] > 0x7e {
			return false
		}
	}
	return true
}

func terminalRef(endpointID state.EndpointID, terminalID string) *apipb.TerminalRef {
	return &apipb.TerminalRef{EndpointId: string(endpointID), TerminalId: terminalID}
}

func terminalTitleFromProto(terminal *apipb.TerminalInfo) string {
	if terminal.GetName() != "" {
		return terminal.GetName()
	}
	if terminal.GetRef().GetTerminalId() != "" {
		return terminal.GetRef().GetTerminalId()
	}
	return "terminal"
}

func int32PointerToInt(value *int32) *int {
	if value == nil {
		return nil
	}
	cloned := int(*value)
	return &cloned
}

func unixNanoTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value)
}

func terminalStateFromProto(value apipb.TerminalState) string {
	switch value {
	case apipb.TerminalState_TERMINAL_STATE_CREATED:
		return "created"
	case apipb.TerminalState_TERMINAL_STATE_RUNNING:
		return "running"
	case apipb.TerminalState_TERMINAL_STATE_EXITED:
		return "exited"
	case apipb.TerminalState_TERMINAL_STATE_REMOVED:
		return "removed"
	default:
		return ""
	}
}

func attachmentModeToProto(value string) apipb.AttachmentMode {
	if strings.EqualFold(value, "observer") {
		return apipb.AttachmentMode_ATTACHMENT_MODE_OBSERVER
	}
	return apipb.AttachmentMode_ATTACHMENT_MODE_COLLABORATOR
}

func resizePolicyToProto(value string) apipb.ResizePolicy {
	switch value {
	case state.TerminalResizeRoleFollower:
		return apipb.ResizePolicy_RESIZE_POLICY_FOLLOWER
	case state.TerminalResizeRoleObserver:
		return apipb.ResizePolicy_RESIZE_POLICY_OBSERVER
	default:
		return apipb.ResizePolicy_RESIZE_POLICY_OWNER
	}
}

func resizePolicyFromProto(value apipb.ResizePolicy) string {
	switch value {
	case apipb.ResizePolicy_RESIZE_POLICY_FOLLOWER:
		return state.TerminalResizeRoleFollower
	case apipb.ResizePolicy_RESIZE_POLICY_OBSERVER:
		return state.TerminalResizeRoleObserver
	default:
		return state.TerminalResizeRoleOwner
	}
}

func resizeControlReasonFromProto(value apipb.ResizeControlReason) string {
	switch value {
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_OWNER:
		return "owner"
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_FOLLOWER:
		return "follower"
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_OBSERVER:
		return "observer"
	case apipb.ResizeControlReason_RESIZE_CONTROL_REASON_SIZE_LOCKED:
		return "size_locked"
	default:
		return ""
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
