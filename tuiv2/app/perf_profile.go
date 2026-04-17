package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type perfProfile struct {
	mu          sync.Mutex
	file        *os.File
	path        string
	ownedTrace  bool
	last        perftrace.Snapshot
	seq         uint64
	ctx         perfProfileContext
	ch          chan string
	done        chan struct{}
	sessionInfo perfProfileSession
}

type perfProfileSession struct {
	StartedAt     time.Time `json:"started_at"`
	RemoteLatency bool      `json:"remote_latency"`
	PID           int       `json:"pid"`
}

type perfProfileContext struct {
	Width                  int    `json:"width"`
	Height                 int    `json:"height"`
	ViewBytes              int    `json:"view_bytes"`
	ActivePaneID           string `json:"active_pane_id"`
	ActiveTerminalID       string `json:"active_terminal_id"`
	ActiveAltScreen        bool   `json:"active_alt_screen"`
	VisiblePaneCount       int    `json:"visible_pane_count"`
	FloatingCount          int    `json:"floating_count"`
	LayoutKind             string `json:"layout_kind"`
	VerticalScrollAllowed  bool   `json:"vertical_scroll_allowed"`
	VerticalScrollMode     string `json:"vertical_scroll_mode"`
	VerticalScrollDecision string `json:"vertical_scroll_decision"`
}

type perfProfileRecord struct {
	Kind      string                  `json:"kind"`
	Seq       uint64                  `json:"seq"`
	At        time.Time               `json:"at"`
	Session   perfProfileSession      `json:"session"`
	Context   perfProfileContext      `json:"context"`
	Events    []perfProfileEventDelta `json:"events,omitempty"`
	ElapsedMs float64                 `json:"elapsed_ms"`
}

type perfProfileEventDelta struct {
	Name      string  `json:"name"`
	Count     uint64  `json:"count"`
	Bytes     uint64  `json:"bytes"`
	TotalMs   float64 `json:"total_ms"`
	AverageMs float64 `json:"average_ms"`
}

func newPerfProfileFromEnv() *perfProfile {
	path := strings.TrimSpace(os.Getenv("TERMX_PERF_PROFILE"))
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil
	}
	file, err := os.Create(path)
	if err != nil {
		return nil
	}
	ownedTrace := false
	if perftrace.Current() == nil {
		perftrace.Enable()
		ownedTrace = true
	}
	p := &perfProfile{
		file:       file,
		path:       path,
		ownedTrace: ownedTrace,
		last:       perftrace.SnapshotCurrent(),
		ch:         make(chan string, 128),
		done:       make(chan struct{}),
		sessionInfo: perfProfileSession{
			StartedAt:     time.Now().UTC(),
			RemoteLatency: shared.RemoteLatencyProfileEnabled(),
			PID:           os.Getpid(),
		},
	}
	go p.run()
	p.writeRecordLocked(perfProfileRecord{
		Kind:    "session_start",
		Seq:     0,
		At:      time.Now().UTC(),
		Session: p.sessionInfo,
	})
	return p
}

func (p *perfProfile) Close() {
	if p == nil {
		return
	}
	p.Sample("session_end")
	close(p.ch)
	<-p.done
	if p.ownedTrace {
		perftrace.Disable()
	}
}

func (p *perfProfile) SetContext(ctx perfProfileContext) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.ctx = ctx
	p.mu.Unlock()
}

func (p *perfProfile) Sample(kind string) {
	if p == nil || strings.TrimSpace(kind) == "" {
		return
	}
	select {
	case p.ch <- kind:
	default:
	}
}

func (p *perfProfile) run() {
	defer close(p.done)
	for kind := range p.ch {
		p.record(kind)
	}
	if p.file != nil {
		_ = p.file.Close()
	}
}

func (p *perfProfile) record(kind string) {
	if p == nil {
		return
	}
	now := time.Now().UTC()
	next := perftrace.SnapshotCurrent()
	p.mu.Lock()
	ctx := p.ctx
	p.seq++
	seq := p.seq
	events := diffPerfProfileEvents(p.last, next)
	p.last = next
	record := perfProfileRecord{
		Kind:      kind,
		Seq:       seq,
		At:        now,
		Session:   p.sessionInfo,
		Context:   ctx,
		Events:    events,
		ElapsedMs: next.ElapsedMs,
	}
	// Keep explicit boundaries even if no new events were emitted.
	if len(events) > 0 || kind == "session_end" {
		p.writeRecordLocked(record)
	}
	p.mu.Unlock()
}

func (p *perfProfile) writeRecordLocked(record perfProfileRecord) {
	if p == nil || p.file == nil {
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	_, _ = p.file.Write(append(data, '\n'))
}

func diffPerfProfileEvents(previous, next perftrace.Snapshot) []perfProfileEventDelta {
	prevMap := make(map[string]perftrace.EventSnapshot, len(previous.Events))
	for _, event := range previous.Events {
		prevMap[event.Name] = event
	}
	deltas := make([]perfProfileEventDelta, 0, len(next.Events))
	for _, event := range next.Events {
		prev := prevMap[event.Name]
		count := event.Count - prev.Count
		bytes := event.Bytes - prev.Bytes
		totalMs := event.TotalMs - prev.TotalMs
		if count == 0 && bytes == 0 && totalMs == 0 {
			continue
		}
		delta := perfProfileEventDelta{
			Name:    event.Name,
			Count:   count,
			Bytes:   bytes,
			TotalMs: totalMs,
		}
		if count > 0 {
			delta.AverageMs = totalMs / float64(count)
		}
		deltas = append(deltas, delta)
	}
	return deltas
}

func (m *Model) currentPerfProfileContext(viewBytes int) perfProfileContext {
	ctx := perfProfileContext{ViewBytes: viewBytes}
	if m == nil {
		return ctx
	}
	ctx.Width = m.width
	ctx.Height = m.height
	ctx.ActiveAltScreen = m.activePaneAlternateScreen()
	if m.workbench == nil {
		return ctx
	}
	if pane := m.workbench.ActivePane(); pane != nil {
		ctx.ActivePaneID = pane.ID
		ctx.ActiveTerminalID = pane.TerminalID
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return ctx
	}
	ctx.FloatingCount = len(visible.FloatingPanes)
	if visible.ActiveTab >= 0 && visible.ActiveTab < len(visible.Tabs) {
		ctx.VisiblePaneCount = len(visible.Tabs[visible.ActiveTab].Panes)
	}
	ctx.LayoutKind = perfProfileLayoutKind(visible)
	mode, reason := m.verticalScrollOptimizationMode()
	ctx.VerticalScrollAllowed = mode != verticalScrollModeNone
	ctx.VerticalScrollMode = mode.String()
	ctx.VerticalScrollDecision = reason
	return ctx
}

func perfProfileLayoutKind(visible *workbench.VisibleWorkbench) string {
	if visible == nil {
		return "none"
	}
	if len(visible.FloatingPanes) > 0 {
		return "floating"
	}
	if visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return "no-active-tab"
	}
	panes := visible.Tabs[visible.ActiveTab].Panes
	switch len(panes) {
	case 0:
		return "empty"
	case 1:
		return "single"
	}
	sharedRows := false
	sharedCols := false
	for i := range panes {
		pane := panes[i]
		for j := i + 1; j < len(panes); j++ {
			other := panes[j]
			if rowRangesOverlap(pane.Rect.Y, pane.Rect.Y+pane.Rect.H, other.Rect.Y, other.Rect.Y+other.Rect.H) {
				sharedRows = true
			}
			if colRangesOverlap(pane.Rect.X, pane.Rect.X+pane.Rect.W, other.Rect.X, other.Rect.X+other.Rect.W) {
				sharedCols = true
			}
		}
	}
	if !sharedRows && sharedCols {
		return "stacked"
	}
	if sharedRows && !sharedCols {
		return "side-by-side"
	}
	return "mixed"
}

func rowRangesOverlap(a0, a1, b0, b1 int) bool {
	return a0 < b1 && b0 < a1
}

func colRangesOverlap(a0, a1, b0, b1 int) bool {
	return a0 < b1 && b0 < a1
}
