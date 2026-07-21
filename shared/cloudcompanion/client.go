// Package cloudcompanion 定义公开 termx 进程与官方 Cloud Companion 之间的领域边界。
//
// 该包只描述账号状态、设备 presence、WebRTC signaling、RelayLease 和网络质量摘要。
// DeviceIdentity 私钥、CapabilityGrant、DataChannel 与 terminal protocol payload 必须留在公开进程内。
package cloudcompanion

import (
	"context"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

const (
	// ProtocolVersionMin 是当前公开进程能够发起的最小 Companion IPC 协议版本。
	// Hello 协商不到该版本时，调用方必须把 managed cloud endpoint 标记为不兼容，不能回退到旧 Hub API。
	ProtocolVersionMin uint32 = 4
	// ProtocolVersionMax 是当前公开进程能够接受的最大 Companion IPC 协议版本。
	// 新增能力必须通过 Hello 的能力交集启用，不能仅根据 companion 二进制版本猜测。
	ProtocolVersionMax uint32 = 4
)

// Client 是公开进程访问本机 Cloud Companion 的最小领域接口。
//
// 实现属于官方闭源 companion 或测试 fake；调用方仍拥有 WebRTC、DTLS、设备信任、terminal capability
// 与 termx protocol。任一方法失败只能影响当前 managed cloud endpoint，不能触发 local/SSH fallback。
type Client interface {
	// Hello 协商本地 IPC protocol version 与能力交集；每条新连接必须首先调用。
	Hello(context.Context, *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error)
	// Status 返回脱敏的 companion、账号和设备会话状态，不返回账号 token。
	Status(context.Context, *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error)
	// ResolveEndpoint 定位 managed daemon 并返回公开 WebRTC 所需的 managed session 与 ICE 配置。
	ResolveEndpoint(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error)
	// ListManagedDevices 返回当前账号的最小 client/daemon 目录，不授予 terminal capability。
	ListManagedDevices(context.Context, *cloudpb.ListManagedDevicesRequest) (*cloudpb.ListManagedDevicesResponse, error)
	// BeginPresence 为已 enrollment 的 daemon 获取一次性 device-scoped presence challenge。
	// challenge 必须由公开进程内的 DeviceIdentity 签名，Companion 不能代签或复用 enrollment proof。
	BeginPresence(context.Context, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error)
	// OpenPresence 为已完成 DeviceIdentity proof 的 daemon 打开下行 presence/signaling 流。
	OpenPresence(context.Context, *cloudpb.OpenPresenceRequest) (PresenceStream, error)
	// CreateSignalingSession 提交不含 capability 的 client WebRTC offer 并返回下行 answer 流。
	CreateSignalingSession(context.Context, *cloudpb.CreateSignalingSessionRequest) (SignalingStream, error)
	// CompleteSignalingOffer 回传 daemon 对单个 offer 的 answer 或稳定失败，不结束其他 signaling session。
	CompleteSignalingOffer(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error)
	// ReportDaemonRuntime 上报当前 Presence 绑定的完整 managed session inventory。
	// Companion 只转发 generated Proto，不读取 terminal、grant 或 DataChannel payload。
	ReportDaemonRuntime(context.Context, *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error)
	// ReportDaemonCommandResult 上报 daemon 对精确 deny-only command 的独立执行 receipt。
	ReportDaemonCommandResult(context.Context, *cloudpb.ReportDaemonCommandResultRequest) (*cloudpb.ReportDaemonCommandResultResponse, error)
	// AcquireRelayLease 获取服务准入租约；租约不表达 terminal 权限，也不能替代 CapabilityGrant。
	AcquireRelayLease(context.Context, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
	// PlanManagedRoute 获取只含 direct/single-relay ICE 约束和稳定原因的短期 SmartRoute 计划。
	PlanManagedRoute(context.Context, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error)
	// ReportPathQuality 上报不含 payload、grant 或 terminal identity 的聚合网络质量摘要。
	ReportPathQuality(context.Context, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error)
	// ReportConnectionOutcome 上报一次 managed connection 的路径结果和稳定错误分类。
	ReportConnectionOutcome(context.Context, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error)
}

// LifecycleClient 是公开 CLI 管理本机 Companion 账号与 daemon enrollment 的领域接口。
// 登录 token 只能由 Companion 写入 OS credential store；公开调用方只接收 flow、challenge 和脱敏 session summary。
type LifecycleClient interface {
	// BeginLogin 启动 browser 或 device-code 登录流程，不自动打开 URL 或执行下载脚本。
	BeginLogin(context.Context, *cloudpb.BeginLoginRequest) (*cloudpb.LoginFlow, error)
	// CompleteLogin 等待并完成指定登录 flow；成功后 secret 只保存在 Companion 的 OS credential store。
	CompleteLogin(context.Context, *cloudpb.CompleteLoginRequest) (*cloudpb.CompleteLoginResponse, error)
	// BeginDeviceEnrollment 用一次性 code 和公开 DeviceIdentity key 获取短期 challenge。
	BeginDeviceEnrollment(context.Context, *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error)
	// CompleteDeviceEnrollment 提交公开 daemon 对 challenge 的签名 proof，并保存 device 云会话。
	CompleteDeviceEnrollment(context.Context, *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.CompleteDeviceEnrollmentResponse, error)
	// Logout 删除明确选择的 account/device 云会话，不删除 DeviceIdentity、grant store 或 endpoint 配置。
	Logout(context.Context, *cloudpb.LogoutRequest) (*cloudpb.LogoutResponse, error)
	// Doctor 返回本机安装、协议、账号和设备状态的脱敏诊断。
	Doctor(context.Context, *cloudpb.DoctorRequest) (*cloudpb.DoctorResponse, error)
	// Shutdown 请求固定本地 Companion 有序退出；它不改变云会话或公开 daemon lifecycle。
	Shutdown(context.Context, *cloudpb.ShutdownRequest) (*cloudpb.ShutdownResponse, error)
}

// FullClient 组合 managed connectivity 与本地 lifecycle contract。
// 桌面 IPC client 和官方移动私有模块必须实现同一组合；公开 managed dialer 仍只依赖较小的 Client。
type FullClient interface {
	Client
	LifecycleClient
}

// PresenceStream 是 daemon 从 companion 接收 presence 状态和 WebRTC offer 的下行流。
// 流中只能出现 cloudpb.PresenceEvent；关闭或失败不允许停止 daemon 本地 listener，也不允许重建第二份 terminal inventory。
type PresenceStream interface {
	// Receive 阻塞读取下一条 daemon presence 事件；流结束或 IPC 失败时返回错误。
	Receive() (*cloudpb.PresenceEvent, error)
	// Close 幂等关闭当前 presence 流，不关闭 daemon 本地 listener。
	Close() error
}

// SignalingStream 是 client 从 companion 接收 WebRTC answer/candidate 的下行流。
// CapabilityGrant 必须等 DTLS DataChannel 建立后再直接交给目标 daemon，本流不得承载 grant 或 terminal payload。
type SignalingStream interface {
	// Receive 阻塞读取当前 signaling session 的 answer、candidate 或稳定错误。
	Receive() (*cloudpb.SignalingEvent, error)
	// Close 幂等关闭当前 signaling 流，不改变其他 endpoint 或 transport。
	Close() error
}
