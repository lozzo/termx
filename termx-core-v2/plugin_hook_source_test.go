package termxcorev2

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-shared/plugin"
)

func TestDaemonHookSourceMapsTerminalLifecycleWithoutClientIdentity(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	source := NewDaemonHookSource(DaemonHookSourceConfig{DaemonID: "daemon-a", Now: func() time.Time { return now }})
	code := 23
	event, ok := source.TerminalEvent(Event{
		Type:       EventTerminalExited,
		TerminalID: "term-1",
		Terminal: &TerminalInfo{
			ID:       "term-1",
			Name:     "build",
			State:    TerminalStateExited,
			Size:     Size{Cols: 120, Rows: 40},
			ExitCode: &code,
			ExitedAt: now.Add(-time.Second),
		},
		Timestamp: now,
	})
	if !ok {
		t.Fatal("expected terminal exited hook")
	}
	if event.Type != plugin.SystemEventDaemonTerminalExited || event.SourceHost != plugin.HostDaemon || event.DaemonID != "daemon-a" || event.DaemonTerminalID != "term-1" {
		t.Fatalf("unexpected daemon hook envelope %#v", event)
	}
	if event.EndpointID != "" || event.TerminalRef != nil {
		t.Fatalf("daemon source must not invent client TerminalRef, got endpoint=%q ref=%#v", event.EndpointID, event.TerminalRef)
	}
	if event.ObjectKind != plugin.ObjectKindTerminal || event.ObjectID != "term-1" || event.Lossy {
		t.Fatalf("unexpected lifecycle object/lossy fields %#v", event)
	}
	if event.Trace.TraceID != event.EventID || event.Sequence != 1 || !event.Time.Equal(now) {
		t.Fatalf("unexpected trace/sequence/time %#v", event)
	}
	var payload DaemonTerminalLifecyclePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.TerminalID != "term-1" || payload.Name != "build" || payload.State != string(TerminalStateExited) || payload.ExitCode == nil || *payload.ExitCode != 23 {
		t.Fatalf("unexpected lifecycle payload %#v", payload)
	}
	if payload.Size == nil || payload.Size.Cols != 120 || payload.Size.Rows != 40 {
		t.Fatalf("payload should include terminal size metadata, got %#v", payload.Size)
	}
}

func TestDaemonHookSourceMapsResizeAndIgnoresLiveInvalidation(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 1, 0, 0, time.UTC)
	source := NewDaemonHookSource(DaemonHookSourceConfig{DaemonID: "daemon-a", Now: func() time.Time { return now }})
	resized, ok := source.TerminalEvent(Event{
		Type:       EventTerminalResized,
		TerminalID: "term-1",
		Terminal: &TerminalInfo{
			ID:    "term-1",
			Name:  "shell",
			State: TerminalStateRunning,
			Size:  Size{Cols: 100, Rows: 32},
		},
		OldSize: Size{Cols: 80, Rows: 24},
		NewSize: Size{Cols: 100, Rows: 32},
	})
	if !ok {
		t.Fatal("expected resize hook")
	}
	if resized.Type != plugin.SystemEventDaemonTerminalResized || !resized.Lossy {
		t.Fatalf("resize should be lossy daemon hook, got %#v", resized)
	}
	var payload DaemonTerminalResizePayload
	if err := json.Unmarshal(resized.Payload, &payload); err != nil {
		t.Fatalf("decode resize payload: %v", err)
	}
	if payload.OldSize == nil || payload.NewSize == nil || payload.OldSize.Cols != 80 || payload.NewSize.Cols != 100 {
		t.Fatalf("unexpected resize payload %#v", payload)
	}
	if _, ok := source.TerminalEvent(Event{Type: EventTerminalLiveInvalidated, TerminalID: "term-1"}); ok {
		t.Fatal("live invalidation must not be converted to plugin hook")
	}
}

func TestTerminalActivityTrackerEmitsMetadataOnlyActivityIdleAndResumed(t *testing.T) {
	start := time.Date(2026, 7, 9, 10, 2, 0, 0, time.UTC)
	source := NewDaemonHookSource(DaemonHookSourceConfig{DaemonID: "daemon-a", Now: func() time.Time { return start }})
	tracker, err := NewTerminalActivityTracker(TerminalActivityTrackerConfig{
		Source:     source,
		TerminalID: "term-1",
		IdleAfter:  time.Minute,
	})
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}

	secretOutput := "codex completed with private output"
	activity := tracker.RecordPTYOutput(start, len(secretOutput))
	if len(activity) != 1 || activity[0].Type != plugin.SystemEventDaemonTerminalOutputActivity {
		t.Fatalf("expected one activity hook, got %#v", activity)
	}
	if strings.Contains(string(activity[0].Payload), secretOutput) {
		t.Fatalf("activity payload must not contain raw PTY output: %s", activity[0].Payload)
	}
	var activityPayload TerminalOutputActivityPayload
	if err := json.Unmarshal(activity[0].Payload, &activityPayload); err != nil {
		t.Fatalf("decode activity payload: %v", err)
	}
	if activityPayload.Bytes != len(secretOutput) || activityPayload.TotalBytes != uint64(len(secretOutput)) || activityPayload.OutputSequence != 1 {
		t.Fatalf("unexpected activity payload %#v", activityPayload)
	}
	if activity[0].EndpointID != "" || activity[0].TerminalRef != nil || !activity[0].Lossy {
		t.Fatalf("pty activity must stay daemon-local lossy metadata, got %#v", activity[0])
	}

	if events := tracker.Tick(start.Add(30*time.Second), string(TerminalStateRunning)); len(events) != 0 {
		t.Fatalf("idle before threshold should not emit, got %#v", events)
	}
	idle := tracker.Tick(start.Add(61*time.Second), string(TerminalStateRunning))
	if len(idle) != 1 || idle[0].Type != plugin.SystemEventDaemonTerminalOutputIdle {
		t.Fatalf("expected one idle hook, got %#v", idle)
	}
	var idlePayload TerminalOutputIdlePayload
	if err := json.Unmarshal(idle[0].Payload, &idlePayload); err != nil {
		t.Fatalf("decode idle payload: %v", err)
	}
	if idlePayload.IdleFor != 61*time.Second || idlePayload.LastOutputAt != start || idlePayload.TerminalState != string(TerminalStateRunning) {
		t.Fatalf("unexpected idle payload %#v", idlePayload)
	}
	if again := tracker.Tick(start.Add(90*time.Second), string(TerminalStateRunning)); len(again) != 0 {
		t.Fatalf("idle should fire once until resumed output, got %#v", again)
	}

	resumed := tracker.RecordPTYOutput(start.Add(95*time.Second), 5)
	if len(resumed) != 2 || resumed[0].Type != plugin.SystemEventDaemonTerminalOutputResumed || resumed[1].Type != plugin.SystemEventDaemonTerminalOutputActivity {
		t.Fatalf("expected resumed then activity hooks, got %#v", resumed)
	}
	var resumedPayload TerminalOutputResumedPayload
	if err := json.Unmarshal(resumed[0].Payload, &resumedPayload); err != nil {
		t.Fatalf("decode resumed payload: %v", err)
	}
	if resumedPayload.Bytes != 5 || resumedPayload.IdleFor != 95*time.Second || resumedPayload.OutputSequence != 2 {
		t.Fatalf("unexpected resumed payload %#v", resumedPayload)
	}
}

func TestDaemonHookSourceInheritsCauseTraceForLoopProtection(t *testing.T) {
	source := NewDaemonHookSource(DaemonHookSourceConfig{DaemonID: "daemon-a"})
	cause := plugin.MessageTrace{TraceID: "trace-plugin", ActorPath: []plugin.PluginID{"acme.deploy"}, Depth: 2}
	event, ok := source.TerminalEventWithTrace(Event{
		Type:       EventTerminalRemoved,
		TerminalID: "term-1",
	}, cause)
	if !ok {
		t.Fatal("expected removed hook")
	}
	if event.Trace.TraceID != "trace-plugin" || event.Trace.Depth != 2 || !event.Trace.ContainsActor("acme.deploy") {
		t.Fatalf("hook should inherit cause trace for self-caused filtering, got %#v", event.Trace)
	}
	cause.ActorPath[0] = "mutated"
	if !event.Trace.ContainsActor("acme.deploy") {
		t.Fatalf("hook trace must be cloned, got %#v", event.Trace)
	}
}

func TestTerminalActivityTrackerRequiresSharedDaemonSource(t *testing.T) {
	if _, err := NewTerminalActivityTracker(TerminalActivityTrackerConfig{TerminalID: "term-1"}); err == nil {
		t.Fatal("expected missing source to fail")
	}
}

func TestServerPluginHookStreamPublishesLifecyclePTYActivityAndIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	hooks := server.SubscribePluginHooks(ctx, PluginHookFilter{DaemonTerminalID: "term-hook"})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-hook", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	created := mustReadPluginHook(t, hooks)
	if created.Type != plugin.SystemEventDaemonTerminalCreated || created.DaemonTerminalID != "term-hook" || created.EndpointID != "" || created.TerminalRef != nil {
		t.Fatalf("unexpected created hook %#v", created)
	}

	server.mu.Lock()
	terminal := server.terminals["term-hook"]
	server.mu.Unlock()
	if terminal == nil {
		t.Fatal("terminal handle missing")
	}
	if err := terminal.IngestOutput("hello"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	activity := mustReadPluginHook(t, hooks)
	if activity.Type != plugin.SystemEventDaemonTerminalOutputActivity || !activity.Lossy {
		t.Fatalf("unexpected activity hook %#v", activity)
	}
	if n := server.PollTerminalOutputIdle(time.Now().Add(2 * time.Minute)); n != 1 {
		t.Fatalf("expected one idle hook, got %d", n)
	}
	idle := mustReadPluginHook(t, hooks)
	if idle.Type != plugin.SystemEventDaemonTerminalOutputIdle {
		t.Fatalf("unexpected idle hook %#v", idle)
	}
	if _, ok := server.hookSource.TerminalEvent(Event{Type: EventTerminalLiveInvalidated, TerminalID: "term-hook"}); ok {
		t.Fatal("live invalidation should not become plugin hook")
	}
}

func mustReadPluginHook(t *testing.T, hooks <-chan plugin.HookEvent) plugin.HookEvent {
	t.Helper()
	select {
	case event := <-hooks:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for plugin hook")
	}
	return plugin.HookEvent{}
}
