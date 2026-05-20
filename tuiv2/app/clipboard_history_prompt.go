package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) openCreateClipboardEntryPrompt() {
	if m == nil || m.modalHost == nil {
		return
	}
	requestID := "create-clipboard-entry"
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:            "create-clipboard-entry",
		Title:           "New Clipboard Entry",
		Hint:            "enter clipboard text",
		PaneID:          m.currentOrActionPaneID(""),
		AllowEmpty:      false,
		ReturnMode:      input.ModePicker,
		ReturnRequestID: clipboardHistoryRequestID(),
	}
	m.render.Invalidate()
}

func (m *Model) openEditClipboardEntryPrompt(entry clipboardHistoryEntry) {
	if m == nil || m.modalHost == nil || strings.TrimSpace(entry.ID) == "" {
		return
	}
	text := entry.Text
	requestID := "edit-clipboard-entry:" + entry.ID
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:            "edit-clipboard-entry",
		Title:           "Edit Clipboard Entry",
		Hint:            "update clipboard text",
		Value:           text,
		Cursor:          len([]rune(text)),
		Original:        text,
		TerminalID:      entry.ID,
		PaneID:          entry.PaneID,
		AllowEmpty:      false,
		ReturnMode:      input.ModePicker,
		ReturnRequestID: clipboardHistoryRequestID(),
	}
	m.render.Invalidate()
}

func (m *Model) submitCreateClipboardEntryPrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	text := prompt.ValueState().Value()
	if strings.TrimSpace(text) == "" {
		return func() tea.Msg { return inputError("clipboard text is required") }
	}
	now := time.Now().UTC()
	m.clipboardSeq++
	entry := clipboardHistoryEntry{
		ID:        fmt.Sprintf("clip-%d-%d", now.UnixNano(), m.clipboardSeq),
		Text:      text,
		PaneID:    clipboardPromptPaneID(prompt),
		SourceApp: "tuiv2",
		CreatedAt: now,
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.closeModal(input.ModePrompt, requestID, input.ModeState{})
	cmd := m.upsertClipboardHistoryEntry(entry)
	m.render.Invalidate()
	return cmd
}

func (m *Model) submitEditClipboardEntryPrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	id := strings.TrimSpace(prompt.TerminalID)
	if id == "" {
		return func() tea.Msg { return context.Canceled }
	}
	text := prompt.ValueState().Value()
	if strings.TrimSpace(text) == "" {
		return func() tea.Msg { return inputError("clipboard text is required") }
	}
	if text == prompt.Original {
		if existing := m.clipboardHistoryEntryByID(id); existing != nil {
			text = existing.Text
		}
	}
	entry := clipboardHistoryEntry{
		ID:        id,
		Text:      text,
		PaneID:    clipboardPromptPaneID(prompt),
		SourceApp: "tuiv2",
		CreatedAt: time.Now().UTC(),
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.closeModal(input.ModePrompt, requestID, input.ModeState{})
	cmd := m.upsertClipboardHistoryEntry(entry)
	m.render.Invalidate()
	return cmd
}

func clipboardPromptPaneID(prompt *modal.PromptState) string {
	if prompt == nil {
		return "manual"
	}
	if paneID := strings.TrimSpace(prompt.PaneID); paneID != "" {
		return paneID
	}
	return "manual"
}
