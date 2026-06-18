package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ShellSetPanelPresentationMsg struct {
	Presentation state.PanelPresentation
}

func (ShellSetPanelPresentationMsg) isMsg() {}

type ShellTogglePanelPresentationMsg struct{}

func (ShellTogglePanelPresentationMsg) isMsg() {}

type ShellSetHeaderVisibleMsg struct {
	Visible bool
}

func (ShellSetHeaderVisibleMsg) isMsg() {}

type ShellToggleHeaderVisibleMsg struct{}

func (ShellToggleHeaderVisibleMsg) isMsg() {}

type ShellSetFooterVisibleMsg struct {
	Visible bool
}

func (ShellSetFooterVisibleMsg) isMsg() {}

type ShellToggleFooterVisibleMsg struct{}

func (ShellToggleFooterVisibleMsg) isMsg() {}

type ShellAddToastMsg struct {
	Toast state.ToastSpec
}

func (ShellAddToastMsg) isMsg() {}

type ShellTickToastsMsg struct {
	Ticks uint64
}

func (ShellTickToastsMsg) isMsg() {}

type ShellCloseCurrentToastMsg struct{}

func (ShellCloseCurrentToastMsg) isMsg() {}

type ShellClearToastsMsg struct{}

func (ShellClearToastsMsg) isMsg() {}

type ShellOpenTerminalPickerMsg struct{}

func (ShellOpenTerminalPickerMsg) isMsg() {}

type ShellOpenTerminalPoolMsg struct{}

func (ShellOpenTerminalPoolMsg) isMsg() {}

type ShellOpenWorkbenchTreeMsg struct{}

func (ShellOpenWorkbenchTreeMsg) isMsg() {}

type ShellOpenFloatingOverviewMsg struct{}

func (ShellOpenFloatingOverviewMsg) isMsg() {}

type ShellOpenPromptMsg struct {
	Prompt state.PromptState
}

func (ShellOpenPromptMsg) isMsg() {}

type ShellOpenHelpMsg struct {
	Section string
}

func (ShellOpenHelpMsg) isMsg() {}

type ShellCloseOverlayMsg struct{}

func (ShellCloseOverlayMsg) isMsg() {}

type ShellPromptSetValueMsg struct {
	Value string
}

func (ShellPromptSetValueMsg) isMsg() {}

type ShellPromptSubmitMsg struct{}

func (ShellPromptSubmitMsg) isMsg() {}

type ShellPromptCancelMsg struct{}

func (ShellPromptCancelMsg) isMsg() {}

type ShellContentActionMsg struct {
	ActionID string
	PaneID   string
	Floating bool
	Row      int
}

func (ShellContentActionMsg) isMsg() {}

type ShellMoveClipboardHistoryDividerMsg struct {
	Delta int
}

func (ShellMoveClipboardHistoryDividerMsg) isMsg() {}

type ShellActivateTerminalInputMsg struct {
	PaneID     string
	FloatingID string
}

func (ShellActivateTerminalInputMsg) isMsg() {}

type ShellOverlayMouseSelectMsg struct {
	Delta int
}

func (ShellOverlayMouseSelectMsg) isMsg() {}

type ShellArmOwnerConfirmMsg struct {
	ViewID string
}

func (ShellArmOwnerConfirmMsg) isMsg() {}

type ShellClearOwnerConfirmMsg struct {
	Seq uint64
}

func (ShellClearOwnerConfirmMsg) isMsg() {}

type HostThemeMsg struct {
	Update state.HostThemeUpdate
}

func (HostThemeMsg) isMsg() {}

const ownerConfirmDelay = 500 * time.Millisecond

const ownerConfirmClearToken CancelToken = "terminal.owner.confirm.clear"

type ShellSplitActivePaneMsg struct {
	Pane      state.PaneState
	Direction state.SplitDirection
}

func (ShellSplitActivePaneMsg) isMsg() {}

type ShellPaneCommandMsg struct {
	Command state.PaneCommand
}

func (ShellPaneCommandMsg) isMsg() {}

type ShellFloatingCommandMsg struct {
	Command state.FloatingCommand
}

func (ShellFloatingCommandMsg) isMsg() {}

type ShellWorkbenchCommandMsg struct {
	Command state.WorkbenchCommand
}

func (ShellWorkbenchCommandMsg) isMsg() {}

type PaneCommandFeedbackEffect struct {
	Result  state.PaneCommandResult
	Command state.PaneCommand
}

func (PaneCommandFeedbackEffect) isEffect() {}

func NewShellReducer() Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case ShellSetPanelPresentationMsg:
			return reducePaneCommand(root, state.PaneCommand{
				Action:       state.PaneCommandSetPresentation,
				Presentation: msg.Presentation,
				Source:       state.PaneCommandSourceTest,
			})
		case ShellTogglePanelPresentationMsg:
			return reducePaneCommand(root, state.PaneCommand{
				Action: state.PaneCommandTogglePresentation,
				Source: state.PaneCommandSourceTest,
			})
		case ShellSetHeaderVisibleMsg:
			root.Shell = root.Shell.SetHeaderVisible(msg.Visible)
		case ShellToggleHeaderVisibleMsg:
			root.Shell = root.Shell.ToggleHeaderVisible()
		case ShellSetFooterVisibleMsg:
			root.Shell = root.Shell.SetFooterVisible(msg.Visible)
		case ShellToggleFooterVisibleMsg:
			root.Shell = root.Shell.ToggleFooterVisible()
		case ShellAddToastMsg:
			root.Shell = root.Shell.AddToast(msg.Toast)
		case ShellTickToastsMsg:
			root.Shell = root.Shell.TickToasts(msg.Ticks)
		case TickMsg:
			if msg.Token != "" && msg.Token != toastTickToken {
				return root, nil
			}
			if len(root.Shell.Toasts) == 0 {
				return root, nil
			}
			ticks := msg.Ticks
			if ticks == 0 {
				ticks = 1
			}
			root.Shell = root.Shell.TickToasts(ticks)
		case ShellCloseCurrentToastMsg:
			root.Shell = root.Shell.CloseCurrentToast()
		case ShellClearToastsMsg:
			root.Shell = root.Shell.ClearToasts()
		case ShellOpenTerminalPickerMsg:
			root.Shell = root.Shell.OpenTerminalPicker()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
		case ShellOpenTerminalPoolMsg:
			root.Shell = root.Shell.OpenTerminalPool()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
		case ShellOpenWorkbenchTreeMsg:
			root.Shell = openWorkbenchTreeAtActivePane(root)
		case ShellOpenFloatingOverviewMsg:
			root.Shell = root.Shell.OpenFloatingOverview()
		case ShellOpenPromptMsg:
			root.Shell = root.Shell.OpenPrompt(msg.Prompt)
		case ShellOpenHelpMsg:
			root.Shell = root.Shell.OpenHelp(msg.Section)
		case ShellCloseOverlayMsg:
			root.Shell = root.Shell.CloseOverlay()
		case ShellPromptSetValueMsg:
			root.Shell = root.Shell.SetPromptValue(msg.Value)
		case ShellPromptSubmitMsg:
			return reducePromptSubmit(root)
		case ShellPromptCancelMsg:
			root.Shell = root.Shell.CancelPrompt().CloseOverlay()
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "prompt.cancel", Body: "canceled"})
		case ShellContentActionMsg:
			next, effects := reduceShellContentAction(root, msg)
			return rearmInteractionModeTimeout(next, effects)
		case ShellMoveClipboardHistoryDividerMsg:
			root.Shell = root.Shell.MoveClipboardHistoryNameWidth(msg.Delta)
		case ShellActivateTerminalInputMsg:
			return reduceShellActivateTerminalInput(root, msg)
		case ShellArmOwnerConfirmMsg:
			root.Shell = root.Shell.ArmOwnerConfirm(msg.ViewID)
			seq := root.Shell.OwnerConfirm.Seq
			return root.Advance(), []Effect{ownerConfirmClearEffect(seq)}
		case ShellClearOwnerConfirmMsg:
			root.Shell = root.Shell.ClearOwnerConfirm(msg.Seq)
		case HostThemeMsg:
			root.HostTheme = root.HostTheme.ApplyUpdate(msg.Update)
		case ShellSplitActivePaneMsg:
			return reduceWorkbenchCommand(root, state.WorkbenchCommand{
				Action: state.WorkbenchCommandPaneSplit,
				Pane: state.PaneCommand{
					Action:         state.PaneCommandSplit,
					SplitDirection: msg.Direction,
					NewPane:        msg.Pane,
					Source:         state.PaneCommandSourceTest,
				},
				Source: state.PaneCommandSourceTest,
			})
		case ShellPaneCommandMsg:
			return reducePaneCommand(root, msg.Command)
		case ShellFloatingCommandMsg:
			return reduceFloatingCommand(root, msg.Command)
		case ShellWorkbenchCommandMsg:
			return reduceWorkbenchCommand(root, msg.Command)
		default:
			return root, nil
		}
		return root.Advance(), nil
	}
}

func ownerConfirmClearEffect(seq uint64) Effect {
	return FuncEffect{
		Token: ownerConfirmClearToken,
		Async: true,
		Run: func(ctx context.Context) Msg {
			// owner? 是临时确认提示，超时后必须退回 authoritative follower 展示。
			timer := time.NewTimer(ownerConfirmDelay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil
			case <-timer.C:
				return ShellClearOwnerConfirmMsg{Seq: seq}
			}
		},
	}
}

func openWorkbenchTreeAtActivePane(root state.Root) state.ShellStore {
	shell := root.Shell.OpenWorkbenchTree()
	activePaneID := shell.ActivePaneID
	if activePaneID == "" {
		return shell
	}
	root.Shell = shell
	items := state.WorkbenchTreeItems(root)
	for index, item := range items {
		if item.Kind == state.WorkbenchTreeKindPane && item.PaneID == activePaneID {
			return shell.SetWorkbenchTreeSelectedIndex(index, len(items))
		}
	}
	return shell
}

func reduceShellActivateTerminalInput(root state.Root, msg ShellActivateTerminalInputMsg) (state.Root, []Effect) {
	shell := root.Shell.EnsureDefaults()
	if msg.FloatingID != "" {
		command := state.FloatingCommand{
			Action:   state.FloatingCommandFocusRaise,
			TargetID: msg.FloatingID,
			Source:   state.PaneCommandSourceMouse,
		}
		nextShell, result := shell.ApplyFloatingCommand(command)
		root.Shell = addFloatingCommandToast(nextShell, command, result)
		if result.Status == state.FloatingCommandOK {
			// 点击 terminal 内容代表回到输入态，后续键盘输入必须直达 active TerminalView。
			root.Shell = root.Shell.ExitInteractionMode()
		}
		return root.Advance(), nil
	}
	if msg.PaneID == "" {
		root.Shell = shell.ExitInteractionMode()
		return root.Advance(), nil
	}
	command := state.PaneCommand{
		Action: state.PaneCommandFocus,
		Target: state.PaneCommandTarget{
			PaneID: msg.PaneID,
		},
		Source: state.PaneCommandSourceMouse,
	}
	command = command.WithDefaults(shell)
	targetPane, hasTargetPane := shell.Pane(command.Target)
	nextShell, result := shell.ApplyPaneCommand(command)
	if result.Status == state.PaneCommandOK {
		root.Shell = deactivateFloatingAfterPaneCommand(nextShell, command).ExitInteractionMode()
		root = updateTerminalViewsAfterPaneCommand(root, command, targetPane, hasTargetPane)
		root.Shell = addPaneCommandToast(root.Shell, command, result)
		return root.Advance(), paneCommandEffects(command, result, targetPane, hasTargetPane)
	}
	root.Shell = addPaneCommandToast(shell, command, result)
	return root.Advance(), []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
}

func reduceShellContentAction(root state.Root, msg ShellContentActionMsg) (state.Root, []Effect) {
	if msg.Floating {
		if floatingID, ok := floatingIDForContentAction(root, msg); ok && floatingID != "" {
			root.Shell, _ = root.Shell.ApplyFloatingCommand(state.FloatingCommand{
				Action:   state.FloatingCommandFocusRaise,
				TargetID: floatingID,
				Source:   state.PaneCommandSourceMouse,
			})
		}
	} else if msg.PaneID != "" {
		root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{PaneID: msg.PaneID})
	}
	spec, ok := render.ActionSpecByIDString(msg.ActionID)
	if !ok || spec.Dispatch == render.ActionDispatchNone {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "content action", Body: "unknown " + msg.ActionID})
		return root.Advance(), nil
	}
	if spec.Dispatch != render.ActionDispatchApp {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "content action", Body: "unsupported " + msg.ActionID})
		return root.Advance(), nil
	}
	switch spec.ID {
	case render.ActionTabSwitch:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabSwitch, TargetID: msg.PaneID, Source: state.PaneCommandSourceMouse})
	case render.ActionTabClose:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabClose, TargetID: msg.PaneID, Source: state.PaneCommandSourceMouse})
	case render.ActionTabCreate:
		shell := root.Shell.EnsureDefaults()
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: nextTabName(shell), Source: state.PaneCommandSourceMouse})
	case render.ActionTabRename:
		root.Shell = root.Shell.OpenPrompt(tabRenamePrompt(root.Shell.EnsureDefaults()))
		return root.Advance(), nil
	case render.ActionTabPrevious:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabPrevious, Source: state.PaneCommandSourceMouse})
	case render.ActionTabNext:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabNext, Source: state.PaneCommandSourceMouse})
	case render.ActionFooterPaneMode:
		root.Shell = root.Shell.SetInteractionMode(state.InteractionModePane)
		return root.Advance(), nil
	case render.ActionFooterResizeMode:
		root.Shell = root.Shell.SetInteractionMode(state.InteractionModeResize)
		return root.Advance(), nil
	case render.ActionFooterTabMode:
		root.Shell = root.Shell.SetInteractionMode(state.InteractionModeTab)
		return root.Advance(), nil
	case render.ActionFooterWorkspaceMode:
		root.Shell = root.Shell.SetInteractionMode(state.InteractionModeWorkspace)
		return root.Advance(), nil
	case render.ActionFooterFloatingMode:
		root.Shell = root.Shell.SetInteractionMode(state.InteractionModeFloating)
		return root.Advance(), nil
	case render.ActionFooterCopyMode:
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "v", Ctrl: true}}
		}}}
	case render.ActionFooterGlobalMode:
		root.Shell = root.Shell.SetInteractionMode(state.InteractionModeGlobal)
		return root.Advance(), nil
	case render.ActionFooterPicker:
		root.Shell = root.Shell.OpenTerminalPicker()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	case render.ActionFooterToggleHeader:
		root.Shell = root.Shell.ToggleHeaderVisible()
		return root.Advance(), nil
	case render.ActionFooterToggleFooter:
		root.Shell = root.Shell.ToggleFooterVisible()
		return root.Advance(), nil
	case render.ActionFooterOpenPool:
		root.Shell = root.Shell.OpenTerminalPool()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	case render.ActionFooterOpenTree:
		root.Shell = root.Shell.OpenWorkbenchTree()
		return root.Advance(), nil
	case render.ActionClipboardHistorySelect:
		items := state.ClipboardHistoryItems(root)
		root.Shell = root.Shell.SetClipboardHistorySelectedIndex(msg.Row, len(items))
		return root.Advance(), nil
	case render.ActionClipboardHistoryPaste:
		return reduceClipboardHistoryPaste(root)
	case render.ActionClipboardHistoryNew:
		return reduceClipboardHistoryNew(root)
	case render.ActionClipboardHistoryEdit:
		return reduceClipboardHistoryEdit(root)
	case render.ActionClipboardHistoryDelete:
		return reduceClipboardHistoryDelete(root)
	case render.ActionFooterCloseToast:
		root.Shell = root.Shell.CloseCurrentToast()
		return root.Advance(), nil
	case render.ActionFooterClearToasts:
		root.Shell = root.Shell.ClearToasts()
		return root.Advance(), nil
	case render.ActionFooterQuit:
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg { return QuitMsg{} }}}
	case render.ActionFooterNewWorkspace:
		shell := root.Shell.EnsureDefaults()
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: nextWorkspaceName(shell), Source: state.PaneCommandSourceMouse})
	case render.ActionFooterRenameWorkspace:
		root.Shell = root.Shell.OpenPrompt(workspaceRenamePrompt(root.Shell.EnsureDefaults()))
		return root.Advance(), nil
	case render.ActionFooterPreviousWorkspace:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspacePrevious, Source: state.PaneCommandSourceMouse})
	case render.ActionFooterNextWorkspace:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceNext, Source: state.PaneCommandSourceMouse})
	case render.ActionFooterDeleteWorkspace:
		shell := root.Shell.EnsureDefaults()
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceDelete, TargetID: shell.Workspace.ID, Confirm: state.PaneConfirmAccepted, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterClose:
		shell := root.Shell.EnsureDefaults()
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandPaneClose, Target: state.PaneCommandTarget{PaneID: shell.ActivePaneID}, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterDetach:
		shell := root.Shell.EnsureDefaults()
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandPaneDetach, Target: state.PaneCommandTarget{PaneID: shell.ActivePaneID}, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterFocus:
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandFocusNext, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterSplitRight:
		shell := root.Shell.EnsureDefaults()
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandSplit, Target: state.PaneCommandTarget{PaneID: shell.ActivePaneID}, SplitDirection: state.SplitDirectionVertical, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterSplitDown:
		shell := root.Shell.EnsureDefaults()
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandSplit, Target: state.PaneCommandTarget{PaneID: shell.ActivePaneID}, SplitDirection: state.SplitDirectionHorizontal, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterZoom:
		shell := root.Shell.EnsureDefaults()
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandToggleZoom, Target: state.PaneCommandTarget{PaneID: shell.ActivePaneID}, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterBalance:
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandBalance, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterCard:
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandSetPresentation, Presentation: state.PanelPresentationCard, Source: state.PaneCommandSourceMouse})
	case render.ActionPaneFooterSplitLine:
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandSetPresentation, Presentation: state.PanelPresentationSplitLine, Source: state.PaneCommandSourceMouse})
	case render.ActionResizeLeft, render.ActionResizeRight, render.ActionResizeUp, render.ActionResizeDown:
		command, ok := resizeFooterPaneCommand(spec.ID)
		if !ok {
			break
		}
		return reducePaneCommand(root, command)
	case render.ActionResizeBalance:
		return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandBalance, Source: state.PaneCommandSourceMouse})
	case render.ActionResizeLayoutLock:
		return root, []Effect{terminalSizeLockToggleEffect()}
	case render.ActionResizeLayoutToggle:
		return applyActiveTerminalViewLayoutCommand(root, state.TerminalViewLayoutCommand{Action: "toggle-layout"})
	case render.ActionResizeLayoutPan:
		return applyActiveTerminalViewLayoutCommand(root, state.TerminalViewLayoutCommand{Action: "pan", DeltaX: 2})
	case render.ActionResizeLayoutAlign:
		return applyActiveTerminalViewLayoutCommand(root, state.TerminalViewLayoutCommand{Action: "align", AlignX: state.TerminalViewAlignCenter, AlignY: state.TerminalViewAlignCenter})
	case render.ActionResizeLayoutCenter:
		return applyActiveTerminalViewLayoutCommand(root, state.TerminalViewLayoutCommand{Action: "center"})
	case render.ActionResizeLayoutReset:
		return applyActiveTerminalViewLayoutCommand(root, state.TerminalViewLayoutCommand{Action: "reset"})
	case render.ActionCopyOlder:
		// copy footer 只生成等价 PageUp 输入，authoritative history 请求仍由 copy reducer 统一处理。
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}}
		}}}
	case render.ActionTerminalTakeResizeOwner:
		root.Shell = root.Shell.ClearOwnerConfirm(0)
		if msg.Floating {
			return requestFloatingResizeOwner(root, floatingTargetIDForContentAction(root, msg))
		}
		return requestPaneResizeOwner(root, msg.PaneID)
	case render.ActionEmptyClose, render.ActionExitedClose:
		if msg.Floating {
			return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
		}
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{
			Action: state.WorkbenchCommandPaneClose,
			Target: state.PaneCommandTarget{PaneID: msg.PaneID},
			Source: state.PaneCommandSourceMouse,
		})
	case render.ActionPickerAttach:
		items := state.TerminalPickerItems(root)
		if msg.Row >= 0 {
			root.Shell = root.Shell.SetTerminalPickerSelectedIndex(msg.Row, len(items))
		}
		selected, ok := terminalPickerItemAt(items, msg.Row)
		if !ok && msg.PaneID != "" {
			selected = state.TerminalPickerItem{PaneID: msg.PaneID}
			ok = true
		}
		if ok && selected.CreateNew {
			target := terminalPoolTargetForOverlay(root)
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return ShellOpenPromptMsg{Prompt: createTerminalPromptForTarget(root, target)}
			}}}
		}
		if ok && selected.TerminalID != "" {
			target := terminalPoolTargetForOverlay(root)
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID, TargetPaneID: target.PaneID, TargetFloatingID: target.FloatingID}
			}}}
		}
	case render.ActionPickerNew:
		target := terminalPoolTargetForOverlay(root)
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return ShellOpenPromptMsg{Prompt: createTerminalPromptForTarget(root, target)}
		}}}
	case render.ActionPickerSplit:
		selected, ok := terminalPickerItemAt(state.TerminalPickerItems(root), msg.Row)
		if !ok || selected.TerminalID == "" {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.split", Body: "no terminal"})
			return root.Advance(), nil
		}
		shell := root.Shell.EnsureDefaults()
		next, effects := reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandSplit, Target: state.PaneCommandTarget{PaneID: shell.ActivePaneID}, SplitDirection: state.SplitDirectionVertical, NewPane: state.PaneState{ID: nextKeyboardPaneID(shell), Title: "pane", Kind: state.PaneEmpty}, Source: state.PaneCommandSourceKeyboard})
		targetPaneID := next.Shell.EnsureDefaults().ActivePaneID
		return next, append(effects, FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID, TargetPaneID: targetPaneID}
		}})
	case render.ActionPickerEdit:
		selected, ok := terminalPickerItemAt(state.TerminalPickerItems(root), msg.Row)
		if !ok || selected.TerminalID == "" {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.edit", Body: "no terminal"})
			return root.Advance(), nil
		}
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolEditRequestMsg{TerminalID: selected.TerminalID, Title: selected.Title, Tags: map[string]string{"edited-by": "termx-tui-v3"}}
		}}}
	case render.ActionPickerKill:
		selected, ok := terminalPickerItemAt(state.TerminalPickerItems(root), msg.Row)
		if !ok || selected.TerminalID == "" {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.kill", Body: "no terminal"})
			return root.Advance(), nil
		}
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolKillRequestMsg{TerminalID: selected.TerminalID}
		}}}
	case render.ActionPickerDelete:
		selected, ok := terminalPickerItemAt(state.TerminalPickerItems(root), msg.Row)
		if !ok || selected.TerminalID == "" {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.delete", Body: "no terminal"})
			return root.Advance(), nil
		}
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolRemoveRequestMsg{TerminalID: selected.TerminalID}
		}}}
	case render.ActionPoolSelect:
		items := state.TerminalPoolPageItems(root)
		root.Shell = root.Shell.SetTerminalPoolSelectedIndex(msg.Row, len(items))
		return root.Advance(), nil
	case render.ActionPoolAttach:
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			target := terminalPoolTargetForOverlay(root)
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID, TargetPaneID: target.PaneID, TargetFloatingID: target.FloatingID}
			}}}
		}
	case render.ActionPoolAttachTab:
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			next, effects := reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: nextTabName(root.Shell.EnsureDefaults()), Source: state.PaneCommandSourceKeyboard})
			targetPaneID := next.Shell.EnsureDefaults().ActivePaneID
			return next, append(effects, FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID, TargetPaneID: targetPaneID}
			}})
		}
	case render.ActionPoolAttachFloat:
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			floatingID := nextFloatingID(root.Shell)
			next, effects := reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandCreate, TargetID: floatingID, Pane: state.PaneState{ID: nextFloatingPaneID(root.Shell), Title: "floating", Kind: state.PaneEmpty}, Title: "floating", Source: state.PaneCommandSourceKeyboard})
			return next, append(effects, FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID, TargetFloatingID: floatingID}
			}})
		}
	case render.ActionPoolKill:
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolKillRequestMsg{TerminalID: selected.TerminalID}
			}}}
		}
	case render.ActionPoolEdit:
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			tags := cloneStringMap(selected.Tags)
			if tags == nil {
				tags = map[string]string{}
			}
			tags["edited-by"] = "termx-tui-v3"
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolEditRequestMsg{TerminalID: selected.TerminalID, Title: selected.Title, Tags: tags}
			}}}
		}
	case render.ActionPoolDelete:
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolRemoveRequestMsg{TerminalID: selected.TerminalID}
			}}}
		}
	case render.ActionWorkbenchSelect:
		items := state.WorkbenchTreeItems(root)
		root.Shell = root.Shell.SetWorkbenchTreeSelectedIndex(msg.Row, len(items))
		return root.Advance(), nil
	case render.ActionWorkbenchOpen:
		items := state.WorkbenchTreeItems(root)
		if msg.Row >= 0 {
			root.Shell = root.Shell.SetWorkbenchTreeSelectedIndex(msg.Row, len(items))
			items = state.WorkbenchTreeItems(root)
		}
		return reduceWorkbenchTreeOpen(root, items)
	case render.ActionWorkbenchRename:
		return reduceWorkbenchTreeRename(root, state.WorkbenchTreeItems(root))
	case render.ActionWorkbenchNew:
		return reduceWorkbenchTreeNew(root, state.WorkbenchTreeItems(root))
	case render.ActionWorkbenchDelete:
		return reduceWorkbenchTreeDelete(root, state.WorkbenchTreeItems(root))
	case render.ActionWorkbenchDetach:
		return reduceWorkbenchTreeDetach(root, state.WorkbenchTreeItems(root))
	case render.ActionWorkbenchZoom:
		return reduceWorkbenchTreeZoom(root, state.WorkbenchTreeItems(root))
	case render.ActionClipboardHistoryOpen:
		root.Shell = root.Shell.OpenClipboardHistory()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
			return ClipboardStorageLoadRequestMsg{Reason: "open"}
		}}}
	case render.ActionPromptSubmit:
		return reducePromptSubmit(root)
	case render.ActionPromptCancel:
		root.Shell = root.Shell.CloseOverlay()
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "prompt.cancel", Body: "canceled"})
		return root.Advance(), nil
	case render.ActionHelpOpen:
		root.Shell = root.Shell.OpenHelp("most-used")
		return root.Advance(), nil
	case render.ActionHelpClose:
		root.Shell = root.Shell.CloseOverlay()
		return root.Advance(), nil
	case render.ActionFloatingNew:
		return reduceFloatingCommand(root, state.FloatingCommand{
			Action:   state.FloatingCommandCreate,
			TargetID: nextFloatingID(root.Shell),
			Pane:     state.PaneState{ID: nextFloatingPaneID(root.Shell), Title: "floating", Kind: state.PaneEmpty},
			Title:    "floating",
			Source:   state.PaneCommandSourceMouse,
		})
	case render.ActionFloatingRaise:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandFocusRaise, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingOverview:
		root.Shell = root.Shell.OpenFloatingOverview()
		return root.Advance(), nil
	case render.ActionFloatingSummon:
		items := state.FloatingOverviewItems(root)
		if msg.Row >= 0 {
			root.Shell = root.Shell.SetFloatingOverviewSelectedIndex(msg.Row, len(items))
			items = state.FloatingOverviewItems(root)
		}
		selected, ok := selectedFloatingOverviewItem(items)
		if !ok {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "floating.summon", Body: "no floating"})
			return root.Advance(), nil
		}
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandSummon, TargetID: selected.FloatingID, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingClose:
		// footer close 没有 pane id 时，FloatingCommand 会按 active floating 作为目标。
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingToggleAll:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandToggleAll, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingShowAll:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandShowAll, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingCollapseAll:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandCollapseAll, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingFit:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandFit, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingAutoFit:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandToggleAutoFit, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingPick:
		root.Shell = root.Shell.OpenTerminalPicker()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	case render.ActionFloatingTakeOwner:
		shell := root.Shell.EnsureDefaults()
		if shell.ActiveFloatingID == "" {
			root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.owner", Body: "no active floating"})
			return root.Advance(), nil
		}
		return requestFloatingResizeOwner(root, shell.ActiveFloatingID)
	case render.ActionFloatingResize:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandResize, TargetID: floatingTargetIDForContentAction(root, msg), DeltaW: 2, DeltaH: 1, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingCenter:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandCenter, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingCollapse:
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandToggleCollapse, TargetID: floatingTargetIDForContentAction(root, msg), Source: state.PaneCommandSourceMouse})
	case render.ActionExitedRestart:
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolRestartIfExitedRequestMsg{TerminalID: terminalIDForShellContentAction(root, msg)}
		}}}
	case render.ActionExitedReconnect:
		root.Shell = root.Shell.OpenTerminalPicker()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	case render.ActionEmptyManager:
		root.Shell = root.Shell.OpenTerminalPool()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	case render.ActionEmptyCreate:
		target := terminalPoolTargetForContentAction(root, msg)
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return ShellOpenPromptMsg{Prompt: createTerminalPromptForTarget(root, target)}
		}}}
	case render.ActionEmptyAttach:
		root.Shell = root.Shell.OpenTerminalPicker()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "content action", Body: "unknown " + msg.ActionID})
	return root.Advance(), nil
}

func requestPaneResizeOwner(root state.Root, paneID string) (state.Root, []Effect) {
	if paneID == "" {
		return root, nil
	}
	binding, ok := root.TerminalViews.PaneBinding(paneID)
	if !ok || binding.TerminalID == "" {
		root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.owner", Body: "no terminal view"})
		return root.Advance(), nil
	}
	return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
		return liveAttachMsgForResizeOwner(binding)
	}}}
}

func requestFloatingResizeOwner(root state.Root, floatingID string) (state.Root, []Effect) {
	binding, ok := root.TerminalViews.FloatingBinding(floatingID)
	if !ok || binding.TerminalID == "" {
		root.Shell = root.Shell.EnsureDefaults().AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.owner", Body: "no terminal view"})
		return root.Advance(), nil
	}
	return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
		return liveAttachMsgForResizeOwner(binding)
	}}}
}

func liveAttachMsgForResizeOwner(binding state.TerminalViewBinding) LiveAttachMsg {
	return LiveAttachMsg{Config: LiveConfig{
		TerminalID:   binding.TerminalID,
		Cols:         binding.DesiredCols,
		Rows:         binding.DesiredRows,
		ResizePolicy: state.TerminalResizeRoleOwner,
		SurfaceID:    binding.SurfaceID,
		ViewID:       binding.ViewID,
	}}
}

func resizeFooterPaneCommand(actionID render.ActionID) (state.PaneCommand, bool) {
	// footer resize token 与键盘 resize mode 使用同一方向和步长语义。
	command := state.PaneCommand{
		Action: state.PaneCommandResize,
		Delta:  2,
		Source: state.PaneCommandSourceMouse,
	}
	switch actionID {
	case render.ActionResizeLeft:
		command.ResizeDirection = state.PaneResizeLeft
	case render.ActionResizeRight:
		command.ResizeDirection = state.PaneResizeRight
	case render.ActionResizeUp:
		command.ResizeDirection = state.PaneResizeUp
	case render.ActionResizeDown:
		command.ResizeDirection = state.PaneResizeDown
	default:
		return state.PaneCommand{}, false
	}
	return command, true
}

func reducePromptSubmit(root state.Root) (state.Root, []Effect) {
	shell := root.Shell.EnsureDefaults()
	if shell.Overlay.Kind != state.OverlayPrompt || !shell.Overlay.Open {
		return root, nil
	}
	before := shell.Overlay.Prompt
	shell = shell.SubmitPrompt()
	after := shell.Overlay.Prompt
	root.Shell = shell
	if after.Submitted {
		body := after.LastResult
		if body == "" {
			body = "(empty)"
		}
		if after.Purpose == "terminal.create" {
			request, err := terminalCreateRequestFromPrompt(after)
			if err != nil {
				shell = root.Shell.EnsureDefaults()
				prompt := shell.Overlay.Prompt
				prompt.Submitted = false
				prompt.LastResult = err.Error()
				shell.Overlay.Prompt = prompt
				root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.create", Body: err.Error()})
				return root.Advance(), nil
			}
			root.Shell = root.Shell.CloseOverlay()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return request }}}
		}
		if after.Purpose == "clipboard.edit" {
			root.Clipboard = root.Clipboard.ReplaceEntryText(after.TargetID, after.LastResult)
			root.Shell = root.Shell.CloseOverlay()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
				return ClipboardStoragePersistRequestMsg{Reason: "edit"}
			}}}
		}
		if after.Purpose == "clipboard.new" {
			root.Clipboard = root.Clipboard.WithCopiedText(after.LastResult)
			root.Shell = root.Shell.CloseOverlay()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
				return ClipboardStoragePersistRequestMsg{Reason: "new"}
			}}}
		}
		root.Shell = root.Shell.CloseOverlay()
		if command, ok := promptWorkbenchCommand(after); ok {
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
				return ShellWorkbenchCommandMsg{Command: command}
			}}}
		}
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "prompt.submit", Body: body})
		return root.Advance(), nil
	}
	if after.LastResult != "" && after.LastResult != before.LastResult {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "prompt.confirm", Body: after.LastResult})
	}
	return root.Advance(), nil
}

func promptWorkbenchCommand(prompt state.PromptState) (state.WorkbenchCommand, bool) {
	name := prompt.LastResult
	if name == "" {
		name = prompt.Value
	}
	switch prompt.Purpose {
	case "tab.rename":
		return state.WorkbenchCommand{Action: state.WorkbenchCommandTabRename, TargetID: prompt.TargetID, Name: name, Source: state.PaneCommandSourcePalette}, true
	case "workspace.rename":
		return state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceRename, TargetID: prompt.TargetID, Name: name, Source: state.PaneCommandSourcePalette}, true
	case "pane.rename":
		return state.WorkbenchCommand{Action: state.WorkbenchCommandPaneRename, Target: state.PaneCommandTarget{WorkspaceID: prompt.TargetWorkspaceID, TabID: prompt.TargetTabID, PaneID: prompt.TargetID}, Name: name, Source: state.PaneCommandSourcePalette}, true
	default:
		return state.WorkbenchCommand{}, false
	}
}

func reduceClipboardHistoryPaste(root state.Root) (state.Root, []Effect) {
	items := state.ClipboardHistoryItems(root)
	if len(items) == 0 {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "clipboard history", Body: "no clipboard entry"})
		return root.Advance(), nil
	}
	selected := items[0]
	for _, item := range items {
		if item.Selected {
			selected = item
			break
		}
	}
	root.Shell = root.Shell.CloseOverlay()
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
		return CopyModePasteTextMsg{Text: selected.Text}
	}}}
}

func reduceClipboardHistoryEdit(root state.Root) (state.Root, []Effect) {
	items := state.ClipboardHistoryItems(root)
	if len(items) == 0 {
		return root, nil
	}
	selected := items[0]
	for _, item := range items {
		if item.Selected {
			selected = item
			break
		}
	}
	root.Shell = root.Shell.OpenPrompt(state.PromptState{
		Title:       "Edit Clipboard Entry",
		Context:     "Edit selected clipboard entry. Submit updates clipboard history storage.",
		Purpose:     "clipboard.edit",
		TargetID:    selected.ID,
		Value:       selected.Text,
		Placeholder: "clipboard text",
	})
	return root.Advance(), nil
}

func reduceClipboardHistoryNew(root state.Root) (state.Root, []Effect) {
	root.Shell = root.Shell.OpenPrompt(state.PromptState{
		Title:       "New Clipboard Entry",
		Context:     "Create a clipboard entry. Submit updates clipboard history storage.",
		Purpose:     "clipboard.new",
		Placeholder: "clipboard text",
	})
	return root.Advance(), nil
}

func reduceClipboardHistoryDelete(root state.Root) (state.Root, []Effect) {
	items := state.ClipboardHistoryItems(root)
	if len(items) == 0 {
		return root, nil
	}
	selected := items[0]
	for _, item := range items {
		if item.Selected {
			selected = item
			break
		}
	}
	root.Clipboard = root.Clipboard.DeleteEntry(selected.ID, selected.Text)
	root.Shell = root.Shell.SetClipboardHistorySelectedIndex(0, len(state.ClipboardHistoryItems(root)))
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
		return ClipboardStoragePersistRequestMsg{Reason: "delete"}
	}}}
}

func createTerminalPrompt(targetPaneID string) state.PromptState {
	shellCommand := strings.TrimSpace(os.Getenv("SHELL"))
	if shellCommand == "" {
		shellCommand = "/bin/sh"
	}
	workdir, err := os.Getwd()
	if err != nil {
		workdir = ""
	}
	return state.PromptState{
		Title:       "Create Terminal",
		Purpose:     "terminal.create",
		TargetID:    targetPaneID,
		Command:     []string{shellCommand},
		Workdir:     workdir,
		DefaultName: filepath.Base(shellCommand),
		Fields: []state.PromptFieldState{
			{Key: "name", Label: "name", Required: true},
			{Key: "command", Label: "command", Placeholder: shellCommand},
			{Key: "workdir", Label: "workdir", Value: workdir},
		},
	}
}

func createTerminalPromptForTarget(root state.Root, target terminalPoolTarget) state.PromptState {
	if target.FloatingID != "" {
		prompt := createTerminalPrompt(target.PaneID)
		prompt.Context = "floating"
		prompt.TargetID = target.FloatingID
		return prompt
	}
	if target.PaneID == "" {
		target = terminalPoolTargetForActive(root)
	}
	prompt := createTerminalPrompt(target.PaneID)
	prompt.Context = "pane"
	prompt.TargetID = target.PaneID
	return prompt
}

func terminalCreateRequestFromPrompt(prompt state.PromptState) (TerminalPoolCreateRequestMsg, error) {
	name := strings.TrimSpace(prompt.FieldValue("name"))
	if name == "" {
		return TerminalPoolCreateRequestMsg{}, fmt.Errorf("name is required")
	}
	command, err := parsePromptCommand(prompt.FieldValue("command"))
	if err != nil {
		return TerminalPoolCreateRequestMsg{}, err
	}
	if len(command) == 0 {
		command = append([]string(nil), prompt.Command...)
	}
	if len(command) == 0 {
		command = services.DefaultTerminalCommand()
	}
	workdir := strings.TrimSpace(prompt.FieldValue("workdir"))
	if workdir == "" {
		workdir = strings.TrimSpace(prompt.Workdir)
	}
	request := TerminalPoolCreateRequestMsg{Title: name, Command: command, CWD: workdir}
	if prompt.Context == "floating" {
		request.TargetFloatingID = prompt.TargetID
	} else {
		request.TargetPaneID = prompt.TargetID
	}
	return request, nil
}

func parsePromptCommand(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	// 这里只实现 tuiv2 表单需要的轻量 shell-like 拆分，避免为 prompt 引入完整 shell 解析器。
	var (
		args       []string
		current    []rune
		inSingle   bool
		inDouble   bool
		escaped    bool
		quotedPart bool
	)
	flush := func(force bool) {
		if len(current) == 0 && !quotedPart && !force {
			return
		}
		args = append(args, string(current))
		current = current[:0]
		quotedPart = false
	}
	for _, r := range value {
		switch {
		case escaped:
			current = append(current, r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			quotedPart = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			quotedPart = true
		case !inSingle && !inDouble && unicode.IsSpace(r):
			flush(false)
		default:
			current = append(current, r)
		}
	}
	if escaped || inSingle || inDouble {
		return nil, fmt.Errorf("invalid command syntax")
	}
	flush(false)
	return args, nil
}

func terminalPickerItemAt(items []state.TerminalPickerItem, row int) (state.TerminalPickerItem, bool) {
	if row >= 0 && row < len(items) {
		return items[row], true
	}
	for _, item := range items {
		if item.Selected {
			return item, true
		}
	}
	return state.TerminalPickerItem{}, false
}

func terminalPoolPageItemForAction(root state.Root, row int) (state.TerminalPoolPageItem, bool) {
	items := state.TerminalPoolPageItems(root)
	if row >= 0 {
		root.Shell = root.Shell.SetTerminalPoolSelectedIndex(row, len(items))
		items = state.TerminalPoolPageItems(root)
	}
	if row >= 0 && row < len(items) {
		return items[row], true
	}
	for _, item := range items {
		if item.Selected {
			return item, true
		}
	}
	return state.TerminalPoolPageItem{}, false
}

func reduceWorkbenchTreeOpen(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.open", Body: "no node"})
		return root.Advance(), nil
	}
	switch selected.Kind {
	case state.WorkbenchTreeKindPane:
		root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: selected.PaneID})
		root.Shell = root.Shell.CloseOverlay()
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.open", Body: selected.PaneID})
	case state.WorkbenchTreeKindTab:
		targetPaneID := selected.PaneID
		if targetPaneID == "" {
			targetPaneID = firstPaneIDForTab(root.Shell, selected.TabID)
		}
		if targetPaneID != "" {
			root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: targetPaneID})
			root.Shell = root.Shell.CloseOverlay()
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.open", Body: selected.TabID})
		} else {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.open", Body: "tab has no pane"})
		}
	case state.WorkbenchTreeKindWorkspace:
		root.Shell = root.Shell.CloseOverlay()
		if selected.WorkspaceID == "" || selected.WorkspaceID == root.Shell.EnsureDefaults().Workspace.ID {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.open", Body: selected.WorkspaceName})
			return root.Advance(), nil
		}
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{
			Action:   state.WorkbenchCommandWorkspaceSwitch,
			TargetID: selected.WorkspaceID,
			Source:   state.PaneCommandSourceMouse,
		})
	case state.WorkbenchTreeKindFloating:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "floating", Body: "not implemented"})
	default:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.open", Body: "unknown node"})
	}
	return root.Advance(), nil
}

func reduceWorkbenchTreeRename(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.rename", Body: "no node"})
		return root.Advance(), nil
	}
	switch selected.Kind {
	case state.WorkbenchTreeKindWorkspace:
		root.Shell = root.Shell.OpenPrompt(state.PromptState{Title: "Rename Workspace", Purpose: "workspace.rename", TargetWorkspaceID: selected.WorkspaceID, TargetID: selected.WorkspaceID, Value: selected.WorkspaceName, Placeholder: "workspace name"})
	case state.WorkbenchTreeKindTab:
		root.Shell = root.Shell.OpenPrompt(state.PromptState{Title: "Rename Tab", Purpose: "tab.rename", TargetWorkspaceID: selected.WorkspaceID, TargetTabID: selected.TabID, TargetID: selected.TabID, Value: selected.TabTitle, Placeholder: "tab name"})
	case state.WorkbenchTreeKindPane:
		root.Shell = root.Shell.OpenPrompt(state.PromptState{Title: "Rename Pane", Purpose: "pane.rename", TargetWorkspaceID: selected.WorkspaceID, TargetTabID: selected.TabID, TargetID: selected.PaneID, Value: selected.PaneTitle, Placeholder: "pane name"})
	default:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.rename", Body: "unsupported node"})
	}
	return root.Advance(), nil
}

func reduceWorkbenchTreeNew(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceCreate, Name: nextWorkspaceName(root.Shell), Source: state.PaneCommandSourceMouse})
	}
	switch selected.Kind {
	case state.WorkbenchTreeKindWorkspace:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: nextTabName(root.Shell), Source: state.PaneCommandSourceMouse})
	case state.WorkbenchTreeKindTab, state.WorkbenchTreeKindPane:
		target := state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: selected.PaneID}
		if target.PaneID == "" {
			target.PaneID = firstPaneIDForTab(root.Shell, selected.TabID)
		}
		command := state.PaneCommand{
			Action:         state.PaneCommandSplit,
			Target:         target,
			SplitDirection: state.SplitDirectionVertical,
			NewPane:        state.PaneState{ID: nextKeyboardPaneID(root.Shell), Title: "pane", Kind: state.PaneEmpty},
			Source:         state.PaneCommandSourceMouse,
		}
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandPaneSplit, Pane: command, Source: state.PaneCommandSourceMouse})
	default:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.new", Body: "unsupported node"})
		return root.Advance(), nil
	}
}

func reduceWorkbenchTreeDelete(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.delete", Body: "no node"})
		return root.Advance(), nil
	}
	switch selected.Kind {
	case state.WorkbenchTreeKindWorkspace:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceDelete, TargetID: selected.WorkspaceID, Confirm: state.PaneConfirmAccepted, Source: state.PaneCommandSourceMouse})
	case state.WorkbenchTreeKindTab:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabClose, TargetID: selected.TabID, Source: state.PaneCommandSourceMouse})
	case state.WorkbenchTreeKindPane:
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandPaneClose, Target: state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: selected.PaneID}, Source: state.PaneCommandSourceMouse})
	default:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.delete", Body: "unsupported node"})
		return root.Advance(), nil
	}
}

func reduceWorkbenchTreeDetach(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.detach", Body: "no node"})
		return root.Advance(), nil
	}
	if selected.Kind != state.WorkbenchTreeKindPane {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.detach", Body: "select a pane"})
		return root.Advance(), nil
	}
	return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandPaneDetach, Target: state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: selected.PaneID}, Source: state.PaneCommandSourceKeyboard})
}

func reduceWorkbenchTreeZoom(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.zoom", Body: "no node"})
		return root.Advance(), nil
	}
	if selected.Kind != state.WorkbenchTreeKindPane {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.zoom", Body: "select a pane"})
		return root.Advance(), nil
	}
	return reducePaneCommand(root, state.PaneCommand{Action: state.PaneCommandToggleZoom, Target: state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: selected.PaneID}, Source: state.PaneCommandSourceKeyboard})
}

func workbenchTreeSelectedItem(items []state.WorkbenchTreeItem) (state.WorkbenchTreeItem, bool) {
	if len(items) == 0 {
		return state.WorkbenchTreeItem{}, false
	}
	for _, item := range items {
		if item.Selected {
			return item, true
		}
	}
	return items[0], true
}

func firstPaneIDForTab(shell state.ShellStore, tabID string) string {
	shell = shell.EnsureDefaults()
	for _, tab := range shell.Workspace.Tabs {
		if tab.ID == tabID && len(tab.Panes) > 0 {
			return tab.Panes[0].ID
		}
	}
	return ""
}

func terminalIDForContentAction(root state.Root, paneID string) string {
	if binding, ok := root.TerminalViews.PaneBinding(paneID); ok && binding.TerminalID != "" {
		return binding.TerminalID
	}
	return ""
}

func terminalIDForShellContentAction(root state.Root, msg ShellContentActionMsg) string {
	if msg.Floating {
		floatingID := floatingTargetIDForContentAction(root, msg)
		if binding, ok := root.TerminalViews.FloatingBinding(floatingID); ok && binding.TerminalID != "" {
			return binding.TerminalID
		}
		return ""
	}
	return terminalIDForContentAction(root, msg.PaneID)
}

func terminalPoolTargetForOverlay(root state.Root) terminalPoolTarget {
	shell := root.Shell.EnsureDefaults()
	if targetID := shell.Overlay.TargetID; targetID != "" {
		if target, ok := terminalPoolTargetForID(shell, targetID); ok {
			return target
		}
	}
	return terminalPoolTargetForActive(root)
}

func terminalPoolTargetForContentAction(root state.Root, msg ShellContentActionMsg) terminalPoolTarget {
	if msg.Floating {
		if floatingID := floatingTargetIDForContentAction(root, msg); floatingID != "" {
			if target, ok := terminalPoolTargetForID(root.Shell.EnsureDefaults(), floatingID); ok {
				return target
			}
		}
	}
	if msg.PaneID != "" {
		return terminalPoolTarget{PaneID: msg.PaneID, ViewID: state.TerminalPaneViewID(msg.PaneID)}
	}
	return terminalPoolTargetForActive(root)
}

func terminalPoolTargetForActive(root state.Root) terminalPoolTarget {
	shell := root.Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "" {
		if target, ok := terminalPoolTargetForID(shell, shell.ActiveFloatingID); ok {
			return target
		}
	}
	return terminalPoolTarget{PaneID: shell.ActivePaneID, ViewID: state.TerminalPaneViewID(shell.ActivePaneID)}
}

func terminalPoolTargetForID(shell state.ShellStore, id string) (terminalPoolTarget, bool) {
	shell = shell.EnsureDefaults()
	for _, floating := range shell.Floatings {
		if floating.ID == id || floating.Pane.ID == id {
			return terminalPoolTarget{PaneID: floating.Pane.ID, FloatingID: floating.ID, ViewID: state.TerminalFloatingViewID(floating.ID)}, true
		}
	}
	if _, ok := shell.Pane(state.PaneCommandTarget{PaneID: id}); ok {
		return terminalPoolTarget{PaneID: id, ViewID: state.TerminalPaneViewID(id)}, true
	}
	return terminalPoolTarget{}, false
}

func floatingTargetIDForContentAction(root state.Root, msg ShellContentActionMsg) string {
	id, _ := floatingIDForContentAction(root, msg)
	return id
}

func floatingIDForContentAction(root state.Root, msg ShellContentActionMsg) (string, bool) {
	shell := root.Shell.EnsureDefaults()
	if msg.PaneID == "" {
		return shell.ActiveFloatingID, shell.ActiveFloatingID != ""
	}
	if msg.Floating {
		if floatingID, ok := shell.FloatingIDForPaneID(msg.PaneID); ok {
			return floatingID, true
		}
	}
	for _, floating := range shell.Floatings {
		if floating.ID == msg.PaneID {
			return floating.ID, true
		}
	}
	return "", false
}

func reducePaneCommand(root state.Root, command state.PaneCommand) (state.Root, []Effect) {
	command = command.WithDefaults(root.Shell)
	targetPane, hasTargetPane := root.Shell.Pane(command.Target)
	command = inheritSplitTerminalPane(root, command, targetPane, hasTargetPane)
	nextShell, result := root.Shell.ApplyPaneCommand(command)
	if result.Status == state.PaneCommandOK {
		root.Shell = deactivateFloatingAfterPaneCommand(nextShell, command)
		root = updateTerminalViewsAfterPaneCommand(root, command, targetPane, hasTargetPane)
		root.Shell = addPaneCommandToast(root.Shell, command, result)
		effects := paneCommandEffects(command, result, targetPane, hasTargetPane)
		return root.Advance(), effects
	}
	root.Shell = addPaneCommandToast(root.Shell, command, result)
	return root.Advance(), []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
}

func inheritSplitTerminalPane(root state.Root, command state.PaneCommand, targetPane state.PaneState, hasTargetPane bool) state.PaneCommand {
	if command.Action != state.PaneCommandSplit || command.NewPane.TerminalID != "" {
		return command
	}
	if command.NewPane.Kind == state.PaneEmpty {
		return command
	}
	terminalID := ""
	if hasTargetPane {
		terminalID = targetPane.TerminalID
		if binding, ok := root.TerminalViews.PaneBinding(targetPane.ID); ok && binding.TerminalID != "" {
			terminalID = binding.TerminalID
		}
	}
	if terminalID == "" {
		return command
	}
	command.NewPane.TerminalID = terminalID
	command.NewPane.Kind = state.PaneTerminalLive
	if command.NewPane.Title == "" || command.NewPane.Title == command.NewPane.ID || command.NewPane.Title == "pane" {
		command.NewPane.Title = terminalID
	}
	return command
}

func reduceFloatingCommand(root state.Root, command state.FloatingCommand) (state.Root, []Effect) {
	command = withFloatingCommandDefaults(root, command)
	nextShell, result := root.Shell.ApplyFloatingCommand(command)
	root.Shell = addFloatingCommandToast(nextShell, command, result)
	effects := []Effect{}
	if result.Status == state.FloatingCommandOK && command.Action == state.FloatingCommandClose {
		root.TerminalViews = root.TerminalViews.DetachFloating(result.ID)
		root = invalidateCopyModeForClosedFloating(root, result.ID)
	}
	if result.Status == state.FloatingCommandOK && command.Action == state.FloatingCommandDeactivate {
		root = invalidateCopyModeForInactiveView(root)
	}
	if result.Status == state.FloatingCommandOK && shouldPersistFloatingCommand(command) {
		effects = append(effects, FuncEffect{Run: func(context.Context) Msg {
			return WorkbenchStoragePersistRequestMsg{Reason: string(result.Action)}
		}})
	}
	return root.Advance(), effects
}

func reduceWorkbenchCommand(root state.Root, command state.WorkbenchCommand) (state.Root, []Effect) {
	previousShell := root.Shell
	nextShell, result := root.Shell.ApplyWorkbenchCommand(command)
	root.Shell = addWorkbenchCommandToast(nextShell, result)
	if result.Status != state.WorkbenchCommandOK {
		return root.Advance(), nil
	}
	root = updateTerminalViewsAfterWorkbenchCommand(root, previousShell, command, result)
	if workbenchCommandChangesActiveView(command.Action) {
		root = invalidateCopyModeForInactiveView(root)
	}
	effects := []Effect{FuncEffect{Run: func(context.Context) Msg {
		return WorkbenchStoragePersistRequestMsg{Reason: string(result.Action)}
	}}}
	for _, terminalID := range result.Killed {
		id := terminalID
		effects = append(effects, FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolKillRequestMsg{TerminalID: id}
		}})
	}
	return root.Advance(), effects
}

func updateTerminalViewsAfterPaneCommand(root state.Root, command state.PaneCommand, targetPane state.PaneState, hasTargetPane bool) state.Root {
	switch command.Action {
	case state.PaneCommandSplit:
		if command.NewPane.TerminalID == "" && command.NewPane.Kind == state.PaneEmpty {
			return root
		}
		terminalID := command.NewPane.TerminalID
		var targetBinding state.TerminalViewBinding
		hasTargetBinding := false
		if hasTargetPane {
			targetBinding, hasTargetBinding = root.TerminalViews.PaneBinding(targetPane.ID)
			if terminalID == "" && hasTargetBinding {
				terminalID = targetBinding.TerminalID
			}
		}
		if terminalID == "" && hasTargetPane {
			terminalID = targetPane.TerminalID
		}
		if terminalID == "" {
			return root
		}
		binding := state.NewPaneTerminalView(command.NewPane.ID, terminalID, 0, 0, 0, state.TerminalResizeRoleOwner, "", state.TerminalPaneViewID(command.NewPane.ID), true)
		if hasTargetBinding && targetBinding.TerminalID == terminalID {
			binding.SurfaceID = targetBinding.SurfaceID
			binding.DesiredCols = targetBinding.DesiredCols
			binding.DesiredRows = targetBinding.DesiredRows
		}
		root.TerminalViews = root.TerminalViews.BindPane(binding).TransferPaneResizeOwner(command.NewPane.ID)
	case state.PaneCommandClose:
		root.TerminalViews = root.TerminalViews.DetachPane(command.Target.PaneID)
		root = invalidateCopyModeForClosedPane(root, command.Target.PaneID)
	case state.PaneCommandCloseAndKill:
		if hasTargetPane && targetPane.TerminalID != "" {
			root.TerminalViews = root.TerminalViews.RemoveTerminal(targetPane.TerminalID)
		} else {
			root.TerminalViews = root.TerminalViews.DetachPane(command.Target.PaneID)
		}
		root = invalidateCopyModeForClosedPane(root, command.Target.PaneID)
	}
	return root
}

func updateTerminalViewsAfterWorkbenchCommand(root state.Root, previousShell state.ShellStore, command state.WorkbenchCommand, result state.WorkbenchCommandResult) state.Root {
	switch result.Action {
	case state.WorkbenchCommandPaneSplit:
		root = bindWorkbenchSplitTerminalView(root, previousShell, command, result)
	case state.WorkbenchCommandPaneDetach:
		root.TerminalViews = root.TerminalViews.DetachPane(result.ID)
		root = invalidateCopyModeForClosedPane(root, result.ID)
	case state.WorkbenchCommandPaneClose:
		root.TerminalViews = root.TerminalViews.DetachPane(result.ID)
		root = invalidateCopyModeForClosedPane(root, result.ID)
	case state.WorkbenchCommandPaneKill, state.WorkbenchCommandTabKill:
		for _, terminalID := range result.Killed {
			root.TerminalViews = root.TerminalViews.RemoveTerminal(terminalID)
		}
	case state.WorkbenchCommandTabClose:
		for _, pane := range panesForWorkbenchTarget(previousShell, command.TargetID) {
			root.TerminalViews = root.TerminalViews.DetachPane(pane.ID)
			root = invalidateCopyModeForClosedPane(root, pane.ID)
		}
	}
	return root
}

func invalidateCopyModeForClosedPane(root state.Root, paneID string) state.Root {
	if paneID == "" || !copyModeInputContext(root.CopyMode) || root.CopyMode.PaneID != paneID {
		return root
	}
	root.History = root.History.InvalidateWindow()
	root.CopyMode = state.CopyModeStore{}
	return root
}

func invalidateCopyModeForClosedFloating(root state.Root, floatingID string) state.Root {
	if floatingID == "" || !copyModeInputContext(root.CopyMode) || root.CopyMode.ViewID != state.TerminalFloatingViewID(floatingID) {
		return root
	}
	root.History = root.History.InvalidateWindow()
	root.CopyMode = state.CopyModeStore{}
	return root
}

func invalidateCopyModeForInactiveView(root state.Root) state.Root {
	if !copyModeInputContext(root.CopyMode) {
		return root
	}
	shell := root.Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "" {
		if root.CopyMode.ViewID == state.TerminalFloatingViewID(shell.ActiveFloatingID) {
			return root
		}
		root.History = root.History.InvalidateWindow()
		root.CopyMode = state.CopyModeStore{}
		return root
	}
	if root.CopyMode.ViewID == state.TerminalPaneViewID(shell.ActivePaneID) {
		return root
	}
	root.History = root.History.InvalidateWindow()
	root.CopyMode = state.CopyModeStore{}
	return root
}

func workbenchCommandChangesActiveView(action state.WorkbenchCommandAction) bool {
	switch action {
	case state.WorkbenchCommandTabSwitch,
		state.WorkbenchCommandTabNext,
		state.WorkbenchCommandTabPrevious,
		state.WorkbenchCommandTabClose,
		state.WorkbenchCommandWorkspaceSwitch,
		state.WorkbenchCommandWorkspaceNext,
		state.WorkbenchCommandWorkspacePrevious,
		state.WorkbenchCommandWorkspaceDelete:
		return true
	default:
		return false
	}
}

func bindWorkbenchSplitTerminalView(root state.Root, previousShell state.ShellStore, command state.WorkbenchCommand, result state.WorkbenchCommandResult) state.Root {
	if result.ID == "" {
		return root
	}
	if command.Pane.NewPane.TerminalID == "" && command.Pane.NewPane.Kind == state.PaneEmpty {
		return root
	}
	target := command.Pane.Target
	if target.PaneID == "" {
		previous := previousShell.EnsureDefaults()
		target = state.PaneCommandTarget{WorkspaceID: previous.Workspace.ID, TabID: previous.Workspace.ActiveTabID, PaneID: previous.ActivePaneID}
	}
	targetPane, hasTargetPane := previousShell.Pane(target)
	terminalID := command.Pane.NewPane.TerminalID
	var targetBinding state.TerminalViewBinding
	hasTargetBinding := false
	if hasTargetPane {
		targetBinding, hasTargetBinding = root.TerminalViews.PaneBinding(targetPane.ID)
		if terminalID == "" && hasTargetBinding {
			terminalID = targetBinding.TerminalID
		}
		if terminalID == "" {
			terminalID = targetPane.TerminalID
		}
	}
	if terminalID == "" {
		return root
	}
	binding := state.NewPaneTerminalView(result.ID, terminalID, 0, 0, 0, state.TerminalResizeRoleOwner, "", state.TerminalPaneViewID(result.ID), true)
	if hasTargetBinding && targetBinding.TerminalID == terminalID {
		binding.SurfaceID = targetBinding.SurfaceID
		binding.DesiredCols = targetBinding.DesiredCols
		binding.DesiredRows = targetBinding.DesiredRows
	}
	root.TerminalViews = root.TerminalViews.BindPane(binding).TransferPaneResizeOwner(result.ID)
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: result.ID}, terminalID)
	return root
}

func panesForWorkbenchTarget(shell state.ShellStore, tabID string) []state.PaneState {
	shell = shell.EnsureDefaults()
	if tabID == "" {
		tabID = shell.Workspace.ActiveTabID
	}
	for _, tab := range shell.Workspace.Tabs {
		if tab.ID == tabID {
			return tab.Panes
		}
	}
	return nil
}

func withFloatingCommandDefaults(root state.Root, command state.FloatingCommand) state.FloatingCommand {
	if command.BoundsW <= 0 {
		command.BoundsW = root.Viewport.Cols
	}
	if command.BoundsH <= 0 {
		command.BoundsH = root.Viewport.Rows
	}
	if command.BoundsW <= 0 {
		command.BoundsW = 80
	}
	if command.BoundsH <= 0 {
		command.BoundsH = 24
	}
	if command.Action == state.FloatingCommandCreate && command.Pane.ID == "" {
		command.Pane = state.PaneState{ID: "floating-pane", Title: "floating", Kind: state.PaneEmpty}
	}
	if command.Action == state.FloatingCommandFit || command.Action == state.FloatingCommandToggleAutoFit || command.Action == state.FloatingCommandRefreshAutoFit {
		command.FitCols, command.FitRows = floatingCommandFitSize(root, command)
	}
	return command
}

func addFloatingCommandToast(shell state.ShellStore, command state.FloatingCommand, result state.FloatingCommandResult) state.ShellStore {
	if result.Status == state.FloatingCommandOK {
		if shouldSuppressFloatingCommandSuccessToast(command) {
			return shell
		}
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(result.Action), Body: result.ID})
	}
	return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(result.Action), Body: result.Reason})
}

func shouldSuppressFloatingCommandSuccessToast(command state.FloatingCommand) bool {
	switch command.Action {
	case state.FloatingCommandFocusRaise, state.FloatingCommandDeactivate, state.FloatingCommandMove, state.FloatingCommandResize, state.FloatingCommandRefreshAutoFit:
		return true
	default:
		return false
	}
}

func shouldPersistFloatingCommand(command state.FloatingCommand) bool {
	switch command.Action {
	case state.FloatingCommandCreate,
		state.FloatingCommandClose,
		state.FloatingCommandCenter,
		state.FloatingCommandToggleCollapse,
		state.FloatingCommandSummon,
		state.FloatingCommandMove,
		state.FloatingCommandResize,
		state.FloatingCommandToggleAll,
		state.FloatingCommandShowAll,
		state.FloatingCommandCollapseAll,
		state.FloatingCommandFit,
		state.FloatingCommandToggleAutoFit:
		return true
	default:
		return false
	}
}

func floatingCommandFitSize(root state.Root, command state.FloatingCommand) (int, int) {
	binding, ok := floatingCommandBinding(root, command)
	if !ok || binding.TerminalID == "" {
		return 0, 0
	}
	surface := root.Surface.SurfaceForTerminal(binding.TerminalID)
	if surface.Cols > 0 && surface.Rows > 0 {
		return surface.Cols, surface.Rows
	}
	if binding.DesiredCols > 0 && binding.DesiredRows > 0 {
		return binding.DesiredCols, binding.DesiredRows
	}
	if root.Session.TerminalID == binding.TerminalID {
		return root.Session.DesiredSize()
	}
	return 0, 0
}

func floatingCommandBinding(root state.Root, command state.FloatingCommand) (state.TerminalViewBinding, bool) {
	if command.TargetID != "" {
		if binding, ok := root.TerminalViews.FloatingBinding(command.TargetID); ok {
			return binding, true
		}
	}
	activeFloatingID := root.Shell.EnsureDefaults().ActiveFloatingID
	if activeFloatingID == "" {
		return state.TerminalViewBinding{}, false
	}
	return root.TerminalViews.FloatingBinding(activeFloatingID)
}

func deactivateFloatingAfterPaneCommand(shell state.ShellStore, command state.PaneCommand) state.ShellStore {
	switch command.Action {
	case state.PaneCommandFocus, state.PaneCommandSplit, state.PaneCommandClose, state.PaneCommandZoom, state.PaneCommandUnzoom, state.PaneCommandToggleZoom:
		next, _ := shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandDeactivate, Source: command.Source})
		return next
	default:
		return shell
	}
}

func addWorkbenchCommandToast(shell state.ShellStore, result state.WorkbenchCommandResult) state.ShellStore {
	if result.Status == state.WorkbenchCommandOK {
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(result.Action), Body: result.ID})
	}
	return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(result.Action), Body: result.Reason})
}

func paneCommandEffects(command state.PaneCommand, result state.PaneCommandResult, targetPane state.PaneState, hasTargetPane bool) []Effect {
	effects := []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
	if command.Action != state.PaneCommandCloseAndKill {
		return effects
	}
	if hasTargetPane && targetPane.TerminalID != "" {
		id := targetPane.TerminalID
		effects = append(effects, FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolKillRequestMsg{TerminalID: id}
		}})
	}
	return effects
}

func addPaneCommandToast(shell state.ShellStore, command state.PaneCommand, result state.PaneCommandResult) state.ShellStore {
	switch result.Status {
	case state.PaneCommandOK:
		if shouldSuppressPaneCommandSuccessToast(command) {
			return shell
		}
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(command.Action)})
	case state.PaneCommandNeedsConfirmation:
		return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(command.Action), Body: result.Reason, Pending: true})
	case state.PaneCommandInvalid:
		return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(command.Action), Body: result.Reason})
	default:
		return shell
	}
}

func shouldSuppressPaneCommandSuccessToast(command state.PaneCommand) bool {
	switch command.Action {
	case state.PaneCommandFocus, state.PaneCommandFocusNext, state.PaneCommandFocusPrevious, state.PaneCommandResize:
		return true
	default:
		return false
	}
}
