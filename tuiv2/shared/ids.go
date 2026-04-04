package shared

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type IDSource interface {
	Next(prefix string) string
}

type SequentialIDSource struct {
	counter atomic.Int64
}

type scopedCounters struct {
	mu       sync.Mutex
	counters map[string]*atomic.Uint64
}

var globalCounters = scopedCounters{counters: make(map[string]*atomic.Uint64)}

func NewSequentialIDSource() *SequentialIDSource {
	return &SequentialIDSource{}
}

func (s *SequentialIDSource) Next(prefix string) string {
	n := s.counter.Add(1)
	return fmt.Sprintf("%s%d", prefix, n)
}

func GenerateShortID() string {
	return NextScopedID("short")
}

func NextScopedID(scope string) string {
	return strconv.FormatUint(globalCounters.next(scope), 10)
}

func ObserveScopedID(scope, raw string) {
	globalCounters.observe(scope, raw)
}

func NextPaneID() string {
	return NextScopedID("pane")
}

func ObservePaneID(raw string) {
	ObserveScopedID("pane", raw)
}

func NextTabID() string {
	return NextScopedID("tab")
}

func ObserveTabID(raw string) {
	ObserveScopedID("tab", raw)
}

func NextWorkspaceID() string {
	return NextScopedID("workspace")
}

func ObserveWorkspaceID(raw string) {
	ObserveScopedID("workspace", raw)
}

func LessNumericStrings(a, b string) bool {
	an, aok := parseObservedNumericID(a)
	bn, bok := parseObservedNumericID(b)
	if aok && bok {
		if an != bn {
			return an < bn
		}
	}
	if aok != bok {
		return aok
	}
	return a < b
}

func (s *scopedCounters) next(scope string) uint64 {
	counter := s.counter(scope)
	return counter.Add(1)
}

func (s *scopedCounters) observe(scope, raw string) {
	value, ok := parseObservedNumericID(raw)
	if !ok {
		return
	}
	counter := s.counter(scope)
	for {
		current := counter.Load()
		if current >= value {
			return
		}
		if counter.CompareAndSwap(current, value) {
			return
		}
	}
}

func (s *scopedCounters) counter(scope string) *atomic.Uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counters == nil {
		s.counters = make(map[string]*atomic.Uint64)
	}
	if counter := s.counters[scope]; counter != nil {
		return counter
	}
	counter := &atomic.Uint64{}
	s.counters[scope] = counter
	return counter
}

func parseObservedNumericID(raw string) (uint64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	if n, err := strconv.ParseUint(value, 10, 64); err == nil && n > 0 {
		return n, true
	}
	if idx := strings.LastIndexByte(value, '-'); idx >= 0 && idx < len(value)-1 {
		suffix := value[idx+1:]
		if n, err := strconv.ParseUint(suffix, 10, 64); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}
