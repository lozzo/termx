package state

import "strings"

func ClipboardHistoryItems(root Root) []ClipboardHistoryItem {
	shell := root.Shell.EnsureDefaults()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	items := make([]ClipboardHistoryItem, 0, len(root.Clipboard.Entries))
	for _, entry := range root.Clipboard.Entries {
		item := ClipboardHistoryItem{
			ID:      entry.ID,
			Title:   entry.Title,
			Preview: entry.Preview,
			Text:    entry.Text,
		}
		if !matchesClipboardHistoryQuery(item, query) {
			continue
		}
		items = append(items, item)
	}
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}

func matchesClipboardHistoryQuery(item ClipboardHistoryItem, query string) bool {
	if query == "" {
		return true
	}
	title := strings.ToLower(item.Title)
	preview := strings.ToLower(item.Preview)
	text := strings.ToLower(item.Text)
	return strings.Contains(title, query) || strings.Contains(preview, query) || strings.Contains(text, query)
}
