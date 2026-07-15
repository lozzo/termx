// Package cloudservice 定义 desktop companion 到私有 Control Plane 与 Hub 的网络 adapter 边界。
//
// Adapter 只传递账号 authorization、设备公开 proof、SDP/ICE、Relay lease、route plan 和质量摘要；
// 不允许出现 DeviceIdentity 私钥、CapabilityGrant、DataChannel 或 terminal protocol payload。
package cloudservice

import (
	"context"

	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
)

// ControlPlaneAdapter 是 companion 调用官方 Control Plane 的私有 TLS contract。
// 实现负责把 Authorization 变成账号或设备 request credential，并返回稳定 cloudcompanion.Error。
type ControlPlaneAdapter interface {
	// BeginLogin 启动官方 browser/device-code flow，只返回 public flow metadata。
	BeginLogin(context.Context, *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error)
	// CompleteLogin 兑换指定 flow 并返回只在 companion 内可读取 secret 的 account session。
	CompleteLogin(context.Context, *cloudpb.CompleteLoginRequest) (session.Session, error)
	// BeginDeviceEnrollment 用一次性 code、public key 和设备 metadata 获取短期 challenge。
	BeginDeviceEnrollment(context.Context, *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error)
	// CompleteDeviceEnrollment 验证公开 daemon 的 DeviceProof 并返回 private device cloud session。
	CompleteDeviceEnrollment(context.Context, *cloudpb.CompleteDeviceEnrollmentRequest) (session.Session, error)
	// RefreshSession 单次轮换 OS credential store 中的 refresh secret，并返回新的 edge session。
	RefreshSession(context.Context, session.RefreshAuthorization) (session.Session, error)
	// PlanManagedRoute 获取不含私有 score/cost 的 direct/single-relay SmartRoute 计划。
	PlanManagedRoute(context.Context, session.Authorization, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error)
	// ReportPathQuality 转发不含 payload 和 terminal identity 的聚合质量摘要。
	ReportPathQuality(context.Context, session.Authorization, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error)
	// ReportConnectionOutcome 转发 managed connection 的稳定路径结果。
	ReportConnectionOutcome(context.Context, session.Authorization, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error)
}

// PresenceSource 是 Hub adapter 的 daemon 下行事件源。
// Receive 必须响应 context cancel；Close 只关闭当前 daemon connection 的 presence。
type PresenceSource interface {
	Receive(context.Context) (*cloudpb.PresenceEvent, error)
	Close() error
}

// SignalingSource 是 Hub adapter 的单 managed session 下行事件源。
// 它只能承载 answer、ICE candidate、稳定错误或 closed event。
type SignalingSource interface {
	Receive(context.Context) (*cloudpb.SignalingEvent, error)
	Close() error
}

// HubAdapter 是 companion 调用官方 Hub 的私有 TLS contract。
// account/device edge credential 由 Hub 离线验证；HubAdapter 不验证 terminal scope 或 grant。
type HubAdapter interface {
	// BeginPresence 使用 daemon edge credential 从 Hub 本地 policy 获取一次性 DeviceIdentity challenge。
	BeginPresence(context.Context, session.Authorization, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error)
	// OpenPresence 使用同一 daemon edge credential 和 fresh proof 打开有界 presence event source。
	OpenPresence(context.Context, session.Authorization, *cloudpb.OpenPresenceRequest) (PresenceSource, error)
	// CreateSignalingSession 使用启动阶段 client edge credential 转发 offer；请求热路径不得访问 Control Plane。
	CreateSignalingSession(context.Context, session.Authorization, *cloudpb.CreateSignalingSessionRequest) (SignalingSource, error)
	// CompleteSignalingOffer 使用 daemon edge credential 和 active presence ownership 返回 answer。
	CompleteSignalingOffer(context.Context, session.Authorization, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error)
	// AcquireRelayLease 使用 edge credential 和 Hub 本地区域预算取得 caller-specific TURN material。
	AcquireRelayLease(context.Context, session.Authorization, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
	// ResolveEndpoint 使用缓存 HubDirectory 和 edge credential 从 Hub 本地解析 target presence。
	ResolveEndpoint(context.Context, session.Authorization, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error)
	// ListManagedDevices 从 Hub 签名内存投影读取同账号 client/daemon 目录。
	ListManagedDevices(context.Context, session.Authorization, *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error)
}
