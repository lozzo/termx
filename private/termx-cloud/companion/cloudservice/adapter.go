// Package cloudservice 定义 desktop companion 到私有 Control Plane 与 Hub 的网络 adapter 边界。
//
// Adapter 只传递账号 authorization、设备公开 proof、SDP/ICE、Relay lease 和质量摘要；
// 不允许出现 DeviceIdentity 私钥、CapabilityGrant、DataChannel 或 terminal protocol payload。
package cloudservice

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/companion/session"
	"github.com/lozzow/termx/termx-proto/cloudpb"
)

// HubAdmission 是 companion 内部持有的短期 Hub 服务凭据。
// Ticket 不进入 public IPC response；Reference、HubID 和 expiry 可用于脱敏诊断。
type HubAdmission struct {
	Reference        string
	HubID            string
	ManagedSessionID string
	ExpiresAt        time.Time
	ticket           []byte
}

// NewHubAdmission 创建 Control Plane 返回的短期 Hub admission。
// 空 ticket、identity binding 或已过期 admission 会被拒绝。
func NewHubAdmission(reference, hubID, managedSessionID string, expiresAt time.Time, ticket []byte, now time.Time) (HubAdmission, error) {
	if reference == "" || hubID == "" || managedSessionID == "" || len(ticket) == 0 || !now.Before(expiresAt) {
		return HubAdmission{}, fmt.Errorf("invalid Hub admission")
	}
	return HubAdmission{Reference: reference, HubID: hubID, ManagedSessionID: managedSessionID, ExpiresAt: expiresAt.UTC(), ticket: append([]byte(nil), ticket...)}, nil
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
	return fmt.Sprintf("HubAdmission{reference=%s hub_id=%s managed_session_id=%s expires_at=%s ticket=[REDACTED]}", admission.Reference, admission.HubID, admission.ManagedSessionID, admission.ExpiresAt.Format(time.RFC3339))
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
	// AcquirePresenceAdmission 为 daemon proof 获取短期 Hub presence admission。
	AcquirePresenceAdmission(context.Context, session.Authorization, *cloudpb.OpenPresenceRequest) (HubAdmission, error)
	// AcquireClientAdmission 为固定 managed session 与 target 获取 client signaling admission。
	AcquireClientAdmission(context.Context, session.Authorization, *cloudpb.CreateSignalingSessionRequest) (HubAdmission, error)
	// AcquireDaemonAnswerAdmission 为 presence 收到的固定 managed session 获取 daemon answer admission。
	AcquireDaemonAnswerAdmission(context.Context, session.Authorization, string, *cloudpb.CompleteSignalingOfferRequest) (HubAdmission, error)
	// AcquireRelayLease 根据当前 entitlement 获取 caller-specific 短期 Relay lease 和 route plan。
	AcquireRelayLease(context.Context, session.Authorization, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
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
// HubAdmission 只决定 signaling 服务准入，HubAdapter 不验证 terminal scope 或 grant。
type HubAdapter interface {
	// OpenPresence 使用 daemon-specific admission 打开有界 presence event source。
	OpenPresence(context.Context, session.Authorization, HubAdmission, *cloudpb.OpenPresenceRequest) (PresenceSource, error)
	// CreateSignalingSession 使用 client-specific admission 转发 offer/ICE 并返回 answer source。
	CreateSignalingSession(context.Context, session.Authorization, HubAdmission, *cloudpb.CreateSignalingSessionRequest) (SignalingSource, error)
	// CompleteSignalingOffer 把 daemon 对当前 presence 中 offer 的 answer 或稳定错误返回 Hub。
	CompleteSignalingOffer(context.Context, session.Authorization, HubAdmission, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error)
}
