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

func TestRouteKeyMsg_NormalMode_ExtendedBindings(t *testing.T) {
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
			t.Fatalf("msg %v: expected %q, got %#v", testCase.msg, testCase.want, result.Action)
		}
		if result.TerminalInput != nil {
			t.Fatalf("msg %v: expected no terminal input, got %#v", testCase.msg, result.TerminalInput)
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

func TestRouteKeyMsg_PickerMode_UnboundRuneIgnored(t *testing.T) {
	r := NewRouter()
	r.SetMode(ModeState{Kind: ModePicker})
	result := r.RouteKeyMsg(runeKey('a'))
	if result.Action != nil || result.TerminalInput != nil {
		t.Fatalf("expected ignored key in picker mode, got %#v", result)
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

func TestRouteKeyMsg_NormalMode_QuestionMarkProducesOpenHelp(t *testing.T) {
	r := NewRouter()
	result := r.RouteKeyMsg(ctrlKey(tea.KeyCtrlG))
	if result.Action == nil || result.Action.Kind != ActionEnterGlobalMode {
		t.Fatalf("expected ActionEnterGlobalMode from Ctrl-G, got %#v", result.Action)
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
