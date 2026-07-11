// Package quality 保存私有 Probe Aggregator 的脱敏质量时间窗口。
//
// 公开进程生成 RTT、丢失、抖动、吞吐和断线窗口；本包只把这些窗口与服务端可信
// usage/cost summary 关联。它不签发 RelayLease、不计算 route score，也没有自动切换 side effect。
package quality

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion/pathquality"
)

var (
	// ErrDuplicateConflict 表示同一 managed session/path/window correlation key 被不同内容复用。
	// 冲突窗口必须拒绝，不能覆盖已经进入质量基线的服务端真值。
	ErrDuplicateConflict = errors.New("path quality observation idempotency conflict")
	// ErrCapacity 表示 Probe Aggregator 的有界 series 容量已满。
	// 调用方应执行明确的持久化或容量治理，不能静默丢弃其他 corridor 的窗口。
	ErrCapacity = errors.New("path quality aggregator capacity reached")
	// ErrNotFound 表示请求的匿名质量 series 尚无观测窗口。
	ErrNotFound = errors.New("path quality series not found")
)

// CostSource 标识私有成本 summary 的可信来源。
// 该值只在闭源服务内存在，公开 caller 不能通过 PathQualitySummary 填写或覆盖成本。
type CostSource string

const (
	// CostSourceNone 表示当前窗口没有托管网络成本，例如 direct path。
	CostSourceNone CostSource = "none"
	// CostSourceRelayUsage 表示成本来自已经验签、去重并结算的 Relay usage。
	CostSourceRelayUsage CostSource = "relay_usage"
	// CostSourceRouteEstimate 表示成本来自当前私有 route/provider 成本表的受控估算。
	CostSourceRouteEstimate CostSource = "route_estimate"
)

// CostSummary 是与一个质量窗口时间范围对应的私有网络成本摘要。
// EstimatedMicrounits 使用内部统一最小成本单位；BillableBytes 与 ActiveMillis 来自可信
// Relay usage 或 route estimate，不得从公开客户端上报值推导。
type CostSummary struct {
	Source              CostSource
	EstimatedMicrounits uint64
	BillableBytes       uint64
	ActiveMillis        uint64
}

// Validate 校验成本来源与数值形状。
// `none` 必须保持全零，避免把缺失成本误标成免费；非零成本必须标明受信来源。
func (summary CostSummary) Validate() error {
	switch summary.Source {
	case CostSourceNone:
		if summary.EstimatedMicrounits != 0 || summary.BillableBytes != 0 || summary.ActiveMillis != 0 {
			return fmt.Errorf("cost source none contains values")
		}
	case CostSourceRelayUsage, CostSourceRouteEstimate:
	default:
		return fmt.Errorf("unknown cost source %q", summary.Source)
	}
	return nil
}

// CostRateCard 是私有 provider/corridor 成本表投影。
// 两个费率都使用内部统一 microunit；具体商业权重和供应商合同不进入 public protocol。
type CostRateCard struct {
	MicrounitsPerGiB    uint64
	MicrounitsPerMinute uint64
}

// Summarize 把已经验真的 billable bytes 与 active duration 转换为有来源的成本摘要。
// 计算按最小单位向上取整并在溢出时饱和，避免 cost guard 因整数截断低估路径成本。
func (rate CostRateCard) Summarize(source CostSource, billableBytes, activeMillis uint64) (CostSummary, error) {
	if source != CostSourceRelayUsage && source != CostSourceRouteEstimate {
		return CostSummary{}, fmt.Errorf("cost rate requires a trusted source")
	}
	bytesCost := multiplyDivideCeil(billableBytes, rate.MicrounitsPerGiB, 1<<30)
	timeCost := multiplyDivideCeil(activeMillis, rate.MicrounitsPerMinute, uint64(time.Minute/time.Millisecond))
	return CostSummary{
		Source:              source,
		EstimatedMicrounits: saturatingAdd(bytesCost, timeCost),
		BillableBytes:       billableBytes,
		ActiveMillis:        activeMillis,
	}, nil
}

// SeriesKey 是 Probe Aggregator 的匿名质量基线键。
// managed session 只用于窗口幂等关联，不进入 series key；相同 path/region/network taxonomy
// 可以跨短期 session 形成 corridor 基线，但不会暴露账号、设备或 endpoint identity。
type SeriesKey struct {
	ObservedPath cloudpb.ObservedPath
	NetworkClass string
	Region       string
	CarrierTag   string
	ProviderTag  string
}

// Record 是一个已校验公开质量窗口和私有成本 summary 的不可变配对。
// Window 是网络测量真值，Cost 是服务端商业成本真值，两者由相同时间窗口关联但来源保持分离。
type Record struct {
	Window       pathquality.Window
	Cost         CostSummary
	CostAttached bool
}

// Baseline 是一个匿名 series 当前保留窗口的可解释汇总。
// RTT/Jitter 字段是各窗口分位数按 sample count 的加权均值，不伪装成跨窗口原始样本分位数；
// GA002 可以读取该结果，但必须在独立 planner 中实现阈值、hysteresis 和 cost guard。
type Baseline struct {
	Series                  SeriesKey
	WindowCount             uint64
	SampleCount             uint64
	LatestWindowEndedAt     time.Time
	MeanWindowRTTP50Millis  uint64
	MeanWindowRTTP95Millis  uint64
	MeanWindowJitterMillis  uint64
	LossBasisPoints         uint32
	MeanThroughputBPS       uint64
	DisconnectCount         uint64
	ConnectedMillis         uint64
	EstimatedCostMicrounits uint64
	BillableBytes           uint64
	CostActiveMillis        uint64
	CostWindowCount         uint64
	UnpricedWindowCount     uint64
}

// Config 固定 Probe Aggregator 的内存容量和窗口保留期。
// 当前实现是 deterministic domain harness；生产 adapter 必须在相同约束下接持久化，不能依赖无界 map。
type Config struct {
	MaxSeries           int
	MaxWindowsPerSeries int
	MaxWindowAge        time.Duration
}

// IngestResult 描述一次窗口写入是否命中完全相同的幂等记录。
// Duplicate 不会重复增加窗口、disconnect 或 cost 汇总。
type IngestResult struct {
	Duplicate   bool
	Series      SeriesKey
	Observation ObservationRef
}

// ObservationRef 是私有质量窗口与稍后到达的可信 usage/cost 之间的关联键。
// 它只使用 managed session correlation、observed path 和窗口边界，不包含账号、设备、terminal 或 credential。
type ObservationRef struct {
	ManagedSessionID string
	ObservedPath     cloudpb.ObservedPath
	StartedMillis    int64
	EndedMillis      int64
}

type series struct {
	records []Record
	seen    map[ObservationRef]Record
}

// Aggregator 是并发安全、容量有界的 Probe Aggregator domain owner。
// Ingest 只提交质量与成本记录；查询只返回副本，不提供 route selection、lease 或 transport callback。
type Aggregator struct {
	mu     sync.RWMutex
	config Config
	series map[SeriesKey]*series
	index  map[ObservationRef]SeriesKey
}

// NewAggregator 创建空 Probe Aggregator。
// 所有容量与保留期必须显式为正，缺失限制会失败而不是退化成无界 telemetry store。
func NewAggregator(config Config) (*Aggregator, error) {
	if config.MaxSeries < 1 || config.MaxWindowsPerSeries < 1 || config.MaxWindowAge <= 0 {
		return nil, fmt.Errorf("invalid path quality aggregator configuration")
	}
	return &Aggregator{config: config, series: make(map[SeriesKey]*series), index: make(map[ObservationRef]SeriesKey)}, nil
}

// Ingest 校验公开质量窗口、时间范围和幂等键后原子写入。
// 质量通常早于 Relay usage 到达，因此本方法不接受 caller cost；私有结算完成后必须用 AttachCost
// 和返回的 ObservationRef 关联。未来或过旧窗口会被拒绝，整个过程不触发选路动作。
func (aggregator *Aggregator) Ingest(request *cloudpb.ReportPathQualityRequest, observedAt time.Time) (IngestResult, error) {
	if aggregator == nil || observedAt.IsZero() {
		return IngestResult{}, fmt.Errorf("invalid path quality ingest")
	}
	if request == nil {
		return IngestResult{}, fmt.Errorf("decode path quality window: missing request")
	}
	window, err := pathquality.Decode(request.GetSummary())
	if err != nil {
		return IngestResult{}, fmt.Errorf("decode path quality window: %w", err)
	}
	observedAt = observedAt.UTC()
	if window.EndedAt.After(observedAt) || window.StartedAt.Before(observedAt.Add(-aggregator.config.MaxWindowAge)) {
		return IngestResult{}, fmt.Errorf("path quality window outside retention bounds")
	}
	key := SeriesKey{
		ObservedPath: window.ObservedPath,
		NetworkClass: window.NetworkClass,
		Region:       window.Region,
		CarrierTag:   window.CarrierTag,
		ProviderTag:  window.ProviderTag,
	}
	observation := ObservationRef{
		ManagedSessionID: window.ManagedSessionID,
		ObservedPath:     window.ObservedPath,
		StartedMillis:    window.StartedAt.UnixMilli(),
		EndedMillis:      window.EndedAt.UnixMilli(),
	}
	record := Record{Window: window}

	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()
	current := aggregator.series[key]
	if current == nil {
		if len(aggregator.series) >= aggregator.config.MaxSeries {
			return IngestResult{}, ErrCapacity
		}
		current = &series{seen: make(map[ObservationRef]Record)}
		aggregator.series[key] = current
	}
	if existing, exists := current.seen[observation]; exists {
		if existing.Window != window {
			return IngestResult{}, ErrDuplicateConflict
		}
		return IngestResult{Duplicate: true, Series: key, Observation: observation}, nil
	}
	current.records = append(current.records, record)
	current.seen[observation] = record
	aggregator.index[observation] = key
	sort.Slice(current.records, func(left, right int) bool {
		if current.records[left].Window.EndedAt.Equal(current.records[right].Window.EndedAt) {
			return current.records[left].Window.ManagedSessionID < current.records[right].Window.ManagedSessionID
		}
		return current.records[left].Window.EndedAt.Before(current.records[right].Window.EndedAt)
	})
	for len(current.records) > aggregator.config.MaxWindowsPerSeries {
		evicted := current.records[0]
		current.records = current.records[1:]
		evictedObservation := observationRef(evicted.Window)
		delete(current.seen, evictedObservation)
		delete(aggregator.index, evictedObservation)
	}
	return IngestResult{Series: key, Observation: observation}, nil
}

// AttachCost 把私有 usage ledger 或 provider rate card 生成的成本附加到既有质量窗口。
// 相同成本重复附加是幂等成功；不同成本复用同一 observation ref 会失败，公开 caller 没有调用该方法的 contract。
func (aggregator *Aggregator) AttachCost(observation ObservationRef, cost CostSummary) (bool, error) {
	if aggregator == nil {
		return false, ErrNotFound
	}
	if err := cost.Validate(); err != nil {
		return false, err
	}
	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()
	key, exists := aggregator.index[observation]
	if !exists {
		return false, ErrNotFound
	}
	current := aggregator.series[key]
	record, exists := current.seen[observation]
	if !exists {
		return false, ErrNotFound
	}
	if cost.ActiveMillis > uint64(record.Window.EndedAt.Sub(record.Window.StartedAt).Milliseconds()) {
		return false, fmt.Errorf("cost summary exceeds quality window")
	}
	if record.CostAttached {
		if record.Cost != cost {
			return false, ErrDuplicateConflict
		}
		return true, nil
	}
	record.Cost = cost
	record.CostAttached = true
	current.seen[observation] = record
	for index := range current.records {
		if observationRef(current.records[index].Window) == observation {
			current.records[index] = record
			break
		}
	}
	return false, nil
}

// Windows 返回一个匿名 series 当前保留的时间有序记录副本。
// 调用方修改返回 slice 不会改变 Aggregator 真值；未知 series 返回 ErrNotFound。
func (aggregator *Aggregator) Windows(key SeriesKey) ([]Record, error) {
	if aggregator == nil {
		return nil, ErrNotFound
	}
	aggregator.mu.RLock()
	defer aggregator.mu.RUnlock()
	current := aggregator.series[key]
	if current == nil {
		return nil, ErrNotFound
	}
	return append([]Record(nil), current.records...), nil
}

// Baseline 返回一个匿名 series 在 observedAt 时仍处于 MaxWindowAge 内的质量与成本汇总。
// 该函数是纯查询，不缓存 score、不选择 candidate，也不调用 Hub、Relay 或 Companion；
// 只有陈旧窗口时返回 ErrNotFound，防止后续 planner 把历史网络状态误当作当前质量。
func (aggregator *Aggregator) Baseline(key SeriesKey, observedAt time.Time) (Baseline, error) {
	if aggregator == nil || observedAt.IsZero() {
		return Baseline{}, ErrNotFound
	}
	records, err := aggregator.Windows(key)
	if err != nil {
		return Baseline{}, err
	}
	observedAt = observedAt.UTC()
	retained := records[:0]
	for _, record := range records {
		if !record.Window.EndedAt.After(observedAt) && !record.Window.StartedAt.Before(observedAt.Add(-aggregator.config.MaxWindowAge)) {
			retained = append(retained, record)
		}
	}
	records = retained
	if len(records) == 0 {
		return Baseline{}, ErrNotFound
	}
	baseline := Baseline{Series: key, WindowCount: uint64(len(records))}
	var weightedP50 uint64
	var weightedP95 uint64
	var weightedJitter uint64
	var weightedThroughput uint64
	var throughputWeight uint64
	var lossEvents uint64
	var packets uint64
	for _, record := range records {
		window := record.Window
		if window.EndedAt.After(baseline.LatestWindowEndedAt) {
			baseline.LatestWindowEndedAt = window.EndedAt
		}
		weight := uint64(window.SampleCount)
		baseline.SampleCount = saturatingAdd(baseline.SampleCount, weight)
		weightedP50 = saturatingAdd(weightedP50, saturatingMultiply(uint64(window.RTTP50.Milliseconds()), weight))
		weightedP95 = saturatingAdd(weightedP95, saturatingMultiply(uint64(window.RTTP95.Milliseconds()), weight))
		weightedJitter = saturatingAdd(weightedJitter, saturatingMultiply(uint64(window.Jitter.Milliseconds()), weight))
		durationWeight := uint64(window.EndedAt.Sub(window.StartedAt).Milliseconds())
		throughputWeight = saturatingAdd(throughputWeight, durationWeight)
		weightedThroughput = saturatingAdd(weightedThroughput, saturatingMultiply(window.ThroughputBPS, durationWeight))
		lossEvents = saturatingAdd(lossEvents, window.LossEventCount)
		packets = saturatingAdd(packets, window.PacketCount)
		baseline.DisconnectCount = saturatingAdd(baseline.DisconnectCount, uint64(window.DisconnectCount))
		baseline.ConnectedMillis = saturatingAdd(baseline.ConnectedMillis, uint64(window.Connected.Milliseconds()))
		if record.CostAttached {
			baseline.CostWindowCount++
			baseline.EstimatedCostMicrounits = saturatingAdd(baseline.EstimatedCostMicrounits, record.Cost.EstimatedMicrounits)
			baseline.BillableBytes = saturatingAdd(baseline.BillableBytes, record.Cost.BillableBytes)
			baseline.CostActiveMillis = saturatingAdd(baseline.CostActiveMillis, record.Cost.ActiveMillis)
		} else {
			baseline.UnpricedWindowCount++
		}
	}
	if baseline.SampleCount > 0 {
		baseline.MeanWindowRTTP50Millis = weightedP50 / baseline.SampleCount
		baseline.MeanWindowRTTP95Millis = weightedP95 / baseline.SampleCount
		baseline.MeanWindowJitterMillis = weightedJitter / baseline.SampleCount
	}
	if throughputWeight > 0 {
		baseline.MeanThroughputBPS = weightedThroughput / throughputWeight
	}
	baseline.LossBasisPoints = ratioBasisPoints(lossEvents, packets)
	return baseline, nil
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

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
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

func multiplyDivideCeil(left, right, divisor uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	high, low := bits.Mul64(left, right)
	if high >= divisor {
		return math.MaxUint64
	}
	quotient, remainder := bits.Div64(high, low, divisor)
	if remainder != 0 {
		if quotient == math.MaxUint64 {
			return math.MaxUint64
		}
		quotient++
	}
	return quotient
}

func observationRef(window pathquality.Window) ObservationRef {
	return ObservationRef{
		ManagedSessionID: window.ManagedSessionID,
		ObservedPath:     window.ObservedPath,
		StartedMillis:    window.StartedAt.UnixMilli(),
		EndedMillis:      window.EndedAt.UnixMilli(),
	}
}
