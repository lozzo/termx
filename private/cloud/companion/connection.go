package companion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

var _ cloudcompanion.Client = (*Connection)(nil)

// Connection 是单个本地 IPC peer 的 Cloud Companion contract 状态机。
// caller role、协商能力和 stream registry 都属于该连接，不能跨 TUI、CLI、daemon 或 endpoint 共享。
type Connection struct {
	service *Service

	mu           sync.Mutex
	helloDone    bool
	closed       bool
	role         cloudpb.CallerRole
	capabilities map[cloudpb.CompanionCapability]struct{}
	nextStreamID uint64
	streams      map[uint64]ownedStream
	presenceOpen bool
	offers       map[string]string
}

// Hello 协商当前 versioned IPC protocol、caller role 和 capability 交集。
// 它必须是连接首个操作且只能成功一次；无共同版本、弱 nonce 或非法 role 均 fail closed。
func (connection *Connection) Hello(ctx context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
	if err := ensureContext(ctx); err != nil {
		return nil, temporaryError("companion hello was canceled")
	}
	if request == nil || request.GetProtocolMin() == 0 || request.GetProtocolMax() < request.GetProtocolMin() || request.GetTermxVersion() == "" || len(request.GetRequestNonce()) < 16 || len(request.GetRequestNonce()) > 64 {
		return nil, protocolError("invalid companion hello request")
	}
	if request.GetProtocolMin() > cloudcompanion.ProtocolVersionMax || request.GetProtocolMax() < cloudcompanion.ProtocolVersionMin {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "no compatible companion protocol version")
	}
	if !validCallerRole(request.GetCallerRole()) {
		return nil, protocolError("invalid companion caller role")
	}
	negotiated := make(map[cloudpb.CompanionCapability]struct{})
	ordered := make([]cloudpb.CompanionCapability, 0, len(request.GetRequestedCapabilities()))
	seen := make(map[cloudpb.CompanionCapability]struct{})
	for _, capability := range request.GetRequestedCapabilities() {
		if !knownCapability(capability) {
			return nil, protocolError("unknown companion capability")
		}
		if _, duplicate := seen[capability]; duplicate {
			return nil, protocolError("duplicate companion capability")
		}
		seen[capability] = struct{}{}
		if _, supported := connection.service.capabilities[capability]; supported && capabilityAllowedForRole(capability, request.GetCallerRole()) {
			negotiated[capability] = struct{}{}
			ordered = append(ordered, capability)
		}
	}
	responseNonce := make([]byte, 32)
	if _, err := io.ReadFull(connection.service.nonceReader, responseNonce); err != nil {
		return nil, temporaryError("companion nonce generation failed")
	}

	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closed {
		return nil, temporaryError("companion connection is closed")
	}
	if connection.helloDone {
		return nil, protocolError("companion hello already completed")
	}
	connection.helloDone = true
	connection.role = request.GetCallerRole()
	connection.capabilities = negotiated
	return &cloudpb.CompanionHelloResponse{
		SelectedProtocol:      cloudcompanion.ProtocolVersionMax,
		CompanionVersion:      connection.service.version,
		SupportedCapabilities: ordered,
		BuildChannel:          connection.service.buildChannel,
		ResponseNonce:         responseNonce,
	}, nil
}

// Status 返回当前 caller role 可见的脱敏账号或 device session 状态。
// token、Hub ticket、Relay credential 和 adapter 原始错误永不进入 response。
func (connection *Connection) Status(ctx context.Context, _ *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error) {
	if err := ensureContext(ctx); err != nil {
		return nil, temporaryError("companion status request was canceled")
	}
	role, capabilities, err := connection.snapshotHello()
	if err != nil {
		return nil, err
	}
	expectedKind := sessionKindForRole(role)
	stored, loadErr := connection.service.sessions.Load(ctx, expectedKind, connection.service.now())
	if loadErr != nil {
		state := requiredSessionState(role)
		if !errors.Is(loadErr, session.ErrNotFound) && !errors.Is(loadErr, session.ErrExpired) {
			state = cloudpb.CompanionState_COMPANION_STATE_UNAVAILABLE
		}
		return &cloudpb.StatusResponse{State: state, Capabilities: capabilities}, nil
	}
	defer stored.Destroy()
	metadata := stored.Metadata()
	if metadata.Kind != expectedKind {
		return &cloudpb.StatusResponse{State: requiredSessionState(role), Capabilities: capabilities}, nil
	}
	return &cloudpb.StatusResponse{
		State:                cloudpb.CompanionState_COMPANION_STATE_READY,
		AccountLabel:         metadata.AccountLabel,
		AccountId:            metadata.AccountID,
		DeviceId:             metadata.DeviceID,
		SessionExpiresAtUnix: uint64(metadata.ExpiresAt.Unix()),
		Capabilities:         capabilities,
	}, nil
}

// BeginLogin 启动 account browser/device-code flow。
// 返回值只包含短期 flow reference 和用户交互 metadata，账号 token 始终留在 private Control Plane adapter。
func (connection *Connection) BeginLogin(ctx context.Context, request *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
	if err := connection.requireLifecycleCapability(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP); err != nil {
		return nil, err
	}
	if err := validateBeginLoginRequest(request); err != nil {
		return nil, err
	}
	flow, err := connection.service.controlPlane.BeginLogin(ctx, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateLoginFlow(flow, connection.service.now(), connection.service.allowPublicHTTPLoginURL); err != nil {
		return nil, err
	}
	return cloneMessage(flow), nil
}

// CompleteLogin 兑换短期 flow，并把 account session 写入 OS credential store。
// public response 只返回 metadata；adapter 返回的 token bytes 在 Save 后立即销毁。
func (connection *Connection) CompleteLogin(ctx context.Context, request *cloudpb.CompleteLoginRequest) (*cloudpb.CompleteLoginResponse, error) {
	if err := connection.requireLifecycleCapability(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP); err != nil {
		return nil, err
	}
	if request == nil || request.GetFlowId() == "" {
		return nil, protocolError("invalid login completion request")
	}
	stored, err := connection.service.controlPlane.CompleteLogin(ctx, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	defer stored.Destroy()
	if stored.Metadata().Kind != session.KindAccount {
		return nil, protocolError("Control Plane returned a non-account login session")
	}
	if err := connection.service.sessions.Save(ctx, stored, connection.service.now()); err != nil {
		return nil, temporaryError("OS credential store rejected the account session")
	}
	return &cloudpb.CompleteLoginResponse{Session: sessionSummary(stored.Metadata())}, nil
}

// BeginDeviceEnrollment 用一次性 code、public key 与设备 metadata 获取短期 challenge。
// DeviceIdentity private key 不进入 Companion；challenge 必须回到公开 daemon/CLI 签名。
func (connection *Connection) BeginDeviceEnrollment(ctx context.Context, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	if err := connection.requireLifecycleCapability(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CallerRole_CALLER_ROLE_DAEMON); err != nil {
		return nil, err
	}
	if err := validateBeginEnrollmentRequest(request); err != nil {
		return nil, err
	}
	challenge, err := connection.service.controlPlane.BeginDeviceEnrollment(ctx, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateEnrollmentChallenge(challenge, connection.service.now()); err != nil {
		return nil, err
	}
	return cloneMessage(challenge), nil
}

// CompleteDeviceEnrollment 转发公开 daemon 生成的 DeviceProof，并保存 private device 云会话。
// 成功不会生成、读取或撤销 CapabilityGrant，也不会扩大 terminal scope。
func (connection *Connection) CompleteDeviceEnrollment(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.CompleteDeviceEnrollmentResponse, error) {
	if err := connection.requireLifecycleCapability(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CallerRole_CALLER_ROLE_DAEMON); err != nil {
		return nil, err
	}
	if err := validateCompleteEnrollmentRequest(request); err != nil {
		return nil, err
	}
	stored, err := connection.service.controlPlane.CompleteDeviceEnrollment(ctx, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	defer stored.Destroy()
	if stored.Metadata().Kind != session.KindDevice {
		return nil, protocolError("Control Plane returned a non-device enrollment session")
	}
	if err := connection.service.sessions.Save(ctx, stored, connection.service.now()); err != nil {
		return nil, temporaryError("OS credential store rejected the device session")
	}
	return &cloudpb.CompleteDeviceEnrollmentResponse{Session: sessionSummary(stored.Metadata())}, nil
}

// Logout 删除请求中明确选择的 account/device 云会话。
// 该操作不读取或删除公开 DeviceIdentity、CapabilityGrant store、connections.yaml 或 SSH 配置。
func (connection *Connection) Logout(ctx context.Context, request *cloudpb.LogoutRequest) (*cloudpb.LogoutResponse, error) {
	if request == nil || !request.GetAccountSession() && !request.GetDeviceSession() {
		return nil, protocolError("logout must select at least one cloud session")
	}
	role, _, err := connection.snapshotHello()
	if err != nil {
		return nil, err
	}
	if role != cloudpb.CallerRole_CALLER_ROLE_CLI && role != cloudpb.CallerRole_CALLER_ROLE_TUI && role != cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP && role != cloudpb.CallerRole_CALLER_ROLE_DAEMON {
		return nil, protocolError("logout is not allowed for caller role")
	}
	if request.GetAccountSession() {
		if err := connection.requireNegotiatedCapability(cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION); err != nil {
			return nil, err
		}
		if err := connection.service.sessions.Delete(ctx, session.KindAccount); err != nil {
			return nil, temporaryError("OS credential store could not delete the account session")
		}
	}
	if request.GetDeviceSession() {
		if err := connection.requireNegotiatedCapability(cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT); err != nil {
			return nil, err
		}
		if err := connection.service.sessions.Delete(ctx, session.KindDevice); err != nil {
			return nil, temporaryError("OS credential store could not delete the device session")
		}
	}
	return &cloudpb.LogoutResponse{}, nil
}

// Doctor 返回当前 caller 可见状态和固定 code 的脱敏诊断。
// diagnostic 不含 token body、SDP credential、TURN password、grant 或 terminal identity。
func (connection *Connection) Doctor(ctx context.Context, _ *cloudpb.DoctorRequest) (*cloudpb.DoctorResponse, error) {
	status, err := connection.Status(ctx, &cloudpb.StatusRequest{})
	if err != nil {
		return nil, err
	}
	severity := cloudpb.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_INFO
	message := "Cloud Companion protocol and local credential store are ready"
	if status.GetState() != cloudpb.CompanionState_COMPANION_STATE_READY {
		severity = cloudpb.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_WARNING
		message = "Cloud Companion requires account login or device enrollment"
	}
	return &cloudpb.DoctorResponse{Status: status, Items: []*cloudpb.DiagnosticItem{{Code: "companion_state", Severity: severity, Message: message, Reference: connection.service.version}}}, nil
}

// Shutdown 验证 CLI caller 后确认本地进程退出请求。
// 真正关闭 listener 由 public IPC Server 在 ack 写入成功后执行，避免先停进程再丢响应。
func (connection *Connection) Shutdown(ctx context.Context, request *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error) {
	if err := ensureContext(ctx); err != nil {
		return nil, temporaryError("companion shutdown request was canceled")
	}
	role, _, err := connection.snapshotHello()
	if err != nil {
		return nil, err
	}
	if role != cloudpb.CallerRole_CALLER_ROLE_CLI || request == nil || request.GetReason() == "" {
		return nil, protocolError("invalid companion shutdown request")
	}
	return &cloudpb.ShutdownResponse{}, nil
}

// ResolveEndpoint 通过 Control Plane 创建或定位 managed session。
// 返回的 target、endpoint、Hub 和 ICE metadata 会在进入 public WebRTC 前严格校验。
func (connection *Connection) ResolveEndpoint(ctx context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING, accountRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateResolveRequest(request); err != nil {
		return nil, err
	}
	response, err := connection.service.hub.ResolveEndpoint(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateResolvedEndpoint(request, response); err != nil {
		return nil, err
	}
	return cloneMessage(response), nil
}

// ListManagedDevices 从 Hub 本地授权投影读取同账号设备；该请求不访问 Control Plane 数据库。
// 返回目录不授予 terminal capability，公开客户端仍须持有 daemon 签发的 CapabilityGrant。
func (connection *Connection) ListManagedDevices(ctx context.Context, request *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_DIRECTORY, directoryRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if request == nil || request.GetSchemaVersion() != 1 {
		return nil, protocolError("invalid managed device directory request")
	}
	response, err := connection.service.hub.ListManagedDevices(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateManagedDevices(response); err != nil {
		return nil, err
	}
	return cloneMessage(response), nil
}

// BeginPresence 使用 daemon device cloud session 获取一次性 presence challenge。
// challenge 只回到公开 daemon 签名；Companion 不持有 DeviceIdentity private key，也不能复用 enrollment challenge。
func (connection *Connection) BeginPresence(ctx context.Context, request *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE, cloudpb.CallerRole_CALLER_ROLE_DAEMON)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateBeginPresenceRequest(request); err != nil {
		return nil, err
	}
	challenge, err := connection.service.controlPlane.BeginPresence(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validatePresenceChallenge(challenge, connection.service.now()); err != nil {
		return nil, err
	}
	return cloneMessage(challenge), nil
}

// OpenPresence 为 daemon public proof 获取内部 admission 并打开当前连接拥有的有界 stream。
// proof 只含 public key 和签名；companion 不接收或推导 DeviceIdentity 私钥。
func (connection *Connection) OpenPresence(ctx context.Context, request *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_PRESENCE, cloudpb.CallerRole_CALLER_ROLE_DAEMON)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validatePresenceRequest(request); err != nil {
		return nil, err
	}
	if err := connection.beginPresence(); err != nil {
		return nil, err
	}
	presenceEstablished := false
	defer func() {
		if !presenceEstablished {
			connection.endPresence()
		}
	}()
	request = cloneMessage(request)
	admission, err := connection.service.controlPlane.AcquirePresenceAdmission(ctx, authorization, request)
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	defer admission.Destroy()
	if err := validateAdmission(admission, cloudservice.HubSessionPresence, request.GetPresenceSessionId(), connection.service.now()); err != nil {
		return nil, err
	}
	source, err := connection.service.hub.OpenPresence(ctx, authorization, admission, request)
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if source == nil {
		return nil, protocolError("Hub returned an empty presence source")
	}
	stream := newPresenceStream(ctx, source, admission.SessionID, request.GetProof().GetDeviceId(), connection.service.streamCapacity, connection.registerStream, connection.trackOffer, connection.endPresence)
	presenceEstablished = true
	return stream, nil
}

// CreateSignalingSession 使用启动阶段账号 edge credential 直接向 Hub 转发 WebRTC offer。
// 返回 stream 只属于当前连接和 managed session，关闭其他连接不会影响它。
func (connection *Connection) CreateSignalingSession(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING, accountRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateSignalingRequest(request); err != nil {
		return nil, err
	}
	if err := connection.requireRouteCapability(request.GetRoutePreference()); err != nil {
		return nil, err
	}
	request = cloneMessage(request)
	source, err := connection.service.hub.CreateSignalingSession(ctx, authorization, request)
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if source == nil {
		return nil, protocolError("Hub returned an empty signaling source")
	}
	stream := newSignalingStream(ctx, source, connection.service.streamCapacity, connection.registerStream)
	return stream, nil
}

// CompleteSignalingOffer 转发 daemon 对当前 presence offer 的 answer 或稳定失败。
// 它不关闭其他 signaling session，也不接受 grant、terminal 或 DataChannel payload。
func (connection *Connection) CompleteSignalingOffer(ctx context.Context, request *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING, cloudpb.CallerRole_CALLER_ROLE_DAEMON)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateCompleteOfferRequest(request); err != nil {
		return nil, err
	}
	_, ownsOffer := connection.ownedOffer(request.GetSignalingSessionId())
	if !ownsOffer {
		return nil, protocolError("signaling offer does not belong to this daemon connection")
	}
	request = cloneMessage(request)
	if failure := request.GetError(); failure != nil {
		// daemon 原始错误文本可能含平台或凭据信息；云链路只保留稳定 code/retryable。
		failure.Message = ""
		failure.RetryAfterMillis = 0
		failure.CorrelationId = ""
	}
	response, err := connection.service.hub.CompleteSignalingOffer(ctx, authorization, request)
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if response == nil {
		return nil, protocolError("Hub returned an empty offer acknowledgement")
	}
	connection.completeOffer(request.GetSignalingSessionId())
	return cloneMessage(response), nil
}

// AcquireRelayLease 获取 entitlement-bound 短期 Relay lease 和 route metadata。
// signed lease 只用于公开 WebRTC ICE/TURN 建连，不能替代 daemon capability。
func (connection *Connection) AcquireRelayLease(ctx context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE, managedRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateRelayLeaseRequest(request); err != nil {
		return nil, err
	}
	if err := connection.requireRouteCapability(request.GetRoutePreference()); err != nil {
		return nil, err
	}
	response, err := connection.service.hub.AcquireRelayLease(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateRelayLeaseResponse(request, response, connection.service.now()); err != nil {
		return nil, err
	}
	return cloneMessage(response), nil
}

// PlanManagedRoute 获取受 SmartRoute capability 保护的短期 direct/single-relay ICE 计划。
// Companion 只转发稳定原因和执行 material；私有 score、成本预算和候选明细不能进入 public IPC。
func (connection *Connection) PlanManagedRoute(ctx context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE, managedRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateManagedRoutePlanRequest(request); err != nil {
		return nil, err
	}
	response, err := connection.service.controlPlane.PlanManagedRoute(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateManagedRoutePlanResponse(request, response, connection.service.now()); err != nil {
		return nil, err
	}
	return cloneMessage(response), nil
}

// ReportPathQuality 转发聚合网络质量窗口。
// 校验只允许 managed session、路径和统计值，不接受 packet payload、IP 明细或 terminal identity。
func (connection *Connection) ReportPathQuality(ctx context.Context, request *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY, managedRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validatePathQualityRequest(request); err != nil {
		return nil, err
	}
	response, err := connection.service.controlPlane.ReportPathQuality(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if response == nil {
		return nil, protocolError("Control Plane returned an empty quality acknowledgement")
	}
	return cloneMessage(response), nil
}

// ReportConnectionOutcome 转发一次 managed connection 的稳定 path/error 结果。
// Adapter 原始错误和 credential 不会被拼接到 public error message。
func (connection *Connection) ReportConnectionOutcome(ctx context.Context, request *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	authorization, err := connection.authorize(ctx, cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY, managedRoles...)
	if err != nil {
		return nil, err
	}
	defer authorization.Destroy()
	if err := validateConnectionOutcomeRequest(request); err != nil {
		return nil, err
	}
	response, err := connection.service.controlPlane.ReportConnectionOutcome(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if response == nil {
		return nil, protocolError("Control Plane returned an empty outcome acknowledgement")
	}
	return cloneMessage(response), nil
}

// Close 幂等关闭当前 IPC connection 及其拥有的 streams。
// 其他 TUI、daemon 或 mobile connection 的 presence/signaling 不会被触及。
func (connection *Connection) Close() error {
	connection.mu.Lock()
	if connection.closed {
		connection.mu.Unlock()
		return nil
	}
	connection.closed = true
	streams := make([]ownedStream, 0, len(connection.streams))
	for _, stream := range connection.streams {
		streams = append(streams, stream)
	}
	connection.streams = make(map[uint64]ownedStream)
	connection.mu.Unlock()
	for _, stream := range streams {
		_ = stream.Close()
	}
	return nil
}

var (
	accountRoles   = []cloudpb.CallerRole{cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP}
	directoryRoles = []cloudpb.CallerRole{cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP}
	managedRoles   = []cloudpb.CallerRole{cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP, cloudpb.CallerRole_CALLER_ROLE_DAEMON}
)

func (connection *Connection) authorize(ctx context.Context, capability cloudpb.CompanionCapability, roles ...cloudpb.CallerRole) (session.Authorization, error) {
	if err := ensureContext(ctx); err != nil {
		return session.Authorization{}, temporaryError("managed cloud request was canceled")
	}
	role, _, err := connection.snapshotHello()
	if err != nil {
		return session.Authorization{}, err
	}
	if !containsRole(roles, role) {
		return session.Authorization{}, protocolError("operation is not allowed for caller role")
	}
	connection.mu.Lock()
	_, negotiated := connection.capabilities[capability]
	connection.mu.Unlock()
	if !negotiated {
		return session.Authorization{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "required companion capability was not negotiated")
	}
	expectedKind := sessionKindForRole(role)
	stored, err := connection.service.sessions.Load(ctx, expectedKind, connection.service.now())
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			code := cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED
			if role == cloudpb.CallerRole_CALLER_ROLE_DAEMON {
				code = cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED
			}
			return session.Authorization{}, cloudcompanion.NewError(code, "cloud session is required")
		}
		if errors.Is(err, session.ErrExpired) {
			return session.Authorization{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud session expired")
		}
		if errors.Is(err, session.ErrInvalid) {
			return session.Authorization{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud session is invalid")
		}
		return session.Authorization{}, temporaryError("OS credential store is unavailable")
	}
	if stored.Metadata().Kind != expectedKind {
		stored.Destroy()
		return session.Authorization{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud session does not match caller role")
	}
	authorization := stored.Authorization()
	stored.Destroy()
	return authorization, nil
}

func (connection *Connection) requireLifecycleCapability(ctx context.Context, capability cloudpb.CompanionCapability, roles ...cloudpb.CallerRole) error {
	if err := ensureContext(ctx); err != nil {
		return temporaryError("companion lifecycle request was canceled")
	}
	role, _, err := connection.snapshotHello()
	if err != nil {
		return err
	}
	if !containsRole(roles, role) {
		return protocolError("lifecycle operation is not allowed for caller role")
	}
	return connection.requireNegotiatedCapability(capability)
}

func (connection *Connection) requireNegotiatedCapability(capability cloudpb.CompanionCapability) error {
	connection.mu.Lock()
	_, negotiated := connection.capabilities[capability]
	connection.mu.Unlock()
	if !negotiated {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "required companion capability was not negotiated")
	}
	return nil
}

func sessionSummary(metadata session.Metadata) *cloudpb.CloudSessionSummary {
	return &cloudpb.CloudSessionSummary{
		AccountLabel:  metadata.AccountLabel,
		AccountId:     metadata.AccountID,
		DeviceId:      metadata.DeviceID,
		ExpiresAtUnix: uint64(metadata.ExpiresAt.Unix()),
	}
}

func (connection *Connection) snapshotHello() (cloudpb.CallerRole, []cloudpb.CompanionCapability, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closed {
		return cloudpb.CallerRole_CALLER_ROLE_UNSPECIFIED, nil, temporaryError("companion connection is closed")
	}
	if !connection.helloDone {
		return cloudpb.CallerRole_CALLER_ROLE_UNSPECIFIED, nil, protocolError("companion hello is required")
	}
	capabilities := make([]cloudpb.CompanionCapability, 0, len(connection.capabilities))
	for capability := range connection.capabilities {
		capabilities = append(capabilities, capability)
	}
	sortCapabilities(capabilities)
	return connection.role, capabilities, nil
}

func (connection *Connection) registerStream(stream ownedStream) func() {
	connection.mu.Lock()
	if connection.closed {
		connection.mu.Unlock()
		_ = stream.Close()
		return func() {}
	}
	connection.nextStreamID++
	streamID := connection.nextStreamID
	connection.streams[streamID] = stream
	connection.mu.Unlock()
	return func() {
		connection.mu.Lock()
		delete(connection.streams, streamID)
		connection.mu.Unlock()
	}
}

func (connection *Connection) beginPresence() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closed {
		return temporaryError("companion connection is closed")
	}
	if connection.presenceOpen {
		return protocolError("daemon connection already owns a presence stream")
	}
	connection.presenceOpen = true
	return nil
}

func (connection *Connection) endPresence() {
	connection.mu.Lock()
	connection.presenceOpen = false
	clear(connection.offers)
	connection.mu.Unlock()
}

func (connection *Connection) trackOffer(signalingSessionID, managedSessionID string) {
	connection.mu.Lock()
	if connection.presenceOpen && !connection.closed {
		connection.offers[signalingSessionID] = managedSessionID
	}
	connection.mu.Unlock()
}

func (connection *Connection) ownedOffer(signalingSessionID string) (string, bool) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	managedSessionID, ok := connection.offers[signalingSessionID]
	return managedSessionID, ok
}

func (connection *Connection) completeOffer(signalingSessionID string) {
	connection.mu.Lock()
	delete(connection.offers, signalingSessionID)
	connection.mu.Unlock()
}

func (connection *Connection) requireRouteCapability(preference cloudpb.RoutePreference) error {
	if preference != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE && preference != cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR {
		return nil
	}
	connection.mu.Lock()
	_, negotiated := connection.capabilities[cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE]
	connection.mu.Unlock()
	if !negotiated {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "SmartRoute capability was not negotiated")
	}
	return nil
}

func cloneMessage[T proto.Message](message T) T {
	if any(message) == nil {
		var zero T
		return zero
	}
	return proto.Clone(message).(T)
}

func sanitizeAdapterError(err error) error {
	if err == nil {
		return nil
	}
	var cloudErr *cloudcompanion.Error
	if errors.As(err, &cloudErr) {
		if !validCloudErrorCode(cloudErr.Code, false) {
			return protocolError("managed cloud service returned an unknown error code")
		}
		return &cloudcompanion.Error{
			Code:          cloudErr.Code,
			Message:       "managed cloud service rejected the request",
			Retryable:     cloudErr.Retryable,
			RetryAfter:    cloudErr.RetryAfter,
			CorrelationID: cloudErr.CorrelationID,
		}
	}
	return temporaryError("managed cloud service request failed")
}

func protocolError(message string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, message)
}

func temporaryError(message string) error {
	err := cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, message)
	err.Retryable = true
	return err
}

func validCallerRole(role cloudpb.CallerRole) bool {
	return role == cloudpb.CallerRole_CALLER_ROLE_TUI || role == cloudpb.CallerRole_CALLER_ROLE_CLI || role == cloudpb.CallerRole_CALLER_ROLE_DAEMON || role == cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP
}

func sessionKindForRole(role cloudpb.CallerRole) session.Kind {
	if role == cloudpb.CallerRole_CALLER_ROLE_DAEMON {
		return session.KindDevice
	}
	return session.KindAccount
}

func requiredSessionState(role cloudpb.CallerRole) cloudpb.CompanionState {
	if role == cloudpb.CallerRole_CALLER_ROLE_DAEMON {
		return cloudpb.CompanionState_COMPANION_STATE_DEVICE_ENROLLMENT_REQUIRED
	}
	return cloudpb.CompanionState_COMPANION_STATE_LOGIN_REQUIRED
}

func containsRole(roles []cloudpb.CallerRole, target cloudpb.CallerRole) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

func sortCapabilities(capabilities []cloudpb.CompanionCapability) {
	for index := 1; index < len(capabilities); index++ {
		for current := index; current > 0 && capabilities[current] < capabilities[current-1]; current-- {
			capabilities[current], capabilities[current-1] = capabilities[current-1], capabilities[current]
		}
	}
}

func ensureContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	return ctx.Err()
}
