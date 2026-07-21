package managed

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion/pathquality"
)

const (
	defaultQualitySampleInterval = 5 * time.Second
	defaultQualityWindow         = time.Minute
	qualityReportTimeout         = 2 * time.Second
)

// QualityObservationOptions 控制单个 managed session 的匿名网络质量观测。
// 分类字段只能使用 taxonomy tag，禁止包含 IP、hostname、endpoint label、terminal identity 或 credential reference。
type QualityObservationOptions struct {
	Enabled        bool
	SampleInterval time.Duration
	Window         time.Duration
	NetworkClass   string
	Region         string
	CarrierTag     string
	ProviderTag    string
}

type qualityReporter struct {
	cloud            CloudClient
	managedSessionID string
	options          QualityObservationOptions
	startedAt        time.Time
	collector        *pathquality.Collector
	pairID           string
	path             cloudpb.ObservedPath
	windowStarted    time.Time
}

func normalizeQualityObservationOptions(options QualityObservationOptions) (QualityObservationOptions, error) {
	if !options.Enabled {
		return options, nil
	}
	if options.SampleInterval == 0 {
		options.SampleInterval = defaultQualitySampleInterval
	}
	if options.Window == 0 {
		options.Window = defaultQualityWindow
	}
	if options.SampleInterval <= 0 || options.Window < options.SampleInterval {
		return QualityObservationOptions{}, fmt.Errorf("invalid managed WebRTC quality observation intervals")
	}
	if strings.TrimSpace(options.NetworkClass) == "" {
		options.NetworkClass = "unknown"
	}
	metadata := pathquality.Metadata{
		ManagedSessionID: "validation", ObservedPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT,
		NetworkClass: options.NetworkClass, Region: options.Region, CarrierTag: options.CarrierTag, ProviderTag: options.ProviderTag,
	}
	if err := metadata.Validate(); err != nil {
		return QualityObservationOptions{}, fmt.Errorf("invalid managed WebRTC quality metadata: %w", err)
	}
	return options, nil
}

func (reporter *qualityReporter) run(peer port.ManagedPeer, protocolDone, closeRequested <-chan struct{}) {
	ticker := time.NewTicker(reporter.options.SampleInterval)
	defer ticker.Stop()
	reporter.observe(peer, time.Now().UTC())
	for {
		select {
		case sampledAt := <-ticker.C:
			reporter.observe(peer, sampledAt.UTC())
		case <-protocolDone:
			reporter.finish(peer)
			return
		case <-closeRequested:
			reporter.finish(peer)
			return
		}
	}
}

func (reporter *qualityReporter) finish(peer port.ManagedPeer) {
	reporter.observe(peer, time.Now().UTC())
	reporter.flush()
	if snapshot, ok := peer.Snapshot(time.Now().UTC()); ok {
		reporter.reportOutcome(snapshot)
	}
}

func (reporter *qualityReporter) observe(peer port.ManagedPeer, sampledAt time.Time) {
	snapshot, ok := peer.Snapshot(sampledAt)
	if !ok {
		return
	}
	path := observedPathToProto(snapshot.Path)
	if path == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED {
		return
	}
	sample := pathquality.Sample{
		At: snapshot.At, RoundTripTime: snapshot.RoundTrip, BytesSent: snapshot.BytesSent,
		BytesReceived: snapshot.BytesRecv, PacketsSent: snapshot.PacketsSent, LossEvents: snapshot.LossEvents, Connected: snapshot.Connected,
	}
	if reporter.collector == nil || reporter.pairID != snapshot.PairID || reporter.path != path {
		reporter.flush()
		reporter.startWindow(snapshot, path, sample)
		return
	}
	if err := reporter.collector.Observe(sample); err != nil {
		reporter.flush()
		reporter.startWindow(snapshot, path, sample)
		return
	}
	if !reporter.windowStarted.IsZero() && sampledAt.Sub(reporter.windowStarted) >= reporter.options.Window {
		reporter.flush()
	}
}

func (reporter *qualityReporter) startWindow(snapshot port.ManagedPeerSnapshot, path cloudpb.ObservedPath, sample pathquality.Sample) {
	networkClass := strings.TrimSpace(reporter.options.NetworkClass)
	if networkClass == "" || networkClass == "unknown" && strings.TrimSpace(snapshot.NetworkClass) != "" {
		networkClass = strings.TrimSpace(snapshot.NetworkClass)
	}
	collector, err := pathquality.NewCollector(pathquality.Metadata{
		ManagedSessionID: reporter.managedSessionID, ObservedPath: path, NetworkClass: networkClass,
		Region: reporter.options.Region, CarrierTag: reporter.options.CarrierTag, ProviderTag: reporter.options.ProviderTag,
	}, sample)
	if err != nil {
		return
	}
	reporter.collector = collector
	reporter.pairID = snapshot.PairID
	reporter.path = path
	reporter.windowStarted = sample.At
}

func (reporter *qualityReporter) flush() {
	if reporter.collector == nil {
		return
	}
	window, err := reporter.collector.Flush()
	if err != nil {
		return
	}
	reporter.windowStarted = window.EndedAt
	summary, err := window.Proto()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), qualityReportTimeout)
	defer cancel()
	_, _ = reporter.cloud.ReportPathQuality(ctx, &cloudpb.ReportPathQualityRequest{Summary: summary})
}

func (reporter *qualityReporter) reportOutcome(snapshot port.ManagedPeerSnapshot) {
	path := observedPathToProto(snapshot.Path)
	if path == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED {
		return
	}
	errorCode := cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED
	if !snapshot.Connected {
		errorCode = cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE
	}
	connected := time.Since(reporter.startedAt)
	if connected < 0 {
		connected = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), qualityReportTimeout)
	defer cancel()
	_, _ = reporter.cloud.ReportConnectionOutcome(ctx, &cloudpb.ReportConnectionOutcomeRequest{Outcome: &cloudpb.ConnectionOutcome{
		ManagedSessionId: reporter.managedSessionID, ObservedPath: path, ErrorCode: errorCode, ConnectedMillis: uint64(connected.Milliseconds()),
	}})
}

func observedPathToProto(path endpoint.Path) cloudpb.ObservedPath {
	switch path {
	case endpoint.PathDirect:
		return cloudpb.ObservedPath_OBSERVED_PATH_DIRECT
	case endpoint.PathSingleRelay:
		return cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
	default:
		return cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED
	}
}
