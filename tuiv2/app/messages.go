package app

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

type RenderTickMsg struct{}

type SemanticActionMsg struct {
	Action input.SemanticAction
}

type TerminalInputMsg struct {
	Input input.TerminalInput
}

type EffectAppliedMsg struct {
	Effect orchestrator.Effect
}
