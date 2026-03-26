package tui

import (
	"context"
	"errors"
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
	ctx := context.Background()
	model, err := loadInitialModel(ctx, client, cfg)
	if err != nil {
		return err
	}
	if cfg.Logger != nil {
		cfg.Logger.Info("starting tui", "screen", model.Screen)
	}
	return programRunner.Run(model, input, output)
}

func loadInitialModel(ctx context.Context, client Client, cfg Config) (app.Model, error) {
	if cfg.AttachID == "" && cfg.WorkspaceStatePath != "" {
		store := tuiruntime.NewWorkspaceStore(cfg.WorkspaceStatePath)
		model, err := store.Load(ctx)
		switch {
		case err == nil:
			if client == nil {
				return model, nil
			}
			// 恢复文件只保留持久态，运行时 snapshot 必须在启动时重新向 daemon 取回。
			return tuiruntime.RebindWorkspaceSessions(ctx, client, model)
		case errors.Is(err, os.ErrNotExist):
		default:
			return app.Model{}, err
		}
	}

	model := app.NewModel(cfg.Workspace)
	if client == nil {
		return model, nil
	}

	return tuiruntime.Bootstrap(ctx, client, tuiruntime.BootstrapConfig{
		Workspace:    cfg.Workspace,
		DefaultShell: cfg.DefaultShell,
		AttachID:     cfg.AttachID,
	})
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
