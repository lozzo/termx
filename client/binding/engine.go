package binding

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"

	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/wirepb"
	"google.golang.org/protobuf/proto"
)

const (
	// ABIVersion 是 C/JNI/WASM binding 符号与 EventEnvelope 语义版本。
	// 不兼容的 handle ownership、函数签名或事件 oneof 变更必须递增该值。
	ABIVersion uint32 = 2
	// MaxPayloadBytes 限制跨语言单次 protobuf 输入，防止 JNI/WASM 分配无界内存。
	MaxPayloadBytes      = 4 << 20
	defaultEventCapacity = 256
	maxLiveHandles       = 4096
)

var (
	// ErrClosed 表示 binding engine 已关闭，任何新操作都不能创建 session 或复活旧 handle。
	ErrClosed = errors.New("binding engine is closed")
	// ErrInvalidHandle 表示 opaque handle 不属于当前 engine 或类型不匹配。
	ErrInvalidHandle = errors.New("binding handle is invalid")
	// ErrHandleActive 表示调用方试图 release 仍在执行或仍未关闭的 handle。
	ErrHandleActive = errors.New("binding handle is still active")
)

// Host 是 binding 到跨端 Go Client Engine composition root 的唯一依赖。
// request 来自 bindingpb；返回 session 必须已经完成 transport、remote auth、Hello，并由 client/runtime 持有 generation truth。
type Host interface {
	// OpenSession 按 generated binding request 建立一个 ApplicationReadySession；context 取消必须释放未发布资源。
	OpenSession(context.Context, *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadySession, error)
}

// Engine 持有当前跨语言实例的 operation/session handle registry 和有界事件队列。
// handle 只在单个 Engine 内有效且永不复用；业务资源仍由 apipb.ResourceHandle 与 API Layer 管理，不能混入本 registry。
type Engine struct {
	host Host

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu         sync.Mutex
	closed     bool
	nextHandle uint64
	operations map[uint64]*operation
	sessions   map[uint64]*sessionRecord
	streams    map[uint64]*streamRecord

	emitMu    sync.Mutex
	sequence  uint64
	events    chan []byte
	closeOnce sync.Once
}

type operation struct {
	cancel context.CancelFunc
	done   bool
}

type sessionRecord struct {
	session clientruntime.ApplicationReadySession
	closed  bool
}

type streamRecord struct {
	sendMu        sync.Mutex
	stream        clientruntime.ResourceStream
	sessionHandle uint64
	closed        bool
	uploadHash    hash.Hash
	uploadBytes   int64
}

// NewEngine 创建使用默认有界事件容量的 binding engine。
// host 缺失时立即失败，禁止创建运行期才 fallback 的半初始化实例。
func NewEngine(host Host) (*Engine, error) {
	return NewEngineWithEventCapacity(host, defaultEventCapacity)
}

// NewEngineWithEventCapacity 创建测试或平台指定容量的 binding engine。
// capacity 必须为正；队列满时生产者施加背压，结果和事件不得静默丢弃。
func NewEngineWithEventCapacity(host Host, capacity int) (*Engine, error) {
	if host == nil {
		return nil, fmt.Errorf("binding host is required")
	}
	if capacity <= 0 {
		return nil, fmt.Errorf("binding event capacity must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		host: host, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		operations: make(map[uint64]*operation), sessions: make(map[uint64]*sessionRecord), streams: make(map[uint64]*streamRecord), events: make(chan []byte, capacity),
	}, nil
}

// OpenResourceStream 解析 bindingpb.OpenResourceStreamRequest，并把 session-bound framing stream 注册为 opaque handle。
// resource 必须由同一 ready session 的 API result 签发；binding 不解析 opaque token 内部 channel 编号。
func (engine *Engine) OpenResourceStream(sessionHandle uint64, payload []byte) (uint64, error) {
	if err := validatePayload(payload); err != nil {
		return 0, err
	}
	request := &bindingpb.OpenResourceStreamRequest{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, request); err != nil {
		return 0, fmt.Errorf("decode resource stream request: %w", err)
	}
	if request.GetResource() == nil {
		return 0, fmt.Errorf("resource stream handle is required")
	}
	session, err := engine.activeSession(sessionHandle)
	if err != nil {
		return 0, err
	}
	provider, ok := session.(clientruntime.ResourceStreamSession)
	if !ok {
		return 0, fmt.Errorf("application session does not support resource streams")
	}
	stream, err := provider.OpenResourceStream(proto.Clone(request.GetResource()).(*apipb.ResourceHandle))
	if err != nil {
		return 0, err
	}
	if stream == nil {
		return 0, fmt.Errorf("application session returned no resource stream")
	}
	engine.mu.Lock()
	if engine.closed {
		engine.mu.Unlock()
		_ = stream.Close()
		return 0, ErrClosed
	}
	handle, err := engine.allocateHandleLocked()
	if err == nil {
		engine.streams[handle] = &streamRecord{stream: stream, sessionHandle: sessionHandle, uploadHash: sha256.New()}
	}
	engine.mu.Unlock()
	if err != nil {
		_ = stream.Close()
		return 0, err
	}
	go engine.forwardResourceStream(handle, stream)
	return handle, nil
}

// SendResourceStreamFrame 发送 serialized bindingpb.ResourceStreamFrame。
// frame handle 必须与参数一致，且只允许受控 file stream frame type，防止借 binding 写 terminal framing。
func (engine *Engine) SendResourceStreamFrame(streamHandle uint64, payload []byte) error {
	if err := validatePayload(payload); err != nil {
		return err
	}
	frame := &bindingpb.ResourceStreamFrame{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, frame); err != nil {
		return fmt.Errorf("decode resource stream frame: %w", err)
	}
	if frame.GetStreamHandle() != 0 && frame.GetStreamHandle() != streamHandle {
		return fmt.Errorf("resource stream frame handle mismatch")
	}
	record, err := engine.activeStreamRecord(streamHandle)
	if err != nil {
		return err
	}
	record.sendMu.Lock()
	defer record.sendMu.Unlock()
	typ, err := bindingFrameTypeToWire(frame.GetType(), true)
	if err != nil {
		return err
	}
	payloadSnapshot := append([]byte(nil), frame.GetPayload()...)
	if frame.GetType() == bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_DATA {
		var data wirepb.FileTransferData
		if err := proto.Unmarshal(payloadSnapshot, &data); err != nil {
			return fmt.Errorf("decode upload data frame: %w", err)
		}
		engine.mu.Lock()
		if record.closed || data.GetOffset() != record.uploadBytes || len(data.GetData()) == 0 {
			engine.mu.Unlock()
			return fmt.Errorf("upload data frame offset is invalid")
		}
		engine.mu.Unlock()
		if err := record.stream.Send(engine.ctx, typ, payloadSnapshot); err != nil {
			return err
		}
		engine.mu.Lock()
		_, _ = record.uploadHash.Write(data.GetData())
		record.uploadBytes += int64(len(data.GetData()))
		engine.mu.Unlock()
		return nil
	}
	if frame.GetType() == bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_FINISH_AUTO {
		if len(payloadSnapshot) != 0 {
			return fmt.Errorf("automatic upload finish payload must be empty")
		}
		engine.mu.Lock()
		payloadSnapshot, err = proto.Marshal(&wirepb.FileTransferFinish{Size: record.uploadBytes, Sha256: record.uploadHash.Sum(nil)})
		engine.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return record.stream.Send(engine.ctx, typ, payloadSnapshot)
}

// CloseResourceStream 关闭 opaque stream handle；关闭后 handle 仍需 Release 才从 registry 删除。
func (engine *Engine) CloseResourceStream(handle uint64) error {
	engine.mu.Lock()
	if engine.closed {
		engine.mu.Unlock()
		return ErrClosed
	}
	record := engine.streams[handle]
	if record == nil {
		engine.mu.Unlock()
		return ErrInvalidHandle
	}
	if record.closed {
		engine.mu.Unlock()
		return nil
	}
	record.closed = true
	stream := record.stream
	engine.mu.Unlock()
	return stream.Close()
}

// OpenSession 解析 bindingpb.OpenSessionRequest 并异步请求 Host 建立 session。
// 返回 operation handle；完成结果只能通过 NextEvent 的 bindingpb.EventEnvelope 获取。
func (engine *Engine) OpenSession(payload []byte) (uint64, error) {
	if err := validatePayload(payload); err != nil {
		return 0, err
	}
	request := &bindingpb.OpenSessionRequest{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, request); err != nil {
		return 0, fmt.Errorf("decode open session request: %w", err)
	}
	if request.GetRequestId() == "" || request.GetEndpointId() == "" {
		return 0, fmt.Errorf("open session request is incomplete")
	}
	switch request.GetIntent() {
	case bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE,
		bindingpb.ConnectIntent_CONNECT_INTENT_BACKGROUND,
		bindingpb.ConnectIntent_CONNECT_INTENT_PROBE:
	default:
		return 0, fmt.Errorf("open session intent is unsupported")
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	requestSnapshot := proto.Clone(request).(*bindingpb.OpenSessionRequest)
	go engine.runOpen(handle, operationContext, requestSnapshot)
	return handle, nil
}

// Execute 解析完整 apipb.CommandEnvelope，并在指定 session 上异步执行。
// command unknown fields 保留到 Go Client Engine；binding 不解释业务 oneof，也不按 command 类型扩张导出函数。
func (engine *Engine) Execute(sessionHandle uint64, payload []byte) (uint64, error) {
	if err := validatePayload(payload); err != nil {
		return 0, err
	}
	command := &apipb.CommandEnvelope{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, command); err != nil {
		return 0, fmt.Errorf("decode application command: %w", err)
	}
	if command.GetCommand() == nil {
		return 0, fmt.Errorf("application command is required")
	}
	session, err := engine.activeSession(sessionHandle)
	if err != nil {
		return 0, err
	}
	handle, operationContext, err := engine.startOperation()
	if err != nil {
		return 0, err
	}
	commandSnapshot := proto.Clone(command).(*apipb.CommandEnvelope)
	go engine.runExecute(handle, sessionHandle, operationContext, session, commandSnapshot)
	return handle, nil
}

// NextEvent 阻塞读取下一条 bindingpb.EventEnvelope protobuf bytes。
// 返回内容是独立副本；调用方拥有该 buffer，C/WASM wrapper 必须提供匹配的显式 free/release 语义。
func (engine *Engine) NextEvent(ctx context.Context) ([]byte, error) {
	if engine == nil {
		return nil, ErrClosed
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-engine.done:
		return nil, ErrClosed
	case payload := <-engine.events:
		return append([]byte(nil), payload...), nil
	}
}

// Cancel 取消仍在执行的 open/execute operation。
// cancellation 只影响该 operation；最终 CANCELLED 结果仍通过事件队列交付，调用方之后必须 Release operation handle。
func (engine *Engine) Cancel(operationHandle uint64) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed {
		return ErrClosed
	}
	operation := engine.operations[operationHandle]
	if operation == nil {
		return ErrInvalidHandle
	}
	if operation.done {
		return ErrHandleActive
	}
	operation.cancel()
	return nil
}

// CloseSession 请求关闭指定 session，但保留已关闭 handle 直到显式 Release。
// lifecycle goroutine 负责发布唯一 SessionClosedEvent；重复 close 不创建第二个事件或新 generation。
func (engine *Engine) CloseSession(sessionHandle uint64) error {
	engine.mu.Lock()
	if engine.closed {
		engine.mu.Unlock()
		return ErrClosed
	}
	record := engine.sessions[sessionHandle]
	if record == nil {
		engine.mu.Unlock()
		return ErrInvalidHandle
	}
	if record.closed {
		engine.mu.Unlock()
		return nil
	}
	session := record.session
	streams := make([]clientruntime.ResourceStream, 0)
	for _, streamRecord := range engine.streams {
		if streamRecord.sessionHandle == sessionHandle && !streamRecord.closed {
			streamRecord.closed = true
			streams = append(streams, streamRecord.stream)
		}
	}
	engine.mu.Unlock()
	var first error
	for _, stream := range streams {
		if err := stream.Close(); err != nil && first == nil {
			first = err
		}
	}
	if err := session.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

// Release 从本地 handle registry 删除已经完成的 operation 或已经关闭的 session。
// 活动 handle 不能释放；该函数不替代 apipb.ReleaseResourceCommand，daemon-owned resource 必须继续走 Proto API。
func (engine *Engine) Release(handle uint64) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed {
		return ErrClosed
	}
	if operation := engine.operations[handle]; operation != nil {
		if !operation.done {
			return ErrHandleActive
		}
		delete(engine.operations, handle)
		return nil
	}
	if record := engine.sessions[handle]; record != nil {
		if !record.closed {
			return ErrHandleActive
		}
		delete(engine.sessions, handle)
		return nil
	}
	if record := engine.streams[handle]; record != nil {
		if !record.closed {
			return ErrHandleActive
		}
		delete(engine.streams, handle)
		return nil
	}
	return ErrInvalidHandle
}

// Close 取消全部 operation、关闭全部 session，并解除 NextEvent/背压等待。
// 方法幂等；它不释放平台 wrapper 自己分配的输出 buffer，wrapper 仍须执行对应 buffer free。
func (engine *Engine) Close() error {
	if engine == nil {
		return nil
	}
	var first error
	engine.closeOnce.Do(func() {
		engine.mu.Lock()
		engine.closed = true
		for _, operation := range engine.operations {
			operation.cancel()
		}
		sessions := make([]clientruntime.ApplicationReadySession, 0, len(engine.sessions))
		for _, record := range engine.sessions {
			sessions = append(sessions, record.session)
		}
		streams := make([]clientruntime.ResourceStream, 0, len(engine.streams))
		for _, record := range engine.streams {
			streams = append(streams, record.stream)
			record.closed = true
		}
		engine.mu.Unlock()
		engine.cancel()
		close(engine.done)
		for _, session := range sessions {
			if err := session.Close(); err != nil && first == nil {
				first = err
			}
		}
		for _, stream := range streams {
			if err := stream.Close(); err != nil && first == nil {
				first = err
			}
		}
	})
	return first
}

func (engine *Engine) forwardResourceStream(handle uint64, stream clientruntime.ResourceStream) {
	var terminalError error
	defer func() {
		engine.mu.Lock()
		if record := engine.streams[handle]; record != nil {
			record.closed = true
		}
		engine.mu.Unlock()
		event := &bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_ResourceStreamClosed{ResourceStreamClosed: &bindingpb.ResourceStreamClosedEvent{StreamHandle: handle}}}
		if terminalError != nil && !errors.Is(terminalError, io.EOF) && !errors.Is(terminalError, context.Canceled) {
			event.GetResourceStreamClosed().Error = apiError(terminalError)
		}
		engine.emit(event)
	}()
	for {
		typ, payload, err := stream.Receive(engine.ctx)
		if err != nil {
			terminalError = err
			return
		}
		bindingType, err := wireFrameTypeToBinding(typ)
		if err != nil {
			terminalError = err
			return
		}
		engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_ResourceStreamFrame{ResourceStreamFrame: &bindingpb.ResourceStreamFrame{
			StreamHandle: handle, Type: bindingType, Payload: append([]byte(nil), payload...),
		}}})
	}
}

func (engine *Engine) runOpen(handle uint64, ctx context.Context, request *bindingpb.OpenSessionRequest) {
	session, err := engine.host.OpenSession(ctx, request)
	if err == nil {
		if session == nil || session.Done() == nil || session.Stamp().Validate() != nil {
			if session != nil {
				_ = session.Close()
			}
			session = nil
			err = fmt.Errorf("binding host returned an invalid ready session")
		}
	}
	var sessionHandle uint64
	if err == nil {
		engine.mu.Lock()
		operation := engine.operations[handle]
		if engine.closed || operation == nil || ctx.Err() != nil {
			engine.mu.Unlock()
			_ = session.Close()
			err = context.Canceled
			session = nil
		} else {
			var allocateErr error
			sessionHandle, allocateErr = engine.allocateHandleLocked()
			if allocateErr == nil {
				engine.sessions[sessionHandle] = &sessionRecord{session: session}
			}
			engine.mu.Unlock()
			if allocateErr != nil {
				_ = session.Close()
				err = allocateErr
				session = nil
			}
		}
	}
	engine.markOperationDone(handle)
	event := &bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_OpenSession{OpenSession: &bindingpb.OpenSessionResult{
		RequestId: request.GetRequestId(), OperationHandle: handle, SessionHandle: sessionHandle,
	}}}
	if session != nil {
		event.GetOpenSession().Session = runtimeStampToProto(session.Stamp())
	} else {
		event.GetOpenSession().Error = apiError(err)
	}
	engine.emit(event)
	if session != nil {
		go engine.forwardSession(sessionHandle, session)
	}
}

func (engine *Engine) runExecute(handle, sessionHandle uint64, ctx context.Context, session clientruntime.ApplicationReadySession, command *apipb.CommandEnvelope) {
	result, err := session.ExecuteApplication(ctx, command)
	if ctx.Err() != nil {
		result = nil
		err = ctx.Err()
	}
	engine.markOperationDone(handle)
	event := &bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_Execute{Execute: &bindingpb.ExecuteResult{
		OperationHandle: handle, SessionHandle: sessionHandle,
	}}}
	if err != nil {
		event.GetExecute().Error = apiError(err)
	} else if result == nil {
		event.GetExecute().Error = apiError(fmt.Errorf("application session returned no result"))
	} else {
		event.GetExecute().Result = proto.Clone(result).(*apipb.ResultEnvelope)
	}
	engine.emit(event)
}

func (engine *Engine) forwardSession(handle uint64, session clientruntime.ApplicationReadySession) {
	events, err := session.ApplicationEvents(engine.ctx)
	if err != nil {
		_ = session.Close()
		engine.finishSession(handle, session, err)
		return
	}
	for {
		select {
		case <-engine.done:
			return
		case <-session.Done():
			engine.finishSession(handle, session, session.Err())
			return
		case event, ok := <-events:
			if !ok {
				_ = session.Close()
				engine.finishSession(handle, session, io.EOF)
				return
			}
			if event != nil {
				engine.emit(&bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_Application{Application: &bindingpb.ApplicationEvent{
					SessionHandle: handle, Event: proto.Clone(event).(*apipb.EventEnvelope),
				}}})
			}
		}
	}
}

func (engine *Engine) finishSession(handle uint64, session clientruntime.ApplicationReadySession, err error) {
	engine.mu.Lock()
	record := engine.sessions[handle]
	if record == nil || record.session != session || record.closed {
		engine.mu.Unlock()
		return
	}
	record.closed = true
	engine.mu.Unlock()
	event := &bindingpb.EventEnvelope{Event: &bindingpb.EventEnvelope_SessionClosed{SessionClosed: &bindingpb.SessionClosedEvent{
		SessionHandle: handle, Session: runtimeStampToProto(session.Stamp()),
	}}}
	if err != nil && !errors.Is(err, io.EOF) {
		event.GetSessionClosed().Error = apiError(err)
	}
	engine.emit(event)
}

func (engine *Engine) startOperation() (uint64, context.Context, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed {
		return 0, nil, ErrClosed
	}
	handle, err := engine.allocateHandleLocked()
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithCancel(engine.ctx)
	engine.operations[handle] = &operation{cancel: cancel}
	return handle, ctx, nil
}

func (engine *Engine) allocateHandleLocked() (uint64, error) {
	if len(engine.operations)+len(engine.sessions) >= maxLiveHandles || engine.nextHandle == ^uint64(0) {
		return 0, fmt.Errorf("binding handle capacity is exhausted")
	}
	engine.nextHandle++
	if engine.nextHandle == 0 {
		return 0, fmt.Errorf("binding handle sequence is exhausted")
	}
	return engine.nextHandle, nil
}

func (engine *Engine) markOperationDone(handle uint64) {
	engine.mu.Lock()
	if operation := engine.operations[handle]; operation != nil {
		operation.done = true
		operation.cancel()
	}
	engine.mu.Unlock()
}

func (engine *Engine) activeSession(handle uint64) (clientruntime.ApplicationReadySession, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed {
		return nil, ErrClosed
	}
	record := engine.sessions[handle]
	if record == nil || record.closed {
		return nil, ErrInvalidHandle
	}
	return record.session, nil
}

func (engine *Engine) activeStreamRecord(handle uint64) (*streamRecord, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed {
		return nil, ErrClosed
	}
	record := engine.streams[handle]
	if record == nil || record.closed {
		return nil, ErrInvalidHandle
	}
	return record, nil
}

func bindingFrameTypeToWire(typ bindingpb.ResourceStreamFrameType, outbound bool) (uint8, error) {
	switch typ {
	case bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_DATA:
		return 0x21, nil
	case bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_ACK:
		return 0x22, nil
	case bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_FINISH:
		return 0x23, nil
	case bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_RESULT:
		if !outbound {
			return 0x24, nil
		}
	case bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_ERROR:
		if !outbound {
			return 0x04, nil
		}
	case bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_FINISH_AUTO:
		if outbound {
			return 0x23, nil
		}
	}
	return 0, fmt.Errorf("resource stream frame type is unsupported")
}

func wireFrameTypeToBinding(typ uint8) (bindingpb.ResourceStreamFrameType, error) {
	for _, candidate := range []bindingpb.ResourceStreamFrameType{
		bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_DATA,
		bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_ACK,
		bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_FINISH,
		bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_FILE_RESULT,
		bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_ERROR,
	} {
		wireType, _ := bindingFrameTypeToWire(candidate, false)
		if wireType == typ {
			return candidate, nil
		}
	}
	return bindingpb.ResourceStreamFrameType_RESOURCE_STREAM_FRAME_TYPE_UNSPECIFIED, fmt.Errorf("resource stream received unsupported frame type %d", typ)
}

func (engine *Engine) emit(event *bindingpb.EventEnvelope) {
	if engine == nil || event == nil {
		return
	}
	engine.emitMu.Lock()
	defer engine.emitMu.Unlock()
	engine.sequence++
	event.AbiVersion = ABIVersion
	event.Sequence = engine.sequence
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(event)
	if err != nil {
		return
	}
	select {
	case <-engine.done:
	case engine.events <- payload:
	}
}

func validatePayload(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("binding protobuf payload is empty")
	}
	if len(payload) > MaxPayloadBytes {
		return fmt.Errorf("binding protobuf payload exceeds %d bytes", MaxPayloadBytes)
	}
	return nil
}

func runtimeStampToProto(stamp clientruntime.EndpointSessionStamp) *apipb.EndpointSessionStamp {
	return &apipb.EndpointSessionStamp{
		EndpointId: string(stamp.EndpointID), RouteId: string(stamp.RouteID), Generation: uint64(stamp.Generation),
	}
}

func apiError(err error) *apipb.ApiError {
	if err == nil {
		return nil
	}
	code := apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE
	message := "binding operation failed"
	retryable := false
	attempted := true
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code, message = apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED, "binding operation was cancelled"
	case errors.Is(err, ErrInvalidHandle):
		code, message, attempted = apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST, "binding handle is invalid", false
	default:
		switch clientruntime.CodeOf(err) {
		case clientruntime.ErrorInvalidRequest:
			code, message, attempted = apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST, "client runtime rejected the request", false
		case clientruntime.ErrorIdentity, clientruntime.ErrorAuthorization:
			code, message = apipb.ApiErrorCode_API_ERROR_CODE_UNAUTHORIZED, "client authorization failed"
		case clientruntime.ErrorCanceled:
			code, message = apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED, "client operation was cancelled"
		case clientruntime.ErrorStaleSession:
			code, message = apipb.ApiErrorCode_API_ERROR_CODE_STALE_SESSION, "client session is stale"
		case clientruntime.ErrorUnavailable, clientruntime.ErrorUnsupportedRoute:
			code, message, retryable = apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE, "client session is unavailable", true
		}
		var runtimeErr *clientruntime.Error
		if errors.As(err, &runtimeErr) {
			attempted = runtimeErr.Attempted
		}
	}
	return &apipb.ApiError{Code: code, Message: message, Retryable: retryable, Attempted: attempted}
}
