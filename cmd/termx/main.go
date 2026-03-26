package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	isInteractiveTerminal = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	}
	runTUI               = tui.Run
	dialOrStartTUIClient = func(path string, logFile string, logger *slog.Logger) (tui.Client, error) {
		client, err := dialOrStartClient(path, logFile, logger)
		if err != nil {
			return nil, err
		}
		return tui.NewProtocolClient(client), nil
	}
)

func nestedTUIBlocked() bool {
	return os.Getenv("TERMX") == "1" && os.Getenv("TERMX_ALLOW_NESTED") != "1"
}

func rejectNestedTUI() error {
	if !nestedTUIBlocked() {
		return nil
	}
	return fmt.Errorf("refusing to start termx TUI inside a termx-managed terminal; use a normal shell, or set TERMX_ALLOW_NESTED=1 if you really want nesting")
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var socket string
	var logFile string
	var layout string
	var iconSet string
	var prefixTimeout time.Duration
	cmd := &cobra.Command{
		Use: "termx",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting tui root command", "socket", resolveSocket(socket), "log_file", logPath, "layout", layout)
			if !isInteractiveTerminal() {
				return fmt.Errorf("termx TUI requires an interactive terminal; use `termx --help` or subcommands like `new`, `ls`, `attach`, `kill`, `daemon`")
			}
			if err := rejectNestedTUI(); err != nil {
				logger.Warn("blocked nested tui launch")
				return err
			}
			client, err := dialOrStartTUIClient(resolveSocket(socket), logPath, logger)
			if err != nil {
				logger.Error("failed to connect to daemon", "error", err)
				return err
			}
			defer client.Close()
			return runTUI(client, tui.Config{
				DefaultShell:       os.Getenv("SHELL"),
				Workspace:          "main",
				IconSet:            iconSet,
				PrefixTimeout:      prefixTimeout,
				StartupLayout:      layout,
				WorkspaceStatePath: resolveWorkspaceStatePath(),
				StartupAutoLayout:  true,
				StartupPicker:      true,
				Logger:             logger,
			}, os.Stdin, os.Stdout)
		},
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "socket path")
	cmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: $TERMX_LOG_FILE or XDG state dir)")
	cmd.PersistentFlags().StringVar(&iconSet, "icon-set", os.Getenv("TERMX_ICON_SET"), "icon set: ascii, unicode, nerd")
	cmd.PersistentFlags().DurationVar(&prefixTimeout, "prefix-timeout", tui.DefaultPrefixTimeout, "mode hold timeout after Ctrl+ shortcuts")
	cmd.Flags().StringVar(&layout, "layout", "", "startup layout name or YAML path")
	cmd.AddCommand(daemonCommand(&socket))
	cmd.AddCommand(newCommand(&socket, &logFile))
	cmd.AddCommand(lsCommand(&socket, &logFile))
	cmd.AddCommand(killCommand(&socket, &logFile))
	cmd.AddCommand(attachCommand(&socket, &logFile, &iconSet, &prefixTimeout))
	return cmd
}

func envBoolEnabled(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func daemonCommand(socket *string) *cobra.Command {
	return &cobra.Command{
		Use: "daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			logFile, _ := cmd.Flags().GetString("log-file")
			logger, closeLogger, logPath, err := openLogFileLogger(logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting daemon", "socket", resolveSocket(*socket), "log_file", logPath)
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			opts := []termx.ServerOption{termx.WithLogger(logger)}
			if *socket != "" {
				opts = append(opts, termx.WithSocketPath(*socket))
			}
			srv := termx.NewServer(opts...)
			err = srv.ListenAndServe(ctx)
			if err != nil {
				logger.Error("daemon exited with error", "error", err)
			} else {
				logger.Info("daemon exited")
			}
			return err
		},
	}
}

func newCommand(socket *string, logFile *string) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:  "new -- CMD [ARGS...]",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			if len(args) == 0 {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				args = []string{shell}
			}
			logger.Info("creating terminal", "socket", resolveSocket(*socket), "command", strings.Join(args, " "), "log_file", logPath)
			client, err := dialOrStartClient(resolveSocket(*socket), logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()
			created, err := client.Create(context.Background(), protocol.CreateParams{
				Command: args,
				Name:    name,
				Size:    currentSize(),
			})
			if err != nil {
				logger.Error("create terminal failed", "error", err)
				return err
			}
			logger.Info("created terminal", "terminal_id", created.TerminalID)
			fmt.Fprintln(cmd.OutOrStdout(), created.TerminalID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "terminal name")
	return cmd
}

func lsCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use: "ls",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, _, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			client, err := dialOrStartClient(resolveSocket(*socket), resolveLogFilePath(*logFile), logger)
			if err != nil {
				return err
			}
			defer client.Close()
			list, err := client.List(context.Background())
			if err != nil {
				logger.Error("list terminals failed", "error", err)
				return err
			}
			logger.Info("listed terminals", "count", len(list.Terminals))
			for _, item := range list.Terminals {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%dx%d\n",
					item.ID, item.Name, strings.Join(item.Command, " "), item.State, item.Size.Cols, item.Size.Rows)
			}
			return nil
		},
	}
}

func killCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:  "kill <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("killing terminal", "terminal_id", args[0], "socket", resolveSocket(*socket), "log_file", logPath)
			client, err := dialOrStartClient(resolveSocket(*socket), logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()
			err = client.Kill(context.Background(), args[0])
			if err != nil {
				logger.Error("kill terminal failed", "terminal_id", args[0], "error", err)
			}
			return err
		},
	}
}

func attachCommand(socket *string, logFile *string, iconSet *string, prefixTimeout *time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:  "attach <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting attach tui", "terminal_id", args[0], "socket", resolveSocket(*socket), "log_file", logPath)
			if err := rejectNestedTUI(); err != nil {
				logger.Warn("blocked nested attach tui", "terminal_id", args[0])
				return err
			}
			client, err := dialOrStartTUIClient(resolveSocket(*socket), logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()
			return runTUI(client, tui.Config{
				DefaultShell:  os.Getenv("SHELL"),
				Workspace:     "main",
				AttachID:      args[0],
				IconSet:       *iconSet,
				PrefixTimeout: *prefixTimeout,
				Logger:        logger,
			}, os.Stdin, os.Stdout)
		},
	}
}

func dialClient(path string) (*protocol.Client, error) {
	conn, err := unixtransport.Dial(path)
	if err != nil {
		return nil, err
	}
	client := protocol.NewClient(conn)
	if err := client.Hello(context.Background(), protocol.Hello{
		Version: protocol.Version,
		Client:  "termx-cli",
	}); err != nil {
		return nil, err
	}
	return client, nil
}

func dialOrStartClient(path string, logFile string, logger *slog.Logger) (*protocol.Client, error) {
	client, err := dialClient(path)
	if err == nil {
		if logger != nil {
			logger.Debug("connected to existing daemon", "socket", path)
		}
		return client, nil
	}
	if logger != nil {
		logger.Warn("initial daemon dial failed, attempting auto-start", "socket", path, "error", err)
	}
	if startErr := startDaemon(path, logFile); startErr != nil {
		return nil, err
	}
	if waitErr := tui.WaitForSocket(path, 5*time.Second, func() error {
		c, dialErr := dialClient(path)
		if dialErr != nil {
			return dialErr
		}
		_ = c.Close()
		return nil
	}); waitErr != nil {
		return nil, waitErr
	}
	if logger != nil {
		logger.Info("auto-started daemon became ready", "socket", path)
	}
	return dialClient(path)
}

func startDaemon(path string, logFile string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--socket", path}
	if logFile != "" {
		args = append(args, "--log-file", logFile)
	}
	args = append(args, "daemon")
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func resolveSocket(path string) string {
	if path != "" {
		return path
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/termx.sock"
	}
	return fmt.Sprintf("%s/termx-%d.sock", os.TempDir(), os.Getuid())
}

func currentSize() protocol.Size {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return protocol.Size{}
	}
	return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
}
