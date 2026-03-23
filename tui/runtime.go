package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"golang.org/x/term"
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

type runtimeDependencies struct {
	Planner          StartupPlanner
	TaskExecutor     StartupTaskExecutor
	SessionBootstrap RuntimeSessionBootstrapper
	ProgramRunner    ProgramRunner
	Renderer         btui.Renderer
	TerminalSize     func(input io.Reader, output io.Writer) protocol.Size
}

// Run 当前会先完成 startup 规划、bootstrap task 和 runtime session 接线，
// 再启动一个最小 Bubble Tea 程序，后续继续在这条主线上补 renderer 和真实交互循环。
func Run(client Client, cfg Config, input io.Reader, output io.Writer) error {
	return runWithDependencies(client, cfg, input, output, runtimeDependencies{
		Planner:          NewStartupPlanner(nil),
		TaskExecutor:     NewStartupTaskExecutor(),
		SessionBootstrap: NewRuntimeSessionBootstrapper(),
		ProgramRunner:    bubbleteaProgramRunner{},
		Renderer:         runtimeRenderer{},
		TerminalSize:     currentTerminalSize,
	})
}

func runWithDependencies(client Client, cfg Config, input io.Reader, output io.Writer, deps runtimeDependencies) error {
	if deps.Planner == nil {
		deps.Planner = NewStartupPlanner(nil)
	}
	if deps.TaskExecutor == nil {
		deps.TaskExecutor = NewStartupTaskExecutor()
	}
	if deps.SessionBootstrap == nil {
		deps.SessionBootstrap = NewRuntimeSessionBootstrapper()
	}
	if deps.ProgramRunner == nil {
		deps.ProgramRunner = bubbleteaProgramRunner{}
	}
	if deps.Renderer == nil {
		deps.Renderer = runtimeRenderer{}
	}
	if deps.TerminalSize == nil {
		deps.TerminalSize = currentTerminalSize
	}

	ctx := context.Background()
	if cfg.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.RequestTimeout)
		defer cancel()
	}

	plan, err := deps.Planner.Plan(ctx, cfg)
	if err != nil {
		return err
	}
	logStartupWarnings(cfg.Logger, plan.Warnings)

	bootstrapped, err := deps.TaskExecutor.Execute(ctx, client, deps.TerminalSize(input, output), plan)
	if err != nil {
		return err
	}
	logStartupWarnings(cfg.Logger, bootstrapped.Warnings)

	sessions, err := deps.SessionBootstrap.Bootstrap(ctx, client, bootstrapped.State)
	if err != nil {
		return err
	}
	defer stopRuntimeSessions(sessions)

	terminalStore := NewRuntimeTerminalStore(sessions)
	renderer := deps.Renderer
	switch rendererValue := renderer.(type) {
	case runtimeRenderer:
		rendererValue.Screens = terminalStore
		renderer = rendererValue
	case *runtimeRenderer:
		rendererValue.Screens = terminalStore
		renderer = rendererValue
	}

	model := btui.NewModel(btui.ModelConfig{
		InitialState:       bootstrapped.State,
		Mapper:             btui.NewIntentMapper(btui.Config{PrefixTimeout: cfg.PrefixTimeout}),
		Reducer:            nil,
		EffectHandler:      btui.RuntimeEffectHandler{Executor: btui.DefaultRuntimeExecutor{}},
		Renderer:           renderer,
		UnmappedKeyHandler: NewRuntimeTerminalInputHandler(client, terminalStore),
	})
	return deps.ProgramRunner.Run(model, input, output)
}

func currentTerminalSize(_ io.Reader, output io.Writer) protocol.Size {
	file, ok := output.(*os.File)
	if !ok || file == nil {
		return protocol.Size{}
	}
	cols, rows, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return protocol.Size{}
	}
	return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
}

func logStartupWarnings(logger *slog.Logger, warnings []string) {
	if logger == nil {
		return
	}
	for _, warning := range warnings {
		if warning == "" {
			continue
		}
		logger.Warn("tui startup warning", "warning", warning)
	}
}

func stopRuntimeSessions(sessions RuntimeSessions) {
	for _, session := range sessions.Terminals {
		if session.Stop != nil {
			session.Stop()
		}
	}
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
