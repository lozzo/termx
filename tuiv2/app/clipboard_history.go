package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

const clipboardHistoryLimit = 50

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

func (m *Model) pushClipboardHistory(text, paneID string) {
	if m == nil || text == "" {
		return
	}
	if len(m.clipboardHistory) > 0 && m.clipboardHistory[0].Text == text {
		m.clipboardHistory[0].CreatedAt = time.Now()
		m.clipboardHistory[0].PaneID = paneID
		return
	}
	m.clipboardSeq++
	entry := clipboardHistoryEntry{
		ID:        fmt.Sprintf("clip-%d", m.clipboardSeq),
		Text:      text,
		Preview:   clipboardPreview(text),
		PaneID:    paneID,
		CreatedAt: time.Now(),
	}
	m.clipboardHistory = append([]clipboardHistoryEntry{entry}, m.clipboardHistory...)
	if len(m.clipboardHistory) > clipboardHistoryLimit {
		m.clipboardHistory = m.clipboardHistory[:clipboardHistoryLimit]
	}
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
	if m == nil || m.modalHost == nil {
		return nil
	}
	requestID := clipboardHistoryRequestID()
	m.openModal(input.ModePicker, requestID)
	m.markModalReady(input.ModePicker, requestID)
	items := make([]modal.PickerItem, 0, len(m.clipboardHistory))
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
	if len(items) == 0 {
		items = append(items, modal.PickerItem{
			Name:        "Clipboard history is empty",
			State:       "copy text first",
			Description: "Use Space to mark, then Space or y to copy in copy mode.",
		})
	}
	m.modalHost.Picker = &modal.PickerState{
		Title:    "Clipboard History",
		Footer:   "[Enter] paste  [Esc] close",
		Items:    items,
		Selected: 0,
	}
	m.modalHost.Picker.ApplyFilter()
	m.render.Invalidate()
	return nil
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
