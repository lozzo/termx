package pathquality

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
)

func TestCollectorBuildsRedactedQualityWindow(t *testing.T) {
	startedAt := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	collector, err := NewCollector(Metadata{
		ManagedSessionID: "managed-1",
		ObservedPath:     cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
		NetworkClass:     "WiFi",
		Region:           "EU-West",
		CarrierTag:       "carrier-a",
		ProviderTag:      "provider-a",
	}, Sample{
		At: startedAt, RoundTripTime: 40 * time.Millisecond,
		BytesSent: 1_000, BytesReceived: 2_000, PacketsSent: 100, LossEvents: 2, Connected: true,
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	observations := []Sample{
		{At: startedAt.Add(10 * time.Second), RoundTripTime: 60 * time.Millisecond, BytesSent: 3_000, BytesReceived: 4_000, PacketsSent: 200, LossEvents: 4, Connected: true},
		{At: startedAt.Add(20 * time.Second), RoundTripTime: 50 * time.Millisecond, BytesSent: 5_000, BytesReceived: 6_000, PacketsSent: 300, LossEvents: 6, Connected: false},
		{At: startedAt.Add(30 * time.Second), RoundTripTime: 100 * time.Millisecond, BytesSent: 7_000, BytesReceived: 8_000, PacketsSent: 400, LossEvents: 8, Connected: true},
	}
	for _, observation := range observations {
		if err := collector.Observe(observation); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	window, err := collector.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if window.RTTP50 != 50*time.Millisecond || window.RTTP95 != 100*time.Millisecond || window.Jitter != 26_666_666*time.Nanosecond {
		t.Fatalf("latency window = p50 %s p95 %s jitter %s", window.RTTP50, window.RTTP95, window.Jitter)
	}
	if window.PacketCount != 306 || window.LossEventCount != 6 || window.LossBasisPoints != 196 {
		t.Fatalf("loss window = packets %d losses %d basis points %d", window.PacketCount, window.LossEventCount, window.LossBasisPoints)
	}
	if window.ThroughputBPS != 3_200 || window.Connected != 20*time.Second || window.DisconnectCount != 1 || window.SampleCount != 4 {
		t.Fatalf("transport window = throughput %d connected %s disconnects %d samples %d", window.ThroughputBPS, window.Connected, window.DisconnectCount, window.SampleCount)
	}
	wire, err := window.Proto()
	if err != nil {
		t.Fatalf("Proto: %v", err)
	}
	decoded, err := Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.ManagedSessionID != "managed-1" || decoded.NetworkClass != "wifi" || decoded.Region != "eu-west" || decoded.CarrierTag != "carrier-a" || decoded.ProviderTag != "provider-a" {
		t.Fatalf("decoded metadata = %+v", decoded.Metadata)
	}
	if decoded.RTTP50 != window.RTTP50 || decoded.RTTP95 != window.RTTP95 || decoded.ThroughputBPS != window.ThroughputBPS {
		t.Fatalf("decoded window = %+v, want %+v", decoded, window)
	}
}

func TestCollectorRejectsCounterRollbackAndKeepsWindow(t *testing.T) {
	startedAt := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	collector, err := NewCollector(Metadata{ManagedSessionID: "managed-1", ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, NetworkClass: "ethernet"}, Sample{
		At: startedAt, RoundTripTime: time.Millisecond, BytesSent: 10, BytesReceived: 20, PacketsSent: 30, LossEvents: 1, Connected: true,
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	rollback := Sample{At: startedAt.Add(time.Second), RoundTripTime: time.Millisecond, BytesSent: 9, BytesReceived: 20, PacketsSent: 30, LossEvents: 1, Connected: true}
	if err := collector.Observe(rollback); !errors.Is(err, ErrCounterRollback) {
		t.Fatalf("Observe rollback error = %v", err)
	}
	valid := Sample{At: startedAt.Add(2 * time.Second), RoundTripTime: 2 * time.Millisecond, BytesSent: 20, BytesReceived: 30, PacketsSent: 40, LossEvents: 1, Connected: true}
	if err := collector.Observe(valid); err != nil {
		t.Fatalf("Observe after rollback: %v", err)
	}
	window, err := collector.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if window.StartedAt != startedAt || window.EndedAt != valid.At {
		t.Fatalf("window bounds = %s..%s", window.StartedAt, window.EndedAt)
	}
	next := Sample{At: startedAt.Add(3 * time.Second), RoundTripTime: 3 * time.Millisecond, BytesSent: 30, BytesReceived: 40, PacketsSent: 50, LossEvents: 2, Connected: true}
	if err := collector.Observe(next); err != nil {
		t.Fatalf("Observe next window: %v", err)
	}
	nextWindow, err := collector.Flush()
	if err != nil {
		t.Fatalf("Flush next window: %v", err)
	}
	if nextWindow.StartedAt != valid.At || nextWindow.EndedAt != next.At || nextWindow.PacketCount != 11 {
		t.Fatalf("next window = %+v", nextWindow)
	}
}

func TestMetadataRejectsRawAddressAndFreeText(t *testing.T) {
	base := Metadata{ManagedSessionID: "managed-1", ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, NetworkClass: "wifi"}
	base.Region = "192.0.2.10"
	if err := base.Validate(); err == nil {
		t.Fatal("raw IP region must be rejected")
	}
	base.Region = "Europe West"
	if err := base.Validate(); err == nil {
		t.Fatal("free-text region must be rejected")
	}
}

func TestLossRatioDoesNotOverflowAtLargeCounters(t *testing.T) {
	if got := ratioBasisPoints(math.MaxUint64/2, math.MaxUint64); got != 5_000 {
		t.Fatalf("large loss ratio = %d, want 5000", got)
	}
}
