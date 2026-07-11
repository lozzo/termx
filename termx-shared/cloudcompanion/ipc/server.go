package ipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
)

// ClientFactory 为每条通过 OS peer 验证的连接创建独立 Companion domain client。
// 返回值必须拥有自己的 Hello、caller role、request 与 stream 状态，不能跨连接共享这些真值。
type ClientFactory func() (cloudcompanion.FullClient, error)

// Server 把 platform listener 上的每条 OS connection 映射为独立 Cloud Companion contract connection。
// 它只编排公开 protobuf wire，不实现账号、Hub、Relay、WebRTC 或 terminal 业务。
type Server struct {
	NewClient ClientFactory
	// OnShutdown 在合法 Shutdown response 已成功写回 caller 后触发进程级有序退出。
	// callback 只能停止当前 Companion listener，不能删除凭据、grant 或公开 daemon 状态。
	OnShutdown func()
}

// Serve 接受并验证 listener 上的本地 peer，直到 context 取消或 listener 失败。
// context 取消会关闭 listener 和所有已接受连接；不可信 peer 只关闭自己的连接。
func (server *Server) Serve(ctx context.Context, listener net.Listener) error {
	if server == nil || server.NewClient == nil || listener == nil {
		return fmt.Errorf("invalid Cloud Companion IPC server configuration")
	}
	serveContext, cancelServe := context.WithCancel(ctx)
	go func() {
		<-serveContext.Done()
		_ = listener.Close()
	}()
	var wait sync.WaitGroup
	defer wait.Wait()
	defer cancelServe()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if serveContext.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept Cloud Companion IPC connection: %w", err)
		}
		if err := verifyLocalClient(conn); err != nil {
			_ = conn.Close()
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = server.ServeConn(serveContext, conn)
		}()
	}
}

// ServeConn 在一条已由调用方验证的连接上运行 Companion IPC。
// 该入口主要供平台 listener 和 net.Pipe harness 使用；生产 listener 必须先执行 peer credential/ACL 验证。
func (server *Server) ServeConn(ctx context.Context, conn net.Conn) error {
	if server == nil || server.NewClient == nil || conn == nil {
		return fmt.Errorf("invalid Cloud Companion IPC connection")
	}
	domainClient, err := server.NewClient()
	if err != nil || domainClient == nil {
		_ = conn.Close()
		return fmt.Errorf("create Cloud Companion domain connection")
	}
	connectionContext, cancelConnection := context.WithCancel(ctx)
	state := &serverConnection{
		ctx:            connectionContext,
		cancel:         cancelConnection,
		conn:           conn,
		client:         domainClient,
		serverShutdown: server.OnShutdown,
		active:         make(map[uint64]context.CancelFunc),
		streams:        make(map[uint64]*serverOwnedStream),
		nextStreamID:   1,
	}
	go func() {
		<-connectionContext.Done()
		_ = conn.Close()
	}()
	defer state.close()
	for {
		request := new(cloudpb.IPCRequest)
		if err := readFrame(conn, request); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || connectionContext.Err() != nil {
				return nil
			}
			return err
		}
		if request.GetRequestId() == 0 || request.GetRequestId() <= state.lastRequestID || request.GetOperation() == nil {
			state.writeError(request.GetRequestId(), protocolIPCError("invalid or replayed companion IPC request"))
			return nil
		}
		state.lastRequestID = request.GetRequestId()
		if !state.helloDone {
			if request.GetHello() == nil {
				state.writeError(request.GetRequestId(), protocolIPCError("companion Hello must be the first request"))
				return nil
			}
			response, err := domainClient.Hello(connectionContext, request.GetHello())
			if err != nil {
				state.writeError(request.GetRequestId(), err)
				return nil
			}
			state.helloDone = true
			if err := state.write(&cloudpb.IPCResponse{RequestId: request.GetRequestId(), Result: &cloudpb.IPCResponse_Hello{Hello: response}}); err != nil {
				return err
			}
			continue
		}
		if cancel := request.GetCancel(); cancel != nil {
			state.cancelRequest(cancel.GetTargetRequestId())
			if err := state.writeAcknowledgement(request.GetRequestId()); err != nil {
				return err
			}
			continue
		}
		if closeStream := request.GetCloseStream(); closeStream != nil {
			state.closeStream(closeStream.GetStreamId())
			if err := state.writeAcknowledgement(request.GetRequestId()); err != nil {
				return err
			}
			continue
		}
		if request.GetHello() != nil {
			state.writeError(request.GetRequestId(), protocolIPCError("companion Hello already completed"))
			continue
		}
		requestContext, cancelRequest := context.WithCancel(connectionContext)
		state.mu.Lock()
		state.active[request.GetRequestId()] = cancelRequest
		state.mu.Unlock()
		go state.runRequest(requestContext, request, cancelRequest)
	}
}

type serverConnection struct {
	ctx    context.Context
	cancel context.CancelFunc
	conn   net.Conn
	client cloudcompanion.FullClient

	writeMu        sync.Mutex
	mu             sync.Mutex
	helloDone      bool
	lastRequestID  uint64
	nextStreamID   uint64
	active         map[uint64]context.CancelFunc
	streams        map[uint64]*serverOwnedStream
	closeOnce      sync.Once
	serverShutdown func()
}

func (connection *serverConnection) runRequest(ctx context.Context, request *cloudpb.IPCRequest, cancel context.CancelFunc) {
	defer func() {
		connection.mu.Lock()
		delete(connection.active, request.GetRequestId())
		connection.mu.Unlock()
	}()
	response, stream, err := connection.dispatch(ctx, request)
	if err != nil {
		cancel()
		connection.writeError(request.GetRequestId(), err)
		return
	}
	if stream != nil {
		streamID := connection.registerStream(stream, cancel)
		if streamID == 0 {
			cancel()
			_ = stream.Close()
			connection.writeError(request.GetRequestId(), protocolIPCError("companion stream id overflow"))
			return
		}
		if err := connection.write(&cloudpb.IPCResponse{RequestId: request.GetRequestId(), Result: &cloudpb.IPCResponse_StreamOpened{StreamOpened: &cloudpb.IPCStreamOpened{StreamId: streamID}}}); err != nil {
			connection.closeStream(streamID)
			connection.cancel()
			return
		}
		switch typed := stream.(type) {
		case cloudcompanion.PresenceStream:
			go connection.pumpPresence(streamID, typed)
		case cloudcompanion.SignalingStream:
			go connection.pumpSignaling(streamID, typed)
		default:
			connection.closeStream(streamID)
			connection.cancel()
		}
		return
	}
	cancel()
	response.RequestId = request.GetRequestId()
	if err := connection.write(response); err != nil {
		connection.cancel()
		_ = connection.conn.Close()
		return
	}
	if request.GetShutdown() != nil && connection.serverShutdown != nil {
		go connection.serverShutdown()
	}
}

func (connection *serverConnection) dispatch(ctx context.Context, request *cloudpb.IPCRequest) (*cloudpb.IPCResponse, io.Closer, error) {
	switch operation := request.GetOperation().(type) {
	case *cloudpb.IPCRequest_Hello:
		response, err := connection.client.Hello(ctx, operation.Hello)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_Hello{Hello: response}}, nil, err
	case *cloudpb.IPCRequest_Status:
		response, err := connection.client.Status(ctx, operation.Status)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_Status{Status: response}}, nil, err
	case *cloudpb.IPCRequest_BeginLogin:
		response, err := connection.client.BeginLogin(ctx, operation.BeginLogin)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_LoginFlow{LoginFlow: response}}, nil, err
	case *cloudpb.IPCRequest_CompleteLogin:
		response, err := connection.client.CompleteLogin(ctx, operation.CompleteLogin)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_CompleteLogin{CompleteLogin: response}}, nil, err
	case *cloudpb.IPCRequest_BeginDeviceEnrollment:
		response, err := connection.client.BeginDeviceEnrollment(ctx, operation.BeginDeviceEnrollment)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_EnrollmentChallenge{EnrollmentChallenge: response}}, nil, err
	case *cloudpb.IPCRequest_CompleteDeviceEnrollment:
		response, err := connection.client.CompleteDeviceEnrollment(ctx, operation.CompleteDeviceEnrollment)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_CompleteEnrollment{CompleteEnrollment: response}}, nil, err
	case *cloudpb.IPCRequest_Logout:
		response, err := connection.client.Logout(ctx, operation.Logout)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_Logout{Logout: response}}, nil, err
	case *cloudpb.IPCRequest_Doctor:
		response, err := connection.client.Doctor(ctx, operation.Doctor)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_Doctor{Doctor: response}}, nil, err
	case *cloudpb.IPCRequest_Shutdown:
		response, err := connection.client.Shutdown(ctx, operation.Shutdown)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_Shutdown{Shutdown: response}}, nil, err
	case *cloudpb.IPCRequest_ResolveEndpoint:
		response, err := connection.client.ResolveEndpoint(ctx, operation.ResolveEndpoint)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_ResolvedEndpoint{ResolvedEndpoint: response}}, nil, err
	case *cloudpb.IPCRequest_OpenPresence:
		stream, err := connection.client.OpenPresence(ctx, operation.OpenPresence)
		return nil, stream, err
	case *cloudpb.IPCRequest_CreateSignalingSession:
		stream, err := connection.client.CreateSignalingSession(ctx, operation.CreateSignalingSession)
		return nil, stream, err
	case *cloudpb.IPCRequest_CompleteSignalingOffer:
		response, err := connection.client.CompleteSignalingOffer(ctx, operation.CompleteSignalingOffer)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_CompleteSignalingOffer{CompleteSignalingOffer: response}}, nil, err
	case *cloudpb.IPCRequest_AcquireRelayLease:
		response, err := connection.client.AcquireRelayLease(ctx, operation.AcquireRelayLease)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_RelayLease{RelayLease: response}}, nil, err
	case *cloudpb.IPCRequest_PlanManagedRoute:
		response, err := connection.client.PlanManagedRoute(ctx, operation.PlanManagedRoute)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_ManagedRoutePlan{ManagedRoutePlan: response}}, nil, err
	case *cloudpb.IPCRequest_ReportPathQuality:
		response, err := connection.client.ReportPathQuality(ctx, operation.ReportPathQuality)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_ReportPathQuality{ReportPathQuality: response}}, nil, err
	case *cloudpb.IPCRequest_ReportConnectionOutcome:
		response, err := connection.client.ReportConnectionOutcome(ctx, operation.ReportConnectionOutcome)
		return &cloudpb.IPCResponse{Result: &cloudpb.IPCResponse_ReportConnectionOutcome{ReportConnectionOutcome: response}}, nil, err
	default:
		return nil, nil, protocolIPCError("unsupported companion IPC operation")
	}
}

func (connection *serverConnection) registerStream(stream io.Closer, cancel context.CancelFunc) uint64 {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	streamID := connection.nextStreamID
	connection.nextStreamID++
	if streamID == 0 {
		return 0
	}
	connection.streams[streamID] = &serverOwnedStream{Closer: stream, cancel: cancel}
	return streamID
}

func (connection *serverConnection) cancelRequest(requestID uint64) {
	connection.mu.Lock()
	cancel := connection.active[requestID]
	connection.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (connection *serverConnection) closeStream(streamID uint64) {
	connection.mu.Lock()
	stream := connection.streams[streamID]
	delete(connection.streams, streamID)
	connection.mu.Unlock()
	if stream != nil {
		stream.cancel()
		_ = stream.Closer.Close()
	}
}

func (connection *serverConnection) pumpPresence(streamID uint64, stream cloudcompanion.PresenceStream) {
	defer connection.closeStream(streamID)
	for {
		event, err := stream.Receive()
		if err != nil {
			connection.finishStream(streamID, err)
			return
		}
		if event == nil {
			connection.finishStream(streamID, protocolIPCError("companion returned an empty presence event"))
			return
		}
		if err := connection.write(&cloudpb.IPCResponse{StreamId: streamID, Result: &cloudpb.IPCResponse_PresenceEvent{PresenceEvent: event}}); err != nil {
			connection.cancel()
			_ = connection.conn.Close()
			return
		}
	}
}

func (connection *serverConnection) pumpSignaling(streamID uint64, stream cloudcompanion.SignalingStream) {
	defer connection.closeStream(streamID)
	for {
		event, err := stream.Receive()
		if err != nil {
			connection.finishStream(streamID, err)
			return
		}
		if event == nil {
			connection.finishStream(streamID, protocolIPCError("companion returned an empty signaling event"))
			return
		}
		if err := connection.write(&cloudpb.IPCResponse{StreamId: streamID, Result: &cloudpb.IPCResponse_SignalingEvent{SignalingEvent: event}}); err != nil {
			connection.cancel()
			_ = connection.conn.Close()
			return
		}
	}
}

func (connection *serverConnection) finishStream(streamID uint64, err error) {
	if errors.Is(err, io.EOF) {
		_ = connection.write(&cloudpb.IPCResponse{StreamId: streamID, Result: &cloudpb.IPCResponse_StreamClosed{StreamClosed: &cloudpb.IPCStreamClosed{}}})
		return
	}
	_ = connection.write(&cloudpb.IPCResponse{StreamId: streamID, Result: &cloudpb.IPCResponse_Error{Error: ipcWireError(err)}})
}

func (connection *serverConnection) writeAcknowledgement(requestID uint64) error {
	return connection.write(&cloudpb.IPCResponse{RequestId: requestID, Result: &cloudpb.IPCResponse_Acknowledgement{Acknowledgement: &cloudpb.IPCAcknowledgement{}}})
}

func (connection *serverConnection) writeError(requestID uint64, err error) {
	_ = connection.write(&cloudpb.IPCResponse{RequestId: requestID, Result: &cloudpb.IPCResponse_Error{Error: ipcWireError(err)}})
}

func (connection *serverConnection) write(response *cloudpb.IPCResponse) error {
	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()
	return writeFrame(connection.conn, response)
}

func (connection *serverConnection) close() {
	connection.closeOnce.Do(func() {
		connection.cancel()
		connection.mu.Lock()
		active := connection.active
		streams := connection.streams
		connection.active = make(map[uint64]context.CancelFunc)
		connection.streams = make(map[uint64]*serverOwnedStream)
		connection.mu.Unlock()
		for _, cancel := range active {
			cancel()
		}
		for _, stream := range streams {
			stream.cancel()
			_ = stream.Closer.Close()
		}
		if closer, ok := connection.client.(io.Closer); ok {
			_ = closer.Close()
		}
		_ = connection.conn.Close()
	})
}

type serverOwnedStream struct {
	io.Closer
	cancel context.CancelFunc
}

func ipcWireError(err error) *cloudpb.CloudError {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		err = cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Cloud Companion request was canceled")
	}
	return cloudcompanion.ErrorToWire(err)
}

func protocolIPCError(message string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, message)
}
