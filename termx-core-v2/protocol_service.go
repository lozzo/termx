package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
	"github.com/rivo/uniseg"
)

const (
	protocolErrorBadRequest  = 400
	protocolErrorForbidden   = 403
	protocolErrorNotFound    = 404
	protocolErrorUnavailable = 503
	protocolErrorInternal    = 500
)

const daemonBoundaryReclaimMinHeapMBEnv = "TERMX_DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB"
const daemonBoundaryReclaimDefaultMinHeapBytes = 8 << 20

var errProtocolAttachmentMismatch = errors.New("protocol attachment mismatch")
var daemonBoundaryReclaimMinHeapBytes = parseDaemonBoundaryReclaimMinHeapBytes()
var daemonBoundaryReclaimLastHeapSys atomic.Uint64

func parseDaemonBoundaryReclaimMinHeapBytes() uint64 {
	raw := strings.TrimSpace(os.Getenv(daemonBoundaryReclaimMinHeapMBEnv))
	if raw == "" {
		return daemonBoundaryReclaimDefaultMinHeapBytes
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value << 20
}

func maybeReclaimDaemonBoundaryHeap() {
	if daemonBoundaryReclaimMinHeapBytes == 0 {
		return
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	idleUnreleased := uint64(0)
	if mem.HeapIdle > mem.HeapReleased {
		idleUnreleased = mem.HeapIdle - mem.HeapReleased
	}
	if mem.HeapAlloc < daemonBoundaryReclaimMinHeapBytes && idleUnreleased < daemonBoundaryReclaimMinHeapBytes {
		return
	}
	if !claimDaemonBoundaryReclaimHeapSys(mem.HeapSys) {
		return
	}
	// 中文说明：这是 request/terminal 批次边界上的显式 runtime page 归还，只在
	// history/snapshot response 已发送或 history 批次已落盘后运行；它不删除任何
	// history truth，也不是后台定时 scrub。
	debug.FreeOSMemory()
}

func claimDaemonBoundaryReclaimHeapSys(heapSys uint64) bool {
	for {
		last := daemonBoundaryReclaimLastHeapSys.Load()
		if last != 0 && heapSys < last+daemonBoundaryReclaimMinHeapBytes {
			return false
		}
		if daemonBoundaryReclaimLastHeapSys.CompareAndSwap(last, heapSys) {
			return true
		}
	}
}

type protocolSession struct {
	server        *Server
	conn          transport.Transport
	scope         TransportScope
	sessionID     uint64
	sendMu        sync.Mutex
	nextCh        atomic.Uint32
	nextSnapshot  atomic.Uint64
	mu            sync.RWMutex
	attachments   map[uint16]protocolAttachment
	streamCancels map[uint16]context.CancelFunc
	eventCancels  map[uint64]context.CancelFunc
	nextEventSub  uint64
	historyMu     sync.Mutex
	historyPins   map[string]frozenHistorySnapshot
	historyLatest map[string]string
	requests      sync.WaitGroup
}

type frozenHistorySnapshot struct {
	TerminalID string
	Snapshot   history.FrozenSnapshot
}

// protocolAttachment 是 daemon-side channel/view registry；它不保存 TUI workspace/pane truth。
type protocolAttachment struct {
	SessionID    uint64
	TerminalID   string
	Channel      uint16
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
	Epoch        uint64
}

type protocolAttachmentKey struct {
	SessionID uint64
	Channel   uint16
}

func newProtocolSession(server *Server, conn transport.Transport, scope TransportScope) *protocolSession {
	session := &protocolSession{
		server:        server,
		conn:          conn,
		scope:         scope.normalized(),
		sessionID:     server.nextProtocolSessionID.Add(1),
		attachments:   make(map[uint16]protocolAttachment),
		streamCancels: make(map[uint16]context.CancelFunc),
		eventCancels:  make(map[uint64]context.CancelFunc),
		historyPins:   make(map[string]frozenHistorySnapshot),
		historyLatest: make(map[string]string),
	}
	session.nextCh.Store(6)
	return session
}

func (session *protocolSession) run(ctx context.Context) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		cancel()
		session.requests.Wait()
		session.stopAllAttachmentStreams()
		session.stopEvents()
		session.releaseProtocolAttachments()
		session.releaseAllFrozenSnapshots()
	}()
	for {
		frame, err := session.conn.Recv()
		if err != nil {
			return err
		}
		channel, typ, payload, err := wire.DecodeFrame(frame)
		if err != nil {
			return err
		}
		if channel == 0 {
			if err := session.handleControlFrame(sessionCtx, typ, payload); err != nil {
				return err
			}
			continue
		}
		if err := session.handleStreamFrame(sessionCtx, channel, typ, payload); err != nil {
			if sendErr := session.sendError(0, protocolErrorBadRequest, err.Error()); sendErr != nil {
				return sendErr
			}
		}
	}
}

func (session *protocolSession) handleControlFrame(ctx context.Context, typ uint8, payload []byte) error {
	switch typ {
	case wire.TypeHello:
		hello, err := protocol.DecodeHelloPayload(payload)
		if err != nil {
			return session.sendError(0, protocolErrorBadRequest, err.Error())
		}
		if hello.Version != 0 && hello.Version != wire.Version {
			return session.sendError(0, protocolErrorBadRequest, fmt.Sprintf("unsupported wire version %d", hello.Version))
		}
		response, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Server: ModuleName})
		if err != nil {
			return err
		}
		return session.sendFrame(0, wire.TypeHello, response)
	case wire.TypeRequest:
		req, err := protocol.DecodeRequestPayload(payload)
		if err != nil {
			return session.sendError(0, protocolErrorBadRequest, err.Error())
		}
		// 中文说明：control request 不能在同一 client 上互相 head-of-line blocking。
		// history.window latest 可能短暂等待 history 追平，普通 input ack 仍要能并发处理。
		session.requests.Add(1)
		go func() {
			defer session.requests.Done()
			_ = session.handleRequest(ctx, req)
		}()
		return nil
	default:
		return session.sendError(0, protocolErrorBadRequest, fmt.Sprintf("unsupported control frame type %d", typ))
	}
}

func (session *protocolSession) handleRequest(ctx context.Context, req protocol.Request) error {
	result, binary, code, err := session.dispatchRequest(ctx, req)
	if err != nil {
		return session.sendError(req.ID, code, err.Error())
	}
	if binary {
		payload, err := protocol.EncodeBinaryResponsePayload(req.ID, result)
		if err != nil {
			return err
		}
		err = session.sendFrame(0, wire.TypeResponseBinary, payload)
		maybeReclaimDaemonBoundaryHeap()
		return err
	}
	payload, err := protocol.EncodeResponsePayload(protocol.Response{ID: req.ID, Result: result})
	if err != nil {
		return err
	}
	err = session.sendFrame(0, wire.TypeResponse, payload)
	maybeReclaimDaemonBoundaryHeap()
	return err
}

func (session *protocolSession) dispatchRequest(ctx context.Context, req protocol.Request) ([]byte, bool, int, error) {
	params, err := protocol.DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return nil, false, protocolErrorBadRequest, err
	}
	params, err = session.scope.constrainMethod(req.Method, params)
	if err != nil {
		return nil, false, protocolErrorForbidden, err
	}
	switch req.Method {
	case "create":
		in := params.(protocol.CreateParams)
		info, err := session.server.RegisterTerminal(TerminalRecord{
			ID:      in.ID,
			Name:    in.Name,
			Command: append([]string(nil), in.Command...),
			Tags:    cloneStringMap(in.Tags),
			Size:    coreSizeFromProtocol(in.Size),
			Options: TerminalCreateOptions{
				Dir:                in.Dir,
				Env:                append([]string(nil), in.Env...),
				ScrollbackSize:     in.ScrollbackSize,
				ScrollbackMaxBytes: in.ScrollbackMaxBytes,
				ScrollbackMaxAge:   in.ScrollbackMaxAge,
			},
		})
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.CreateResult{TerminalID: info.ID, State: string(info.State)})
	case "list":
		items := session.server.ListTerminals()
		coreLifecycleTrace(session.server.cfg.logger, "protocol.list",
			"count", len(items),
			"items", coreTerminalListSummary(items),
		)
		out := protocol.ListResult{Terminals: make([]protocol.TerminalInfo, 0, len(items))}
		for _, item := range items {
			out.Terminals = append(out.Terminals, session.protocolInfoFromCoreV2(item))
		}
		return encodeMethodResult(req.Method, out)
	case "get":
		in := params.(protocol.GetParams)
		info, err := session.server.GetTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, session.protocolInfoFromCoreV2(info))
	case "set_tags":
		in := params.(protocol.SetTagsParams)
		info, err := session.server.GetTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		_, err = session.server.SetMetadata(ctx, in.TerminalID, info.Name, in.Tags)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "set_metadata":
		in := params.(protocol.SetMetadataParams)
		_, err := session.server.SetMetadata(ctx, in.TerminalID, in.Name, in.Tags)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "kill":
		in := params.(protocol.GetParams)
		err := session.server.KillTerminal(ctx, in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "restart":
		in := params.(protocol.GetParams)
		if info, err := session.server.GetTerminal(in.TerminalID); err == nil {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.request", coreTerminalInfoAttrs(info)...)
		} else {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.request",
				"terminal_id", in.TerminalID,
				"error", err.Error(),
			)
		}
		err := session.server.RestartTerminal(ctx, in.TerminalID)
		if err != nil {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.result",
				"terminal_id", in.TerminalID,
				"error", err.Error(),
			)
			return nil, false, errorCode(err), err
		}
		if info, infoErr := session.server.GetTerminal(in.TerminalID); infoErr == nil {
			coreLifecycleTrace(session.server.cfg.logger, "protocol.restart.result", coreTerminalInfoAttrs(info)...)
		}
		return encodeMethodResult(req.Method, nil)
	case "remove":
		in := params.(protocol.GetParams)
		err := session.server.RemoveTerminal(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "resize":
		in := params.(protocol.ResizeParams)
		err := session.server.ResizeTerminal(ctx, in.TerminalID, in.Cols, in.Rows)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "input":
		in := params.(protocol.InputParams)
		if err := session.input(ctx, in); err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "attach":
		in := params.(protocol.AttachParams)
		attachment, control, err := session.attach(in)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if err := session.startAttachmentStream(ctx, attachment); err != nil {
			session.detach(protocol.DetachParams{Channel: attachment.Channel})
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.AttachResult{
			Mode:          attachment.Mode,
			Channel:       attachment.Channel,
			ResizeControl: control,
		})
	case "detach":
		in := params.(protocol.DetachParams)
		session.detach(in)
		return encodeMethodResult(req.Method, nil)
	case "ensure_resize":
		in := params.(protocol.EnsureResizeParams)
		attachment, err := session.attachmentForChannel(in.Channel)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if attachment.TerminalID != in.TerminalID {
			return nil, false, protocolErrorBadRequest, fmt.Errorf("resize channel %d is attached to %s, not %s", in.Channel, attachment.TerminalID, in.TerminalID)
		}
		control, canResize, err := session.resizeControlForRequest(attachment, in.ResizePolicy, in.SurfaceID, in.ViewID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if !canResize {
			return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
				Size:          control.ResizeOwnership.Size,
				Resized:       false,
				ResizeControl: control,
			})
		}
		if in.ResizePolicy == protocol.ResizePolicyOwner {
			control = session.resizeControlForOwner(attachment, control.ResizeOwnership.Size)
		}
		if control.ResizeOwnership != nil && control.ResizeOwnership.Size == (protocol.Size{Cols: in.Cols, Rows: in.Rows}) {
			// 中文说明：owner 转移即使尺寸相同也必须先刷新 ownership；
			// 但 PTY 尺寸没变时不能实际 resize，避免制造多余 resize 事件和历史 invalidation。
			return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
				Size:          control.ResizeOwnership.Size,
				Resized:       false,
				ResizeControl: control,
			})
		}
		err = session.server.ResizeTerminal(ctx, in.TerminalID, in.Cols, in.Rows)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		if in.ResizePolicy == protocol.ResizePolicyOwner {
			control = session.resizeControlForOwner(attachment, protocol.Size{Cols: in.Cols, Rows: in.Rows})
		}
		return encodeMethodResult(req.Method, protocol.EnsureResizeResult{
			Size:          protocol.Size{Cols: in.Cols, Rows: in.Rows},
			Resized:       true,
			ResizeControl: control,
		})
	case "resize.lock":
		in := params.(protocol.ResizeControlParams)
		control, err := session.setResizeLock(in, true)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.ResizeControlResult{Size: control.ResizeOwnership.Size, ResizeControl: control})
	case "resize.unlock":
		in := params.(protocol.ResizeControlParams)
		control, err := session.setResizeLock(in, false)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.ResizeControlResult{Size: control.ResizeOwnership.Size, ResizeControl: control})
	case "snapshot":
		in := params.(protocol.SnapshotParams)
		snapshot, err := session.liveCompactSnapshot(in)
		if err != nil {
			return nil, true, errorCode(err), err
		}
		payload, err := protocol.EncodeCompactSnapshotPayload(snapshot)
		if err != nil {
			return nil, true, protocolErrorInternal, err
		}
		return payload, true, 0, nil
	case "events":
		in := params.(protocol.EventsParams)
		session.startEvents(ctx, in)
		return encodeMethodResult(req.Method, nil)
	case "storage.get":
		in := params.(protocol.StorageGetParams)
		entry, err := session.server.StorageGet(ctx, in.AppID, storageScopeFromProtocol(in.Scope), in.OwnerID, in.Key)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocolStorageEntryFromCore(entry))
	case "storage.put":
		in := params.(protocol.StoragePutParams)
		entry, err := session.server.StoragePut(ctx, StoragePutRequest{
			AppID:           in.AppID,
			Scope:           storageScopeFromProtocol(in.Scope),
			OwnerID:         in.OwnerID,
			Key:             in.Key,
			Value:           append([]byte(nil), in.Value...),
			CheckVersion:    in.CheckVersion,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocolStorageEntryFromCore(entry))
	case "storage.delete":
		in := params.(protocol.StorageDeleteParams)
		result, err := session.server.StorageDelete(ctx, StorageDeleteRequest{
			AppID:           in.AppID,
			Scope:           storageScopeFromProtocol(in.Scope),
			OwnerID:         in.OwnerID,
			Key:             in.Key,
			CheckVersion:    in.CheckVersion,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocol.StorageDeleteResult{
			AppID:   result.AppID,
			Scope:   protocolStorageScopeFromCore(result.Scope),
			OwnerID: result.OwnerID,
			Key:     result.Key,
			Deleted: result.Deleted,
			Version: result.Version,
		})
	case "storage.list":
		in := params.(protocol.StorageListParams)
		entries := session.server.StorageList(ctx, in.AppID, storageScopeFromProtocol(in.Scope), in.OwnerID, in.Prefix)
		out := protocol.StorageListResult{Entries: make([]protocol.StorageEntry, 0, len(entries))}
		for _, entry := range entries {
			out.Entries = append(out.Entries, protocolStorageEntryFromCore(entry))
		}
		return encodeMethodResult(req.Method, out)
	case "workbench.get":
		in := params.(protocol.WorkbenchGetParams)
		snapshot, err := session.server.WorkbenchSnapshot(ctx, in.WorkspaceID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, snapshot)
	case "workbench.apply":
		in := params.(protocol.WorkbenchMutateParams)
		result, err := session.server.ApplyWorkbenchMutation(ctx, in)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, result)
	case "history.window":
		in := params.(protocol.HistoryWindowParams)
		window, err := session.historyWindow(ctx, in)
		if err != nil {
			return nil, true, errorCode(err), err
		}
		payload, err := protocol.EncodeHistoryWindowPayload(window)
		if err != nil {
			return nil, true, protocolErrorInternal, err
		}
		return payload, true, 0, nil
	case "history.copy":
		in := params.(protocol.HistoryWindowParams)
		payload, err := session.historyCopy(ctx, in)
		if err != nil {
			return nil, true, errorCode(err), err
		}
		return payload, true, 0, nil
	case "history.release":
		in := params.(protocol.HistoryWindowParams)
		if in.Token == "" {
			return nil, false, protocolErrorBadRequest, fmt.Errorf("history release requires token")
		}
		session.releaseFrozenSnapshot(in.TerminalID, in.Token)
		return encodeMethodResult(req.Method, nil)
	case "remote.status":
		service, err := session.remoteService()
		if err != nil {
			return nil, false, errorCode(err), err
		}
		status, err := service.Status(ctx)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, status)
	case "remote.pair.start":
		service, err := session.remoteService()
		if err != nil {
			return nil, false, errorCode(err), err
		}
		in := params.(protocol.RemotePairStartParams)
		result, err := service.PairStart(ctx, in)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, result)
	case "remote.local.enable":
		service, err := session.remoteService()
		if err != nil {
			return nil, false, errorCode(err), err
		}
		in := params.(protocol.RemoteLocalEnableParams)
		status, err := service.LocalEnable(ctx, in)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, status)
	case "remote.local.status":
		service, err := session.remoteService()
		if err != nil {
			return nil, false, errorCode(err), err
		}
		status, err := service.LocalStatus(ctx)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, status)
	case "remote.local.disable":
		service, err := session.remoteService()
		if err != nil {
			return nil, false, errorCode(err), err
		}
		status, err := service.LocalDisable(ctx)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, status)
	default:
		return nil, false, protocolErrorNotFound, fmt.Errorf("unknown method: %s", req.Method)
	}
}

func (session *protocolSession) remoteService() (RemoteService, error) {
	service := session.server.RemoteService()
	if service == nil {
		return nil, ErrRemoteServiceUnavailable
	}
	return service, nil
}

func (session *protocolSession) liveCompactSnapshot(params protocol.SnapshotParams) (*protocol.CompactSnapshot, error) {
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	terminal, err := session.server.Terminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	var rows []protocol.CompactRow
	screenInfo := terminal.VisitLiveTrimmedScreenRows(func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell) {
		if rows == nil {
			rows = make([]protocol.CompactRow, 0, rowIndex+1)
		}
		for len(rows) < rowIndex {
			rows = append(rows, protocol.CompactRow{})
		}
		// 中文说明：snapshot 协议已经是 compact rows，不能再先构造
		// [][]protocol.Cell；这里按需读取 vterm cell 并直接压成 wire row。
		rows = append(rows, protocol.BuildCompactRowPreserveTrailingBlankCells(cellCount, func(index int) protocol.Cell {
			return vtermCellToProtocol(cellAt(index))
		}))
	})
	for len(rows) < screenInfo.Rows {
		rows = append(rows, protocol.CompactRow{})
	}
	attrs := coreTerminalInfoAttrs(info)
	attrs = append(attrs,
		"screen_rows", len(rows),
		"cursor_row", screenInfo.Cursor.Row,
		"cursor_col", screenInfo.Cursor.Col,
		"cursor_visible", screenInfo.Cursor.Visible,
	)
	coreLifecycleTrace(session.server.cfg.logger, "protocol.snapshot", attrs...)
	return &protocol.CompactSnapshot{
		TerminalID:        params.TerminalID,
		Size:              protocolSizeFromCore(info.Size),
		ScreenRows:        rows,
		ScreenIsAlternate: screenInfo.IsAlternateScreen,
		Cursor:            vtermCursorToProtocol(screenInfo.Cursor),
		Modes:             vtermModesToProtocol(screenInfo.Modes),
		HistoryGeneration: uint64(terminal.HistoryGeneration()),
		ScreenOwnership:   repeatString(protocol.RowOwnershipScreen, len(rows)),
		Timestamp:         time.Now().UTC(),
	}, nil
}

func (session *protocolSession) liveScreenUpdatePayload(terminalID string) ([]byte, error) {
	info, err := session.server.GetTerminal(terminalID)
	if err != nil {
		return nil, err
	}
	terminal, err := session.server.Terminal(terminalID)
	if err != nil {
		return nil, err
	}
	var rows [][]protocol.Cell
	screenInfo := terminal.VisitLiveTrimmedScreenRows(func(rowIndex int, cellCount int, cellAt func(int) vterm.Cell) {
		for len(rows) <= rowIndex {
			rows = append(rows, nil)
		}
		if cellCount <= 0 {
			rows[rowIndex] = nil
			return
		}
		row := make([]protocol.Cell, cellCount)
		for i := 0; i < cellCount; i++ {
			row[i] = vtermCellToProtocol(cellAt(i))
		}
		rows[rowIndex] = row
	})
	for len(rows) < screenInfo.Rows {
		rows = append(rows, nil)
	}
	return protocol.EncodeScreenUpdatePayload(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocolSizeFromCore(info.Size),
		Screen: protocol.ScreenData{
			Cells:             rows,
			IsAlternateScreen: screenInfo.IsAlternateScreen,
		},
		Cursor: vtermCursorToProtocol(screenInfo.Cursor),
		Modes:  vtermModesToProtocol(screenInfo.Modes),
	})
}

func vtermCellToProtocol(cell vterm.Cell) protocol.Cell {
	return protocol.Cell{
		Content:    cell.Content,
		Width:      cell.Width,
		Style:      vtermStyleToProtocol(cell.Style),
		LinkURL:    cell.LinkURL,
		LinkParams: cell.LinkParams,
	}
}

func vtermStyleToProtocol(style vterm.CellStyle) protocol.CellStyle {
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

func vtermCursorToProtocol(cursor vterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func vtermModesToProtocol(modes vterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		MouseX10:          modes.MouseX10,
		MouseNormal:       modes.MouseNormal,
		MouseButtonEvent:  modes.MouseButtonEvent,
		MouseAnyEvent:     modes.MouseAnyEvent,
		MouseSGR:          modes.MouseSGR,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func repeatString(value string, count int) []string {
	if count <= 0 {
		return nil
	}
	out := make([]string, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func encodeMethodResult(method string, result any) ([]byte, bool, int, error) {
	payload, err := protocol.EncodeMethodResult(method, result)
	if err != nil {
		return nil, false, protocolErrorInternal, err
	}
	return payload, false, 0, nil
}

func (session *protocolSession) handleStreamFrame(ctx context.Context, channel uint16, typ uint8, payload []byte) error {
	attachment, err := session.attachmentForChannel(channel)
	if err != nil {
		return err
	}
	if err := session.scope.allowsAttachment(attachment); err != nil {
		return err
	}
	switch typ {
	case wire.TypeInput:
		return session.server.WriteInput(ctx, attachment.TerminalID, payload)
	case wire.TypeResize:
		cols, rows, err := wire.DecodeResizePayload(payload)
		if err != nil {
			return err
		}
		control, canResize, err := session.resizeControlForRequest(attachment, attachment.ResizePolicy, attachment.SurfaceID, attachment.ViewID)
		if err != nil {
			return err
		}
		if !canResize {
			return fmt.Errorf("resize denied for channel %d: %s", channel, control.Reason)
		}
		if err := session.server.ResizeTerminal(ctx, attachment.TerminalID, cols, rows); err != nil {
			return err
		}
		session.resizeControlForOwner(attachment, protocol.Size{Cols: cols, Rows: rows})
		return nil
	case wire.TypeStreamReady, wire.TypeBootstrapDone:
		return nil
	default:
		return fmt.Errorf("unsupported stream frame type %d", typ)
	}
}

func (session *protocolSession) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 24
	}
	cols := params.Cols
	if cols <= 0 {
		info, err := session.server.GetTerminal(params.TerminalID)
		if err != nil {
			return nil, err
		}
		cols = int(info.Size.Cols)
	}
	var window history.HistoryWindow
	if params.Mode == "newer" {
		if err := session.validateNewerWindowRequest(params, cols); err != nil {
			return nil, err
		}
		snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
		if !ok {
			return nil, ErrStaleHistoryWindow
		}
		window = frozenSnapshotNewerWindow(snapshot, cols, limit, protocolAfterCursorToCore(params))
		window.Token = history.WindowToken(params.Token)
		if params.BoundaryFirstLineID != 0 {
			// newer response 的 payload 只包含本页较新 rows，但它要接回客户端
			// 已加载 frozen 视图；FirstLineID 必须保持当前 head boundary。
			window.FirstLineID = history.LogicalLineID(params.BoundaryFirstLineID)
		}
		if params.BoundaryLastLineID != 0 {
			window.LastLineID = history.LogicalLineID(params.BoundaryLastLineID)
		}
	} else if params.CursorValid {
		if err := session.validateOlderWindowRequest(params, cols); err != nil {
			return nil, err
		}
		snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
		if !ok {
			return nil, ErrStaleHistoryWindow
		}
		window = frozenSnapshotOlderWindow(snapshot, cols, limit, protocolCursorToCore(params))
		window.Token = history.WindowToken(params.Token)
		if params.BoundaryLastLineID != 0 {
			// older response 的 payload 只包含本页更老 rows，但它要接回客户端
			// 已加载 frozen 视图；LastLineID 必须保持当前 tail boundary，不能变成本页尾。
			window.LastLineID = history.LogicalLineID(params.BoundaryLastLineID)
		}
	} else if params.Token != "" {
		if err := session.validateFrozenWindowRequest(params, cols); err != nil {
			return nil, err
		}
		snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
		if !ok {
			return nil, ErrStaleHistoryWindow
		}
		// 带 token 但不带 cursor 表示在当前 frozen snapshot 内跳到最老页；
		// 这是跳转语义，返回 replace，避免客户端为了到顶一次性加载中间所有页。
		window = frozenSnapshotOldestWindow(snapshot, cols, limit)
		window.Token = history.WindowToken(params.Token)
	} else {
		terminal, err := session.server.Terminal(params.TerminalID)
		if err != nil {
			return nil, err
		}
		// 中文说明：copy/latest 的 frozen boundary 是用户进入历史模式时 core 已经入账的
		// logical line 边界；这里不能等待 history queue 追平后续高压输出，否则会卡住
		// entering 滚动，并把用户进入 copy 之后的“未来日志”混进本次 snapshot。
		snapshot := terminal.FreezePinnedSnapshot()
		if params.Generation != 0 {
			snapshot = terminal.FreezePinnedSnapshotAtGeneration(history.Generation(params.Generation))
		}
		snapshot.Token = session.sessionFrozenSnapshotToken(params.TerminalID, snapshot.Token)
		session.storeFrozenSnapshot(params.TerminalID, snapshot)
		window = frozenSnapshotLatestWindow(snapshot, cols, limit)
	}
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	size := Size{Cols: uint16(cols), Rows: info.Size.Rows}
	return protocolHistoryWindowFromCore(params.TerminalID, size, window), nil
}

func (session *protocolSession) historyCopy(ctx context.Context, params protocol.HistoryWindowParams) ([]byte, error) {
	cols := params.Cols
	if cols <= 0 {
		info, err := session.server.GetTerminal(params.TerminalID)
		if err != nil {
			return nil, err
		}
		cols = int(info.Size.Cols)
	}
	if err := session.validateFrozenWindowRequest(params, cols); err != nil {
		return nil, err
	}
	if !params.RangeValid || params.RangeStartLineID == 0 || params.RangeEndLineID == 0 {
		return nil, fmt.Errorf("history copy requires logical range")
	}
	snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
	if !ok {
		return nil, ErrStaleHistoryWindow
	}
	text, err := frozenSnapshotCopyRange(snapshot, historyCopyRange{
		startLineID: history.LogicalLineID(params.RangeStartLineID),
		startCol:    params.RangeStartCol,
		endLineID:   history.LogicalLineID(params.RangeEndLineID),
		endCol:      params.RangeEndCol,
	})
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (session *protocolSession) validateOlderWindowRequest(params protocol.HistoryWindowParams, cols int) error {
	if err := session.validateFrozenWindowRequest(params, cols); err != nil {
		return err
	}
	snapshot, _ := session.frozenSnapshot(params.TerminalID, params.Token)
	if params.CursorValid {
		cursor := protocolCursorToCore(params)
		if !frozenSnapshotCursorValid(snapshot, cols, cursor) {
			return ErrStaleHistoryWindow
		}
	}
	return nil
}

func (session *protocolSession) validateNewerWindowRequest(params protocol.HistoryWindowParams, cols int) error {
	if err := session.validateFrozenWindowRequest(params, cols); err != nil {
		return err
	}
	if !params.AfterCursorValid {
		return ErrStaleHistoryWindow
	}
	snapshot, _ := session.frozenSnapshot(params.TerminalID, params.Token)
	if !frozenSnapshotAfterCursorValid(snapshot, cols, protocolAfterCursorToCore(params)) {
		return ErrStaleHistoryWindow
	}
	return nil
}

func (session *protocolSession) validateFrozenWindowRequest(params protocol.HistoryWindowParams, cols int) error {
	snapshot, ok := session.frozenSnapshot(params.TerminalID, params.Token)
	if !ok {
		return ErrStaleHistoryWindow
	}
	if params.Token != "" && params.Token != snapshot.Token {
		return ErrStaleHistoryWindow
	}
	if params.Generation != 0 && params.Generation != uint64(snapshot.Generation) {
		return ErrStaleHistoryWindow
	}
	// older 请求带回的是客户端当前 frozen latest/prepend 视图的 logical
	// boundary，而不是“只看最后一行”重算出来的尾部 line id。这里要接受
	// snapshot 当前投影尾部任意合法 suffix window 的 boundary，避免多行 latest
	// 或 prepend 过的 frozen 视图被误判成 stale。
	if !frozenSnapshotBoundaryValid(snapshot, cols, params.BoundaryFirstLineID, params.BoundaryLastLineID) {
		return ErrStaleHistoryWindow
	}
	return nil
}

func (session *protocolSession) startEvents(ctx context.Context, params protocol.EventsParams) {
	eventCtx, cancel := context.WithCancel(ctx)
	session.mu.Lock()
	session.nextEventSub++
	subID := session.nextEventSub
	session.eventCancels[subID] = cancel
	session.mu.Unlock()
	events := session.server.Events(eventCtx, eventFilterFromProtocol(params))
	go func() {
		defer session.clearEventSubscription(subID)
		for {
			select {
			case <-eventCtx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				payload, err := protocol.EncodeEventPayload(protocolEventFromCoreV2(event))
				if err != nil {
					continue
				}
				_ = session.sendFrame(0, wire.TypeEvent, payload)
			}
		}
	}()
}

func (session *protocolSession) stopEvents() {
	session.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(session.eventCancels))
	for id, cancel := range session.eventCancels {
		cancels = append(cancels, cancel)
		delete(session.eventCancels, id)
	}
	session.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (session *protocolSession) clearEventSubscription(id uint64) {
	session.mu.Lock()
	delete(session.eventCancels, id)
	session.mu.Unlock()
}

func (session *protocolSession) attach(params protocol.AttachParams) (protocolAttachment, *protocol.ResizeControl, error) {
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		coreLifecycleTrace(session.server.cfg.logger, "protocol.attach.request",
			"terminal_id", params.TerminalID,
			"view_id", params.ViewID,
			"surface_id", params.SurfaceID,
			"resize_policy", params.ResizePolicy,
			"error", err.Error(),
		)
		return protocolAttachment{}, nil, err
	}
	attrs := coreTerminalInfoAttrs(info)
	attrs = append(attrs,
		"view_id", params.ViewID,
		"surface_id", params.SurfaceID,
		"resize_policy", params.ResizePolicy,
		"mode", params.Mode,
	)
	coreLifecycleTrace(session.server.cfg.logger, "protocol.attach.request", attrs...)
	channel := uint16(session.nextCh.Add(1))
	attachment := protocolAttachment{
		SessionID:    session.sessionID,
		TerminalID:   params.TerminalID,
		Channel:      channel,
		Mode:         normalizeAttachMode(params.Mode),
		ResizePolicy: normalizeAttachResizePolicy(params.ResizePolicy),
		SurfaceID:    params.SurfaceID,
		ViewID:       params.ViewID,
	}
	session.mu.Lock()
	session.attachments[channel] = attachment
	session.mu.Unlock()
	control := session.registerProtocolAttachment(attachment, protocolSizeFromCore(info.Size))
	if control != nil && (control.Reason == protocol.ResizeControlReasonOwner || control.Reason == protocol.ResizeControlReasonSizeLocked) {
		if control.ResizeOwnership != nil {
			attachment.Epoch = control.ResizeOwnership.Epoch
		}
		attachment.ResizePolicy = protocol.ResizePolicyOwner
		session.mu.Lock()
		session.attachments[channel] = attachment
		session.mu.Unlock()
	}
	coreLifecycleTrace(session.server.cfg.logger, "protocol.attach.result",
		"terminal_id", attachment.TerminalID,
		"channel", attachment.Channel,
		"view_id", attachment.ViewID,
		"surface_id", attachment.SurfaceID,
		"resize_policy", attachment.ResizePolicy,
		"can_resize", control.CanResize,
		"control_reason", control.Reason,
		"owner_view_id", control.OwnerViewID,
		"owner_surface_id", control.OwnerSurfaceID,
		"state", string(info.State),
	)
	return attachment, control, nil
}

func (session *protocolSession) startAttachmentStream(ctx context.Context, attachment protocolAttachment) error {
	streamCtx, cancel := context.WithCancel(ctx)
	session.mu.Lock()
	if previous := session.streamCancels[attachment.Channel]; previous != nil {
		previous()
	}
	session.streamCancels[attachment.Channel] = cancel
	session.mu.Unlock()
	events := session.server.Events(streamCtx, EventFilter{
		TerminalID: attachment.TerminalID,
		Types: []EventType{
			EventTerminalChanged,
			EventTerminalResized,
			EventTerminalExited,
		},
	})
	go session.forwardAttachmentStream(streamCtx, attachment, events)
	if err := session.sendLiveScreenUpdate(attachment); err != nil {
		session.stopAttachmentStreams([]uint16{attachment.Channel})
		return err
	}
	return nil
}

func (session *protocolSession) forwardAttachmentStream(ctx context.Context, attachment protocolAttachment, events <-chan Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			if err := session.sendLiveScreenUpdate(attachment); err != nil {
				return
			}
		}
	}
}

func (session *protocolSession) sendLiveScreenUpdate(attachment protocolAttachment) error {
	if _, err := session.attachmentForChannel(attachment.Channel); err != nil {
		return err
	}
	payload, err := session.liveScreenUpdatePayload(attachment.TerminalID)
	if err != nil {
		return err
	}
	// 中文说明：stream frame 属于 attachment channel 的 live 投影；它只读 core-v2
	// live surface，不创建 history truth，也不把 App 本地缓存写回 core。
	return session.sendFrame(attachment.Channel, wire.TypeScreenUpdate, payload)
}

func (session *protocolSession) stopAttachmentStreams(channels []uint16) {
	if len(channels) == 0 {
		return
	}
	session.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(channels))
	for _, channel := range channels {
		if cancel := session.streamCancels[channel]; cancel != nil {
			cancels = append(cancels, cancel)
			delete(session.streamCancels, channel)
		}
	}
	session.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (session *protocolSession) stopAllAttachmentStreams() {
	session.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(session.streamCancels))
	for channel, cancel := range session.streamCancels {
		cancels = append(cancels, cancel)
		delete(session.streamCancels, channel)
	}
	session.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (session *protocolSession) detach(params protocol.DetachParams) {
	session.mu.Lock()
	var detached []protocolAttachment
	for channel, attachment := range session.attachments {
		if !detachMatches(params, channel, attachment) {
			continue
		}
		delete(session.attachments, channel)
		detached = append(detached, attachment)
	}
	session.mu.Unlock()
	channels := make([]uint16, 0, len(detached))
	for _, attachment := range detached {
		channels = append(channels, attachment.Channel)
	}
	session.stopAttachmentStreams(channels)
	session.unregisterProtocolAttachments(detached)
}

func detachMatches(params protocol.DetachParams, channel uint16, attachment protocolAttachment) bool {
	if params.Channel != 0 {
		return params.Channel == channel
	}
	if params.TerminalID != "" && params.TerminalID != attachment.TerminalID {
		return false
	}
	if params.SurfaceID != "" && params.SurfaceID != attachment.SurfaceID {
		return false
	}
	if params.ViewID != "" && params.ViewID != attachment.ViewID {
		return false
	}
	return params.TerminalID != "" || params.SurfaceID != "" || params.ViewID != ""
}

func (session *protocolSession) input(ctx context.Context, params protocol.InputParams) error {
	attachment, err := session.attachmentForChannel(params.Channel)
	if err != nil {
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"surface_id", params.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	if attachment.TerminalID != params.TerminalID {
		err := fmt.Errorf("%w: input channel %d is attached to %s, not %s", errProtocolAttachmentMismatch, params.Channel, attachment.TerminalID, params.TerminalID)
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"attached_terminal", attachment.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"attached_view_id", attachment.ViewID,
			"surface_id", params.SurfaceID,
			"attached_surface_id", attachment.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	if params.SurfaceID != "" && attachment.SurfaceID != params.SurfaceID {
		err := fmt.Errorf("%w: input channel %d surface mismatch: %s != %s", errProtocolAttachmentMismatch, params.Channel, attachment.SurfaceID, params.SurfaceID)
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"attached_view_id", attachment.ViewID,
			"surface_id", params.SurfaceID,
			"attached_surface_id", attachment.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	if params.ViewID != "" && attachment.ViewID != params.ViewID {
		err := fmt.Errorf("%w: input channel %d view mismatch: %s != %s", errProtocolAttachmentMismatch, params.Channel, attachment.ViewID, params.ViewID)
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"attached_view_id", attachment.ViewID,
			"surface_id", params.SurfaceID,
			"attached_surface_id", attachment.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	err = session.server.WriteInput(ctx, attachment.TerminalID, params.Data)
	if err != nil {
		coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
			"terminal_id", params.TerminalID,
			"channel", params.Channel,
			"view_id", params.ViewID,
			"surface_id", params.SurfaceID,
			"bytes", len(params.Data),
			"error", err.Error(),
		)
		return err
	}
	coreLifecycleTrace(session.server.cfg.logger, "protocol.input",
		"terminal_id", params.TerminalID,
		"channel", params.Channel,
		"view_id", params.ViewID,
		"surface_id", params.SurfaceID,
		"bytes", len(params.Data),
		"result", "ok",
	)
	return nil
}

func (session *protocolSession) attachmentForChannel(channel uint16) (protocolAttachment, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()
	attachment, ok := session.attachments[channel]
	if !ok {
		return protocolAttachment{}, ErrTerminalNotFound
	}
	return attachment, nil
}

func (session *protocolSession) resizeControlForRequest(attachment protocolAttachment, policy string, surfaceID string, viewID string) (*protocol.ResizeControl, bool, error) {
	info, err := session.server.GetTerminal(attachment.TerminalID)
	if err != nil {
		return nil, false, err
	}
	if policy != "" {
		attachment.ResizePolicy = normalizeResizePolicy(policy)
	}
	if surfaceID != "" {
		attachment.SurfaceID = surfaceID
	}
	if viewID != "" {
		attachment.ViewID = viewID
	}
	session.mu.Lock()
	if current, ok := session.attachments[attachment.Channel]; ok {
		current.ResizePolicy = attachment.ResizePolicy
		current.SurfaceID = attachment.SurfaceID
		current.ViewID = attachment.ViewID
		attachment = current
		session.attachments[attachment.Channel] = current
	}
	session.mu.Unlock()
	control := session.updateProtocolAttachmentControl(attachment, protocolSizeFromCore(info.Size), attachment.ResizePolicy == protocol.ResizePolicyOwner)
	return control, control.CanResize, nil
}

func (session *protocolSession) setResizeLock(params protocol.ResizeControlParams, locked bool) (*protocol.ResizeControl, error) {
	attachment, err := session.attachmentForChannel(params.Channel)
	if err != nil {
		return nil, err
	}
	if attachment.TerminalID != params.TerminalID {
		return nil, fmt.Errorf("resize control channel %d is attached to %s, not %s", params.Channel, attachment.TerminalID, params.TerminalID)
	}
	control, _, err := session.resizeControlForRequest(attachment, params.ResizePolicy, params.SurfaceID, params.ViewID)
	if err != nil {
		return nil, err
	}
	ownerAttachmentID := ""
	if control.ResizeOwnership != nil {
		ownerAttachmentID = control.ResizeOwnership.OwnerAttachmentID
	}
	if ownerAttachmentID != protocolAttachmentOwnerID(attachment) || attachment.ResizePolicy != protocol.ResizePolicyOwner {
		return control, nil
	}
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	return session.setGlobalResizeLock(attachment, protocolSizeFromCore(info.Size), locked), nil
}

func (session *protocolSession) resizeControlForOwner(attachment protocolAttachment, size protocol.Size) *protocol.ResizeControl {
	session.mu.Lock()
	if current, ok := session.attachments[attachment.Channel]; ok {
		current.ResizePolicy = protocol.ResizePolicyOwner
		attachment = current
		session.attachments[attachment.Channel] = current
	}
	session.mu.Unlock()
	return session.updateProtocolAttachmentControl(attachment, size, true)
}

func (session *protocolSession) protocolInfoFromCoreV2(info TerminalInfo) protocol.TerminalInfo {
	return session.server.ProtocolTerminalInfo(info)
}

func (session *protocolSession) registerProtocolAttachment(attachment protocolAttachment, size protocol.Size) *protocol.ResizeControl {
	session.server.protocolAttachmentMu.Lock()
	defer session.server.protocolAttachmentMu.Unlock()
	if attachment.SessionID == 0 {
		attachment.SessionID = session.sessionID
	}
	key := attachmentKey(attachment)
	changed := true
	if existing, ok := session.server.protocolAttachments[key]; ok {
		changed = existing != attachment
	}
	takeOwner := attachment.ResizePolicy == protocol.ResizePolicyOwner || session.server.protocolResizeOwners[attachment.TerminalID] == ""
	if takeOwner {
		session.server.protocolOwnerEpoch++
		attachment.ResizePolicy = protocol.ResizePolicyOwner
		attachment.Epoch = session.server.protocolOwnerEpoch
		session.server.protocolResizeOwners[attachment.TerminalID] = key
		changed = true
	}
	session.server.protocolAttachments[key] = attachment
	session.server.protocolChannelIndex[protocolAttachmentKey{SessionID: attachment.SessionID, Channel: attachment.Channel}] = key
	control := session.resizeControlForGlobalAttachmentLocked(attachment, size)
	session.publishProtocolAttachmentChangedLocked(attachment.TerminalID, changed)
	return control
}

func (session *protocolSession) updateProtocolAttachmentControl(attachment protocolAttachment, size protocol.Size, takeOwner bool) *protocol.ResizeControl {
	session.server.protocolAttachmentMu.Lock()
	defer session.server.protocolAttachmentMu.Unlock()
	if attachment.SessionID == 0 {
		attachment.SessionID = session.sessionID
	}
	key := attachmentKey(attachment)
	if current, ok := session.server.protocolAttachments[key]; ok {
		current.ResizePolicy = attachment.ResizePolicy
		current.SurfaceID = attachment.SurfaceID
		current.ViewID = attachment.ViewID
		attachment = current
	} else {
		session.server.protocolChannelIndex[protocolAttachmentKey{SessionID: attachment.SessionID, Channel: attachment.Channel}] = key
	}
	changed := true
	if existing, ok := session.server.protocolAttachments[key]; ok {
		changed = existing != attachment
	}
	if takeOwner || session.server.protocolResizeOwners[attachment.TerminalID] == "" {
		session.server.protocolOwnerEpoch++
		attachment.ResizePolicy = protocol.ResizePolicyOwner
		attachment.Epoch = session.server.protocolOwnerEpoch
		session.server.protocolResizeOwners[attachment.TerminalID] = key
		changed = true
	}
	session.server.protocolAttachments[key] = attachment
	control := session.resizeControlForGlobalAttachmentLocked(attachment, size)
	session.publishProtocolAttachmentChangedLocked(attachment.TerminalID, changed)
	return control
}

func (session *protocolSession) unregisterProtocolAttachments(detached []protocolAttachment) {
	if len(detached) == 0 {
		return
	}
	session.server.protocolAttachmentMu.Lock()
	defer session.server.protocolAttachmentMu.Unlock()
	changedTerminals := make(map[string]bool)
	for _, attachment := range detached {
		key := attachmentKey(attachment)
		if _, ok := session.server.protocolAttachments[key]; ok {
			delete(session.server.protocolAttachments, key)
			changedTerminals[attachment.TerminalID] = true
		}
		delete(session.server.protocolChannelIndex, protocolAttachmentKey{SessionID: attachment.SessionID, Channel: attachment.Channel})
		if session.server.protocolResizeOwners[attachment.TerminalID] == key {
			delete(session.server.protocolResizeOwners, attachment.TerminalID)
			session.promoteGlobalResizeOwnerLocked(attachment.TerminalID)
			changedTerminals[attachment.TerminalID] = true
		}
	}
	for terminalID := range changedTerminals {
		session.publishProtocolAttachmentChangedLocked(terminalID, true)
	}
}

func (session *protocolSession) releaseProtocolAttachments() {
	session.mu.Lock()
	detached := make([]protocolAttachment, 0, len(session.attachments))
	for channel, attachment := range session.attachments {
		delete(session.attachments, channel)
		detached = append(detached, attachment)
	}
	session.mu.Unlock()
	channels := make([]uint16, 0, len(detached))
	for _, attachment := range detached {
		channels = append(channels, attachment.Channel)
	}
	session.stopAttachmentStreams(channels)
	session.unregisterProtocolAttachments(detached)
}

func (session *protocolSession) promoteGlobalResizeOwnerLocked(terminalID string) {
	for key, attachment := range session.server.protocolAttachments {
		if attachment.TerminalID != terminalID || attachment.ResizePolicy == protocol.ResizePolicyObserver {
			continue
		}
		session.server.protocolOwnerEpoch++
		attachment.ResizePolicy = protocol.ResizePolicyOwner
		attachment.Epoch = session.server.protocolOwnerEpoch
		session.server.protocolAttachments[key] = attachment
		session.server.protocolResizeOwners[terminalID] = key
		return
	}
}

func (session *protocolSession) setGlobalResizeLock(attachment protocolAttachment, size protocol.Size, locked bool) *protocol.ResizeControl {
	session.server.protocolAttachmentMu.Lock()
	defer session.server.protocolAttachmentMu.Unlock()
	terminalID := attachment.TerminalID
	if session.server.protocolSizeLocks[terminalID] != locked {
		session.server.protocolOwnerEpoch++
		session.server.protocolSizeLocks[terminalID] = locked
		if ownerKey := session.server.protocolResizeOwners[terminalID]; ownerKey != "" {
			if owner, ok := session.server.protocolAttachments[ownerKey]; ok {
				owner.Epoch = session.server.protocolOwnerEpoch
				session.server.protocolAttachments[ownerKey] = owner
			}
		}
	}
	if current, ok := session.server.protocolAttachments[attachmentKey(attachment)]; ok {
		attachment = current
	}
	control := session.resizeControlForGlobalAttachmentLocked(attachment, size)
	session.publishProtocolAttachmentChangedLocked(terminalID, true)
	return control
}

func (session *protocolSession) resizeControlForGlobalAttachmentLocked(attachment protocolAttachment, size protocol.Size) *protocol.ResizeControl {
	key := attachmentKey(attachment)
	ownerKey := session.server.protocolResizeOwners[attachment.TerminalID]
	owner, hasOwner := session.server.protocolAttachments[ownerKey]
	if !hasOwner || owner.TerminalID != attachment.TerminalID {
		owner = attachment
		ownerKey = key
		session.server.protocolResizeOwners[attachment.TerminalID] = key
	}
	ownership := &protocol.ResizeOwnership{
		OwnerAttachmentID: protocolAttachmentOwnerID(owner),
		OwnerSurfaceID:    owner.SurfaceID,
		OwnerViewID:       owner.ViewID,
		Size:              size,
		SizeLocked:        session.server.protocolSizeLocks[attachment.TerminalID],
		Epoch:             owner.Epoch,
	}
	control := &protocol.ResizeControl{
		CanResize:       ownerKey == key && attachment.ResizePolicy == protocol.ResizePolicyOwner && !ownership.SizeLocked,
		Reason:          protocol.ResizeControlReasonFollower,
		SizeLocked:      ownership.SizeLocked,
		SurfaceID:       attachment.SurfaceID,
		OwnerSurfaceID:  owner.SurfaceID,
		OwnerViewID:     owner.ViewID,
		ResizeOwnership: ownership,
	}
	if ownership.SizeLocked && ownerKey == key && attachment.ResizePolicy == protocol.ResizePolicyOwner {
		control.Reason = protocol.ResizeControlReasonSizeLocked
	} else if attachment.ResizePolicy == protocol.ResizePolicyObserver {
		control.Reason = protocol.ResizeControlReasonObserver
	} else if control.CanResize {
		control.Reason = protocol.ResizeControlReasonOwner
	}
	return control
}

func (session *protocolSession) applyProtocolOwnershipToInfo(out *protocol.TerminalInfo, size protocol.Size) {
	session.server.applyProtocolOwnershipToInfo(out, size)
}

func (server *Server) applyProtocolOwnershipToInfo(out *protocol.TerminalInfo, size protocol.Size) {
	if out == nil {
		return
	}
	server.protocolAttachmentMu.Lock()
	defer server.protocolAttachmentMu.Unlock()
	ownerKey := server.protocolResizeOwners[out.ID]
	owner, hasOwner := server.protocolAttachments[ownerKey]
	for _, attachment := range server.protocolAttachments {
		if attachment.TerminalID == out.ID {
			out.ResizeOwnerAttachmentCount++
		}
	}
	if hasOwner && owner.TerminalID == out.ID {
		out.ResizeOwnership = &protocol.ResizeOwnership{
			OwnerAttachmentID: protocolAttachmentOwnerID(owner),
			OwnerSurfaceID:    owner.SurfaceID,
			OwnerViewID:       owner.ViewID,
			Size:              size,
			SizeLocked:        server.protocolSizeLocks[out.ID],
			Epoch:             owner.Epoch,
		}
	}
}

func (session *protocolSession) publishProtocolAttachmentChangedLocked(terminalID string, changed bool) {
	if !changed || terminalID == "" {
		return
	}
	info, err := session.server.GetTerminal(terminalID)
	if err != nil {
		return
	}
	terminal := info.Clone()
	session.server.events.publish(Event{
		Type:       EventTerminalMetadataChanged,
		TerminalID: terminalID,
		Terminal:   &terminal,
	})
}

func attachmentKey(attachment protocolAttachment) string {
	return fmt.Sprintf("%d:%d", attachment.SessionID, attachment.Channel)
}

func protocolAttachmentOwnerID(attachment protocolAttachment) string {
	return attachmentKey(attachment)
}

func normalizeResizePolicy(policy string) string {
	switch policy {
	case protocol.ResizePolicyFollower, protocol.ResizePolicyObserver:
		return policy
	default:
		return protocol.ResizePolicyOwner
	}
}

func normalizeAttachResizePolicy(policy string) string {
	switch policy {
	case protocol.ResizePolicyOwner, protocol.ResizePolicyObserver:
		return policy
	default:
		return protocol.ResizePolicyFollower
	}
}

func (session *protocolSession) sendError(id uint64, code int, message string) error {
	payload, err := protocol.EncodeErrorPayload(protocol.ErrorMessage{
		ID: id,
		Error: protocol.ProtocolError{
			Code:    code,
			Message: message,
		},
	})
	if err != nil {
		return err
	}
	return session.sendFrame(0, wire.TypeError, payload)
}

func (session *protocolSession) sendFrame(channel uint16, typ uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	return session.conn.Send(frame)
}

func protocolHistoryWindowFromCore(terminalID string, size Size, window history.HistoryWindow) *protocol.HistoryWindow {
	rows := make([]protocol.CompactRow, len(window.Rows))
	rowKinds := make([]string, len(window.Rows))
	rowOwnership := make([]string, len(window.Rows))
	rowLineIDs := make([]uint64, len(window.Rows))
	rowInLine := make([]int, len(window.Rows))
	for i, row := range window.Rows {
		rows[i] = protocol.CompactRowFromCellsPreserveTrailingBlankCells(protocolCellsFromHistory(row.Cells), true)
		if row.TailFill != nil {
			style := protocolCompactRowStyleFromHistory(row.TailFill.Style)
			rows[i].TailFill = &style
		}
		rowKinds[i] = row.Kind
		rowLineIDs[i] = uint64(row.LineID)
		rowInLine[i] = row.RowInLine
		if row.Committed {
			rowOwnership[i] = protocol.RowOwnershipPersisted
		} else if row.LineID != 0 {
			rowOwnership[i] = protocol.RowOwnershipLiveTailLive
		}
	}
	lines := make([]protocol.HistoryLineSpan, len(window.Spans))
	for i, span := range window.Spans {
		lines[i] = protocol.HistoryLineSpan{
			StartRow:      span.FirstRow,
			EndRow:        span.LastRow,
			RowKind:       span.Kind,
			LogicalLineID: uint64(span.LineID),
			ClippedBefore: span.ClippedBefore,
			ClippedAfter:  span.ClippedAfter,
		}
	}
	return &protocol.HistoryWindow{
		TerminalID:   terminalID,
		Token:        string(window.Token),
		Op:           protocol.HistoryWindowOp(window.Op),
		Size:         protocolSizeFromCore(size),
		Rows:         rows,
		RowKinds:     rowKinds,
		RowOwnership: rowOwnership,
		Lines:        lines,
		LoadedRows:   len(rows),
		TotalRows:    window.TotalRows,
		LoadedLines:  window.LoadedLines,
		LogicalTotal: window.TotalLines,
		HasMore:      window.HasMore,
		Generation:   uint64(window.Generation),
		FirstLineID:  uint64(window.FirstLineID),
		LastLineID:   uint64(window.LastLineID),
		CursorValid:  window.Cursor.Valid,
		CursorLineID: uint64(window.Cursor.BeforeLineID),
		CursorRow:    window.Cursor.BeforeRowInLine,
		RowLineIDs:   rowLineIDs,
		RowInLine:    rowInLine,
		Timestamp:    time.Now().UTC(),
	}
}

func protocolCompactRowStyleFromHistory(style history.CellStyle) protocol.CompactRowStyle {
	return protocol.CompactRowStyle{
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

func protocolCellsFromHistory(cells []history.Cell) []protocol.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]protocol.Cell, len(cells))
	for i, cell := range cells {
		out[i] = protocol.Cell{
			Content:    cell.Text,
			Width:      cell.Width,
			Style:      protocolCellStyleFromHistory(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
	}
	return out
}

func protocolCellStyleFromHistory(style history.CellStyle) protocol.CellStyle {
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

func protocolInfoFromCoreV2(info TerminalInfo) protocol.TerminalInfo {
	return protocol.TerminalInfo{
		ID:        info.ID,
		Name:      info.Name,
		Command:   append([]string(nil), info.Command...),
		Tags:      cloneStringMap(info.Tags),
		Size:      protocolSizeFromCore(info.Size),
		State:     string(info.State),
		CWD:       info.CWD,
		LiveCWD:   info.LiveCWD,
		CreatedAt: info.CreatedAt,
		ExitCode:  copyIntPtr(info.ExitCode),
		ExitedAt:  info.ExitedAt,
	}
}

func (server *Server) ProtocolTerminalInfo(info TerminalInfo) protocol.TerminalInfo {
	out := protocolInfoFromCoreV2(info)
	server.applyProtocolOwnershipToInfo(&out, ProtocolSizeFromCore(info.Size))
	return out
}

func ProtocolEventFromCoreV2(event Event) protocol.Event {
	return protocolEventFromCoreV2(event)
}

func protocolEventFromCoreV2(event Event) protocol.Event {
	out := protocol.Event{
		TerminalID: event.TerminalID,
		Timestamp:  event.Timestamp,
	}
	switch event.Type {
	case EventTerminalCreated:
		out.Type = protocol.EventTerminalCreated
		if event.Terminal != nil {
			out.Created = &protocol.TerminalCreatedData{
				Name:    event.Terminal.Name,
				Command: append([]string(nil), event.Terminal.Command...),
				Size:    protocolSizeFromCore(event.Terminal.Size),
			}
		}
	case EventTerminalExited:
		out.Type = protocol.EventTerminalStateChanged
		out.StateChanged = &protocol.TerminalStateChangedData{
			NewState: string(TerminalStateExited),
		}
		if event.Terminal != nil {
			out.StateChanged.ExitCode = copyIntPtr(event.Terminal.ExitCode)
			out.StateChanged.ExitedAt = event.Terminal.ExitedAt
		}
	case EventTerminalResized:
		out.Type = protocol.EventTerminalResized
		out.Resized = &protocol.TerminalResizedData{
			OldSize: protocolSizeFromCore(event.OldSize),
			NewSize: protocolSizeFromCore(event.NewSize),
		}
	case EventTerminalMetadataChanged:
		out.Type = protocol.EventTerminalMetadataChanged
	case EventTerminalChanged:
		out.Type = protocol.EventTerminalStateChanged
		if event.Terminal != nil && (event.LifecycleKnown || event.Terminal.State == TerminalStateExited) {
			out.StateChanged = &protocol.TerminalStateChangedData{
				NewState: string(event.Terminal.State),
				ExitCode: copyIntPtr(event.Terminal.ExitCode),
				ExitedAt: event.Terminal.ExitedAt,
			}
		}
	case EventTerminalRemoved:
		out.Type = protocol.EventTerminalRemoved
		out.Removed = &protocol.TerminalRemovedData{Reason: "removed"}
	case EventStorageChanged:
		out.Type = protocol.EventStorageChanged
		if event.Storage != nil {
			out.Storage = &protocol.StorageChangedData{
				AppID:   event.Storage.AppID,
				Scope:   protocolStorageScopeFromCore(event.Storage.Scope),
				OwnerID: event.Storage.OwnerID,
				Key:     event.Storage.Key,
				Version: event.Storage.Version,
				Op:      event.Storage.Op,
			}
		}
	case EventWorkbenchChanged:
		out.Type = protocol.EventWorkbenchChanged
		if event.Workbench != nil {
			out.Workbench = &protocol.WorkbenchChangedData{
				WorkspaceID: event.Workbench.WorkspaceID,
				Version:     event.Workbench.Version,
				Action:      event.Workbench.Action,
				ResourceID:  event.Workbench.ResourceID,
			}
		}
	default:
		out.Type = protocol.EventTerminalStateChanged
	}
	return out
}

func eventFilterFromProtocol(params protocol.EventsParams) EventFilter {
	out := EventFilter{TerminalID: params.TerminalID}
	for _, typ := range params.Types {
		switch typ {
		case protocol.EventTerminalCreated:
			out.Types = append(out.Types, EventTerminalCreated)
		case protocol.EventTerminalRemoved:
			out.Types = append(out.Types, EventTerminalRemoved)
		case protocol.EventTerminalStateChanged:
			out.Types = append(out.Types, EventTerminalChanged, EventTerminalExited)
		case protocol.EventTerminalMetadataChanged, protocol.EventTerminalResized:
			if typ == protocol.EventTerminalMetadataChanged {
				out.Types = append(out.Types, EventTerminalMetadataChanged)
			} else {
				out.Types = append(out.Types, EventTerminalResized)
			}
		case protocol.EventStorageChanged:
			out.Types = append(out.Types, EventStorageChanged)
		case protocol.EventWorkbenchChanged:
			out.Types = append(out.Types, EventWorkbenchChanged)
		}
	}
	out.StorageAppID = params.StorageAppID
	out.StorageScope = storageScopeFromProtocol(params.StorageScope)
	out.StorageOwnerID = params.StorageOwnerID
	out.StorageKeyPrefix = params.StorageKeyPrefix
	out.WorkbenchID = params.WorkbenchID
	return out
}

func EventFilterFromProtocol(params protocol.EventsParams) EventFilter {
	return eventFilterFromProtocol(params)
}

func protocolStorageEntryFromCore(entry StorageEntry) protocol.StorageEntry {
	return protocol.StorageEntry{
		AppID:     entry.AppID,
		Scope:     protocolStorageScopeFromCore(entry.Scope),
		OwnerID:   entry.OwnerID,
		Key:       entry.Key,
		Value:     append([]byte(nil), entry.Value...),
		Version:   entry.Version,
		UpdatedAt: entry.UpdatedAt,
	}
}

func ProtocolStorageEntryFromCore(entry StorageEntry) protocol.StorageEntry {
	return protocolStorageEntryFromCore(entry)
}

func protocolStorageScopeFromCore(scope StorageScope) protocol.StorageScope {
	switch scope {
	case StorageScopePrivate:
		return protocol.StorageScopePrivate
	default:
		return protocol.StorageScopePublic
	}
}

func ProtocolStorageScopeFromCore(scope StorageScope) protocol.StorageScope {
	return protocolStorageScopeFromCore(scope)
}

func storageScopeFromProtocol(scope protocol.StorageScope) StorageScope {
	switch scope {
	case protocol.StorageScopePrivate:
		return StorageScopePrivate
	default:
		return StorageScopePublic
	}
}

func StorageScopeFromProtocol(scope protocol.StorageScope) StorageScope {
	return storageScopeFromProtocol(scope)
}

func protocolSizeFromCore(size Size) protocol.Size {
	return protocol.Size{Cols: size.Cols, Rows: size.Rows}
}

func ProtocolSizeFromCore(size Size) protocol.Size {
	return protocolSizeFromCore(size)
}

func coreSizeFromProtocol(size protocol.Size) Size {
	return Size{Cols: size.Cols, Rows: size.Rows}
}

func SizeFromProtocol(size protocol.Size) Size {
	return coreSizeFromProtocol(size)
}

func normalizeAttachMode(mode string) string {
	if mode == "" {
		return "collaborator"
	}
	return mode
}

func (session *protocolSession) storeFrozenSnapshot(terminalID string, snapshot history.FrozenSnapshot) {
	session.historyMu.Lock()
	defer session.historyMu.Unlock()
	if previous := session.historyLatest[terminalID]; previous != "" && previous != snapshot.Token {
		session.dropSupersededFrozenSnapshotsLocked(terminalID, snapshot.Token, previous)
	}
	session.historyPins[snapshot.Token] = frozenHistorySnapshot{
		TerminalID: terminalID,
		Snapshot:   snapshot,
	}
	session.historyLatest[terminalID] = snapshot.Token
}

func (session *protocolSession) dropSupersededFrozenSnapshotsLocked(terminalID string, currentToken string, previousToken string) {
	for token, pin := range session.historyPins {
		if pin.TerminalID != terminalID || token == currentToken || token == previousToken {
			continue
		}
		// 中文说明：保留当前 token 和上一个 token，保证刚被新 latest 超过的
		// copy 会话还能继续 older；更旧 pin 不再无限持有大历史 payload。
		delete(session.historyPins, token)
		pin.Snapshot.ReleaseObserver()
	}
}

func (session *protocolSession) releaseFrozenSnapshot(terminalID string, token string) {
	if token == "" {
		return
	}
	session.historyMu.Lock()
	pin, ok := session.historyPins[token]
	if ok && (terminalID == "" || pin.TerminalID == terminalID) {
		delete(session.historyPins, token)
		for terminalID, latest := range session.historyLatest {
			if latest == token {
				delete(session.historyLatest, terminalID)
			}
		}
	} else {
		ok = false
	}
	session.historyMu.Unlock()
	if ok {
		// 中文说明：token drop 是 observer 生命周期边界，释放后 store 可清理延迟删除 payload。
		pin.Snapshot.ReleaseObserver()
	}
}

func (session *protocolSession) releaseAllFrozenSnapshots() {
	session.historyMu.Lock()
	pins := make([]frozenHistorySnapshot, 0, len(session.historyPins))
	for token, pin := range session.historyPins {
		pins = append(pins, pin)
		delete(session.historyPins, token)
	}
	session.historyLatest = make(map[string]string)
	session.historyMu.Unlock()
	for _, pin := range pins {
		// 中文说明：client session 关闭时兜底释放所有 frozen observer，避免 daemon 长期保留旧版本。
		pin.Snapshot.ReleaseObserver()
	}
}

func (session *protocolSession) sessionFrozenSnapshotToken(terminalID string, base string) string {
	seq := session.nextSnapshot.Add(1)
	if base == "" {
		base = "snap"
	}
	return fmt.Sprintf("%s:%s:%d", base, terminalID, seq)
}

func (session *protocolSession) frozenSnapshot(terminalID string, token string) (history.FrozenSnapshot, bool) {
	session.historyMu.Lock()
	defer session.historyMu.Unlock()
	if token == "" {
		token = session.historyLatest[terminalID]
	}
	if token == "" {
		return history.FrozenSnapshot{}, false
	}
	pin, ok := session.historyPins[token]
	if !ok {
		return history.FrozenSnapshot{}, false
	}
	if pin.TerminalID != terminalID {
		return history.FrozenSnapshot{}, false
	}
	return pin.Snapshot, true
}

func protocolCursorToCore(params protocol.HistoryWindowParams) history.HistoryCursor {
	return history.HistoryCursor{
		Valid:           params.CursorValid,
		BeforeLineID:    history.LogicalLineID(params.BeforeLineID),
		BeforeRowInLine: params.BeforeRowInLine,
	}
}

func protocolAfterCursorToCore(params protocol.HistoryWindowParams) history.HistoryCursor {
	return history.HistoryCursor{
		Valid:           params.AfterCursorValid,
		BeforeLineID:    history.LogicalLineID(params.AfterLineID),
		BeforeRowInLine: params.AfterRowInLine,
	}
}

type historyCopyRange struct {
	startLineID history.LogicalLineID
	startCol    int
	endLineID   history.LogicalLineID
	endCol      int
}

func frozenSnapshotCopyRange(snapshot history.FrozenSnapshot, copyRange historyCopyRange) (string, error) {
	startIndex, ok := snapshot.LineIndex(copyRange.startLineID)
	if !ok {
		return "", ErrStaleHistoryWindow
	}
	endIndex, ok := snapshot.LineIndex(copyRange.endLineID)
	if !ok {
		return "", ErrStaleHistoryWindow
	}
	if startIndex > endIndex || (startIndex == endIndex && copyRange.startCol > copyRange.endCol) {
		startIndex, endIndex = endIndex, startIndex
		copyRange.startLineID, copyRange.endLineID = copyRange.endLineID, copyRange.startLineID
		copyRange.startCol, copyRange.endCol = copyRange.endCol, copyRange.startCol
	}
	var builder strings.Builder
	for index := startIndex; index <= endIndex; index++ {
		line, ok := snapshot.LineAt(index)
		if !ok {
			return "", ErrStaleHistoryWindow
		}
		from := 0
		to := historyLineDisplayWidth(line.Line)
		if index == startIndex {
			from = clampProtocolCopyInt(copyRange.startCol, 0, to)
		}
		if index == endIndex {
			to = clampProtocolCopyInt(copyRange.endCol, 0, to)
		}
		if from > to {
			from, to = to, from
		}
		if index > startIndex {
			builder.WriteByte('\n')
		}
		builder.WriteString(historyLineSliceDisplay(line.Line, from, to))
	}
	return builder.String(), nil
}

func historyLineDisplayWidth(line history.LogicalLine) int {
	width := 0
	for _, cell := range line.Cells {
		width += historyCellDisplayWidthSnapshot(cell)
	}
	return width
}

func historyLineSliceDisplay(line history.LogicalLine, from int, to int) string {
	width := historyLineDisplayWidth(line)
	from = clampProtocolCopyInt(from, 0, width)
	to = clampProtocolCopyInt(to, from, width)
	if to <= from {
		return ""
	}
	var builder strings.Builder
	cursor := 0
	for _, cell := range line.Cells {
		cellWidth := historyCellDisplayWidthSnapshot(cell)
		next := cursor + cellWidth
		if rangesOverlapProtocolCopy(cursor, next, from, to) {
			builder.WriteString(historyCellSliceDisplaySnapshot(cell, from-cursor, to-cursor))
		}
		cursor = next
	}
	return builder.String()
}

func historyCellDisplayWidthSnapshot(cell history.Cell) int {
	if cell.Width > 0 {
		return cell.Width
	}
	return historyCellTextWidth(cell.Text)
}

func historyCellSliceDisplaySnapshot(cell history.Cell, from int, to int) string {
	width := historyCellDisplayWidthSnapshot(cell)
	from = clampProtocolCopyInt(from, 0, width)
	to = clampProtocolCopyInt(to, from, width)
	if to <= from {
		return ""
	}
	textWidth := historyCellTextWidth(cell.Text)
	textPart := ""
	if from < textWidth {
		textEnd := to
		if textEnd > textWidth {
			textEnd = textWidth
		}
		textPart = historyTextSliceDisplaySnapshot(cell.Text, from, textEnd)
	}
	pad := to - maxProtocolCopyInt(from, textWidth)
	if pad <= 0 {
		return textPart
	}
	return textPart + strings.Repeat(" ", pad)
}

func historyTextSliceDisplaySnapshot(text string, from int, to int) string {
	if to <= from {
		return ""
	}
	var builder strings.Builder
	cursor := 0
	for _, cluster := range historyTextClustersSnapshot(text) {
		width := uniseg.StringWidth(cluster)
		next := cursor + width
		if width == 0 {
			next = cursor
		}
		if rangesOverlapProtocolCopy(cursor, maxProtocolCopyInt(next, cursor+1), from, to) {
			builder.WriteString(cluster)
		}
		cursor += width
	}
	return builder.String()
}

func rangesOverlapProtocolCopy(leftFrom int, leftTo int, rightFrom int, rightTo int) bool {
	return leftFrom < rightTo && rightFrom < leftTo
}

func clampProtocolCopyInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxProtocolCopyInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func frozenSnapshotLatestWindow(snapshot history.FrozenSnapshot, cols int, rows int) history.HistoryWindow {
	return frozenSnapshotWindow(snapshot, cols, rows, history.HistoryCursor{}, history.HistoryWindowReplace)
}

func frozenSnapshotOlderWindow(snapshot history.FrozenSnapshot, cols int, rows int, cursor history.HistoryCursor) history.HistoryWindow {
	return frozenSnapshotWindow(snapshot, cols, rows, cursor, history.HistoryWindowPrepend)
}

func frozenSnapshotNewerWindow(snapshot history.FrozenSnapshot, cols int, rows int, cursor history.HistoryCursor) history.HistoryWindow {
	if rows <= 0 {
		rows = 24
	}
	selected, hasMore, ok := projectFrozenSnapshotNewerRowsAfterCursor(snapshot, cols, rows, cursor)
	if !ok {
		return history.HistoryWindow{
			Token:      history.WindowToken(snapshot.Token),
			Op:         history.HistoryWindowAppend,
			Cols:       cols,
			Generation: snapshot.Generation,
		}
	}
	return buildFrozenSnapshotWindow(snapshot, cols, history.HistoryWindowAppend, selected, history.HistoryCursor{}, hasMore)
}

func frozenSnapshotOldestWindow(snapshot history.FrozenSnapshot, cols int, rows int) history.HistoryWindow {
	if rows <= 0 {
		rows = 24
	}
	selected, hasMore := projectFrozenSnapshotOldestHeadRows(snapshot, cols, rows)
	return buildFrozenSnapshotWindow(snapshot, cols, history.HistoryWindowReplace, selected, history.HistoryCursor{}, hasMore)
}

func frozenSnapshotCursorValid(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) bool {
	if cols <= 0 || !cursor.Valid {
		return false
	}
	_, _, ok := snapshotCursorStartPosition(snapshot, cols, cursor)
	return ok
}

func frozenSnapshotAfterCursorValid(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) bool {
	if cols <= 0 || !cursor.Valid || cursor.BeforeLineID == 0 {
		return false
	}
	_, _, ok := snapshotCursorEndPosition(snapshot, cols, cursor)
	return ok
}

func frozenSnapshotBoundaryValid(snapshot history.FrozenSnapshot, cols int, firstLineID uint64, lastLineID uint64) bool {
	if firstLineID == 0 && lastLineID == 0 {
		return true
	}
	if cols <= 0 {
		return false
	}
	lineCount := snapshot.VisibleLineCount()
	if lineCount == 0 {
		return firstLineID == 0 && lastLineID == 0
	}
	if firstLineID == 0 {
		first, ok := snapshot.LineAt(0)
		if !ok {
			return false
		}
		firstLineID = uint64(first.Line.ID)
	}
	last, ok := snapshot.LineAt(lineCount - 1)
	if !ok {
		return false
	}
	if lastLineID == 0 {
		lastLineID = uint64(last.Line.ID)
	}
	if lastLineID != uint64(last.Line.ID) {
		return false
	}
	_, ok = snapshotLineIndex(snapshot, history.LogicalLineID(firstLineID))
	return ok
}

func frozenSnapshotWindow(snapshot history.FrozenSnapshot, cols int, rows int, cursor history.HistoryCursor, op history.HistoryWindowOp) history.HistoryWindow {
	if rows <= 0 {
		rows = 24
	}
	if op == history.HistoryWindowReplace {
		selected, hasMore, cursor := projectFrozenSnapshotLatestTailRows(snapshot, cols, rows)
		return buildFrozenSnapshotWindow(snapshot, cols, op, selected, cursor, hasMore)
	}
	selected, nextCursor, hasMore, ok := projectFrozenSnapshotOlderRowsBeforeCursor(snapshot, cols, rows, cursor)
	if !ok {
		return history.HistoryWindow{
			Token:      history.WindowToken(snapshot.Token),
			Op:         op,
			Cols:       cols,
			Generation: snapshot.Generation,
		}
	}
	return buildFrozenSnapshotWindow(snapshot, cols, op, selected, nextCursor, hasMore)
}

type snapshotProjectedRow struct {
	row          history.VisualRow
	lineRowCount int
	committed    bool
}

func buildFrozenSnapshotWindow(
	snapshot history.FrozenSnapshot,
	cols int,
	op history.HistoryWindowOp,
	selected []snapshotProjectedRow,
	cursor history.HistoryCursor,
	hasMore bool,
) history.HistoryWindow {
	spans, visualRows, firstLine, lastLine := buildSnapshotWindowRows(selected)
	return history.HistoryWindow{
		Token:       history.WindowToken(snapshot.Token),
		Op:          op,
		Cols:        cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     hasMore,
		Generation:  snapshot.Generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalRows:   len(selected),
		TotalLines:  countCommittedSnapshotLines(snapshot),
	}
}

func buildSnapshotWindowRows(rows []snapshotProjectedRow) ([]history.LogicalLineSpan, []history.VisualRow, history.LogicalLineID, history.LogicalLineID) {
	if len(rows) == 0 {
		return nil, nil, 0, 0
	}
	visualRows := make([]history.VisualRow, len(rows))
	spans := make([]history.LogicalLineSpan, 0)
	firstLine := rows[0].row.LineID
	lastLine := rows[len(rows)-1].row.LineID
	for i := 0; i < len(rows); {
		lineID := rows[i].row.LineID
		start := i
		end := i
		for end+1 < len(rows) && rows[end+1].row.LineID == lineID {
			end++
		}
		clippedBefore := rows[start].row.RowInLine > 0
		clippedAfter := rows[end].row.RowInLine < rows[end].lineRowCount-1
		for rowIndex := start; rowIndex <= end; rowIndex++ {
			row := rows[rowIndex].row
			row.Committed = rows[rowIndex].committed
			row.ClippedBefore = clippedBefore
			row.ClippedAfter = clippedAfter
			visualRows[rowIndex] = row
		}
		spans = append(spans, history.LogicalLineSpan{
			LineID:         lineID,
			FirstRow:       start,
			LastRow:        end,
			Kind:           rows[start].row.Kind,
			ClippedBefore:  clippedBefore,
			ClippedAfter:   clippedAfter,
			LineGeneration: rows[start].row.LineGeneration,
		})
		i = end + 1
	}
	return spans, visualRows, firstLine, lastLine
}

func projectFrozenSnapshotLatestRows(snapshot history.FrozenSnapshot, cols int) []snapshotProjectedRow {
	rows := make([]snapshotProjectedRow, 0)
	for i := 0; i < snapshot.VisibleLineCount(); i++ {
		line, ok := snapshot.LineAt(i)
		if !ok {
			continue
		}
		rows = append(rows, projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)...)
	}
	return rows
}

func projectFrozenSnapshotLatestTailRows(snapshot history.FrozenSnapshot, cols int, maxRows int) ([]snapshotProjectedRow, bool, history.HistoryCursor) {
	if start, ok := frozenSnapshotAltScreenFrameTailStart(snapshot); ok {
		// 中文说明：frozen latest 也要保持 alt-screen current frame 隔离；
		// 上方 committed history 只通过 older cursor 暴露，不能自动回填到首屏。
		selected, hasMore := projectFrozenSnapshotTailRowsFromLineIndex(snapshot, cols, maxRows, start)
		return selected, hasMore, latestCursorSnapshotAltScreenTail(snapshot, selected, hasMore)
	}
	if start, ok := frozenSnapshotPrimaryFrameTailStart(snapshot); ok {
		// 中文说明：primary screen app 的 archived frames 属于同一会话历史，
		// 但 latest 首屏只能显示 current frame；旧帧通过 older cursor 进入。
		selected, hasMore := projectFrozenSnapshotTailRowsFromLineIndex(snapshot, cols, maxRows, start)
		return selected, hasMore, latestCursorSnapshotTail(selected, hasMore)
	}
	selected, hasMore := projectFrozenSnapshotTailRowsFromLineIndex(snapshot, cols, maxRows, 0)
	return selected, hasMore, latestCursorSnapshotTail(selected, hasMore)
}

func projectFrozenSnapshotTailRowsFromLineIndex(snapshot history.FrozenSnapshot, cols int, maxRows int, minLineIndex int) ([]snapshotProjectedRow, bool) {
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := minLineIndex > 0
	stop := false
	for i := snapshot.VisibleLineCount() - 1; i >= minLineIndex; i-- {
		line, ok := snapshot.LineAt(i)
		if !ok {
			continue
		}
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
		for rowIndex := len(lineRows) - 1; rowIndex >= 0; rowIndex-- {
			if len(rows) >= maxRows {
				hasMore = true
				stop = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if stop {
			break
		}
	}
	reverseSnapshotProjectedRows(rows)
	return rows, hasMore
}

func frozenSnapshotAltScreenFrameTailStart(snapshot history.FrozenSnapshot) (int, bool) {
	lineCount := snapshot.VisibleLineCount()
	if lineCount == 0 {
		return 0, false
	}
	last, ok := snapshot.LineAt(lineCount - 1)
	if !ok || last.Line.Kind != history.RowKindAltScreenFrame {
		return 0, false
	}
	start := lineCount - 1
	for start > 0 {
		line, ok := snapshot.LineAt(start - 1)
		if !ok || line.Line.Kind != history.RowKindAltScreenFrame {
			break
		}
		start--
	}
	return start, true
}

func frozenSnapshotPrimaryFrameTailStart(snapshot history.FrozenSnapshot) (int, bool) {
	lineCount := snapshot.VisibleLineCount()
	if lineCount == 0 {
		return 0, false
	}
	last, ok := snapshot.LineAt(lineCount - 1)
	if !ok || last.Line.Kind != history.RowKindScreenFrame {
		return 0, false
	}
	start := lineCount - 1
	for start > 0 {
		line, ok := snapshot.LineAt(start - 1)
		if !ok || line.Line.Kind != history.RowKindScreenFrame {
			break
		}
		start--
	}
	return start, true
}

func projectFrozenSnapshotOldestHeadRows(snapshot history.FrozenSnapshot, cols int, maxRows int) ([]snapshotProjectedRow, bool) {
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := false
	for i := 0; i < snapshot.VisibleLineCount(); i++ {
		line, ok := snapshot.LineAt(i)
		if !ok {
			continue
		}
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
		for _, row := range lineRows {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, row)
		}
		if hasMore {
			break
		}
	}
	return rows, hasMore
}

func projectFrozenSnapshotOlderRowsBeforeCursor(snapshot history.FrozenSnapshot, cols int, maxRows int, cursor history.HistoryCursor) ([]snapshotProjectedRow, history.HistoryCursor, bool, bool) {
	startLineIndex, startRowIndex, ok := snapshotCursorStartPosition(snapshot, cols, cursor)
	if !ok {
		return nil, history.HistoryCursor{}, false, false
	}
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := false
	for lineIndex := startLineIndex; lineIndex >= 0; lineIndex-- {
		line, ok := snapshot.LineAt(lineIndex)
		if !ok {
			continue
		}
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
		rowIndex := len(lineRows) - 1
		if lineIndex == startLineIndex {
			rowIndex = startRowIndex
		}
		for ; rowIndex >= 0; rowIndex-- {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if hasMore {
			break
		}
	}
	reverseSnapshotProjectedRows(rows)
	nextCursor := history.HistoryCursor{}
	if hasMore && len(rows) > 0 {
		nextCursor = cursorFromSnapshotRow(rows[0])
	}
	return rows, nextCursor, hasMore, true
}

func projectFrozenSnapshotNewerRowsAfterCursor(snapshot history.FrozenSnapshot, cols int, maxRows int, cursor history.HistoryCursor) ([]snapshotProjectedRow, bool, bool) {
	startLineIndex, startRowIndex, ok := snapshotCursorEndPosition(snapshot, cols, cursor)
	if !ok {
		return nil, false, false
	}
	rows := make([]snapshotProjectedRow, 0, maxRows)
	hasMore := false
	for lineIndex := startLineIndex; lineIndex < snapshot.VisibleLineCount(); lineIndex++ {
		line, ok := snapshot.LineAt(lineIndex)
		if !ok {
			continue
		}
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
		rowIndex := 0
		if lineIndex == startLineIndex {
			rowIndex = startRowIndex
		}
		for ; rowIndex < len(lineRows); rowIndex++ {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if hasMore {
			break
		}
	}
	return rows, hasMore, true
}

func latestCursorSnapshotTail(rows []snapshotProjectedRow, hasMore bool) history.HistoryCursor {
	if !hasMore || len(rows) == 0 {
		return history.HistoryCursor{}
	}
	// 中文说明：copy mode 用的是冻结快照；冻结后 live-tail 行也已经是只读
	// source。older 分页必须从当前页第一行继续往上走，不能跳过尚未 committed
	// 但已经被冻结进 copy/history 的屏幕行。
	return cursorFromSnapshotRow(rows[0])
}

func latestCursorSnapshotAltScreenTail(snapshot history.FrozenSnapshot, rows []snapshotProjectedRow, hasMore bool) history.HistoryCursor {
	if !hasMore || len(rows) == 0 {
		return history.HistoryCursor{}
	}
	// 中文说明：alt-screen current frame 只是一块 transient UI。older 分页应回到
	// committed history 尾部，不能扫过同一个 frozen snapshot 里上一轮 primary frame。
	if countCommittedSnapshotLines(snapshot) > 0 {
		return history.HistoryCursor{Valid: true}
	}
	return history.HistoryCursor{}
}

func snapshotCursorStartPosition(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) (int, int, bool) {
	if !cursor.Valid {
		return -1, -1, false
	}
	if cursor.BeforeLineID == 0 {
		lineCount := countCommittedSnapshotLines(snapshot)
		if lineCount == 0 {
			return -1, -1, false
		}
		lineIndex := lineCount - 1
		line, ok := snapshot.LineAt(lineIndex)
		if !ok {
			return -1, -1, false
		}
		lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
		if len(lineRows) == 0 {
			return lineIndex - 1, -1, true
		}
		return lineIndex, len(lineRows) - 1, true
	}
	lineIndex, ok := snapshotLineIndex(snapshot, cursor.BeforeLineID)
	if !ok {
		return -1, -1, false
	}
	line, ok := snapshot.LineAt(lineIndex)
	if !ok {
		return -1, -1, false
	}
	lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
	for rowIndex, row := range lineRows {
		if row.row.RowInLine == cursor.BeforeRowInLine {
			return lineIndex, rowIndex - 1, true
		}
	}
	return -1, -1, false
}

func snapshotCursorEndPosition(snapshot history.FrozenSnapshot, cols int, cursor history.HistoryCursor) (int, int, bool) {
	if !cursor.Valid || cursor.BeforeLineID == 0 {
		return -1, -1, false
	}
	lineIndex, ok := snapshotLineIndex(snapshot, cursor.BeforeLineID)
	if !ok {
		return -1, -1, false
	}
	line, ok := snapshot.LineAt(lineIndex)
	if !ok {
		return -1, -1, false
	}
	lineRows := projectFrozenSnapshotRows([]history.SnapshotLine{line}, cols)
	for rowIndex, row := range lineRows {
		if row.row.RowInLine == cursor.BeforeRowInLine {
			if rowIndex+1 < len(lineRows) {
				return lineIndex, rowIndex + 1, true
			}
			return lineIndex + 1, 0, true
		}
	}
	return -1, -1, false
}

func snapshotVisibleLines(snapshot history.FrozenSnapshot) []history.SnapshotLine {
	lines := make([]history.SnapshotLine, 0, snapshot.VisibleLineCount())
	for i := 0; i < snapshot.VisibleLineCount(); i++ {
		line, ok := snapshot.LineAt(i)
		if ok {
			lines = append(lines, line)
		}
	}
	return lines
}

func snapshotCommittedLines(snapshot history.FrozenSnapshot) []history.SnapshotLine {
	lines := make([]history.SnapshotLine, 0, countCommittedSnapshotLines(snapshot))
	for i := 0; i < snapshot.VisibleLineCount(); i++ {
		line, ok := snapshot.LineAt(i)
		if !ok {
			continue
		}
		if line.Committed {
			lines = append(lines, line)
		}
	}
	return lines
}

func snapshotCommittedEnd(snapshot history.FrozenSnapshot) int {
	if snapshot.CommittedLines > 0 && snapshot.CommittedLines <= snapshot.VisibleLineCount() {
		return snapshot.CommittedLines
	}
	for index := 0; index < snapshot.VisibleLineCount(); index++ {
		line, ok := snapshot.LineAt(index)
		if !ok {
			continue
		}
		if !line.Committed {
			return index
		}
	}
	return snapshot.VisibleLineCount()
}

func snapshotLineIndex(snapshot history.FrozenSnapshot, id history.LogicalLineID) (int, bool) {
	return snapshot.LineIndex(id)
}

func reverseSnapshotProjectedRows(rows []snapshotProjectedRow) {
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
}

func projectFrozenSnapshotCommittedRows(snapshot history.FrozenSnapshot, cols int) []snapshotProjectedRow {
	lines := make([]history.SnapshotLine, 0, countCommittedSnapshotLines(snapshot))
	for i := 0; i < snapshot.VisibleLineCount(); i++ {
		line, ok := snapshot.LineAt(i)
		if !ok {
			continue
		}
		if line.Committed {
			lines = append(lines, line)
		}
	}
	return projectFrozenSnapshotRows(lines, cols)
}

func projectFrozenSnapshotRows(lines []history.SnapshotLine, cols int) []snapshotProjectedRow {
	var rows []snapshotProjectedRow
	for _, snapLine := range lines {
		lineRows := projectFrozenSnapshotLine(snapLine.Line, cols)
		for _, row := range lineRows {
			rows = append(rows, snapshotProjectedRow{
				row:          row,
				lineRowCount: len(lineRows),
				committed:    snapLine.Committed,
			})
		}
	}
	return rows
}

func projectFrozenSnapshotLine(line history.LogicalLine, cols int) []history.VisualRow {
	cells := normalizeProjectionCellsSnapshot(line.Cells)
	if isFixedGridSnapshotLineKind(line.Kind) {
		row := history.VisualRow{
			Text:           lineTextFromSnapshotCells(cells),
			Cells:          cloneHistoryCellsSnapshot(cells),
			LineID:         line.ID,
			Kind:           line.Kind,
			LineGeneration: line.Generation,
		}
		if line.TailFill != nil {
			fill := *line.TailFill
			row.TailFill = &fill
		}
		return []history.VisualRow{row}
	}
	if len(cells) == 0 {
		row := history.VisualRow{LineID: line.ID, LineGeneration: line.Generation, Kind: line.Kind}
		if line.TailFill != nil {
			fill := *line.TailFill
			row.TailFill = &fill
		}
		return []history.VisualRow{row}
	}
	rows := make([]history.VisualRow, 0)
	rowIndex := 0
	for _, chunk := range wrapCellsSnapshot(cells, cols) {
		rows = append(rows, history.VisualRow{
			Text:           lineTextFromSnapshotCells(chunk),
			Cells:          cloneHistoryCellsSnapshot(chunk),
			LineID:         line.ID,
			RowInLine:      rowIndex,
			Kind:           line.Kind,
			LineGeneration: line.Generation,
		})
		rowIndex++
	}
	if line.TailFill != nil && len(rows) > 0 {
		fill := *line.TailFill
		rows[len(rows)-1].TailFill = &fill
	}
	return rows
}

func isFixedGridSnapshotLineKind(kind string) bool {
	return kind == history.RowKindScreenFrame || kind == history.RowKindArchivedScreenFrame || kind == history.RowKindAltScreenFrame
}

func normalizeProjectionCellsSnapshot(cells []history.Cell) []history.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(cells))
	for _, cell := range cells {
		if cell.Text == "" && cell.Width <= 0 {
			continue
		}
		if cell.Width <= 0 {
			out = append(out, expandHistoryCellSnapshot(cell)...)
			continue
		}
		out = append(out, cell)
	}
	return out
}

func wrapCellsSnapshot(cells []history.Cell, cols int) [][]history.Cell {
	if cols <= 0 {
		return [][]history.Cell{cloneHistoryCellsSnapshot(cells)}
	}
	rows := make([][]history.Cell, 0)
	current := make([]history.Cell, 0)
	width := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		rows = append(rows, cloneHistoryCellsSnapshot(current))
		current = current[:0]
		width = 0
	}
	for _, cell := range cells {
		cellWidth := cell.Width
		if cellWidth <= 0 {
			cellWidth = historyCellTextWidth(cell.Text)
		}
		if cellWidth <= 0 {
			continue
		}
		if width+cellWidth <= cols {
			current = append(current, cell)
			width += cellWidth
			if width >= cols {
				flush()
			}
			continue
		}
		for _, part := range splitMeasuredHistoryCellSnapshot(cell) {
			partWidth := part.Width
			if partWidth <= 0 {
				partWidth = historyCellTextWidth(part.Text)
			}
			if partWidth <= 0 {
				continue
			}
			if width > 0 && width+partWidth > cols {
				flush()
			}
			current = append(current, part)
			width += partWidth
			if width >= cols {
				flush()
			}
		}
	}
	flush()
	if len(rows) == 0 {
		rows = append(rows, nil)
	}
	return rows
}

func cloneHistoryCellsSnapshot(cells []history.Cell) []history.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]history.Cell, len(cells))
	copy(out, cells)
	return out
}

func lineTextFromSnapshotCells(cells []history.Cell) string {
	var builder strings.Builder
	for _, cell := range cells {
		builder.WriteString(cell.Text)
	}
	return builder.String()
}

func historyCellTextWidth(text string) int {
	width := 0
	for _, cluster := range historyTextClustersSnapshot(text) {
		width += uniseg.StringWidth(cluster)
	}
	return width
}

func splitMeasuredHistoryCellSnapshot(cell history.Cell) []history.Cell {
	clusters := historyTextClustersSnapshot(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = uniseg.StringWidth(cluster)
		out = append(out, next)
	}
	return out
}

func expandHistoryCellSnapshot(cell history.Cell) []history.Cell {
	clusters := historyTextClustersSnapshot(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]history.Cell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = uniseg.StringWidth(cluster)
		out = append(out, next)
	}
	return out
}

func historyTextClustersSnapshot(text string) []string {
	if text == "" {
		return nil
	}
	graphemes := uniseg.NewGraphemes(text)
	clusters := make([]string, 0)
	for graphemes.Next() {
		cluster := graphemes.Str()
		if cluster == "\n" {
			continue
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func latestCursorSnapshot(rows []snapshotProjectedRow, selectionStart int) history.HistoryCursor {
	if len(rows) == 0 {
		return history.HistoryCursor{}
	}
	for i := selectionStart; i < len(rows); i++ {
		if !rows[i].committed {
			continue
		}
		if hasCommittedRowBeforeSnapshot(rows, i) {
			return cursorFromSnapshotRow(rows[i])
		}
		return history.HistoryCursor{}
	}
	if hasAnyCommittedRowSnapshot(rows) {
		return history.HistoryCursor{Valid: true}
	}
	return history.HistoryCursor{}
}

func cursorBeforeSelectedSnapshot(rows []snapshotProjectedRow, selectionStart int) history.HistoryCursor {
	if len(rows) == 0 || selectionStart <= 0 {
		return history.HistoryCursor{}
	}
	return cursorFromSnapshotRow(rows[selectionStart])
}

func cursorFromSnapshotRow(row snapshotProjectedRow) history.HistoryCursor {
	return history.HistoryCursor{
		Valid:           true,
		BeforeLineID:    row.row.LineID,
		BeforeRowInLine: row.row.RowInLine,
	}
}

func historyCursorBoundaryIndex(rows []snapshotProjectedRow, cursor history.HistoryCursor) int {
	if !cursor.Valid {
		return -1
	}
	if cursor.BeforeLineID == 0 {
		return len(rows)
	}
	for i, row := range rows {
		if row.row.LineID == cursor.BeforeLineID && row.row.RowInLine == cursor.BeforeRowInLine {
			return i
		}
	}
	return -1
}

func hasCommittedRowBeforeSnapshot(rows []snapshotProjectedRow, index int) bool {
	for i := 0; i < index; i++ {
		if rows[i].committed {
			return true
		}
	}
	return false
}

func hasAnyCommittedRowSnapshot(rows []snapshotProjectedRow) bool {
	for _, row := range rows {
		if row.committed {
			return true
		}
	}
	return false
}

func tailStartSnapshot(totalRows int, maxRows int) int {
	if totalRows <= maxRows {
		return 0
	}
	return totalRows - maxRows
}

func countCommittedSnapshotLines(snapshot history.FrozenSnapshot) int {
	if snapshot.CommittedLines > 0 {
		return snapshot.CommittedLines
	}
	count := 0
	for i := 0; i < snapshot.VisibleLineCount(); i++ {
		line, ok := snapshot.LineAt(i)
		if !ok {
			continue
		}
		if line.Committed {
			count++
		}
	}
	return count
}

func tokenPartHasNumericPrefix(part string, prefix string) bool {
	return strings.HasPrefix(part, prefix) && len(part) > len(prefix) && part[len(prefix)] >= '0' && part[len(prefix)] <= '9'
}

func errorCode(err error) int {
	switch {
	case errors.Is(err, ErrTerminalNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrInvalidTerminalID), errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrInvalidServerSize), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrStaleHistoryWindow), errors.Is(err, ErrInvalidStorageKey), errors.Is(err, ErrStorageVersionConflict), errors.Is(err, ErrInvalidWorkbenchMutation), errors.Is(err, ErrDuplicateWorkbenchResource), errors.Is(err, ErrWorkbenchVersionConflict), errors.Is(err, errProtocolAttachmentMismatch):
		return protocolErrorBadRequest
	case errors.Is(err, ErrStorageEntryNotFound), errors.Is(err, ErrWorkbenchNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrRemoteServiceUnavailable):
		return protocolErrorUnavailable
	default:
		return protocolErrorInternal
	}
}

func copyIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
