package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func cloneIntPointer(src *int) *int {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func (m *Model) openEditTerminalPrompt(item *modal.PickerItem) {
	if m == nil || m.modalHost == nil || item == nil || item.TerminalID == "" {
		return
	}
	name := strings.TrimSpace(item.Name)
	requestID := "edit-terminal:" + item.TerminalID
	returnMode := m.promptReturnMode()
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:        "edit-terminal-name",
		Title:       "Edit Terminal",
		Hint:        "[Enter] continue  [Esc] cancel",
		Value:       name,
		Cursor:      len([]rune(name)),
		Original:    name,
		DefaultName: name,
		TerminalID:  item.TerminalID,
		Command:     append([]string(nil), item.CommandArgs...),
		Name:        name,
		Tags:        cloneStringMap(item.Tags),
		ReturnMode:  returnMode,
	}
	m.render.Invalidate()
}

func (m *Model) openCreateTerminalPrompt(paneID string, target modal.CreateTargetKind) {
	if m == nil || m.modalHost == nil {
		return
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	requestID := "create-terminal:" + paneID
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:         "create-terminal-form",
		Title:        "Create Terminal",
		Hint:         "name is required; command, workdir, tags are optional",
		PaneID:       paneID,
		Command:      []string{shell},
		DefaultName:  filepath.Base(shell),
		CreateTarget: target,
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Required: true},
			{Key: "command", Label: "command", Placeholder: shell},
			{Key: "workdir", Label: "workdir"},
			{Key: "tags", Label: "tags", Placeholder: "role=dev env=test"},
		},
	}
	m.render.Invalidate()
}

func (m *Model) openRenameWorkspacePrompt() {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	workspace := m.workbench.CurrentWorkspace()
	if workspace == nil {
		return
	}
	requestID := "rename-workspace:" + workspace.Name
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-workspace",
		Title:      "rename workspace",
		Hint:       "[Enter] save  [Esc] cancel",
		Value:      workspace.Name,
		Cursor:     len([]rune(workspace.Name)),
		Original:   workspace.Name,
		AllowEmpty: false,
	}
	m.render.Invalidate()
}

func (m *Model) openRenameTabPrompt() {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return
	}
	requestID := "rename-tab:" + tab.ID
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-tab",
		Title:      "rename tab",
		Hint:       "[Enter] save  [Esc] cancel",
		Value:      tab.Name,
		Cursor:     len([]rune(tab.Name)),
		Original:   tab.Name,
		AllowEmpty: false,
	}
	m.render.Invalidate()
}

func (m *Model) promptReturnMode() input.ModeKind {
	if m == nil {
		return input.ModeNormal
	}
	if m.mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		return input.ModeTerminalManager
	}
	return input.ModeNormal
}

func (m *Model) restorePromptReturnMode(prompt *modal.PromptState) {
	if m == nil {
		return
	}
	mode := input.ModeNormal
	if prompt != nil && prompt.ReturnMode != "" {
		mode = prompt.ReturnMode
	}
	next := input.ModeState{Kind: mode}
	if mode == input.ModeTerminalManager {
		next.RequestID = terminalPoolPageModeToken
	}
	m.setMode(next)
}
