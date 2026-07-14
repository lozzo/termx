package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var addDevelopmentCommands = func(*cobra.Command, *string, *string, *string) {}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cliExitCode(err))
	}
}

func newRootCmd() *cobra.Command {
	var socket string
	var logFile string
	var configPath string
	cmd := &cobra.Command{
		Use:           "termx",
		Short:         "A terminal multiplexer for local and remote daemon endpoints",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runV3RootCommand(cmd, socket, logFile, configPath)
		},
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "socket path")
	cmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: $TERMX_LOG_FILE or XDG state dir)")
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "termx config path (default: XDG config dir tui-v3.yaml)")
	cmd.AddCommand(v3DaemonCommand(&socket, &logFile, &configPath))
	terminalRuntime := terminalCommandRuntime{socket: &socket, logFile: &logFile, configPath: &configPath}
	cmd.AddCommand(newTerminalCommand(terminalRuntime))
	cmd.AddCommand(newTerminalAliasCommands(terminalRuntime)...)
	cmd.AddCommand(v3CloudCommand())
	cmd.AddCommand(v3PairCommand())
	cmd.AddCommand(newConfigCommand(&configPath, &socket, &logFile))
	cmd.AddCommand(newEndpointCommand(&socket, &logFile))
	cmd.AddCommand(newFileCommand(&socket, &logFile))
	cmd.AddCommand(newWorkspaceCommand(&socket, &logFile))
	cmd.AddCommand(v3LicensesCommand())
	addDevelopmentCommands(cmd, &socket, &logFile, &configPath)
	return cmd
}
