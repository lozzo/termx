package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lozzow/termx/tui/app"
	tuiruntime "github.com/lozzow/termx/tui/runtime"
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

var programRunner tuiruntime.ProgramRunner = tuiruntime.NewProgramRunner()

// Run 保持外部 CLI 入口稳定，但内部已经切到新的根应用壳层。
// 顶层 screen router 与 overlay stack 需要在这里统一建模，避免运行入口继续依赖旧的 reset stub。
func Run(client Client, cfg Config, input io.Reader, output io.Writer) error {
	if cfg.Logger != nil {
		cfg.Logger.Info("starting tui root shell", "screen", app.ScreenWorkbench)
	}
	model := app.NewModel()
	if client != nil {
		bootstrapped, err := tuiruntime.Bootstrap(context.Background(), client, tuiruntime.BootstrapConfig{
			DefaultShell: cfg.DefaultShell,
			Workspace:    cfg.Workspace,
			AttachID:     cfg.AttachID,
		})
		if err != nil {
			return err
		}
		model = bootstrapped
	}
	return programRunner.Run(model, input, output)
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
