package sessionbind

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Target struct {
	TabID      string
	PaneID     string
	TerminalID string
}

type ReconnectResult struct {
	Target             Target
	KeptExitedTerminal bool
}

type BindDetachedTerminalRequest struct {
	TabID      string
	PaneID     string
	TerminalID string
	Binding    runtime.DetachedTerminalBinding
}

type BindDetachedTerminalResult struct {
	Target            Target
	LoadSnapshotAfter bool
}

type Manager struct {
	workbench *workbench.Workbench
	runtime   *runtime.Runtime
}

func NewManager(wb *workbench.Workbench, rt *runtime.Runtime) *Manager {
	return &Manager{workbench: wb, runtime: rt}
}

func (m *Manager) Close(paneID string) (Target, error) {
	target, err := m.currentPaneTarget(paneID)
	if err != nil || target.TabID == "" || target.PaneID == "" {
		return Target{}, err
	}
	if _, err := m.workbench.ClosePane(target.TabID, target.PaneID); err != nil {
		return Target{}, err
	}
	m.runtimeUnbind(target)
	if current := m.workbench.CurrentTab(); current != nil && current.ID == target.TabID && current.ActivePaneID != "" {
		_ = m.workbench.FocusPane(target.TabID, current.ActivePaneID)
	}
	return target, nil
}

func (m *Manager) Detach(paneID string) (Target, error) {
	target, err := m.currentPaneTarget(paneID)
	if err != nil || target.TabID == "" || target.PaneID == "" {
		return Target{}, err
	}
	if err := m.detachBinding(target); err != nil {
		return Target{}, err
	}
	return target, nil
}

func (m *Manager) Reconnect(paneID string) (ReconnectResult, error) {
	target, err := m.currentPaneTarget(paneID)
	if err != nil || target.TabID == "" || target.PaneID == "" {
		return ReconnectResult{}, err
	}
	if m.shouldKeepReconnectBinding(target.TerminalID) {
		return ReconnectResult{Target: target, KeptExitedTerminal: true}, nil
	}
	if err := m.detachBinding(target); err != nil {
		return ReconnectResult{}, err
	}
	return ReconnectResult{Target: target}, nil
}

func (m *Manager) BindDetachedTerminal(req BindDetachedTerminalRequest) (BindDetachedTerminalResult, error) {
	target, err := m.resolveTarget(req.TabID, req.PaneID)
	if err != nil {
		return BindDetachedTerminalResult{}, err
	}
	if target.TabID == "" || target.PaneID == "" || strings.TrimSpace(req.TerminalID) == "" {
		return BindDetachedTerminalResult{}, nil
	}
	name := strings.TrimSpace(req.Binding.Name)
	req.Binding.Name = name
	if err := m.workbench.BindPaneTerminal(target.TabID, target.PaneID, req.TerminalID); err != nil {
		return BindDetachedTerminalResult{}, err
	}
	_ = m.workbench.FocusPane(target.TabID, target.PaneID)
	if name != "" {
		m.workbench.SetPaneTitleByTerminalID(req.TerminalID, name)
	}
	if m.runtime != nil {
		m.runtime.UnbindPane(target.PaneID, target.TerminalID)
		m.runtime.BindDetachedTerminal(target.PaneID, req.TerminalID, req.Binding)
	}
	return BindDetachedTerminalResult{
		Target: Target{
			TabID:      target.TabID,
			PaneID:     target.PaneID,
			TerminalID: req.TerminalID,
		},
		LoadSnapshotAfter: m.runtime != nil && req.Binding.State == "exited",
	}, nil
}

func (m *Manager) detachBinding(target Target) error {
	if m == nil || m.workbench == nil {
		return fmt.Errorf("pane lifecycle: workbench unavailable")
	}
	if target.TabID == "" || target.PaneID == "" {
		return nil
	}
	if err := m.workbench.BindPaneTerminal(target.TabID, target.PaneID, ""); err != nil {
		return err
	}
	m.runtimeUnbind(target)
	return nil
}

func (m *Manager) currentPaneTarget(paneID string) (Target, error) {
	if m == nil || m.workbench == nil {
		return Target{}, fmt.Errorf("pane lifecycle: workbench unavailable")
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return Target{}, nil
	}
	targetPaneID := strings.TrimSpace(paneID)
	if targetPaneID == "" {
		targetPaneID = tab.ActivePaneID
	}
	if targetPaneID == "" {
		return Target{}, nil
	}
	pane := tab.Panes[targetPaneID]
	if pane == nil {
		return Target{}, nil
	}
	return Target{
		TabID:      tab.ID,
		PaneID:     targetPaneID,
		TerminalID: pane.TerminalID,
	}, nil
}

func (m *Manager) resolveTarget(tabID, paneID string) (Target, error) {
	if m == nil || m.workbench == nil || paneID == "" {
		return Target{}, nil
	}
	workspace := m.workbench.CurrentWorkspace()
	if workspace == nil {
		return Target{}, fmt.Errorf("select terminal: no current workspace")
	}
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.Panes[paneID] == nil {
			continue
		}
		if tabID != "" && tab.ID != tabID {
			continue
		}
		return Target{
			TabID:      tab.ID,
			PaneID:     paneID,
			TerminalID: tab.Panes[paneID].TerminalID,
		}, nil
	}
	if tabID != "" {
		return Target{}, fmt.Errorf("select terminal: pane %s not found in tab %s", paneID, tabID)
	}
	return Target{}, fmt.Errorf("select terminal: pane %s not found", paneID)
}

func (m *Manager) runtimeUnbind(target Target) {
	if m == nil || m.runtime == nil || target.PaneID == "" {
		return
	}
	m.runtime.UnbindPane(target.PaneID, target.TerminalID)
}

func (m *Manager) shouldKeepReconnectBinding(terminalID string) bool {
	if m == nil || m.runtime == nil || terminalID == "" {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	return terminal != nil && terminal.State == "exited"
}
