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

// Run 只负责组装根模型与程序运行器，业务状态机留在 app/runtime 内部演进。
func Run(client Client, cfg Config, input io.Reader, output io.Writer) error {
	_ = client
	model := app.NewModel(cfg.Workspace)
	if cfg.Logger != nil {
		cfg.Logger.Info("starting tui", "screen", model.Screen)
	}
	return programRunner.Run(model, input, output)
}

// WaitForSocket 保持给 CLI 的等待逻辑稳定，不与 TUI 重构耦合。
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
