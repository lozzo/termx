package perftrace

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Recorder struct {
	startedAt time.Time

	mu     sync.Mutex
	events map[string]*eventStats
}

type eventStats struct {
	count uint64
	bytes uint64
	total time.Duration
	max   time.Duration
}

type Snapshot struct {
	StartedAt   time.Time       `json:"started_at"`
	CollectedAt time.Time       `json:"collected_at"`
	ElapsedMs   float64         `json:"elapsed_ms"`
	Events      []EventSnapshot `json:"events"`
}

type EventSnapshot struct {
	Name      string  `json:"name"`
	Count     uint64  `json:"count"`
	Bytes     uint64  `json:"bytes"`
	TotalMs   float64 `json:"total_ms"`
	AverageMs float64 `json:"average_ms"`
	MaxMs     float64 `json:"max_ms"`
}

var active atomic.Pointer[Recorder]

func NewRecorder() *Recorder {
	return &Recorder{
		startedAt: time.Now().UTC(),
		events:    make(map[string]*eventStats),
	}
}

func Enable() *Recorder {
	recorder := NewRecorder()
	active.Store(recorder)
	return recorder
}

func Disable() {
	active.Store(nil)
}

func Current() *Recorder {
	return active.Load()
}

func Reset() {
	recorder := Current()
	if recorder == nil {
		return
	}
	recorder.Reset()
}

func Measure(name string) func(bytes int) {
	recorder := Current()
	if recorder == nil || name == "" {
		return func(int) {}
	}
	start := time.Now()
	return func(bytes int) {
		recorder.observe(name, time.Since(start), bytes)
	}
}

func Count(name string, bytes int) {
	recorder := Current()
	if recorder == nil || name == "" {
		return
	}
	recorder.observe(name, 0, bytes)
}

func SnapshotCurrent() Snapshot {
	recorder := Current()
	if recorder == nil {
		return Snapshot{}
	}
	return recorder.Snapshot()
}

func WriteJSON(path string, snapshot Snapshot) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (r *Recorder) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.startedAt = time.Now().UTC()
	r.events = make(map[string]*eventStats)
	r.mu.Unlock()
}

func (r *Recorder) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	events := make([]EventSnapshot, 0, len(r.events))
	for name, stats := range r.events {
		event := EventSnapshot{
			Name:    name,
			Count:   stats.count,
			Bytes:   stats.bytes,
			TotalMs: durationMillis(stats.total),
			MaxMs:   durationMillis(stats.max),
		}
		if stats.count > 0 {
			event.AverageMs = durationMillis(stats.total) / float64(stats.count)
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Name < events[j].Name
	})

	return Snapshot{
		StartedAt:   r.startedAt,
		CollectedAt: time.Now().UTC(),
		ElapsedMs:   durationMillis(time.Since(r.startedAt)),
		Events:      events,
	}
}

func (s Snapshot) Event(name string) (EventSnapshot, bool) {
	for _, event := range s.Events {
		if event.Name == name {
			return event, true
		}
	}
	return EventSnapshot{}, false
}

func (r *Recorder) observe(name string, duration time.Duration, bytes int) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	stats := r.events[name]
	if stats == nil {
		stats = &eventStats{}
		r.events[name] = stats
	}
	stats.count++
	if bytes > 0 {
		stats.bytes += uint64(bytes)
	}
	stats.total += duration
	if duration > stats.max {
		stats.max = duration
	}
	r.mu.Unlock()
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
