package input

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestInputEventKind(t *testing.T) {
	event := InputEvent{Kind: EventKindKey}
	if event.Kind != EventKindKey {
		t.Fatalf("unexpected input event kind %q", event.Kind)
	}
}

func TestRoutePageUpEntersOrRequestsCopyMode(t *testing.T) {
	event := InputEvent{Kind: EventKindKey, Key: KeyPageUp}
	if intent := Route(event, false); intent.Kind != IntentEnterCopyMode {
		t.Fatalf("expected enter copy mode, got %#v", intent)
	}
	if intent := Route(event, true); intent.Kind != IntentRequestOlder {
		t.Fatalf("expected older request, got %#v", intent)
	}
	down := InputEvent{Kind: EventKindKey, Key: KeyPageDn}
	if intent := Route(down, true); intent.Kind != IntentRequestNewer {
		t.Fatalf("expected newer request, got %#v", intent)
	}
}

func TestRouteWheelUpEntersOrRequestsCopyMode(t *testing.T) {
	event := InputEvent{Kind: EventKindMouse, Mouse: MouseWheelUp}
	if intent := Route(event, false); intent.Kind != IntentEnterCopyMode {
		t.Fatalf("expected enter copy mode, got %#v", intent)
	}
	if intent := Route(event, true); intent.Kind != IntentRequestOlder {
		t.Fatalf("expected older request, got %#v", intent)
	}
	down := InputEvent{Kind: EventKindMouse, Mouse: MouseWheelDown}
	if intent := Route(down, true); intent.Kind != IntentRequestNewer {
		t.Fatalf("expected newer request, got %#v", intent)
	}
}

func TestRouteTerminalInputAndCopyModeSelection(t *testing.T) {
	key := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "x"}, false)
	if key.Kind != IntentTerminalInput || string(key.Bytes) != "x" {
		t.Fatalf("unexpected terminal input intent %#v", key)
	}
	enter := Route(InputEvent{Kind: EventKindKey, Key: KeyEnter}, false)
	if enter.Kind != IntentTerminalInput || string(enter.Bytes) != "\r" {
		t.Fatalf("unexpected enter intent %#v", enter)
	}
	esc := Route(InputEvent{Kind: EventKindKey, Key: KeyEsc}, false)
	if esc.Kind != IntentTerminalInput || string(esc.Bytes) != "\x1b" {
		t.Fatalf("unexpected esc intent %#v", esc)
	}

	mouse := Route(InputEvent{Kind: EventKindMouse, Mouse: MouseLeft, Row: 2, Col: 3}, true)
	if mouse.Kind != IntentMouseSelect || mouse.Event.Row != 2 || mouse.Event.Col != 3 {
		t.Fatalf("unexpected mouse selection intent %#v", mouse)
	}
}

func TestRouteHostThemeEventDoesNotBecomeTerminalInput(t *testing.T) {
	intent := Route(InputEvent{
		Kind:  EventKindHostTheme,
		Theme: HostThemeEvent{DefaultFG: "#aabbcc"},
	}, false)
	if intent.Kind != IntentNone {
		t.Fatalf("host theme event must not become terminal input, got %#v", intent)
	}
}

func TestRouteCtrlFAndCtrlVToUIIntents(t *testing.T) {
	ctrlF := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x06", Ctrl: true}, false)
	if ctrlF.Kind != IntentOpenTerminalPicker {
		t.Fatalf("expected terminal picker intent, got %#v", ctrlF)
	}
	ctrlV := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x16", Ctrl: true}, false)
	if ctrlV.Kind != IntentEnterCopyMode {
		t.Fatalf("expected display/copy intent, got %#v", ctrlV)
	}
	namedCtrlF := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "f", Ctrl: true}, false)
	if namedCtrlF.Kind != IntentOpenTerminalPicker {
		t.Fatalf("expected named ctrl-f terminal picker intent, got %#v", namedCtrlF)
	}
}

func TestRouteCopyModePasteShortcuts(t *testing.T) {
	history := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "H"}, true)
	if history.Kind != IntentOpenClipboardHistory {
		t.Fatalf("expected clipboard history intent, got %#v", history)
	}
	lastCopy := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "p"}, true)
	if lastCopy.Kind != IntentPasteLastCopy {
		t.Fatalf("expected paste last copy intent, got %#v", lastCopy)
	}
	systemClipboard := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "P"}, true)
	if systemClipboard.Kind != IntentPasteClipboard {
		t.Fatalf("expected paste clipboard intent, got %#v", systemClipboard)
	}
}

func TestRouteInteractionModePrefixesAndModeKeys(t *testing.T) {
	ctrlP := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x10", Ctrl: true}, false)
	if ctrlP.Kind != IntentSetInteractionMode || ctrlP.Mode != InteractionModePane {
		t.Fatalf("expected pane mode intent, got %#v", ctrlP)
	}
	ctrlT := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x14", Ctrl: true}, false)
	if ctrlT.Kind != IntentSetInteractionMode || ctrlT.Mode != InteractionModeTab {
		t.Fatalf("expected tab mode intent, got %#v", ctrlT)
	}
	ctrlW := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x17", Ctrl: true}, false)
	if ctrlW.Kind != IntentSetInteractionMode || ctrlW.Mode != InteractionModeWorkspace {
		t.Fatalf("expected workspace mode intent, got %#v", ctrlW)
	}
	paneFocus := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "n"}, false, InteractionModePane)
	if paneFocus.Kind != IntentPaneCommand || paneFocus.Command != "pane focus-next" {
		t.Fatalf("expected focus-next pane command, got %#v", paneFocus)
	}
	tabNew := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "c"}, false, InteractionModeTab)
	if tabNew.Kind != IntentWorkbenchCommand || tabNew.Command != "tab create" {
		t.Fatalf("expected tab create command, got %#v", tabNew)
	}
	tabNext := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "n"}, false, InteractionModeTab)
	if tabNext.Kind != IntentWorkbenchCommand || tabNext.Command != "tab next" {
		t.Fatalf("expected tab next command, got %#v", tabNext)
	}
	tabRename := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "r"}, false, InteractionModeTab)
	if tabRename.Kind != IntentWorkbenchCommand || tabRename.Command != "tab rename" {
		t.Fatalf("expected tab rename command, got %#v", tabRename)
	}
	tabJump := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "3"}, false, InteractionModeTab)
	if tabJump.Kind != IntentWorkbenchCommand || tabJump.Command != "tab jump 3" {
		t.Fatalf("expected tab jump command, got %#v", tabJump)
	}
	workspaceNext := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "l"}, false, InteractionModeWorkspace)
	if workspaceNext.Kind != IntentWorkbenchCommand || workspaceNext.Command != "workspace next" {
		t.Fatalf("expected workspace next command, got %#v", workspaceNext)
	}
	resizeRight := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyRight}, false, InteractionModeResize)
	if resizeRight.Kind != IntentPaneCommand || resizeRight.Command != "pane resize right delta=2" {
		t.Fatalf("expected resize-right pane command, got %#v", resizeRight)
	}
	globalFooter := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "f"}, false, InteractionModeGlobal)
	if globalFooter.Kind != IntentShellAction || globalFooter.Action != ShellActionToggleFooter {
		t.Fatalf("expected global footer action, got %#v", globalFooter)
	}
	globalPool := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "t"}, false, InteractionModeGlobal)
	if globalPool.Kind != IntentShellAction || globalPool.Action != ShellActionOpenPool {
		t.Fatalf("expected global terminal pool action, got %#v", globalPool)
	}
	globalPrompt := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: ":"}, false, InteractionModeGlobal)
	if globalPrompt.Kind != IntentShellAction || globalPrompt.Action != ShellActionOpenPrompt {
		t.Fatalf("expected global prompt action, got %#v", globalPrompt)
	}
	globalHelp := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "?"}, false, InteractionModeGlobal)
	if globalHelp.Kind != IntentShellAction || globalHelp.Action != ShellActionOpenHelp {
		t.Fatalf("expected global help action, got %#v", globalHelp)
	}
	esc := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyEsc}, false, InteractionModePane)
	if esc.Kind != IntentExitInteraction {
		t.Fatalf("expected interaction exit, got %#v", esc)
	}
}

func TestBindingCatalogIsUniqueAndContainsDocumentedAliases(t *testing.T) {
	seen := map[string]string{}
	for _, binding := range BindingCatalog() {
		key := string(binding.Mode) + "|" + string(binding.Key) + "|" + binding.Char + "|" + boolKey(binding.Ctrl) + boolKey(binding.Alt) + boolKey(binding.Shift)
		if previous, ok := seen[key]; ok {
			t.Fatalf("duplicate binding %s and %s for %s", previous, binding.ID, key)
		}
		seen[key] = binding.ID
	}
	cases := []struct {
		name    string
		event   InputEvent
		mode    InteractionMode
		kind    IntentKind
		command string
		action  ShellAction
	}{
		{name: "pane w close", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "w"}, kind: IntentWorkbenchCommand, command: "pane close"},
		{name: "pane d detach", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "d"}, kind: IntentWorkbenchCommand, command: "pane detach"},
		{name: "pane r reconnect", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "r"}, kind: IntentWorkbenchCommand, command: "pane reconnect"},
		{name: "pane R restart", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "R"}, kind: IntentWorkbenchCommand, command: "pane restart"},
		{name: "pane a owner", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "a"}, kind: IntentWorkbenchCommand, command: "pane take-owner"},
		{name: "pane s size lock", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "s"}, kind: IntentWorkbenchCommand, command: "terminal size lock"},
		{name: "pane percent split right", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "%"}, kind: IntentPaneCommand, command: "pane split-right"},
		{name: "pane quote split down", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\""}, kind: IntentPaneCommand, command: "pane split-down"},
		{name: "pane ctrl-d split right", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x04", Ctrl: true}, kind: IntentPaneCommand, command: "pane split-right"},
		{name: "pane ctrl-e split down", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x05", Ctrl: true}, kind: IntentPaneCommand, command: "pane split-down"},
		{name: "pane X kill", mode: InteractionModePane, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "X"}, kind: IntentWorkbenchCommand, command: "pane kill confirm=accepted"},
		{name: "resize a owner", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "a"}, kind: IntentWorkbenchCommand, command: "pane take-owner"},
		{name: "resize s size lock", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "s"}, kind: IntentWorkbenchCommand, command: "terminal size lock"},
		{name: "resize space layout", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: " "}, kind: IntentWorkbenchCommand, command: "terminal layout toggle"},
		{name: "resize shift left pan", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyLeft, Shift: true}, kind: IntentWorkbenchCommand, command: "terminal layout pan-left"},
		{name: "resize align right", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "$"}, kind: IntentWorkbenchCommand, command: "terminal layout align-right"},
		{name: "resize center", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "m"}, kind: IntentWorkbenchCommand, command: "terminal layout center"},
		{name: "resize reset", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "r"}, kind: IntentWorkbenchCommand, command: "terminal layout reset"},
		{name: "resize equals balance", mode: InteractionModeResize, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "="}, kind: IntentPaneCommand, command: "pane balance"},
		{name: "floating f pick", mode: InteractionModeFloating, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "f"}, kind: IntentShellAction, action: ShellActionOpenPicker},
		{name: "floating o overview", mode: InteractionModeFloating, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "o"}, kind: IntentShellAction, action: ShellActionFloatingOverview},
		{name: "floating 3 summon", mode: InteractionModeFloating, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "3"}, kind: IntentShellAction, action: ShellActionFloatingSummon},
		{name: "floating a owner", mode: InteractionModeFloating, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "a"}, kind: IntentWorkbenchCommand, command: "floating take-owner"},
		{name: "tab c create", mode: InteractionModeTab, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "c"}, kind: IntentWorkbenchCommand, command: "tab create"},
		{name: "tab n next", mode: InteractionModeTab, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "n"}, kind: IntentWorkbenchCommand, command: "tab next"},
		{name: "tab p previous", mode: InteractionModeTab, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "p"}, kind: IntentWorkbenchCommand, command: "tab previous"},
		{name: "tab X kill", mode: InteractionModeTab, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "X"}, kind: IntentWorkbenchCommand, command: "tab kill confirm=accepted"},
		{name: "workspace c create", mode: InteractionModeWorkspace, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "c"}, kind: IntentWorkbenchCommand, command: "workspace create"},
		{name: "workspace n next", mode: InteractionModeWorkspace, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "n"}, kind: IntentWorkbenchCommand, command: "workspace next"},
		{name: "workspace p previous", mode: InteractionModeWorkspace, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "p"}, kind: IntentWorkbenchCommand, command: "workspace previous"},
		{name: "workspace x delete", mode: InteractionModeWorkspace, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "x"}, kind: IntentWorkbenchCommand, command: "workspace delete confirm=accepted"},
		{name: "workspace f tree", mode: InteractionModeWorkspace, event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "f"}, kind: IntentShellAction, action: ShellActionOpenTree},
	}
	for _, tc := range cases {
		intent := RouteWithMode(tc.event, false, tc.mode)
		if intent.Kind != tc.kind || intent.Command != tc.command || intent.Action != tc.action {
			t.Fatalf("%s: unexpected intent %#v", tc.name, intent)
		}
	}
}

func TestPaneModeUsesTuiv2KeyboardSplitAliases(t *testing.T) {
	cases := []struct {
		name  string
		event InputEvent
		want  string
	}{
		{name: "percent", event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "%"}, want: "pane split-right"},
		{name: "ctrl-d", event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "d", Ctrl: true}, want: "pane split-right"},
		{name: "quote", event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\""}, want: "pane split-down"},
		{name: "ctrl-e", event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "e", Ctrl: true}, want: "pane split-down"},
	}
	for _, tc := range cases {
		intent := RouteWithMode(tc.event, false, InteractionModePane)
		if intent.Kind != IntentPaneCommand || intent.Command != tc.want {
			t.Fatalf("%s: unexpected split intent %#v", tc.name, intent)
		}
	}
	for _, char := range []string{"v"} {
		intent := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: char}, false, InteractionModePane)
		if intent.Kind != IntentNone {
			t.Fatalf("legacy split key %q should stay unbound, got %#v", char, intent)
		}
	}
}

func TestInputBindingCatalogHasSingleProductionOwner(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob input files: %v", err)
	}
	owners := []string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, name := range spec.Names {
				if name.Name == "bindingCatalog" {
					owners = append(owners, file)
				}
			}
			return true
		})
	}
	if len(owners) != 1 || owners[0] != "bindings.go" {
		t.Fatalf("input binding catalog must have a single production owner bindings.go, got %#v", owners)
	}
}

func TestNormalModePassesUnboundRawKeysAndCommonCtrlToTerminal(t *testing.T) {
	cases := []struct {
		name  string
		event InputEvent
		want  string
	}{
		{name: "ctrl-c", event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x03", Ctrl: true, RawSeq: "\x03"}, want: "\x03"},
		{name: "arrow", event: InputEvent{Kind: EventKindKey, Key: KeyLeft, RawSeq: "\x1b[D"}, want: "\x1b[D"},
		{name: "home", event: InputEvent{Kind: EventKindKey, Key: KeyHome, RawSeq: "\x1b[H"}, want: "\x1b[H"},
		{name: "delete", event: InputEvent{Kind: EventKindKey, Key: KeyDelete, RawSeq: "\x1b[3~"}, want: "\x1b[3~"},
		{name: "shift-tab", event: InputEvent{Kind: EventKindKey, Key: KeyShiftTab, Shift: true, RawSeq: "\x1b[Z"}, want: "\x1b[Z"},
		{name: "alt-x", event: InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "x", Alt: true, RawSeq: "\x1bx"}, want: "\x1bx"},
	}
	for _, tc := range cases {
		intent := Route(tc.event, false)
		if intent.Kind != IntentTerminalInput || string(intent.Bytes) != tc.want {
			t.Fatalf("%s: unexpected terminal passthrough %#v", tc.name, intent)
		}
	}
}

func TestUIModesSwallowUnboundKeysAndMouseRequiresTracking(t *testing.T) {
	key := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyDelete, RawSeq: "\x1b[3~"}, false, InteractionModePane)
	if key.Kind != IntentNone {
		t.Fatalf("ui mode should swallow unbound delete, got %#v", key)
	}
	mouse := InputEvent{Kind: EventKindMouse, Mouse: MouseRight, RawSeq: "\x1b[<2;10;4M"}
	if intent := RouteWithOptions(mouse, RouteOptions{}); intent.Kind != IntentNone {
		t.Fatalf("mouse without tracking should not passthrough, got %#v", intent)
	}
	intent := RouteWithOptions(mouse, RouteOptions{TerminalMousePassthrough: true})
	if intent.Kind != IntentTerminalInput || !intent.RawMouse || string(intent.Bytes) != mouse.RawSeq {
		t.Fatalf("mouse tracking should raw passthrough, got %#v", intent)
	}
	wheel := InputEvent{Kind: EventKindMouse, Mouse: MouseWheelUp, RawSeq: "\x1b[<64;10;5M"}
	intent = RouteWithOptions(wheel, RouteOptions{TerminalMousePassthrough: true})
	if intent.Kind != IntentTerminalInput || !intent.RawMouse || string(intent.Bytes) != wheel.RawSeq {
		t.Fatalf("tracked wheel should passthrough before entering copy mode, got %#v", intent)
	}
	intent = RouteWithOptions(wheel, RouteOptions{CopyModeActive: true, TerminalMousePassthrough: true})
	if intent.Kind != IntentRequestOlder {
		t.Fatalf("active copy mode should keep wheel for history, got %#v", intent)
	}
	wheelDown := InputEvent{Kind: EventKindMouse, Mouse: MouseWheelDown, RawSeq: "\x1b[<65;10;5M"}
	intent = RouteWithOptions(wheelDown, RouteOptions{CopyModeActive: true, TerminalMousePassthrough: true})
	if intent.Kind != IntentRequestNewer {
		t.Fatalf("active copy mode should keep wheel down for history, got %#v", intent)
	}
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
