package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

type Config struct {
	DefaultShell       string
	Workspace          string
	AttachID           string
	IconSet            string
	DebugUI            bool
	StartupLayout      string
	WorkspaceStatePath string
	StartupAutoLayout  bool
	StartupPicker      bool
	Logger             *slog.Logger
	RequestTimeout     time.Duration
	PrefixTimeout      time.Duration
}

const DefaultPrefixTimeout = 3 * time.Second

// Run 明确表示当前主线 TUI 已经重置。
// 这里故意只保留 CLI 依赖的稳定接口，避免旧实现继续挂在主线上“半死不活”地迭代。
func Run(_ Client, cfg Config, _ io.Reader, _ io.Writer) error {
	if cfg.Logger != nil {
		cfg.Logger.Warn("tui mainline has been reset", "archive", "deprecated/tui-reset-2026-03-25", "legacy", "deprecated/tui-legacy")
	}
	return fmt.Errorf("termx TUI 已重置：当前主线暂无可运行界面，请基于新的产品定义重新实现；参考目录：deprecated/tui-legacy/ 与 deprecated/tui-reset-2026-03-25/")
}

// WaitForSocket 仍保留给 CLI 自动拉起 daemon 使用，这部分和 TUI 重写重置无关。
func WaitForSocket(path string, timeout time.Duration, probe func() error) error {
	if probe == nil {
		return fmt.Errorf("probe is nil")
	}
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			if err := probe(); err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return context.DeadlineExceeded
}
