package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func refreshPromptCompletions(shell state.ShellStore) state.ShellStore {
	shell = shell.EnsureDefaults()
	prompt := shell.Overlay.Prompt
	field := prompt.ActivePromptField()
	if field == nil || strings.TrimSpace(field.Key) != "workdir" {
		return shell.ClearPromptSuggestions()
	}
	title, items, empty := workdirSuggestionPopup(field.Value, field.Cursor)
	return shell.SetActivePromptSuggestions(title, items, empty)
}

func workdirSuggestionPopup(value string, cursor int) (string, []string, string) {
	runes := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	prefix := string(runes[:cursor])
	if strings.TrimSpace(prefix) == "" {
		return "", nil, ""
	}
	baseDisplay, baseResolved, fragment, ok := workdirCompletionBase(prefix)
	if !ok {
		return "", nil, ""
	}
	title := "path: " + baseResolved
	entries, err := os.ReadDir(baseResolved)
	if err != nil {
		return title, nil, "(path not found)"
	}
	fragmentLower := strings.ToLower(fragment)
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(fragment, ".") {
			continue
		}
		if fragmentLower != "" && !strings.HasPrefix(strings.ToLower(name), fragmentLower) {
			continue
		}
		label := name + string(filepath.Separator)
		if baseDisplay != "" {
			label = baseDisplay + label
		}
		items = append(items, label)
	}
	sort.Strings(items)
	if len(items) == 0 {
		return title, nil, "(no matching directories)"
	}
	return title, items, ""
}

func workdirCompletionBase(prefix string) (string, string, string, bool) {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", false
	}
	switch {
	case prefix == "~":
		if strings.TrimSpace(home) == "" {
			return "", "", "", false
		}
		return "~/", home, "", true
	case strings.HasPrefix(prefix, "~/"):
		rest := strings.TrimPrefix(prefix, "~/")
		base, fragment := splitPathPrefix(rest)
		return "~/" + base, filepath.Join(home, filepath.FromSlash(base)), fragment, true
	case strings.HasPrefix(prefix, "/"):
		base, fragment := splitPathPrefix(strings.TrimPrefix(prefix, "/"))
		return "/" + base, filepath.Join(string(filepath.Separator), filepath.FromSlash(base)), fragment, true
	default:
		base, fragment := splitPathPrefix(prefix)
		return base, filepath.Join(cwd, filepath.FromSlash(base)), fragment, true
	}
}

func splitPathPrefix(prefix string) (string, string) {
	lastSlash := strings.LastIndex(prefix, "/")
	if lastSlash < 0 {
		return "", prefix
	}
	return prefix[:lastSlash+1], prefix[lastSlash+1:]
}
