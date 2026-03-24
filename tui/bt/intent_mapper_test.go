package bt

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestIntentMapperRootCtrlWOpensWorkspacePicker(t *testing.T) {
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)},
		PrefixTimeout: 3 * time.Second,
	})

	intents := mapper.MapKey(newAppStateWithSinglePane(), tea.KeyMsg{Type: tea.KeyCtrlW})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.OpenWorkspacePickerIntent); !ok {
		t.Fatalf("expected open workspace picker intent, got %T", intents[0])
	}
}

func TestIntentMapperRootCtrlFOpensTerminalPicker(t *testing.T) {
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)},
		PrefixTimeout: 3 * time.Second,
	})

	intents := mapper.MapKey(newAppStateWithSinglePane(), tea.KeyMsg{Type: tea.KeyCtrlF})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.OpenTerminalPickerIntent); !ok {
		t.Fatalf("expected open terminal picker intent, got %T", intents[0])
	}
}

func TestIntentMapperRootCtrlGArmsGlobalModeAndTOpensTerminalManager(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: now},
		PrefixTimeout: 3 * time.Second,
	})

	intents := mapper.MapKey(newAppStateWithSinglePane(), tea.KeyMsg{Type: tea.KeyCtrlG})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	activate, ok := intents[0].(intent.ActivateModeIntent)
	if !ok {
		t.Fatalf("expected activate mode intent, got %T", intents[0])
	}
	if activate.Mode != types.ModeGlobal || activate.DeadlineAt == nil || !activate.DeadlineAt.Equal(now.Add(3*time.Second)) {
		t.Fatalf("unexpected activate mode payload: %+v", activate)
	}

	state := newAppStateWithSinglePane()
	state.UI.Mode = types.ModeState{
		Active:     types.ModeGlobal,
		DeadlineAt: activate.DeadlineAt,
	}
	intents = mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.OpenTerminalManagerIntent); !ok {
		t.Fatalf("expected open terminal manager intent, got %T", intents[0])
	}
}

func TestIntentMapperWorkspacePickerMapsNavigationAndQuery(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayWorkspacePicker}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "down",
			key:  tea.KeyMsg{Type: tea.KeyDown},
			want: intent.WorkspacePickerMoveIntent{Delta: 1},
		},
		{
			name: "left",
			key:  tea.KeyMsg{Type: tea.KeyLeft},
			want: intent.WorkspacePickerCollapseIntent{},
		},
		{
			name: "enter",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.WorkspacePickerSubmitIntent{},
		},
		{
			name: "backspace",
			key:  tea.KeyMsg{Type: tea.KeyBackspace},
			want: intent.WorkspacePickerBackspaceIntent{},
		},
		{
			name: "query",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")},
			want: intent.WorkspacePickerAppendQueryIntent{Text: "ops"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := mapper.MapKey(state, tc.key)
			if len(intents) != 1 {
				t.Fatalf("expected one intent, got %d", len(intents))
			}
			if intents[0] != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, intents[0])
			}
		})
	}
}

func TestIntentMapperOverlayMouseWheelMapsSelectionMoves(t *testing.T) {
	mapper := NewIntentMapper(Config{})

	cases := []struct {
		name  string
		state types.AppState
		mouse tea.MouseMsg
		want  any
	}{
		{
			name: "workspace picker wheel down",
			state: func() types.AppState {
				s := newAppStateWithSinglePane()
				s.UI.Overlay = types.OverlayState{Kind: types.OverlayWorkspacePicker}
				return s
			}(),
			mouse: tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
			want:  intent.WorkspacePickerMoveIntent{Delta: 1},
		},
		{
			name: "terminal picker wheel up",
			state: func() types.AppState {
				s := newAppStateWithSinglePane()
				s.UI.Overlay = types.OverlayState{Kind: types.OverlayTerminalPicker}
				return s
			}(),
			mouse: tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress},
			want:  intent.TerminalPickerMoveIntent{Delta: -1},
		},
		{
			name: "terminal manager wheel down",
			state: func() types.AppState {
				s := newAppStateWithSinglePane()
				s.UI.Overlay = types.OverlayState{Kind: types.OverlayTerminalManager}
				return s
			}(),
			mouse: tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
			want:  intent.TerminalManagerMoveIntent{Delta: 1},
		},
		{
			name: "layout resolve wheel up",
			state: func() types.AppState {
				s := newAppStateWithSinglePane()
				s.UI.Overlay = types.OverlayState{Kind: types.OverlayLayoutResolve}
				return s
			}(),
			mouse: tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress},
			want:  intent.LayoutResolveMoveIntent{Delta: -1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := mapper.MapMouse(tc.state, tc.mouse, "")
			if len(intents) != 1 {
				t.Fatalf("expected one intent, got %d", len(intents))
			}
			if intents[0] != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, intents[0])
			}
		})
	}
}

func TestIntentMapperTerminalManagerMouseClickSelectsVisibleTerminalRow(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithTerminalManagerTargets()
	manager := state.UI.Overlay.Data.(*terminalmanagerdomain.State)
	manager.MoveSelection(1)
	view := strings.Join([]string{
		"termx",
		"terminal_manager_rows: | terminal_manager_rows_rendered: 4 | terminal_manager_rows_truncated: true",
		"  [header] VISIBLE",
		"  [terminal] api-dev",
		"  [header] PARKED",
		"> [terminal] build-log",
	}, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      3,
	}, view)
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	move, ok := intents[0].(intent.TerminalManagerMoveIntent)
	if !ok {
		t.Fatalf("expected terminal manager move intent, got %T", intents[0])
	}
	if move.Delta != -1 {
		t.Fatalf("expected click on visible api-dev row to move selection back once, got %+v", move)
	}
}

func TestIntentMapperWorkspacePickerMouseClickOnSelectedRowSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithTwoWorkspaces()
	rd := reducer.New()
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyCtrlW}) {
		state = rd.Reduce(state, in).State
	}
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")}) {
		state = rd.Reduce(state, in).State
	}
	picker := state.UI.Overlay.Data.(*workspacedomain.PickerState)
	rows := picker.VisibleRows()
	selected, _ := picker.SelectedRow()
	selectedIndex := 0
	for idx, row := range rows {
		if row.Node.Key == selected.Node.Key {
			selectedIndex = idx
			break
		}
	}
	start, end := overlayPreviewWindow(len(rows), overlayPreviewRowLimit, selectedIndex)
	viewLines := []string{"termx", fmt.Sprintf("workspace_picker_rows: | workspace_picker_rows_rendered: %d", end-start)}
	for _, row := range rows[start:end] {
		prefix := "  "
		if row.Node.Key == selected.Node.Key {
			prefix = "> "
		}
		viewLines = append(viewLines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
	}
	view := strings.Join(viewLines, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      2 + (selectedIndex - start),
	}, view)
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.WorkspacePickerSubmitIntent); !ok {
		t.Fatalf("expected workspace picker submit intent, got %T", intents[0])
	}
}

func TestIntentMapperWorkspacePickerMouseClickOnCreateRowMovesAndSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithTwoWorkspaces()
	rd := reducer.New()
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyCtrlW}) {
		state = rd.Reduce(state, in).State
	}
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")}) {
		state = rd.Reduce(state, in).State
	}
	picker := state.UI.Overlay.Data.(*workspacedomain.PickerState)
	rows := picker.VisibleRows()
	selected, _ := picker.SelectedRow()
	selectedIndex := 0
	for idx, row := range rows {
		if row.Node.Key == selected.Node.Key {
			selectedIndex = idx
			break
		}
	}
	start, end := overlayPreviewWindow(len(rows), overlayPreviewRowLimit, selectedIndex)
	viewLines := []string{"termx", fmt.Sprintf("workspace_picker_rows: | workspace_picker_rows_rendered: %d", end-start)}
	for _, row := range rows[start:end] {
		prefix := "  "
		if row.Node.Key == selected.Node.Key {
			prefix = "> "
		}
		viewLines = append(viewLines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
	}
	view := strings.Join(viewLines, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      2,
	}, view)
	if len(intents) != 2 {
		t.Fatalf("expected two intents, got %d", len(intents))
	}
	expectedDelta := start - selectedIndex
	if intents[0] != (intent.WorkspacePickerMoveIntent{Delta: expectedDelta}) {
		t.Fatalf("expected workspace picker move intent, got %+v", intents[0])
	}
	if _, ok := intents[1].(intent.WorkspacePickerSubmitIntent); !ok {
		t.Fatalf("expected workspace picker submit intent, got %T", intents[1])
	}
}

func TestIntentMapperTerminalPickerMouseClickOnSelectedRowSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithTerminalManagerTargets()
	state.UI.Overlay = types.OverlayState{}
	rd := reducer.New()
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyCtrlF}) {
		state = rd.Reduce(state, in).State
	}
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")}) {
		state = rd.Reduce(state, in).State
	}
	picker := state.UI.Overlay.Data.(*terminalpickerdomain.State)
	rows := picker.VisibleRows()
	selected, _ := picker.SelectedRow()
	selectedIndex := 0
	for idx, row := range rows {
		if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			selectedIndex = idx
			break
		}
	}
	start, end := overlayPreviewWindow(len(rows), overlayPreviewRowLimit, selectedIndex)
	viewLines := []string{"termx", fmt.Sprintf("terminal_picker_rows: | terminal_picker_rows_rendered: %d", end-start)}
	for _, row := range rows[start:end] {
		prefix := "  "
		if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		viewLines = append(viewLines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	view := strings.Join(viewLines, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      2 + (selectedIndex - start),
	}, view)
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.TerminalPickerSubmitIntent); !ok {
		t.Fatalf("expected terminal picker submit intent, got %T", intents[0])
	}
}

func TestIntentMapperTerminalPickerMouseClickOnCreateRowMovesAndSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithTerminalManagerTargets()
	state.UI.Overlay = types.OverlayState{}
	rd := reducer.New()
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyCtrlF}) {
		state = rd.Reduce(state, in).State
	}
	for _, in := range mapper.MapKey(state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")}) {
		state = rd.Reduce(state, in).State
	}
	picker := state.UI.Overlay.Data.(*terminalpickerdomain.State)
	rows := picker.VisibleRows()
	selected, _ := picker.SelectedRow()
	selectedIndex := 0
	for idx, row := range rows {
		if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			selectedIndex = idx
			break
		}
	}
	start, end := overlayPreviewWindow(len(rows), overlayPreviewRowLimit, selectedIndex)
	viewLines := []string{"termx", fmt.Sprintf("terminal_picker_rows: | terminal_picker_rows_rendered: %d", end-start)}
	for _, row := range rows[start:end] {
		prefix := "  "
		if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		viewLines = append(viewLines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	view := strings.Join(viewLines, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      2,
	}, view)
	if len(intents) != 2 {
		t.Fatalf("expected two intents, got %d", len(intents))
	}
	expectedDelta := start - selectedIndex
	if intents[0] != (intent.TerminalPickerMoveIntent{Delta: expectedDelta}) {
		t.Fatalf("expected terminal picker move intent, got %+v", intents[0])
	}
	if _, ok := intents[1].(intent.TerminalPickerSubmitIntent); !ok {
		t.Fatalf("expected terminal picker submit intent, got %T", intents[1])
	}
}

func TestIntentMapperLayoutResolveMouseClickOnSelectedRowSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayLayoutResolve,
		Data: layoutresolvedomain.NewState(types.PaneID("pane-1"), "backend-dev", "env=dev"),
	}
	view := strings.Join([]string{
		"termx",
		"layout_resolve_rows: | layout_resolve_rows_rendered: 3",
		"> [connect_existing] connect existing",
	}, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      2,
	}, view)
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.LayoutResolveSubmitIntent); !ok {
		t.Fatalf("expected layout resolve submit intent, got %T", intents[0])
	}
}

func TestIntentMapperLayoutResolveMouseClickOnCreateNewMovesAndSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayLayoutResolve,
		Data: layoutresolvedomain.NewState(types.PaneID("pane-1"), "backend-dev", "env=dev"),
	}
	view := strings.Join([]string{
		"termx",
		"layout_resolve_rows: | layout_resolve_rows_rendered: 3",
		"> [connect_existing] connect existing",
		"  [create_new] create new",
		"  [skip] skip",
	}, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      3,
	}, view)
	if len(intents) != 2 {
		t.Fatalf("expected two intents, got %d", len(intents))
	}
	if intents[0] != (intent.LayoutResolveMoveIntent{Delta: 1}) {
		t.Fatalf("expected layout resolve move intent, got %+v", intents[0])
	}
	if _, ok := intents[1].(intent.LayoutResolveSubmitIntent); !ok {
		t.Fatalf("expected layout resolve submit intent, got %T", intents[1])
	}
}

func TestIntentMapperTerminalManagerMouseClickOnSelectedRowSubmits(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithTerminalManagerTargets()
	manager := state.UI.Overlay.Data.(*terminalmanagerdomain.State)
	manager.MoveSelection(1)
	view := strings.Join([]string{
		"termx",
		"terminal_manager_rows: | terminal_manager_rows_rendered: 4 | terminal_manager_rows_truncated: true",
		"  [header] VISIBLE",
		"  [terminal] api-dev",
		"  [header] PARKED",
		"> [terminal] build-log",
	}, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      5,
	}, view)
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, ok := intents[0].(intent.TerminalManagerConnectHereIntent); !ok {
		t.Fatalf("expected terminal manager connect-here intent, got %T", intents[0])
	}
}

func TestIntentMapperTerminalPickerMapsNavigationAndQuery(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayTerminalPicker}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "down",
			key:  tea.KeyMsg{Type: tea.KeyDown},
			want: intent.TerminalPickerMoveIntent{Delta: 1},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.TerminalPickerSubmitIntent{},
		},
		{
			name: "backspace",
			key:  tea.KeyMsg{Type: tea.KeyBackspace},
			want: intent.TerminalPickerBackspaceIntent{},
		},
		{
			name: "query",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ops")},
			want: intent.TerminalPickerAppendQueryIntent{Text: "ops"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := mapper.MapKey(state, tc.key)
			if len(intents) != 1 {
				t.Fatalf("expected one intent, got %d", len(intents))
			}
			if intents[0] != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, intents[0])
			}
		})
	}
}

func TestIntentMapperPromptMapsStructuredFieldKeys(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind: promptdomain.KindEditTerminalMetadata,
			Fields: []promptdomain.Field{
				{Key: "name", Value: "build-log"},
				{Key: "tags", Value: "group=build"},
			},
		},
	}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "tab",
			key:  tea.KeyMsg{Type: tea.KeyTab},
			want: intent.PromptNextFieldIntent{},
		},
		{
			name: "shift tab",
			key:  tea.KeyMsg{Type: tea.KeyShiftTab},
			want: intent.PromptPreviousFieldIntent{},
		},
		{
			name: "input",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-v2")},
			want: intent.PromptAppendInputIntent{Text: "-v2"},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.SubmitPromptIntent{},
		},
		{
			name: "cancel",
			key:  tea.KeyMsg{Type: tea.KeyEsc},
			want: intent.CancelPromptIntent{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := mapper.MapKey(state, tc.key)
			if len(intents) != 1 {
				t.Fatalf("expected one intent, got %d", len(intents))
			}
			if intents[0] != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, intents[0])
			}
		})
	}
}

func TestIntentMapperPromptMouseClickSelectsStructuredField(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind: promptdomain.KindEditTerminalMetadata,
			Fields: []promptdomain.Field{
				{Key: "name", Label: "Name", Value: "build-log"},
				{Key: "tags", Label: "Tags", Value: "group=build"},
			},
		},
	}
	view := strings.Join([]string{
		"termx",
		"prompt_fields: | prompt_fields_rendered: 2",
		"> [name] Name: build-log",
		"  [tags] Tags: group=build",
	}, "\n")

	intents := mapper.MapMouse(state, tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      3,
	}, view)
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if intents[0] != (intent.PromptSelectFieldIntent{Index: 1}) {
		t.Fatalf("expected prompt select field intent, got %+v", intents[0])
	}
}

func TestIntentMapperTerminalManagerMapsSelectionAndQuery(t *testing.T) {
	mapper := NewIntentMapper(Config{})
	state := newAppStateWithSinglePane()
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayTerminalManager}

	cases := []struct {
		name string
		key  tea.KeyMsg
		want any
	}{
		{
			name: "up",
			key:  tea.KeyMsg{Type: tea.KeyUp},
			want: intent.TerminalManagerMoveIntent{Delta: -1},
		},
		{
			name: "query",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("build")},
			want: intent.TerminalManagerAppendQueryIntent{Text: "build"},
		},
		{
			name: "submit",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			want: intent.TerminalManagerConnectHereIntent{},
		},
		{
			name: "new tab",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")},
			want: intent.TerminalManagerConnectInNewTabIntent{},
		},
		{
			name: "floating",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")},
			want: intent.TerminalManagerConnectInFloatingPaneIntent{},
		},
		{
			name: "edit",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")},
			want: intent.TerminalManagerEditMetadataIntent{},
		},
		{
			name: "acquire owner",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")},
			want: intent.TerminalManagerAcquireOwnerIntent{},
		},
		{
			name: "stop",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
			want: intent.TerminalManagerStopIntent{},
		},
		{
			name: "cancel",
			key:  tea.KeyMsg{Type: tea.KeyEsc},
			want: intent.CloseOverlayIntent{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := mapper.MapKey(state, tc.key)
			if len(intents) != 1 {
				t.Fatalf("expected one intent, got %d", len(intents))
			}
			if intents[0] != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, intents[0])
			}
		})
	}
}

func TestE2EIntentMapperScenarioWorkspacePickerSearchesAndJumpsToPane(t *testing.T) {
	mapper := NewIntentMapper(Config{
		Clock:         fixedClock{now: time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)},
		PrefixTimeout: 3 * time.Second,
	})
	state := newAppStateWithTwoWorkspaces()
	rd := reducer.New()

	sequence := []tea.KeyMsg{
		{Type: tea.KeyCtrlW},
		{Type: tea.KeyRunes, Runes: []rune("float-dev")},
		{Type: tea.KeyEnter},
	}
	for _, key := range sequence {
		for _, in := range mapper.MapKey(state, key) {
			state = rd.Reduce(state, in).State
		}
	}

	if state.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected active workspace to switch to ws-2, got %q", state.Domain.ActiveWorkspaceID)
	}
	if state.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close, got %q", state.UI.Overlay.Kind)
	}
	if state.UI.Focus.PaneID != types.PaneID("pane-float") || state.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected focus to land on floating pane, got %+v", state.UI.Focus)
	}
}

func newAppStateWithSinglePane() types.AppState {
	return types.AppState{
		Domain: types.DomainState{
			ActiveWorkspaceID: types.WorkspaceID("ws-1"),
			WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-1")},
			Workspaces: map[types.WorkspaceID]types.WorkspaceState{
				types.WorkspaceID("ws-1"): {
					ID:          types.WorkspaceID("ws-1"),
					Name:        "ws-1",
					ActiveTabID: types.TabID("tab-1"),
					TabOrder:    []types.TabID{types.TabID("tab-1")},
					Tabs: map[types.TabID]types.TabState{
						types.TabID("tab-1"): {
							ID:           types.TabID("tab-1"),
							Name:         "tab-1",
							ActivePaneID: types.PaneID("pane-1"),
							ActiveLayer:  types.FocusLayerTiled,
							Panes: map[types.PaneID]types.PaneState{
								types.PaneID("pane-1"): {
									ID:        types.PaneID("pane-1"),
									Kind:      types.PaneKindTiled,
									SlotState: types.PaneSlotEmpty,
								},
							},
						},
					},
				},
			},
			Terminals:   map[types.TerminalID]types.TerminalRef{},
			Connections: map[types.TerminalID]types.ConnectionState{},
		},
		UI: types.UIState{
			Focus: types.FocusState{
				Layer:       types.FocusLayerTiled,
				WorkspaceID: types.WorkspaceID("ws-1"),
				TabID:       types.TabID("tab-1"),
				PaneID:      types.PaneID("pane-1"),
			},
		},
	}
}

func newAppStateWithTwoWorkspaces() types.AppState {
	state := newAppStateWithSinglePane()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ws-2",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "tab-2",
				ActivePaneID: types.PaneID("pane-float"),
				ActiveLayer:  types.FocusLayerFloating,
				FloatingOrder: []types.PaneID{
					types.PaneID("pane-float"),
				},
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-float"): {
						ID:         types.PaneID("pane-float"),
						Kind:       types.PaneKindFloating,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-float"),
					},
				},
			},
		},
	}
	state.Domain.Terminals[types.TerminalID("term-float")] = types.TerminalRef{
		ID:    types.TerminalID("term-float"),
		Name:  "float-dev",
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-float")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-float"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-float")},
		OwnerPaneID:      types.PaneID("pane-float"),
	}
	return state
}

func newAppStateWithTerminalManagerTargets() types.AppState {
	state := newAppStateWithSinglePane()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotConnected
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Command: []string{"tail", "-f", "build.log"},
		Tags:    map[string]string{"group": "build"},
	}
	state.Domain.Terminals[types.TerminalID("term-3")] = types.TerminalRef{
		ID:      types.TerminalID("term-3"),
		Name:    "ops-watch",
		State:   types.TerminalRunStateRunning,
		Command: []string{"journalctl", "-f"},
		Tags:    map[string]string{"team": "ops"},
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: terminalmanagerdomain.NewState(state.Domain, state.UI.Focus),
	}
	return state
}
