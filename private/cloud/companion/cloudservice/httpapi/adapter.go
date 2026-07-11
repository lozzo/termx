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

// Config 固定显式 dev-local Control Plane/Hub origin 与 HTTP transport。
// 两个 origin 必须是 loopback http；redirect 始终被拒绝，避免 bearer 或 admission 被转发。
type Config struct {
	ControlPlaneURL string
	HubURL          string
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

// New 创建只允许 loopback 的 dev-local adapter。
// 非 http、带 userinfo/query/path、非 loopback 或缺失 origin 均 fail closed。
func New(config Config) (*Adapter, error) {
	control, err := validateLoopbackURL(config.ControlPlaneURL)
	if err != nil {
		return nil, fmt.Errorf("invalid dev Control Plane adapter: %w", err)
	}
	hub, err := validateLoopbackURL(config.HubURL)
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

// ResolveEndpoint 通过 account session 创建独立 ManagedSession。
func (adapter *Adapter) ResolveEndpoint(ctx context.Context, authorization session.Authorization, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	response := &cloudpb.ResolvedEndpoint{}
	if err := adapter.postProto(ctx, adapter.controlURL+ControlResolveEndpointPath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// BeginPresence 通过 device cloud session 获取 fresh PresenceSession challenge。
func (adapter *Adapter) BeginPresence(ctx context.Context, authorization session.Authorization, request *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
	response := &cloudpb.PresenceChallenge{}
	if err := adapter.postProto(ctx, adapter.controlURL+ControlBeginPresencePath, authorization, request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// AcquirePresenceAdmission 验证 fresh proof 并取得 presence-only Hub ticket。
func (adapter *Adapter) AcquirePresenceAdmission(ctx context.Context, authorization session.Authorization, request *cloudpb.OpenPresenceRequest) (cloudservice.HubAdmission, error) {
	return adapter.postAdmission(ctx, ControlPresenceAdmissionPath, authorization, request)
}

// AcquireClientAdmission 为固定 ManagedSession 和 target 取得 client offer ticket。
func (adapter *Adapter) AcquireClientAdmission(ctx context.Context, authorization session.Authorization, request *cloudpb.CreateSignalingSessionRequest) (cloudservice.HubAdmission, error) {
	return adapter.postAdmission(ctx, ControlClientAdmissionPath, authorization, request)
}

// AcquireDaemonAnswerAdmission 为 daemon 已消费的单个 offer 取得 managed answer ticket。
func (adapter *Adapter) AcquireDaemonAnswerAdmission(ctx context.Context, authorization session.Authorization, managedSessionID string, request *cloudpb.CompleteSignalingOfferRequest) (cloudservice.HubAdmission, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return cloudservice.HubAdmission{}, fmt.Errorf("encode answer admission request: %w", err)
	}
	defer clear(payload)
	wire := AnswerAdmissionRequest{ManagedSessionID: managedSessionID, Payload: payload}
	return adapter.postAdmissionJSON(ctx, ControlAnswerAdmissionPath, authorization, wire)
}

// AcquireRelayLease 通过真实 Control Plane listener 获取 caller-specific 短期 TURN material。
// signed lease 与 credential 只返回公开 WebRTC primitive；adapter 不缓存、不记录，也不把它们写进 endpoint registry。
func (adapter *Adapter) AcquireRelayLease(ctx context.Context, authorization session.Authorization, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	response := &cloudpb.RelayLease{}
	if err := adapter.postProto(ctx, adapter.controlURL+ControlAcquireRelayLeasePath, authorization, request, response); err != nil {
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

// OpenPresence 使用 presence-only admission 打开 Hub frame stream。
func (adapter *Adapter) OpenPresence(ctx context.Context, _ session.Authorization, admission cloudservice.HubAdmission, request *cloudpb.OpenPresenceRequest) (cloudservice.PresenceSource, error) {
	response, err := adapter.openHubStream(ctx, HubOpenPresencePath, admission, request)
	if err != nil {
		return nil, err
	}
	return &presenceSource{streamSource: newStreamSource(response.Body)}, nil
}

// CreateSignalingSession 使用 managed client admission 打开 Hub answer frame stream。
func (adapter *Adapter) CreateSignalingSession(ctx context.Context, _ session.Authorization, admission cloudservice.HubAdmission, request *cloudpb.CreateSignalingSessionRequest) (cloudservice.SignalingSource, error) {
	response, err := adapter.openHubStream(ctx, HubCreateSignalingPath, admission, request)
	if err != nil {
		return nil, err
	}
	return &signalingSource{streamSource: newStreamSource(response.Body)}, nil
}

// CompleteSignalingOffer 使用 managed daemon admission 把 answer/error 返回 Hub。
func (adapter *Adapter) CompleteSignalingOffer(ctx context.Context, _ session.Authorization, admission cloudservice.HubAdmission, request *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	response := &cloudpb.CompleteSignalingOfferResponse{}
	if err := adapter.postHubProto(ctx, HubCompleteSignalingPath, admission, request, response); err != nil {
		return nil, err
	}
	return response, nil
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

func (adapter *Adapter) postAdmission(ctx context.Context, path string, authorization session.Authorization, request proto.Message) (cloudservice.HubAdmission, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return cloudservice.HubAdmission{}, err
	}
	defer clear(payload)
	return adapter.postAdmissionBytes(ctx, path, authorization, ProtobufMediaType, payload)
}

func (adapter *Adapter) postAdmissionJSON(ctx context.Context, path string, authorization session.Authorization, request any) (cloudservice.HubAdmission, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return cloudservice.HubAdmission{}, err
	}
	defer clear(payload)
	return adapter.postAdmissionBytes(ctx, path, authorization, JSONMediaType, payload)
}

func (adapter *Adapter) postAdmissionBytes(ctx context.Context, path string, authorization session.Authorization, contentType string, payload []byte) (cloudservice.HubAdmission, error) {
	response, err := adapter.do(ctx, adapter.controlURL+path, authorization, contentType, payload)
	if err != nil {
		return cloudservice.HubAdmission{}, err
	}
	defer response.Body.Close()
	if !responseContentTypeIs(response, JSONMediaType) {
		return cloudservice.HubAdmission{}, protocolNetworkError("Control Plane returned an invalid Hub admission media type")
	}
	var wire AdmissionWire
	if err := decodeJSON(response.Body, &wire); err != nil {
		return cloudservice.HubAdmission{}, protocolNetworkError("Control Plane returned an invalid Hub admission")
	}
	return admissionFromWire(wire, adapter.now())
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

func (adapter *Adapter) postHubProto(ctx context.Context, path string, admission cloudservice.HubAdmission, request, response proto.Message) error {
	payload, err := proto.Marshal(request)
	if err != nil {
		return err
	}
	defer clear(payload)
	wire := admissionToWire(admission)
	defer clear(wire.Ticket)
	envelope, err := json.Marshal(HubRequest{Admission: wire, Payload: payload})
	if err != nil {
		return err
	}
	defer clear(envelope)
	httpResponse, err := adapter.do(ctx, adapter.hubURL+path, session.Authorization{}, JSONMediaType, envelope)
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

func (adapter *Adapter) openHubStream(ctx context.Context, path string, admission cloudservice.HubAdmission, request proto.Message) (*http.Response, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return nil, err
	}
	defer clear(payload)
	wire := admissionToWire(admission)
	defer clear(wire.Ticket)
	envelope, err := json.Marshal(HubRequest{Admission: wire, Payload: payload})
	if err != nil {
		return nil, err
	}
	defer clear(envelope)
	response, err := adapter.do(ctx, adapter.hubURL+path, session.Authorization{}, JSONMediaType, envelope)
	if err != nil {
		return nil, err
	}
	if !responseContentTypeIs(response, StreamMediaType) {
		_ = response.Body.Close()
		return nil, protocolNetworkError("Hub returned an invalid stream media type")
	}
	return response, nil
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
	return session.New(session.Metadata{
		Kind: wire.Kind, AccountID: wire.AccountID, AccountLabel: wire.AccountLabel,
		DeviceID: wire.DeviceID, ExpiresAt: time.Unix(wire.ExpiresAt, 0).UTC(),
	}, wire.AccessToken, now)
}

func admissionFromWire(wire AdmissionWire, now time.Time) (cloudservice.HubAdmission, error) {
	defer clear(wire.Ticket)
	return cloudservice.NewHubAdmission(cloudservice.HubAdmissionMetadata{
		Reference: wire.Reference, HubID: wire.HubID, AccountID: wire.AccountID, DeviceID: wire.DeviceID,
		TargetDeviceID: wire.TargetDeviceID, SessionKind: wire.SessionKind, SessionID: wire.SessionID,
		ExpiresAt: time.Unix(wire.ExpiresAt, 0).UTC(),
	}, wire.Ticket, now)
}

func admissionToWire(admission cloudservice.HubAdmission) AdmissionWire {
	return AdmissionWire{
		Reference: admission.Reference, HubID: admission.HubID, AccountID: admission.AccountID, DeviceID: admission.DeviceID,
		TargetDeviceID: admission.TargetDeviceID, SessionKind: admission.SessionKind, SessionID: admission.SessionID,
		ExpiresAt: admission.ExpiresAt.Unix(), Ticket: admission.TicketBytes(),
	}
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
