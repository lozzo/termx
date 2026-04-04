package input

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func runeKey(r rune) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func specialKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func TestRouteKeyMsg_CtrlF_NormalMode_ProducesOpenPicker(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlF))
	if result.Action == nil || result.Action.Kind != ActionOpenPicker {
		t.Fatalf("expected ActionOpenPicker, got %#v", result.Action)
	}
	if result.TerminalInput != nil {
		t.Fatal("TerminalInput should be nil when semantic action fires")
	}
}

func TestRouteKeyMsg_NormalMode_AllModeEntryBindings(t *testing.T) {
	r := NewRouter()
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: ctrlKey(tea.KeyCtrlP), want: ActionEnterPaneMode},
		{msg: ctrlKey(tea.KeyCtrlR), want: ActionEnterResizeMode},
		{msg: ctrlKey(tea.KeyCtrlT), want: ActionEnterTabMode},
		{msg: ctrlKey(tea.KeyCtrlW), want: ActionEnterWorkspaceMode},
		{msg: ctrlKey(tea.KeyCtrlO), want: ActionEnterFloatingMode},
		{msg: ctrlKey(tea.KeyCtrlV), want: ActionEnterDisplayMode},
		{msg: ctrlKey(tea.KeyCtrlF), want: ActionOpenPicker},
		{msg: ctrlKey(tea.KeyCtrlG), want: ActionEnterGlobalMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("normal+%v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
		if result.TerminalInput != nil {
			t.Fatalf("normal+%v: expected no terminal input, got %#v", testCase.msg, result.TerminalInput)
		}
	}
}

func TestRouteKeyMsg_PaneMode_PlainKeysProduceFocusActions(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePane})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: runeKey('h'), want: ActionFocusPaneLeft},
		{msg: runeKey('j'), want: ActionFocusPaneDown},
		{msg: runeKey('k'), want: ActionFocusPaneUp},
		{msg: runeKey('l'), want: ActionFocusPaneRight},
		{msg: runeKey('%'), want: ActionSplitPane},
		{msg: runeKey('"'), want: ActionSplitPaneHorizontal},
		{msg: runeKey('z'), want: ActionZoomPane},
		{msg: runeKey('d'), want: ActionDetachPane},
		{msg: runeKey('r'), want: ActionReconnectPane},
		{msg: runeKey('a'), want: ActionBecomeOwner},
		{msg: runeKey('X'), want: ActionClosePaneKill},
		{msg: runeKey('w'), want: ActionClosePane},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("pane+%v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_StickyModes_CtrlFStillOpensPicker(t *testing.T) {
	modes := []ModeKind{
		ModePane,
		ModeResize,
		ModeTab,
		ModeWorkspace,
		ModeFloating,
		ModeDisplay,
		ModeGlobal,
	}

	for _, mode := range modes {
		r := NewRouter()
		r.SetMode(ModeState{Kind: mode})
		result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlF))
		if result.Action == nil || result.Action.Kind != ActionOpenPicker {
			t.Fatalf("mode %q: expected Ctrl-F to open picker, got %#v", mode, result.Action)
		}
		if result.TerminalInput != nil {
			t.Fatalf("mode %q: expected no terminal input, got %#v", mode, result.TerminalInput)
		}
	}
}

func TestRouteKeyMsg_ResizeMode_LargeStepBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeResize})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: runeKey('h'), want: ActionResizePaneLeft},
		{msg: runeKey('H'), want: ActionResizePaneLargeLeft},
		{msg: runeKey('j'), want: ActionResizePaneDown},
		{msg: runeKey('J'), want: ActionResizePaneLargeDown},
		{msg: runeKey('a'), want: ActionBecomeOwner},
		{msg: runeKey('='), want: ActionBalancePanes},
		{msg: specialKey(tea.KeySpace), want: ActionCycleLayout},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("resize+%v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_WorkspaceMode_UsesLegacyAlignedBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeWorkspace})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: runeKey('f'), want: ActionOpenWorkspacePicker},
		{msg: runeKey('c'), want: ActionCreateWorkspace},
		{msg: runeKey('r'), want: ActionRenameWorkspace},
		{msg: runeKey('x'), want: ActionDeleteWorkspace},
		{msg: runeKey('n'), want: ActionNextWorkspace},
		{msg: runeKey('p'), want: ActionPrevWorkspace},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("workspace+%v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_TabMode_UsesLegacyAlignedBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeTab})

	cases := []struct {
		msg      tea.KeyMsg
		want     ActionKind
		wantText string
	}{
		{msg: runeKey('c'), want: ActionCreateTab},
		{msg: runeKey('r'), want: ActionRenameTab},
		{msg: runeKey('n'), want: ActionNextTab},
		{msg: runeKey('p'), want: ActionPrevTab},
		{msg: runeKey('x'), want: ActionKillTab},
		{msg: runeKey('1'), want: ActionJumpTab, wantText: "1"},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}

	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("tab+%v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
		if testCase.wantText != "" && result.Action.Text != testCase.wantText {
			t.Fatalf("tab+%v: expected action text %q, got %#v", testCase.msg, testCase.wantText, result.Action)
		}
	}
}

func TestRouteKeyMsg_NormalMode_NonInterceptedCtrlKeysPassthrough(t *testing.T) {
	// Ctrl keys not bound to mode-entry shortcuts must pass through to the terminal.
	r := NewRouter()
	cases := []struct {
		key  tea.KeyType
		want byte
	}{
		{tea.KeyCtrlC, 0x03},
		{tea.KeyCtrlD, 0x04},
		{tea.KeyCtrlZ, 0x1a},
		{tea.KeyCtrlA, 0x01},
		{tea.KeyCtrlE, 0x05},
	}
	for _, tc := range cases {
		result := r.RouteKeyMsg(ctrlKey(tc.key))
		if result.Action != nil {
			t.Fatalf("key %v should pass through, got action %q", tc.key, result.Action.Kind)
		}
		if result.TerminalInput == nil || len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != tc.want {
			t.Fatalf("key %v: expected passthrough byte 0x%02x, got %#v", tc.key, tc.want, result.TerminalInput)
		}
	}
}

func TestRouteKeyMsg_CtrlC_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlC))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != 0x03 {
		t.Fatalf("expected Ctrl-C passthrough, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_Escape_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(specialKey(tea.KeyEsc))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != 0x1b {
		t.Fatalf("expected escape passthrough, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_Enter_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(specialKey(tea.KeyEnter))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != '\r' {
		t.Fatalf("expected enter passthrough carriage return, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_CtrlJ_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlJ))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != '\n' {
		t.Fatalf("expected Ctrl-J passthrough line feed, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_CtrlM_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlM))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || len(result.TerminalInput.Data) != 1 || result.TerminalInput.Data[0] != '\r' {
		t.Fatalf("expected Ctrl-M passthrough carriage return, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_ShiftTab_NormalMode_UsesEncodedFallback(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(specialKey(tea.KeyShiftTab))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || result.TerminalInput.Kind != TerminalInputEncodedKey {
		t.Fatalf("expected encoded-key terminal input, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_PrintableRune_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(runeKey('a'))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || string(result.TerminalInput.Data) != "a" {
		t.Fatalf("expected rune passthrough, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_QuestionMark_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(runeKey('?'))
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || string(result.TerminalInput.Data) != "?" {
		t.Fatalf("expected question mark passthrough, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_MultipleRunes_NormalMode_Passthrough(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'i'}})
	if result.Action != nil {
		t.Fatalf("expected no action, got %#v", result.Action)
	}
	if result.TerminalInput == nil || string(result.TerminalInput.Data) != "hi" {
		t.Fatalf("expected multi-rune passthrough, got %#v", result.TerminalInput)
	}
}

func TestRouteKeyMsg_PickerMode_UsesPickerBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePicker})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: specialKey(tea.KeyUp), want: ActionPickerUp},
		{msg: specialKey(tea.KeyDown), want: ActionPickerDown},
		{msg: specialKey(tea.KeyEnter), want: ActionSubmitPrompt},
		{msg: specialKey(tea.KeyTab), want: ActionPickerAttachSplit},
		{msg: specialKey(tea.KeyCtrlE), want: ActionEditTerminal},
		{msg: specialKey(tea.KeyCtrlK), want: ActionKillTerminal},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_PickerMode_LegacyAlignedBindingsRestored(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePicker})

	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: specialKey(tea.KeyTab), want: ActionPickerAttachSplit},
		{msg: specialKey(tea.KeyCtrlE), want: ActionEditTerminal},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_PickerMode_UnboundRuneIgnored(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePicker})
	result := r.RouteKeyMsg(runeKey('a'))
	if result.Action != nil || result.TerminalInput != nil {
		t.Fatalf("expected ignored key in picker mode, got %#v", result)
	}
}

func TestRouteKeyMsg_TerminalManagerMode_UsesTerminalManagerBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeTerminalManager})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: specialKey(tea.KeyUp), want: ActionPickerUp},
		{msg: specialKey(tea.KeyDown), want: ActionPickerDown},
		{msg: specialKey(tea.KeyEnter), want: ActionSubmitPrompt},
		{msg: specialKey(tea.KeyCtrlT), want: ActionAttachTab},
		{msg: specialKey(tea.KeyCtrlO), want: ActionAttachFloating},
		{msg: specialKey(tea.KeyCtrlE), want: ActionEditTerminal},
		{msg: specialKey(tea.KeyCtrlK), want: ActionKillTerminal},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_GlobalMode_UsesLegacyAlignedBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeGlobal})

	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: runeKey('?'), want: ActionOpenHelp},
		{msg: runeKey('t'), want: ActionOpenTerminalManager},
		{msg: runeKey('q'), want: ActionQuit},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_FloatingMode_UsesCanonicalFloatingBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeFloating})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: specialKey(tea.KeyTab), want: ActionFocusNextFloatingPane},
		{msg: specialKey(tea.KeyShiftTab), want: ActionFocusPrevFloatingPane},
		{msg: runeKey('h'), want: ActionMoveFloatingLeft},
		{msg: runeKey('j'), want: ActionMoveFloatingDown},
		{msg: runeKey('k'), want: ActionMoveFloatingUp},
		{msg: runeKey('l'), want: ActionMoveFloatingRight},
		{msg: runeKey('H'), want: ActionResizeFloatingLeft},
		{msg: runeKey('J'), want: ActionResizeFloatingDown},
		{msg: runeKey('K'), want: ActionResizeFloatingUp},
		{msg: runeKey('L'), want: ActionResizeFloatingRight},
		{msg: runeKey('a'), want: ActionBecomeOwner},
		{msg: runeKey('c'), want: ActionCenterFloatingPane},
		{msg: runeKey('n'), want: ActionCreateFloatingPane},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_WorkspacePickerMode_UsesWorkspacePickerBindings(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModeWorkspacePicker})
	cases := []struct {
		msg  tea.KeyMsg
		want ActionKind
	}{
		{msg: specialKey(tea.KeyUp), want: ActionPickerUp},
		{msg: specialKey(tea.KeyDown), want: ActionPickerDown},
		{msg: specialKey(tea.KeyEnter), want: ActionSubmitPrompt},
		{msg: specialKey(tea.KeyEsc), want: ActionCancelMode},
	}
	for _, testCase := range cases {
		result := r.RouteKeyMsg(testCase.msg)
		if result.Action == nil || result.Action.Kind != testCase.want {
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
	}
}

func TestRouteKeyMsg_NormalMode_CtrlBackslashProducesOpenWorkspacePicker(t *testing.T) {
	t.Skip("workspace root key moved to Ctrl-W per canonical keybinding spec")
}

func TestRouteKeyMsg_PaneMode_UnboundKeyIgnored(t *testing.T) {
	// Unrecognised keys in pane mode are silently swallowed (not forwarded to terminal).
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePane})
	result := r.RouteKeyMsg(runeKey('x'))
	if result.Action != nil || result.TerminalInput != nil {
		t.Fatalf("expected ignored key in pane mode, got %#v", result)
	}
}

func TestRouter_SetMode_RoundTrip(t *testing.T) {
	r := NewRouter()
	if r.Mode().Kind != ModeNormal {
		t.Fatalf("expected ModeNormal, got %q", r.Mode().Kind)
	}
	r.SetMode(ModeState{Kind: ModePicker})
	if r.Mode().Kind != ModePicker {
		t.Fatalf("expected ModePicker, got %q", r.Mode().Kind)
	}
}

func TestRouter_CustomKeymap_RespectedInNormalMode(t *testing.T) {
	km := Keymap{Normal: []Binding{{Type: tea.KeyCtrlT, Action: ActionOpenTerminalManager}}}
	r := NewRouterWithKeymap(km)
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlT))
	if result.Action == nil || result.Action.Kind != ActionOpenTerminalManager {
		t.Fatalf("expected custom action, got %#v", result.Action)
	}
	result2 := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlF))
	if result2.Action != nil {
		t.Fatalf("expected no action for unmapped key, got %#v", result2.Action)
	}
}
