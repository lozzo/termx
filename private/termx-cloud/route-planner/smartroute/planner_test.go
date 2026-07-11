package smartroute

import (
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/route-planner/quality"
	"github.com/lozzow/termx/termx-proto/cloudpb"
)

func TestPlannerSwitchesFromUnstableDirectAfterConsecutiveImprovement(t *testing.T) {
	now := time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC)
	config := testConfig()
	config.RequiredConsecutiveWins = 2
	planner, err := NewPlanner(config)
	if err != nil {
		t.Fatal(err)
	}
	initial := Request{
		ManagedSessionID: "managed-1",
		CostBudget:       CostBudget{Known: true, MaxMicrounits: 1_000},
		Candidates: []Candidate{
			directCandidate(now, 40, 10, 0),
			relayCandidate(now, "relay-eu", 80, 20, 0, 100),
		},
	}
	decision, err := planner.Select(initial, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Selected.ID != "direct" || decision.Reason != ReasonInitialBest || decision.Changed {
		t.Fatalf("initial decision = %+v", decision)
	}

	degraded := initial
	degraded.Candidates = []Candidate{
		directCandidate(now.Add(time.Minute), 450, 600, 1),
		relayCandidate(now.Add(time.Minute), "relay-eu", 70, 20, 0, 100),
	}
	first, err := planner.Select(degraded, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Selected.ID != "direct" || first.Reason != ReasonHysteresisHold || first.Changed {
		t.Fatalf("first degraded decision = %+v", first)
	}
	repeated, err := planner.Select(degraded, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Selected.ID != "direct" || repeated.Reason != ReasonHysteresisHold || repeated.Changed {
		t.Fatalf("repeated evidence decision = %+v", repeated)
	}
	refreshed := degraded
	refreshed.Candidates = []Candidate{
		directCandidate(now.Add(2*time.Minute), 450, 600, 1),
		relayCandidate(now.Add(2*time.Minute), "relay-eu", 70, 20, 0, 100),
	}
	second, err := planner.Select(refreshed, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Selected.ID != "relay-eu" || second.Reason != ReasonDirectUnstable || !second.Changed {
		t.Fatalf("second degraded decision = %+v", second)
	}
}

func TestPlannerCostGuardKeepsDirectWithoutRejectingWholeRequest(t *testing.T) {
	now := time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC)
	planner, _ := NewPlanner(testConfig())
	decision, err := planner.Select(Request{
		ManagedSessionID: "managed-cost",
		CostBudget:       CostBudget{Known: true, MaxMicrounits: 50},
		Candidates: []Candidate{
			directCandidate(now, 300, 400, 1),
			relayCandidate(now, "relay-fast", 30, 10, 0, 51),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Selected.ID != "direct" || decision.Reason != ReasonCostGuard {
		t.Fatalf("cost-guard decision = %+v", decision)
	}
	if rejectionFor(decision.Diagnostics, "relay-fast") != RejectionCostGuard {
		t.Fatalf("cost diagnostics = %+v", decision.Diagnostics)
	}
}

func TestPlannerDoesNotMisattributeUnrelatedCostGuard(t *testing.T) {
	now := time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC)
	planner, _ := NewPlanner(testConfig())
	decision, err := planner.Select(Request{
		ManagedSessionID: "managed-cost-attribution",
		CostBudget:       CostBudget{Known: true, MaxMicrounits: 50},
		Candidates: []Candidate{
			directCandidate(now, 30, 10, 0),
			relayCandidate(now, "relay-viable", 100, 20, 0, 40),
			relayCandidate(now, "relay-over-budget", 10, 0, 0, 51),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Selected.ID != "direct" || decision.Reason != ReasonInitialBest {
		t.Fatalf("cost attribution decision = %+v", decision)
	}
	if rejectionFor(decision.Diagnostics, "relay-over-budget") != RejectionCostGuard {
		t.Fatalf("cost diagnostics = %+v", decision.Diagnostics)
	}
}

func TestPlannerCandidateFailuresStayLocalAndMeshIsRejected(t *testing.T) {
	now := time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC)
	planner, _ := NewPlanner(testConfig())
	unhealthy := relayCandidate(now, "relay-unhealthy", 20, 0, 0, 10)
	unhealthy.Constraints.Healthy = false
	unreachable := directCandidate(now, 10, 0, 0)
	unreachable.Constraints.Reachable = false
	mesh := relayCandidate(now, "mesh", 10, 0, 0, 10)
	mesh.Path = cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH
	mesh.Quality.Series.ObservedPath = mesh.Path
	decision, err := planner.Select(Request{
		ManagedSessionID: "managed-local-failure",
		CostBudget:       CostBudget{Known: true, MaxMicrounits: 100},
		Candidates: []Candidate{
			unreachable,
			unhealthy,
			mesh,
			relayCandidate(now, "relay-healthy", 60, 30, 0, 20),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Selected.ID != "relay-healthy" || decision.Reason != ReasonOnlyViable {
		t.Fatalf("local-failure decision = %+v", decision)
	}
	if rejectionFor(decision.Diagnostics, "direct") != RejectionUnreachable ||
		rejectionFor(decision.Diagnostics, "relay-unhealthy") != RejectionUnhealthy ||
		rejectionFor(decision.Diagnostics, "mesh") != RejectionUnsupportedPath {
		t.Fatalf("local-failure diagnostics = %+v", decision.Diagnostics)
	}
}

func TestPlannerEnforcesMinimumHoldAndSwitchCooldown(t *testing.T) {
	now := time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC)
	config := testConfig()
	config.MinimumHold = 30 * time.Second
	config.SwitchCooldown = time.Minute
	planner, _ := NewPlanner(config)
	initial := Request{
		ManagedSessionID: "managed-hold",
		CostBudget:       CostBudget{Known: true, MaxMicrounits: 100},
		Candidates: []Candidate{
			directCandidate(now, 20, 0, 0),
			relayCandidate(now, "relay-eu", 100, 20, 0, 10),
		},
	}
	if _, err := planner.Select(initial, now); err != nil {
		t.Fatal(err)
	}
	preferRelay := initial
	preferRelay.Candidates = []Candidate{
		directCandidate(now.Add(10*time.Second), 500, 500, 1),
		relayCandidate(now.Add(10*time.Second), "relay-eu", 30, 0, 0, 10),
	}
	held, err := planner.Select(preferRelay, now.Add(10*time.Second))
	if err != nil || held.Reason != ReasonMinimumHold || held.Selected.ID != "direct" {
		t.Fatalf("minimum hold = (%+v, %v)", held, err)
	}
	switched, err := planner.Select(preferRelay, now.Add(31*time.Second))
	if err != nil || !switched.Changed || switched.Selected.ID != "relay-eu" {
		t.Fatalf("relay switch = (%+v, %v)", switched, err)
	}
	preferDirect := initial
	preferDirect.Candidates = []Candidate{
		directCandidate(now.Add(40*time.Second), 10, 0, 0),
		relayCandidate(now.Add(40*time.Second), "relay-eu", 500, 500, 1, 10),
	}
	cooling, err := planner.Select(preferDirect, now.Add(40*time.Second))
	if err != nil || cooling.Reason != ReasonCooldown || cooling.Selected.ID != "relay-eu" {
		t.Fatalf("cooldown = (%+v, %v)", cooling, err)
	}
}

func TestPlannerRejectsUnknownRelayCostAndBoundsSessionState(t *testing.T) {
	now := time.Date(2026, time.July, 11, 13, 0, 0, 0, time.UTC)
	config := testConfig()
	config.MaxSessions = 1
	config.StateTTL = time.Hour
	planner, _ := NewPlanner(config)
	unknownCost := relayCandidate(now, "relay-unknown", 10, 0, 0, 0)
	unknownCost.Cost = CostEstimate{State: CostUnknown}
	_, err := planner.Select(Request{
		ManagedSessionID: "managed-unknown",
		CostBudget:       CostBudget{Known: true, MaxMicrounits: 100},
		Candidates:       []Candidate{unknownCost},
	}, now)
	if !errors.Is(err, ErrNoViableRoute) {
		t.Fatalf("unknown cost error = %v", err)
	}
	if _, err := planner.Select(Request{
		ManagedSessionID: "managed-one", Candidates: []Candidate{directCandidate(now, 10, 0, 0)},
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := planner.Select(Request{
		ManagedSessionID: "managed-two", Candidates: []Candidate{directCandidate(now, 10, 0, 0)},
	}, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("state capacity error = %v", err)
	}
}

func testConfig() Config {
	config := DefaultConfig()
	config.MinimumHold = 0
	config.SwitchCooldown = 0
	config.RequiredConsecutiveWins = 1
	config.MinimumImprovementBasisPoints = 500
	return config
}

func directCandidate(now time.Time, p95 uint64, loss uint32, disconnects uint64) Candidate {
	return Candidate{
		ID: "direct", Path: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT,
		Quality:           qualityBaseline(now, cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, p95, loss, disconnects),
		QualityValidUntil: now.Add(5 * time.Minute),
		Cost:              CostEstimate{State: CostNone},
		Constraints:       CandidateConstraints{Reachable: true, PolicyAllowed: true},
	}
}

func relayCandidate(now time.Time, relayID string, p95 uint64, loss uint32, disconnects uint64, cost uint64) Candidate {
	return Candidate{
		ID: relayID, Path: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
		RelayID: relayID, Region: "eu-west",
		Quality:           qualityBaseline(now, cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, p95, loss, disconnects),
		QualityValidUntil: now.Add(5 * time.Minute),
		Cost:              CostEstimate{State: CostEstimated, EstimatedMicrounits: cost, ValidUntil: now.Add(5 * time.Minute)},
		Constraints: CandidateConstraints{
			Reachable: true, Healthy: true, CapacityAvailable: true, PolicyAllowed: true, Entitled: true,
		},
	}
}

func qualityBaseline(now time.Time, path cloudpb.ObservedPath, p95 uint64, loss uint32, disconnects uint64) quality.Baseline {
	return quality.Baseline{
		Series:      quality.SeriesKey{ObservedPath: path, NetworkClass: "wifi", Region: "eu-west"},
		WindowCount: 4, SampleCount: 16,
		LatestWindowEndedAt:    now,
		MeanWindowRTTP50Millis: p95 / 2, MeanWindowRTTP95Millis: p95, MeanWindowJitterMillis: p95 / 10,
		LossBasisPoints: loss, MeanThroughputBPS: 1_000_000,
		DisconnectCount: disconnects, ConnectedMillis: 240_000,
	}
}

func rejectionFor(diagnostics []CandidateDiagnostic, candidateID string) RejectionReason {
	for _, diagnostic := range diagnostics {
		if diagnostic.CandidateID == candidateID {
			return diagnostic.Rejection
		}
	}
	return ""
}
