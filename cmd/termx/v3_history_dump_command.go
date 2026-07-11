package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/spf13/cobra"
)

type v3HistoryDumpConfig struct {
	TerminalID string
	OutPath    string
	Cols       int
	Limit      int
	All        bool
}

func v3HistoryDumpCommand(socket *string, logFile *string) *cobra.Command {
	cfg := v3HistoryDumpConfig{
		Cols:  120,
		Limit: 512,
		All:   true,
	}
	cmd := &cobra.Command{
		Use:   "history-dump <terminal-id>",
		Short: "Dump core-v2 authoritative terminal history to a file",
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
			if err := runV3HistoryDump(cmd.Context(), client, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "termx v3 history dump ok: terminal=%s out=%s\n", cfg.TerminalID, cfg.OutPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfg.OutPath, "out", "o", "", "file path to write the authoritative history dump")
	cmd.Flags().IntVar(&cfg.Cols, "cols", cfg.Cols, "projection columns for logical-line wrapping")
	cmd.Flags().IntVar(&cfg.Limit, "limit", cfg.Limit, "rows per authoritative history.window request")
	cmd.Flags().BoolVar(&cfg.All, "all", cfg.All, "page older windows until core reports no older rows")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func runV3HistoryDump(ctx context.Context, client *protocol.Client, cfg v3HistoryDumpConfig) error {
	if client == nil {
		return fmt.Errorf("nil core-v2 protocol client")
	}
	if strings.TrimSpace(cfg.TerminalID) == "" {
		return fmt.Errorf("terminal id is required")
	}
	if strings.TrimSpace(cfg.OutPath) == "" {
		return fmt.Errorf("history dump output path is required")
	}
	if cfg.Cols <= 0 {
		return fmt.Errorf("history dump cols must be positive")
	}
	if cfg.Limit <= 0 {
		return fmt.Errorf("history dump limit must be positive")
	}
	file, err := os.Create(cfg.OutPath)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)

	latest, err := client.HistoryWindow(ctx, protocol.HistoryWindowParams{
		TerminalID: cfg.TerminalID,
		Cols:       cfg.Cols,
		Limit:      cfg.Limit,
		Mode:       "latest",
	})
	if err != nil {
		return err
	}
	if latest.Token != "" {
		defer func() {
			// 中文说明：history-dump 只观察 core authoritative window；
			// release 失败只影响诊断 token 生命周期，不改变已写出的 dump 内容。
			_ = client.ReleaseHistory(context.Background(), protocol.HistoryWindowParams{
				TerminalID: cfg.TerminalID,
				Token:      latest.Token,
			})
		}()
	}

	if err := writeV3HistoryDumpHeader(writer, cfg, latest); err != nil {
		return err
	}
	pages := []*protocol.HistoryWindow{latest}
	if cfg.All {
		olderCursor := latest
		for {
			if olderCursor == nil || !olderCursor.CursorValid {
				break
			}
			older, err := client.HistoryWindow(ctx, protocol.HistoryWindowParams{
				TerminalID:          cfg.TerminalID,
				Cols:                cfg.Cols,
				Limit:               cfg.Limit,
				Mode:                "older",
				Token:               latest.Token,
				Generation:          latest.Generation,
				CursorValid:         olderCursor.CursorValid,
				BeforeLineID:        olderCursor.CursorLineID,
				BeforeRowInLine:     olderCursor.CursorRow,
				BeforeRowIndex:      olderCursor.CursorRowIndex,
				CursorSegment:       olderCursor.CursorSegment,
				BoundaryFirstLineID: olderCursor.FirstLineID,
				BoundaryLastLineID:  latest.LastLineID,
			})
			if err != nil {
				return err
			}
			if len(older.Rows) == 0 {
				break
			}
			pages = append(pages, older)
			olderCursor = older
		}
	}
	globalRow := 0
	for pageIndex := len(pages) - 1; pageIndex >= 0; pageIndex-- {
		var nextRow int
		nextRow, err = writeV3HistoryDumpWindow(writer, len(pages)-1-pageIndex, pages[pageIndex], globalRow)
		if err != nil {
			return err
		}
		globalRow = nextRow
	}
	return writer.Flush()
}

func writeV3HistoryDumpHeader(writer *bufio.Writer, cfg v3HistoryDumpConfig, latest *protocol.HistoryWindow) error {
	fmt.Fprintf(writer, "# termx core-v2 authoritative history dump\n")
	fmt.Fprintf(writer, "# terminal_id=%s cols=%d limit=%d all=%v\n", cfg.TerminalID, cfg.Cols, cfg.Limit, cfg.All)
	fmt.Fprintf(writer, "# output_order=oldest-to-newest\n")
	if latest != nil {
		fmt.Fprintf(writer, "# latest_token=%s latest_generation=%d latest_boundary=%d..%d latest_cursor_valid=%v latest_cursor=%s/%d:%d index=%d logical_total=%d\n",
			latest.Token,
			latest.Generation,
			latest.FirstLineID,
			latest.LastLineID,
			latest.CursorValid,
			latest.CursorSegment,
			latest.CursorLineID,
			latest.CursorRow,
			latest.CursorRowIndex,
			latest.LogicalTotal,
		)
	}
	fmt.Fprintln(writer)
	return writer.Flush()
}

func writeV3HistoryDumpWindow(writer *bufio.Writer, index int, window *protocol.HistoryWindow, globalRow int) (int, error) {
	if window == nil {
		return globalRow, nil
	}
	fmt.Fprintf(writer, "## window %d op=%s rows=%d token=%s generation=%d boundary=%d..%d cursor_valid=%v cursor=%s/%d:%d index=%d has_more=%v logical_total=%d\n",
		index,
		window.Op,
		len(window.Rows),
		window.Token,
		window.Generation,
		window.FirstLineID,
		window.LastLineID,
		window.CursorValid,
		window.CursorSegment,
		window.CursorLineID,
		window.CursorRow,
		window.CursorRowIndex,
		window.HasMore,
		window.LogicalTotal,
	)
	for rowIndex, row := range window.Rows {
		lineID := rowMetaUint64(window.RowLineIDs, rowIndex)
		rowInLine := rowMetaInt(window.RowInLine, rowIndex)
		projectionIndex := rowMetaInt(window.RowIndexes, rowIndex)
		segment := rowMetaString(window.RowSegments, rowIndex)
		kind := rowMetaString(window.RowKinds, rowIndex)
		text := compactProtocolRowText(row)
		fmt.Fprintf(writer, "%06d page_row=%d projection=%d line=%d row=%d segment=%s kind=%s text=%q\n", globalRow, rowIndex, projectionIndex, lineID, rowInLine, segment, kind, text)
		globalRow++
	}
	fmt.Fprintln(writer)
	return globalRow, nil
}

func compactProtocolRowText(row protocol.CompactRow) string {
	if row.Text != "" {
		return row.Text
	}
	var builder strings.Builder
	for _, run := range row.Runs {
		builder.WriteString(run.Text)
	}
	if builder.Len() > 0 {
		return builder.String()
	}
	for _, cell := range row.Cells {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func rowMetaString(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func rowMetaUint64(values []uint64, index int) uint64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func rowMetaInt(values []int, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}
