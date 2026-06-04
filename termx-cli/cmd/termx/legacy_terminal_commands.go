package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

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

func removeCommand(socket *string, logFile *string) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"delete", "remove", "del"},
		Short:   "Delete a terminal from the daemon inventory",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, closeLogger, logPath, err := openLogFileLogger(*logFile)
			if err != nil {
				return err
			}
			defer closeLogger()
			logger.Info("removing terminal", "terminal_id", args[0], "socket", resolveSocket(*socket), "log_file", logPath)
			client, err := dialOrStartClient(resolveSocket(*socket), logPath, logger)
			if err != nil {
				return err
			}
			defer client.Close()
			err = client.Remove(context.Background(), args[0])
			if err != nil {
				logger.Error("remove terminal failed", "terminal_id", args[0], "error", err)
			}
			return err
		},
	}
}

func currentSize() protocol.Size {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return protocol.Size{}
	}
	return protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
}
