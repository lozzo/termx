package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	corev2 "github.com/lozzow/termx/termx-core-v2"
	tuiv3 "github.com/lozzow/termx/termx-tui-v3"
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
	runTUIv3Smoke = tuiv3.SmokeRun
)

func v3Command(socket *string, logFile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "v3",
		Short: "Run experimental termx-core-v2 and termx-tui-v3 commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(v3DaemonCommand(socket, logFile))
	cmd.AddCommand(v3PingCommand(socket, logFile))
	cmd.AddCommand(v3SmokeCommand())
	cmd.AddCommand(v3NewCommand(socket, logFile))
	cmd.AddCommand(v3LsCommand(socket, logFile))
	cmd.AddCommand(v3KillCommand(socket, logFile))
	cmd.AddCommand(v3RemoveCommand(socket, logFile))
	return cmd
}

func v3DaemonCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the experimental core-v2 daemon in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()

			// v3 daemon 是显式实验入口，默认 daemon 仍保留旧 runtime。
			socketPath := resolveV3Socket(*socket)
			opts := []corev2.ServerOption{corev2.WithLogger(logger), corev2.WithSocketPath(socketPath)}
			srv := newCoreV2Server(opts...)
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			defer func() {
				_ = srv.Shutdown(context.Background())
			}()

			logger.Info("starting core-v2 experimental daemon", "socket", socketPath, "log_file", logPath)
			err = srv.ListenAndServe(ctx)
			if err != nil {
				logger.Error("core-v2 experimental daemon exited with error", "error", err)
			} else {
				logger.Info("core-v2 experimental daemon exited")
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
			frame, err := runTUIv3Smoke(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "termx v3 smoke ok: tui=%s lines=%d\n", tuiv3.ModuleName, len(frame.Lines))
			for _, line := range frame.Lines {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
}
