package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/spf13/cobra"
)

type v3HistoryBacklogConfig struct {
	TerminalID string
	OutPath    string
}

func v3HistoryBacklogCommand(socket *string, logFile *string) *cobra.Command {
	cfg := v3HistoryBacklogConfig{}
	cmd := &cobra.Command{
		Use:   "history-backlog <terminal-id>",
		Short: "Dump core-v2 history backlog diagnostics",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.TerminalID = strings.TrimSpace(args[0])
			if cfg.TerminalID == "" {
				return fmt.Errorf("terminal id is required")
			}
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
			if cfg.OutPath == "" {
				return runV3HistoryBacklog(cmd.Context(), client, cfg, cmd.OutOrStdout())
			}
			file, err := os.Create(cfg.OutPath)
			if err != nil {
				return err
			}
			defer file.Close()
			if err := runV3HistoryBacklog(cmd.Context(), client, cfg, file); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "termx v3 history backlog ok: terminal=%s out=%s\n", cfg.TerminalID, cfg.OutPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfg.OutPath, "out", "o", "", "file path to write the history backlog TSV")
	return cmd
}

func runV3HistoryBacklog(ctx context.Context, client *protocol.Client, cfg v3HistoryBacklogConfig, writer io.Writer) error {
	if client == nil {
		return fmt.Errorf("nil core-v2 protocol client")
	}
	if strings.TrimSpace(cfg.TerminalID) == "" {
		return fmt.Errorf("terminal id is required")
	}
	status, err := client.HistoryBacklogStatus(ctx, cfg.TerminalID)
	if err != nil {
		return err
	}
	writeV3HistoryBacklogTSV(writer, status)
	return nil
}

func writeV3HistoryBacklogTSV(writer io.Writer, status *protocol.HistoryBacklogStatus) {
	if status == nil {
		status = &protocol.HistoryBacklogStatus{}
	}
	fmt.Fprintln(writer, "terminal_id\thistory_enabled\tapplied_seq\ttarget_seq\tcatchup_pending\tpending_transactions\tpending_bytes\tbackpressure_mode\tbuffer_limit_bytes\tbackpressure_events\tbackpressure_wait_nanos\tin_flight\tclosed")
	fmt.Fprintf(writer, "%s\t%t\t%d\t%d\t%t\t%d\t%d\t%s\t%d\t%d\t%d\t%t\t%t\n",
		status.TerminalID,
		status.HistoryEnabled,
		status.AppliedSeq,
		status.TargetSeq,
		status.CatchupPending,
		status.PendingTransactions,
		status.PendingBytes,
		status.BackpressureMode,
		status.BufferLimitBytes,
		status.BackpressureEvents,
		status.BackpressureWaitNanos,
		status.InFlight,
		status.Closed,
	)
}
