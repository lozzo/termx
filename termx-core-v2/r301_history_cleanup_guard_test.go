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
	if !strings.Contains(workerSource, "SemanticTap 之后的 compact history journal backlog") {
		t.Fatal("history backlog must be documented as post-SemanticTap compact journal queue")
	}
	for _, forbidden := range []string{"terminalHistoryIngestSpool", "os.CreateTemp", "io.WriteString", "[]string) error", "Enqueue(text string)", "TerminalSemanticTransaction", "cloneSemanticTapTransaction"} {
		if strings.Contains(workerSource, forbidden) {
			t.Fatalf("history backlog must not retain raw PTY replay, spool, or full transaction queue path: %s", forbidden)
		}
	}
	terminalSource := r301ReadFile(t, "terminal.go")
	for _, forbidden := range []string{"historySemantic", "splitTerminalHistorySemanticWrites", "consumeTerminalHistoryPrivateCSI", "historyWorker.Enqueue(text)"} {
		if strings.Contains(terminalSource, forbidden) {
			t.Fatalf("terminal production path must not retain history raw replay/double-vterm code: %s", forbidden)
		}
	}
	if !strings.Contains(terminalSource, "enqueueOrApplyProcessHistoryJournalTransaction(result.Transaction(), historyWorker, info.ID)") {
		t.Fatal("terminal production path must fan out tap transaction through journal backlog gate")
	}
	if !strings.Contains(terminalSource, "HistoryJournalFromTransaction(terminalID, tx)") {
		t.Fatal("terminal production path must enqueue compact HistoryJournal, not full transaction")
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
	var files []string
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
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
		t.Fatalf("walk production files: %v", err)
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
