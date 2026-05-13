package termx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	rtdebug "runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/workbenchsvc"
)

type daemonResidencySnapshot struct {
	Label       string
	RSSKB       uint64
	HeapAlloc   uint64
	HeapObjects uint64
	NumGC       uint32
	Goroutines  int
}

func TestPerfResidencyDaemon(t *testing.T) {
	if os.Getenv("TERMX_RUN_DAEMON_RESIDENCY") != "1" {
		t.Skip("set TERMX_RUN_DAEMON_RESIDENCY=1 to run daemon residency harness")
	}

	ctx := context.Background()
	srv := NewServer()
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	idle := takeDaemonResidencySnapshot(t, "daemon_idle_startup")
	t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
		idle.Label, idle.RSSKB, idle.HeapAlloc, idle.HeapObjects, idle.NumGC, idle.Goroutines)

	info, err := srv.Create(ctx, CreateOptions{
		Command: []string{"/bin/sh", "-lc", "cat"},
		Name:    "perf-daemon-cat",
		Size:    Size{Cols: 96, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	defer func() {
		_ = srv.Kill(context.Background(), info.ID)
	}()

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	if _, err := srv.Subscribe(streamCtx, info.ID); err != nil {
		t.Fatalf("subscribe terminal: %v", err)
	}

	session, err := srv.workbench.CreateSession(workbenchsvc.CreateSessionOptions{
		ID:   "perf-daemon-session",
		Name: "perf-daemon-session",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := srv.workbench.AttachSession(session.ID, workbenchsvc.AttachSessionOptions{
		ClientID:   "perf-daemon-client",
		WindowCols: 120,
		WindowRows: 40,
	}); err != nil {
		t.Fatalf("attach session: %v", err)
	}

	for i := 0; i < 64; i++ {
		name := fmt.Sprintf("perf-daemon-%03d", i)
		if err := srv.SetMetadata(ctx, info.ID, name, map[string]string{
			"phase": "baseline",
			"iter":  strconv.Itoa(i),
		}); err != nil {
			t.Fatalf("set metadata %d: %v", i, err)
		}
	}
	if err := srv.WriteInput(ctx, info.ID, []byte("daemon-residency\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	after := takeDaemonResidencySnapshot(t, "daemon_one_terminal_one_session")
	t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
		after.Label, after.RSSKB, after.HeapAlloc, after.HeapObjects, after.NumGC, after.Goroutines)
}

func TestPerfTerminalStressScreenUpdates(t *testing.T) {
	if os.Getenv("TERMX_RUN_TERMINAL_STRESS") != "1" {
		t.Skip("set TERMX_RUN_TERMINAL_STRESS=1 to run terminal stress harness")
	}

	script := "../scripts/generate_terminal_stress.py"
	if _, err := os.Stat(script); err != nil {
		t.Skipf("stress script unavailable: %v", err)
	}

	lines := envInt("TERMX_STRESS_LINES", 20000)
	timeout := time.Duration(envInt("TERMX_STRESS_TIMEOUT_SECONDS", 60)) * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	srv := NewServer(WithDefaultScrollback(2000), WithDefaultKeepAfterExit(time.Second))
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	before := takeDaemonResidencySnapshot(t, "terminal_stress_before")
	info, err := srv.Create(ctx, CreateOptions{
		Command: []string{
			"python3",
			script,
			"--lines", strconv.Itoa(lines),
			"--seed", "1",
			"--width-hint", "120",
		},
		Name:           "perf-terminal-stress",
		Size:           Size{Cols: 120, Rows: 40},
		ScrollbackSize: 2000,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create terminal: %v", err)
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	stream, err := srv.Subscribe(streamCtx, info.ID)
	if err != nil {
		t.Fatalf("subscribe terminal: %v", err)
	}

	start := time.Now()
	var stats terminalStressStreamStats
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out after %s waiting for stress terminal; stats=%+v: %v", timeout, stats, ctx.Err())
		case msg, ok := <-stream:
			if !ok {
				goto done
			}
			stats.observe(t, msg)
			if msg.Type == StreamClosed {
				goto done
			}
		}
	}

done:
	elapsed := time.Since(start)
	snap, err := srv.Snapshot(context.Background(), info.ID, SnapshotOptions{ScrollbackLimit: 2000})
	if err != nil {
		t.Fatalf("snapshot terminal: %v", err)
	}
	after := takeDaemonResidencySnapshot(t, "terminal_stress_after")
	t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
		before.Label, before.RSSKB, before.HeapAlloc, before.HeapObjects, before.NumGC, before.Goroutines)
	t.Logf("%s rss_kb=%d heap_alloc=%d heap_objects=%d num_gc=%d goroutines=%d",
		after.Label, after.RSSKB, after.HeapAlloc, after.HeapObjects, after.NumGC, after.Goroutines)
	t.Logf("terminal_stress lines=%d elapsed=%s frames=%d screen_updates=%d screen_bytes=%d bootstrap=%d resizes=%d sync_lost=%d decoded_ops=%d scrollback_appends=%d snapshot_scrollback=%d screen_rows=%d",
		lines,
		elapsed,
		stats.frames,
		stats.screenUpdates,
		stats.screenUpdateBytes,
		stats.bootstrapDone,
		stats.resizes,
		stats.syncLost,
		stats.decodedOps,
		stats.scrollbackAppends,
		len(snap.Scrollback),
		len(snap.Screen.Cells),
	)
}

func takeDaemonResidencySnapshot(t *testing.T, label string) daemonResidencySnapshot {
	t.Helper()
	goruntime.GC()
	rtdebug.FreeOSMemory()
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)
	return daemonResidencySnapshot{
		Label:       label,
		RSSKB:       currentDaemonRSSKB(t),
		HeapAlloc:   mem.HeapAlloc,
		HeapObjects: mem.HeapObjects,
		NumGC:       mem.NumGC,
		Goroutines:  goruntime.NumGoroutine(),
	}
}

type terminalStressStreamStats struct {
	frames            int
	screenUpdates     int
	screenUpdateBytes int
	bootstrapDone     int
	resizes           int
	syncLost          int
	decodedOps        int
	scrollbackAppends int
}

func (s *terminalStressStreamStats) observe(t *testing.T, msg StreamMessage) {
	t.Helper()
	s.frames++
	switch msg.Type {
	case StreamScreenUpdate:
		s.screenUpdates++
		s.screenUpdateBytes += len(msg.Payload)
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			t.Fatalf("decode screen update: %v", err)
		}
		s.decodedOps += len(update.Ops)
		s.scrollbackAppends += len(update.ScrollbackAppend)
	case StreamBootstrapDone:
		s.bootstrapDone++
	case StreamResize:
		s.resizes++
	case StreamSyncLost:
		s.syncLost++
	}
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func currentDaemonRSSKB(t *testing.T) uint64 {
	t.Helper()
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Logf("rss lookup failed: %v", err)
		return 0
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return 0
	}
	rss, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		t.Logf("rss parse failed: %v", err)
		return 0
	}
	return rss
}
