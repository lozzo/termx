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
			return runV3RootCommand(cmd, socket, logFile)
		},
	}
	cmd.PersistentFlags().StringVar(&socket, "socket", "", "socket path")
	cmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: $TERMX_LOG_FILE or XDG state dir)")
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "termx config path (default: XDG config dir termx.yaml)")
	cmd.AddCommand(v3DaemonCommand(&socket, &logFile, &configPath))
	cmd.AddCommand(v3NewCommand(&socket, &logFile))
	cmd.AddCommand(v3LsCommand(&socket, &logFile))
	cmd.AddCommand(v3KillCommand(&socket, &logFile))
	cmd.AddCommand(v3RemoveCommand(&socket, &logFile))
	cmd.AddCommand(v3AttachCommand(&socket, &logFile))
	cmd.AddCommand(remoteCommand(&socket, &logFile, &configPath))
	cmd.AddCommand(v3Command(&socket, &logFile, &configPath))
	return cmd
}
