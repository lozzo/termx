package cloudservice

import (
	"context"

	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

// UnconfiguredAdapter 是未注入官方 TLS service adapter 时的显式 fail-closed 边界。
// 它只允许 Companion binary 完成本地 Hello/Status/installer smoke；所有 cloud/login/signaling 操作返回 ROUTE_UNAVAILABLE，绝不访问旧 Hub 或本地 fake 服务。
type UnconfiguredAdapter struct{}

// NewUnconfiguredAdapter 创建不具备任何 cloud network capability 的显式 adapter。
// official release build 必须用真实私有 TLS adapter 替换它；该类型不会因环境变量或 endpoint 配置自动升级权限。
func NewUnconfiguredAdapter() *UnconfiguredAdapter { return &UnconfiguredAdapter{} }

// BeginLogin 拒绝未装配的账号登录网络请求。
func (*UnconfiguredAdapter) BeginLogin(context.Context, *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error) {
	return nil, unavailableAdapterError()
}

// CompleteLogin 拒绝未装配的账号 flow 兑换。
func (*UnconfiguredAdapter) CompleteLogin(context.Context, *cloudpb.CompleteLoginRequest) (session.Session, error) {
	return session.Session{}, unavailableAdapterError()
}

// BeginDeviceEnrollment 拒绝未装配的 device enrollment 网络请求。
func (*UnconfiguredAdapter) BeginDeviceEnrollment(context.Context, *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	return nil, unavailableAdapterError()
}

// CompleteDeviceEnrollment 拒绝未装配的 device proof 兑换。
func (*UnconfiguredAdapter) CompleteDeviceEnrollment(context.Context, *cloudpb.CompleteDeviceEnrollmentRequest) (session.Session, error) {
	return session.Session{}, unavailableAdapterError()
}

// ResolveEndpoint 拒绝未装配的 managed endpoint 定位。
func (*UnconfiguredAdapter) ResolveEndpoint(context.Context, session.Authorization, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	return nil, unavailableAdapterError()
}

// BeginPresence 拒绝未装配的 fresh daemon presence challenge 请求。
func (*UnconfiguredAdapter) BeginPresence(context.Context, session.Authorization, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
	return nil, unavailableAdapterError()
}

// AcquirePresenceAdmission 拒绝未装配的 daemon presence admission。
func (*UnconfiguredAdapter) AcquirePresenceAdmission(context.Context, session.Authorization, *cloudpb.OpenPresenceRequest) (HubAdmission, error) {
	return HubAdmission{}, unavailableAdapterError()
}

// PlanManagedRoute 拒绝未装配的 SmartRoute 计划请求。
func (*UnconfiguredAdapter) PlanManagedRoute(context.Context, session.Authorization, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	return nil, unavailableAdapterError()
}

// ReportPathQuality 拒绝未装配的质量上报。
func (*UnconfiguredAdapter) ReportPathQuality(context.Context, session.Authorization, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	return nil, unavailableAdapterError()
}

// ReportConnectionOutcome 拒绝未装配的连接结果上报。
func (*UnconfiguredAdapter) ReportConnectionOutcome(context.Context, session.Authorization, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	return nil, unavailableAdapterError()
}

// OpenPresence 拒绝未装配的 Hub presence stream。
func (*UnconfiguredAdapter) OpenPresence(context.Context, session.Authorization, HubAdmission, *cloudpb.OpenPresenceRequest) (PresenceSource, error) {
	return nil, unavailableAdapterError()
}

// CreateSignalingSession 拒绝未装配的 Hub signaling stream。
func (*UnconfiguredAdapter) CreateSignalingSession(context.Context, session.Authorization, *cloudpb.CreateSignalingSessionRequest) (SignalingSource, error) {
	return nil, unavailableAdapterError()
}

// CompleteSignalingOffer 拒绝未装配的 Hub answer 请求。
func (*UnconfiguredAdapter) CompleteSignalingOffer(context.Context, session.Authorization, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
	return nil, unavailableAdapterError()
}

// AcquireRelayLease 拒绝未装配的 Hub Relay lease 请求。
func (*UnconfiguredAdapter) AcquireRelayLease(context.Context, session.Authorization, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	return nil, unavailableAdapterError()
}

func unavailableAdapterError() error {
	err := cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "official cloud service adapter is not configured in this build")
	err.Retryable = false
	return err
}
