package client

import (
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	pion "github.com/pion/webrtc/v4"
)

func TestQualityObservationReadsDirectCandidateStatsWithoutAddresses(t *testing.T) {
	report := pion.StatsReport{
		"pair": pion.ICECandidatePairStats{
			ID: "pair", LocalCandidateID: "local", RemoteCandidateID: "remote",
			State: pion.StatsICECandidatePairStateSucceeded, Nominated: true,
			CurrentRoundTripTime: 0.042, BytesSent: 1_000, BytesReceived: 2_000,
			PacketsSent: 100, RetransmissionsSent: 2, PacketsDiscardedOnSend: 1,
		},
		"local":  pion.ICECandidateStats{ID: "local", CandidateType: pion.ICECandidateTypeHost, NetworkType: "wifi", IP: "192.0.2.1", Port: 5000},
		"remote": pion.ICECandidateStats{ID: "remote", CandidateType: pion.ICECandidateTypeSrflx, IP: "198.51.100.1", Port: 6000},
	}
	sampledAt := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	observation, ok := qualityObservationFromStats(report, sampledAt, true)
	if !ok {
		t.Fatal("quality observation was not produced")
	}
	if observation.path != cloudpb.ObservedPath_OBSERVED_PATH_DIRECT || observation.networkClass != "wifi" || observation.sample.RoundTripTime != 42*time.Millisecond {
		t.Fatalf("observation = %+v", observation)
	}
	if observation.sample.BytesSent != 1_000 || observation.sample.BytesReceived != 2_000 || observation.sample.PacketsSent != 100 || observation.sample.LossEvents != 3 {
		t.Fatalf("sample counters = %+v", observation.sample)
	}
}

func TestQualityObservationProjectsRelayWithoutRouteAction(t *testing.T) {
	report := pion.StatsReport{
		"pair":   pion.ICECandidatePairStats{ID: "pair", LocalCandidateID: "local", RemoteCandidateID: "remote", State: pion.StatsICECandidatePairStateSucceeded, Nominated: true},
		"local":  pion.ICECandidateStats{ID: "local", CandidateType: pion.ICECandidateTypeRelay},
		"remote": pion.ICECandidateStats{ID: "remote", CandidateType: pion.ICECandidateTypeHost},
	}
	observation, ok := qualityObservationFromStats(report, time.Now(), true)
	if !ok || observation.path != cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY {
		t.Fatalf("relay observation = (%+v, %v)", observation, ok)
	}
}

func TestQualityObservationOptionsRejectInvalidWindow(t *testing.T) {
	_, err := normalizeQualityObservationOptions(QualityObservationOptions{Enabled: true, SampleInterval: time.Second, Window: time.Millisecond})
	if err == nil {
		t.Fatal("quality window shorter than sample interval must fail")
	}
}
