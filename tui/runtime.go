package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lozzow/termx/protocol"
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

var errRuntimeLoopNotImplemented = errors.New("termx TUI runtime loop is not implemented yet")

type runtimeDependencies struct {
	Planner          StartupPlanner
	TaskExecutor     StartupTaskExecutor
	SessionBootstrap RuntimeSessionBootstrapper
	TerminalSize     func(input io.Reader, output io.Writer) protocol.Size
}

// Run 目前只作为新架构迁移期的兼容壳存在。
// 当前先把 startup plan、bootstrap task 和 runtime session bootstrap 串起来。
// 真正的 Bubble Tea 生命周期还未接回，因此在完成前置接线后仍返回明确占位错误，避免假成功。
func Run(client Client, cfg Config, input io.Reader, output io.Writer) error {
	return runWithDependencies(client, cfg, input, output, runtimeDependencies{
		Planner:          NewStartupPlanner(nil),
		TaskExecutor:     NewStartupTaskExecutor(),
		SessionBootstrap: NewRuntimeSessionBootstrapper(),
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

	return errRuntimeLoopNotImplemented
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
