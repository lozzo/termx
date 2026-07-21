package smartroute

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
	"sync"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

type scoredCandidate struct {
	candidate Candidate
	score     Score
}

type sessionState struct {
	selectedID           string
	selectedAt           time.Time
	lastSwitchAt         time.Time
	lastEvaluated        time.Time
	challengerID         string
	challengerWins       uint32
	challengerEvidenceAt time.Time
}

// Planner 是 managed-session scoped hysteresis 和选择状态的并发安全 owner。
// Select 只返回决策，不调用 Companion、Hub、Relay、WebRTC 或 terminal runtime。
type Planner struct {
	mu       sync.Mutex
	config   Config
	sessions map[string]*sessionState
}

// NewPlanner 创建容量有界的 single-relay SmartRoute planner。
// 无效配置直接失败，不提供无 cost guard、无 hold 或无 session bound 的 fallback。
func NewPlanner(config Config) (*Planner, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Planner{config: config, sessions: make(map[string]*sessionState)}, nil
}

// Select 对候选执行硬约束、私有评分和 session hysteresis。
// 候选局部失败进入 Diagnostics；只有所有候选都失败时返回 ErrNoViableRoute，已有 session 状态保持不变。
func (planner *Planner) Select(request Request, now time.Time) (Decision, error) {
	if planner == nil || !canonicalIdentifier(request.ManagedSessionID) || now.IsZero() {
		return Decision{}, fmt.Errorf("invalid SmartRoute request")
	}
	if len(request.Candidates) == 0 {
		return Decision{}, ErrNoViableRoute
	}
	if len(request.Candidates) > planner.config.MaxCandidates {
		return Decision{}, ErrTooManyCandidates
	}
	now = now.UTC()
	viable, diagnostics := planner.evaluateCandidates(request, now)
	if len(viable) == 0 {
		return Decision{Diagnostics: diagnostics}, ErrNoViableRoute
	}
	sort.Slice(viable, func(left, right int) bool {
		if viable[left].score.Total != viable[right].score.Total {
			return viable[left].score.Total < viable[right].score.Total
		}
		if viable[left].candidate.Path != viable[right].candidate.Path {
			return viable[left].candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT
		}
		return viable[left].candidate.ID < viable[right].candidate.ID
	})

	planner.mu.Lock()
	defer planner.mu.Unlock()
	planner.prune(now)
	state := planner.sessions[request.ManagedSessionID]
	if state != nil && now.Before(state.lastEvaluated) {
		return Decision{}, fmt.Errorf("SmartRoute decision time moved backwards")
	}
	best := viable[0]
	if state == nil {
		if len(planner.sessions) >= planner.config.MaxSessions {
			return Decision{}, ErrCapacity
		}
		planner.sessions[request.ManagedSessionID] = &sessionState{
			selectedID: best.candidate.ID, selectedAt: now, lastEvaluated: now,
		}
		return Decision{
			Selected: best.candidate, Score: best.score,
			Reason: initialReason(best, viable, diagnostics), Diagnostics: diagnostics,
		}, nil
	}

	current, currentOK := findScoredCandidate(viable, state.selectedID)
	if !currentOK {
		reason := ReasonCurrentUnavailable
		if best.candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT &&
			rejectionForCandidate(diagnostics, state.selectedID) == RejectionCostGuard {
			reason = ReasonCostGuard
		}
		planner.switchState(state, best.candidate.ID, now)
		return Decision{Selected: best.candidate, Score: best.score, Reason: reason, Changed: true, Diagnostics: diagnostics}, nil
	}
	state.lastEvaluated = now
	if best.candidate.ID == current.candidate.ID {
		resetChallenger(state)
		return Decision{Selected: current.candidate, Score: current.score, Reason: ReasonCurrentBest, Diagnostics: diagnostics}, nil
	}
	if !state.lastSwitchAt.IsZero() && now.Before(state.lastSwitchAt.Add(planner.config.SwitchCooldown)) {
		return Decision{
			Selected: current.candidate, Score: current.score, Reason: ReasonCooldown,
			HoldUntil: state.lastSwitchAt.Add(planner.config.SwitchCooldown), Diagnostics: diagnostics,
		}, nil
	}
	if now.Before(state.selectedAt.Add(planner.config.MinimumHold)) {
		return Decision{
			Selected: current.candidate, Score: current.score, Reason: ReasonMinimumHold,
			HoldUntil: state.selectedAt.Add(planner.config.MinimumHold), Diagnostics: diagnostics,
		}, nil
	}
	if improvementBasisPoints(current.score.Total, best.score.Total) < planner.config.MinimumImprovementBasisPoints {
		resetChallenger(state)
		return Decision{Selected: current.candidate, Score: current.score, Reason: ReasonInsufficientImprovement, Diagnostics: diagnostics}, nil
	}
	if state.challengerID != best.candidate.ID {
		state.challengerID = best.candidate.ID
		state.challengerWins = 1
		state.challengerEvidenceAt = best.candidate.Quality.LatestWindowEndedAt
	} else if best.candidate.Quality.LatestWindowEndedAt.After(state.challengerEvidenceAt) && state.challengerWins < math.MaxUint32 {
		state.challengerWins++
		state.challengerEvidenceAt = best.candidate.Quality.LatestWindowEndedAt
	}
	if state.challengerWins < planner.config.RequiredConsecutiveWins {
		return Decision{Selected: current.candidate, Score: current.score, Reason: ReasonHysteresisHold, Diagnostics: diagnostics}, nil
	}
	reason := switchReason(planner.config, current, best)
	planner.switchState(state, best.candidate.ID, now)
	return Decision{Selected: best.candidate, Score: best.score, Reason: reason, Changed: true, Diagnostics: diagnostics}, nil
}

func (planner *Planner) evaluateCandidates(request Request, now time.Time) ([]scoredCandidate, []CandidateDiagnostic) {
	counts := make(map[string]int, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if canonicalIdentifier(candidate.ID) {
			counts[candidate.ID]++
		}
	}
	viable := make([]scoredCandidate, 0, len(request.Candidates))
	diagnostics := make([]CandidateDiagnostic, 0)
	for _, candidate := range request.Candidates {
		if counts[candidate.ID] > 1 {
			diagnostics = append(diagnostics, CandidateDiagnostic{CandidateID: candidate.ID, Rejection: RejectionDuplicate})
			continue
		}
		rejection := planner.rejectCandidate(candidate, request.CostBudget, now)
		if rejection != "" {
			diagnostics = append(diagnostics, CandidateDiagnostic{CandidateID: candidate.ID, Rejection: rejection})
			continue
		}
		viable = append(viable, scoredCandidate{candidate: candidate, score: planner.score(candidate)})
	}
	return viable, diagnostics
}

func (planner *Planner) rejectCandidate(candidate Candidate, budget CostBudget, now time.Time) RejectionReason {
	if !canonicalIdentifier(candidate.ID) {
		return RejectionMalformed
	}
	if candidate.Path != cloudpb.ObservedPath_OBSERVED_PATH_DIRECT && candidate.Path != cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY {
		return RejectionUnsupportedPath
	}
	if candidate.Quality.Series.ObservedPath != candidate.Path || candidate.Quality.WindowCount == 0 ||
		candidate.Quality.SampleCount < planner.config.MinimumSamples || candidate.Quality.LatestWindowEndedAt.IsZero() ||
		candidate.Quality.LatestWindowEndedAt.After(now) || !now.Before(candidate.QualityValidUntil) {
		return RejectionInsufficientQuality
	}
	if !candidate.Constraints.Reachable {
		return RejectionUnreachable
	}
	if !candidate.Constraints.PolicyAllowed {
		return RejectionPolicy
	}
	if candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT {
		if candidate.RelayID != "" || candidate.Region != "" || candidate.Cost.State != CostNone ||
			candidate.Cost.EstimatedMicrounits != 0 || !candidate.Cost.ValidUntil.IsZero() {
			return RejectionMalformed
		}
		return ""
	}
	if !canonicalIdentifier(candidate.RelayID) || !validTag(candidate.Region) {
		return RejectionMalformed
	}
	if !candidate.Constraints.Entitled {
		return RejectionEntitlement
	}
	if !candidate.Constraints.Healthy {
		return RejectionUnhealthy
	}
	if !candidate.Constraints.CapacityAvailable {
		return RejectionCapacity
	}
	if candidate.Cost.State != CostEstimated || !now.Before(candidate.Cost.ValidUntil) || !budget.Known {
		return RejectionCostUnknown
	}
	if candidate.Cost.EstimatedMicrounits > budget.MaxMicrounits {
		return RejectionCostGuard
	}
	return ""
}

func (planner *Planner) score(candidate Candidate) Score {
	weights := planner.config.Weights
	baseline := candidate.Quality
	score := Score{
		LatencyPenalty:     saturatingMultiply(baseline.MeanWindowRTTP95Millis, weights.Latency),
		LossPenalty:        saturatingMultiply(uint64(baseline.LossBasisPoints), weights.Loss),
		JitterPenalty:      saturatingMultiply(baseline.MeanWindowJitterMillis, weights.Jitter),
		InstabilityPenalty: saturatingMultiply(disconnectRateBasisPoints(baseline.DisconnectCount, baseline.ConnectedMillis), weights.Instability),
		CongestionPenalty:  saturatingMultiply(throughputShortfallBasisPoints(baseline.MeanThroughputBPS, weights.TargetThroughputBPS), weights.Congestion),
	}
	if candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY {
		score.HopPenalty = weights.RelayHop
		costUnits := divideCeil(candidate.Cost.EstimatedMicrounits, weights.CostUnitMicrounits)
		score.CostPenalty = saturatingMultiply(costUnits, weights.Cost)
	}
	for _, penalty := range []uint64{
		score.LatencyPenalty, score.LossPenalty, score.JitterPenalty, score.InstabilityPenalty,
		score.CongestionPenalty, score.HopPenalty, score.CostPenalty,
	} {
		score.Total = saturatingAdd(score.Total, penalty)
	}
	return score
}

func (planner *Planner) prune(now time.Time) {
	for managedSessionID, state := range planner.sessions {
		if !now.Before(state.lastEvaluated) && now.Sub(state.lastEvaluated) >= planner.config.StateTTL {
			delete(planner.sessions, managedSessionID)
		}
	}
}

func (planner *Planner) switchState(state *sessionState, candidateID string, now time.Time) {
	state.selectedID = candidateID
	state.selectedAt = now
	state.lastSwitchAt = now
	state.lastEvaluated = now
	resetChallenger(state)
}

func resetChallenger(state *sessionState) {
	state.challengerID = ""
	state.challengerWins = 0
	state.challengerEvidenceAt = time.Time{}
}

func initialReason(best scoredCandidate, viable []scoredCandidate, diagnostics []CandidateDiagnostic) Reason {
	if len(viable) == 1 {
		if best.candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT && hasRejection(diagnostics, RejectionCostGuard) {
			return ReasonCostGuard
		}
		return ReasonOnlyViable
	}
	return ReasonInitialBest
}

func switchReason(config Config, current, best scoredCandidate) Reason {
	if current.candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT &&
		best.candidate.Path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY &&
		(current.candidate.Quality.LossBasisPoints >= config.DirectUnstableLossBasisPoints ||
			current.candidate.Quality.MeanWindowRTTP95Millis >= config.DirectUnstableRTTP95Millis) {
		return ReasonDirectUnstable
	}
	if best.candidate.Quality.LossBasisPoints < current.candidate.Quality.LossBasisPoints {
		return ReasonLowerLoss
	}
	if best.candidate.Quality.MeanWindowRTTP95Millis < current.candidate.Quality.MeanWindowRTTP95Millis {
		return ReasonLowerLatency
	}
	return ReasonLowerScore
}

func findScoredCandidate(candidates []scoredCandidate, candidateID string) (scoredCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.candidate.ID == candidateID {
			return candidate, true
		}
	}
	return scoredCandidate{}, false
}

func rejectionForCandidate(diagnostics []CandidateDiagnostic, candidateID string) RejectionReason {
	for _, diagnostic := range diagnostics {
		if diagnostic.CandidateID == candidateID {
			return diagnostic.Rejection
		}
	}
	return ""
}

func hasRejection(diagnostics []CandidateDiagnostic, rejection RejectionReason) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Rejection == rejection {
			return true
		}
	}
	return false
}

func improvementBasisPoints(current, candidate uint64) uint32 {
	if candidate >= current || current == 0 {
		return 0
	}
	delta := current - candidate
	high, low := bits.Mul64(delta, 10_000)
	if high >= current {
		return 10_000
	}
	value, _ := bits.Div64(high, low, current)
	if value > 10_000 {
		value = 10_000
	}
	return uint32(value)
}

func disconnectRateBasisPoints(disconnects, connectedMillis uint64) uint64 {
	if disconnects == 0 || connectedMillis == 0 {
		return 0
	}
	high, low := bits.Mul64(disconnects, uint64(time.Minute/time.Millisecond)*10_000)
	if high >= connectedMillis {
		return 10_000
	}
	value, _ := bits.Div64(high, low, connectedMillis)
	if value > 10_000 {
		return 10_000
	}
	return value
}

func throughputShortfallBasisPoints(actual, target uint64) uint64 {
	if actual >= target || target == 0 {
		return 0
	}
	return uint64(improvementBasisPoints(target, actual))
}

func divideCeil(value, divisor uint64) uint64 {
	if value == 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}

func saturatingMultiply(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > math.MaxUint64/right {
		return math.MaxUint64
	}
	return left * right
}

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}
