package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const floatingMoveTraceEnv = "TERMX_FLOATING_MOVE_TRACE_OUT"

type floatingMoveTraceRecorder struct {
	mu        sync.Mutex
	nextID    uint64
	pending   *floatingMoveTraceRecord
	completed []floatingMoveTraceRecord
	outPath   string
}

type floatingMoveTraceRecord struct {
	ID          uint64                   `json:"id"`
	Source      string                   `json:"source"`
	Action      string                   `json:"action"`
	PaneID      string                   `json:"pane_id"`
	Outcome     string                   `json:"outcome"`
	StartedAt   time.Time                `json:"started_at"`
	CompletedAt time.Time                `json:"completed_at"`
	StartRect   workbench.Rect           `json:"start_rect"`
	EndRect     workbench.Rect           `json:"end_rect"`
	Stages      []floatingMoveTraceStage `json:"stages"`
	TotalMs     float64                  `json:"total_ms"`
}

type floatingMoveTraceStage struct {
	Name string  `json:"name"`
	AtMs float64 `json:"at_ms"`
}

func newFloatingMoveTraceRecorder(path string) *floatingMoveTraceRecorder {
	return &floatingMoveTraceRecorder{
		outPath: strings.TrimSpace(path),
	}
}

func (r *floatingMoveTraceRecorder) Start(source, action, paneID string, rect workbench.Rect) {
	if r == nil || paneID == "" || !r.enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending != nil {
		r.completeLocked("superseded", r.pending.EndRect)
	}
	r.nextID++
	now := time.Now().UTC()
	r.pending = &floatingMoveTraceRecord{
		ID:        r.nextID,
		Source:    source,
		Action:    action,
		PaneID:    paneID,
		StartedAt: now,
		StartRect: rect,
		EndRect:   rect,
		Stages: []floatingMoveTraceStage{{
			Name: "input.start",
			AtMs: 0,
		}},
	}
	perftrace.Count(floatingMoveTraceMetricPrefix(source, action)+".started", 0)
}

func (r *floatingMoveTraceRecorder) Mark(stage string) {
	if r == nil || stage == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markLocked(stage)
}

func (r *floatingMoveTraceRecorder) MutationApplied(rect workbench.Rect) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		return
	}
	r.pending.EndRect = rect
	if r.pending.StartRect == rect {
		r.markLocked("mutation.noop")
		r.completeLocked("noop", rect)
		return
	}
	r.markLocked("mutation.changed")
}

func (r *floatingMoveTraceRecorder) Complete(outcome string, rect workbench.Rect) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completeLocked(outcome, rect)
}

func (r *floatingMoveTraceRecorder) Snapshot() []floatingMoveTraceRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]floatingMoveTraceRecord, len(r.completed))
	copy(out, r.completed)
	return out
}

func (r *floatingMoveTraceRecorder) enabled() bool {
	if r == nil {
		return false
	}
	return r.outPath != "" || perftrace.Current() != nil
}

func (r *floatingMoveTraceRecorder) HasPending() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending != nil
}

func (r *floatingMoveTraceRecorder) markLocked(stage string) {
	if r.pending == nil || stage == "" {
		return
	}
	r.pending.Stages = append(r.pending.Stages, floatingMoveTraceStage{
		Name: stage,
		AtMs: moveTraceMillis(time.Since(r.pending.StartedAt)),
	})
}

func (r *floatingMoveTraceRecorder) completeLocked(outcome string, rect workbench.Rect) {
	if r.pending == nil {
		return
	}
	if outcome == "" {
		outcome = "completed"
	}
	if rect.W > 0 || rect.H > 0 {
		r.pending.EndRect = rect
	}
	r.pending.Outcome = outcome
	r.markLocked("complete." + outcome)
	r.pending.CompletedAt = time.Now().UTC()
	r.pending.TotalMs = moveTraceMillis(r.pending.CompletedAt.Sub(r.pending.StartedAt))
	record := *r.pending
	r.completed = append(r.completed, record)
	if len(r.completed) > 128 {
		r.completed = append([]floatingMoveTraceRecord(nil), r.completed[len(r.completed)-128:]...)
	}
	r.observeLocked(record)
	r.appendLocked(record)
	r.pending = nil
}

func (r *floatingMoveTraceRecorder) observeLocked(record floatingMoveTraceRecord) {
	prefix := floatingMoveTraceMetricPrefix(record.Source, record.Action)
	perftrace.Count(prefix+".completed."+record.Outcome, 0)
	perftrace.ObserveDuration(prefix+".latency.total", time.Duration(record.TotalMs*float64(time.Millisecond)))
	for i := 1; i < len(record.Stages); i++ {
		prev := record.Stages[i-1]
		next := record.Stages[i]
		delta := next.AtMs - prev.AtMs
		if delta < 0 {
			continue
		}
		perftrace.ObserveDuration(
			prefix+".latency."+sanitizeTraceStageName(prev.Name)+"_to_"+sanitizeTraceStageName(next.Name),
			time.Duration(delta*float64(time.Millisecond)),
		)
	}
}

func (r *floatingMoveTraceRecorder) appendLocked(record floatingMoveTraceRecord) {
	if r.outPath == "" {
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	data = append(data, '\n')
	f, err := os.OpenFile(r.outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
}

func floatingMoveTraceMetricPrefix(source, action string) string {
	if source == "" {
		source = "unknown"
	}
	if action == "" {
		action = "unknown"
	}
	return "app.floating_move." + sanitizeTraceStageName(source) + "." + sanitizeTraceStageName(action)
}

func sanitizeTraceStageName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	var out strings.Builder
	out.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			lastUnderscore = false
		default:
			if lastUnderscore {
				continue
			}
			out.WriteByte('_')
			lastUnderscore = true
		}
	}
	sanitized := strings.Trim(out.String(), "_")
	if sanitized == "" {
		return "unknown"
	}
	if _, err := strconv.Atoi(sanitized[:1]); err == nil {
		return "n_" + sanitized
	}
	return sanitized
}

func isFloatingMoveAction(kind input.ActionKind) bool {
	switch kind {
	case input.ActionMoveFloatingLeft,
		input.ActionMoveFloatingRight,
		input.ActionMoveFloatingUp,
		input.ActionMoveFloatingDown:
		return true
	default:
		return false
	}
}

type floatingMoveTraceHandle struct {
	paneID string
	rect   workbench.Rect
	active bool
}

func (m *Model) beginFloatingMoveActionTrace(action input.SemanticAction) floatingMoveTraceHandle {
	if m == nil || m.moveTrace == nil || !isFloatingMoveAction(action.Kind) {
		return floatingMoveTraceHandle{}
	}
	paneID, rect, ok := m.activeFloatingTraceTarget(action.PaneID)
	if !ok {
		return floatingMoveTraceHandle{}
	}
	m.moveTrace.Start("keyboard", string(action.Kind), paneID, rect)
	m.moveTrace.Mark("mutation.begin")
	return floatingMoveTraceHandle{paneID: paneID, rect: rect, active: true}
}

func (m *Model) finishFloatingMoveActionTrace(handle floatingMoveTraceHandle) {
	if m == nil || m.moveTrace == nil || !handle.active {
		return
	}
	_, rect, ok := m.activeFloatingTraceTarget(handle.paneID)
	if !ok {
		m.moveTrace.Complete("pane_lost", handle.rect)
		return
	}
	m.moveTrace.MutationApplied(rect)
	if m.moveTrace.HasPending() {
		m.moveTrace.Mark("render.invalidate")
	}
}

func (m *Model) activeFloatingTraceTarget(explicitPaneID string) (string, workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return "", workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return "", workbench.Rect{}, false
	}
	paneID := explicitPaneID
	if paneID == "" {
		paneID = tab.ActivePaneID
	}
	if paneID == "" {
		return "", workbench.Rect{}, false
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			return paneID, floating.Rect, true
		}
	}
	return "", workbench.Rect{}, false
}

func moveTraceMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
