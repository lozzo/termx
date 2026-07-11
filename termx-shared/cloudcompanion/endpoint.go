package cloudcompanion

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/connection"
)

const (
	// EndpointPhaseIdle 表示 managed endpoint 尚未开始连接。
	EndpointPhaseIdle EndpointPhase = "idle"
	// EndpointPhaseResolving 表示公开客户端正在通过 Companion 解析目标设备和 managed session。
	EndpointPhaseResolving EndpointPhase = "resolving"
	// EndpointPhaseSignaling 表示公开客户端正在交换 SDP/ICE，Companion 仍不能接触 grant 或 DataChannel。
	EndpointPhaseSignaling EndpointPhase = "signaling"
	// EndpointPhaseConnecting 表示 WebRTC 正在完成 ICE/DTLS 连接。
	EndpointPhaseConnecting EndpointPhase = "connecting"
	// EndpointPhaseAuthorizing 表示 DataChannel 已建立，公开客户端正在执行设备证明和 capability handshake。
	EndpointPhaseAuthorizing EndpointPhase = "authorizing"
	// EndpointPhaseConnected 表示端到端授权已经完成，termx protocol 可以开始工作。
	EndpointPhaseConnected EndpointPhase = "connected"
	// EndpointPhaseFailed 表示当前 endpoint 局部失败；该状态不得触发其他 transport 或旧协议 fallback。
	EndpointPhaseFailed EndpointPhase = "failed"
)

const (
	// PathUnknown 表示尚未形成可报告的 WebRTC candidate path。
	PathUnknown Path = ""
	// PathDirect 表示 client 与 daemon 使用直接 candidate pair。
	PathDirect Path = "direct"
	// PathSingleRelay 表示端到端 DTLS 流量经过一个托管 Relay。
	PathSingleRelay Path = "single_relay"
	// PathRelayMesh 表示端到端 DTLS 流量经过两个 Edge Relay 和受控 backbone。
	PathRelayMesh Path = "relay_mesh"
)

// EndpointPhase 是 TUI、App 和测试 fixture 共用的 managed endpoint 连接阶段。
// 它只描述客户端运行时，不拥有 daemon terminal lifecycle，也不能作为授权成功的替代证据。
type EndpointPhase string

// Path 是 WebRTC transport 的实际网络路径投影。
// direct、single_relay 与 relay_mesh 都属于同一个 endpoint/transport，路径变化不得重建 endpoint identity。
type Path string

// DialPolicy 是 connection registry relay_mode 到 Companion route preference 和 WebRTC ICE policy 的唯一投影。
// RoutePreference 约束云端可分配的付费能力；RelayOnly 只约束公开 WebRTC primitive 的 candidate policy。
type DialPolicy struct {
	RoutePreference cloudpb.RoutePreference
	RelayOnly       bool
}

// ValidateManagedConfig 校验公开客户端准备建立的 managed WebRTC endpoint。
// 真值来自共享 connection registry；该函数不读取 grant secret，也不访问 Companion 或 daemon。
func ValidateManagedConfig(cfg connection.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.Transport != connection.TransportHubP2P {
		return fmt.Errorf("endpoint %q transport %q is not managed WebRTC", cfg.ID, cfg.Transport)
	}
	return nil
}

// DialPolicyForRelayMode 把公开 registry 的 relay_mode 映射为统一的云 route preference 与 ICE 约束。
// 未知值 fail closed；调用方不得因此退回 local、SSH、旧 Hub API 或任意默认中继。
func DialPolicyForRelayMode(mode connection.RelayMode) (DialPolicy, error) {
	switch connection.RelayMode(strings.TrimSpace(string(mode))) {
	case "", connection.RelayAuto:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY}, nil
	case connection.RelayDirect:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY}, nil
	case connection.RelayOnly:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, RelayOnly: true}, nil
	default:
		return DialPolicy{}, fmt.Errorf("unknown managed WebRTC relay mode %q", mode)
	}
}

// PathFromWire 把 Companion 的 wire observed path 转换为公开客户端稳定值。
// UNSPECIFIED 保持 unknown，未知枚举不得猜测成 direct 或免费路径。
func PathFromWire(path cloudpb.ObservedPath) Path {
	switch path {
	case cloudpb.ObservedPath_OBSERVED_PATH_DIRECT:
		return PathDirect
	case cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY:
		return PathSingleRelay
	case cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH:
		return PathRelayMesh
	default:
		return PathUnknown
	}
}

// StableErrorName 返回 TUI、App 和 fixture 共用的 cloud 错误名称。
// 名称只用于跨平台状态投影；安全分支仍必须使用 CloudErrorCode 枚举。
func StableErrorName(code cloudpb.CloudErrorCode) string {
	name := strings.TrimPrefix(code.String(), "CLOUD_ERROR_CODE_")
	return strings.ToLower(name)
}
