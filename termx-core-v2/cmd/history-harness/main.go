package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("history-harness", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cols := fs.Int("cols", 80, "terminal columns")
	rows := fs.Int("rows", 24, "terminal rows")
	scrollback := fs.Int("scrollback", 10000, "vterm scrollback size")
	inputPath := fs.String("in", "", "ANSI input file, or stdin when empty/-")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := *inputPath
	if path == "" && fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	payload, err := readInput(path, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "history-harness: read input: %v\n", err)
		return 1
	}
	if err := dumpANSI(stdout, payload, *cols, *rows, *scrollback); err != nil {
		fmt.Fprintf(stderr, "history-harness: %v\n", err)
		return 1
	}
	return 0
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func dumpANSI(out io.Writer, payload []byte, cols int, rows int, scrollback int) error {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	source := vterm.NewSemanticSource(cols, rows, scrollback, nil)
	buffer := history.NewScreenHistoryBuffer(cols, rows)
	tx, err := source.ApplyPTYWrite(payload)
	if err != nil {
		return fmt.Errorf("vterm semantic source: %w", err)
	}
	// 中文说明：debug harness 也必须走正式 terminal semantic transaction；
	// 不能为了观察方便从 raw ANSI 或 live screen diff 反推 history truth。
	if err := buffer.ApplyTransaction(tx); err != nil {
		return fmt.Errorf("screen buffer apply seq=%d: %w", tx.Seq, err)
	}
	projection := buffer.ProjectHistory()
	fmt.Fprintf(out, "# termx history harness cols=%d rows=%d seq=%d in_alt=%v\n", cols, rows, tx.Seq, buffer.InAlt)
	dumpPhysicalRows(out, "committed", buffer.CommittedRows())
	dumpPhysicalRows(out, activeScreenName(buffer), activeScreenRows(buffer))
	dumpProjection(out, projection)
	return nil
}

func activeScreenName(buffer *history.ScreenHistoryBuffer) string {
	if buffer != nil && buffer.InAlt {
		return "current-alt"
	}
	return "current-main"
}

func activeScreenRows(buffer *history.ScreenHistoryBuffer) []history.PhysicalRow {
	if buffer == nil {
		return nil
	}
	if buffer.InAlt && buffer.Alt != nil {
		rows := make([]history.PhysicalRow, len(buffer.Alt.Rows))
		copy(rows, buffer.Alt.Rows)
		return rows
	}
	return buffer.VisibleRows()
}

func dumpPhysicalRows(out io.Writer, section string, rows []history.PhysicalRow) {
	fmt.Fprintf(out, "\n# %s physical rows\n", section)
	for index, row := range rows {
		if row.Text() == "" && !row.Sealed && !row.Wrapped && !row.Continued {
			continue
		}
		fmt.Fprintf(out,
			"ROW section=%s index=%d row_id=%d version=%d owner=%s sealed=%v wrapped=%v continued=%v text=%s\n",
			section,
			index,
			row.ID,
			row.Version,
			rowOwnerString(row.OwnerKind),
			row.Sealed,
			row.Wrapped,
			row.Continued,
			strconv.Quote(row.Text()),
		)
	}
}

func dumpProjection(out io.Writer, projection history.ScreenHistoryProjection) {
	fmt.Fprintf(out, "\n# projected logical lines\n")
	for index, line := range projection.Lines {
		fmt.Fprintf(out,
			"LINE index=%d line_id=%d segment=%s kind=%s sealed=%v wrapped=%v continued=%v row_ids=%s versions=%s text=%s\n",
			index,
			line.LineID,
			line.Segment,
			line.Kind,
			line.Sealed,
			line.Wrapped,
			line.Continued,
			uintsString(line.RowIDs),
			uintsString(line.Versions),
			strconv.Quote(cellsText(line.Cells)),
		)
	}
	fmt.Fprintf(out, "\n# projected rows\n")
	for _, row := range projection.Rows {
		fmt.Fprintf(out,
			"PROW index=%d line_id=%d row_id=%d version=%d segment=%s kind=%s owner=%s sealed=%v wrapped=%v continued=%v screen_row=%d row_in_line=%d text=%s\n",
			row.ProjectionRowIndex,
			row.LineID,
			row.RowID,
			row.Version,
			row.Segment,
			row.Kind,
			rowOwnerString(row.OwnerKind),
			row.Sealed,
			row.Wrapped,
			row.Continued,
			row.ScreenRow,
			row.RowInLine,
			strconv.Quote(cellsText(row.Cells)),
		)
	}
}

func rowOwnerString(kind history.RowOwnerKind) string {
	switch kind {
	case history.RowOwnerShellStream:
		return "shell-stream"
	case history.RowOwnerPrimaryFrame:
		return "primary-frame"
	case history.RowOwnerAltScreen:
		return "alt-screen"
	case history.RowOwnerSystem:
		return "system"
	default:
		return "unknown"
	}
}

func cellsText(cells []history.Cell) string {
	var b strings.Builder
	for _, cell := range cells {
		b.WriteString(cell.Text)
	}
	return b.String()
}

func uintsString[T ~uint64](values []T) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.FormatUint(uint64(value), 10)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
