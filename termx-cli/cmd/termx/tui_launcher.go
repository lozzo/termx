package main

import (
	"fmt"
	"io"
	"os"

	tuiv2app "github.com/lozzow/termx/tuiv2/app"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared" //nolint:typecheck
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	isInteractiveTerminal = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	}
	runTUIv2 = func(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
		socketPath := resolveSocket(cfg.SocketPath)
		cfg.SocketPath = socketPath
		client, err := dialOrStartClient(socketPath, cfg.LogFilePath, nil)
		if err != nil {
			return err
		}
		defer client.Close()
		return tuiv2app.RunWithClient(cfg, bridge.NewProtocolClient(client), stdin, stdout)
	}
)

func nestedTUIBlocked() bool {
	return os.Getenv("TERMX") == "1" && os.Getenv("TERMX_ALLOW_NESTED") != "1"
}

func rejectNestedTUI() error {
	if !nestedTUIBlocked() {
		return nil
	}
	return fmt.Errorf("refusing to start termx TUI inside a termx remote terminal; use a normal shell, or set TERMX_ALLOW_NESTED=1 if you really want nesting")
}

func tuiSharedConfig(workspace, sessionID, attachID, socket, logPath, workspaceStatePath, configPath string) (shared.Config, error) {
	if configPath == "" {
		configPath = shared.DefaultConfigPath()
	}
	if err := shared.EnsureDefaultConfigFile(configPath); err != nil {
		return shared.Config{}, err
	}
	fileCfg, err := shared.LoadConfig(configPath)
	if err != nil {
		return shared.Config{}, err
	}
	fileCfg.Workspace = workspace
	fileCfg.SessionID = sessionID
	fileCfg.AttachID = attachID
	fileCfg.SocketPath = socket
	fileCfg.LogFilePath = logPath
	fileCfg.WorkspaceStatePath = workspaceStatePath
	fileCfg.ConfigPath = configPath
	return fileCfg, nil
}

func legacyCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "legacy",
		Short: "Run the legacy termx-core and tuiv2 root TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting legacy tuiv2 root command", "log_file", logPath)
			if !isInteractiveTerminal() {
				return fmt.Errorf("termx legacy TUI requires an interactive terminal; use `termx --help`")
			}
			if err := rejectNestedTUI(); err != nil {
				logger.Warn("blocked nested legacy tui launch")
				return err
			}
			cfg, err := tuiSharedConfig("main", "main", "", *socket, logPath, resolveWorkspaceStatePath(), *configPath)
			if err != nil {
				return err
			}
			return runTUIv2(cfg, os.Stdin, os.Stdout)
		},
	}
}

func attachCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
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
			cfg, err := tuiSharedConfig("main", "main", args[0], *socket, logPath, "", *configPath)
			if err != nil {
				return err
			}
			return runTUIv2(cfg, os.Stdin, os.Stdout)
		},
	}
}
