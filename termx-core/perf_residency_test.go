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
