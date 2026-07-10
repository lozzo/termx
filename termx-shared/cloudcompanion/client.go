// Package cloudcompanion 定义公开 termx 进程与官方 Cloud Companion 之间的领域边界。
//
// 该包只描述账号状态、设备 presence、WebRTC signaling、RelayLease 和网络质量摘要。
// DeviceIdentity 私钥、CapabilityGrant、DataChannel 与 terminal protocol payload 必须留在公开进程内。
package cloudcompanion

import (
	"context"

	"github.com/lozzow/termx/termx-proto/cloudpb"
)

const (
	// ProtocolVersionMin 是当前公开进程能够发起的最小 Companion IPC 协议版本。
	// Hello 协商不到该版本时，调用方必须把 managed cloud endpoint 标记为不兼容，不能回退到旧 Hub API。
	ProtocolVersionMin uint32 = 1
	// ProtocolVersionMax 是当前公开进程能够接受的最大 Companion IPC 协议版本。
	// 新增能力必须通过 Hello 的能力交集启用，不能仅根据 companion 二进制版本猜测。
	ProtocolVersionMax uint32 = 1
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
	// OpenPresence 为已完成 DeviceIdentity proof 的 daemon 打开下行 presence/signaling 流。
	OpenPresence(context.Context, *cloudpb.OpenPresenceRequest) (PresenceStream, error)
	// CreateSignalingSession 提交不含 capability 的 client WebRTC offer 并返回下行 answer 流。
	CreateSignalingSession(context.Context, *cloudpb.CreateSignalingSessionRequest) (SignalingStream, error)
	// CompleteSignalingOffer 回传 daemon 对单个 offer 的 answer 或稳定失败，不结束其他 signaling session。
	CompleteSignalingOffer(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error)
	// AcquireRelayLease 获取服务准入租约；租约不表达 terminal 权限，也不能替代 CapabilityGrant。
	AcquireRelayLease(context.Context, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error)
	// ReportPathQuality 上报不含 payload、grant 或 terminal identity 的聚合网络质量摘要。
	ReportPathQuality(context.Context, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error)
	// ReportConnectionOutcome 上报一次 managed connection 的路径结果和稳定错误分类。
	ReportConnectionOutcome(context.Context, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error)
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
