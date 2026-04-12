package app

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) openCreateWorkspaceNamePrompt(returnMode input.ModeKind) {
	m.openCreateWorkspaceNamePromptWithValue(returnMode, "")
}

func (m *Model) openCreateWorkspaceNamePromptWithValue(returnMode input.ModeKind, initialValue string) {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	initialValue = strings.TrimSpace(initialValue)
	requestID := "create-workspace"
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-workspace",
		Title:      "create workspace",
		Hint:       "[Enter] create  [Esc] cancel",
		Value:      initialValue,
		Cursor:     len([]rune(initialValue)),
		AllowEmpty: false,
		ReturnMode: returnMode,
	}
	m.render.Invalidate()
}
