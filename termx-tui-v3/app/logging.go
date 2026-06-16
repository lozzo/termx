package app

import (
	"context"
	"errors"
	"log/slog"
)

func isContextLifecycleError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// 正常退出、重连或 watcher 重订阅会触发 context 取消；只记日志，不能污染 pane/toast。
func logEffectError(logger *slog.Logger, component string, err error, attrs ...any) {
	if logger == nil || err == nil {
		return
	}
	values := append([]any{"component", component, "error", err}, attrs...)
	if isContextLifecycleError(err) {
		logger.Debug("tui-v3 effect stopped", values...)
		return
	}
	logger.Warn("tui-v3 effect failed", values...)
}
