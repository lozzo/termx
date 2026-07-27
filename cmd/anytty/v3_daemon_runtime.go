package main

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	corev2 "github.com/anytty/anytty/core"
)

const daemonMemoryLimitMBEnv = "ANYTTY_DAEMON_MEMORY_LIMIT_MB"
const daemonHistoryBackpressureModeEnv = "ANYTTY_HISTORY_BACKPRESSURE_MODE"
const daemonHistoryBackpressureBufferMBEnv = "ANYTTY_HISTORY_BACKPRESSURE_BUFFER_MB"

func applyDaemonRuntimeTuning(logger *slog.Logger) {
	limitMBText := strings.TrimSpace(os.Getenv(daemonMemoryLimitMBEnv))
	if limitMBText == "" {
		return
	}
	limitMB, err := strconv.Atoi(limitMBText)
	if err != nil || limitMB <= 0 {
		logger.Warn("invalid daemon memory limit", "env", daemonMemoryLimitMBEnv, "value", limitMBText)
		return
	}
	limitBytes := int64(limitMB) << 20
	previous := debug.SetMemoryLimit(limitBytes)
	// 中文说明：这是显式 GC pacing 上限，用于 RSS smoke 验证 allocator 高水位；
	// 不清理 history/live truth，也不在运行中靠定时 scrub 掩盖内存问题。
	logger.Info("daemon memory limit configured", "limit_mb", limitMB, "previous_bytes", previous)
}

func daemonHistoryBackpressureConfig(logger *slog.Logger) corev2.HistoryBackpressureConfig {
	cfg := corev2.HistoryBackpressureConfig{}.Normalize()
	switch mode := strings.ToLower(strings.TrimSpace(os.Getenv(daemonHistoryBackpressureModeEnv))); mode {
	case "":
	case string(corev2.HistoryBackpressureLowLatency), "low_latency":
		cfg.Mode = corev2.HistoryBackpressureLowLatency
	case string(corev2.HistoryBackpressureBounded):
		cfg.Mode = corev2.HistoryBackpressureBounded
	default:
		logger.Warn("invalid history backpressure mode", "env", daemonHistoryBackpressureModeEnv, "value", mode)
	}
	bufferMBText := strings.TrimSpace(os.Getenv(daemonHistoryBackpressureBufferMBEnv))
	if bufferMBText != "" {
		bufferMB, err := strconv.Atoi(bufferMBText)
		if err != nil || bufferMB <= 0 {
			logger.Warn("invalid history backpressure buffer", "env", daemonHistoryBackpressureBufferMBEnv, "value", bufferMBText)
		} else {
			cfg.BufferBytes = int64(bufferMB) << 20
		}
	}
	cfg = cfg.Normalize()
	// 中文说明：背压配置只控制 history pending 输出调度；truth 仍由 linehist
	// EvictedRows 落盘维护，不能靠 live snapshot 或 TUI rows 补历史。
	logger.Info("history backpressure configured", "mode", cfg.Mode, "buffer_bytes", cfg.BufferBytes)
	return cfg
}
