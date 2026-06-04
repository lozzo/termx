package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var socket string
	var logFile string
	var configPath string
	cmd := &cobra.Command{
		Use: "termx",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("starting tuiv2 root command", "log_file", logPath)
			if !isInteractiveTerminal() {
				return fmt.Errorf("termx TUI requires an interactive terminal; use `termx --help` or subcommands like `new`, `ls`, `attach`, `kill`, `rm`, `daemon`")
			}
			if err := rejectNestedTUI(); err != nil {
				logger.Warn("blocked nested tui launch")
				return err
			}
			cfg, err := tuiSharedConfig("main", "main", "", socket, logPath, resolveWorkspaceStatePath(), configPath)
			if err != nil {
				return err
			}
			return runTUIv2(cfg, os.Stdin, os.Stdout)
		},
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "socket path")
	cmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: $TERMX_LOG_FILE or XDG state dir)")
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "termx config path (default: XDG config dir termx.yaml)")
	cmd.AddCommand(daemonCommand(&socket, &configPath))
	cmd.AddCommand(newCommand(&socket, &logFile))
	cmd.AddCommand(lsCommand(&socket, &logFile))
	cmd.AddCommand(killCommand(&socket, &logFile))
	cmd.AddCommand(removeCommand(&socket, &logFile))
	cmd.AddCommand(attachCommand(&socket, &logFile, &configPath))
	cmd.AddCommand(remoteCommand(&socket, &logFile, &configPath))
	cmd.AddCommand(v3Command(&socket, &logFile))
	return cmd
}
