package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

const clipboardHistoryLimit = 50

const clipboardHistoryCreateItemID = "__clipboard_create__"

type clipboardHistoryEntry struct {
	ID        string
	Text      string
	Preview   string
	PaneID    string
	CreatedAt time.Time
}

func clipboardHistoryRequestID() string {
	return "clipboard-history"
}

func (m *Model) pushClipboardHistory(text, paneID string) tea.Cmd {
	if m == nil || text == "" {
		return nil
	}
	now := time.Now().UTC()
	if len(m.clipboardHistory) > 0 && m.clipboardHistory[0].Text == text {
		m.clipboardHistory[0].CreatedAt = now
		m.clipboardHistory[0].PaneID = paneID
		m.clipboardHistory[0] = normalizeClipboardHistoryEntry(m.clipboardHistory[0])
		m.yankBuffer = text
		return m.storeClipboardHistoryEntryCmd(m.clipboardHistory[0])
	}
	m.clipboardSeq++
	entry := normalizeClipboardHistoryEntry(clipboardHistoryEntry{
		ID:        fmt.Sprintf("clip-%d-%d", now.UnixNano(), m.clipboardSeq),
		Text:      text,
		PaneID:    paneID,
		CreatedAt: now,
	})
	m.clipboardHistory = append([]clipboardHistoryEntry{entry}, m.clipboardHistory...)
	if len(m.clipboardHistory) > clipboardHistoryLimit {
		m.clipboardHistory = m.clipboardHistory[:clipboardHistoryLimit]
	}
	m.yankBuffer = text
	return m.storeClipboardHistoryEntryCmd(entry)
}

func normalizeClipboardHistoryEntry(entry clipboardHistoryEntry) clipboardHistoryEntry {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.PaneID = strings.TrimSpace(entry.PaneID)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	} else {
		entry.CreatedAt = entry.CreatedAt.UTC()
	}
	if strings.TrimSpace(entry.Preview) == "" {
		entry.Preview = clipboardPreview(entry.Text)
	}
	return entry
}

func clipboardPreview(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.TrimSpace(text)
	if len([]rune(text)) > 72 {
		return string([]rune(text)[:72]) + "..."
	}
	if text == "" {
		return "(empty)"
	}
	return text
}

func (m *Model) openClipboardHistory() tea.Cmd {
	if m == nil {
		return nil
	}
	if m.clipboardStore != nil {
		_ = m.showClipboardHistoryPicker()
		return m.loadClipboardHistoryCmd(true)
	}
	return m.showClipboardHistoryPicker()
}

func (m *Model) showClipboardHistoryPicker() tea.Cmd {
	if m == nil || m.modalHost == nil {
		return nil
	}
	requestID := clipboardHistoryRequestID()
	m.openModal(input.ModePicker, requestID)
	m.markModalReady(input.ModePicker, requestID)
	items := make([]modal.PickerItem, 0, len(m.clipboardHistory)+2)
	items = append(items, modal.PickerItem{
		TerminalID:  clipboardHistoryCreateItemID,
		Name:        "New clipboard entry",
		State:       "new",
		Description: "Add shared clipboard text.",
		CreateNew:   true,
	})
	for _, entry := range m.clipboardHistory {
		items = append(items, modal.PickerItem{
			TerminalID:  entry.ID,
			Name:        entry.Preview,
			State:       entry.CreatedAt.Format("15:04:05"),
			Location:    entry.PaneID,
			Description: entry.Text,
			CreatedAt:   entry.CreatedAt,
		})
	}
	if len(m.clipboardHistory) == 0 {
		items = append(items, modal.PickerItem{
			Name:        "Clipboard history is empty",
			State:       "copy text first",
			Description: "Copied selections from copy mode appear here.",
		})
	}
	m.modalHost.Picker = &modal.PickerState{
		Title:    "Clipboard History",
		Items:    items,
		Selected: clipboardHistoryInitialSelection(m.clipboardHistory),
	}
	m.modalHost.Picker.ApplyFilter()
	m.render.Invalidate()
	return nil
}

func clipboardHistoryInitialSelection(entries []clipboardHistoryEntry) int {
	if len(entries) == 0 {
		return 0
	}
	return 1
}

func (m *Model) loadClipboardHistoryCmd(openPicker bool) tea.Cmd {
	if m == nil || m.clipboardStore == nil {
		return nil
	}
	store := m.clipboardStore
	return func() tea.Msg {
		entries, err := store.List(context.Background())
		return clipboardHistoryLoadedMsg{Entries: entries, OpenPicker: openPicker, Err: err}
	}
}

func (m *Model) storeClipboardHistoryEntryCmd(entry clipboardHistoryEntry) tea.Cmd {
	if m == nil || m.clipboardStore == nil || entry.Text == "" {
		return nil
	}
	store := m.clipboardStore
	return func() tea.Msg {
		return clipboardHistoryStoredMsg{Err: store.Put(context.Background(), entry)}
	}
}

func (m *Model) applyLoadedClipboardHistory(entries []clipboardHistoryEntry) {
	if m == nil {
		return
	}
	m.clipboardHistory = mergeClipboardHistoryEntries(m.clipboardHistory, entries)
	if len(m.clipboardHistory) > 0 {
		m.yankBuffer = m.clipboardHistory[0].Text
	}
}

func (m *Model) upsertClipboardHistoryEntry(entry clipboardHistoryEntry) tea.Cmd {
	if m == nil {
		return nil
	}
	entry = normalizeClipboardHistoryEntry(entry)
	if entry.ID == "" || entry.Text == "" {
		return nil
	}
	for i := range m.clipboardHistory {
		if m.clipboardHistory[i].ID != entry.ID {
			continue
		}
		m.clipboardHistory[i] = entry
		m.sortClipboardHistory()
		m.yankBuffer = m.clipboardHistory[0].Text
		_ = m.showClipboardHistoryPicker()
		return m.storeClipboardHistoryEntryCmd(entry)
	}
	m.clipboardHistory = append([]clipboardHistoryEntry{entry}, m.clipboardHistory...)
	m.sortClipboardHistory()
	if len(m.clipboardHistory) > clipboardHistoryLimit {
		m.clipboardHistory = m.clipboardHistory[:clipboardHistoryLimit]
	}
	m.yankBuffer = m.clipboardHistory[0].Text
	_ = m.showClipboardHistoryPicker()
	return m.storeClipboardHistoryEntryCmd(entry)
}

func (m *Model) deleteClipboardHistoryEntry(id string) tea.Cmd {
	if m == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	filtered := m.clipboardHistory[:0]
	for _, entry := range m.clipboardHistory {
		if entry.ID != id {
			filtered = append(filtered, entry)
		}
	}
	m.clipboardHistory = filtered
	if len(m.clipboardHistory) == 0 {
		m.yankBuffer = ""
	} else {
		m.yankBuffer = m.clipboardHistory[0].Text
	}
	_ = m.showClipboardHistoryPicker()
	return m.deleteClipboardHistoryEntryCmd(id)
}

func (m *Model) deleteClipboardHistoryEntryCmd(id string) tea.Cmd {
	if m == nil || m.clipboardStore == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	store := m.clipboardStore
	id = strings.TrimSpace(id)
	return func() tea.Msg {
		return clipboardHistoryDeletedMsg{Err: store.Delete(context.Background(), id)}
	}
}

func (m *Model) sortClipboardHistory() {
	if m == nil {
		return
	}
	sort.SliceStable(m.clipboardHistory, func(i, j int) bool {
		return m.clipboardHistory[i].CreatedAt.After(m.clipboardHistory[j].CreatedAt)
	})
}

func mergeClipboardHistoryEntries(current, loaded []clipboardHistoryEntry) []clipboardHistoryEntry {
	if len(current) == 0 && len(loaded) == 0 {
		return nil
	}
	byID := make(map[string]clipboardHistoryEntry, len(current)+len(loaded))
	add := func(entry clipboardHistoryEntry) {
		entry = normalizeClipboardHistoryEntry(entry)
		if entry.ID == "" || entry.Text == "" {
			return
		}
		existing, ok := byID[entry.ID]
		if !ok || entry.CreatedAt.After(existing.CreatedAt) {
			byID[entry.ID] = entry
		}
	}
	for _, entry := range loaded {
		add(entry)
	}
	for _, entry := range current {
		add(entry)
	}
	out := make([]clipboardHistoryEntry, 0, len(byID))
	for _, entry := range byID {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > clipboardHistoryLimit {
		out = out[:clipboardHistoryLimit]
	}
	return out
}

func (m *Model) clipboardHistoryEntryByID(id string) *clipboardHistoryEntry {
	if m == nil || id == "" {
		return nil
	}
	for i := range m.clipboardHistory {
		if m.clipboardHistory[i].ID == id {
			return &m.clipboardHistory[i]
		}
	}
	return nil
}
