package app

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) openCreateWorkspaceNamePrompt(returnMode input.ModeKind) {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	requestID := "create-workspace"
	m.openModal(input.ModePrompt, requestID)
	m.markModalReady(input.ModePrompt, requestID)
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-workspace",
		Title:      "create workspace",
		Hint:       "[Enter] create  [Esc] cancel",
		AllowEmpty: false,
		ReturnMode: returnMode,
	}
	m.render.Invalidate()
}
