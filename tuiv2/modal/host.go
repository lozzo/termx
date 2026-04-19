package modal

import "github.com/lozzow/termx/tuiv2/input"

type ModalHost struct {
	Session          *ModalSession
	Picker           *PickerState
	Prompt           *PromptState
	Help             *HelpState
	WorkspacePicker  *WorkspacePickerState
	FloatingOverview *FloatingOverviewState
}

func NewHost() *ModalHost {
	return &ModalHost{}
}

func (h *ModalHost) Open(kind input.ModeKind, requestID string) {
	if h == nil {
		return
	}
	h.Session = &ModalSession{
		Kind:      kind,
		Phase:     ModalPhaseOpening,
		Loading:   false,
		RequestID: requestID,
	}
}

func (h *ModalHost) StartLoading(kind input.ModeKind, requestID string) {
	if h == nil {
		return
	}
	h.Session = &ModalSession{
		Kind:      kind,
		Phase:     ModalPhaseLoading,
		Loading:   true,
		RequestID: requestID,
	}
}

func (h *ModalHost) MarkReady(kind input.ModeKind, requestID string) {
	if h == nil || h.Session == nil {
		return
	}
	h.Session.Kind = kind
	h.Session.RequestID = requestID
	h.Session.Phase = ModalPhaseReady
	h.Session.Loading = false
}

func (h *ModalHost) Close(kind input.ModeKind, requestID string) {
	if h == nil {
		return
	}
	if h.Session != nil {
		h.Session.Kind = kind
		h.Session.RequestID = requestID
		h.Session.Phase = ModalPhaseClosing
	}
	h.Session = nil
}
