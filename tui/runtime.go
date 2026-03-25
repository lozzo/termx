package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
	tuiruntime "github.com/lozzow/termx/tui/runtime"
	"github.com/lozzow/termx/tui/state/workspace"
	"os"
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

	ctx := context.Background()
	model := newTemporaryRootModel(cfg.Workspace)
	workspaceStore := newWorkspaceStoreForConfig(cfg)
	if cfg.AttachID != "" {
		workspaceStore = nil
	}
	restored := false

	if workspaceStore != nil && cfg.AttachID == "" {
		loaded, err := workspaceStore.Load(ctx)
		if err == nil {
			model = loaded
			restored = true
		} else if cfg.Logger != nil && !errors.Is(err, os.ErrNotExist) {
			cfg.Logger.Warn("restore workspace state failed, falling back to temporary workspace", "path", cfg.WorkspaceStatePath, "error", err)
		}
	}

	if !restored && client != nil {
		bootstrapped, err := tuiruntime.Bootstrap(ctx, client, tuiruntime.BootstrapConfig{
			DefaultShell: cfg.DefaultShell,
			Workspace:    cfg.Workspace,
			AttachID:     cfg.AttachID,
		})
		if err != nil {
			return err
		}
		model = bootstrapped
	} else if restored && client != nil {
		model = tuiruntime.RebindRestoredModel(ctx, client, model)
	}

	runnerModel := tea.Model(model)
	if workspaceStore != nil {
		runnerModel = tuiruntime.WrapModelWithWorkspacePersistence(model, tuiruntime.NewUpdateLoop(nil, tuiruntime.NewDebouncedWorkspaceSaver(workspaceStore, tuiruntime.DefaultWorkspaceSaveDebounce)))
	}
	return programRunner.Run(runnerModel, input, output)
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

func newTemporaryRootModel(name string) app.Model {
	model := app.NewModel()
	if name == "" {
		name = "main"
	}
	model.Workspace = workspace.NewTemporary(name)
	return model
}

func newWorkspaceStoreForConfig(cfg Config) tuiruntime.WorkspaceStore {
	if cfg.WorkspaceStatePath == "" {
		return nil
	}
	return tuiruntime.NewWorkspaceStore(cfg.WorkspaceStatePath)
}
