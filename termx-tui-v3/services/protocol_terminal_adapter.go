package services

import (
	"context"
	"fmt"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// ProtocolTerminalClient 是 tui-v3 service adapter 依赖的 core-v2 protocol 边界。
// live invalidation 方法只透传 observed native revision 补 wake 边沿；adapter
// 不把 renderer/FrameSink 状态写回 core，也不从 protocol 侧推断 history truth。
type ProtocolTerminalClient interface {
	AttachWithOptions(context.Context, protocol.AttachParams) (*protocol.AttachResult, error)
	Detach(context.Context, protocol.DetachParams) error
	Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error)
	List(context.Context) (*protocol.ListResult, error)
	LiveScreen(context.Context, string) (*protocol.NativeScreenSnapshot, error)
	NextLiveInvalidation(context.Context, string, uint64) (*protocol.Event, error)
	Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error)
	Restart(context.Context, string) error
	Kill(context.Context, string) error
	Remove(context.Context, string) error
	SetMetadata(context.Context, string, string, map[string]string) error
	SetTags(context.Context, string, map[string]string) error
	Input(context.Context, uint16, []byte) error
	Resize(context.Context, uint16, uint16, uint16) error
	EnsureResize(context.Context, protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error)
}

type ProtocolInputRequestClient interface {
	InputWithOptions(context.Context, protocol.InputParams) error
}

// ProtocolTerminalServiceAdapter 把 TUI-v3 terminal service 契约映射到 termx protocol。
type ProtocolTerminalServiceAdapter struct {
	Client ProtocolTerminalClient
}

func (adapter ProtocolTerminalServiceAdapter) Attach(ctx context.Context, req TerminalAttachRequest) (TerminalAttachResult, error) {
	if adapter.Client == nil {
		return TerminalAttachResult{}, ErrMissingTerminalClient
	}
	mode := req.Mode
	if mode == "" {
		mode = "collaborator"
	}
	result, err := adapter.Client.AttachWithOptions(ctx, protocol.AttachParams{
		TerminalID:   req.TerminalID,
		Mode:         mode,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	})
	if err != nil {
		return TerminalAttachResult{}, err
	}
	out := TerminalAttachResult{
		TerminalID:   req.TerminalID,
		Channel:      result.Channel,
		Cols:         req.Cols,
		Rows:         req.Rows,
		CanResize:    true,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	}
	if result.ResizeControl != nil {
		applyProtocolResizeControlToAttach(&out, result.ResizeControl)
	}
	return out, nil
}

func (adapter ProtocolTerminalServiceAdapter) Detach(ctx context.Context, req TerminalDetachRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.Detach(ctx, protocol.DetachParams{
		TerminalID: req.TerminalID,
		Channel:    req.Channel,
		SurfaceID:  req.SurfaceID,
		ViewID:     req.ViewID,
	})
}

func (adapter ProtocolTerminalServiceAdapter) List(ctx context.Context, _ TerminalListRequest) (TerminalListResult, error) {
	if adapter.Client == nil {
		return TerminalListResult{}, ErrMissingTerminalClient
	}
	result, err := adapter.Client.List(ctx)
	if err != nil {
		return TerminalListResult{}, err
	}
	items := make([]TerminalPoolItem, 0, len(result.Terminals))
	for _, terminal := range result.Terminals {
		items = append(items, TerminalPoolItem{
			TerminalID:      terminal.ID,
			Title:           terminalPoolTitleFromProtocol(terminal),
			State:           terminal.State,
			CWD:             terminal.CWD,
			Command:         append([]string(nil), terminal.Command...),
			Tags:            cloneStringMap(terminal.Tags),
			ExitCode:        cloneIntPointer(terminal.ExitCode),
			ExitedAt:        terminal.ExitedAt,
			Cols:            int(terminal.Size.Cols),
			Rows:            int(terminal.Size.Rows),
			AttachmentCount: terminal.ResizeOwnerAttachmentCount,
		})
	}
	return TerminalListResult{Items: items}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Create(ctx context.Context, req TerminalCreateRequest) (TerminalCreateResult, error) {
	if adapter.Client == nil {
		return TerminalCreateResult{}, ErrMissingTerminalClient
	}
	command := append([]string(nil), req.Command...)
	if len(command) == 0 {
		command = DefaultTerminalCommand()
	}
	result, err := adapter.Client.Create(ctx, protocol.CreateParams{
		ID:      req.TerminalID,
		Name:    req.Title,
		Command: command,
		Tags:    cloneStringMap(req.Tags),
		Dir:     req.CWD,
		Size:    protocol.Size{Cols: uint16(req.Cols), Rows: uint16(req.Rows)},
	})
	if err != nil {
		return TerminalCreateResult{}, err
	}
	return TerminalCreateResult{TerminalID: result.TerminalID, State: result.State}, nil
}

func (adapter ProtocolTerminalServiceAdapter) Restart(ctx context.Context, req TerminalRestartRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.Restart(ctx, req.TerminalID)
}

func (adapter ProtocolTerminalServiceAdapter) Reconnect(ctx context.Context, req TerminalReconnectRequest) (TerminalAttachResult, error) {
	return adapter.Attach(ctx, TerminalAttachRequest{
		TerminalID:   req.TerminalID,
		Cols:         req.Cols,
		Rows:         req.Rows,
		Mode:         req.Mode,
		ResizePolicy: req.ResizePolicy,
		SurfaceID:    req.SurfaceID,
		ViewID:       req.ViewID,
	})
}

func (adapter ProtocolTerminalServiceAdapter) Kill(ctx context.Context, req TerminalKillRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.Kill(ctx, req.TerminalID)
}

func (adapter ProtocolTerminalServiceAdapter) Remove(ctx context.Context, req TerminalRemoveRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.Remove(ctx, req.TerminalID)
}

func (adapter ProtocolTerminalServiceAdapter) EditMetadata(ctx context.Context, req TerminalEditMetadataRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.SetMetadata(ctx, req.TerminalID, req.Title, cloneStringMap(req.Tags))
}

func (adapter ProtocolTerminalServiceAdapter) EditTags(ctx context.Context, req TerminalEditTagsRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	return adapter.Client.SetTags(ctx, req.TerminalID, cloneStringMap(req.Tags))
}

func (adapter ProtocolTerminalServiceAdapter) SendInput(ctx context.Context, req TerminalInputRequest) error {
	if adapter.Client == nil {
		return ErrMissingTerminalClient
	}
	if req.Channel == 0 {
		return fmt.Errorf("terminal input requires attached channel")
	}
	if client, ok := adapter.Client.(ProtocolInputRequestClient); ok {
		return client.InputWithOptions(ctx, protocol.InputParams{
			TerminalID: req.TerminalID,
			Channel:    req.Channel,
			SurfaceID:  req.SurfaceID,
			ViewID:     req.ViewID,
			Data:       req.Bytes,
		})
	}
	return adapter.Client.Input(ctx, req.Channel, req.Bytes)
}

func (adapter ProtocolTerminalServiceAdapter) Resize(ctx context.Context, req TerminalResizeRequest) (TerminalResizeResult, error) {
	if adapter.Client == nil {
		return TerminalResizeResult{}, ErrMissingTerminalClient
	}
	if req.Channel == 0 {
		return TerminalResizeResult{}, fmt.Errorf("terminal resize requires attached channel")
	}
	cols := uint16(req.Cols)
	rows := uint16(req.Rows)
	if req.SurfaceID != "" || req.ViewID != "" {
		resizePolicy := req.ResizePolicy
		if resizePolicy == "" {
			resizePolicy = protocol.ResizePolicyOwner
		}
		result, err := adapter.Client.EnsureResize(ctx, protocol.EnsureResizeParams{
			TerminalID:   req.TerminalID,
			Channel:      req.Channel,
			Cols:         cols,
			Rows:         rows,
			ResizePolicy: resizePolicy,
			SurfaceID:    req.SurfaceID,
			ViewID:       req.ViewID,
		})
		if err != nil {
			return TerminalResizeResult{}, err
		}
		return terminalResizeResultFromProtocol(req, result), nil
	}
	if err := adapter.Client.Resize(ctx, req.Channel, cols, rows); err != nil {
		return TerminalResizeResult{}, err
	}
	return TerminalResizeResult{TerminalID: req.TerminalID, Cols: req.Cols, Rows: req.Rows, Resized: true, CanResize: true, ResizePolicy: req.ResizePolicy, SurfaceID: req.SurfaceID, ViewID: req.ViewID}, nil
}

func applyProtocolResizeControlToAttach(out *TerminalAttachResult, control *protocol.ResizeControl) {
	if out == nil || control == nil {
		return
	}
	out.CanResize = control.CanResize
	out.SizeLocked = control.SizeLocked
	out.ControlReason = control.Reason
	out.ResizePolicy = resizePolicyFromProtocolControl(control, out.ResizePolicy)
	out.OwnerSurfaceID = control.OwnerSurfaceID
	out.OwnerViewID = control.OwnerViewID
	if control.ResizeOwnership != nil {
		out.SizeLocked = control.ResizeOwnership.SizeLocked
		out.ResizeEpoch = control.ResizeOwnership.Epoch
		if control.ResizeOwnership.Size != (protocol.Size{}) {
			out.Cols = int(control.ResizeOwnership.Size.Cols)
			out.Rows = int(control.ResizeOwnership.Size.Rows)
		}
	}
}

func terminalResizeResultFromProtocol(req TerminalResizeRequest, result *protocol.EnsureResizeResult) TerminalResizeResult {
	out := TerminalResizeResult{TerminalID: req.TerminalID, Cols: req.Cols, Rows: req.Rows, ResizePolicy: req.ResizePolicy, SurfaceID: req.SurfaceID, ViewID: req.ViewID}
	if result == nil {
		return out
	}
	out.Resized = result.Resized
	out.Cols = int(result.Size.Cols)
	out.Rows = int(result.Size.Rows)
	if result.ResizeControl != nil {
		out.CanResize = result.ResizeControl.CanResize
		out.SizeLocked = result.ResizeControl.SizeLocked
		out.ControlReason = result.ResizeControl.Reason
		out.ResizePolicy = resizePolicyFromProtocolControl(result.ResizeControl, out.ResizePolicy)
		out.OwnerSurfaceID = result.ResizeControl.OwnerSurfaceID
		out.OwnerViewID = result.ResizeControl.OwnerViewID
		if result.ResizeControl.ResizeOwnership != nil {
			out.SizeLocked = result.ResizeControl.ResizeOwnership.SizeLocked
			out.ResizeEpoch = result.ResizeControl.ResizeOwnership.Epoch
			if result.ResizeControl.ResizeOwnership.Size != (protocol.Size{}) {
				out.Cols = int(result.ResizeControl.ResizeOwnership.Size.Cols)
				out.Rows = int(result.ResizeControl.ResizeOwnership.Size.Rows)
			}
		}
	}
	return out
}

func resizePolicyFromProtocolControl(control *protocol.ResizeControl, fallback string) string {
	if control == nil {
		return fallback
	}
	// core-v2 的 ResizeControl.Reason 是 attachment 这次 attach/ensure 后的权威角色；
	// 不能继续沿用请求里的 ResizePolicy，否则 UI 会显示半旧的 owner/follower 状态。
	switch control.Reason {
	case protocol.ResizeControlReasonOwner, protocol.ResizeControlReasonSizeLocked:
		return state.TerminalResizeRoleOwner
	case protocol.ResizeControlReasonObserver:
		return state.TerminalResizeRoleObserver
	case protocol.ResizeControlReasonFollower:
		return state.TerminalResizeRoleFollower
	default:
		if control.CanResize {
			return state.TerminalResizeRoleOwner
		}
		return fallback
	}
}

func (adapter ProtocolTerminalServiceAdapter) LiveSurface(ctx context.Context, req TerminalSurfaceRequest) (TerminalSurfaceResult, error) {
	if adapter.Client == nil {
		return TerminalSurfaceResult{}, ErrMissingTerminalClient
	}
	snapshot, err := adapter.Client.LiveScreen(ctx, req.TerminalID)
	if err != nil {
		return TerminalSurfaceResult{}, err
	}
	finishConvert := perftrace.Measure("tui.protocol.live_surface_convert")
	// 中文说明：v3 live display 的 screen truth 只能来自 core latest native screen；
	// lifecycle 由 list/state event 单独承载，不能再混入 snapshot RPC。
	liveSnapshot := state.LiveSurfaceSnapshot{
		TerminalID: req.TerminalID,
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
	finishConvert(liveSnapshotApproxBytes(liveSnapshot))
	return TerminalSurfaceResult{
		Ready:          true,
		Snapshot:       liveSnapshot,
		LifecycleKnown: false,
	}, nil
}

func (adapter ProtocolTerminalServiceAdapter) ArmLiveInvalidation(ctx context.Context, req TerminalLiveEventRequest) (TerminalLiveEvent, error) {
	if adapter.Client == nil {
		return TerminalLiveEvent{}, ErrMissingTerminalClient
	}
	event, err := adapter.Client.NextLiveInvalidation(ctx, req.TerminalID, req.ObservedRevision)
	if err != nil {
		return TerminalLiveEvent{}, err
	}
	if event == nil {
		return TerminalLiveEvent{}, context.Canceled
	}
	liveEvent := liveInvalidationFromProtocol(req, *event)
	perftrace.Count("tui.protocol.live_event", protocolLiveEventApproxBytes(liveEvent))
	return liveEvent, nil
}

func protocolLiveEventApproxBytes(event TerminalLiveEvent) int {
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

func liveInvalidationFromProtocol(req TerminalLiveEventRequest, event protocol.Event) TerminalLiveEvent {
	out := TerminalLiveEvent{TerminalID: req.TerminalID}
	if event.TerminalID != "" {
		out.TerminalID = event.TerminalID
	}
	if event.Type != protocol.EventTerminalLiveInvalidated {
		out.Err = fmt.Errorf("unexpected live invalidation event type: %v", event.Type)
		return out
	}
	// 中文说明：live invalidation 只是唤醒信号；TUI app 自己决定何时拉取 latest screen。
	out.Refresh = true
	if event.LiveInvalidated != nil {
		out.Snapshot.Revision = event.LiveInvalidated.Revision
	}
	return out
}

func (adapter ProtocolTerminalServiceAdapter) terminalInfo(ctx context.Context, terminalID string) (protocol.TerminalInfo, error) {
	result, err := adapter.Client.List(ctx)
	if err != nil {
		return protocol.TerminalInfo{}, err
	}
	for _, item := range result.Terminals {
		if item.ID == terminalID {
			return item, nil
		}
	}
	return protocol.TerminalInfo{}, fmt.Errorf("terminal metadata unavailable: %s", terminalID)
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

func terminalPoolTitleFromProtocol(terminal protocol.TerminalInfo) string {
	if terminal.Name != "" {
		return terminal.Name
	}
	if terminal.ID != "" {
		return terminal.ID
	}
	return "terminal"
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
