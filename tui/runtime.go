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
	StartupLayout      string
	WorkspaceStatePath string
	StartupAutoLayout  bool
	StartupPicker      bool
	Logger             *slog.Logger
	RequestTimeout     time.Duration
	PrefixTimeout      time.Duration
}

const DefaultPrefixTimeout = 3 * time.Second

// Run 目前只作为新架构迁移期的兼容壳存在。
// 真正的 Bubble Tea runtime 还未接回，因此先返回明确错误，避免假成功。
func Run(client Client, cfg Config, input io.Reader, output io.Writer) error {
	_ = client
	_ = cfg
	_ = input
	_ = output
	return fmt.Errorf("termx TUI runtime is not implemented yet")
}

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
