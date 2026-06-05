package main

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	corev2 "github.com/lozzow/termx/termx-core-v2"
	tuiv3 "github.com/lozzow/termx/termx-tui-v3"
	tuiapp "github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/spf13/cobra"
)

type coreV2Server interface {
	ListenAndServe(context.Context) error
	Shutdown(context.Context) error
}

var (
	newCoreV2Server = func(opts ...corev2.ServerOption) coreV2Server {
		return corev2.NewServer(opts...)
	}
	runTUIv3Smoke         = tuiv3.SmokeRun
	runTUIv3SmokeDetailed = tuiv3.SmokeRunDetailed
)

func v3Command(socket *string, logFile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "v3",
		Short: "Run experimental termx-core-v2 and termx-tui-v3 commands",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(v3DaemonCommand(socket, logFile))
	cmd.AddCommand(v3PingCommand(socket, logFile))
	cmd.AddCommand(v3SmokeCommand())
	cmd.AddCommand(v3E2ESmokeCommand())
	cmd.AddCommand(v3TmuxSmokeCommand())
	cmd.AddCommand(v3TmuxTerminalSmokeCommand())
	cmd.AddCommand(v3TmuxResizeSmokeCommand())
	cmd.AddCommand(v3TmuxANSISmokeCommand())
	cmd.AddCommand(v3NewCommand(socket, logFile))
	cmd.AddCommand(v3LsCommand(socket, logFile))
	cmd.AddCommand(v3KillCommand(socket, logFile))
	cmd.AddCommand(v3RemoveCommand(socket, logFile))
	cmd.AddCommand(v3AttachCommand(socket, logFile))
	cmd.AddCommand(v3PaneCommandAdapterCommand())
	return cmd
}

func v3DaemonCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the core-v2 daemon in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()

			socketPath := resolveV3Socket(*socket)
			opts := []corev2.ServerOption{corev2.WithLogger(logger), corev2.WithSocketPath(socketPath)}
			srv := newCoreV2Server(opts...)
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			defer func() {
				_ = srv.Shutdown(context.Background())
			}()

			logger.Info("starting core-v2 daemon", "socket", socketPath, "log_file", logPath)
			err = srv.ListenAndServe(ctx)
			if err != nil {
				logger.Error("core-v2 daemon exited with error", "error", err)
			} else {
				logger.Info("core-v2 daemon exited")
			}
			return err
		},
	}
}

func v3PingCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Connect to the experimental core-v2 daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			socketPath := resolveV3Socket(*socket)
			client, err := dialOrStartV3Client(socketPath, logPath, logger)
			if err != nil {
				return err
			}
			if client != nil {
				defer client.Close()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "termx v3 daemon ok: socket=%s\n", socketPath)
			return nil
		},
	}
}

func v3SmokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "smoke",
		Short: "Run a non-interactive tui-v3 smoke render",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runTUIv3SmokeDetailed(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "termx v3 smoke ok: tui=%s cases=%d\n", tuiv3.ModuleName, len(result.Cases))
			for _, item := range result.Cases {
				fmt.Fprintf(cmd.OutOrStdout(), "case: %s lines=%d\n", item.Name, len(item.Frame.Lines))
				for _, line := range item.Frame.Lines {
					fmt.Fprintln(cmd.OutOrStdout(), line)
				}
			}
			return nil
		},
	}
}

func v3E2ESmokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "e2e-smoke",
		Short: "Run a non-interactive local core-v2/tui-v3 end-to-end smoke",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runV3E2ESmoke(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"termx v3 e2e smoke ok: terminal=%s frames=%d viewport=%dx%d session=%dx%d copy_cols=%d pane_commands=%d panes=%d active=%s zoom_checked=%v\n",
				result.TerminalID,
				result.Frames,
				result.ViewportCols,
				result.ViewportRows,
				result.SessionCols,
				result.SessionRows,
				result.CopyCols,
				result.PaneCommands,
				result.PaneCount,
				result.ActivePaneID,
				result.ZoomChecked,
			)
			return nil
		},
	}
}

func v3TmuxSmokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tmux-smoke",
		Short: "Run a tmux black-box harness smoke",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runV3TmuxSmoke(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"termx v3 tmux smoke ok: session=%s input=%s artifact_dir=%s ansi=%s plain=%s\n",
				result.Session,
				result.SentInput,
				result.ArtifactDir,
				result.ANSIPath,
				result.PlainPath,
			)
			return nil
		},
	}
}

func v3TmuxTerminalSmokeCommand() *cobra.Command {
	var termxBin string
	cmd := &cobra.Command{
		Use:   "tmux-terminal-smoke",
		Short: "Run a tmux black-box terminal create/attach/input smoke",
		RunE: func(cmd *cobra.Command, args []string) error {
			if termxBin == "" {
				exe, err := osExecutable()
				if err != nil {
					return err
				}
				termxBin = exe
			}
			result, err := runV3TmuxTerminalSmoke(cmd.Context(), termxBin)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"termx v3 tmux terminal smoke ok: terminal=%s session=%s input=%s artifact_dir=%s ansi=%s plain=%s daemon_log=%s socket=%s timeline=%s\n",
				result.TerminalID,
				result.Session,
				result.SentInput,
				result.ArtifactDir,
				result.ANSIPath,
				result.PlainPath,
				result.DaemonLog,
				result.SocketPath,
				result.TimelinePath,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&termxBin, "termx-bin", "", "termx binary path to run inside tmux")
	return cmd
}

func v3TmuxResizeSmokeCommand() *cobra.Command {
	var termxBin string
	cmd := &cobra.Command{
		Use:   "tmux-resize-smoke",
		Short: "Run a tmux black-box resize and layout smoke",
		RunE: func(cmd *cobra.Command, args []string) error {
			if termxBin == "" {
				exe, err := osExecutable()
				if err != nil {
					return err
				}
				termxBin = exe
			}
			result, err := runV3TmuxResizeSmoke(cmd.Context(), termxBin)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"termx v3 tmux resize smoke ok: terminal=%s session=%s window=%s before=%s after=%s artifact_dir=%s ansi=%s plain=%s daemon_log=%s socket=%s timeline=%s\n",
				result.TerminalID,
				result.Session,
				result.WindowSize,
				result.BeforeSize,
				result.AfterSize,
				result.ArtifactDir,
				result.ANSIPath,
				result.PlainPath,
				result.DaemonLog,
				result.SocketPath,
				result.TimelinePath,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&termxBin, "termx-bin", "", "termx binary path to run inside tmux")
	return cmd
}

func v3TmuxANSISmokeCommand() *cobra.Command {
	var termxBin string
	cmd := &cobra.Command{
		Use:   "tmux-ansi-smoke",
		Short: "Run a tmux black-box ANSI/theme/live surface smoke",
		RunE: func(cmd *cobra.Command, args []string) error {
			if termxBin == "" {
				exe, err := osExecutable()
				if err != nil {
					return err
				}
				termxBin = exe
			}
			result, err := runV3TmuxANSISmoke(cmd.Context(), termxBin)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"termx v3 tmux ansi smoke ok: terminal=%s session=%s artifact_dir=%s ansi=%s plain=%s daemon_log=%s socket=%s timeline=%s\n",
				result.TerminalID,
				result.Session,
				result.ArtifactDir,
				result.ANSIPath,
				result.PlainPath,
				result.DaemonLog,
				result.SocketPath,
				result.TimelinePath,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&termxBin, "termx-bin", "", "termx binary path to run inside tmux")
	return cmd
}

func v3PaneCommandAdapterCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pane-command -- COMMAND",
		Short: "Parse a tui-v3 pane mini command without mutating runtime state",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("pane-command requires a mini command")
			}
			parsed, err := tuiapp.ParsePaneMiniCommand(strings.Join(args, " "))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pane command: action=%s pane=%s source=%s\n", parsed.Action, parsed.Target.PaneID, parsed.Source)
			return nil
		},
	}
}
