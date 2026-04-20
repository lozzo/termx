package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkdirSuggestionPopupListsMatchingDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "alphabet", "beta"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	prefix := filepath.Join(root, "al")
	title, items, empty := workdirSuggestionPopup(prefix, len([]rune(prefix)))
	if !strings.Contains(title, root) {
		t.Fatalf("title = %q, want path rooted at %q", title, root)
	}
	if empty != "" {
		t.Fatalf("empty = %q, want empty", empty)
	}
	if len(items) < 2 {
		t.Fatalf("items = %#v, want at least two matches", items)
	}
	if got, want := items[0], filepath.Join(root, "alpha")+string(filepath.Separator); got != want {
		t.Fatalf("first item = %q, want %q", got, want)
	}
}

func TestWorkdirSuggestionPopupShowsExactPathChildren(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "workspace")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.Mkdir(filepath.Join(target, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	title, items, empty := workdirSuggestionPopup(target+string(filepath.Separator), len([]rune(target+string(filepath.Separator))))
	if !strings.Contains(title, target) {
		t.Fatalf("title = %q, want to mention %q", title, target)
	}
	if empty != "" {
		t.Fatalf("empty = %q, want empty", empty)
	}
	if got, want := items[0], target+string(filepath.Separator)+"logs"+string(filepath.Separator); got != want {
		t.Fatalf("first item = %q, want %q", got, want)
	}
}

func TestWorkdirSuggestionPopupReportsMissingPath(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing", "al")
	title, items, empty := workdirSuggestionPopup(missing, len([]rune(missing)))
	if !strings.Contains(title, filepath.Join(root, "missing")) {
		t.Fatalf("title = %q, want missing path", title)
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want none", items)
	}
	if empty != "(path not found)" {
		t.Fatalf("empty = %q, want path-not-found marker", empty)
	}
}
