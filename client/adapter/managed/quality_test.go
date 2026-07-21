package managed

import (
	"context"
	"testing"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestQualityReporterUsesPortablePeerSnapshot(t *testing.T) {
	cloud := &cloudcompanion.FakeClient{
		ReportPathQualityFunc: func(_ context.Context, _ *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
			return &cloudpb.ReportPathQualityResponse{}, nil
		},
		ReportConnectionOutcomeFunc: func(_ context.Context, _ *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
			return &cloudpb.ReportConnectionOutcomeResponse{}, nil
		},
	}
	now := time.Now().UTC()
	peer := &fakeManagedPeer{snapshots: []port.ManagedPeerSnapshot{
		{PairID: "pair-1", Path: endpoint.PathDirect, At: now, RoundTrip: 10 * time.Millisecond, BytesSent: 10, BytesRecv: 20, PacketsSent: 2, Connected: true},
		{PairID: "pair-1", Path: endpoint.PathDirect, At: now.Add(time.Second), RoundTrip: 12 * time.Millisecond, BytesSent: 30, BytesRecv: 50, PacketsSent: 4, LossEvents: 1, Connected: true},
		{PairID: "pair-1", Path: endpoint.PathDirect, At: now.Add(2 * time.Second), Connected: true},
	}}
	reporter := &qualityReporter{
		cloud: cloud, managedSessionID: "managed-1", startedAt: now,
		options: QualityObservationOptions{Enabled: true, SampleInterval: time.Second, Window: time.Minute, NetworkClass: "test"},
	}
	reporter.observe(peer, now)
	reporter.observe(peer, now.Add(time.Second))
	reporter.flush()
	reporter.reportOutcome(peer.snapshots[2])
	recorded := cloud.Requests()
	if len(recorded.ReportPathQuality) != 1 || len(recorded.ReportConnectionOutcome) != 1 {
		t.Fatalf("quality requests = %+v", recorded)
	}
	if recorded.ReportConnectionOutcome[0].GetOutcome().GetObservedPath() != cloudpb.ObservedPath_OBSERVED_PATH_DIRECT {
		t.Fatalf("outcome = %+v", recorded.ReportConnectionOutcome[0])
	}
}
