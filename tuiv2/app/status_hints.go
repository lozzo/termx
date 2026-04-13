package app

import (
	"strconv"
	"strings"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type statusHintContext struct {
	activeTab        *workbench.VisibleTab
	activePane       *workbench.VisiblePane
	activeRole       string
	tabCount         int
	workspaceCount   int
	hasFloating      bool
	activeIsFloating bool
	selectedTreeKind string
	runtime          *render.VisibleRuntimeStateProxy
}

func (m *Model) buildStatusHints(vm render.RenderVM) []string {
	mode := input.ModeKind(strings.TrimSpace(vm.Status.InputMode))
	if mode == "" {
		mode = input.ModeNormal
	}
	ctx := buildStatusHintContext(vm)
	out := make([]string, 0, 8)
	seen := make(map[string]struct{})
	for _, doc := range input.DefaultBindingCatalog() {
		if doc.Mode != mode || strings.TrimSpace(doc.StatusText) == "" {
			continue
		}
		if !statusDocVisible(doc, mode, ctx) {
			continue
		}
		if _, ok := seen[doc.StatusText]; ok {
			continue
		}
		seen[doc.StatusText] = struct{}{}
		out = append(out, doc.StatusText)
	}
	return out
}

func (m *Model) buildStatusBarRightTokens(vm render.RenderVM) []render.RenderStatusToken {
	tokens := make([]render.RenderStatusToken, 0, 8)
	if vm.Overlay.Kind == render.VisibleOverlayWorkspacePicker && vm.Overlay.WorkspacePicker != nil {
		tokens = append(tokens, workspacePickerStatusBarRightTokens(vm.Overlay.WorkspacePicker.SelectedItem())...)
	}
	if vm.Workbench != nil {
		tokens = append(tokens, render.RenderStatusToken{Label: "ws:" + vm.Workbench.WorkspaceName})
		if label := floatingSummaryLabel(vm.Workbench); label != "" {
			tokens = append(tokens, render.RenderStatusToken{
				Kind:   render.HitRegionFloatingOverview,
				Label:  label,
				Action: input.SemanticAction{Kind: input.ActionOpenFloatingOverview},
			})
		}
	}
	if vm.Runtime != nil {
		tokens = append(tokens, render.RenderStatusToken{Label: "terminals:" + strconv.Itoa(len(vm.Runtime.Terminals))})
	}
	return tokens
}

func buildStatusHintContext(vm render.RenderVM) statusHintContext {
	ctx := statusHintContext{runtime: vm.Runtime}
	if vm.Overlay.Kind == render.VisibleOverlayWorkspacePicker && vm.Overlay.WorkspacePicker != nil {
		if selected := vm.Overlay.WorkspacePicker.SelectedItem(); selected != nil {
			switch {
			case selected.CreateNew:
				ctx.selectedTreeKind = "create"
			case strings.TrimSpace(selected.PaneID) != "":
				ctx.selectedTreeKind = "pane"
			case strings.TrimSpace(selected.TabID) != "":
				ctx.selectedTreeKind = "tab"
			default:
				ctx.selectedTreeKind = "workspace"
			}
		}
	}
	if vm.Workbench == nil {
		return ctx
	}
	ctx.tabCount = len(vm.Workbench.Tabs)
	ctx.workspaceCount = vm.Workbench.WorkspaceCount
	ctx.hasFloating = len(vm.Workbench.FloatingPanes) > 0
	if vm.Workbench.ActiveTab < 0 || vm.Workbench.ActiveTab >= len(vm.Workbench.Tabs) {
		return ctx
	}
	ctx.activeTab = &vm.Workbench.Tabs[vm.Workbench.ActiveTab]
	activePaneID := strings.TrimSpace(ctx.activeTab.ActivePaneID)
	if activePaneID == "" {
		return ctx
	}
	for i := range vm.Workbench.FloatingPanes {
		if vm.Workbench.FloatingPanes[i].ID == activePaneID {
			ctx.activePane = &vm.Workbench.FloatingPanes[i]
			ctx.activeIsFloating = true
			break
		}
	}
	if ctx.activePane == nil {
		for i := range ctx.activeTab.Panes {
			if vm.Workbench.Tabs[vm.Workbench.ActiveTab].Panes[i].ID == activePaneID {
				ctx.activePane = &ctx.activeTab.Panes[i]
				break
			}
		}
	}
	if ctx.activePane != nil {
		ctx.activeRole = paneRoleInVisibleRuntime(vm.Runtime, ctx.activePane.ID)
	}
	return ctx
}

func statusDocVisible(doc input.BindingDoc, mode input.ModeKind, ctx statusHintContext) bool {
	switch mode {
	case input.ModePane:
		switch doc.Binding.Action {
		case input.ActionDetachPane, input.ActionClosePaneKill:
			return ctx.activePaneConnected()
		case input.ActionRestartTerminal:
			return ctx.activePaneExited()
		case input.ActionBecomeOwner:
			return ctx.canBecomeOwner()
		case input.ActionToggleTerminalSizeLock:
			return ctx.activePaneConnected()
		case input.ActionReconnectPane:
			return ctx.activePane != nil && !ctx.activePaneExited()
		case input.ActionFocusPaneLeft, input.ActionFocusPaneRight, input.ActionFocusPaneUp, input.ActionFocusPaneDown,
			input.ActionSplitPane, input.ActionSplitPaneHorizontal,
			input.ActionClosePane, input.ActionZoomPane:
			return ctx.activePane != nil
		}
	case input.ModeResize:
		switch doc.Binding.Action {
		case input.ActionBecomeOwner:
			return ctx.canBecomeOwner()
		case input.ActionToggleTerminalSizeLock:
			return ctx.activePaneConnected()
		case input.ActionResizePaneLeft, input.ActionResizePaneRight, input.ActionResizePaneUp, input.ActionResizePaneDown,
			input.ActionResizePaneLargeLeft, input.ActionResizePaneLargeRight, input.ActionResizePaneLargeUp, input.ActionResizePaneLargeDown,
			input.ActionBalancePanes, input.ActionCycleLayout:
			return ctx.activeTab != nil
		}
	case input.ModeTab:
		switch doc.Binding.Action {
		case input.ActionNextTab, input.ActionPrevTab, input.ActionJumpTab:
			return ctx.tabCount > 1
		case input.ActionRenameTab, input.ActionKillTab:
			return ctx.activeTab != nil
		case input.ActionCreateTab:
			return ctx.tabCount >= 0
		}
	case input.ModeWorkspace:
		switch doc.Binding.Action {
		case input.ActionNextWorkspace, input.ActionPrevWorkspace:
			return ctx.workspaceCount > 1
		case input.ActionOpenWorkspacePicker, input.ActionCreateWorkspace, input.ActionRenameWorkspace, input.ActionDeleteWorkspace:
			return ctx.workspaceCount >= 0
		}
	case input.ModeWorkspacePicker:
		switch doc.Binding.Action {
		case input.ActionSubmitPrompt:
			return ctx.selectedTreeKind != ""
		case input.ActionCreateWorkspace:
			return ctx.selectedTreeKind == "" || ctx.selectedTreeKind == "workspace" || ctx.selectedTreeKind == "create"
		case input.ActionRenameWorkspace:
			return ctx.selectedTreeKind == "workspace" || ctx.selectedTreeKind == "tab"
		case input.ActionDeleteWorkspace:
			return ctx.selectedTreeKind == "workspace" || ctx.selectedTreeKind == "tab" || ctx.selectedTreeKind == "pane"
		case input.ActionDetachPane, input.ActionZoomPane:
			return ctx.selectedTreeKind == "pane"
		}
	case input.ModeFloating:
		switch doc.Binding.Action {
		case input.ActionCreateFloatingPane:
			return ctx.tabCount >= 0
		case input.ActionFocusNextFloatingPane, input.ActionFocusPrevFloatingPane:
			return ctx.hasFloating
		case input.ActionOpenFloatingOverview, input.ActionSummonFloatingPane,
			input.ActionExpandAllFloatingPanes, input.ActionCollapseAllFloatingPanes:
			return ctx.hasFloating
		case input.ActionMoveFloatingLeft, input.ActionMoveFloatingDown, input.ActionMoveFloatingUp, input.ActionMoveFloatingRight,
			input.ActionResizeFloatingLeft, input.ActionResizeFloatingDown, input.ActionResizeFloatingUp, input.ActionResizeFloatingRight,
			input.ActionCenterFloatingPane, input.ActionToggleFloatingVisibility, input.ActionCloseFloatingPane, input.ActionOpenPicker,
			input.ActionCollapseFloatingPane, input.ActionAutoFitFloatingPane, input.ActionToggleFloatingAutoFit:
			return ctx.activeIsFloating
		case input.ActionBecomeOwner:
			return ctx.activeIsFloating && ctx.canBecomeOwner()
		}
	case input.ModeDisplay:
		switch doc.Binding.Action {
		case input.ActionScrollUp, input.ActionScrollDown:
			return ctx.activePaneConnected()
		case input.ActionZoomPane:
			return ctx.activePane != nil
		}
	}
	return true
}

func (c statusHintContext) activePaneConnected() bool {
	return c.activePane != nil && strings.TrimSpace(c.activePane.TerminalID) != ""
}

func (c statusHintContext) activePaneExited() bool {
	if !c.activePaneConnected() || c.runtime == nil {
		return false
	}
	for _, terminal := range c.runtime.Terminals {
		if terminal.TerminalID == c.activePane.TerminalID {
			return terminal.State == "exited"
		}
	}
	return false
}

func (c statusHintContext) canBecomeOwner() bool {
	return c.activePaneConnected() && c.activeRole == "follower"
}

func paneRoleInVisibleRuntime(runtimeState *render.VisibleRuntimeStateProxy, paneID string) string {
	if runtimeState == nil || strings.TrimSpace(paneID) == "" {
		return ""
	}
	for _, binding := range runtimeState.Bindings {
		if binding.PaneID == paneID {
			return binding.Role
		}
	}
	return ""
}

func workspacePickerStatusBarRightTokens(item *modal.WorkspacePickerItem) []render.RenderStatusToken {
	if item == nil {
		return nil
	}
	switch {
	case item.CreateNew:
		label := "sel:new-workspace"
		if name := strings.TrimSpace(item.CreateName); name != "" {
			label = "sel:new:" + name
		}
		return []render.RenderStatusToken{{Label: label}}
	case strings.TrimSpace(item.PaneID) != "":
		tokens := []render.RenderStatusToken{{Label: "sel:pane:" + strings.TrimSpace(item.Name)}}
		if state := strings.TrimSpace(item.State); state != "" {
			tokens = append(tokens, render.RenderStatusToken{Label: state})
		}
		if role := strings.TrimSpace(item.Role); role != "" {
			tokens = append(tokens, render.RenderStatusToken{Label: role})
		}
		if item.Floating {
			tokens = append(tokens, render.RenderStatusToken{Label: "floating"})
		}
		return tokens
	case strings.TrimSpace(item.TabID) != "":
		tokens := []render.RenderStatusToken{{Label: "sel:tab:" + strings.TrimSpace(item.Name)}}
		if item.PaneCount > 0 {
			tokens = append(tokens, render.RenderStatusToken{Label: "panes:" + strconv.Itoa(item.PaneCount)})
		}
		if item.FloatingCount > 0 {
			tokens = append(tokens, render.RenderStatusToken{Label: "float:" + strconv.Itoa(item.FloatingCount)})
		}
		return tokens
	default:
		tokens := []render.RenderStatusToken{{Label: "sel:ws:" + strings.TrimSpace(item.Name)}}
		if item.TabCount > 0 {
			tokens = append(tokens, render.RenderStatusToken{Label: "tabs:" + strconv.Itoa(item.TabCount)})
		}
		if item.PaneCount > 0 {
			tokens = append(tokens, render.RenderStatusToken{Label: "panes:" + strconv.Itoa(item.PaneCount)})
		}
		if item.FloatingCount > 0 {
			tokens = append(tokens, render.RenderStatusToken{Label: "float:" + strconv.Itoa(item.FloatingCount)})
		}
		return tokens
	}
}

func floatingSummaryLabel(visible *workbench.VisibleWorkbench) string {
	if visible == nil || visible.FloatingTotal == 0 {
		return ""
	}
	if visible.FloatingCollapsed > 0 {
		if visible.FloatingHidden > 0 {
			return "float:" + strconv.Itoa(visible.FloatingTotal) + " collapsed:" + strconv.Itoa(visible.FloatingCollapsed) + " hidden:" + strconv.Itoa(visible.FloatingHidden)
		}
		return "float:" + strconv.Itoa(visible.FloatingTotal) + " collapsed:" + strconv.Itoa(visible.FloatingCollapsed)
	}
	if visible.FloatingHidden > 0 {
		return "float:" + strconv.Itoa(visible.FloatingTotal) + " hidden:" + strconv.Itoa(visible.FloatingHidden)
	}
	return "float:" + strconv.Itoa(visible.FloatingTotal)
}
