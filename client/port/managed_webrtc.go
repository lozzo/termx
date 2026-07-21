package port

import (
	"context"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// ManagedMessageChannel 是 Go Client Engine 对可靠有序 WebRTC DataChannel 的最小平台要求。
// native Pion 与浏览器 adapter 都必须复制收到的 bytes，并实现显式背压和关闭；该接口不解释 remote-auth 或 Proto API payload。
type ManagedMessageChannel interface {
	// SetMessageHandler 注册完整 DataChannel message 接收器；实现必须在回调前复制平台可复用 buffer。
	SetMessageHandler(func([]byte))
	// SetCloseHandler 注册底层 channel 生命周期终止通知；回调不得自行创建新 session。
	SetCloseHandler(func())
	// BufferedAmount 返回平台当前尚未发送的字节数，只用于 transport 背压。
	BufferedAmount() uint64
	// SetBufferedAmountLowThreshold 设置解除发送等待的低水位。
	SetBufferedAmountLowThreshold(uint64)
	// SetBufferedAmountLowHandler 注册低水位通知，允许重复或合并通知但不得丢失 channel close。
	SetBufferedAmountLowHandler(func())
	// Send 发送一个可靠有序 message；失败必须显式返回，不能静默丢帧或 fallback。
	Send([]byte) error
	// Close 幂等关闭当前 channel，不修改 endpoint registry 或 session generation。
	Close() error
}

// ManagedPeerFactory 创建单个 managed WebRTC attempt 使用的平台 peer。
// ICE material 来自已验证的 Companion route plan；factory 不得自行请求其他 route、修改 relay policy 或执行 fallback。
type ManagedPeerFactory interface {
	// OpenManagedPeer 只使用调用方提供的 ICE material 和 policy 创建单个 peer。
	OpenManagedPeer(context.Context, []*cloudpb.IceServer, cloudpb.RoutePreference, bool) (ManagedPeer, error)
}

// ManagedPeer 是 native Pion 与浏览器 RTCPeerConnection 共同实现的平台 primitive。
// Go managed adapter 拥有 signaling/auth/protocol 顺序；实现只能执行指定 SDP/ICE 操作并报告实际 DTLS/path 证据。
type ManagedPeer interface {
	// Channel 返回创建 peer 时同步建立的可靠有序 protocol DataChannel。
	Channel() ManagedMessageChannel
	// CreateOffer 生成完成本地 candidate gathering 的 SDP offer。
	CreateOffer(context.Context) (string, error)
	// ApplyAnswer 应用受信 signaling 返回的 SDP answer 与 remote candidates。
	ApplyAnswer(context.Context, string, []*cloudpb.IceCandidate) error
	// WaitReady 等待 DataChannel 可发送；context 取消必须停止等待并由调用方关闭 peer。
	WaitReady(context.Context) error
	// RemoteCertificateFingerprint 返回实际 DTLS peer certificate 的规范化 SHA-256 fingerprint。
	RemoteCertificateFingerprint() (string, error)
	// ObservedPath 返回当前 selected candidate pair 的 direct/single-relay 投影。
	ObservedPath() endpoint.Path
	// Snapshot 返回不含地址和身份信息的质量计数；无稳定 candidate pair 时返回 false。
	Snapshot(time.Time) (ManagedPeerSnapshot, bool)
	// Close 幂等释放 peer、ICE、DTLS、SCTP 和 DataChannel 平台资源。
	Close() error
}

// ManagedPeerSnapshot 是质量上报读取的平台网络计数快照。
// 它不包含 IP、hostname、SDP、credential、endpoint label 或 terminal identity；计数回退表示底层 candidate pair 已换代。
type ManagedPeerSnapshot struct {
	PairID       string
	Path         endpoint.Path
	NetworkClass string
	At           time.Time
	RoundTrip    time.Duration
	BytesSent    uint64
	BytesRecv    uint64
	PacketsSent  uint64
	LossEvents   uint64
	Connected    bool
}
