package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/perftrace"
	"github.com/lozzow/termx/shared/transport"
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
	server             *Server
	conn               transport.Transport
	scope              TransportScope
	application        ApplicationExecutor
	sessionID          uint64
	sendMu             sync.Mutex
	nextCh             atomic.Uint32
	nextSnapshot       atomic.Uint64
	mu                 sync.RWMutex
	attachments        map[uint16]protocolAttachment
	attachmentTokens   map[string]uint16
	eventSubscriptions map[uint64]applicationEventSubscription
	nextEventSub       uint64
	requests           sync.WaitGroup
	fileMu             sync.Mutex
	fileChannels       map[uint16]*sessionFileTransfer
	fileIDs            map[string]uint16
}

type applicationEventSubscription struct {
	cancel context.CancelFunc
	filter EventFilter
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
	Token        []byte
}

type protocolAttachmentKey struct {
	SessionID uint64
	Channel   uint16
}

type attachmentRequest struct {
	TerminalID   string
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type attachmentDetachRequest struct {
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
}

type attachmentInputRequest struct {
	TerminalID string
	Channel    uint16
	SurfaceID  string
	ViewID     string
	Data       []byte
}

type attachmentResizeControlRequest struct {
	TerminalID   string
	Channel      uint16
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type attachmentResizeOwnership struct {
	OwnerAttachmentID string
	OwnerSurfaceID    string
	OwnerViewID       string
	Size              Size
	SizeLocked        bool
	Epoch             uint64
}

type attachmentResizeControl struct {
	CanResize       bool
	Reason          string
	SizeLocked      bool
	SurfaceID       string
	OwnerSurfaceID  string
	OwnerViewID     string
	ResizeOwnership *attachmentResizeOwnership
}

const (
	attachmentResizePolicyOwner    = "owner"
	attachmentResizePolicyFollower = "follower"
	attachmentResizePolicyObserver = "observer"
)

const (
	attachmentResizeReasonOwner      = "owner"
	attachmentResizeReasonFollower   = "follower"
	attachmentResizeReasonObserver   = "observer"
	attachmentResizeReasonSizeLocked = "size_locked"
)

func protocolAttachmentEqual(left, right protocolAttachment) bool {
	return left.SessionID == right.SessionID &&
		left.TerminalID == right.TerminalID &&
		left.Channel == right.Channel &&
		left.Mode == right.Mode &&
		left.ResizePolicy == right.ResizePolicy &&
		left.SurfaceID == right.SurfaceID &&
		left.ViewID == right.ViewID &&
		left.Epoch == right.Epoch &&
		bytes.Equal(left.Token, right.Token)
}

func newProtocolSession(server *Server, conn transport.Transport, scope TransportScope) *protocolSession {
	session := &protocolSession{
		server:             server,
		conn:               conn,
		scope:              scope.normalized(),
		sessionID:          server.nextProtocolSessionID.Add(1),
		attachments:        make(map[uint16]protocolAttachment),
		attachmentTokens:   make(map[string]uint16),
		fileChannels:       make(map[uint16]*sessionFileTransfer),
		fileIDs:            make(map[string]uint16),
		eventSubscriptions: make(map[uint64]applicationEventSubscription),
	}
	// 中文说明：application executor 与当前连接 session 同寿命；具体 API Layer 装配由 composition root 注入。
	if server.cfg.applicationFactory != nil {
		session.application = server.cfg.applicationFactory(session)
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
		session.releaseAllFileTransfers()
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
			if sendErr := session.sendStreamError(channel, protocolErrorBadRequest, err.Error()); sendErr != nil {
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
	case wire.TypeSessionClose:
		if err := protocol.DecodeSessionClosePayload(payload); err != nil {
			return session.sendError(0, protocolErrorBadRequest, err.Error())
		}
		return io.EOF
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
	if req.Method == "api.execute" {
		return session.dispatchApplicationPayload(ctx, req.Params)
	}
	return nil, false, protocolErrorNotFound, fmt.Errorf("unknown method: %s", req.Method)
}

func (session *protocolSession) clientAccessService() (ClientAccessService, error) {
	service := session.server.ClientAccessService()
	if service == nil {
		return nil, ErrClientAccessServiceUnavailable
	}
	return service, nil
}

func (session *protocolSession) remoteService() (RemoteService, error) {
	service := session.server.RemoteService()
	if service == nil {
		return nil, ErrRemoteServiceUnavailable
	}
	return service, nil
}

func (session *protocolSession) handleStreamFrame(ctx context.Context, channel uint16, typ uint8, payload []byte) error {
	if transfer := session.fileTransferForChannel(channel); transfer != nil {
		return session.handleFileTransferFrame(ctx, transfer, typ, payload)
	}
	attachment, err := session.attachmentForChannel(channel)
	if err != nil {
		return err
	}
	if err := session.scope.allowsAttachment(attachment); err != nil {
		return err
	}
	switch typ {
	case wire.TypeBootstrapDone:
		return nil
	default:
		return fmt.Errorf("unsupported stream frame type %d", typ)
	}
}

func (session *protocolSession) sendStreamError(channel uint16, code int, message string) error {
	payload, err := protocol.EncodeErrorPayload(protocol.ErrorMessage{Error: protocol.ProtocolError{Code: code, Message: message}})
	if err != nil {
		return err
	}
	return session.sendFrame(channel, wire.TypeError, payload)
}

func (session *protocolSession) stopEvents() {
	session.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(session.eventSubscriptions))
	for id, subscription := range session.eventSubscriptions {
		cancels = append(cancels, subscription.cancel)
		delete(session.eventSubscriptions, id)
	}
	session.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (session *protocolSession) clearEventSubscription(id uint64) {
	session.mu.Lock()
	delete(session.eventSubscriptions, id)
	session.mu.Unlock()
}

func (session *protocolSession) attach(params attachmentRequest, publishToken bool) (protocolAttachment, *attachmentResizeControl, error) {
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
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return protocolAttachment{}, nil, fmt.Errorf("allocate attachment token: %w", err)
	}
	// 中文说明：opaque token 的 channel 前缀只属于 protocol binding，用于把 Proto resource handle
	// 重新绑定到现有 stream frame；API Layer、TUI 和第三方客户端不得解析这个内部格式。
	binary.BigEndian.PutUint16(token[:2], channel)
	attachment := protocolAttachment{
		SessionID:    session.sessionID,
		TerminalID:   params.TerminalID,
		Channel:      channel,
		Mode:         normalizeAttachMode(params.Mode),
		ResizePolicy: normalizeAttachResizePolicy(params.ResizePolicy),
		SurfaceID:    params.SurfaceID,
		ViewID:       params.ViewID,
		Token:        token,
	}
	replaced := session.replaceProtocolAttachmentsForView(attachment)
	session.unregisterProtocolAttachments(replaced)
	session.mu.Lock()
	session.attachments[channel] = attachment
	if publishToken {
		session.attachmentTokens[string(token)] = channel
	}
	session.mu.Unlock()
	control := session.registerProtocolAttachment(attachment, info.Size)
	if control != nil && (control.Reason == attachmentResizeReasonOwner || control.Reason == attachmentResizeReasonSizeLocked) {
		if control.ResizeOwnership != nil {
			attachment.Epoch = control.ResizeOwnership.Epoch
		}
		attachment.ResizePolicy = attachmentResizePolicyOwner
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

func (session *protocolSession) publishAttachmentToken(attachment protocolAttachment) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	current, ok := session.attachments[attachment.Channel]
	if !ok || !bytes.Equal(current.Token, attachment.Token) {
		return fmt.Errorf("%w: pending attachment is no longer active", errProtocolAttachmentMismatch)
	}
	session.attachmentTokens[string(attachment.Token)] = attachment.Channel
	return nil
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
		delete(session.attachmentTokens, string(current.Token))
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

func (session *protocolSession) detach(params attachmentDetachRequest) {
	session.mu.Lock()
	var detached []protocolAttachment
	for channel, attachment := range session.attachments {
		if !detachMatches(params, channel, attachment) {
			continue
		}
		delete(session.attachments, channel)
		delete(session.attachmentTokens, string(attachment.Token))
		detached = append(detached, attachment)
	}
	session.mu.Unlock()
	session.unregisterProtocolAttachments(detached)
}

func detachMatches(params attachmentDetachRequest, channel uint16, attachment protocolAttachment) bool {
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

func (session *protocolSession) input(ctx context.Context, params attachmentInputRequest) error {
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

func (session *protocolSession) attachmentForToken(token []byte) (protocolAttachment, error) {
	if len(token) == 0 {
		return protocolAttachment{}, ErrTerminalNotFound
	}
	session.mu.RLock()
	channel, ok := session.attachmentTokens[string(token)]
	attachment := session.attachments[channel]
	session.mu.RUnlock()
	if !ok || attachment.Channel == 0 {
		return protocolAttachment{}, ErrTerminalNotFound
	}
	return attachment, nil
}

func (session *protocolSession) resizeControlForRequest(attachment protocolAttachment, policy string, surfaceID string, viewID string) (*attachmentResizeControl, bool, error) {
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
	control := session.updateProtocolAttachmentControl(attachment, info.Size, attachment.ResizePolicy == attachmentResizePolicyOwner)
	return control, control.CanResize, nil
}

func (session *protocolSession) setResizeLock(params attachmentResizeControlRequest, locked bool) (*attachmentResizeControl, error) {
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
	if ownerAttachmentID != protocolAttachmentOwnerID(attachment) || attachment.ResizePolicy != attachmentResizePolicyOwner {
		return control, nil
	}
	info, err := session.server.GetTerminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	return session.setGlobalResizeLock(attachment, info.Size, locked), nil
}

func (session *protocolSession) resizeControlForOwner(attachment protocolAttachment, size Size) *attachmentResizeControl {
	session.mu.Lock()
	if current, ok := session.attachments[attachment.Channel]; ok {
		current.ResizePolicy = attachmentResizePolicyOwner
		attachment = current
		session.attachments[attachment.Channel] = current
	}
	session.mu.Unlock()
	return session.updateProtocolAttachmentControl(attachment, size, true)
}

func (session *protocolSession) registerProtocolAttachment(attachment protocolAttachment, size Size) *attachmentResizeControl {
	session.server.protocolAttachmentMu.Lock()
	defer session.server.protocolAttachmentMu.Unlock()
	if attachment.SessionID == 0 {
		attachment.SessionID = session.sessionID
	}
	key := attachmentKey(attachment)
	changed := true
	if existing, ok := session.server.protocolAttachments[key]; ok {
		changed = !protocolAttachmentEqual(existing, attachment)
	}
	takeOwner := attachment.ResizePolicy == attachmentResizePolicyOwner || session.server.protocolResizeOwners[attachment.TerminalID] == ""
	if takeOwner {
		session.server.protocolOwnerEpoch++
		attachment.ResizePolicy = attachmentResizePolicyOwner
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

func (session *protocolSession) updateProtocolAttachmentControl(attachment protocolAttachment, size Size, takeOwner bool) *attachmentResizeControl {
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
		changed = !protocolAttachmentEqual(existing, attachment)
	}
	if takeOwner || session.server.protocolResizeOwners[attachment.TerminalID] == "" {
		session.server.protocolOwnerEpoch++
		attachment.ResizePolicy = attachmentResizePolicyOwner
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
		delete(session.attachmentTokens, string(attachment.Token))
		detached = append(detached, attachment)
	}
	session.mu.Unlock()
	session.unregisterProtocolAttachments(detached)
}

func (session *protocolSession) promoteGlobalResizeOwnerLocked(terminalID string) {
	for key, attachment := range session.server.protocolAttachments {
		if attachment.TerminalID != terminalID || attachment.ResizePolicy == attachmentResizePolicyObserver {
			continue
		}
		session.server.protocolOwnerEpoch++
		attachment.ResizePolicy = attachmentResizePolicyOwner
		attachment.Epoch = session.server.protocolOwnerEpoch
		session.server.protocolAttachments[key] = attachment
		session.server.protocolResizeOwners[terminalID] = key
		return
	}
}

func (session *protocolSession) setGlobalResizeLock(attachment protocolAttachment, size Size, locked bool) *attachmentResizeControl {
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

func (session *protocolSession) resizeControlForGlobalAttachmentLocked(attachment protocolAttachment, size Size) *attachmentResizeControl {
	key := attachmentKey(attachment)
	ownerKey := session.server.protocolResizeOwners[attachment.TerminalID]
	owner, hasOwner := session.server.protocolAttachments[ownerKey]
	if !hasOwner || owner.TerminalID != attachment.TerminalID {
		owner = attachment
		ownerKey = key
		session.server.protocolResizeOwners[attachment.TerminalID] = key
	}
	ownership := &attachmentResizeOwnership{
		OwnerAttachmentID: protocolAttachmentOwnerID(owner),
		OwnerSurfaceID:    owner.SurfaceID,
		OwnerViewID:       owner.ViewID,
		Size:              size,
		SizeLocked:        session.server.protocolSizeLocks[attachment.TerminalID],
		Epoch:             owner.Epoch,
	}
	control := &attachmentResizeControl{
		CanResize:       ownerKey == key && attachment.ResizePolicy == attachmentResizePolicyOwner && !ownership.SizeLocked,
		Reason:          attachmentResizeReasonFollower,
		SizeLocked:      ownership.SizeLocked,
		SurfaceID:       attachment.SurfaceID,
		OwnerSurfaceID:  owner.SurfaceID,
		OwnerViewID:     owner.ViewID,
		ResizeOwnership: ownership,
	}
	if ownership.SizeLocked && ownerKey == key && attachment.ResizePolicy == attachmentResizePolicyOwner {
		control.Reason = attachmentResizeReasonSizeLocked
	} else if attachment.ResizePolicy == attachmentResizePolicyObserver {
		control.Reason = attachmentResizeReasonObserver
	} else if control.CanResize {
		control.Reason = attachmentResizeReasonOwner
	}
	return control
}

func (server *Server) protocolAttachmentCount(terminalID string) int {
	server.protocolAttachmentMu.Lock()
	defer server.protocolAttachmentMu.Unlock()
	seen := make(map[string]struct{})
	for _, attachment := range server.protocolAttachments {
		if attachment.TerminalID != terminalID {
			continue
		}
		key := protocolAttachmentViewCountKey(attachment)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
	}
	return len(seen)
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
	case attachmentResizePolicyFollower, attachmentResizePolicyObserver:
		return policy
	default:
		return attachmentResizePolicyOwner
	}
}

func normalizeAttachResizePolicy(policy string) string {
	switch policy {
	case attachmentResizePolicyOwner, attachmentResizePolicyObserver:
		return policy
	default:
		return attachmentResizePolicyFollower
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
	case errors.Is(err, ErrInvalidTerminalID), errors.Is(err, ErrInvalidCommand), errors.Is(err, ErrInvalidServerSize), errors.Is(err, ErrTerminalExited), errors.Is(err, ErrInvalidStorageKey), errors.Is(err, ErrStorageVersionConflict), errors.Is(err, ErrDuplicateTerminal), errors.Is(err, errProtocolAttachmentMismatch):
		return protocolErrorBadRequest
	case errors.Is(err, ErrStorageEntryNotFound):
		return protocolErrorNotFound
	case errors.Is(err, ErrRemoteServiceUnavailable), errors.Is(err, ErrClientAccessServiceUnavailable), errors.Is(err, ErrHistoryNotRebuilt), errors.Is(err, ErrHistoryDisabled):
		return protocolErrorUnavailable
	default:
		return protocolErrorInternal
	}
}

func fileProtocolErrorCode(err error) int {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return protocolErrorNotFound
	case errors.Is(err, os.ErrPermission):
		return protocolErrorForbidden
	case strings.Contains(err.Error(), "must be absolute"), strings.Contains(err.Error(), "cursor"), strings.Contains(err.Error(), "regular file"):
		return protocolErrorBadRequest
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
