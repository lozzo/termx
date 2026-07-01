package termxcorev2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestR301HistoryCleanupGuard(t *testing.T) {
	productionGoFiles := r301ProductionGoFiles(t)
	for _, path := range productionGoFiles {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				r301RejectProgramNameLiteral(t, path, typed.Name)
			case *ast.BasicLit:
				if typed.Kind == token.STRING {
					value, err := strconv.Unquote(typed.Value)
					if err != nil {
						value = typed.Value
					}
					r301RejectProgramNameLiteral(t, path, value)
				}
			}
			return true
		})
	}

	for _, removed := range []string{
		"terminal_semantic_ingest.go",
		"terminal_history_pipeline.go",
		"terminal_history_queue.go",
		"history_ingest.go",
		"history_projector.go",
	} {
		if _, err := os.Stat(removed); err == nil {
			t.Fatalf("old history implementation file must stay deleted: %s", removed)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", removed, err)
		}
	}

	terminalSource := r301ReadFile(t, "terminal.go")
	if strings.Contains(terminalSource, "historyQueue.Enqueue(text)") || strings.Contains(terminalSource, "historyANSIParser") || strings.Contains(terminalSource, "terminalHistoryPipeline") {
		t.Fatal("real PTY output must not re-enter old raw parser history queue/pipeline")
	}
	workerSource := r301ReadFile(t, "terminal_history_ingest_worker.go")
	if !strings.Contains(workerSource, "terminalHistoryIngestQueue") {
		t.Fatal("R358 history semantic worker guard must cover the ingest worker")
	}
}

func TestR385TerminalHistoryBacklogOnlyAcceptsSemanticTapJournals(t *testing.T) {
	workerSource := r301ReadFile(t, "terminal_history_ingest_worker.go")
	if !strings.Contains(workerSource, "compact history journal backlog") {
		t.Fatal("history backlog must be documented as compact journal queue")
	}
	for _, forbidden := range []string{"terminalHistoryIngestSpool", "os.CreateTemp", "io.WriteString", "[]string) error", "Enqueue(text string)", "TerminalSemanticTransaction", "cloneSemanticTapTransaction"} {
		if strings.Contains(workerSource, forbidden) {
			t.Fatalf("history backlog must not retain raw PTY replay, spool, or full transaction queue path: %s", forbidden)
		}
	}
	terminalSource := r301ReadFile(t, "terminal.go")
	for _, forbidden := range []string{"splitTerminalHistorySemanticWrites", "consumeTerminalHistoryPrivateCSI", "historyWorker.Enqueue(text)", "terminalLiveRowsFromNativeSnapshot("} {
		if strings.Contains(terminalSource, forbidden) {
			t.Fatalf("terminal production path must not retain history raw parser or live-snapshot history code: %s", forbidden)
		}
	}
	if !strings.Contains(terminalSource, "terminal.live = live.NewSurfaceTrackWithOptions") || !strings.Contains(terminalSource, "terminal.tap = NewSemanticTap(info.ID, info.Size, nil)") {
		t.Fatal("R396 production path must split live SurfaceTrack and response-less history SemanticTap owners")
	}
	if !strings.Contains(terminalSource, "enqueueOrApplyProcessHistoryJournal(result, historyWorker, terminalID)") {
		t.Fatal("terminal history semantic path must fan out tap result through journal backlog gate")
	}
	if !strings.Contains(terminalSource, "history.HistoryJournalFromDecision") {
		t.Fatal("terminal production path must build decision-aware compact HistoryJournal")
	}
	for _, forbidden := range []string{
		"applyHistoryJournalFastPathLocked",
		"historyJournalAllowsTerminalFastPath",
		"ingestHistoryTransactions",
		"applyHistoryTransactionLocked",
	} {
		if strings.Contains(terminalSource, forbidden) {
			t.Fatalf("R404 production history path must not retain journal/full-transaction fallback gate: %s", forbidden)
		}
	}
}

func TestR386ProcessHistorySemanticHotPathUsesJournalBeforeTransaction(t *testing.T) {
	terminalSource := r301ReadFile(t, "terminal.go")
	start := strings.Index(terminalSource, "func (terminal *Terminal) ingestHistorySemanticOutput")
	if start < 0 {
		t.Fatal("missing ingestHistorySemanticOutput production hot path")
	}
	end := strings.Index(terminalSource[start:], "func (terminal *Terminal) markExited")
	if end < 0 {
		t.Fatal("missing ingestHistorySemanticOutput boundary")
	}
	body := terminalSource[start : start+end]
	if !strings.Contains(body, "enqueueOrApplyProcessHistoryJournal(result, historyWorker, terminalID)") {
		t.Fatal("process history semantic hot path must pass SemanticTapResult to journal-first history gate")
	}
	if strings.Contains(body, "result.Transaction()") {
		t.Fatal("process history semantic hot path must not pull full transaction before journal gate")
	}
	gateBody := r301SourceFunctionBody(t, terminalSource, "func (terminal *Terminal) enqueueOrApplyProcessHistoryJournal")
	journalIndex := strings.Index(gateBody, "result.HistoryJournal()")
	transactionIndex := strings.Index(gateBody, "result.Transaction()")
	if journalIndex < 0 || transactionIndex < 0 || journalIndex > transactionIndex {
		t.Fatal("journal fanout gate must inspect compact journal before pulling full transaction for classifier")
	}
}

func TestR403HistoryRebuildCleanupRejectsTraceAndDisplayAnchors(t *testing.T) {
	for _, path := range append(r301ProductionGoFiles(t), r301TUIProductionGoFiles(t)...) {
		source := r301ReadFile(t, path)
		for _, forbidden := range []string{
			"TERMX_HISTORY_TRACE",
			"coreHistoryTrace",
			"HistoryTraceWindowSummary",
			"ViewportTopPadding",
			"liveSurfaceMatchedViewport",
			"copyHistoryViewportTopPadding",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("R403 cleanup forbids history trace/display-anchor patch %q in %s", forbidden, path)
			}
		}
	}
}

func TestR404JournalRendererRejectsUnsupportedFallbackContract(t *testing.T) {
	historySource := r301ReadFile(t, "history/journal_renderer.go")
	for _, forbidden := range []string{
		"ErrHistoryJournalUnsupported",
		"supportsJournal",
		"journalBoundaryScanState",
		"journalFrameEventIsBoundaryOnly",
	} {
		if strings.Contains(historySource, forbidden) {
			t.Fatalf("R404 journal renderer must not keep unsupported/fallback pre-scan contract: %s", forbidden)
		}
	}
}

func TestR405TerminalResizeUsesDecisionAwareBoundaryJournal(t *testing.T) {
	terminalSource := r301ReadFile(t, "terminal.go")
	body := r301SourceFunctionBody(t, terminalSource, "func (terminal *Terminal) enqueueOrApplyHistoryResizeTransaction")
	if !strings.Contains(body, "history.HistoryJournalFromDecision") || !strings.Contains(body, "HistoryOutputModeBoundaryOnly") || !strings.Contains(body, "NonHistoryBoundary: true") {
		t.Fatal("resize history ingest must enqueue a decision-aware boundary-only journal")
	}
	if strings.Contains(body, "HistoryJournalFromTransaction") || strings.Contains(body, "applyHistoryResizeTransaction") {
		t.Fatal("resize history ingest must not use default transaction journal or separate renderer path")
	}
}

func TestR405TerminalLifecycleCloseDoesNotPretendToBePTY(t *testing.T) {
	terminalSource := r301ReadFile(t, "terminal.go")
	body := r301SourceFunctionBody(t, terminalSource, "func (terminal *Terminal) forceCloseHistory")
	if !strings.Contains(body, "historyRenderer.Close(reason)") {
		t.Fatal("terminal lifecycle close must seal history through renderer.Close(reason)")
	}
	for _, forbidden := range []string{
		"HistoryJournalFromTransaction",
		"HistoryJournalFromDecision",
		"ApplyPTYWrite",
		"NativeScreenSnapshot",
		"terminal.live",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("terminal lifecycle close must not forge PTY/journal/live snapshot input: %s", forbidden)
		}
	}
}

func r301RejectProgramNameLiteral(t *testing.T, path string, text string) {
	t.Helper()
	for _, forbidden := range []string{"Codex", "codex", "Claude", "claude", "htop", "vim"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("production history path %s must not branch on program name %q", path, forbidden)
		}
	}
}

func r301ProductionGoFiles(t *testing.T) []string {
	t.Helper()
	return r301GoFilesUnder(t, ".")
}

func r301TUIProductionGoFiles(t *testing.T) []string {
	t.Helper()
	return r301GoFilesUnder(t, "../termx-tui-v3")
}

func r301GoFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "docs" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk production files under %s: %v", root, err)
	}
	return files
}

func r301ReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func r301SourceFunctionBody(t *testing.T, source string, signature string) string {
	t.Helper()
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("source missing function %q", signature)
	}
	rest := source[start+len(signature):]
	next := strings.Index(rest, "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:next]
}
