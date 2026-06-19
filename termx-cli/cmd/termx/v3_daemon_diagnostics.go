//go:build unix

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"time"
)

const daemonHeapProfileDirEnv = "TERMX_DAEMON_HEAP_PROFILE_DIR"

func startDaemonHeapProfiler(ctx context.Context, logger *slog.Logger) func(string) {
	dir := strings.TrimSpace(os.Getenv(daemonHeapProfileDirEnv))
	if dir == "" {
		return func(string) {}
	}
	var mu sync.Mutex
	write := func(reason string) {
		mu.Lock()
		defer mu.Unlock()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Warn("core-v2 daemon heap profile directory unavailable", "dir", dir, "error", err)
			return
		}
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		path := filepath.Join(dir, fmt.Sprintf("termx-core-v2-%s-heap%d-%d.pprof", daemonHeapProfileReason(reason), mem.HeapAlloc, time.Now().UnixNano()))
		file, err := os.Create(path)
		if err != nil {
			logger.Warn("core-v2 daemon heap profile create failed", "path", path, "error", err)
			return
		}
		if err := pprof.WriteHeapProfile(file); err != nil {
			_ = file.Close()
			logger.Warn("core-v2 daemon heap profile write failed", "path", path, "error", err)
			return
		}
		if err := file.Close(); err != nil {
			logger.Warn("core-v2 daemon heap profile close failed", "path", path, "error", err)
			return
		}
		logger.Info("core-v2 daemon heap profile written", "path", path, "heap_alloc", mem.HeapAlloc, "reason", reason)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	go func() {
		defer signal.Stop(signals)
		for {
			select {
			case <-ctx.Done():
				return
			case <-signals:
				// 中文说明：RSS smoke 用 SIGUSR1 在关键采样点抓 daemon heap，
				// 默认未设置 profile dir 时完全不启用这条诊断链路。
				write("usr1")
			}
		}
	}()
	return write
}

func daemonHeapProfileReason(reason string) string {
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		return "sample"
	}
	var builder strings.Builder
	for _, r := range reason {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "sample"
	}
	return builder.String()
}
