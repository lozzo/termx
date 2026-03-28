package tui

import "strings"

type Renderer struct {
	workbench     *Workbench
	terminalStore *TerminalStore
}

func NewRenderer(workbench *Workbench, terminalStore *TerminalStore) *Renderer {
	return &Renderer{workbench: workbench, terminalStore: terminalStore}
}

func (r *Renderer) Workbench() *Workbench {
	if r == nil {
		return nil
	}
	return r.workbench
}

func (r *Renderer) TerminalStore() *TerminalStore {
	if r == nil {
		return nil
	}
	return r.terminalStore
}

func (r *Renderer) CachedFrame(model *Model) string {
	if model == nil || model.renderDirty || model.renderCache == "" {
		return ""
	}
	if model.workspacePicker != nil || model.terminalManager != nil || model.terminalPicker != nil || model.showHelp || model.prompt != nil {
		model.renderCacheHits.Add(1)
		return model.renderCache
	}
	if model.renderBatching && (model.program != nil || !model.anyPaneDirty()) {
		model.renderCacheHits.Add(1)
		return model.renderCache
	}
	return ""
}

func (r *Renderer) Render(model *Model) string {
	if model == nil {
		return ""
	}
	return strings.Join([]string{model.renderTabBar(), model.renderContentBody(), model.renderStatus()}, "\n")
}

func (r *Renderer) FinishFrame(model *Model, out string) string {
	if model == nil {
		return out
	}
	model.renderCache = out
	model.renderDirty = false
	model.renderLastFlush = model.now()
	model.renderFrames.Add(1)
	return out
}
