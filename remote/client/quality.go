package client

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion/pathquality"
	pion "github.com/pion/webrtc/v4"
)

const (
	defaultQualitySampleInterval = 5 * time.Second
	defaultQualityWindow         = time.Minute
	qualityReportTimeout         = 2 * time.Second
)

// QualityObservationOptions 控制单个 managed WebRTC session 的被动质量观测。
// Enabled 必须显式开启；零采样周期使用安全默认值。分类字段只能是匿名 taxonomy tag，
// 不能填写 IP、hostname、endpoint label、terminal identity 或 credential reference。
type QualityObservationOptions struct {
	Enabled        bool
	SampleInterval time.Duration
	Window         time.Duration
	NetworkClass   string
	Region         string
	CarrierTag     string
	ProviderTag    string
}

type peerQualityObservation struct {
	pairID       string
	path         cloudpb.ObservedPath
	networkClass string
	sample       pathquality.Sample
}

type qualityReporter struct {
	companion interface {
		ReportPathQuality(context.Context, *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error)
		ReportConnectionOutcome(context.Context, *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error)
	}
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
		ManagedSessionID: "validation",
		ObservedPath:     cloudpb.ObservedPath_OBSERVED_PATH_DIRECT,
		NetworkClass:     options.NetworkClass,
		Region:           options.Region,
		CarrierTag:       options.CarrierTag,
		ProviderTag:      options.ProviderTag,
	}
	if err := metadata.Validate(); err != nil {
		return QualityObservationOptions{}, fmt.Errorf("invalid managed WebRTC quality metadata: %w", err)
	}
	return options, nil
}

func (reporter *qualityReporter) run(peer *pion.PeerConnection, transportDone, closeRequested <-chan struct{}) {
	ticker := time.NewTicker(reporter.options.SampleInterval)
	defer ticker.Stop()
	reporter.observe(peer, time.Now().UTC(), true)
	for {
		select {
		case sampledAt := <-ticker.C:
			reporter.observe(peer, sampledAt.UTC(), false)
		case <-transportDone:
			reporter.finish(peer)
			return
		case <-closeRequested:
			reporter.finish(peer)
			return
		}
	}
}

func (reporter *qualityReporter) finish(peer *pion.PeerConnection) {
	reporter.observe(peer, time.Now().UTC(), true)
	reporter.flush()
	reporter.reportOutcome(peer)
}

func (reporter *qualityReporter) observe(peer *pion.PeerConnection, sampledAt time.Time, final bool) {
	state := peer.ConnectionState()
	connected := state != pion.PeerConnectionStateDisconnected && state != pion.PeerConnectionStateFailed
	if !final && state != pion.PeerConnectionStateConnected {
		connected = false
	}
	observation, ok := qualityObservationFromStats(peer.GetStats(), sampledAt, connected)
	if !ok {
		return
	}
	if reporter.collector == nil || reporter.pairID != observation.pairID || reporter.path != observation.path {
		reporter.flush()
		reporter.startWindow(observation)
		return
	}
	if err := reporter.collector.Observe(observation.sample); err != nil {
		// Candidate counters 回退表示 stats truth 已换代；旧窗口可上报则先上报，再从当前累计值建立新基线。
		reporter.flush()
		reporter.startWindow(observation)
		return
	}
	if !reporter.windowStarted.IsZero() && sampledAt.Sub(reporter.windowStarted) >= reporter.options.Window {
		reporter.flush()
	}
}

func (reporter *qualityReporter) startWindow(observation peerQualityObservation) {
	networkClass := strings.TrimSpace(reporter.options.NetworkClass)
	if networkClass == "" || networkClass == "unknown" && observation.networkClass != "" {
		networkClass = observation.networkClass
	}
	collector, err := pathquality.NewCollector(pathquality.Metadata{
		ManagedSessionID: reporter.managedSessionID,
		ObservedPath:     observation.path,
		NetworkClass:     networkClass,
		Region:           reporter.options.Region,
		CarrierTag:       reporter.options.CarrierTag,
		ProviderTag:      reporter.options.ProviderTag,
	}, observation.sample)
	if err != nil {
		return
	}
	reporter.collector = collector
	reporter.pairID = observation.pairID
	reporter.path = observation.path
	reporter.windowStarted = observation.sample.At
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
	// Telemetry 是非授权 side proof；失败只丢弃当前窗口，禁止改变 ICE policy、请求 lease 或重连。
	_, _ = reporter.companion.ReportPathQuality(ctx, &cloudpb.ReportPathQualityRequest{Summary: summary})
}

func (reporter *qualityReporter) reportOutcome(peer *pion.PeerConnection) {
	if reporter.path == cloudpb.ObservedPath_OBSERVED_PATH_UNSPECIFIED {
		return
	}
	errorCode := cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED
	state := peer.ConnectionState()
	if state == pion.PeerConnectionStateDisconnected || state == pion.PeerConnectionStateFailed {
		errorCode = cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE
	}
	connected := time.Since(reporter.startedAt)
	if connected < 0 {
		connected = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), qualityReportTimeout)
	defer cancel()
	_, _ = reporter.companion.ReportConnectionOutcome(ctx, &cloudpb.ReportConnectionOutcomeRequest{Outcome: &cloudpb.ConnectionOutcome{
		ManagedSessionId: reporter.managedSessionID,
		ObservedPath:     reporter.path,
		ErrorCode:        errorCode,
		ConnectedMillis:  uint64(connected.Milliseconds()),
	}})
}

func qualityObservationFromStats(report pion.StatsReport, sampledAt time.Time, connected bool) (peerQualityObservation, bool) {
	pair, ok := nominatedCandidatePair(report)
	if !ok {
		return peerQualityObservation{}, false
	}
	local, localOK := report[pair.LocalCandidateID].(pion.ICECandidateStats)
	remote, remoteOK := report[pair.RemoteCandidateID].(pion.ICECandidateStats)
	if !localOK || !remoteOK {
		return peerQualityObservation{}, false
	}
	path := cloudpb.ObservedPath_OBSERVED_PATH_DIRECT
	if local.CandidateType == pion.ICECandidateTypeRelay || remote.CandidateType == pion.ICECandidateTypeRelay {
		path = cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY
	}
	rtt := secondsDuration(pair.CurrentRoundTripTime)
	if rtt == 0 {
		for _, stat := range report {
			if sctp, isSCTP := stat.(pion.SCTPTransportStats); isSCTP {
				rtt = secondsDuration(sctp.SmoothedRoundTripTime)
				break
			}
		}
	}
	lossEvents := saturatingQualityAdd(pair.RetransmissionsSent, uint64(pair.PacketsDiscardedOnSend))
	networkClass := strings.ToLower(strings.TrimSpace(local.NetworkType))
	metadata := pathquality.Metadata{ManagedSessionID: "validation", ObservedPath: path, NetworkClass: networkClass}
	if metadata.Validate() != nil {
		networkClass = "unknown"
	}
	return peerQualityObservation{
		pairID: pair.ID, path: path, networkClass: networkClass,
		sample: pathquality.Sample{
			At: sampledAt, RoundTripTime: rtt,
			BytesSent: pair.BytesSent, BytesReceived: pair.BytesReceived,
			PacketsSent: uint64(pair.PacketsSent), LossEvents: lossEvents, Connected: connected,
		},
	}, true
}

func nominatedCandidatePair(report pion.StatsReport) (pion.ICECandidatePairStats, bool) {
	ids := make([]string, 0)
	for id, stat := range report {
		pair, ok := stat.(pion.ICECandidatePairStats)
		if ok && pair.Nominated && pair.State == pion.StatsICECandidatePairStateSucceeded {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return pion.ICECandidatePairStats{}, false
	}
	sort.Strings(ids)
	pair, ok := report[ids[0]].(pion.ICECandidatePairStats)
	if ok && pair.ID == "" {
		pair.ID = ids[0]
	}
	return pair, ok
}

func secondsDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	if seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds * float64(time.Second))
}

func saturatingQualityAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}
