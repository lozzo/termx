package app

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

type surfaceKind uint8

const (
	surfaceWorkbench surfaceKind = iota
	surfaceTerminalPool
)

type surfaceState struct {
	kind         surfaceKind
	terminalPool *modal.TerminalManagerState
}

// UIState owns all UI-local state: input mode, modal lifecycle, and full-page
// surfaces such as the terminal pool.
type UIState struct {
	router  *input.Router
	modal   *modal.ModalHost
	surface surfaceState
}

func NewUIState() *UIState {
	return &UIState{
		router: input.NewRouter(),
		modal:  modal.NewHost(),
		surface: surfaceState{
			kind: surfaceWorkbench,
		},
	}
}

func (u *UIState) Router() *input.Router {
	if u == nil {
		return nil
	}
	return u.router
}

func (u *UIState) ModalHost() *modal.ModalHost {
	if u == nil {
		return nil
	}
	return u.modal
}

func (u *UIState) Mode() input.ModeState {
	if u == nil || u.router == nil {
		return input.ModeState{}
	}
	return u.router.Mode()
}

func (u *UIState) SetMode(mode input.ModeState) {
	if u == nil || u.router == nil {
		return
	}
	u.router.SetMode(mode)
}

func (u *UIState) OpenModal(kind input.ModeKind, requestID string) {
	if u == nil || u.modal == nil {
		return
	}
	u.modal.Open(kind, requestID)
	u.SetMode(input.ModeState{Kind: kind, RequestID: requestID})
}

func (u *UIState) StartLoadingModal(kind input.ModeKind, requestID string) {
	if u == nil || u.modal == nil {
		return
	}
	u.modal.StartLoading(kind, requestID)
	u.SetMode(input.ModeState{Kind: kind, RequestID: requestID})
}

func (u *UIState) MarkModalReady(kind input.ModeKind, requestID string) {
	if u == nil || u.modal == nil {
		return
	}
	u.modal.MarkReady(kind, requestID)
	u.SetMode(input.ModeState{Kind: kind, RequestID: requestID})
}

func (u *UIState) CloseModal(kind input.ModeKind, requestID string, next input.ModeState) {
	if u == nil || u.modal == nil {
		return
	}
	u.modal.Close(kind, requestID)
	u.SetMode(next)
}

func (u *UIState) TerminalPool() *modal.TerminalManagerState {
	if u == nil {
		return nil
	}
	return u.surface.terminalPool
}

func (u *UIState) OpenTerminalPool(requestID string) *modal.TerminalManagerState {
	if u == nil {
		return nil
	}
	u.surface.kind = surfaceTerminalPool
	u.surface.terminalPool = &modal.TerminalManagerState{
		Title: "Terminal Pool",
	}
	u.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: requestID})
	return u.surface.terminalPool
}

func (u *UIState) CloseTerminalPool() {
	if u == nil {
		return
	}
	u.surface.kind = surfaceWorkbench
	u.surface.terminalPool = nil
	u.SetMode(input.ModeState{Kind: input.ModeNormal})
}

func (u *UIState) TerminalPoolOpen() bool {
	return u != nil && u.surface.kind == surfaceTerminalPool && u.surface.terminalPool != nil
}

func (u *UIState) VisibleInputMode() input.ModeKind {
	if u == nil {
		return input.ModeNormal
	}
	if u.TerminalPoolOpen() {
		return input.ModeTerminalManager
	}
	if u.modal != nil && u.modal.Session != nil {
		return u.modal.Session.Kind
	}
	return u.Mode().Kind
}

func (m *Model) syncUIAliases() {
	if m == nil || m.ui == nil {
		return
	}
	m.input = m.ui.Router()
	m.modalHost = m.ui.ModalHost()
	m.terminalPage = m.ui.TerminalPool()
}

func (m *Model) mode() input.ModeState {
	if m == nil || m.ui == nil {
		return input.ModeState{}
	}
	return m.ui.Mode()
}

func (m *Model) setMode(mode input.ModeState) {
	if m == nil || m.ui == nil {
		return
	}
	m.ui.SetMode(mode)
	m.syncUIAliases()
}

func (m *Model) openModal(kind input.ModeKind, requestID string) {
	if m == nil || m.ui == nil {
		return
	}
	m.ui.OpenModal(kind, requestID)
	m.syncUIAliases()
}

func (m *Model) startLoadingModal(kind input.ModeKind, requestID string) {
	if m == nil || m.ui == nil {
		return
	}
	m.ui.StartLoadingModal(kind, requestID)
	m.syncUIAliases()
}

func (m *Model) markModalReady(kind input.ModeKind, requestID string) {
	if m == nil || m.ui == nil {
		return
	}
	m.ui.MarkModalReady(kind, requestID)
	m.syncUIAliases()
}

func (m *Model) closeModal(kind input.ModeKind, requestID string, next input.ModeState) {
	if m == nil || m.ui == nil {
		return
	}
	m.ui.CloseModal(kind, requestID, next)
	m.syncUIAliases()
}

func (m *Model) openTerminalPool() *modal.TerminalManagerState {
	if m == nil || m.ui == nil {
		return nil
	}
	state := m.ui.OpenTerminalPool(terminalPoolPageModeToken)
	m.syncUIAliases()
	return state
}

func (m *Model) closeTerminalPoolSurface() {
	if m == nil || m.ui == nil {
		return
	}
	m.ui.CloseTerminalPool()
	m.syncUIAliases()
}
