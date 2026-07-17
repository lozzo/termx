package cloudcompanion

import (
	"fmt"
	"strings"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/cloudpb"
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

const (
	// RouteReasonInitialBest 表示首次决策选择当前综合最优候选。
	RouteReasonInitialBest RouteSelectionReason = "initial_best"
	// RouteReasonOnlyViable 表示只有一个候选通过全部硬约束。
	RouteReasonOnlyViable RouteSelectionReason = "only_viable"
	// RouteReasonLowerLoss 表示候选的丢失指标形成主要改善。
	RouteReasonLowerLoss RouteSelectionReason = "lower_loss"
	// RouteReasonDirectUnstable 表示 direct 连续越过稳定性阈值后选择 Relay。
	RouteReasonDirectUnstable RouteSelectionReason = "direct_unstable"
	// RouteReasonLowerLatency 表示候选的 P95 RTT 形成主要改善。
	RouteReasonLowerLatency RouteSelectionReason = "lower_latency"
	// RouteReasonLowerScore 表示候选在私有综合评分中胜出。
	RouteReasonLowerScore RouteSelectionReason = "lower_score"
	// RouteReasonCostGuard 表示更昂贵候选被可信 session budget 拒绝。
	RouteReasonCostGuard RouteSelectionReason = "cost_guard"
	// RouteReasonMinimumHold 表示当前路径仍处于最短保持时间。
	RouteReasonMinimumHold RouteSelectionReason = "minimum_hold"
	// RouteReasonCooldown 表示最近切换后的冷却时间尚未结束。
	RouteReasonCooldown RouteSelectionReason = "cooldown"
	// RouteReasonHysteresisHold 表示改善尚未连续满足所需窗口数。
	RouteReasonHysteresisHold RouteSelectionReason = "hysteresis_hold"
	// RouteReasonInsufficientImprovement 表示改善幅度未达到切换门槛。
	RouteReasonInsufficientImprovement RouteSelectionReason = "insufficient_improvement"
	// RouteReasonCurrentUnavailable 表示当前候选失效后切换到仍可用候选。
	RouteReasonCurrentUnavailable RouteSelectionReason = "current_unavailable"
	// RouteReasonCurrentBest 表示当前路径仍是最优候选。
	RouteReasonCurrentBest RouteSelectionReason = "current_best"
)

// EndpointPhase 是 TUI、App 和测试 fixture 共用的 managed endpoint 连接阶段。
// 它只描述客户端运行时，不拥有 daemon terminal lifecycle，也不能作为授权成功的替代证据。
type EndpointPhase string

// Path 是 WebRTC transport 的实际网络路径投影。
// direct、single_relay 与 relay_mesh 都属于同一个 endpoint/transport，路径变化不得重建 endpoint identity。
type Path string

// RouteSelectionReason 是 TUI/App 可展示的稳定选路诊断，不包含私有分数、权重或成本数值。
type RouteSelectionReason string

// DialPolicy 是 connection registry relay_mode 到 Companion route preference 和 WebRTC ICE policy 的唯一投影。
// RoutePreference 约束云端可分配的付费能力；RelayOnly 只约束公开 WebRTC primitive 的 candidate policy。
type DialPolicy struct {
	RoutePreference cloudpb.RoutePreference
	RelayOnly       bool
}

// ValidateManagedRoute 校验公开客户端准备建立的 managed WebRTC route。
// Endpoint 持有 daemon identity，route 持有 Cloud target/credential ref；该函数不读取 secret，也不访问 Companion 或 daemon。
func ValidateManagedRoute(endpoint endpointdomain.Endpoint, route endpointdomain.AccessRoute) error {
	if err := endpoint.Validate(); err != nil {
		return err
	}
	if err := route.Validate(endpoint.DaemonIdentity); err != nil {
		return err
	}
	if route.Kind != endpointdomain.RouteManagedWebRTC {
		return fmt.Errorf("endpoint %q route %q kind %q is not managed WebRTC", endpoint.ID, route.ID, route.Kind)
	}
	return nil
}

// DialPolicyForRelayMode 把公开 registry 的 relay_mode 映射为统一的云 route preference 与 ICE 约束。
// 未知值 fail closed；调用方不得因此退回 local、SSH、旧 Hub API 或任意默认中继。
func DialPolicyForRelayMode(mode endpointdomain.RelayMode) (DialPolicy, error) {
	switch endpointdomain.RelayMode(strings.TrimSpace(string(mode))) {
	case "", endpointdomain.RelayAuto:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY}, nil
	case endpointdomain.RelayDirect:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY}, nil
	case endpointdomain.RelayOnly:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, RelayOnly: true}, nil
	case endpointdomain.RelaySmart:
		return DialPolicy{RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE}, nil
	default:
		return DialPolicy{}, fmt.Errorf("unknown managed WebRTC relay mode %q", mode)
	}
}

// RouteSelectionReasonFromWire 把 Companion 枚举投影为公开稳定字符串。
// 未知值保持空值，调用方必须把它视为协议错误而不是猜测私有选路原因。
func RouteSelectionReasonFromWire(reason cloudpb.RouteSelectionReason) RouteSelectionReason {
	switch reason {
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INITIAL_BEST:
		return RouteReasonInitialBest
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_ONLY_VIABLE:
		return RouteReasonOnlyViable
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_LOWER_LOSS:
		return RouteReasonLowerLoss
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_DIRECT_UNSTABLE:
		return RouteReasonDirectUnstable
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_LOWER_LATENCY:
		return RouteReasonLowerLatency
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_LOWER_SCORE:
		return RouteReasonLowerScore
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_COST_GUARD:
		return RouteReasonCostGuard
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_MINIMUM_HOLD:
		return RouteReasonMinimumHold
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_COOLDOWN:
		return RouteReasonCooldown
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_HYSTERESIS_HOLD:
		return RouteReasonHysteresisHold
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INSUFFICIENT_IMPROVEMENT:
		return RouteReasonInsufficientImprovement
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_CURRENT_UNAVAILABLE:
		return RouteReasonCurrentUnavailable
	case cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_CURRENT_BEST:
		return RouteReasonCurrentBest
	default:
		return ""
	}
}

// IsKnownRouteSelectionReason 判断字符串是否属于公开 contract 冻结的 SmartRoute 原因集合。
// UI 和 adapter 只能投影这些稳定值；空值表示非 SmartRoute，其他值必须按协议错误处理。
func IsKnownRouteSelectionReason(reason RouteSelectionReason) bool {
	switch reason {
	case RouteReasonInitialBest, RouteReasonOnlyViable, RouteReasonLowerLoss, RouteReasonDirectUnstable,
		RouteReasonLowerLatency, RouteReasonLowerScore, RouteReasonCostGuard, RouteReasonMinimumHold,
		RouteReasonCooldown, RouteReasonHysteresisHold, RouteReasonInsufficientImprovement,
		RouteReasonCurrentUnavailable, RouteReasonCurrentBest:
		return true
	default:
		return false
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
