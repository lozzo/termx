// Package cloudservice 定义 desktop companion 到私有 Control Plane 与 Hub 的网络 adapter 边界。
//
// Adapter 只传递账号 authorization、设备公开 proof、SDP/ICE、Relay lease、route plan 和质量摘要；
// 不允许出现 DeviceIdentity 私钥、CapabilityGrant、DataChannel 或 terminal protocol payload。
package cloudservice

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
)

// HubSessionKind 是 Companion 对 presence/managed Hub admission 的私有语义标签。
// 它只用于校验 Control Plane 返回的短票据绑定，不进入 public endpoint 或 terminal protocol。
type HubSessionKind string

const (
	// HubSessionPresence 表示一个 daemon device-scoped PresenceSession。
	HubSessionPresence HubSessionKind = "presence"
	// HubSessionManaged 表示一个 client-target ManagedSession。
	HubSessionManaged HubSessionKind = "managed"
)

// HubAdmissionMetadata 是 Companion 转发 Hub 请求所需的非秘密 ticket binding。
// Account/Device/Session 字段来自 Control Plane 响应，Hub 仍必须以 signed ticket 为最终准入真值。
type HubAdmissionMetadata struct {
	Reference      string
	HubID          string
	AccountID      string
	DeviceID       string
	TargetDeviceID string
	SessionKind    HubSessionKind
	SessionID      string
	ExpiresAt      time.Time
}

// HubAdmission 是 companion 内部持有的短期 Hub 服务凭据。
// Ticket 不进入 public IPC response；Metadata 只能用于请求绑定和脱敏诊断。
type HubAdmission struct {
	HubAdmissionMetadata
	ticket []byte
}

// NewHubAdmission 创建 Control Plane 返回的短期 Hub admission。
// 空 ticket、identity binding、错误 session kind 或已过期 admission 会被拒绝。
func NewHubAdmission(metadata HubAdmissionMetadata, ticket []byte, now time.Time) (HubAdmission, error) {
	if metadata.Reference == "" || metadata.HubID == "" || metadata.AccountID == "" || metadata.DeviceID == "" || metadata.SessionID == "" || len(ticket) == 0 || !now.Before(metadata.ExpiresAt) {
		return HubAdmission{}, fmt.Errorf("invalid Hub admission")
	}
	switch metadata.SessionKind {
	case HubSessionPresence:
		if metadata.TargetDeviceID != "" {
			return HubAdmission{}, fmt.Errorf("invalid Hub presence admission target")
		}
	case HubSessionManaged:
	default:
		return HubAdmission{}, fmt.Errorf("invalid Hub admission session kind")
	}
	metadata.ExpiresAt = metadata.ExpiresAt.UTC()
	return HubAdmission{HubAdmissionMetadata: metadata, ticket: append([]byte(nil), ticket...)}, nil
}

// TicketBytes 返回 Hub TLS adapter 使用的短期 ticket 副本。
// 该值禁止写入 public protobuf、日志、URL 或长期本地存储。
func (admission HubAdmission) TicketBytes() []byte {
	return append([]byte(nil), admission.ticket...)
}

// Destroy 清理 companion 当前 HubAdmission 实例持有的短期 ticket bytes。
// HubAdapter 建立认证 stream 或完成同步请求后必须立即调用，续期应向 Control Plane 重新申请。
func (admission *HubAdmission) Destroy() {
	if admission == nil {
		return
	}
	clear(admission.ticket)
	admission.ticket = nil
}

// String 返回脱敏 admission reference，不泄漏签名 ticket body。
func (admission HubAdmission) String() string {
	return fmt.Sprintf("HubAdmission{reference=%s hub_id=%s session_kind=%s session_id=%s expires_at=%s ticket=[REDACTED]}", admission.Reference, admission.HubID, admission.SessionKind, admission.SessionID, admission.ExpiresAt.Format(time.RFC3339))
}

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
	// ResolveEndpoint 创建或定位 managed session，并返回 Hub/ICE 最小 metadata。
	ResolveEndpoint(context.Context, session.Authorization, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error)
	// BeginPresence 为 device cloud session 获取独立的一次性 presence challenge。
	BeginPresence(context.Context, session.Authorization, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error)
	// AcquirePresenceAdmission 为 daemon proof 获取短期 Hub presence admission。
	AcquirePresenceAdmission(context.Context, session.Authorization, *cloudpb.OpenPresenceRequest) (HubAdmission, error)
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
	// OpenPresence 使用 daemon-specific admission 打开有界 presence event source。
	OpenPresence(context.Context, session.Authorization, HubAdmission, *cloudpb.OpenPresenceRequest) (PresenceSource, error)
	// CreateSignalingSession 使用启动阶段 client edge credential 转发 offer；请求热路径不得访问 Control Plane。
	CreateSignalingSession(context.Context, session.Authorization, *cloudpb.CreateSignalingSessionRequest) (SignalingSource, error)
	// CompleteSignalingOffer 使用 daemon edge credential 和 active presence ownership 返回 answer。
	CompleteSignalingOffer(context.Context, session.Authorization, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error)
	// AcquireRelayLease 使用 edge credential 和 Hub 本地区域预算取得 caller-specific TURN material。
	AcquireRelayLease(context.Context, session.Authorization, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
}
