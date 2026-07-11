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

var _ cloudcompanion.FullClient = (*Client)(nil)

const clientStreamCapacity = 32

// Client 在单条已验证的本地 OS connection 上实现完整 Cloud Companion contract。
// request 与 stream ownership 只存在于该连接；Close 会解除所有等待，不会尝试旧 Hub 或其他 transport。
type Client struct {
	conn net.Conn

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]*pendingRequest
	streams map[uint64]*clientStream
	readErr error
	done    chan struct{}
	once    sync.Once
}

type pendingRequest struct {
	result    chan *cloudpb.IPCResponse
	abandoned bool
}

// NewClient 接管一条已经通过平台 ACL/owner 验证的本地连接。
// 调用方仍必须首先调用 Hello；格式错误、重复 ID 或连接关闭都按当前 Companion 局部失败处理。
func NewClient(conn net.Conn) (*Client, error) {
	if conn == nil {
		return nil, fmt.Errorf("companion IPC connection is required")
	}
	client := &Client{
		conn:    conn,
		nextID:  1,
		pending: make(map[uint64]*pendingRequest),
		streams: make(map[uint64]*clientStream),
		done:    make(chan struct{}),
	}
	go client.readLoop()
	return client, nil
}

// Dial 验证固定平台 endpoint 后建立 Cloud Companion IPC client。
// endpoint 缺失、未运行和 owner/ACL 不可信分别返回稳定 MISSING、NOT_RUNNING 与 UNTRUSTED 错误。
func Dial(ctx context.Context, endpoint string) (*Client, error) {
	conn, err := dialLocal(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if err := verifyLocalServer(conn); err != nil {
		_ = conn.Close()
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "Cloud Companion local server identity is untrusted")
	}
	client, err := NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

// Close 幂等关闭当前 OS connection 及其 request/stream 等待者。
// 它不请求 Companion 进程退出，也不删除 account/device 云会话。
func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	client.fail(io.ErrClosedPipe)
	return nil
}

// Hello 协商当前 OS connection 的 protocol、caller role 与能力交集。
func (client *Client) Hello(ctx context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_Hello{Hello: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetHello()
	if value == nil {
		return nil, protocolResponseError("Hello")
	}
	return value, nil
}

// Status 返回当前 caller role 的脱敏 Companion 与云会话状态。
func (client *Client) Status(ctx context.Context, request *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_Status{Status: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetStatus()
	if value == nil {
		return nil, protocolResponseError("Status")
	}
	return value, nil
}

// BeginLogin 启动账号登录 flow；账号 secret 不进入公开 IPC response。
func (client *Client) BeginLogin(ctx context.Context, request *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_BeginLogin{BeginLogin: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetLoginFlow()
	if value == nil {
		return nil, protocolResponseError("BeginLogin")
	}
	return value, nil
}

// CompleteLogin 完成账号 flow 并只返回脱敏 session summary。
func (client *Client) CompleteLogin(ctx context.Context, request *cloudpb.CompleteLoginRequest) (*cloudpb.CompleteLoginResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CompleteLogin{CompleteLogin: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetCompleteLogin()
	if value == nil {
		return nil, protocolResponseError("CompleteLogin")
	}
	return value, nil
}

// BeginDeviceEnrollment 获取必须由公开 DeviceIdentity 签名的 enrollment challenge。
func (client *Client) BeginDeviceEnrollment(ctx context.Context, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_BeginDeviceEnrollment{BeginDeviceEnrollment: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetEnrollmentChallenge()
	if value == nil {
		return nil, protocolResponseError("BeginDeviceEnrollment")
	}
	return value, nil
}

// CompleteDeviceEnrollment 提交 daemon proof 并只返回脱敏 device session summary。
func (client *Client) CompleteDeviceEnrollment(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.CompleteDeviceEnrollmentResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CompleteDeviceEnrollment{CompleteDeviceEnrollment: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetCompleteEnrollment()
	if value == nil {
		return nil, protocolResponseError("CompleteDeviceEnrollment")
	}
	return value, nil
}

// Logout 删除请求中明确选择的 Companion 云会话。
func (client *Client) Logout(ctx context.Context, request *cloudpb.LogoutRequest) (*cloudpb.LogoutResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_Logout{Logout: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetLogout()
	if value == nil {
		return nil, protocolResponseError("Logout")
	}
	return value, nil
}

// Doctor 返回固定 code 的脱敏本地诊断。
func (client *Client) Doctor(ctx context.Context, request *cloudpb.DoctorRequest) (*cloudpb.DoctorResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_Doctor{Doctor: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetDoctor()
	if value == nil {
		return nil, protocolResponseError("Doctor")
	}
	return value, nil
}

// Shutdown 请求当前用户的 Companion 进程有序退出。
func (client *Client) Shutdown(ctx context.Context, request *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_Shutdown{Shutdown: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetShutdown()
	if value == nil {
		return nil, protocolResponseError("Shutdown")
	}
	return value, nil
}

// ResolveEndpoint 转发不含 grant 的 managed endpoint 定位请求。
func (client *Client) ResolveEndpoint(ctx context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_ResolveEndpoint{ResolveEndpoint: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetResolvedEndpoint()
	if value == nil {
		return nil, protocolResponseError("ResolveEndpoint")
	}
	return value, nil
}

// OpenPresence 打开当前 daemon connection 拥有的 presence stream。
func (client *Client) OpenPresence(ctx context.Context, request *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
	stream, err := client.openStream(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_OpenPresence{OpenPresence: request}}, streamKindPresence)
	if err != nil {
		return nil, err
	}
	return &presenceStream{stream: stream}, nil
}

// CreateSignalingSession 打开当前 managed session 拥有的 signaling stream。
func (client *Client) CreateSignalingSession(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
	stream, err := client.openStream(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CreateSignalingSession{CreateSignalingSession: request}}, streamKindSignaling)
	if err != nil {
		return nil, err
	}
	return &signalingStream{stream: stream}, nil
}

// CompleteSignalingOffer 返回 daemon 对当前 offer 的 answer 或稳定错误。
func (client *Client) CompleteSignalingOffer(ctx context.Context, request *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CompleteSignalingOffer{CompleteSignalingOffer: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetCompleteSignalingOffer()
	if value == nil {
		return nil, protocolResponseError("CompleteSignalingOffer")
	}
	return value, nil
}

// AcquireRelayLease 获取 caller-specific 短期 Relay lease。
func (client *Client) AcquireRelayLease(ctx context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_AcquireRelayLease{AcquireRelayLease: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetRelayLease()
	if value == nil {
		return nil, protocolResponseError("AcquireRelayLease")
	}
	return value, nil
}

// ReportPathQuality 转发不含 payload 的聚合网络质量摘要。
func (client *Client) ReportPathQuality(ctx context.Context, request *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_ReportPathQuality{ReportPathQuality: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetReportPathQuality()
	if value == nil {
		return nil, protocolResponseError("ReportPathQuality")
	}
	return value, nil
}

// ReportConnectionOutcome 转发一次 managed connection 的稳定路径结果。
func (client *Client) ReportConnectionOutcome(ctx context.Context, request *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	response, err := client.call(ctx, &cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_ReportConnectionOutcome{ReportConnectionOutcome: request}})
	if err != nil {
		return nil, err
	}
	value := response.GetReportConnectionOutcome()
	if value == nil {
		return nil, protocolResponseError("ReportConnectionOutcome")
	}
	return value, nil
}

func (client *Client) call(ctx context.Context, request *cloudpb.IPCRequest) (*cloudpb.IPCResponse, error) {
	if client == nil || request == nil || request.GetOperation() == nil {
		return nil, protocolResponseError("request")
	}
	requestID, result, err := client.registerRequest()
	if err != nil {
		return nil, err
	}
	request.RequestId = requestID
	if err := client.write(request); err != nil {
		client.removePending(requestID)
		client.fail(err)
		return nil, client.connectionError()
	}
	select {
	case response, ok := <-result:
		if !ok || response == nil {
			return nil, client.connectionError()
		}
		if wireErr := response.GetError(); wireErr != nil {
			return nil, cloudcompanion.ErrorFromWire(wireErr)
		}
		return response, nil
	case <-ctx.Done():
		if !client.abandonPending(requestID) {
			// response 已离开 pending map 时，read loop 可能已经注册 stream；异步丢弃负责关闭该孤儿 stream。
			go client.discardCompleted(result)
		}
		client.sendControl(&cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_Cancel{Cancel: &cloudpb.IPCCancelRequest{TargetRequestId: requestID}}})
		return nil, ctx.Err()
	case <-client.done:
		return nil, client.connectionError()
	}
}

func (client *Client) openStream(ctx context.Context, request *cloudpb.IPCRequest, kind streamKind) (*clientStream, error) {
	response, err := client.call(ctx, request)
	if err != nil {
		return nil, err
	}
	opened := response.GetStreamOpened()
	if opened == nil || opened.GetStreamId() == 0 {
		return nil, protocolResponseError("stream open")
	}
	client.mu.Lock()
	stream := client.streams[opened.GetStreamId()]
	if stream != nil {
		stream.kind = kind
	}
	client.mu.Unlock()
	if stream == nil {
		return nil, protocolResponseError("stream registration")
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-stream.done:
		case <-client.done:
		}
	}()
	return stream, nil
}

func (client *Client) registerRequest() (uint64, <-chan *cloudpb.IPCResponse, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	select {
	case <-client.done:
		return 0, nil, client.connectionErrorLocked()
	default:
	}
	requestID := client.nextID
	client.nextID++
	if requestID == 0 {
		return 0, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "companion IPC request id overflow")
	}
	result := make(chan *cloudpb.IPCResponse, 1)
	client.pending[requestID] = &pendingRequest{result: result}
	return requestID, result, nil
}

func (client *Client) sendControl(request *cloudpb.IPCRequest) {
	if request == nil || request.GetOperation() == nil {
		return
	}
	requestID, _, err := client.registerRequest()
	if err != nil {
		return
	}
	client.abandonPending(requestID)
	request.RequestId = requestID
	if err := client.write(request); err != nil {
		client.fail(err)
	}
}

func (client *Client) write(request *cloudpb.IPCRequest) error {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	return writeFrame(client.conn, request)
}

func (client *Client) readLoop() {
	for {
		response := new(cloudpb.IPCResponse)
		if err := readFrame(client.conn, response); err != nil {
			client.fail(err)
			return
		}
		if response.GetRequestId() != 0 {
			client.dispatchResponse(response)
			continue
		}
		if response.GetStreamId() == 0 {
			client.fail(errors.New("companion IPC response has no request or stream id"))
			return
		}
		client.dispatchStream(response)
	}
}

func (client *Client) dispatchResponse(response *cloudpb.IPCResponse) {
	client.mu.Lock()
	pending := client.pending[response.GetRequestId()]
	delete(client.pending, response.GetRequestId())
	if pending == nil {
		client.mu.Unlock()
		client.fail(errors.New("companion IPC responded to an unknown request"))
		return
	}
	opened := response.GetStreamOpened()
	if !pending.abandoned && opened != nil && opened.GetStreamId() != 0 {
		if _, exists := client.streams[opened.GetStreamId()]; exists {
			client.mu.Unlock()
			client.fail(errors.New("companion IPC reused a stream id"))
			return
		}
		client.streams[opened.GetStreamId()] = newClientStream(client, opened.GetStreamId())
	}
	client.mu.Unlock()
	if pending.abandoned {
		close(pending.result)
		if opened != nil && opened.GetStreamId() != 0 {
			client.sendControl(&cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CloseStream{CloseStream: &cloudpb.IPCCloseStreamRequest{StreamId: opened.GetStreamId()}}})
		}
		return
	}
	pending.result <- response
	close(pending.result)
}

func (client *Client) dispatchStream(response *cloudpb.IPCResponse) {
	client.mu.Lock()
	stream := client.streams[response.GetStreamId()]
	client.mu.Unlock()
	if stream == nil {
		client.fail(errors.New("companion IPC sent an event for an unknown stream"))
		return
	}
	if !stream.push(response) {
		client.sendControl(&cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CloseStream{CloseStream: &cloudpb.IPCCloseStreamRequest{StreamId: stream.id}}})
	}
}

func (client *Client) removePending(requestID uint64) {
	client.mu.Lock()
	delete(client.pending, requestID)
	client.mu.Unlock()
}

func (client *Client) abandonPending(requestID uint64) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	pending := client.pending[requestID]
	if pending == nil {
		return false
	}
	pending.abandoned = true
	return true
}

func (client *Client) discardCompleted(result <-chan *cloudpb.IPCResponse) {
	select {
	case response, ok := <-result:
		if !ok || response == nil {
			return
		}
		if opened := response.GetStreamOpened(); opened != nil && opened.GetStreamId() != 0 {
			client.closeOrphanedStream(opened.GetStreamId())
		}
	case <-client.done:
	}
}

func (client *Client) closeOrphanedStream(streamID uint64) {
	client.mu.Lock()
	stream := client.streams[streamID]
	client.mu.Unlock()
	if stream != nil {
		_ = stream.Close()
		return
	}
	client.sendControl(&cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CloseStream{CloseStream: &cloudpb.IPCCloseStreamRequest{StreamId: streamID}}})
}

func (client *Client) removeStream(streamID uint64) {
	client.mu.Lock()
	delete(client.streams, streamID)
	client.mu.Unlock()
}

func (client *Client) fail(err error) {
	client.once.Do(func() {
		client.mu.Lock()
		client.readErr = err
		pending := client.pending
		streams := client.streams
		client.pending = make(map[uint64]*pendingRequest)
		client.streams = make(map[uint64]*clientStream)
		close(client.done)
		client.mu.Unlock()
		_ = client.conn.Close()
		for _, request := range pending {
			close(request.result)
		}
		for _, stream := range streams {
			stream.finish(client.connectionError())
		}
	})
}

func (client *Client) connectionError() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.connectionErrorLocked()
}

func (client *Client) connectionErrorLocked() error {
	if cloudcompanion.CodeOf(client.readErr) != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED {
		return client.readErr
	}
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING, "Cloud Companion IPC connection closed")
}

func protocolResponseError(operation string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an invalid "+operation+" response")
}

type streamKind uint8

const (
	streamKindUnknown streamKind = iota
	streamKindPresence
	streamKindSignaling
)

type clientStream struct {
	client *Client
	id     uint64
	kind   streamKind
	items  chan *cloudpb.IPCResponse
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	err    error
}

func newClientStream(client *Client, id uint64) *clientStream {
	return &clientStream{client: client, id: id, items: make(chan *cloudpb.IPCResponse, clientStreamCapacity), done: make(chan struct{})}
}

func (stream *clientStream) push(response *cloudpb.IPCResponse) bool {
	if response.GetStreamClosed() != nil {
		stream.finish(io.EOF)
		return true
	}
	if wireErr := response.GetError(); wireErr != nil {
		stream.finish(cloudcompanion.ErrorFromWire(wireErr))
		return true
	}
	select {
	case <-stream.done:
		return true
	case stream.items <- response:
		return true
	default:
		stream.finish(cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE, "Cloud Companion IPC stream queue is full"))
		return false
	}
}

func (stream *clientStream) receive() (*cloudpb.IPCResponse, error) {
	select {
	case response := <-stream.items:
		return response, nil
	default:
	}
	select {
	case response := <-stream.items:
		return response, nil
	case <-stream.done:
		stream.mu.Lock()
		err := stream.err
		stream.mu.Unlock()
		if err == nil {
			err = io.EOF
		}
		return nil, err
	}
}

func (stream *clientStream) finish(err error) {
	stream.once.Do(func() {
		stream.mu.Lock()
		stream.err = err
		stream.mu.Unlock()
		stream.client.removeStream(stream.id)
		close(stream.done)
	})
}

func (stream *clientStream) Close() error {
	if stream == nil {
		return nil
	}
	stream.client.sendControl(&cloudpb.IPCRequest{Operation: &cloudpb.IPCRequest_CloseStream{CloseStream: &cloudpb.IPCCloseStreamRequest{StreamId: stream.id}}})
	stream.finish(io.EOF)
	return nil
}

type presenceStream struct{ stream *clientStream }

func (stream *presenceStream) Receive() (*cloudpb.PresenceEvent, error) {
	response, err := stream.stream.receive()
	if err != nil {
		return nil, err
	}
	value := response.GetPresenceEvent()
	if value == nil || stream.stream.kind != streamKindPresence {
		stream.stream.finish(protocolResponseError("presence stream"))
		return nil, protocolResponseError("presence stream")
	}
	return value, nil
}

func (stream *presenceStream) Close() error { return stream.stream.Close() }

type signalingStream struct{ stream *clientStream }

func (stream *signalingStream) Receive() (*cloudpb.SignalingEvent, error) {
	response, err := stream.stream.receive()
	if err != nil {
		return nil, err
	}
	value := response.GetSignalingEvent()
	if value == nil || stream.stream.kind != streamKindSignaling {
		stream.stream.finish(protocolResponseError("signaling stream"))
		return nil, protocolResponseError("signaling stream")
	}
	return value, nil
}

func (stream *signalingStream) Close() error { return stream.stream.Close() }
