package quality

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
)

func TestAggregatorKeepsQualityAndTrustedCostSourcesSeparate(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	aggregator, err := NewAggregator(Config{MaxSeries: 4, MaxWindowsPerSeries: 4, MaxWindowAge: time.Hour})
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	request := qualityRequest(now.Add(-time.Minute), "managed-1", cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, 40, 80, 100, 2)
	cost, err := (CostRateCard{MicrounitsPerGiB: 1_000, MicrounitsPerMinute: 49}).Summarize(CostSourceRelayUsage, 12_000, 30_000)
	if err != nil {
		t.Fatalf("Summarize cost: %v", err)
	}
	result, err := aggregator.Ingest(request, now)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.Duplicate || result.Series.ObservedPath != cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY {
		t.Fatalf("ingest result = %+v", result)
	}
	duplicate, err := aggregator.Ingest(request, now)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate ingest = (%+v, %v)", duplicate, err)
	}
	pending, err := aggregator.Baseline(result.Series, now)
	if err != nil {
		t.Fatalf("pending Baseline: %v", err)
	}
	if pending.UnpricedWindowCount != 1 || pending.CostWindowCount != 0 || pending.EstimatedCostMicrounits != 0 {
		t.Fatalf("pending cost baseline = %+v", pending)
	}
	costDuplicate, err := aggregator.AttachCost(result.Observation, cost)
	if err != nil || costDuplicate {
		t.Fatalf("AttachCost = (%v, %v)", costDuplicate, err)
	}
	costDuplicate, err = aggregator.AttachCost(result.Observation, cost)
	if err != nil || !costDuplicate {
		t.Fatalf("duplicate AttachCost = (%v, %v)", costDuplicate, err)
	}
	conflictCost := cost
	conflictCost.EstimatedMicrounits++
	if _, err := aggregator.AttachCost(result.Observation, conflictCost); !errors.Is(err, ErrDuplicateConflict) {
		t.Fatalf("conflicting cost error = %v", err)
	}
	baseline, err := aggregator.Baseline(result.Series, now)
	if err != nil {
		t.Fatalf("Baseline: %v", err)
	}
	if baseline.WindowCount != 1 || baseline.SampleCount != 4 || !baseline.LatestWindowEndedAt.Equal(now.Add(-30*time.Second)) ||
		baseline.MeanWindowRTTP50Millis != 40 || baseline.MeanWindowRTTP95Millis != 80 {
		t.Fatalf("quality baseline = %+v", baseline)
	}
	if baseline.DisconnectCount != 2 || baseline.EstimatedCostMicrounits != 26 || baseline.BillableBytes != 12_000 || baseline.CostActiveMillis != 30_000 || baseline.CostWindowCount != 1 || baseline.UnpricedWindowCount != 0 {
		t.Fatalf("cost/disconnect baseline = %+v", baseline)
	}
}

func TestBaselineRejectsStaleWindows(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	aggregator, err := NewAggregator(Config{MaxSeries: 1, MaxWindowsPerSeries: 2, MaxWindowAge: time.Hour})
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	result, err := aggregator.Ingest(qualityRequest(now.Add(-30*time.Minute), "managed-1", cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, 10, 20, 0, 0), now)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if _, err := aggregator.Baseline(result.Series, now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale Baseline error = %v", err)
	}
}

func TestAggregatorIsBoundedAndDoesNotCreateRouteActions(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	aggregator, err := NewAggregator(Config{MaxSeries: 1, MaxWindowsPerSeries: 2, MaxWindowAge: time.Hour})
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	var key SeriesKey
	for index := 0; index < 3; index++ {
		request := qualityRequest(now.Add(time.Duration(index-3)*time.Minute), "managed-"+string(rune('a'+index)), cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, uint32(10+index), uint32(20+index), 0, 0)
		result, ingestErr := aggregator.Ingest(request, now)
		if ingestErr != nil {
			t.Fatalf("Ingest %d: %v", index, ingestErr)
		}
		key = result.Series
	}
	records, err := aggregator.Windows(key)
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	if len(records) != 2 || records[0].Window.ManagedSessionID != "managed-b" || records[1].Window.ManagedSessionID != "managed-c" {
		t.Fatalf("bounded records = %+v", records)
	}
	other := qualityRequest(now.Add(-time.Minute), "managed-other", cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, 20, 30, 0, 0)
	if _, err := aggregator.Ingest(other, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("second series error = %v", err)
	}
}

func TestAggregatorRejectsPublicCostAndOutOfRangeWindowShapes(t *testing.T) {
	now := time.Date(2026, time.July, 11, 12, 5, 0, 0, time.UTC)
	aggregator, err := NewAggregator(Config{MaxSeries: 1, MaxWindowsPerSeries: 1, MaxWindowAge: time.Hour})
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	request := qualityRequest(now.Add(-time.Minute), "managed-1", cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, 10, 20, 0, 0)
	invalidCost := CostSummary{Source: CostSourceNone, EstimatedMicrounits: 1}
	result, err := aggregator.Ingest(request, now)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if _, err := aggregator.AttachCost(result.Observation, invalidCost); err == nil {
		t.Fatal("cost values without a trusted source must fail")
	}
	request.Summary.WindowEndedAtUnixMillis = uint64(now.Add(time.Minute).UnixMilli())
	request.Summary.ManagedSessionId = "managed-future"
	if _, err := aggregator.Ingest(request, now); err == nil {
		t.Fatal("future quality window must fail")
	}
}

func TestCostRateCardUsesWideArithmetic(t *testing.T) {
	summary, err := (CostRateCard{MicrounitsPerGiB: 2}).Summarize(CostSourceRouteEstimate, math.MaxUint64, 0)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.EstimatedMicrounits != 1<<35 {
		t.Fatalf("wide byte cost = %d, want %d", summary.EstimatedMicrounits, uint64(1<<35))
	}
}

func qualityRequest(startedAt time.Time, managedSessionID string, path cloudpb.ObservedPath, p50, p95, loss uint32, disconnects uint32) *cloudpb.ReportPathQualityRequest {
	endedAt := startedAt.Add(30 * time.Second)
	return &cloudpb.ReportPathQualityRequest{Summary: &cloudpb.PathQualitySummary{
		ManagedSessionId: managedSessionID, ObservedPath: path,
		RttP50Millis: p50, RttP95Millis: p95, JitterMillis: 5,
		LossBasisPoints: loss, ThroughputBps: 8_000, ConnectedMillis: 30_000,
		NetworkClass: "wifi", Region: "eu-west", CarrierTag: "carrier-a", ProviderTag: "provider-a",
		SampleCount: 4, DisconnectCount: disconnects,
		WindowStartedAtUnixMillis: uint64(startedAt.UnixMilli()), WindowEndedAtUnixMillis: uint64(endedAt.UnixMilli()),
		PacketCount: 1_000, LossEventCount: uint64(loss) / 10,
	}}
}
