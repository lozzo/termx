package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

// Config 固定显式 development staging Control Plane/Hub origin 与 HTTP transport。
// 默认必须是 loopback；AllowPublicHTTP 只能来自已验证的 staging-public-http manifest。
type Config struct {
	ControlPlaneURL string
	HubURL          string
	AllowPublicHTTP bool
	HTTPClient      *http.Client
	Now             func() time.Time
}

// Adapter 是 Cloud Companion 的显式 dev-local 网络 adapter。
// 它通过真实 HTTP socket 交换 cloud contract，不 import 或调用 Control Plane/Hub 进程内 Service。
type Adapter struct {
	controlURL string
	hubURL     string
	client     *http.Client
	now        func() time.Time
}

// New 创建默认只允许 loopback 的 development adapter。
// 非 http、带 userinfo/query/path或缺失 origin 均 fail closed；公网明文必须由调用方显式授权。
func New(config Config) (*Adapter, error) {
	control, err := validateServiceURL(config.ControlPlaneURL, config.AllowPublicHTTP)
	if err != nil {
		return nil, fmt.Errorf("invalid dev Control Plane adapter: %w", err)
	}
	hub, err := validateServiceURL(config.HubURL, config.AllowPublicHTTP)
	if err != nil {
		return nil, fmt.Errorf("invalid dev Hub adapter: %w", err)
	}
	baseClient := config.HTTPClient
	if baseClient == nil {
		baseClient = &http.Client{}
	}
	client := *baseClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Adapter{controlURL: strings.TrimSuffix(control.String(), "/"), hubURL: strings.TrimSuffix(hub.String(), "/"), client: &client, now: config.Now}, nil
}

// BeginLogin 启动显式 dev account flow；返回值不包含 access token。
func (adapter *Adapter) BeginLogin(ctx context.Context, request *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
	response := &cloudpb.LoginFlow{}
	if err := adapter.postProto(ctx, adapter.controlURL+ControlBeginLoginPath, session.Authorization{}, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// CompleteLogin 兑换 dev flow，并把 private token 包装为 Companion session。
func (adapter *Adapter) CompleteLogin(ctx context.Context, request *cloudpb.CompleteLoginRequest) (session.Session, error) {
	wire, err := adapter.postSession(ctx, ControlCompleteLoginPath, request)
	if err != nil {
		return session.Session{}, err
	}
	return sessionFromWire(wire, adapter.now())
}

// BeginDeviceEnrollment 获取 daemon DeviceIdentity enrollment challenge。
func (adapter *Adapter) BeginDeviceEnrollment(ctx context.Context, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	response := &cloudpb.DeviceEnrollmentChallenge{}
	if err := adapter.postProto(ctx, adapter.controlURL+ControlBeginEnrollmentPath, session.Authorization{}, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// CompleteDeviceEnrollment 验证公开 proof 并返回 private device cloud session。
func (adapter *Adapter) CompleteDeviceEnrollment(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (session.Session, error) {
	wire, err := adapter.postSession(ctx, ControlCompleteEnrollmentPath, request)
	if err != nil {
		return session.Session{}, err
	}
	return sessionFromWire(wire, adapter.now())
}

// RefreshSession 向 Control Plane 提交一次性 refresh secret，并接收轮换后的完整私有会话。
func (adapter *Adapter) RefreshSession(ctx context.Context, authorization session.RefreshAuthorization) (session.Session, error) {
	token := authorization.Bytes()
	defer clear(token)
	payload, err := json.Marshal(RefreshSessionWire{Kind: authorization.Metadata().Kind, RefreshToken: token})
	if err != nil {
		return session.Session{}, err
	}
	defer clear(payload)
	response, err := adapter.do(ctx, adapter.controlURL+ControlRefreshSessionPath, session.Authorization{}, JSONMediaType, payload)
	if err != nil {
		return session.Session{}, err
	}
	defer response.Body.Close()
	if !responseContentTypeIs(response, JSONMediaType) {
		return session.Session{}, protocolNetworkError("Control Plane returned an invalid refreshed session media type")
	}
	var wire SessionWire
	if decodeJSON(response.Body, &wire) != nil {
		return session.Session{}, protocolNetworkError("Control Plane returned an invalid refreshed session")
	}
	return sessionFromWire(wire, adapter.now())
}

// ResolveEndpoint 使用启动阶段 edge credential 和缓存 HubDirectory 向 Hub 解析 target。
func (adapter *Adapter) ResolveEndpoint(ctx context.Context, authorization session.Authorization, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	response := &cloudpb.ResolvedEndpoint{}
	if err := adapter.postEdgeHubProto(ctx, HubResolveEndpointPath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// ListManagedDevices 使用启动阶段 edge credential 从 Hub 内存投影读取账号设备。
func (adapter *Adapter) ListManagedDevices(ctx context.Context, authorization session.Authorization, request *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error) {
	response := &cloudpb.ListManagedDevicesResponse{}
	if err := adapter.postEdgeHubProto(ctx, HubListManagedDevicesPath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// BeginPresence 通过 daemon edge session 从 Hub 获取 fresh PresenceSession challenge。
func (adapter *Adapter) BeginPresence(ctx context.Context, authorization session.Authorization, request *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
	response := &cloudpb.PresenceChallenge{}
	if err := adapter.postEdgeHubProto(ctx, HubBeginPresencePath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// PlanManagedRoute 在 single Relay 闭环后仍稳定 fail closed；自动 SmartRoute 不属于 CLOUD004。
func (*Adapter) PlanManagedRoute(context.Context, session.Authorization, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	return nil, deferredServiceError("managed route planning")
}

// ReportPathQuality 在自动选路恢复前稳定 fail closed，不缓存或伪造 cloud telemetry 成功。
func (*Adapter) ReportPathQuality(context.Context, session.Authorization, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	return nil, deferredServiceError("path quality reporting")
}

// ReportConnectionOutcome 在自动选路恢复前稳定 fail closed，不把本地结果描述为已结算。
func (*Adapter) ReportConnectionOutcome(context.Context, session.Authorization, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	return nil, deferredServiceError("connection outcome reporting")
}

// OpenPresence 使用 daemon edge session 和 fresh proof 打开 Hub frame stream。
func (adapter *Adapter) OpenPresence(ctx context.Context, authorization session.Authorization, request *cloudpb.OpenPresenceRequest) (cloudservice.PresenceSource, error) {
	response, err := adapter.openEdgeHubStream(ctx, HubOpenPresencePath, authorization, request)
	if err != nil {
		return nil, err
	}
	return &presenceSource{streamSource: newStreamSource(response.Body)}, nil
}

// CreateSignalingSession 使用启动阶段 client edge credential 打开 Hub answer frame stream。
func (adapter *Adapter) CreateSignalingSession(ctx context.Context, authorization session.Authorization, request *cloudpb.CreateSignalingSessionRequest) (cloudservice.SignalingSource, error) {
	response, err := adapter.openEdgeHubStream(ctx, HubCreateSignalingPath, authorization, request)
	if err != nil {
		return nil, err
	}
	return &signalingSource{streamSource: newStreamSource(response.Body)}, nil
}

// CompleteSignalingOffer 使用 daemon edge credential 把 owning presence 的 answer/error 返回 Hub。
func (adapter *Adapter) CompleteSignalingOffer(ctx context.Context, authorization session.Authorization, request *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	response := &cloudpb.CompleteSignalingOfferResponse{}
	if err := adapter.postEdgeHubProto(ctx, HubCompleteSignalingPath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// ReportDaemonRuntime 使用 daemon edge credential 提交当前 Presence 的完整 runtime replacement。
func (adapter *Adapter) ReportDaemonRuntime(ctx context.Context, authorization session.Authorization, request *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error) {
	response := &cloudpb.ReportDaemonRuntimeResponse{}
	if err := adapter.postEdgeHubProto(ctx, HubReportDaemonRuntimePath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// ReportDaemonCommandResult 使用 daemon edge credential 提交独立 command execution receipt。
func (adapter *Adapter) ReportDaemonCommandResult(ctx context.Context, authorization session.Authorization, request *cloudpb.ReportDaemonCommandResultRequest) (*cloudpb.ReportDaemonCommandResultResponse, error) {
	response := &cloudpb.ReportDaemonCommandResultResponse{}
	if err := adapter.postEdgeHubProto(ctx, HubReportDaemonCommandResultPath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// AcquireRelayLease 通过 Hub 区域委派预算获取 caller-specific TURN material。
func (adapter *Adapter) AcquireRelayLease(ctx context.Context, authorization session.Authorization, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	response := &cloudpb.RelayLease{}
	if err := adapter.postEdgeHubProto(ctx, HubAcquireRelayLeasePath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

func (adapter *Adapter) postEdgeHubProto(ctx context.Context, path string, authorization session.Authorization, request, response proto.Message) error {
	httpResponse, err := adapter.doEdgeHub(ctx, path, authorization, request)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	if !responseContentTypeIs(httpResponse, ProtobufMediaType) {
		return protocolNetworkError("Hub returned an invalid protobuf media type")
	}
	data, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxBodyBytes+1))
	defer clear(data)
	if err != nil || len(data) > maxBodyBytes || proto.Unmarshal(data, response) != nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return protocolNetworkError("Hub returned an invalid protobuf response")
	}
	return nil
}

func (adapter *Adapter) openEdgeHubStream(ctx context.Context, path string, authorization session.Authorization, request proto.Message) (*http.Response, error) {
	response, err := adapter.doEdgeHub(ctx, path, authorization, request)
	if err != nil {
		return nil, err
	}
	if !responseContentTypeIs(response, StreamMediaType) {
		_ = response.Body.Close()
		return nil, protocolNetworkError("Hub returned an invalid stream media type")
	}
	return response, nil
}

func (adapter *Adapter) doEdgeHub(ctx context.Context, path string, authorization session.Authorization, request proto.Message) (*http.Response, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return nil, err
	}
	defer clear(payload)
	metadata := authorization.Metadata()
	envelope, err := json.Marshal(EdgeHubRequest{AccountID: metadata.AccountID, DeviceID: metadata.DeviceID, Payload: payload})
	if err != nil {
		return nil, err
	}
	defer clear(envelope)
	return adapter.do(ctx, adapter.hubURL+path, authorization, JSONMediaType, envelope)
}

func (adapter *Adapter) postSession(ctx context.Context, path string, request proto.Message) (SessionWire, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return SessionWire{}, err
	}
	defer clear(payload)
	response, err := adapter.do(ctx, adapter.controlURL+path, session.Authorization{}, ProtobufMediaType, payload)
	if err != nil {
		return SessionWire{}, err
	}
	defer response.Body.Close()
	if !responseContentTypeIs(response, JSONMediaType) {
		return SessionWire{}, protocolNetworkError("Control Plane returned an invalid cloud session media type")
	}
	var wire SessionWire
	if err := decodeJSON(response.Body, &wire); err != nil {
		return SessionWire{}, protocolNetworkError("Control Plane returned an invalid cloud session")
	}
	return wire, nil
}

func (adapter *Adapter) postProto(ctx context.Context, endpoint string, authorization session.Authorization, request, response proto.Message) error {
	payload, err := proto.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode cloud request: %w", err)
	}
	defer clear(payload)
	httpResponse, err := adapter.do(ctx, endpoint, authorization, ProtobufMediaType, payload)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	if !responseContentTypeIs(httpResponse, ProtobufMediaType) {
		return protocolNetworkError("cloud service returned an invalid protobuf media type")
	}
	data, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxBodyBytes+1))
	defer clear(data)
	if err != nil || len(data) > maxBodyBytes || proto.Unmarshal(data, response) != nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return protocolNetworkError("cloud service returned an invalid protobuf response")
	}
	return nil
}

func (adapter *Adapter) do(ctx context.Context, endpoint string, authorization session.Authorization, contentType string, payload []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", contentType)
	if token := authorization.Bytes(); len(token) > 0 {
		request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(token))
		clear(token)
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "dev cloud service is unavailable")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		if !responseContentTypeIs(response, ProtobufMediaType) {
			return nil, protocolNetworkError("cloud service returned an invalid error media type")
		}
		return nil, decodeCloudError(response.Body)
	}
	return response, nil
}

func sessionFromWire(wire SessionWire, now time.Time) (session.Session, error) {
	defer clear(wire.AccessToken)
	defer clear(wire.RefreshToken)
	metadata := session.Metadata{
		Kind: wire.Kind, AccountID: wire.AccountID, AccountLabel: wire.AccountLabel,
		DeviceID: wire.DeviceID, ExpiresAt: time.Unix(wire.ExpiresAt, 0).UTC(),
		HubID: wire.HubID, HubURL: wire.HubURL, HubRegion: wire.HubRegion, HubDirectoryVersion: wire.HubDirectoryVersion,
	}
	if len(wire.RefreshToken) > 0 {
		return session.NewRefreshable(metadata, wire.AccessToken, wire.RefreshToken, time.Unix(wire.RefreshExpiresAt, 0).UTC(), now)
	}
	return session.New(metadata, wire.AccessToken, now)
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxBodyBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func decodeCloudError(reader io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxBodyBytes+1))
	defer clear(data)
	if err != nil || len(data) > maxBodyBytes {
		return protocolNetworkError("cloud service returned an invalid error")
	}
	wire := &cloudpb.CloudError{}
	if proto.Unmarshal(data, wire) != nil || len(wire.ProtoReflect().GetUnknown()) != 0 || wire.GetCode() == cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED {
		return protocolNetworkError("cloud service returned an invalid error")
	}
	return cloudcompanion.ErrorFromWire(wire)
}

func responseContentTypeIs(response *http.Response, expected string) bool {
	if response == nil {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && mediaType == expected && len(parameters) == 0
}

func deferredServiceError(operation string) error {
	err := cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, operation+" is not enabled in CLOUD002 dev cloud")
	err.Retryable = false
	return err
}

func protocolNetworkError(message string) error {
	err := cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, message)
	err.Retryable = false
	return err
}
