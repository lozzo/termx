package pathquality

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

type contractFixture struct {
	SchemaVersion      int                      `json:"schema_version"`
	SummaryFields      []string                 `json:"summary_fields"`
	ForbiddenFragments []string                 `json:"forbidden_fragments"`
	Sample             pathQualityFixtureSample `json:"sample"`
}

type pathQualityFixtureSample struct {
	ManagedSessionID          string `json:"managed_session_id"`
	ObservedPath              string `json:"observed_path"`
	RTTP50Millis              uint32 `json:"rtt_p50_millis"`
	JitterMillis              uint32 `json:"jitter_millis"`
	LossBasisPoints           uint32 `json:"loss_basis_points"`
	ThroughputBPS             uint64 `json:"throughput_bps"`
	ConnectedMillis           uint64 `json:"connected_millis"`
	NetworkClass              string `json:"network_class"`
	Region                    string `json:"region"`
	RTTP95Millis              uint32 `json:"rtt_p95_millis"`
	SampleCount               uint32 `json:"sample_count"`
	DisconnectCount           uint32 `json:"disconnect_count"`
	WindowStartedAtUnixMillis uint64 `json:"window_started_at_unix_millis"`
	WindowEndedAtUnixMillis   uint64 `json:"window_ended_at_unix_millis"`
	PacketCount               uint64 `json:"packet_count"`
	LossEventCount            uint64 `json:"loss_event_count"`
	CarrierTag                string `json:"carrier_tag"`
	ProviderTag               string `json:"provider_tag"`
}

func TestSharedPathQualityContractMatchesGoWireAndValidation(t *testing.T) {
	payload, err := os.ReadFile("../testdata/path_quality_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture contractFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 2 {
		t.Fatalf("schema version = %d, want 2", fixture.SchemaVersion)
	}
	descriptor := (&cloudpb.PathQualitySummary{}).ProtoReflect().Descriptor()
	fields := make([]string, 0, descriptor.Fields().Len())
	for index := 0; index < descriptor.Fields().Len(); index++ {
		fields = append(fields, string(descriptor.Fields().Get(index).Name()))
	}
	if !reflect.DeepEqual(fields, fixture.SummaryFields) {
		t.Fatalf("Go path quality fields = %#v, want %#v", fields, fixture.SummaryFields)
	}
	for _, field := range fields {
		for _, fragment := range fixture.ForbiddenFragments {
			if strings.Contains(field, fragment) {
				t.Fatalf("public quality field %q contains forbidden fragment %q", field, fragment)
			}
		}
	}
	sample := fixture.Sample
	path := cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED
	if sample.ObservedPath == "single_relay" {
		path = cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
	}
	_, err = Decode(&cloudpb.PathQualitySummary{
		ManagedSessionId: sample.ManagedSessionID, ObservedPath: path,
		RttP50Millis: sample.RTTP50Millis, JitterMillis: sample.JitterMillis,
		LossBasisPoints: sample.LossBasisPoints, ThroughputBps: sample.ThroughputBPS,
		ConnectedMillis: sample.ConnectedMillis, NetworkClass: sample.NetworkClass, Region: sample.Region,
		RttP95Millis: sample.RTTP95Millis, SampleCount: sample.SampleCount, DisconnectCount: sample.DisconnectCount,
		WindowStartedAtUnixMillis: sample.WindowStartedAtUnixMillis, WindowEndedAtUnixMillis: sample.WindowEndedAtUnixMillis,
		PacketCount: sample.PacketCount, LossEventCount: sample.LossEventCount,
		CarrierTag: sample.CarrierTag, ProviderTag: sample.ProviderTag,
	})
	if err != nil {
		t.Fatalf("shared sample is invalid in Go: %v", err)
	}
}
