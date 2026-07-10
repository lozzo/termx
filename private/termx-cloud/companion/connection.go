package companion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/lozzow/termx/private/termx-cloud/companion/session"
	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
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
	offers       map[string]struct{}
}

// Hello 协商 protocol v1、caller role 和 capability 交集。
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
	response, err := connection.service.controlPlane.ResolveEndpoint(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateResolvedEndpoint(request, response); err != nil {
		return nil, err
	}
	return cloneMessage(response), nil
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
	if err := validateAdmission(admission, "", connection.service.now()); err != nil {
		return nil, err
	}
	source, err := connection.service.hub.OpenPresence(ctx, authorization, admission, request)
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if source == nil {
		return nil, protocolError("Hub returned an empty presence source")
	}
	stream := newPresenceStream(ctx, source, admission.ManagedSessionID, request.GetProof().GetDeviceId(), connection.service.streamCapacity, connection.registerStream, connection.trackOffer, connection.endPresence)
	presenceEstablished = true
	return stream, nil
}

// CreateSignalingSession 获取 client-specific admission 并转发不含 capability 的 WebRTC offer。
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
	admission, err := connection.service.controlPlane.AcquireClientAdmission(ctx, authorization, request)
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	defer admission.Destroy()
	if err := validateAdmission(admission, request.GetManagedSessionId(), connection.service.now()); err != nil {
		return nil, err
	}
	source, err := connection.service.hub.CreateSignalingSession(ctx, authorization, admission, request)
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
	if !connection.ownsOffer(request.GetSignalingSessionId()) {
		return nil, protocolError("signaling offer does not belong to this daemon connection")
	}
	response, err := connection.service.hub.CompleteSignalingOffer(ctx, authorization, cloneMessage(request))
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
	response, err := connection.service.controlPlane.AcquireRelayLease(ctx, authorization, cloneMessage(request))
	if err != nil {
		return nil, sanitizeAdapterError(err)
	}
	if err := validateRelayLeaseResponse(request, response, connection.service.now()); err != nil {
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
	accountRoles = []cloudpb.CallerRole{cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP}
	managedRoles = []cloudpb.CallerRole{cloudpb.CallerRole_CALLER_ROLE_TUI, cloudpb.CallerRole_CALLER_ROLE_MOBILE_APP, cloudpb.CallerRole_CALLER_ROLE_DAEMON}
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

func (connection *Connection) trackOffer(signalingSessionID string) {
	connection.mu.Lock()
	if connection.presenceOpen && !connection.closed {
		connection.offers[signalingSessionID] = struct{}{}
	}
	connection.mu.Unlock()
}

func (connection *Connection) ownsOffer(signalingSessionID string) bool {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	_, ok := connection.offers[signalingSessionID]
	return ok
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
