package app

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// NewTerminalInputRouterReducer 是普通 terminal 输入的唯一入口。
// UI、overlay、copy mode 已消费的输入不会到这里；未消费的 key/mouse passthrough
// 必须在这里统一解析 active TerminalView binding，避免 key 和 mouse 走两套目标选择逻辑。
func NewTerminalInputRouterReducer(deps LiveDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		inputMsg, ok := msg.(InputMsg)
		if !ok {
			return root, nil
		}
		return reduceTerminalInputRoute(root, inputMsg, deps)
	}
}

func reduceTerminalInputRoute(root state.Root, msg InputMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		logTerminalInputRoute(deps, root, terminalInputRouteLog{
			Event:  msg.Event,
			Result: "blocked",
			Reason: "terminal service missing",
		})
		return setLiveError(root, "terminal service missing"), nil
	}
	if copyModeOwnsActiveInput(root) {
		logTerminalInputRoute(deps, root, terminalInputRouteLog{
			Event:  msg.Event,
			Result: "consumed",
			Reason: "copy mode active",
		})
		return root, []Effect{handledEffect{}}
	}
	shell := root.Shell.ReadonlyDefaults()
	if shell.Overlay.Open {
		logTerminalInputRoute(deps, root, terminalInputRouteLog{
			Event:  msg.Event,
			Result: "consumed",
			Reason: "overlay open",
		})
		return root, []Effect{handledEffect{}}
	}
	if shell.InteractionMode != state.InteractionModeNormal {
		logTerminalInputRoute(deps, root, terminalInputRouteLog{
			Event:  msg.Event,
			Result: "consumed",
			Reason: "interaction mode active",
		})
		return root, []Effect{handledEffect{}}
	}
	target, ok := liveInputTarget(root)
	if !ok {
		logTerminalInputRoute(deps, root, terminalInputRouteLog{
			Event:  msg.Event,
			Result: "blocked",
			Reason: "no active terminal view binding",
		})
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.input", Body: "no terminal bound"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	intent := input.RouteWithOptions(msg.Event, input.RouteOptions{
		CopyModeActive:           false,
		TerminalMousePassthrough: liveMousePassthroughEnabled(root, msg.Event, target),
	})
	if intent.Kind != input.IntentTerminalInput || len(intent.Bytes) == 0 {
		logTerminalInputRoute(deps, root, terminalInputRouteLog{
			Event:  msg.Event,
			Target: target,
			Result: "ignored",
			Reason: string(intent.Kind),
		})
		return root, nil
	}
	logTerminalInputRoute(deps, root, terminalInputRouteLog{
		Event:       msg.Event,
		Target:      target,
		Result:      "terminal",
		Bytes:       len(intent.Bytes),
		RawMouse:    intent.RawMouse,
		NeedsAttach: target.Channel == 0,
	})
	if target.AttachPending {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "terminal.input", Body: "attach pending", Pending: true})
		return root.Advance(), []Effect{handledEffect{}}
	}
	if target.Channel == 0 {
		return root, []Effect{liveAttachForInputEffect(root, target, msg.Event, intent.Bytes, deps)}
	}
	return root, []Effect{terminalSendInputEffect(target, msg.Event, intent.Bytes, true, deps)}
}

func terminalSendInputEffect(target liveInputTargetInfo, event input.InputEvent, bytes []byte, retryOnError bool, deps LiveDeps) Effect {
	payload := append([]byte(nil), bytes...)
	return FuncEffect{
		Run: func(ctx context.Context) Msg {
			err := deps.Terminal.SendInput(ctx, services.TerminalInputRequest{
				TerminalID: target.TerminalID,
				Channel:    target.Channel,
				SurfaceID:  target.SurfaceID,
				ViewID:     target.ViewID,
				Event:      event,
				Bytes:      payload,
			})
			if err != nil {
				logTerminalInputSend(deps, target, event, len(payload), err)
			} else {
				logTerminalInputSendOK(deps, target, event, len(payload))
			}
			return LiveInputResultMsg{
				TerminalID:   target.TerminalID,
				ViewID:       target.ViewID,
				Channel:      target.Channel,
				Event:        event,
				Bytes:        payload,
				RetryOnError: retryOnError,
				Err:          err,
			}
		},
	}
}

type terminalInputRouteLog struct {
	Event       input.InputEvent
	Target      liveInputTargetInfo
	Result      string
	Reason      string
	Bytes       int
	RawMouse    bool
	NeedsAttach bool
}

func logTerminalInputRoute(deps LiveDeps, root state.Root, entry terminalInputRouteLog) {
	if deps.Logger == nil || !terminalInputTraceEnabled() {
		return
	}
	shell := root.Shell.ReadonlyDefaults()
	deps.Logger.Info("tui-v3 input route",
		"event", terminalInputEventSummary(entry.Event),
		"result", entry.Result,
		"reason", entry.Reason,
		"bytes", entry.Bytes,
		"raw_mouse", entry.RawMouse,
		"needs_attach", entry.NeedsAttach,
		"active_pane", shell.ActivePaneID,
		"active_floating", shell.ActiveFloatingID(),
		"interaction_mode", string(shell.InteractionMode),
		"overlay_open", shell.Overlay.Open,
		"overlay_kind", string(shell.Overlay.Kind),
		"copy_active", root.CopyMode.Active,
		"copy_entering", root.CopyMode.Entering,
		"target_view", entry.Target.ViewID,
		"target_pane", entry.Target.PaneID,
		"target_floating", entry.Target.FloatingID,
		"terminal_id", entry.Target.TerminalID,
		"channel", entry.Target.Channel,
		"surface_id", entry.Target.SurfaceID,
	)
}

func logTerminalInputSend(deps LiveDeps, target liveInputTargetInfo, event input.InputEvent, bytes int, err error) {
	if deps.Logger == nil {
		return
	}
	deps.Logger.Warn("tui-v3 terminal input send failed",
		"event", terminalInputEventSummary(event),
		"bytes", bytes,
		"target_view", target.ViewID,
		"target_pane", target.PaneID,
		"target_floating", target.FloatingID,
		"terminal_id", target.TerminalID,
		"channel", target.Channel,
		"surface_id", target.SurfaceID,
		"error", err,
	)
}

func logTerminalInputSendOK(deps LiveDeps, target liveInputTargetInfo, event input.InputEvent, bytes int) {
	if deps.Logger == nil || !terminalInputTraceEnabled() {
		return
	}
	deps.Logger.Info("tui-v3 terminal input sent",
		"event", terminalInputEventSummary(event),
		"bytes", bytes,
		"target_view", target.ViewID,
		"target_pane", target.PaneID,
		"target_floating", target.FloatingID,
		"terminal_id", target.TerminalID,
		"channel", target.Channel,
		"surface_id", target.SurfaceID,
	)
}

func terminalInputTraceEnabled() bool {
	return diagnosticsEnabledFromEnv(tuiInputTraceEnv) || diagnosticsEnabledFromEnv(tuiDiagnosticsEnv)
}

func (runtime *AppRuntime) logHostInputEvent(event input.InputEvent, routed Msg) {
	if runtime == nil || runtime.diagnostics == nil || runtime.diagnostics.logger == nil || !terminalInputTraceEnabled() {
		return
	}
	runtime.diagnostics.logger.Info("tui-v3 host input",
		"event", terminalInputEventSummary(event),
		"routed_msg", fmt.Sprintf("%T", routed),
	)
}

func terminalInputEventSummary(event input.InputEvent) string {
	switch event.Kind {
	case input.EventKindKey:
		if event.Key == input.KeyChar {
			return fmt.Sprintf("key:%s ctrl=%t alt=%t shift=%t raw=%q", event.Char, event.Ctrl, event.Alt, event.Shift, event.RawSeq)
		}
		return fmt.Sprintf("key:%s ctrl=%t alt=%t shift=%t raw=%q", event.Key, event.Ctrl, event.Alt, event.Shift, event.RawSeq)
	case input.EventKindMouse:
		return fmt.Sprintf("mouse:%s row=%d col=%d raw=%q", event.Mouse, event.Row, event.Col, event.RawSeq)
	default:
		return string(event.Kind)
	}
}
