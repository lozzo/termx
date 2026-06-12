package state

import "strings"

// ClipboardStore 保存 reducer-owned clipboard 历史。
// 它只保留当前客户端的复制记录，不直接负责系统 clipboard IO。
type ClipboardStore struct {
	Entries []ClipboardEntry
}

type ClipboardEntry struct {
	ID      string
	Title   string
	Text    string
	Preview string
}

func (store ClipboardStore) WithCopiedText(text string) ClipboardStore {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return store
	}
	entry := ClipboardEntry{
		ID:      clipboardEntryID(text),
		Title:   clipboardEntryTitle(text),
		Text:    text,
		Preview: clipboardEntryPreview(text),
	}
	entries := make([]ClipboardEntry, 0, len(store.Entries)+1)
	entries = append(entries, entry)
	for _, existing := range store.Entries {
		if existing.ID == entry.ID && existing.Text == entry.Text {
			continue
		}
		entries = append(entries, existing)
	}
	store.Entries = entries
	return store
}

func clipboardEntryID(text string) string {
	return "clip:" + text
}

func clipboardEntryTitle(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "clipboard entry"
	}
	first := text
	if index := strings.Index(first, "\n"); index >= 0 {
		first = first[:index]
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return "clipboard entry"
	}
	return first
}

func clipboardEntryPreview(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 1 {
		return text
	}
	first := strings.TrimSpace(lines[0])
	if first == "" {
		return "…"
	}
	return first + " …"
}
