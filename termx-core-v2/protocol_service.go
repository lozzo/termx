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

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-shared/transport"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

const (
	protocolErrorBadRequest  = 400
	protocolErrorForbidden   = 403
	protocolErrorNotFound    = 404
	protocolErrorUnavailable = 503
	protocolErrorInternal    = 500
)

const daemonBoundaryReclaimMinHeapMBEnv = "TERMX_DAEMON_REQUEST_RECLAIM_MIN_HEAP_MB"
const daemonBoundaryReclaimDefaultMinHeapBytes = 0

var errProtocolAttachmentMismatch = errors.New("protocol attachment mismatch")
var daemonBoundaryReclaimMinHeapBytes = parseDaemonBoundaryReclaimMinHeapBytes()
var daemonBoundaryReclaimLastHeapSys atomic.Uint64

func parseDaemonBoundaryReclaimMinHeapBytes() uint64 {
	raw := strings.TrimSpace(os.Getenv(daemonBoundaryReclaimMinHeapMBEnv))
	if raw == "" {
		// 中文说明：protocol request 是交互热路径，默认不能在 response 边界
		// 同步执行 debug.FreeOSMemory；大历史压测后它会把 create/attach/list
		// 这类后续请求串行卡住。需要诊断 RSS 回收时显式设置 env 开启。
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
	server       *Server
	conn         transport.Transport
	scope        TransportScope
	sessionID    uint64
	sendMu       sync.Mutex
	nextCh       atomic.Uint32
	nextSnapshot atomic.Uint64
	mu           sync.RWMutex
	attachments  map[uint16]protocolAttachment
	eventCancels map[uint64]context.CancelFunc
	nextEventSub uint64
	requests     sync.WaitGroup
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
		server:       server,
		conn:         conn,
		scope:        scope.normalized(),
		sessionID:    server.nextProtocolSessionID.Add(1),
		attachments:  make(map[uint16]protocolAttachment),
		eventCancels: make(map[uint64]context.CancelFunc),
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
		session.stopEvents()
		session.releaseProtocolAttachments()
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
	finishTotal := perftrace.Measure("core.protocol.request." + req.Method + ".total")
	responseBytes := 0
	defer func() { finishTotal(responseBytes) }()
	finishDispatch := perftrace.Measure("core.protocol.request." + req.Method + ".dispatch")
	result, binary, code, err := session.dispatchRequest(ctx, req)
	finishDispatch(len(result))
	if err != nil {
		return session.sendError(req.ID, code, err.Error())
	}
	if binary {
		responseBytes = len(result)
		payload, err := protocol.EncodeBinaryResponsePayload(req.ID, result)
		if err != nil {
			return err
		}
		finishSend := perftrace.Measure("core.protocol.request." + req.Method + ".send")
		err = session.sendFrame(0, wire.TypeResponseBinary, payload)
		finishSend(len(payload))
		maybeReclaimDaemonBoundaryHeap()
		return err
	}
	payload, err := protocol.EncodeResponsePayload(protocol.Response{ID: req.ID, Result: result})
	if err != nil {
		return err
	}
	responseBytes = len(payload)
	finishSend := perftrace.Measure("core.protocol.request." + req.Method + ".send")
	err = session.sendFrame(0, wire.TypeResponse, payload)
	finishSend(len(payload))
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
	case "path.list_dirs":
		in := params.(protocol.PathListDirsParams)
		out, err := listPathDirectories(in)
		if err != nil {
			return nil, false, protocolErrorInternal, pathListDirsProtocolError(err)
		}
		return encodeMethodResult(req.Method, out)
	case "path.defaults":
		return encodeMethodResult(req.Method, pathDefaults())
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
		// 中文说明：attach 只注册 input/resize/lifecycle owner；live 画面不再走
		// attachment channel 的 screen.update stream，客户端必须按 invalidation 拉 latest screen。
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
	case "live.screen.get":
		in := params.(protocol.LiveScreenParams)
		finishSnapshot := perftrace.Measure("core.protocol.live_screen_snapshot")
		snapshot, err := session.liveNativeScreenSnapshot(in)
		finishSnapshot(nativeScreenSnapshotApproxBytes(snapshot))
		if err != nil {
			return nil, true, errorCode(err), err
		}
		finishEncode := perftrace.Measure("core.protocol.live_screen_encode")
		payload, err := protocol.EncodeNativeScreenSnapshotPayload(snapshot)
		finishEncode(len(payload))
		if err != nil {
			return nil, true, protocolErrorInternal, err
		}
		return payload, true, 0, nil
	case "live.invalidation.next":
		in := params.(protocol.LiveInvalidationNextParams)
		event, err := session.nextLiveInvalidation(ctx, in)
		if err != nil {
			return nil, true, errorCode(err), err
		}
		payload, err := protocol.EncodeEventPayload(protocolEventFromCoreV2(event))
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
		if err := session.historyRelease(ctx, in); err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, nil)
	case "history.backlog.status":
		in := params.(protocol.GetParams)
		status, err := session.server.TerminalHistoryBacklogStatus(in.TerminalID)
		if err != nil {
			return nil, false, errorCode(err), err
		}
		return encodeMethodResult(req.Method, protocolHistoryBacklogStatusFromCore(status))
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

func protocolHistoryBacklogStatusFromCore(status HistoryBacklogStatus) protocol.HistoryBacklogStatus {
	return protocol.HistoryBacklogStatus{
		TerminalID:            status.TerminalID,
		HistoryEnabled:        status.HistoryEnabled,
		AppliedSeq:            status.AppliedSeq,
		TargetSeq:             status.TargetSeq,
		CatchupPending:        status.CatchupPending,
		PendingTransactions:   status.PendingTransactions,
		PendingBytes:          status.PendingBytes,
		BackpressureMode:      string(status.BackpressureMode),
		BufferLimitBytes:      status.BufferLimitBytes,
		BackpressureEvents:    status.BackpressureEvents,
		BackpressureWaitNanos: status.BackpressureWaitNanos,
		InFlight:              status.InFlight,
		Closed:                status.Closed,
	}
}

func (session *protocolSession) liveNativeScreenSnapshot(params protocol.LiveScreenParams) (*protocol.NativeScreenSnapshot, error) {
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	terminal, err := session.server.Terminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	snapshot := terminal.NativeScreenSnapshot(params.TerminalID)
	out := protocolNativeScreenSnapshotFromCore(snapshot, protocolSizeFromCore(info.Size))
	return &out, nil
}

func protocolNativeScreenSnapshotFromCore(snapshot NativeScreenSnapshot, size protocol.Size) protocol.NativeScreenSnapshot {
	rows := make([]protocol.CompactRow, 0, len(snapshot.Rows))
	for _, row := range snapshot.Rows {
		cells := make([]protocol.Cell, len(row.Cells))
		for i, cell := range row.Cells {
			cells[i] = vtermCellToProtocol(cell)
		}
		rows = append(rows, protocol.BuildCompactRowPreserveTrailingBlankCells(len(cells), func(index int) protocol.Cell {
			return cells[index]
		}))
	}
	if size.Cols == 0 {
		size.Cols = uint16(snapshot.Size.Cols)
	}
	if size.Rows == 0 {
		size.Rows = uint16(snapshot.Size.Rows)
	}
	return protocol.NativeScreenSnapshot{
		TerminalID: snapshot.TerminalID,
		Revision:   uint64(snapshot.Revision),
		Size:       size,
		Rows:       rows,
		AltScreen:  snapshot.AltScreen,
		Cursor:     vtermCursorToProtocol(snapshot.Cursor),
		Modes:      vtermModesToProtocol(snapshot.Modes),
		Timestamp:  snapshot.Timestamp,
	}
}

func nativeScreenSnapshotApproxBytes(snapshot *protocol.NativeScreenSnapshot) int {
	if snapshot == nil {
		return 0
	}
	total := 0
	for _, row := range snapshot.Rows {
		total += len(row.Text)
		for _, run := range row.Runs {
			total += len(run.Text) + len(run.LinkURL) + len(run.LinkParams)
		}
		for _, cell := range row.Cells {
			total += len(cell.Content) + len(cell.LinkURL) + len(cell.LinkParams)
		}
	}
	return total
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
	case wire.TypeBootstrapDone:
		return nil
	default:
		return fmt.Errorf("unsupported stream frame type %d", typ)
	}
}

func (session *protocolSession) historyWindow(ctx context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	if _, err := session.server.GetTerminal(params.TerminalID); err != nil {
		return nil, err
	}
	req := historyWindowRequestFromProtocol(params)
	mode := historyWindowPerfMode(req.Mode)
	finishTotal := perftrace.Measure("core.protocol.history_window." + mode + ".total")
	if req.Mode == history.HistoryWindowModeLatest {
		// 中文说明：protocol latest 为 copy/history 会话建立 core-owned frozen token；
		// TUI 只能原样保存 token/cursor，不能用 live rows 或本地 row count 推断边界。
		finishFreeze := perftrace.Measure("core.protocol.history_window.latest.freeze")
		snapshot, err := session.server.TerminalHistoryFreeze(ctx, params.TerminalID, history.FreezeHistoryRequest{
			TerminalID: params.TerminalID,
			Cols:       req.Cols,
			Limit:      req.Limit,
		})
		finishFreeze(int(snapshot.CommittedUpperBound))
		if err != nil {
			finishTotal(0)
			return nil, err
		}
		req.Token = snapshot.Token
	}
	flushWindow := true
	if req.Mode == history.HistoryWindowModeLatest && req.Token != "" {
		// 中文说明：上面的 TerminalHistoryFreeze 已经等待 history worker 追平并固定
		// token；latest 随后读取同一 frozen token window，不能再把 copy 入口卡在第二个 flush。
		flushWindow = false
	}
	finishWindow := perftrace.Measure("core.protocol.history_window." + mode + ".read")
	window, err := session.server.terminalHistoryWindow(ctx, params.TerminalID, req, flushWindow)
	finishWindow(len(window.Rows))
	if err != nil {
		finishTotal(0)
		return nil, err
	}
	finishEncode := perftrace.Measure("core.protocol.history_window." + mode + ".encode")
	out := protocolHistoryWindowFromDomain(window)
	finishEncode(len(window.Rows))
	finishTotal(len(window.Rows))
	perftrace.Count("core.protocol.history_window."+mode+".rows", len(window.Rows))
	return out, nil
}

func (session *protocolSession) historyCopy(ctx context.Context, params protocol.HistoryWindowParams) ([]byte, error) {
	if _, err := session.server.GetTerminal(params.TerminalID); err != nil {
		return nil, err
	}
	text, err := session.server.TerminalHistoryCopy(ctx, params.TerminalID, historyCopyRequestFromProtocol(params))
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (session *protocolSession) historyRelease(ctx context.Context, params protocol.HistoryWindowParams) error {
	if _, err := session.server.GetTerminal(params.TerminalID); err != nil {
		return err
	}
	return session.server.TerminalHistoryRelease(ctx, params.TerminalID, history.HistoryToken(params.Token))
}

func historyWindowRequestFromProtocol(params protocol.HistoryWindowParams) history.HistoryWindowRequest {
	mode := history.HistoryWindowMode(params.Mode)
	if mode == "" {
		if params.AfterCursorValid {
			mode = history.HistoryWindowModeNewer
		} else if params.CursorValid {
			mode = history.HistoryWindowModeOlder
		} else {
			mode = history.HistoryWindowModeLatest
		}
	}
	beforeRowIndex := params.BeforeRowIndex
	if beforeRowIndex == 0 && params.BeforeRowInLine > 0 {
		// 中文说明：旧 protocol 调用曾把 projection absolute row cursor
		// 放入 BeforeRowInLine；新调用必须使用 BeforeRowIndex。
		beforeRowIndex = params.BeforeRowInLine
	}
	afterRowIndex := params.AfterRowIndex
	if afterRowIndex == 0 && params.AfterRowInLine > 0 {
		afterRowIndex = params.AfterRowInLine
	}
	req := history.HistoryWindowRequest{
		TerminalID: params.TerminalID,
		Mode:       mode,
		Cols:       params.Cols,
		Limit:      params.Limit,
		Token:      history.HistoryToken(params.Token),
		Cursor: history.HistoryCursor{
			Segment:        history.HistorySegment(params.CursorSegment),
			LineID:         history.LogicalLineID(params.BeforeLineID),
			RowInLine:      params.BeforeRowInLine,
			BeforeRowIndex: beforeRowIndex,
			Generation:     history.Generation(params.Generation),
			Token:          history.HistoryToken(params.Token),
			Valid:          params.CursorValid,
		},
		Boundary: history.HistoryBoundary{
			FirstLineID: history.LogicalLineID(params.BoundaryFirstLineID),
			LastLineID:  history.LogicalLineID(params.BoundaryLastLineID),
		},
	}
	if params.AfterCursorValid {
		req.Cursor = history.HistoryCursor{
			Segment:        history.HistorySegment(params.AfterCursorSegment),
			LineID:         history.LogicalLineID(params.AfterLineID),
			RowInLine:      params.AfterRowInLine,
			BeforeRowIndex: afterRowIndex,
			Generation:     history.Generation(params.Generation),
			Token:          history.HistoryToken(params.Token),
			Valid:          true,
		}
	}
	req.Boundary.Cursor = req.Cursor
	if req.Cols <= 0 {
		req.Cols = 80
	}
	return req
}

func historyCopyRequestFromProtocol(params protocol.HistoryWindowParams) history.HistoryCopyRequest {
	req := history.HistoryCopyRequest{
		TerminalID: params.TerminalID,
		Token:      history.HistoryToken(params.Token),
		Cols:       params.Cols,
	}
	if params.RangeValid {
		req.Start = history.HistoryCursor{
			Segment:    history.HistorySegment(params.CursorSegment),
			LineID:     history.LogicalLineID(params.RangeStartLineID),
			RowInLine:  params.RangeStartCol,
			Generation: history.Generation(params.Generation),
			Token:      history.HistoryToken(params.Token),
			Valid:      true,
		}
		req.End = history.HistoryCursor{
			Segment:    history.HistorySegment(params.AfterCursorSegment),
			LineID:     history.LogicalLineID(params.RangeEndLineID),
			RowInLine:  params.RangeEndCol,
			Generation: history.Generation(params.Generation),
			Token:      history.HistoryToken(params.Token),
			Valid:      true,
		}
	}
	return req
}

func protocolHistoryWindowFromDomain(window history.HistoryWindow) *protocol.HistoryWindow {
	rows := make([]protocol.CompactRow, 0, len(window.Rows))
	rowKinds := make([]string, 0, len(window.Rows))
	rowSegments := make([]string, 0, len(window.Rows))
	rowWrapped := make([]bool, 0, len(window.Rows))
	rowOwnership := make([]string, 0, len(window.Rows))
	rowLineIDs := make([]uint64, 0, len(window.Rows))
	rowInLine := make([]int, 0, len(window.Rows))
	rowSessionIDs := make([]uint64, 0, len(window.Rows))
	rowFrameIDs := make([]uint64, 0, len(window.Rows))
	rowFixedGrid := make([]bool, 0, len(window.Rows))
	rowScreenCols := make([]int, 0, len(window.Rows))
	rowScreenRows := make([]int, 0, len(window.Rows))
	rowScreenRowSet := make([]bool, 0, len(window.Rows))
	rowIndexes := make([]int, 0, len(window.Rows))
	for _, row := range window.Rows {
		rows = append(rows, protocolCompactRowFromHistoryCells(row.Cells))
		rowKinds = append(rowKinds, string(row.Kind))
		rowSegments = append(rowSegments, string(row.Segment))
		rowWrapped = append(rowWrapped, row.Wrapped)
		rowOwnership = append(rowOwnership, protocolHistoryRowOwnership(row))
		rowLineIDs = append(rowLineIDs, uint64(row.LineID))
		rowInLine = append(rowInLine, row.RowInLine)
		rowSessionIDs = append(rowSessionIDs, uint64(row.SessionID))
		rowFrameIDs = append(rowFrameIDs, uint64(row.FrameID))
		rowFixedGrid = append(rowFixedGrid, row.FixedGrid)
		rowScreenCols = append(rowScreenCols, row.ScreenCols)
		rowScreenRows = append(rowScreenRows, row.ScreenRow)
		rowScreenRowSet = append(rowScreenRowSet, row.ScreenRowSet)
		rowIndexes = append(rowIndexes, row.ProjectionRowIndex)
	}
	lines := make([]protocol.HistoryLineSpan, 0, len(window.Lines))
	for _, span := range window.Lines {
		lines = append(lines, protocol.HistoryLineSpan{
			StartRow:       span.StartRow,
			EndRow:         protocolHistorySpanEndRow(span),
			RowKind:        string(span.Kind),
			LogicalLineID:  uint64(span.LogicalLineID),
			SessionID:      uint64(span.SessionID),
			FrameID:        uint64(span.FrameID),
			FixedGrid:      protocolHistorySpanFixedGrid(window.Rows, span),
			ScreenCols:     protocolHistorySpanScreenCols(window.Rows, span),
			TimestampStart: span.TimestampStart,
			TimestampEnd:   span.TimestampEnd,
			ClippedBefore:  span.ClippedBefore,
			ClippedAfter:   span.ClippedAfter,
		})
	}
	return &protocol.HistoryWindow{
		TerminalID:      window.TerminalID,
		Token:           string(window.Token),
		Op:              protocol.HistoryWindowOp(window.Op),
		Size:            protocol.Size{Cols: protocolHistoryCols(window.Cols)},
		Rows:            rows,
		RowKinds:        rowKinds,
		RowWrapped:      rowWrapped,
		RowOwnership:    rowOwnership,
		RowSegments:     rowSegments,
		RowSessionIDs:   rowSessionIDs,
		RowFrameIDs:     rowFrameIDs,
		RowFixedGrid:    rowFixedGrid,
		RowScreenCols:   rowScreenCols,
		RowScreenRows:   rowScreenRows,
		RowScreenRowSet: rowScreenRowSet,
		RowIndexes:      rowIndexes,
		Lines:           lines,
		LoadedRows:      len(rows),
		TotalRows:       window.LogicalTotal,
		LoadedLines:     len(lines),
		LogicalTotal:    window.LogicalTotal,
		HasMore:         window.HasMore,
		Generation:      uint64(window.Generation),
		FirstLineID:     uint64(window.Boundary.FirstLineID),
		LastLineID:      uint64(window.Boundary.LastLineID),
		CursorValid:     window.Boundary.Cursor.Valid,
		CursorLineID:    uint64(window.Boundary.Cursor.LineID),
		CursorRow:       window.Boundary.Cursor.RowInLine,
		CursorRowIndex:  window.Boundary.Cursor.BeforeRowIndex,
		CursorSegment:   string(window.Boundary.Cursor.Segment),
		RowLineIDs:      rowLineIDs,
		RowInLine:       rowInLine,
		Timestamp:       window.Timestamp,
	}
}

func protocolCompactRowFromHistoryCells(cells []history.Cell) protocol.CompactRow {
	if len(cells) == 0 {
		return protocol.CompactRow{}
	}
	row := protocol.CompactRow{Cells: make([]protocol.CompactRowCell, 0, len(cells))}
	for _, cell := range cells {
		row.Cells = append(row.Cells, protocol.CompactRowCell{
			Content:    cell.Text,
			Width:      cell.Width,
			Style:      protocolCompactRowStyleFromHistory(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
	}
	return row
}

func protocolCompactRowStyleFromHistory(style history.CellStyle) *protocol.CompactRowStyle {
	if style == (history.CellStyle{}) {
		return nil
	}
	return &protocol.CompactRowStyle{
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

func protocolHistoryRowOwnership(row history.HistoryRow) string {
	if row.Segment == history.HistorySegmentCommitted && row.Committed {
		return protocol.RowOwnershipPersisted
	}
	if row.Segment == history.HistorySegmentCommitted {
		return protocol.RowOwnershipLiveTailLive
	}
	return protocol.RowOwnershipScreen
}

func protocolHistorySpanEndRow(span history.HistoryLineSpan) int {
	if span.EndRow > span.StartRow {
		return span.EndRow - 1
	}
	return span.EndRow
}

func protocolHistorySpanFixedGrid(rows []history.HistoryRow, span history.HistoryLineSpan) bool {
	if span.StartRow < 0 || span.StartRow >= len(rows) {
		return false
	}
	return rows[span.StartRow].FixedGrid
}

func protocolHistorySpanScreenCols(rows []history.HistoryRow, span history.HistoryLineSpan) int {
	if span.StartRow < 0 || span.StartRow >= len(rows) {
		return 0
	}
	return rows[span.StartRow].ScreenCols
}

func protocolHistoryCols(cols int) uint16 {
	if cols <= 0 {
		return 0
	}
	if cols > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(cols)
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
			var event Event
			var ok bool
			select {
			case <-eventCtx.Done():
				return
			case event, ok = <-events:
				if !ok {
					return
				}
			}
			payload, err := protocol.EncodeEventPayload(protocolEventFromCoreV2(event))
			if err != nil {
				continue
			}
			_ = session.sendFrame(0, wire.TypeEvent, payload)
		}
	}()
}

func (session *protocolSession) nextLiveInvalidation(ctx context.Context, params protocol.LiveInvalidationNextParams) (Event, error) {
	if params.TerminalID == "" {
		return Event{}, ErrTerminalNotFound
	}
	// 中文说明：ObservedRevision 只表示客户端已观察到 core native screen 的版本，
	// 不是 rendered revision；core 用它补 one-shot arm 间隙的 wake 边沿。
	return session.server.NextLiveInvalidation(ctx, params.TerminalID, LiveRevision(params.ObservedRevision))
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
	replaced := session.replaceProtocolAttachmentsForView(attachment)
	session.unregisterProtocolAttachments(replaced)
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

func (session *protocolSession) replaceProtocolAttachmentsForView(next protocolAttachment) []protocolAttachment {
	if next.ViewID == "" {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	detached := make([]protocolAttachment, 0)
	for channel, current := range session.attachments {
		if !sameProtocolViewAttachment(next, current) {
			continue
		}
		// 中文说明：同一个 client view 重新 attach 时，新 channel 才是输入/resize
		// 真值；旧 channel 必须从 daemon attachment registry 释放，避免 chrome 计数膨胀。
		delete(session.attachments, channel)
		detached = append(detached, current)
	}
	return detached
}

func sameProtocolViewAttachment(next protocolAttachment, current protocolAttachment) bool {
	if next.ViewID == "" || current.ViewID != next.ViewID {
		return false
	}
	if next.SurfaceID != "" {
		return current.SurfaceID == next.SurfaceID
	}
	return true
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
	seen := make(map[string]struct{})
	for _, attachment := range server.protocolAttachments {
		if attachment.TerminalID != out.ID {
			continue
		}
		key := protocolAttachmentViewCountKey(attachment)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.ResizeOwnerAttachmentCount++
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

func protocolAttachmentViewCountKey(attachment protocolAttachment) string {
	if attachment.ViewID != "" {
		// 中文说明：attachment count 对外表达“有几个 client view 连到 terminal”，
		// 不能把同一 view 重试 attach 产生的历史 channel 算成多个连接。
		return attachment.SurfaceID + "\x00" + attachment.ViewID
	}
	return attachmentKey(attachment)
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
		Resources: protocol.TerminalResourceUsage{
			PID:            info.Resources.PID,
			CPUPercentX100: info.Resources.CPUPercentX100,
			MemoryBytes:    info.Resources.MemoryBytes,
			SampledAt:      info.Resources.SampledAt,
		},
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
	case EventTerminalLiveInvalidated:
		out.Type = protocol.EventTerminalLiveInvalidated
		revision := uint64(0)
		if event.Live != nil {
			revision = uint64(event.Live.Revision)
		}
		out.LiveInvalidated = &protocol.LiveScreenInvalidatedData{Revision: revision}
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
		case protocol.EventTerminalLiveInvalidated:
			out.Types = append(out.Types, EventTerminalLiveInvalidated)
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

func tokenPartHasNumericPrefix(part string, prefix string) bool {
	return strings.HasPrefix(part, prefix) && len(part) > len(prefix) && part[len(prefix)] >= '0' && part[len(prefix)] <= '9'
}

func errorCode(err error) int {
	switch {
	case errors.Is(err, ErrTerminalNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrInvalidTerminalID), errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrInvalidServerSize), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrInvalidStorageKey), errors.Is(err, ErrStorageVersionConflict), errors.Is(err, ErrInvalidWorkbenchMutation), errors.Is(err, ErrDuplicateWorkbenchResource), errors.Is(err, ErrWorkbenchVersionConflict), errors.Is(err, errProtocolAttachmentMismatch):
		return protocolErrorBadRequest
	case errors.Is(err, ErrStorageEntryNotFound), errors.Is(err, ErrWorkbenchNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrRemoteServiceUnavailable), errors.Is(err, ErrHistoryNotRebuilt), errors.Is(err, ErrHistoryDisabled):
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
