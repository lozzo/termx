package input

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/lozzow/termx/termx-shared/plugin"
)

const (
	ActionClientModePaneEnter      plugin.ActionID = "termx.client.mode.pane.enter"
	ActionClientModeResizeEnter    plugin.ActionID = "termx.client.mode.resize.enter"
	ActionClientModeGlobalEnter    plugin.ActionID = "termx.client.mode.global.enter"
	ActionClientModeFloatingEnter  plugin.ActionID = "termx.client.mode.float.enter"
	ActionClientModeTabEnter       plugin.ActionID = "termx.client.mode.tab.enter"
	ActionClientModeWorkspaceEnter plugin.ActionID = "termx.client.mode.workspace.enter"

	ActionClientTerminalPickerOpen plugin.ActionID = "termx.client.terminal_picker.open"
	ActionClientCopyModeEnter      plugin.ActionID = "termx.client.copy_mode.enter"

	ActionClientPanelClose           plugin.ActionID = "termx.client.panel.close"
	ActionClientPanelDetach          plugin.ActionID = "termx.client.panel.detach"
	ActionClientPanelReconnect       plugin.ActionID = "termx.client.panel.reconnect"
	ActionClientPanelRestart         plugin.ActionID = "termx.client.panel.restart"
	ActionClientPanelTakeOwner       plugin.ActionID = "termx.client.panel.take_owner"
	ActionClientPanelCloseKill       plugin.ActionID = "termx.client.panel.close_and_kill_terminal"
	ActionClientPanelSplitRight      plugin.ActionID = "termx.client.panel.split_right"
	ActionClientPanelSplitDown       plugin.ActionID = "termx.client.panel.split_down"
	ActionClientPanelToggleZoom      plugin.ActionID = "termx.client.panel.toggle_zoom"
	ActionClientPanelBalance         plugin.ActionID = "termx.client.panel.balance"
	ActionClientPanelPresentationSet plugin.ActionID = "termx.client.panel.presentation.set"
	ActionClientPanelFocusNext       plugin.ActionID = "termx.client.panel.focus_next"
	ActionClientPanelFocusPrevious   plugin.ActionID = "termx.client.panel.focus_previous"
	ActionClientPanelResize          plugin.ActionID = "termx.client.panel.resize"

	ActionClientTerminalSizeLockToggle plugin.ActionID = "termx.client.terminal.size_lock.toggle"
	ActionClientTerminalViewToggle     plugin.ActionID = "termx.client.terminal_view.layout.toggle"
	ActionClientTerminalViewPan        plugin.ActionID = "termx.client.terminal_view.layout.pan"
	ActionClientTerminalViewAlign      plugin.ActionID = "termx.client.terminal_view.layout.align"
	ActionClientTerminalViewCenter     plugin.ActionID = "termx.client.terminal_view.layout.center"
	ActionClientTerminalViewReset      plugin.ActionID = "termx.client.terminal_view.layout.reset"

	ActionClientChromeHeaderToggle plugin.ActionID = "termx.client.chrome.header.toggle"
	ActionClientChromeFooterToggle plugin.ActionID = "termx.client.chrome.footer.toggle"
	ActionClientToastClear         plugin.ActionID = "termx.client.toast.clear"
	ActionClientToastClose         plugin.ActionID = "termx.client.toast.close"
	ActionClientTerminalPoolOpen   plugin.ActionID = "termx.client.terminal_pool.open"
	ActionClientWorkbenchTreeOpen  plugin.ActionID = "termx.client.workbench_tree.open"
	ActionClientShortcutLockToggle plugin.ActionID = "termx.client.shortcut_lock.toggle"
	ActionClientPromptOpen         plugin.ActionID = "termx.client.prompt.open"
	ActionClientHelpOpen           plugin.ActionID = "termx.client.help.open"
	ActionClientSessionQuit        plugin.ActionID = "termx.client.session.quit"

	ActionClientFloatCreate        plugin.ActionID = "termx.client.float.create"
	ActionClientFloatOverview      plugin.ActionID = "termx.client.float.overview"
	ActionClientFloatSummon        plugin.ActionID = "termx.client.float.summon"
	ActionClientFloatPickerOpen    plugin.ActionID = "termx.client.float.terminal_picker.open"
	ActionClientFloatTakeOwner     plugin.ActionID = "termx.client.float.take_owner"
	ActionClientFloatClose         plugin.ActionID = "termx.client.float.close"
	ActionClientFloatCollapse      plugin.ActionID = "termx.client.float.collapse"
	ActionClientFloatCenter        plugin.ActionID = "termx.client.float.center"
	ActionClientFloatToggleAll     plugin.ActionID = "termx.client.float.visibility.toggle_all"
	ActionClientFloatFit           plugin.ActionID = "termx.client.float.fit"
	ActionClientFloatAutoFitToggle plugin.ActionID = "termx.client.float.auto_fit.toggle"
	ActionClientFloatMove          plugin.ActionID = "termx.client.float.move"
	ActionClientFloatResize        plugin.ActionID = "termx.client.float.resize"

	ActionClientTabCreate             plugin.ActionID = "termx.client.tab.create"
	ActionClientTabNext               plugin.ActionID = "termx.client.tab.next"
	ActionClientTabPrevious           plugin.ActionID = "termx.client.tab.previous"
	ActionClientTabActivate           plugin.ActionID = "termx.client.tab.activate"
	ActionClientTabRename             plugin.ActionID = "termx.client.tab.rename"
	ActionClientTabClose              plugin.ActionID = "termx.client.tab.close"
	ActionClientTabCloseKillTerminals plugin.ActionID = "termx.client.tab.close_and_kill_terminals"

	ActionClientWorkspaceCreate plugin.ActionID = "termx.client.workspace.create"
	ActionClientWorkspaceNext   plugin.ActionID = "termx.client.workspace.next"
	ActionClientWorkspacePrev   plugin.ActionID = "termx.client.workspace.previous"
	ActionClientWorkspaceRename plugin.ActionID = "termx.client.workspace.rename"
	ActionClientWorkspaceDelete plugin.ActionID = "termx.client.workspace.delete"
)

// ClientActionSpec 是 TUI client-local action registry 的单条声明。
// 它只描述按键或 UI action 如何转成现有 semantic intent；不读取 state，也不执行 reducer。
type ClientActionSpec struct {
	ID        plugin.ActionID
	Kind      IntentKind
	Command   string
	Action    ShellAction
	Reason    string
	Mode      InteractionMode
	ParamKeys []string
	Danger    bool
}

// ClientActionSpecCatalog 返回 TUI 内建 client action 列表。
// 所有内建 action 必须使用 termx.client.* 命名空间，供快捷键和后续 client control 共用。
func ClientActionSpecCatalog() []ClientActionSpec {
	out := make([]ClientActionSpec, len(clientActionSpecs))
	copy(out, clientActionSpecs)
	for i := range out {
		out[i].ParamKeys = append([]string(nil), out[i].ParamKeys...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ClientActionSpecByID 查找 TUI 内建 client action。
// 调用方只能按 ActionID 查 catalog，不应绕过 action 直接解释快捷键。
func ClientActionSpecByID(id plugin.ActionID) (ClientActionSpec, bool) {
	spec, ok := clientActionSpecByID[id]
	if ok {
		spec.ParamKeys = append([]string(nil), spec.ParamKeys...)
	}
	return spec, ok
}

// ResolveClientAction 把 ActionID 与参数解析为 input semantic intent。
// 它是纯适配层：client action 的真实副作用仍必须回到 app reducer/effect 路径。
func ResolveClientAction(id plugin.ActionID, args map[string]string, event InputEvent) (Intent, bool) {
	spec, ok := ClientActionSpecByID(id)
	if !ok {
		return Intent{}, false
	}
	intent := Intent{
		Kind:       spec.Kind,
		Event:      event,
		Command:    spec.Command,
		Action:     spec.Action,
		Reason:     spec.Reason,
		Mode:       spec.Mode,
		ActionID:   id,
		ActionArgs: cloneActionArgs(args),
	}
	switch id {
	case ActionClientTabActivate:
		index := args["index"]
		if index == "" {
			return Intent{}, false
		}
		intent.Command = "tab jump " + index
	case ActionClientPanelResize:
		direction := args["direction"]
		delta := args["delta"]
		if direction == "" || delta == "" {
			return Intent{}, false
		}
		intent.Command = "pane resize " + direction + " delta=" + delta
	case ActionClientPanelPresentationSet:
		style := args["style"]
		if style == "" {
			return Intent{}, false
		}
		intent.Command = "pane presentation " + style
	case ActionClientTerminalViewPan:
		direction := args["direction"]
		if direction == "" {
			return Intent{}, false
		}
		intent.Command = "terminal layout pan-" + direction
	case ActionClientTerminalViewAlign:
		edge := args["edge"]
		if edge == "" {
			return Intent{}, false
		}
		intent.Command = "terminal layout align-" + edge
	case ActionClientTerminalViewCenter:
		axis := args["axis"]
		switch axis {
		case "":
			intent.Command = "terminal layout center"
		case "x", "y":
			intent.Command = "terminal layout center-" + axis
		default:
			return Intent{}, false
		}
	case ActionClientFloatSummon:
		index := args["index"]
		if index == "" {
			return Intent{}, false
		}
		intent.Reason = index
	case ActionClientFloatMove, ActionClientFloatResize:
		direction := args["direction"]
		if direction == "" {
			return Intent{}, false
		}
		intent.Reason = direction
	}
	return intent, true
}

var clientActionSpecs = []ClientActionSpec{
	{ID: ActionClientModePaneEnter, Kind: IntentSetInteractionMode, Mode: InteractionModePane},
	{ID: ActionClientModeResizeEnter, Kind: IntentSetInteractionMode, Mode: InteractionModeResize},
	{ID: ActionClientModeGlobalEnter, Kind: IntentSetInteractionMode, Mode: InteractionModeGlobal},
	{ID: ActionClientModeFloatingEnter, Kind: IntentSetInteractionMode, Mode: InteractionModeFloating},
	{ID: ActionClientModeTabEnter, Kind: IntentSetInteractionMode, Mode: InteractionModeTab},
	{ID: ActionClientModeWorkspaceEnter, Kind: IntentSetInteractionMode, Mode: InteractionModeWorkspace},
	{ID: ActionClientTerminalPickerOpen, Kind: IntentOpenTerminalPicker},
	{ID: ActionClientCopyModeEnter, Kind: IntentEnterCopyMode},
	{ID: ActionClientPanelClose, Kind: IntentWorkbenchCommand, Command: "pane close", Danger: true},
	{ID: ActionClientPanelDetach, Kind: IntentWorkbenchCommand, Command: "pane detach"},
	{ID: ActionClientPanelReconnect, Kind: IntentWorkbenchCommand, Command: "pane reconnect"},
	{ID: ActionClientPanelRestart, Kind: IntentWorkbenchCommand, Command: "pane restart"},
	{ID: ActionClientPanelTakeOwner, Kind: IntentWorkbenchCommand, Command: "pane take-owner"},
	{ID: ActionClientPanelCloseKill, Kind: IntentWorkbenchCommand, Command: "pane kill confirm=accepted", Danger: true},
	{ID: ActionClientPanelSplitRight, Kind: IntentPaneCommand, Command: "pane split-right"},
	{ID: ActionClientPanelSplitDown, Kind: IntentPaneCommand, Command: "pane split-down"},
	{ID: ActionClientPanelToggleZoom, Kind: IntentPaneCommand, Command: "pane toggle-zoom"},
	{ID: ActionClientPanelBalance, Kind: IntentPaneCommand, Command: "pane balance"},
	{ID: ActionClientPanelPresentationSet, Kind: IntentPaneCommand, ParamKeys: []string{"style"}},
	{ID: ActionClientPanelFocusNext, Kind: IntentPaneCommand, Command: "pane focus-next"},
	{ID: ActionClientPanelFocusPrevious, Kind: IntentPaneCommand, Command: "pane focus-prev"},
	{ID: ActionClientPanelResize, Kind: IntentPaneCommand, ParamKeys: []string{"direction", "delta"}},
	{ID: ActionClientTerminalSizeLockToggle, Kind: IntentWorkbenchCommand, Command: "terminal size lock"},
	{ID: ActionClientTerminalViewToggle, Kind: IntentWorkbenchCommand, Command: "terminal layout toggle"},
	{ID: ActionClientTerminalViewPan, Kind: IntentWorkbenchCommand, ParamKeys: []string{"direction"}},
	{ID: ActionClientTerminalViewAlign, Kind: IntentWorkbenchCommand, ParamKeys: []string{"edge"}},
	{ID: ActionClientTerminalViewCenter, Kind: IntentWorkbenchCommand, ParamKeys: []string{"axis"}},
	{ID: ActionClientTerminalViewReset, Kind: IntentWorkbenchCommand, Command: "terminal layout reset"},
	{ID: ActionClientChromeHeaderToggle, Kind: IntentShellAction, Action: ShellActionToggleHeader},
	{ID: ActionClientChromeFooterToggle, Kind: IntentShellAction, Action: ShellActionToggleFooter},
	{ID: ActionClientToastClear, Kind: IntentShellAction, Action: ShellActionClearToasts},
	{ID: ActionClientToastClose, Kind: IntentShellAction, Action: ShellActionCloseToast},
	{ID: ActionClientTerminalPoolOpen, Kind: IntentShellAction, Action: ShellActionOpenPool},
	{ID: ActionClientWorkbenchTreeOpen, Kind: IntentShellAction, Action: ShellActionOpenTree},
	{ID: ActionClientShortcutLockToggle, Kind: IntentShellAction, Action: ShellActionToggleShortcutLock},
	{ID: ActionClientPromptOpen, Kind: IntentShellAction, Action: ShellActionOpenPrompt},
	{ID: ActionClientHelpOpen, Kind: IntentShellAction, Action: ShellActionOpenHelp},
	{ID: ActionClientSessionQuit, Kind: IntentShellAction, Action: ShellActionQuit, Danger: true},
	{ID: ActionClientFloatCreate, Kind: IntentShellAction, Action: ShellActionFloatingNew},
	{ID: ActionClientFloatOverview, Kind: IntentShellAction, Action: ShellActionFloatingOverview},
	{ID: ActionClientFloatSummon, Kind: IntentShellAction, Action: ShellActionFloatingSummon, ParamKeys: []string{"index"}},
	{ID: ActionClientFloatPickerOpen, Kind: IntentShellAction, Action: ShellActionOpenPicker},
	{ID: ActionClientFloatTakeOwner, Kind: IntentWorkbenchCommand, Command: "floating take-owner"},
	{ID: ActionClientFloatClose, Kind: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "close", Danger: true},
	{ID: ActionClientFloatCollapse, Kind: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "collapse"},
	{ID: ActionClientFloatCenter, Kind: IntentShellAction, Action: ShellActionFloatingCtrl, Reason: "center"},
	{ID: ActionClientFloatToggleAll, Kind: IntentShellAction, Action: ShellActionFloatingGroup, Reason: "toggle-all"},
	{ID: ActionClientFloatFit, Kind: IntentShellAction, Action: ShellActionFloatingGroup, Reason: "fit"},
	{ID: ActionClientFloatAutoFitToggle, Kind: IntentShellAction, Action: ShellActionFloatingGroup, Reason: "toggle-auto-fit"},
	{ID: ActionClientFloatMove, Kind: IntentShellAction, Action: ShellActionFloatingMove, ParamKeys: []string{"direction"}},
	{ID: ActionClientFloatResize, Kind: IntentShellAction, Action: ShellActionFloatingSize, ParamKeys: []string{"direction"}},
	{ID: ActionClientTabCreate, Kind: IntentWorkbenchCommand, Command: "tab create"},
	{ID: ActionClientTabNext, Kind: IntentWorkbenchCommand, Command: "tab next"},
	{ID: ActionClientTabPrevious, Kind: IntentWorkbenchCommand, Command: "tab previous"},
	{ID: ActionClientTabActivate, Kind: IntentWorkbenchCommand, ParamKeys: []string{"index"}},
	{ID: ActionClientTabRename, Kind: IntentWorkbenchCommand, Command: "tab rename"},
	{ID: ActionClientTabClose, Kind: IntentWorkbenchCommand, Command: "tab close", Danger: true},
	{ID: ActionClientTabCloseKillTerminals, Kind: IntentWorkbenchCommand, Command: "tab kill confirm=accepted", Danger: true},
	{ID: ActionClientWorkspaceCreate, Kind: IntentWorkbenchCommand, Command: "workspace create"},
	{ID: ActionClientWorkspaceNext, Kind: IntentWorkbenchCommand, Command: "workspace next"},
	{ID: ActionClientWorkspacePrev, Kind: IntentWorkbenchCommand, Command: "workspace previous"},
	{ID: ActionClientWorkspaceRename, Kind: IntentWorkbenchCommand, Command: "workspace rename"},
	{ID: ActionClientWorkspaceDelete, Kind: IntentWorkbenchCommand, Command: "workspace delete confirm=accepted", Danger: true},
}

var clientActionSpecByID = buildClientActionSpecIndex(clientActionSpecs)

func buildClientActionSpecIndex(specs []ClientActionSpec) map[plugin.ActionID]ClientActionSpec {
	out := make(map[plugin.ActionID]ClientActionSpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" {
			panic("input client action spec id is required")
		}
		if _, exists := out[spec.ID]; exists {
			panic("duplicate input client action spec " + string(spec.ID))
		}
		out[spec.ID] = spec
	}
	return out
}

func buildActionBindingCatalog(raw []Binding) []Binding {
	out := make([]Binding, 0, len(raw))
	for _, binding := range raw {
		actionID, args, err := clientActionForLegacyBinding(binding)
		if err != nil {
			panic(err)
		}
		out = append(out, Binding{
			ID:         binding.ID,
			Mode:       binding.Mode,
			Key:        binding.Key,
			Char:       binding.Char,
			Ctrl:       binding.Ctrl,
			Alt:        binding.Alt,
			Shift:      binding.Shift,
			ActionID:   actionID,
			ActionArgs: args,
		})
	}
	return out
}

func clientActionForLegacyBinding(binding Binding) (plugin.ActionID, map[string]string, error) {
	switch binding.Intent {
	case IntentSetInteractionMode:
		switch binding.Target {
		case InteractionModePane:
			return ActionClientModePaneEnter, nil, nil
		case InteractionModeResize:
			return ActionClientModeResizeEnter, nil, nil
		case InteractionModeGlobal:
			return ActionClientModeGlobalEnter, nil, nil
		case InteractionModeFloating:
			return ActionClientModeFloatingEnter, nil, nil
		case InteractionModeTab:
			return ActionClientModeTabEnter, nil, nil
		case InteractionModeWorkspace:
			return ActionClientModeWorkspaceEnter, nil, nil
		default:
			return "", nil, fmt.Errorf("binding %s has unknown interaction target %q", binding.ID, binding.Target)
		}
	case IntentOpenTerminalPicker:
		return ActionClientTerminalPickerOpen, nil, nil
	case IntentEnterCopyMode:
		return ActionClientCopyModeEnter, nil, nil
	case IntentPaneCommand, IntentWorkbenchCommand:
		return clientActionForCommand(binding)
	case IntentShellAction:
		return clientActionForShell(binding)
	default:
		return "", nil, fmt.Errorf("binding %s has unmapped intent %q", binding.ID, binding.Intent)
	}
}

func clientActionForCommand(binding Binding) (plugin.ActionID, map[string]string, error) {
	switch binding.Command {
	case "pane close":
		return ActionClientPanelClose, nil, nil
	case "pane detach":
		return ActionClientPanelDetach, nil, nil
	case "pane reconnect":
		return ActionClientPanelReconnect, nil, nil
	case "pane restart":
		return ActionClientPanelRestart, nil, nil
	case "pane take-owner":
		return ActionClientPanelTakeOwner, nil, nil
	case "pane kill confirm=accepted":
		return ActionClientPanelCloseKill, nil, nil
	case "pane split-right":
		return ActionClientPanelSplitRight, nil, nil
	case "pane split-down":
		return ActionClientPanelSplitDown, nil, nil
	case "pane toggle-zoom":
		return ActionClientPanelToggleZoom, nil, nil
	case "pane balance":
		return ActionClientPanelBalance, nil, nil
	case "pane presentation card":
		return ActionClientPanelPresentationSet, map[string]string{"style": "card"}, nil
	case "pane presentation split-line":
		return ActionClientPanelPresentationSet, map[string]string{"style": "split-line"}, nil
	case "pane focus-next":
		return ActionClientPanelFocusNext, nil, nil
	case "pane focus-prev":
		return ActionClientPanelFocusPrevious, nil, nil
	case "terminal size lock":
		return ActionClientTerminalSizeLockToggle, nil, nil
	case "terminal layout toggle":
		return ActionClientTerminalViewToggle, nil, nil
	case "terminal layout reset":
		return ActionClientTerminalViewReset, nil, nil
	case "floating take-owner":
		return ActionClientFloatTakeOwner, nil, nil
	case "tab create":
		return ActionClientTabCreate, nil, nil
	case "tab next":
		return ActionClientTabNext, nil, nil
	case "tab previous":
		return ActionClientTabPrevious, nil, nil
	case "tab rename":
		return ActionClientTabRename, nil, nil
	case "tab close":
		return ActionClientTabClose, nil, nil
	case "tab kill confirm=accepted":
		return ActionClientTabCloseKillTerminals, nil, nil
	case "workspace create":
		return ActionClientWorkspaceCreate, nil, nil
	case "workspace next":
		return ActionClientWorkspaceNext, nil, nil
	case "workspace previous":
		return ActionClientWorkspacePrev, nil, nil
	case "workspace rename":
		return ActionClientWorkspaceRename, nil, nil
	case "workspace delete confirm=accepted":
		return ActionClientWorkspaceDelete, nil, nil
	}
	if direction, delta, ok := parsePanelResizeCommand(binding.Command); ok {
		return ActionClientPanelResize, map[string]string{"direction": direction, "delta": delta}, nil
	}
	if index, ok := parseTabJumpCommand(binding.Command); ok {
		return ActionClientTabActivate, map[string]string{"index": strconv.Itoa(index)}, nil
	}
	if direction, ok := parseTerminalLayoutPrefix(binding.Command, "terminal layout pan-"); ok {
		return ActionClientTerminalViewPan, map[string]string{"direction": direction}, nil
	}
	if edge, ok := parseTerminalLayoutPrefix(binding.Command, "terminal layout align-"); ok {
		return ActionClientTerminalViewAlign, map[string]string{"edge": edge}, nil
	}
	if axis, ok := parseTerminalCenterCommand(binding.Command); ok {
		return ActionClientTerminalViewCenter, map[string]string{"axis": axis}, nil
	}
	return "", nil, fmt.Errorf("binding %s has unmapped command %q", binding.ID, binding.Command)
}

func clientActionForShell(binding Binding) (plugin.ActionID, map[string]string, error) {
	switch binding.Action {
	case ShellActionToggleHeader:
		return ActionClientChromeHeaderToggle, nil, nil
	case ShellActionToggleFooter:
		return ActionClientChromeFooterToggle, nil, nil
	case ShellActionClearToasts:
		return ActionClientToastClear, nil, nil
	case ShellActionCloseToast:
		return ActionClientToastClose, nil, nil
	case ShellActionOpenPool:
		return ActionClientTerminalPoolOpen, nil, nil
	case ShellActionOpenTree:
		return ActionClientWorkbenchTreeOpen, nil, nil
	case ShellActionToggleShortcutLock:
		return ActionClientShortcutLockToggle, nil, nil
	case ShellActionOpenPicker:
		return ActionClientFloatPickerOpen, nil, nil
	case ShellActionOpenPrompt:
		return ActionClientPromptOpen, nil, nil
	case ShellActionOpenHelp:
		return ActionClientHelpOpen, nil, nil
	case ShellActionQuit:
		return ActionClientSessionQuit, nil, nil
	case ShellActionFloatingNew:
		return ActionClientFloatCreate, nil, nil
	case ShellActionFloatingOverview:
		return ActionClientFloatOverview, nil, nil
	case ShellActionFloatingSummon:
		return ActionClientFloatSummon, map[string]string{"index": binding.Reason}, nil
	case ShellActionFloatingCtrl:
		switch binding.Reason {
		case "close":
			return ActionClientFloatClose, nil, nil
		case "collapse":
			return ActionClientFloatCollapse, nil, nil
		case "center":
			return ActionClientFloatCenter, nil, nil
		}
	case ShellActionFloatingGroup:
		switch binding.Reason {
		case "toggle-all":
			return ActionClientFloatToggleAll, nil, nil
		case "fit":
			return ActionClientFloatFit, nil, nil
		case "toggle-auto-fit":
			return ActionClientFloatAutoFitToggle, nil, nil
		}
	case ShellActionFloatingMove:
		return ActionClientFloatMove, map[string]string{"direction": binding.Reason}, nil
	case ShellActionFloatingSize:
		return ActionClientFloatResize, map[string]string{"direction": binding.Reason}, nil
	}
	return "", nil, fmt.Errorf("binding %s has unmapped shell action %q reason=%q", binding.ID, binding.Action, binding.Reason)
}

func parsePanelResizeCommand(command string) (string, string, bool) {
	var direction string
	var delta int
	if _, err := fmt.Sscanf(command, "pane resize %s delta=%d", &direction, &delta); err != nil {
		return "", "", false
	}
	switch direction {
	case "left", "right", "up", "down":
		return direction, strconv.Itoa(delta), true
	default:
		return "", "", false
	}
}

func parseTabJumpCommand(command string) (int, bool) {
	var index int
	if _, err := fmt.Sscanf(command, "tab jump %d", &index); err != nil {
		return 0, false
	}
	return index, index >= 1 && index <= 9
}

func parseTerminalLayoutPrefix(command string, prefix string) (string, bool) {
	if len(command) <= len(prefix) || command[:len(prefix)] != prefix {
		return "", false
	}
	value := command[len(prefix):]
	return value, value != ""
}

func parseTerminalCenterCommand(command string) (string, bool) {
	switch command {
	case "terminal layout center":
		return "", true
	case "terminal layout center-x":
		return "x", true
	case "terminal layout center-y":
		return "y", true
	default:
		return "", false
	}
}

func cloneActionArgs(args map[string]string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(args))
	for key, value := range args {
		out[key] = value
	}
	return out
}
