package termxcorev2

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func BenchmarkR392HistoryJournalIngest100K(b *testing.B) {
	const lines = 100_000
	journals, bytes := r392OrdinaryStressJournals(lines, terminalLiveIngestBatchMaxBytes)
	b.ReportAllocs()
	b.SetBytes(int64(bytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		store := history.NewInMemoryHistoryStore("term-r392-bench")
		terminal := newR392BenchmarkTerminal(store)
		b.StartTimer()
		terminal.ingestHistoryJournals(journals)
	}
}

func BenchmarkR392HistoryJournalFileBackedIngest100K(b *testing.B) {
	const lines = 100_000
	journals, bytes := r392OrdinaryStressJournals(lines, terminalLiveIngestBatchMaxBytes)
	root := b.TempDir()
	b.ReportAllocs()
	b.SetBytes(int64(bytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir := filepath.Join(root, fmt.Sprintf("run-%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatalf("mkdir backend: %v", err)
		}
		backend, err := history.NewFileStorageBackend(dir, "term-r392-file-bench")
		if err != nil {
			b.Fatalf("file backend: %v", err)
		}
		store := history.NewBackendHistoryStore("term-r392-bench", backend)
		terminal := newR392BenchmarkTerminal(store)
		b.StartTimer()
		terminal.ingestHistoryJournals(journals)
	}
}

func newR392BenchmarkTerminal(store history.HistoryStore) *Terminal {
	_, journalRenderer := history.NewHistoryRenderers(nil, nil)
	return &Terminal{
		info:            TerminalInfo{ID: "term-r392-bench", Command: []string{"shell"}, Size: Size{Cols: 120, Rows: 30}, State: TerminalStateRunning},
		journalRenderer: journalRenderer,
		historyStore:    store,
		historyEnabled:  true,
	}
}

func r392OrdinaryStressJournals(lines int, targetBytes int) ([]history.HistoryJournal, int) {
	var journals []history.HistoryJournal
	var current []history.JournalLogicalLine
	var currentBytes int
	totalBytes := 0
	flush := func(seq uint64) {
		if len(current) == 0 {
			return
		}
		batch := history.OrdinaryLineBatch{
			Cols:   120,
			Lines:  append([]history.JournalLogicalLine(nil), current...),
			Origin: history.HistoryJournalOriginOrdinaryPrimary,
		}
		journals = append(journals, history.HistoryJournal{
			TerminalID: "term-r392-bench",
			Seq:        seq,
			Size:       history.TerminalSemanticSize{Cols: 120, Rows: 30},
			Source:     history.HistoryJournalSourceSemanticTapTransaction,
			Items: []history.HistoryJournalItem{{
				Kind:     history.HistoryJournalItemOrdinaryLineBatch,
				Ordinary: &batch,
			}},
		})
		current = nil
		currentBytes = 0
	}
	for line := 1; line <= lines; line++ {
		stressLine := r392OrdinaryStressLine(line)
		totalBytes += len(stressLine) + 1
		current = append(current, history.JournalLogicalLine{
			Cells:  r392StyledCells(stressLine, line),
			Origin: history.HistoryJournalOriginOrdinaryPrimary,
		})
		currentBytes += len(stressLine) + 1
		if targetBytes > 0 && currentBytes >= targetBytes {
			flush(uint64(len(journals) + 1))
		}
	}
	flush(uint64(len(journals) + 1))
	return journals, totalBytes
}

func r392OrdinaryStressLine(line int) string {
	return fmt.Sprintf(
		"%06d [INFO  ] history  ingest    id=%06d lat=%03dms q=%d bytes=%d mode=raw cursor=%d:%d rev=%d %s",
		line,
		line,
		(line*37)%1000,
		(line*97)%8192,
		64+(line*211)%65535,
		(line*13)%220,
		(line*17)%120,
		1+(line*19)%4096,
		strings.Repeat("payload", 31),
	)
}

func r392StyledCells(text string, line int) []history.Cell {
	parts := strings.Fields(text)
	cells := make([]history.Cell, 0, len(parts)*2)
	for index, part := range parts {
		style := history.CellStyle{}
		if index%3 == 0 {
			style.FG = fmt.Sprintf("idx:%d", 33+(line+index)%180)
			style.Bold = true
		}
		if index%7 == 0 {
			style.BG = fmt.Sprintf("idx:%d", 17+(line+index)%80)
		}
		if index > 0 {
			cells = append(cells, history.Cell{Text: " ", Width: 1})
		}
		cells = append(cells, history.Cell{Text: part, Width: len(part), Style: style})
	}
	return cells
}
