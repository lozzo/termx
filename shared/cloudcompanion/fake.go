package cloudcompanion

import (
	"context"
	"io"
	"sync"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// FakeClient 是 public client、daemon 与 UI harness 共用的可控 Companion 实现。
// 每个 handler 代表一次明确的 IPC operation；fake 记录克隆后的请求，测试无需启动 Hub、Web Controller 或闭源进程。
type FakeClient struct {
	HelloFunc                    func(context.Context, *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error)
	StatusFunc                   func(context.Context, *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error)
	BeginLoginFunc               func(context.Context, *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error)
	CompleteLoginFunc            func(context.Context, *cloudpb.CompleteLoginRequest) (*cloudpb.CompleteLoginResponse, error)
	BeginDeviceEnrollmentFunc    func(context.Context, *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error)
	CompleteDeviceEnrollmentFunc func(context.Context, *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.CompleteDeviceEnrollmentResponse, error)
	LogoutFunc                   func(context.Context, *cloudpb.LogoutRequest) (*cloudpb.LogoutResponse, error)
	DoctorFunc                   func(context.Context, *cloudpb.DoctorRequest) (*cloudpb.DoctorResponse, error)
	ShutdownFunc                 func(context.Context, *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error)
	ResolveEndpointFunc          func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error)
	ListManagedDevicesFunc       func(context.Context, *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error)
	BeginPresenceFunc            func(context.Context, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error)
	OpenPresenceFunc             func(context.Context, *cloudpb.OpenPresenceRequest) (PresenceStream, error)
	CreateSignalingSessionFunc   func(context.Context, *cloudpb.CreateSignalingSessionRequest) (SignalingStream, error)
	CompleteSignalingOfferFunc   func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error)
	AcquireRelayLeaseFunc        func(context.Context, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
	PlanManagedRouteFunc         func(context.Context, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error)
	ReportPathQualityFunc        func(context.Context, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error)
	ReportConnectionOutcomeFunc  func(context.Context, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error)

	mu       sync.Mutex
	requests RecordedRequests
}

// RecordedRequests 是 FakeClient 已接收 operation 的不可变快照。
// 所有 protobuf 值都在写入和读取时克隆，测试修改快照不会改变 fake 内部真值。
type RecordedRequests struct {
	Hello                    []*cloudpb.CompanionHelloRequest
	Status                   []*cloudpb.StatusRequest
	BeginLogin               []*cloudpb.BeginLoginRequest
	CompleteLogin            []*cloudpb.CompleteLoginRequest
	BeginDeviceEnrollment    []*cloudpb.BeginDeviceEnrollmentRequest
	CompleteDeviceEnrollment []*cloudpb.CompleteDeviceEnrollmentRequest
	Logout                   []*cloudpb.LogoutRequest
	Doctor                   []*cloudpb.DoctorRequest
	Shutdown                 []*cloudpb.ShutdownRequest
	ResolveEndpoint          []*cloudpb.ResolveEndpointRequest
	ListManagedDevices       []*cloudpb.ListManagedDevicesRequest
	BeginPresence            []*cloudpb.BeginPresenceRequest
	OpenPresence             []*cloudpb.OpenPresenceRequest
	CreateSignalingSession   []*cloudpb.CreateSignalingSessionRequest
	CompleteSignalingOffer   []*cloudpb.CompleteSignalingOfferRequest
	AcquireRelayLease        []*cloudpb.AcquireRelayLeaseRequest
	PlanManagedRoute         []*cloudpb.PlanManagedRouteRequest
	ReportPathQuality        []*cloudpb.ReportPathQualityRequest
	ReportConnectionOutcome  []*cloudpb.ReportConnectionOutcomeRequest
}

// Requests 返回 fake 当前记录的请求快照。
// 该方法只用于测试观测，不提供运行时状态恢复或 replay 语义。
func (fake *FakeClient) Requests() RecordedRequests {
	if fake == nil {
		return RecordedRequests{}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return cloneRecordedRequests(fake.requests)
}

// Hello 记录并转发协议协商请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) Hello(ctx context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
	fake.record(func(requests *RecordedRequests) { requests.Hello = append(requests.Hello, cloneMessage(request)) })
	if fake == nil || fake.HelloFunc == nil {
		return nil, missingFakeHandler("Hello")
	}
	return fake.HelloFunc(ctx, request)
}

// Status 记录并转发 companion 状态请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) Status(ctx context.Context, request *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error) {
	fake.record(func(requests *RecordedRequests) { requests.Status = append(requests.Status, cloneMessage(request)) })
	if fake == nil || fake.StatusFunc == nil {
		return nil, missingFakeHandler("Status")
	}
	return fake.StatusFunc(ctx, request)
}

// BeginLogin 记录并转发账号登录启动请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) BeginLogin(ctx context.Context, request *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.BeginLogin = append(requests.BeginLogin, cloneMessage(request))
	})
	if fake == nil || fake.BeginLoginFunc == nil {
		return nil, missingFakeHandler("BeginLogin")
	}
	return fake.BeginLoginFunc(ctx, request)
}

// CompleteLogin 记录并转发账号登录完成请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) CompleteLogin(ctx context.Context, request *cloudpb.CompleteLoginRequest) (*cloudpb.CompleteLoginResponse, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.CompleteLogin = append(requests.CompleteLogin, cloneMessage(request))
	})
	if fake == nil || fake.CompleteLoginFunc == nil {
		return nil, missingFakeHandler("CompleteLogin")
	}
	return fake.CompleteLoginFunc(ctx, request)
}

// BeginDeviceEnrollment 记录并转发 daemon enrollment 启动请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) BeginDeviceEnrollment(ctx context.Context, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.BeginDeviceEnrollment = append(requests.BeginDeviceEnrollment, cloneMessage(request))
	})
	if fake == nil || fake.BeginDeviceEnrollmentFunc == nil {
		return nil, missingFakeHandler("BeginDeviceEnrollment")
	}
	return fake.BeginDeviceEnrollmentFunc(ctx, request)
}

// CompleteDeviceEnrollment 记录并转发 daemon enrollment proof；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) CompleteDeviceEnrollment(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.CompleteDeviceEnrollmentResponse, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.CompleteDeviceEnrollment = append(requests.CompleteDeviceEnrollment, cloneMessage(request))
	})
	if fake == nil || fake.CompleteDeviceEnrollmentFunc == nil {
		return nil, missingFakeHandler("CompleteDeviceEnrollment")
	}
	return fake.CompleteDeviceEnrollmentFunc(ctx, request)
}

// Logout 记录并转发云会话删除请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) Logout(ctx context.Context, request *cloudpb.LogoutRequest) (*cloudpb.LogoutResponse, error) {
	fake.record(func(requests *RecordedRequests) { requests.Logout = append(requests.Logout, cloneMessage(request)) })
	if fake == nil || fake.LogoutFunc == nil {
		return nil, missingFakeHandler("Logout")
	}
	return fake.LogoutFunc(ctx, request)
}

// Doctor 记录并转发脱敏诊断请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) Doctor(ctx context.Context, request *cloudpb.DoctorRequest) (*cloudpb.DoctorResponse, error) {
	fake.record(func(requests *RecordedRequests) { requests.Doctor = append(requests.Doctor, cloneMessage(request)) })
	if fake == nil || fake.DoctorFunc == nil {
		return nil, missingFakeHandler("Doctor")
	}
	return fake.DoctorFunc(ctx, request)
}

// Shutdown 记录并转发本地 Companion 退出请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) Shutdown(ctx context.Context, request *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error) {
	fake.record(func(requests *RecordedRequests) { requests.Shutdown = append(requests.Shutdown, cloneMessage(request)) })
	if fake == nil || fake.ShutdownFunc == nil {
		return nil, missingFakeHandler("Shutdown")
	}
	return fake.ShutdownFunc(ctx, request)
}

// ResolveEndpoint 记录并转发 managed endpoint 定位请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) ResolveEndpoint(ctx context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.ResolveEndpoint = append(requests.ResolveEndpoint, cloneMessage(request))
	})
	if fake == nil || fake.ResolveEndpointFunc == nil {
		return nil, missingFakeHandler("ResolveEndpoint")
	}
	return fake.ResolveEndpointFunc(ctx, request)
}

// ListManagedDevices 记录并转发同账号设备目录请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) ListManagedDevices(ctx context.Context, request *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.ListManagedDevices = append(requests.ListManagedDevices, cloneMessage(request))
	})
	if fake == nil || fake.ListManagedDevicesFunc == nil {
		return nil, missingFakeHandler("ListManagedDevices")
	}
	return fake.ListManagedDevicesFunc(ctx, request)
}

// BeginPresence 记录并转发 daemon fresh presence challenge 请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) BeginPresence(ctx context.Context, request *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.BeginPresence = append(requests.BeginPresence, cloneMessage(request))
	})
	if fake == nil || fake.BeginPresenceFunc == nil {
		return nil, missingFakeHandler("BeginPresence")
	}
	return fake.BeginPresenceFunc(ctx, request)
}

// OpenPresence 记录并转发 daemon presence 请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) OpenPresence(ctx context.Context, request *cloudpb.OpenPresenceRequest) (PresenceStream, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.OpenPresence = append(requests.OpenPresence, cloneMessage(request))
	})
	if fake == nil || fake.OpenPresenceFunc == nil {
		return nil, missingFakeHandler("OpenPresence")
	}
	return fake.OpenPresenceFunc(ctx, request)
}

// CreateSignalingSession 记录并转发 client offer；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) CreateSignalingSession(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (SignalingStream, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.CreateSignalingSession = append(requests.CreateSignalingSession, cloneMessage(request))
	})
	if fake == nil || fake.CreateSignalingSessionFunc == nil {
		return nil, missingFakeHandler("CreateSignalingSession")
	}
	return fake.CreateSignalingSessionFunc(ctx, request)
}

// CompleteSignalingOffer 记录并转发 daemon 对单个 offer 的 answer 或稳定错误；缺少 handler 时返回 PROTOCOL 错误。
func (fake *FakeClient) CompleteSignalingOffer(ctx context.Context, request *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.CompleteSignalingOffer = append(requests.CompleteSignalingOffer, cloneMessage(request))
	})
	if fake == nil || fake.CompleteSignalingOfferFunc == nil {
		return nil, missingFakeHandler("CompleteSignalingOffer")
	}
	return fake.CompleteSignalingOfferFunc(ctx, request)
}

// AcquireRelayLease 记录并转发 Relay 服务准入请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) AcquireRelayLease(ctx context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.AcquireRelayLease = append(requests.AcquireRelayLease, cloneMessage(request))
	})
	if fake == nil || fake.AcquireRelayLeaseFunc == nil {
		return nil, missingFakeHandler("AcquireRelayLease")
	}
	return fake.AcquireRelayLeaseFunc(ctx, request)
}

// PlanManagedRoute 记录并转发 SmartRoute 计划请求；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) PlanManagedRoute(ctx context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.PlanManagedRoute = append(requests.PlanManagedRoute, cloneMessage(request))
	})
	if fake == nil || fake.PlanManagedRouteFunc == nil {
		return nil, missingFakeHandler("PlanManagedRoute")
	}
	return fake.PlanManagedRouteFunc(ctx, request)
}

// ReportPathQuality 记录并转发脱敏网络质量摘要；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) ReportPathQuality(ctx context.Context, request *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.ReportPathQuality = append(requests.ReportPathQuality, cloneMessage(request))
	})
	if fake == nil || fake.ReportPathQualityFunc == nil {
		return nil, missingFakeHandler("ReportPathQuality")
	}
	return fake.ReportPathQualityFunc(ctx, request)
}

// ReportConnectionOutcome 记录并转发 managed connection 结果；缺少 handler 时返回稳定 PROTOCOL 错误。
func (fake *FakeClient) ReportConnectionOutcome(ctx context.Context, request *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	fake.record(func(requests *RecordedRequests) {
		requests.ReportConnectionOutcome = append(requests.ReportConnectionOutcome, cloneMessage(request))
	})
	if fake == nil || fake.ReportConnectionOutcomeFunc == nil {
		return nil, missingFakeHandler("ReportConnectionOutcome")
	}
	return fake.ReportConnectionOutcomeFunc(ctx, request)
}

func (fake *FakeClient) record(appendRequest func(*RecordedRequests)) {
	if fake == nil {
		return
	}
	fake.mu.Lock()
	appendRequest(&fake.requests)
	fake.mu.Unlock()
}

func missingFakeHandler(operation string) error {
	return NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion fake has no "+operation+" handler")
}

type streamItem[T proto.Message] struct {
	message T
	err     error
}

type fakeStream[T proto.Message] struct {
	items chan streamItem[T]
	done  chan struct{}
	once  sync.Once
}

func newFakeStream[T proto.Message](capacity int) *fakeStream[T] {
	if capacity < 0 {
		capacity = 0
	}
	return &fakeStream[T]{items: make(chan streamItem[T], capacity), done: make(chan struct{})}
}

func (stream *fakeStream[T]) push(item streamItem[T]) error {
	select {
	case <-stream.done:
		return io.ErrClosedPipe
	default:
	}
	select {
	case stream.items <- item:
		return nil
	case <-stream.done:
		return io.ErrClosedPipe
	default:
		return NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE, "cloud companion fake stream is full")
	}
}

func (stream *fakeStream[T]) receive() (T, error) {
	select {
	case <-stream.done:
		var zero T
		return zero, io.EOF
	case item := <-stream.items:
		return cloneMessage(item.message), item.err
	}
}

func (stream *fakeStream[T]) close() error {
	stream.once.Do(func() { close(stream.done) })
	return nil
}

// FakePresenceStream 是 daemon presence harness 的有界可控事件流。
// Push/Fail 在缓冲区满时返回 BACKPRESSURE，Close 会解除阻塞的 Receive，避免测试依赖 goroutine 泄漏或隐式重连。
type FakePresenceStream struct {
	stream *fakeStream[*cloudpb.PresenceEvent]
}

// NewFakePresenceStream 创建指定容量的 presence fake stream。
// capacity 为零时 Push 仅在已有 Receive 等待时成功，适合验证严格背压。
func NewFakePresenceStream(capacity int) *FakePresenceStream {
	return &FakePresenceStream{stream: newFakeStream[*cloudpb.PresenceEvent](capacity)}
}

// Push 向 presence stream 写入一条克隆后的事件。
// nil 事件允许用于协议违规 harness，生产调用方必须将其视为 PROTOCOL 错误。
func (stream *FakePresenceStream) Push(event *cloudpb.PresenceEvent) error {
	return stream.stream.push(streamItem[*cloudpb.PresenceEvent]{message: cloneMessage(event)})
}

// Fail 向 presence stream 写入一次接收错误。
// 错误只属于当前 stream；fake 不自动关闭或重开 endpoint。
func (stream *FakePresenceStream) Fail(err error) error {
	return stream.stream.push(streamItem[*cloudpb.PresenceEvent]{err: err})
}

// Receive 返回下一条 presence 事件或流错误。
// Close 后返回 io.EOF；该方法不推断 daemon lifecycle 或 terminal inventory。
func (stream *FakePresenceStream) Receive() (*cloudpb.PresenceEvent, error) {
	return stream.stream.receive()
}

// Close 幂等关闭 presence stream 并解除阻塞接收。
func (stream *FakePresenceStream) Close() error {
	return stream.stream.close()
}

// FakeSignalingStream 是 client signaling harness 的有界可控事件流。
// 它只接收 cloudpb.SignalingEvent，不能绕过 protobuf contract 注入 grant 或 terminal payload。
type FakeSignalingStream struct {
	stream *fakeStream[*cloudpb.SignalingEvent]
}

// NewFakeSignalingStream 创建指定容量的 signaling fake stream。
// capacity 为零时可用于验证调用方在 offer 后及时开始消费 answer。
func NewFakeSignalingStream(capacity int) *FakeSignalingStream {
	return &FakeSignalingStream{stream: newFakeStream[*cloudpb.SignalingEvent](capacity)}
}

// Push 向 signaling stream 写入一条克隆后的事件。
// nil 事件允许用于协议违规 harness，生产调用方必须将其视为 PROTOCOL 错误。
func (stream *FakeSignalingStream) Push(event *cloudpb.SignalingEvent) error {
	return stream.stream.push(streamItem[*cloudpb.SignalingEvent]{message: cloneMessage(event)})
}

// Fail 向 signaling stream 写入一次接收错误。
// 错误只属于当前 signaling session，不改变其他 endpoint 或 transport。
func (stream *FakeSignalingStream) Fail(err error) error {
	return stream.stream.push(streamItem[*cloudpb.SignalingEvent]{err: err})
}

// Receive 返回下一条 signaling 事件或流错误。
// Close 后返回 io.EOF；它不执行 WebRTC、DTLS 或 capability 验证。
func (stream *FakeSignalingStream) Receive() (*cloudpb.SignalingEvent, error) {
	return stream.stream.receive()
}

// Close 幂等关闭 signaling stream 并解除阻塞接收。
func (stream *FakeSignalingStream) Close() error {
	return stream.stream.close()
}

func cloneRecordedRequests(source RecordedRequests) RecordedRequests {
	return RecordedRequests{
		Hello:                    cloneMessages(source.Hello),
		Status:                   cloneMessages(source.Status),
		BeginLogin:               cloneMessages(source.BeginLogin),
		CompleteLogin:            cloneMessages(source.CompleteLogin),
		BeginDeviceEnrollment:    cloneMessages(source.BeginDeviceEnrollment),
		CompleteDeviceEnrollment: cloneMessages(source.CompleteDeviceEnrollment),
		Logout:                   cloneMessages(source.Logout),
		Doctor:                   cloneMessages(source.Doctor),
		Shutdown:                 cloneMessages(source.Shutdown),
		ResolveEndpoint:          cloneMessages(source.ResolveEndpoint),
		ListManagedDevices:       cloneMessages(source.ListManagedDevices),
		BeginPresence:            cloneMessages(source.BeginPresence),
		OpenPresence:             cloneMessages(source.OpenPresence),
		CreateSignalingSession:   cloneMessages(source.CreateSignalingSession),
		CompleteSignalingOffer:   cloneMessages(source.CompleteSignalingOffer),
		AcquireRelayLease:        cloneMessages(source.AcquireRelayLease),
		PlanManagedRoute:         cloneMessages(source.PlanManagedRoute),
		ReportPathQuality:        cloneMessages(source.ReportPathQuality),
		ReportConnectionOutcome:  cloneMessages(source.ReportConnectionOutcome),
	}
}

func cloneMessages[T proto.Message](messages []T) []T {
	cloned := make([]T, len(messages))
	for index, message := range messages {
		cloned[index] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage[T proto.Message](message T) T {
	if any(message) == nil {
		var zero T
		return zero
	}
	return proto.Clone(message).(T)
}
