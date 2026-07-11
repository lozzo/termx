// Package pathquality 把公开 WebRTC transport stats 聚合成可上报的脱敏质量窗口。
//
// 本包不读取 terminal、CapabilityGrant、SDP、IP 地址或 payload；调用方只能提供累计网络计数
// 和经过分类的 region/carrier/provider tag。成本属于私有服务真值，不进入本包或公开 wire。
package pathquality

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
)

var (
	// ErrInsufficientSamples 表示窗口没有至少两个按时间递增的网络样本。
	// 调用方应继续采样，不能把空值或单点值伪装成可用于后续选路的质量窗口。
	ErrInsufficientSamples = errors.New("path quality window requires at least two samples")
	// ErrCounterRollback 表示同一观测窗口内的累计计数发生回退。
	// 这通常意味着 ICE candidate pair 已变化；调用方应结束旧窗口并为新路径重新建立 collector。
	ErrCounterRollback = errors.New("path quality cumulative counter rolled back")
)

// Metadata 固定一个质量窗口允许携带的匿名网络分类。
// ManagedSessionID 仅用于短期关联同一 managed connection；其余字段必须来自受控分类表，
// 不能填写 IP、hostname、endpoint label、terminal identity 或 credential reference。
type Metadata struct {
	ManagedSessionID string
	ObservedPath     cloudpb.ObservedPath
	NetworkClass     string
	Region           string
	CarrierTag       string
	ProviderTag      string
}

// Validate 校验质量窗口的关联键和匿名分类。
// 未知路径、空 network class、原始 IP 或不受限自由文本会被拒绝，避免 telemetry 形成旁路资产清单。
func (metadata Metadata) Validate() error {
	if err := validateCorrelationID(metadata.ManagedSessionID); err != nil {
		return err
	}
	if !validObservedPath(metadata.ObservedPath) {
		return fmt.Errorf("invalid observed path %s", metadata.ObservedPath)
	}
	if err := validateTag("network class", metadata.NetworkClass, true); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"region":       metadata.Region,
		"carrier tag":  metadata.CarrierTag,
		"provider tag": metadata.ProviderTag,
	} {
		if err := validateTag(name, value, false); err != nil {
			return err
		}
	}
	return nil
}

// Sample 是同一 WebRTC candidate pair 在某个时刻的累计网络计数。
// RoundTripTime 来自 ICE/SCTP stats；Bytes、Packets 和 LossEvents 必须保持单调。
// LossEvents 是 ICE 重传与本地发送丢弃等可观测丢失信号，不表示 DataChannel payload 内容。
type Sample struct {
	At            time.Time
	RoundTripTime time.Duration
	BytesSent     uint64
	BytesReceived uint64
	PacketsSent   uint64
	LossEvents    uint64
	Connected     bool
}

// Window 是一个已经完成且可验证的网络质量窗口。
// 它保存聚合后的 RTT、抖动、丢失、吞吐和断线统计，不保存原始逐包数据；
// private Probe Aggregator 可以把它与独立的可信成本 summary 关联，但不得反向补入公开字段。
type Window struct {
	Metadata
	StartedAt       time.Time
	EndedAt         time.Time
	RTTP50          time.Duration
	RTTP95          time.Duration
	Jitter          time.Duration
	LossBasisPoints uint32
	ThroughputBPS   uint64
	Connected       time.Duration
	SampleCount     uint32
	DisconnectCount uint32
	PacketCount     uint64
	LossEventCount  uint64
}

// Validate 校验聚合窗口的时间、计数和分位数关系。
// 失败窗口不得发送给 Companion，也不得进入私有质量基线或后续 SmartRoute 输入。
func (window Window) Validate() error {
	if err := window.Metadata.Validate(); err != nil {
		return err
	}
	if window.StartedAt.IsZero() || window.EndedAt.IsZero() || !window.StartedAt.Before(window.EndedAt) {
		return fmt.Errorf("invalid path quality window bounds")
	}
	if window.StartedAt.UnixMilli() <= 0 || window.EndedAt.UnixMilli() <= window.StartedAt.UnixMilli() {
		return fmt.Errorf("path quality window must be after the Unix epoch")
	}
	if window.SampleCount < 2 {
		return ErrInsufficientSamples
	}
	if window.DisconnectCount > window.SampleCount-1 {
		return fmt.Errorf("invalid disconnect count")
	}
	if window.RTTP50 < 0 || window.RTTP95 < window.RTTP50 || window.Jitter < 0 {
		return fmt.Errorf("invalid path quality latency summary")
	}
	if window.LossBasisPoints > 10_000 || window.LossEventCount > window.PacketCount {
		return fmt.Errorf("invalid path quality loss summary")
	}
	if ratioBasisPoints(window.LossEventCount, window.PacketCount) != window.LossBasisPoints {
		return fmt.Errorf("path quality loss ratio does not match counters")
	}
	if window.Connected < 0 || window.Connected > window.EndedAt.Sub(window.StartedAt) {
		return fmt.Errorf("invalid connected duration")
	}
	return nil
}

// Proto 投影公开 Cloud Companion wire message。
// 返回值只包含 Validate 允许的字段；成本、终端数据和授权凭据没有可写入口。
func (window Window) Proto() (*cloudpb.PathQualitySummary, error) {
	if err := window.Validate(); err != nil {
		return nil, err
	}
	return &cloudpb.PathQualitySummary{
		ManagedSessionId:          strings.TrimSpace(window.ManagedSessionID),
		ObservedPath:              window.ObservedPath,
		RttP50Millis:              durationMillis32(window.RTTP50),
		JitterMillis:              durationMillis32(window.Jitter),
		LossBasisPoints:           window.LossBasisPoints,
		ThroughputBps:             window.ThroughputBPS,
		ConnectedMillis:           durationMillis64(window.Connected),
		NetworkClass:              normalizeTag(window.NetworkClass),
		Region:                    normalizeTag(window.Region),
		RttP95Millis:              durationMillis32(window.RTTP95),
		SampleCount:               window.SampleCount,
		DisconnectCount:           window.DisconnectCount,
		WindowStartedAtUnixMillis: uint64(window.StartedAt.UTC().UnixMilli()),
		WindowEndedAtUnixMillis:   uint64(window.EndedAt.UTC().UnixMilli()),
		PacketCount:               window.PacketCount,
		LossEventCount:            window.LossEventCount,
		CarrierTag:                normalizeTag(window.CarrierTag),
		ProviderTag:               normalizeTag(window.ProviderTag),
	}, nil
}

// Decode 校验并还原公开 wire quality summary。
// Companion 与私有 Probe Aggregator 应共用该入口，避免各自维护一套宽松字段规则。
func Decode(summary *cloudpb.PathQualitySummary) (Window, error) {
	if summary == nil || summary.GetWindowStartedAtUnixMillis() == 0 || summary.GetWindowEndedAtUnixMillis() == 0 || summary.GetWindowStartedAtUnixMillis() > math.MaxInt64 || summary.GetWindowEndedAtUnixMillis() > math.MaxInt64 {
		return Window{}, fmt.Errorf("invalid path quality summary")
	}
	window := Window{
		Metadata: Metadata{
			ManagedSessionID: strings.TrimSpace(summary.GetManagedSessionId()),
			ObservedPath:     summary.GetObservedPath(),
			NetworkClass:     normalizeTag(summary.GetNetworkClass()),
			Region:           normalizeTag(summary.GetRegion()),
			CarrierTag:       normalizeTag(summary.GetCarrierTag()),
			ProviderTag:      normalizeTag(summary.GetProviderTag()),
		},
		StartedAt:       time.UnixMilli(int64(summary.GetWindowStartedAtUnixMillis())).UTC(),
		EndedAt:         time.UnixMilli(int64(summary.GetWindowEndedAtUnixMillis())).UTC(),
		RTTP50:          time.Duration(summary.GetRttP50Millis()) * time.Millisecond,
		RTTP95:          time.Duration(summary.GetRttP95Millis()) * time.Millisecond,
		Jitter:          time.Duration(summary.GetJitterMillis()) * time.Millisecond,
		LossBasisPoints: summary.GetLossBasisPoints(),
		ThroughputBPS:   summary.GetThroughputBps(),
		Connected:       time.Duration(summary.GetConnectedMillis()) * time.Millisecond,
		SampleCount:     summary.GetSampleCount(),
		DisconnectCount: summary.GetDisconnectCount(),
		PacketCount:     summary.GetPacketCount(),
		LossEventCount:  summary.GetLossEventCount(),
	}
	if err := window.Validate(); err != nil {
		return Window{}, err
	}
	return window, nil
}

// Collector 是单个 managed session、单个 observed path 的窗口 owner。
// Observe 只追加单调样本；Flush 生成聚合结果并保留最后一个累计样本作为下一窗口基线，
// 因而不会重复计算 bytes、packet loss 或 connected duration。
type Collector struct {
	metadata Metadata
	samples  []Sample
}

// NewCollector 创建质量窗口 collector 并写入第一个累计基线样本。
// 初始样本非法时直接失败；调用方不得用零时间或负 RTT 填补尚未就绪的 WebRTC stats。
func NewCollector(metadata Metadata, initial Sample) (*Collector, error) {
	metadata = normalizeMetadata(metadata)
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	if err := validateSample(initial); err != nil {
		return nil, err
	}
	return &Collector{metadata: metadata, samples: []Sample{normalizeSample(initial)}}, nil
}

// Observe 追加同一 candidate pair 的累计样本。
// 时间不递增或计数回退时失败，调用方必须显式结束旧路径窗口，不能把两条路径拼成一个 summary。
func (collector *Collector) Observe(sample Sample) error {
	if collector == nil || len(collector.samples) == 0 {
		return fmt.Errorf("path quality collector is not initialized")
	}
	if err := validateSample(sample); err != nil {
		return err
	}
	sample = normalizeSample(sample)
	previous := collector.samples[len(collector.samples)-1]
	if !previous.At.Before(sample.At) {
		return fmt.Errorf("path quality sample time did not advance")
	}
	if sample.BytesSent < previous.BytesSent || sample.BytesReceived < previous.BytesReceived || sample.PacketsSent < previous.PacketsSent || sample.LossEvents < previous.LossEvents {
		return ErrCounterRollback
	}
	collector.samples = append(collector.samples, sample)
	return nil
}

// Flush 完成当前窗口并把最后样本保留为下一窗口的累计基线。
// 聚合本身没有 route action、lease 请求或切换 side effect；GA002 只能消费这里的输出另行决策。
func (collector *Collector) Flush() (Window, error) {
	if collector == nil || len(collector.samples) < 2 {
		return Window{}, ErrInsufficientSamples
	}
	if len(collector.samples) > math.MaxUint32 {
		return Window{}, fmt.Errorf("path quality sample count exceeds wire limit")
	}
	samples := collector.samples
	first := samples[0]
	last := samples[len(samples)-1]
	rtts := make([]time.Duration, 0, len(samples))
	var jitterTotal time.Duration
	var connected time.Duration
	var disconnects uint32
	for index, sample := range samples {
		rtts = append(rtts, sample.RoundTripTime)
		if index == 0 {
			continue
		}
		previous := samples[index-1]
		jitterTotal += absoluteDuration(sample.RoundTripTime - previous.RoundTripTime)
		if previous.Connected {
			connected += sample.At.Sub(previous.At)
		}
		if previous.Connected && !sample.Connected && disconnects < math.MaxUint32 {
			disconnects++
		}
	}
	sort.Slice(rtts, func(left, right int) bool { return rtts[left] < rtts[right] })
	packetDelta := last.PacketsSent - first.PacketsSent
	lossDelta := last.LossEvents - first.LossEvents
	packetCount := saturatingAdd(packetDelta, lossDelta)
	byteCount := saturatingAdd(last.BytesSent-first.BytesSent, last.BytesReceived-first.BytesReceived)
	duration := last.At.Sub(first.At)
	window := Window{
		Metadata:        collector.metadata,
		StartedAt:       first.At,
		EndedAt:         last.At,
		RTTP50:          nearestRank(rtts, 50),
		RTTP95:          nearestRank(rtts, 95),
		Jitter:          jitterTotal / time.Duration(len(samples)-1),
		LossBasisPoints: ratioBasisPoints(lossDelta, packetCount),
		ThroughputBPS:   bitrate(byteCount, duration),
		Connected:       connected,
		SampleCount:     uint32(len(samples)),
		DisconnectCount: disconnects,
		PacketCount:     packetCount,
		LossEventCount:  lossDelta,
	}
	if err := window.Validate(); err != nil {
		return Window{}, err
	}
	collector.samples = []Sample{last}
	return window, nil
}

func validateSample(sample Sample) error {
	if sample.At.IsZero() || sample.RoundTripTime < 0 {
		return fmt.Errorf("invalid path quality sample")
	}
	return nil
}

func normalizeSample(sample Sample) Sample {
	sample.At = sample.At.UTC()
	return sample
}

func normalizeMetadata(metadata Metadata) Metadata {
	metadata.ManagedSessionID = strings.TrimSpace(metadata.ManagedSessionID)
	metadata.NetworkClass = normalizeTag(metadata.NetworkClass)
	metadata.Region = normalizeTag(metadata.Region)
	metadata.CarrierTag = normalizeTag(metadata.CarrierTag)
	metadata.ProviderTag = normalizeTag(metadata.ProviderTag)
	return metadata
}

func validObservedPath(path cloudpb.ObservedPath) bool {
	return path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT || path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY || path == cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH
}

func validateCorrelationID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\t ") {
		return fmt.Errorf("invalid managed session correlation id")
	}
	return nil
}

func validateTag(name, value string, required bool) error {
	value = normalizeTag(value)
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
	if len(value) > 64 || net.ParseIP(value) != nil {
		return fmt.Errorf("invalid %s", name)
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}

func normalizeTag(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func nearestRank(values []time.Duration, percentile int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := (percentile*len(values)+99)/100 - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func absoluteDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func ratioBasisPoints(numerator, denominator uint64) uint32 {
	if numerator == 0 || denominator == 0 {
		return 0
	}
	if numerator >= denominator {
		return 10_000
	}
	high, low := bits.Mul64(numerator, 10_000)
	result, remainder := bits.Div64(high, low, denominator)
	if remainder >= denominator/2+denominator%2 {
		result++
	}
	return uint32(result)
}

func bitrate(bytes uint64, duration time.Duration) uint64 {
	if bytes == 0 || duration <= 0 {
		return 0
	}
	seconds := uint64(duration)
	if bytes > math.MaxUint64/8 || bytes*8 > math.MaxUint64/uint64(time.Second) {
		return math.MaxUint64
	}
	return bytes * 8 * uint64(time.Second) / seconds
}

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}

func durationMillis32(value time.Duration) uint32 {
	millis := value.Milliseconds()
	if millis <= 0 {
		return 0
	}
	if millis > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(millis)
}

func durationMillis64(value time.Duration) uint64 {
	millis := value.Milliseconds()
	if millis <= 0 {
		return 0
	}
	return uint64(millis)
}
