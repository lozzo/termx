// Package smartroute 在私有 Route Planner 内选择 direct 或受限 single-relay 路径。
//
// 本包消费已经脱敏的质量基线、可信成本估算和服务端硬约束；它不读取 terminal、CapabilityGrant、
// DataChannel payload，也不签发 RelayLease 或直接操作客户端 transport。
package smartroute

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/route-planner/quality"
	"github.com/lozzow/termx/proto/cloudpb"
)

var (
	// ErrNoViableRoute 表示所有候选都被各自的硬约束拒绝。
	// 调用方可以记录 Decision.Diagnostics，但不得 fallback 到未授权 Relay 或其他 endpoint transport。
	ErrNoViableRoute = errors.New("no viable SmartRoute candidate")
	// ErrCapacity 表示 planner 的有界 managed-session 状态已满。
	// 生产 adapter 应持久化或分片状态，不能静默驱逐仍活跃会话后丢失 hysteresis。
	ErrCapacity = errors.New("SmartRoute session capacity reached")
	// ErrTooManyCandidates 表示候选源超过当前阶段允许的受限集合。
	// GA002 禁止无上限并发探测或把任意 Relay 列表交给 planner。
	ErrTooManyCandidates = errors.New("SmartRoute candidate limit exceeded")
)

// Reason 是私有决策映射到公开稳定诊断的值。
// 它不包含分数、阈值、权重、成本数值或供应商合同信息。
type Reason string

const (
	// ReasonInitialBest 表示首次决策选中综合最优候选。
	ReasonInitialBest Reason = "initial_best"
	// ReasonOnlyViable 表示只有一个候选通过全部硬约束。
	ReasonOnlyViable Reason = "only_viable"
	// ReasonLowerLoss 表示候选主要因更低丢失胜出。
	ReasonLowerLoss Reason = "lower_loss"
	// ReasonDirectUnstable 表示 direct 越过稳定性阈值后选择 Relay。
	ReasonDirectUnstable Reason = "direct_unstable"
	// ReasonLowerLatency 表示候选主要因更低 P95 RTT 胜出。
	ReasonLowerLatency Reason = "lower_latency"
	// ReasonLowerScore 表示候选在私有综合评分中胜出。
	ReasonLowerScore Reason = "lower_score"
	// ReasonCostGuard 表示更昂贵候选超过可信 session budget。
	ReasonCostGuard Reason = "cost_guard"
	// ReasonMinimumHold 表示当前路径仍处于最短保持时间。
	ReasonMinimumHold Reason = "minimum_hold"
	// ReasonCooldown 表示最近切换后的冷却时间尚未结束。
	ReasonCooldown Reason = "cooldown"
	// ReasonHysteresisHold 表示改善尚未连续满足所需决策次数。
	ReasonHysteresisHold Reason = "hysteresis_hold"
	// ReasonInsufficientImprovement 表示改善幅度不足以承担切换风险。
	ReasonInsufficientImprovement Reason = "insufficient_improvement"
	// ReasonCurrentUnavailable 表示当前候选失效后切换到其他可用候选。
	ReasonCurrentUnavailable Reason = "current_unavailable"
	// ReasonCurrentBest 表示当前路径仍是综合最优候选。
	ReasonCurrentBest Reason = "current_best"
)

// RejectionReason 是单个候选未进入评分集合的稳定私有诊断。
// 一个候选被拒绝不会自动拒绝同一 managed session 的其他候选。
type RejectionReason string

const (
	// RejectionMalformed 表示候选 identity、region、质量或成本形状非法。
	RejectionMalformed RejectionReason = "malformed"
	// RejectionDuplicate 表示候选 ID 在同一次请求中重复，无法形成确定性 route identity。
	RejectionDuplicate RejectionReason = "duplicate"
	// RejectionUnsupportedPath 表示候选不是 GA002 允许的 direct/single-relay。
	RejectionUnsupportedPath RejectionReason = "unsupported_path"
	// RejectionInsufficientQuality 表示质量基线不足或已经过期。
	RejectionInsufficientQuality RejectionReason = "insufficient_quality"
	// RejectionUnreachable 表示主动探测或当前拓扑确认候选不可达。
	RejectionUnreachable RejectionReason = "unreachable"
	// RejectionUnhealthy 表示 Relay 节点健康检查不通过。
	RejectionUnhealthy RejectionReason = "unhealthy"
	// RejectionCapacity 表示 Relay 当前没有可分配容量。
	RejectionCapacity RejectionReason = "capacity"
	// RejectionPolicy 表示数据驻留、组织或区域策略拒绝候选。
	RejectionPolicy RejectionReason = "policy"
	// RejectionEntitlement 表示账号无权使用该 Relay route class。
	RejectionEntitlement RejectionReason = "entitlement"
	// RejectionCostUnknown 表示 Relay 成本尚未由可信 rate card 定价。
	RejectionCostUnknown RejectionReason = "cost_unknown"
	// RejectionCostGuard 表示可信成本估算超过当前 session budget。
	RejectionCostGuard RejectionReason = "cost_guard"
)

// CostState 区分尚未定价、明确无托管成本和已由私有 rate card 定价。
// 显式零估价仍使用 CostEstimated，不能与 CostUnknown 混淆。
type CostState string

const (
	// CostUnknown 表示候选尚无可信成本，不允许进入付费 Relay 选择。
	CostUnknown CostState = "unknown"
	// CostNone 表示 direct 等路径已确认没有托管网络成本。
	CostNone CostState = "none"
	// CostEstimated 表示私有 route/provider rate card 已生成有效估算。
	CostEstimated CostState = "estimated"
)

// CostEstimate 是候选在当前计划有效期内的可信私有成本投影。
// EstimatedMicrounits 不进入 public route plan；ValidUntil 防止 planner 使用过期费率。
type CostEstimate struct {
	State               CostState
	EstimatedMicrounits uint64
	ValidUntil          time.Time
}

// CostBudget 是 entitlement/套餐为单次计划提供的可信成本上限。
// Known=false 时所有付费 Relay 候选 fail closed，公开 caller 不能自行声明预算。
type CostBudget struct {
	Known         bool
	MaxMicrounits uint64
}

// CandidateConstraints 是候选进入评分前必须满足的服务端硬约束。
// direct 只消费 Reachable/PolicyAllowed；single-relay 还必须满足健康、容量和 entitlement。
type CandidateConstraints struct {
	Reachable         bool
	Healthy           bool
	CapacityAvailable bool
	PolicyAllowed     bool
	Entitled          bool
}

// Candidate 是一个受限 direct 或 single-relay 候选。
// Quality 来自 Probe Aggregator，Cost 来自可信 rate card，Constraints 来自拓扑、策略和 entitlement owner。
type Candidate struct {
	ID                string
	Path              cloudpb.ObservedPath
	RelayID           string
	Region            string
	Quality           quality.Baseline
	QualityValidUntil time.Time
	Cost              CostEstimate
	Constraints       CandidateConstraints
}

// Request 是同一 managed session 的一次 SmartRoute 决策输入。
// Candidates 和 CostBudget 只能由私有服务装配；公开 IPC 不携带质量、成本或约束字段。
type Request struct {
	ManagedSessionID string
	Candidates       []Candidate
	CostBudget       CostBudget
}

// Weights 是私有整数评分的可配置权重。
// 这些数值属于服务运营配置，不进入 public protobuf、日志或 selection reason。
type Weights struct {
	Latency             uint64
	Loss                uint64
	Jitter              uint64
	Instability         uint64
	Congestion          uint64
	RelayHop            uint64
	Cost                uint64
	TargetThroughputBPS uint64
	CostUnitMicrounits  uint64
}

// Config 固定候选/状态容量、质量门槛和防抖策略。
// 所有时间和容量必须显式有效；零值 Config 不会退化成无界或立即抖动的 planner。
type Config struct {
	MaxCandidates                 int
	MaxSessions                   int
	StateTTL                      time.Duration
	MinimumSamples                uint64
	MinimumHold                   time.Duration
	SwitchCooldown                time.Duration
	RequiredConsecutiveWins       uint32
	MinimumImprovementBasisPoints uint32
	DirectUnstableLossBasisPoints uint32
	DirectUnstableRTTP95Millis    uint64
	Weights                       Weights
}

// DefaultConfig 返回 GA002 的保守初始策略。
// 生产可按真实 corridor 数据调整，但调整仍只能发生在私有服务配置中。
func DefaultConfig() Config {
	return Config{
		MaxCandidates:                 8,
		MaxSessions:                   4_096,
		StateTTL:                      30 * time.Minute,
		MinimumSamples:                4,
		MinimumHold:                   30 * time.Second,
		SwitchCooldown:                2 * time.Minute,
		RequiredConsecutiveWins:       3,
		MinimumImprovementBasisPoints: 1_500,
		DirectUnstableLossBasisPoints: 200,
		DirectUnstableRTTP95Millis:    250,
		Weights: Weights{
			Latency: 4, Loss: 20, Jitter: 6, Instability: 10, Congestion: 2,
			RelayHop: 200, Cost: 1, TargetThroughputBPS: 128_000, CostUnitMicrounits: 100,
		},
	}
}

// Validate 拒绝无界容量、无效时间、不可达门槛和空评分模型。
// 失败配置不能创建 Planner，也不能通过默认零值绕过 hysteresis 或 cost guard。
func (config Config) Validate() error {
	if config.MaxCandidates < 1 || config.MaxSessions < 1 || config.StateTTL <= 0 || config.MinimumSamples < 2 {
		return fmt.Errorf("invalid SmartRoute capacity or quality configuration")
	}
	if config.MinimumHold < 0 || config.SwitchCooldown < 0 || config.RequiredConsecutiveWins < 1 ||
		config.MinimumImprovementBasisPoints > 10_000 || config.DirectUnstableLossBasisPoints > 10_000 {
		return fmt.Errorf("invalid SmartRoute hysteresis configuration")
	}
	weights := config.Weights
	if weights.TargetThroughputBPS == 0 || weights.CostUnitMicrounits == 0 ||
		weights.Latency|weights.Loss|weights.Jitter|weights.Instability|weights.Congestion|weights.RelayHop|weights.Cost == 0 {
		return fmt.Errorf("invalid SmartRoute score weights")
	}
	return nil
}

// Score 保存一个候选的私有可解释分项。
// 该类型只供服务端诊断和测试；公开 route plan 只能返回稳定 Reason。
type Score struct {
	LatencyPenalty     uint64
	LossPenalty        uint64
	JitterPenalty      uint64
	InstabilityPenalty uint64
	CongestionPenalty  uint64
	HopPenalty         uint64
	CostPenalty        uint64
	Total              uint64
}

// CandidateDiagnostic 描述单个候选的局部拒绝原因。
// 它不包含原始地址、内部成本值、score、terminal 或 credential。
type CandidateDiagnostic struct {
	CandidateID string
	Rejection   RejectionReason
}

// Decision 是 Planner 对当前 managed session 的无副作用结果。
// Selected 只表达计划意图；调用方仍需取得有效 ICE material、建立 WebRTC 并完成公开端到端授权。
type Decision struct {
	Selected    Candidate
	Score       Score
	Reason      Reason
	Changed     bool
	HoldUntil   time.Time
	Diagnostics []CandidateDiagnostic
}

func canonicalIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t ")
}

func validTag(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}
