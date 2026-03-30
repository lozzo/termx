package modal

import "github.com/lozzow/termx/tuiv2/input"

type ModalPhase string

const (
	ModalPhaseOpening   ModalPhase = "opening"
	ModalPhaseLoading   ModalPhase = "loading"
	ModalPhaseReady     ModalPhase = "ready"
	ModalPhaseResolving ModalPhase = "resolving"
	ModalPhaseClosing   ModalPhase = "closing"
)

type ModalSession struct {
	Kind      input.ModeKind // 派生态描述，不是第二份可写 mode 真相
	Phase     ModalPhase
	Loading   bool
	RequestID string
}

type OpenModalEffect struct {
	Kind      input.ModeKind
	RequestID string
}

type LoadModalDataEffect struct {
	Kind      input.ModeKind
	RequestID string
}

type ModalLoadedMsg struct {
	Kind      input.ModeKind
	RequestID string
}

type ModalResultMsg struct {
	Kind      input.ModeKind
	RequestID string
	Value     any
}

type CloseModalEffect struct {
	Kind      input.ModeKind
	RequestID string
}
