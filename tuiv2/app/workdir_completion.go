package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lozzow/termx/tuiv2/modal"
)

const promptSuggestionLimit = 6

func (m *Model) refreshPromptCompletions() {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil {
		return
	}
	prompt := m.modalHost.Prompt
	activeWorkdir := false
	for index := range prompt.Fields {
		field := &prompt.Fields[index]
		clearPromptFieldSuggestions(field)
		editor := field.ValueEditor()
		if editor == nil {
			continue
		}
		editor.ClearCompletion()
		if index != prompt.ActiveField || strings.TrimSpace(field.Key) != "workdir" {
			continue
		}
		activeWorkdir = true
		title, items, empty := workdirSuggestionPopup(editor.Value(), editor.Position())
		field.SuggestionTitle = title
		field.SuggestionItems = items
		field.SuggestionEmpty = empty
		if len(items) == 0 {
			prompt.PromptSuggestionFocused = false
			prompt.PromptSuggestionSelected = 0
			continue
		}
		if prompt.PromptSuggestionSelected < 0 {
			prompt.PromptSuggestionSelected = 0
		}
		if prompt.PromptSuggestionSelected >= len(items) {
			prompt.PromptSuggestionSelected = len(items) - 1
		}
	}
	if !activeWorkdir {
		prompt.PromptSuggestionFocused = false
		prompt.PromptSuggestionSelected = 0
	}
}

func clearPromptFieldSuggestions(field *modal.PromptField) {
	if field == nil {
		return
	}
	field.SuggestionTitle = ""
	field.SuggestionItems = nil
	field.SuggestionEmpty = ""
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
	if len(items) > promptSuggestionLimit {
		items = items[:promptSuggestionLimit]
	}
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

func promptActiveSuggestionItems(prompt *modal.PromptState) []string {
	if prompt == nil {
		return nil
	}
	field := prompt.ActivePromptField()
	if field == nil {
		return nil
	}
	return field.SuggestionItems
}

func promptHasSuggestionItems(prompt *modal.PromptState) bool {
	return len(promptActiveSuggestionItems(prompt)) > 0
}

func (m *Model) movePromptSuggestionSelection(delta int) bool {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil || delta == 0 {
		return false
	}
	items := promptActiveSuggestionItems(m.modalHost.Prompt)
	if len(items) == 0 {
		return false
	}
	next := m.modalHost.Prompt.PromptSuggestionSelected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(items) {
		next = len(items) - 1
	}
	if next == m.modalHost.Prompt.PromptSuggestionSelected {
		return false
	}
	m.modalHost.Prompt.PromptSuggestionSelected = next
	return true
}

func (m *Model) acceptPromptSuggestionSelection() bool {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil {
		return false
	}
	prompt := m.modalHost.Prompt
	items := promptActiveSuggestionItems(prompt)
	if len(items) == 0 {
		return false
	}
	index := prompt.PromptSuggestionSelected
	if index < 0 {
		index = 0
	}
	if index >= len(items) {
		index = len(items) - 1
	}
	field := prompt.ActivePromptField()
	if field == nil {
		return false
	}
	field.Value = items[index]
	field.Cursor = len([]rune(field.Value))
	field.Input.ResetFromLegacy(field.Value, field.Cursor, true, field.Placeholder)
	prompt.PromptSuggestionFocused = false
	m.refreshPromptCompletions()
	return true
}
