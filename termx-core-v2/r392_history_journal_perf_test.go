package termxcorev2

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func BenchmarkR407HistoryIngestQueueDrain100K(b *testing.B) {
	const lines = 100_000
	journals, bytes := r392OrdinaryStressJournals(lines, terminalLiveIngestBatchMaxBytes)
	b.ReportAllocs()
	b.SetBytes(int64(bytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		queue := newTerminalHistoryIngestQueue(0)
		for _, journal := range journals {
			if !queue.Enqueue(journal) {
				b.Fatal("enqueue rejected before close")
			}
		}
		b.StartTimer()
		var drained int
		for drained < len(journals) {
			batch, ok := queue.nextBatch()
			if !ok {
				b.Fatal("queue closed before drain")
			}
			drained += len(batch)
			queue.finishBatch(terminalHistoryIngestBatchCompleteSeq(batch))
		}
	}
}

func BenchmarkR407HistoryTapPipelineChunked100K(b *testing.B) {
	r407BenchmarkHistoryTapPipeline(b, terminalLiveIngestBatchMaxBytes)
}

func BenchmarkR407HistoryTapPipelineCoalesced100K(b *testing.B) {
	r407BenchmarkHistoryTapPipeline(b, terminalHistoryTapIngestBatchMaxBytes)
}

func TestR407RealStressPayloadHistoryTapPipelineKeepsAllLines(t *testing.T) {
	if testing.Short() {
		t.Skip("real stress payload harness is skipped in short mode")
	}
	payload := r407GenerateTerminalStressPayload(t, 10_000, 407104)
	backend, err := history.NewFileStorageBackend(t.TempDir(), "term-r407-real-stress")
	if err != nil {
		t.Fatalf("file backend: %v", err)
	}
	store := history.NewBackendHistoryStore("term-r407-real-stress", backend)
	terminal := newR392BenchmarkTerminal(store)
	tap := NewSemanticTap("term-r407-real-stress", Size{Cols: 100, Rows: 30}, nil)

	for remaining := payload; len(remaining) > 0; {
		head, tail := splitLiveIngestPayload(remaining, terminalHistoryTapIngestBatchMaxBytes)
		result, err := tap.ApplyPTYWrite([]byte(head))
		if err != nil {
			t.Fatalf("tap write: %v", err)
		}
		terminal.ingestHistoryJournals([]history.HistoryJournal{result.HistoryJournal()})
		remaining = tail
	}

	window, err := store.LatestWindow(history.HistoryWindowRequest{TerminalID: "term-r407-real-stress", Mode: history.HistoryWindowModeLatest, Cols: 100, Limit: 16})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if window.LogicalTotal < 10_001 {
		t.Fatalf("real stress payload should preserve every generated logical line, total=%d latest=%q", window.LogicalTotal, strings.Join(historyRowTexts(window.Rows), "|"))
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); !strings.Contains(got, "010000 ") {
		t.Fatalf("latest page must include final stress line 010000, got %q", got)
	}
	oldest, err := store.OldestWindow(history.HistoryWindowRequest{TerminalID: "term-r407-real-stress", Mode: history.HistoryWindowModeOldest, Cols: 100, Limit: 3})
	if err != nil {
		t.Fatalf("oldest window: %v", err)
	}
	if got := strings.Join(historyRowTexts(oldest.Rows), "|"); !strings.Contains(got, "000000 ") || !strings.Contains(got, "000001 ") {
		t.Fatalf("oldest page must include stress head, got %q", got)
	}
}

func r407BenchmarkHistoryTapPipeline(b *testing.B, batchBytes int) {
	const lines = 100_000
	payload, bytes := r407OrdinaryStressPayload(lines)
	b.ReportAllocs()
	b.SetBytes(int64(bytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		backend, err := history.NewFileStorageBackend(b.TempDir(), fmt.Sprintf("term-r407-tap-%d", batchBytes))
		if err != nil {
			b.Fatalf("file backend: %v", err)
		}
		store := history.NewBackendHistoryStore("term-r407-tap", backend)
		terminal := newR392BenchmarkTerminal(store)
		tap := NewSemanticTap("term-r407-tap", Size{Cols: 120, Rows: 30}, nil)
		b.StartTimer()
		remaining := payload
		for len(remaining) > 0 {
			head, tail := splitLiveIngestPayload(remaining, batchBytes)
			result, err := tap.ApplyPTYWrite([]byte(head))
			if err != nil {
				b.Fatalf("tap write: %v", err)
			}
			terminal.ingestHistoryJournals([]history.HistoryJournal{result.HistoryJournal()})
			remaining = tail
		}
	}
}

func r407GenerateTerminalStressPayload(t *testing.T, lines int, seed int) string {
	t.Helper()
	python := os.Getenv("PYTHON")
	if python == "" {
		python = "python3"
	}
	if runtime.GOOS == "windows" {
		t.Skip("stress generator path is POSIX-oriented")
	}
	cmd := exec.Command(python, "../scripts/generate_terminal_stress.py", "--lines", fmt.Sprint(lines), "--seed", fmt.Sprint(seed))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("generate terminal stress payload: %v", err)
	}
	return string(output)
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

func r407OrdinaryStressPayload(lines int) (string, int) {
	var payload strings.Builder
	for line := 1; line <= lines; line++ {
		stressLine := r392OrdinaryStressLine(line)
		payload.WriteString(stressLine)
		payload.WriteString("\r\n")
	}
	return payload.String(), payload.Len()
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
