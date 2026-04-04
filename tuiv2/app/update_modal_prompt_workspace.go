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
	m.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: requestID}
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-workspace",
		Title:      "create workspace",
		Hint:       "[Enter] create  [Esc] cancel",
		AllowEmpty: false,
		ReturnMode: returnMode,
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: requestID})
	m.render.Invalidate()
}
