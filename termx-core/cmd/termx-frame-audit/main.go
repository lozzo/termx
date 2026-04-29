package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/lozzow/termx/termx-core/frameaudit"
)

func main() {
	var (
		dumpPath = flag.String("dump", "", "path to TERMX_FRAME_DUMP file")
		width    = flag.Int("width", 120, "terminal width used to replay frames")
		height   = flag.Int("height", 40, "terminal height used to replay frames")
		autoSize = flag.Bool("auto-size", false, "expand replay size to the minimum inferred from cursor-addressed payload bounds")
		asJSON   = flag.Bool("json", false, "print full report as JSON")
	)
	flag.Parse()

	if *dumpPath == "" {
		fatalf("missing -dump")
	}

	data, err := os.ReadFile(*dumpPath)
	if err != nil {
		fatalf("read dump: %v", err)
	}
	entries, err := frameaudit.ParseDump(data)
	if err != nil {
		fatalf("parse dump: %v", err)
	}
	suggestedWidth, suggestedHeight := frameaudit.SuggestedReplaySize(entries)
	if *autoSize {
		if suggestedWidth > *width {
			*width = suggestedWidth
		}
		if suggestedHeight > *height {
			*height = suggestedHeight
		}
	}
	report, err := frameaudit.AuditEntries(entries, *width, *height)
	if err != nil {
		fatalf("audit dump: %v", err)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fatalf("encode json: %v", err)
		}
		return
	}

	fmt.Printf("size=%dx%d entries=%d bytes=%d noops=%d noop_bytes=%d screen_changed=%d cursor_only=%d changed_rows=%d changed_cells=%d\n",
		report.Width,
		report.Height,
		report.Summary.Entries,
		report.Summary.Bytes,
		report.Summary.Noops,
		report.Summary.NoopBytes,
		report.Summary.ScreenChangedEntries,
		report.Summary.CursorOnlyEntries,
		report.Summary.ChangedRows,
		report.Summary.ChangedCells,
	)
	if suggestedWidth > report.Width || suggestedHeight > report.Height {
		fmt.Printf("warning=payload references exceed replay size suggested_min_size=%dx%d\n", suggestedWidth, suggestedHeight)
	}

	type kindRow struct {
		kind string
		data frameaudit.KindSummary
	}
	var kinds []kindRow
	for kind, summary := range report.Summary.ByKind {
		kinds = append(kinds, kindRow{kind: kind, data: summary})
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i].kind < kinds[j].kind })
	for _, kind := range kinds {
		fmt.Printf("kind=%s entries=%d bytes=%d noops=%d noop_bytes=%d screen_changed=%d cursor_only=%d changed_rows=%d changed_cells=%d\n",
			kind.kind,
			kind.data.Entries,
			kind.data.Bytes,
			kind.data.Noops,
			kind.data.NoopBytes,
			kind.data.ScreenChangedEntries,
			kind.data.CursorOnlyEntries,
			kind.data.ChangedRows,
			kind.data.ChangedCells,
		)
	}

	fmt.Println("top_entries:")
	top := append([]frameaudit.EntryStats(nil), report.Entries...)
	sort.Slice(top, func(i, j int) bool {
		if top[i].Bytes != top[j].Bytes {
			return top[i].Bytes > top[j].Bytes
		}
		return top[i].Index < top[j].Index
	})
	if len(top) > 12 {
		top = top[:12]
	}
	for _, entry := range top {
		fmt.Printf("  #%d kind=%s bytes=%d noop=%t screen_changed=%t cursor_changed=%t changed_rows=%d changed_cells=%d\n",
			entry.Index,
			entry.Kind,
			entry.Bytes,
			entry.Noop,
			entry.ScreenChanged,
			entry.CursorChanged,
			entry.ChangedRows,
			entry.ChangedCells,
		)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
