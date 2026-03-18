package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"golang.org/x/text/unicode/norm"
)

func TestModelPrefixActions(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	if cmd := model.Init(); cmd == nil {
		t.Fatal("expected init command")
	}
	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(model.workspace.Tabs))
	}
	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(tab.Panes))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected split to create pane, got %d", len(model.currentTab().Panes))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if len(model.workspace.Tabs) != 2 {
		t.Fatalf("expected new tab, got %d", len(model.workspace.Tabs))
	}
}

func TestModelViewShowsWelcomeAndHelp(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 28

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	view := model.View()
	if !containsAll(view, "termx", "Ctrl-a", "split", "new tab") {
		t.Fatalf("welcome view missing expected hints:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := model.View()
	if !containsAll(helpView, "Help", "Ctrl-a %", "Ctrl-a c", "Ctrl-a d") {
		t.Fatalf("help overlay missing expected content:\n%s", helpView)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.showHelp {
		t.Fatal("expected esc to close help overlay")
	}
}

func TestHelpAndStatusShowViewportControlsAndState(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 16

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	pane.Offset = Point{X: 4, Y: 0}
	pane.renderDirty = true

	model.width = 240
	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "mode:fixed", "pinned", "readonly", "offset:4,0") {
		t.Fatalf("expected status to expose viewport state, got:\n%s", status)
	}

	model.height = 32
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := xansi.Strip(model.View())
	if !containsAll(helpView, "Ctrl-a M", "Ctrl-a P", "Ctrl-a R", "Ctrl-a Ctrl-h/j/k/l") {
		t.Fatalf("expected help to include viewport controls, got:\n%s", helpView)
	}
}

func TestModelViewPreservesAnsiColors(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[31mRED\x1b[0m"))
	pane.live = true
	pane.renderDirty = true

	view := model.View()
	if !strings.Contains(view, "\x1b[") {
		t.Fatalf("expected ANSI sequences in rendered view:\n%s", view)
	}
	if !strings.Contains(xansi.Strip(view), "RED") {
		t.Fatalf("expected rendered text in view:\n%s", view)
	}
}

func TestModelViewRendersCJKWithoutInsertedSpaces(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("你a好"))
	pane.live = true
	pane.renderDirty = true

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "你a好") {
		t.Fatalf("expected contiguous CJK text in view, got:\n%s", view)
	}
	if strings.Contains(view, "你 a 好") || strings.Contains(view, "你 a好") || strings.Contains(view, "你a 好") {
		t.Fatalf("expected no inserted spaces in CJK rendering, got:\n%s", view)
	}
}

func TestModelViewRendersUnicodeClustersWithoutInsertedSpaces(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	text := "e\u0301🙂한글"
	_, _ = pane.VTerm.Write([]byte(text))
	pane.live = true
	pane.renderDirty = true

	view := xansi.Strip(model.View())
	if !strings.Contains(view, norm.NFC.String(text)) {
		t.Fatalf("expected contiguous unicode text in view, got:\n%s", view)
	}
	if strings.Contains(view, "e ́") || strings.Contains(view, "🙂 한") || strings.Contains(view, "한 글") {
		t.Fatalf("expected no inserted spaces around unicode clusters, got:\n%s", view)
	}
}

func TestActivePaneRendersVisibleCursor(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("cursor"))
	pane.live = true
	pane.renderDirty = true

	view := model.View()
	if !strings.Contains(view, "\x1b[0;7m") {
		t.Fatalf("expected reverse-video cursor in rendered view:\n%s", view)
	}
}

func TestCtrlCAndCtrlDPassThroughToActivePane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = mustRunCmd(t, cmd)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	_ = mustRunCmd(t, cmd)

	if model.quitting {
		t.Fatal("expected ctrl-c/ctrl-d to be sent to pane, not quit TUI")
	}
	if len(client.inputs) != 2 {
		t.Fatalf("expected 2 input writes, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x03" {
		t.Fatalf("expected ctrl-c byte, got %q", got)
	}
	if got := string(client.inputs[1]); got != "\x04" {
		t.Fatalf("expected ctrl-d byte, got %q", got)
	}
}

func TestReadonlyViewportBlocksInputExceptCtrlC(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil || !pane.Readonly {
		t.Fatal("expected active pane to enter readonly mode")
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected ctrl-c to remain writable in readonly mode")
	}
	_ = mustRunCmd(t, cmd)

	if got := len(client.inputs); got != 1 {
		t.Fatalf("expected only ctrl-c to pass through readonly mode, got %d writes", got)
	}
	if got := string(client.inputs[0]); got != "\x03" {
		t.Fatalf("expected ctrl-c payload, got %q", got)
	}
}

func TestReadonlyViewportBlocksRawInputButAllowsCtrlC(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	_, cmd := model.Update(rawInputMsg{data: []byte("ab\x03cd")})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if got := len(client.inputs); got != 1 {
		t.Fatalf("expected raw readonly path to only forward ctrl-c, got %d writes", got)
	}
	if got := string(client.inputs[0]); got != "\x03" {
		t.Fatalf("expected raw readonly payload to contain only ctrl-c, got %q", got)
	}
}

func TestEnterPassThroughUsesCarriageReturn(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\r" {
		t.Fatalf("expected carriage return for enter, got %q", got)
	}
}

func TestRawInputPassesBytesThroughUnchanged(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	raw := []byte{'a', '\r', '\t', 0x7f}
	_, cmd := model.Update(rawInputMsg{data: raw})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != string(raw) {
		t.Fatalf("expected raw bytes %q, got %q", string(raw), got)
	}
}

func TestRawInputUnicodePassesBytesThroughUnchanged(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	raw := []byte("🩷🙂你好")
	_, cmd := model.Update(rawInputMsg{data: raw})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != string(raw) {
		t.Fatalf("expected raw unicode bytes %q, got %q", string(raw), got)
	}
}

func TestRawPrefixCtrlAForwardsLiteralCtrlA(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(rawInputMsg{data: []byte{0x01}})
	_ = mustRunCmd(t, cmd)
	if !model.prefixActive {
		t.Fatal("expected prefix mode to be active")
	}

	_, cmd = model.Update(rawInputMsg{data: []byte{0x01}})
	_ = mustRunCmd(t, cmd)

	if model.prefixActive {
		t.Fatal("expected literal ctrl-a to clear prefix mode")
	}
	if len(client.inputs) != 1 || string(client.inputs[0]) != "\x01" {
		t.Fatalf("expected forwarded ctrl-a, got %#v", client.inputs)
	}
}

func TestRawArrowKeysUseApplicationCursorModeWhenRequested(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?1h"))
	pane.live = true

	_, cmd := model.Update(rawInputMsg{data: []byte("\x1b[A")})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1bOA" {
		t.Fatalf("expected application cursor up sequence, got %q", got)
	}
}

func TestRawArrowKeysStayRawWhenApplicationCursorModeIsOff(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(rawInputMsg{data: []byte("\x1b[A")})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1b[A" {
		t.Fatalf("expected raw cursor up sequence, got %q", got)
	}
}

func TestInputEventArrowUsesApplicationCursorModeWhenRequested(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?1h"))
	pane.live = true

	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyUp}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1bOA" {
		t.Fatalf("expected application cursor up sequence, got %q", got)
	}
}

func TestInputEventTextPassesThroughViaVTerm(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'a', Text: "a"}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "a" {
		t.Fatalf("expected text input to be forwarded, got %q", got)
	}
}

func TestInputEventExtendedTextUsesTextPayload(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	text := "🩷"
	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyExtended, Text: text}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != text {
		t.Fatalf("expected extended text input to be forwarded, got %q", got)
	}
}

func TestInputEventAltExtendedTextUsesEscapePrefix(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	text := "🩷"
	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyExtended, Text: text, Mod: uv.ModAlt}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1b"+text {
		t.Fatalf("expected alt+extended text input to be forwarded with escape prefix, got %q", got)
	}
}

func TestInputEventSpecialKeysEncodeForPane(t *testing.T) {
	tests := []struct {
		name  string
		event uv.KeyPressEvent
		want  string
	}{
		{name: "shift-tab", event: uv.KeyPressEvent{Code: uv.KeyTab, Mod: uv.ModShift}, want: "\x1b[Z"},
		{name: "delete", event: uv.KeyPressEvent{Code: uv.KeyDelete}, want: "\x1b[3~"},
		{name: "home", event: uv.KeyPressEvent{Code: uv.KeyHome}, want: "\x1b[H"},
		{name: "end", event: uv.KeyPressEvent{Code: uv.KeyEnd}, want: "\x1b[F"},
		{name: "pgup", event: uv.KeyPressEvent{Code: uv.KeyPgUp}, want: "\x1b[5~"},
		{name: "pgdown", event: uv.KeyPressEvent{Code: uv.KeyPgDown}, want: "\x1b[6~"},
		{name: "f1", event: uv.KeyPressEvent{Code: uv.KeyF1}, want: "\x1bOP"},
		{name: "f5", event: uv.KeyPressEvent{Code: uv.KeyF5}, want: "\x1b[15~"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{}
			model := NewModel(client, Config{DefaultShell: "/bin/sh"})

			msg := mustRunCmd(t, model.Init())
			_, _ = model.Update(msg)

			_, cmd := model.Update(inputEventMsg{event: tc.event})
			_ = mustRunCmd(t, cmd)

			if len(client.inputs) != 1 {
				t.Fatalf("expected 1 input write, got %d", len(client.inputs))
			}
			if got := string(client.inputs[0]); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestInputEventPasteUsesBracketedPasteWhenPaneRequestsIt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?2004h"))
	pane.live = true

	_, cmd := model.Update(inputEventMsg{event: uv.PasteEvent{Content: "hello\nworld"}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got, want := string(client.inputs[0]), "\x1b[200~hello\nworld\x1b[201~"; got != want {
		t.Fatalf("expected bracketed paste %q, got %q", want, got)
	}
}

func TestPrefixTimeoutClearsPrefixMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.prefixTimeout = time.Millisecond

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if !model.prefixActive {
		t.Fatal("expected prefix mode to be active")
	}
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.prefixActive {
		t.Fatal("expected prefix timeout to clear prefix mode")
	}
}

func TestPrefixArrowNavigationMovesFocus(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	original := model.currentTab().ActivePaneID

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	_, _ = model.Update(mustRunCmd(t, cmd))

	if model.currentTab().ActivePaneID == original {
		t.Fatal("expected split to focus the new pane")
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if model.currentTab().ActivePaneID != original {
		t.Fatalf("expected left arrow to move focus back to %q, got %q", original, model.currentTab().ActivePaneID)
	}
}

func TestPrefixSwapMovesActivePanePosition(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	_, resizeCmd := model.Update(mustRunCmd(t, cmd))
	if resizeCmd != nil {
		_ = mustRunCmd(t, resizeCmd)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'"'}})
	_, resizeCmd = model.Update(mustRunCmd(t, cmd))
	if resizeCmd != nil {
		_ = mustRunCmd(t, resizeCmd)
	}

	tab := model.currentTab()
	activeID := tab.ActivePaneID
	before := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{'}})
	_, resizeCmd = model.Update(mustRunCmd(t, cmd))
	if resizeCmd != nil {
		_ = mustRunCmd(t, resizeCmd)
	}

	after := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]
	if after == before {
		t.Fatalf("expected active pane %q to move after swap, rect stayed %#v", activeID, after)
	}
	if tab.ActivePaneID != activeID {
		t.Fatalf("expected swap to keep focus on %q, got %q", activeID, tab.ActivePaneID)
	}
}

func TestClosingCurrentTabKillsItsPanesAndReturnsToPreviousTab(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	firstTab := model.currentTab()
	firstTabPaneCount := len(firstTab.Panes)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	secondTab := model.currentTab()
	killedIDs := make(map[string]bool, len(secondTab.Panes))
	for _, pane := range secondTab.Panes {
		killedIDs[pane.TerminalID] = true
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'&'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if model.quitting {
		t.Fatal("expected closing one of multiple tabs to keep TUI running")
	}
	if cmd != nil {
		t.Fatal("expected no quit command when another tab remains")
	}
	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected 1 tab after close, got %d", len(model.workspace.Tabs))
	}
	if model.workspace.ActiveTab != 0 {
		t.Fatalf("expected focus to return to first tab, got %d", model.workspace.ActiveTab)
	}
	if got := len(model.currentTab().Panes); got != firstTabPaneCount {
		t.Fatalf("expected original first tab pane count %d, got %d", firstTabPaneCount, got)
	}
	if len(client.killedIDs) != len(killedIDs) {
		t.Fatalf("expected %d killed terminals, got %d", len(killedIDs), len(client.killedIDs))
	}
	for _, terminalID := range client.killedIDs {
		if !killedIDs[terminalID] {
			t.Fatalf("unexpected killed terminal %q", terminalID)
		}
	}
}

func TestClosingLastTabQuitsTUI(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'&'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if !model.quitting {
		t.Fatal("expected closing the last tab to quit the TUI")
	}
	if _, ok := mustRunCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("expected quit command after closing the last tab")
	}
	if len(client.killedIDs) != 1 {
		t.Fatalf("expected 1 killed terminal, got %d", len(client.killedIDs))
	}
}

func TestTerminalPickerEnterReplacesCurrentViewport(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	original := model.currentTab().Panes[model.currentTab().ActivePaneID].TerminalID

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane.TerminalID != "shared-001" {
		t.Fatalf("expected active pane to attach shared terminal, got %q", pane.TerminalID)
	}
	if len(tab.Panes) != 1 {
		t.Fatalf("expected picker attach to replace current viewport, got %d panes", len(tab.Panes))
	}
	if pane.TerminalID == original {
		t.Fatalf("expected terminal to change from %q", original)
	}
	if model.terminalPicker != nil {
		t.Fatal("expected picker to close after attach")
	}
}

func TestTerminalPickerTabSplitsAndAttachesExistingTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	tab := model.currentTab()
	if len(tab.Panes) != 2 {
		t.Fatalf("expected picker tab action to split current viewport, got %d panes", len(tab.Panes))
	}
	active := tab.Panes[tab.ActivePaneID]
	if active == nil || active.TerminalID != "shared-001" {
		t.Fatalf("expected new active pane to attach shared terminal, got %#v", active)
	}
}

func TestClosingViewportDetachesWithoutKillingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	tab := model.currentTab()
	orphanID := tab.Panes[tab.ActivePaneID].TerminalID

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		t.Fatal("expected detach to stay in the TUI when another viewport remains")
	}

	if model.quitting {
		t.Fatal("expected detach to keep TUI running")
	}
	if client.kills != 0 {
		t.Fatalf("expected detach to avoid killing terminal, got %d kill requests", client.kills)
	}
	if len(model.currentTab().Panes) != 1 {
		t.Fatalf("expected one viewport to remain, got %d", len(model.currentTab().Panes))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "○ "+orphanID) {
		t.Fatalf("expected detached terminal %q to appear as orphan in picker:\n%s", orphanID, view)
	}
}

func TestClosingLastViewportQuitsTUIWithoutKillingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if !model.quitting {
		t.Fatal("expected closing the last viewport to quit the TUI")
	}
	if _, ok := mustRunCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("expected quit command after closing the last viewport")
	}
	if client.kills != 0 {
		t.Fatalf("expected closing the last viewport to keep terminal alive, got %d kill requests", client.kills)
	}
}

func TestKillingTerminalClosesAllViewports(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected duplicated terminal to be visible in two viewports, got %d panes", len(model.currentTab().Panes))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if client.kills != 1 || len(client.killedIDs) != 1 || client.killedIDs[0] != "shared-001" {
		t.Fatalf("expected shared terminal kill request, got kills=%d ids=%v", client.kills, client.killedIDs)
	}
	if !model.quitting {
		t.Fatal("expected killing the shared terminal to close all viewports and quit")
	}
	if _, ok := mustRunCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("expected quit command after killing the only terminal")
	}
}

func TestRawPickerInputNavigatesAndKillsSelection(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "orphan-001", Name: "one", Command: []string{"sleep", "1"}, State: "running"},
			{ID: "orphan-002", Name: "two", Command: []string{"sleep", "2"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(rawInputMsg{data: []byte("orphan-001")})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(rawInputMsg{data: []byte{0x7f, '2', 0x0b}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if model.terminalPicker != nil {
		t.Fatal("expected picker to close after ctrl-k")
	}
	if len(client.killedIDs) != 1 || client.killedIDs[0] != "orphan-002" {
		t.Fatalf("expected ctrl-k to kill the selected terminal, got %v", client.killedIDs)
	}
	if cmd != nil {
		t.Fatal("expected killing an orphan from picker to keep TUI open")
	}
}

func TestInputEventPickerPasteAndEnterAttach(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(inputEventMsg{event: uv.PasteEvent{Content: "shared"}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyEnter}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if got := model.currentTab().Panes[model.currentTab().ActivePaneID].TerminalID; got != "shared-001" {
		t.Fatalf("expected picker enter to attach shared terminal, got %q", got)
	}
}

func TestInputEventPrefixXDetachesViewport(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	cmd := model.handlePrefixEvent(uv.KeyPressEvent{Code: '%', Text: "%"})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected split via input events, got %d panes", len(model.currentTab().Panes))
	}

	cmd = model.handlePrefixEvent(uv.KeyPressEvent{Code: 'x', Text: "x"})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if cmd != nil {
		t.Fatal("expected detach via input events to keep TUI open")
	}
	if got := len(model.currentTab().Panes); got != 1 {
		t.Fatalf("expected detach via input events to remove one viewport, got %d panes", got)
	}
}

func TestPrefixResizeAdjustsVerticalBoundary(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	tab := model.currentTab()
	activeID := tab.ActivePaneID
	before := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}

	after := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]
	if after.W <= before.W {
		t.Fatalf("expected active pane width to grow, before=%#v after=%#v", before, after)
	}
	if after.X >= before.X {
		t.Fatalf("expected active pane left edge to move left, before=%#v after=%#v", before, after)
	}
}

func TestPrefixResizeAdjustsHorizontalBoundary(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'"'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	tab := model.currentTab()
	activeID := tab.ActivePaneID
	before := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}

	after := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]
	if after.H <= before.H {
		t.Fatalf("expected active pane height to grow, before=%#v after=%#v", before, after)
	}
	if after.Y >= before.Y {
		t.Fatalf("expected active pane top edge to move up, before=%#v after=%#v", before, after)
	}
}

func TestPrefixSpaceCyclesPredefinedLayouts(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	for _, r := range []rune{'%', '"', '%'} {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
		_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		msg = mustRunCmd(t, cmd)
		_, cmd = model.Update(msg)
		if cmd != nil {
			_ = mustRunCmd(t, cmd)
		}
	}

	tab := model.currentTab()
	rootRect := Rect{X: 0, Y: 0, W: model.width, H: model.height - 2}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	rects := tab.Root.Rects(rootRect)
	for paneID, rect := range rects {
		if rect.W != rootRect.W {
			t.Fatalf("expected even-horizontal full width for %s, got %#v", paneID, rect)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	rects = tab.Root.Rects(rootRect)
	for paneID, rect := range rects {
		if rect.H != rootRect.H {
			t.Fatalf("expected even-vertical full height for %s, got %#v", paneID, rect)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	ids := tab.Root.LeafIDs()
	rects = tab.Root.Rects(rootRect)
	main := rects[ids[0]]
	if main.W <= rootRect.W/2 {
		t.Fatalf("expected main-horizontal first pane to be wide, got %#v", main)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	ids = tab.Root.LeafIDs()
	rects = tab.Root.Rects(rootRect)
	main = rects[ids[0]]
	if main.H <= rootRect.H/2 {
		t.Fatalf("expected main-vertical first pane to be tall, got %#v", main)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	rects = tab.Root.Rects(rootRect)
	for paneID, rect := range rects {
		if rect.W == rootRect.W || rect.H == rootRect.H {
			t.Fatalf("expected tiled layout to constrain both dimensions for %s, got %#v", paneID, rect)
		}
	}
}

func TestPrefixRenameTabCommitsNewName(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

	for _, r := range []rune("editor") {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := model.currentTab().Name; got != "editor" {
		t.Fatalf("expected renamed tab to be %q, got %q", "editor", got)
	}
}

func TestRawRenameTabCanBeCanceled(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	original := model.currentTab().Name

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

	_, cmd := model.Update(rawInputMsg{data: []byte("logs")})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(rawInputMsg{data: []byte{0x1b}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}

	if got := model.currentTab().Name; got != original {
		t.Fatalf("expected tab rename cancel to keep %q, got %q", original, got)
	}
}

func TestPaneTitleUsesCommandName(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/usr/bin/fish"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "fish") {
		t.Fatalf("expected pane title to include command name, got:\n%s", view)
	}
}

func TestModelResizeMessageResizesLocalVTerm(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	_, _ = model.Update(paneResizeMsg{channel: pane.Channel, cols: 90, rows: 33})

	screen := pane.VTerm.ScreenContent()
	if len(screen.Cells) != 33 {
		t.Fatalf("expected 33 rows, got %d", len(screen.Cells))
	}
	if len(screen.Cells) == 0 || len(screen.Cells[0]) != 90 {
		t.Fatalf("expected 90 cols, got %d", len(screen.Cells[0]))
	}
}

func TestPaneCellsUsesCacheUntilDirty(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	first := paneCells(pane)
	second := paneCells(pane)
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("expected cached pane cells")
	}
	if &first[0] != &second[0] {
		t.Fatal("expected second call to reuse cached lines")
	}

	_, _ = pane.VTerm.Write([]byte("changed"))
	pane.live = true
	pane.renderDirty = true
	third := paneCells(pane)
	if &third[0] == &second[0] {
		t.Fatal("expected dirty pane to rebuild cache")
	}
}

func TestOneDirtyPaneDoesNotInvalidateSiblingCaches(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	tab := model.currentTab()
	ids := tab.Root.LeafIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(ids))
	}

	left := tab.Panes[ids[0]]
	right := tab.Panes[ids[1]]
	if left == nil || right == nil {
		t.Fatal("expected both panes")
	}

	_, _ = left.VTerm.Write([]byte("left side"))
	left.live = true
	left.renderDirty = true
	_, _ = right.VTerm.Write([]byte("right side"))
	right.live = true
	right.renderDirty = true

	_ = model.View()
	leftCached := firstCellPtr(left.cellCache)
	rightCached := firstCellPtr(right.cellCache)

	_, _ = left.VTerm.Write([]byte("\r\nchanged"))
	left.live = true
	left.renderDirty = true

	_ = model.View()

	if got := firstCellPtr(left.cellCache); got == leftCached {
		t.Fatal("expected dirty pane cache to be rebuilt")
	}
	if got := firstCellPtr(right.cellCache); got != rightCached {
		t.Fatal("expected clean sibling pane cache to be reused")
	}
}

func TestPaneOutputWaitsForRenderTickWhenBatchingEnabled(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24
	model.renderBatching = true
	model.program = &tea.Program{}

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	initial := model.View()

	_, _ = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame: protocol.StreamFrame{
			Type:    protocol.TypeOutput,
			Payload: []byte("batched output"),
		},
	})

	if got := model.View(); got != initial {
		t.Fatal("expected view to stay cached before render tick")
	}

	_, _ = model.Update(renderTickMsg{})
	if got := xansi.Strip(model.View()); !strings.Contains(got, "batched output") {
		t.Fatalf("expected render tick to flush pending output, got:\n%s", got)
	}
}

func TestSyncLostRecoversPaneFromSnapshotAndKeepsStreaming(t *testing.T) {
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"pane-001": {
				TerminalID: "pane-001",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{
						{
							{Content: "r", Width: 1},
							{Content: "e", Width: 1},
							{Content: "s", Width: 1},
							{Content: "y", Width: 1},
							{Content: "n", Width: 1},
							{Content: "c", Width: 1},
						},
					},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 6, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("stale"))
	pane.live = true
	pane.renderDirty = true

	_, cmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(128)},
	})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "resync") {
		t.Fatalf("expected snapshot content after sync lost, got:\n%s", view)
	}
	if pane.syncLost {
		t.Fatal("expected syncLost flag to clear after recovery")
	}

	_, cmd = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("!")},
	})
	if cmd != nil {
		if msg := mustRunCmd(t, cmd); msg != nil {
			_, _ = model.Update(msg)
		}
	}
	_, _ = model.Update(renderTickMsg{})

	view = xansi.Strip(model.View())
	if !strings.Contains(view, "resync!") {
		t.Fatalf("expected streaming to continue from recovered snapshot, got:\n%s", view)
	}
}

func TestSyncLostWhileRecoveryInFlightDoesNotQueueDuplicateSnapshots(t *testing.T) {
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"pane-001": {
				TerminalID: "pane-001",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
				Cursor:     protocol.CursorState{Row: 0, Col: 2, Visible: true},
				Modes:      protocol.TerminalModes{AutoWrap: true},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	baselineCalls := client.snapshotCalls

	_, firstCmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	if firstCmd == nil {
		t.Fatal("expected first sync lost to request snapshot recovery")
	}

	_, secondCmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	if secondCmd != nil {
		t.Fatal("expected duplicate sync lost to avoid a second snapshot request while recovering")
	}
	if client.snapshotCalls != baselineCalls {
		t.Fatalf("expected snapshot command to be deferred until run, got %d calls", client.snapshotCalls-baselineCalls)
	}

	msg = mustRunCmd(t, firstCmd)
	_, _ = model.Update(msg)
	if client.snapshotCalls != baselineCalls+1 {
		t.Fatalf("expected exactly one snapshot request, got %d", client.snapshotCalls-baselineCalls)
	}
}

func TestSyncLostRecoveryFailureAllowsRetry(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	client.snapshotErr = errors.New("boom")
	_, cmd = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if pane.recovering {
		t.Fatal("expected failed recovery to clear recovering flag")
	}

	client.snapshotErr = nil
	client.snapshotByID = map[string]*protocol.Snapshot{
		"pane-001": {
			TerminalID: "pane-001",
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
			Cursor:     protocol.CursorState{Row: 0, Col: 2, Visible: true},
			Modes:      protocol.TerminalModes{AutoWrap: true},
		},
	}
	_, cmd = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	if cmd == nil {
		t.Fatal("expected retry sync lost to request snapshot again")
	}
}

func TestContinuousDirtyPaneEntersCatchingUpAndEventuallyRecovers(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.renderBatching = true
	model.program = &tea.Program{}
	model.width = 220

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	for i := 0; i < 30; i++ {
		_, cmd := model.Update(paneOutputMsg{
			paneID: pane.ID,
			frame:  protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("x")},
		})
		if cmd != nil {
			if next := mustRunCmd(t, cmd); next != nil {
				_, _ = model.Update(next)
			}
		}
		_, _ = model.Update(renderTickMsg{})
	}

	if !pane.catchingUp {
		t.Fatal("expected pane to enter catching-up mode after sustained dirty ticks")
	}
	if got := xansi.Strip(model.renderStatus()); !strings.Contains(got, "catching-up") {
		t.Fatalf("expected status to mention catching-up, got %q", got)
	}

	for i := 0; i < 5; i++ {
		pane.renderDirty = false
		_, _ = model.Update(renderTickMsg{})
	}

	if pane.catchingUp {
		t.Fatal("expected pane to leave catching-up mode after clean ticks")
	}
}

func TestFixedModeViewportRendersCroppedContentAroundCursor(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 11
	model.height = 8

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, resizeCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	if resizeCmd != nil {
		if next := mustRunCmd(t, resizeCmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "BCDEFGHI") {
		t.Fatalf("expected fixed viewport to crop around cursor, got:\n%s", view)
	}
	if strings.Contains(view, "0123456789ABCDEFGHIJ") {
		t.Fatalf("expected fixed viewport to crop instead of showing the full line, got:\n%s", view)
	}
}

func TestPinnedFixedViewportAllowsManualOffsetPan(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 16
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	pane.Offset = Point{X: 0, Y: 0}
	pane.renderDirty = true
	before := xansi.Strip(model.View())

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	after := xansi.Strip(model.View())

	if before == after {
		t.Fatal("expected manual pan to change rendered content")
	}
	if pane.Offset.X != 4 {
		t.Fatalf("expected offset X to move by 4, got %d", pane.Offset.X)
	}
}

func TestPinnedFixedViewportPanClampsToContentBounds(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 20
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	for i := 0; i < 8; i++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	}
	if pane.Offset.X != 2 {
		t.Fatalf("expected horizontal offset clamp at 2, got %d", pane.Offset.X)
	}

	for i := 0; i < 4; i++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	}
	if pane.Offset.Y != 0 {
		t.Fatalf("expected vertical offset clamp at 0, got %d", pane.Offset.Y)
	}
}

func TestUnpinnedFixedViewportResumesCursorFollow(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 20
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	pane.Offset = Point{X: 0, Y: 0}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	if pane.Pin {
		t.Fatal("expected pin to be disabled")
	}
	if pane.Offset.X != 2 {
		t.Fatalf("expected offset to snap back to cursor-followed view, got %d", pane.Offset.X)
	}
}

func TestViewportModeToggleBackToFitClearsFixedStateAndResizes(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 20
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	pane.Offset = Point{X: 3, Y: 2}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if pane.Mode != ViewportModeFit {
		t.Fatalf("expected fit mode, got %q", pane.Mode)
	}
	if pane.Pin {
		t.Fatal("expected pin cleared when returning to fit mode")
	}
	if pane.Offset != (Point{}) {
		t.Fatalf("expected offset reset, got %+v", pane.Offset)
	}
	if client.resizeCalls == 0 {
		t.Fatal("expected fit mode toggle to resize the PTY")
	}
}

func TestConsumePrefixInputHandlesCtrlPanKeys(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 16
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true
	pane.Mode = ViewportModeFixed
	pane.Pin = true
	pane.Offset = Point{X: 0, Y: 0}

	model.prefixActive = true
	model.rawPending = []byte{0x0c}
	consumed, cmd, ok := model.consumePrefixInput()
	if !ok || consumed != 1 || cmd != nil {
		t.Fatalf("expected ctrl+l prefix input to be consumed inline, got consumed=%d ok=%v cmd=%v", consumed, ok, cmd != nil)
	}
	if pane.Offset.X != 4 {
		t.Fatalf("expected ctrl+l pan to move offset to 4, got %d", pane.Offset.X)
	}

	model.prefixActive = true
	model.rawPending = []byte("\x1b[1;5D")
	consumed, cmd, ok = model.consumePrefixInput()
	if !ok || consumed != len("\x1b[1;5D") || cmd != nil {
		t.Fatalf("expected ctrl+left prefix sequence to be consumed, got consumed=%d ok=%v cmd=%v", consumed, ok, cmd != nil)
	}
	if pane.Offset.X != 0 {
		t.Fatalf("expected ctrl+left pan to clamp back to 0, got %d", pane.Offset.X)
	}
}

func TestFixedViewportDoesNotSendResizeToTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	resizesBefore := client.resizeCalls

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, cmd = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if got := client.resizeCalls; got != resizesBefore {
		t.Fatalf("expected fixed viewport resize to avoid PTY resize, got %d -> %d", resizesBefore, got)
	}
	if pane.Mode != ViewportModeFixed {
		t.Fatalf("expected viewport mode fixed, got %q", pane.Mode)
	}
}

func TestCatchingUpSkipsAlternateRenderTicks(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.renderBatching = true
	model.program = &tea.Program{}

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.catchingUp = true
	pane.renderDirty = true
	model.renderCache = "cached"
	model.renderDirty = false
	model.renderPending.Store(true)

	_, _ = model.Update(renderTickMsg{})
	if model.renderDirty {
		t.Fatal("expected first catching-up tick to skip rendering")
	}

	model.renderPending.Store(true)
	_, _ = model.Update(renderTickMsg{})
	if !model.renderDirty {
		t.Fatal("expected second catching-up tick to allow rendering")
	}
}

func TestComposedCanvasMarksOnlyChangedRowsDirty(t *testing.T) {
	canvas := newComposedCanvas(4, 2)
	initial := canvas.String()
	if initial == "" {
		t.Fatal("expected initial canvas output")
	}
	if canvas.rowDirty[0] || canvas.rowDirty[1] {
		t.Fatal("expected initial String call to clear dirty rows")
	}

	canvas.set(0, 0, blankDrawCell())
	if canvas.rowDirty[0] || canvas.rowDirty[1] {
		t.Fatal("expected writing the same cell to keep rows clean")
	}

	canvas.set(1, 1, drawCell{Content: "x", Width: 1})
	if canvas.rowDirty[0] {
		t.Fatal("expected untouched first row to stay clean")
	}
	if !canvas.rowDirty[1] {
		t.Fatal("expected changed second row to become dirty")
	}

	updated := canvas.String()
	if updated == initial {
		t.Fatal("expected canvas output to change after writing a new cell")
	}
	if canvas.rowDirty[0] || canvas.rowDirty[1] {
		t.Fatal("expected String call to clear dirty rows again")
	}
}

func TestTextLinesToGridHandlesUnicodeGraphemes(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "combining", text: "e\u0301"},
		{name: "emoji", text: "🙂"},
		{name: "emoji-zwj", text: "👩‍💻"},
		{name: "cjk", text: "你好"},
		{name: "hangul", text: "한글"},
		{name: "fullwidth-punct", text: "！"},
		{name: "mixed", text: "Ae\u0301🙂界"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := textLinesToGrid([]string{tc.text})[0]
			if got, want := rowToANSI(row), tc.text; got != want {
				t.Fatalf("expected row text %q, got %q", want, got)
			}
			if got, want := len(row), xansi.StringWidth(tc.text); got != want {
				t.Fatalf("expected row width %d, got %d", want, got)
			}
		})
	}
}

func TestTrimToWidthIsGraphemeAware(t *testing.T) {
	if got := trimToWidth("A👩‍💻B", 3); got != "A👩‍💻" {
		t.Fatalf("expected emoji cluster to stay intact, got %q", got)
	}
	if got := trimToWidth("e\u0301x", 1); got != "e\u0301" {
		t.Fatalf("expected combining cluster to stay intact, got %q", got)
	}
}

func TestCropDrawGridClipsWideCellsAtViewportEdge(t *testing.T) {
	row := stringToDrawCells("A好B", drawStyle{})
	grid := [][]drawCell{row}

	cropped := cropDrawGrid(grid, Point{X: 1, Y: 0}, 2, 1)
	if len(cropped) != 1 || len(cropped[0]) != 2 {
		t.Fatalf("expected 1x2 cropped grid, got %dx%d", len(cropped), len(cropped[0]))
	}
	if got := rowToANSI(cropped[0]); got != "好" {
		t.Fatalf("expected cropped row to keep only the full wide cell, got %q", got)
	}

	clipped := cropDrawGrid(grid, Point{X: 2, Y: 0}, 1, 1)
	if got := rowToANSI(clipped[0]); got != " " {
		t.Fatalf("expected clipped continuation cell to render blank, got %q", got)
	}
}

func mustRunCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

type fakeClient struct {
	next          int
	nextChannel   uint16
	inputs        [][]byte
	resizeCalls   int
	resizeCols    uint16
	resizeRows    uint16
	resizeChannel uint16
	kills         int
	killedIDs     []string
	attachedIDs   []string
	listResult    []protocol.TerminalInfo
	snapshotByID  map[string]*protocol.Snapshot
	snapshotCalls int
	snapshotErr   error
	terminalByID  map[string]protocol.TerminalInfo
	terminalOrder []string
}

func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	f.next++
	id := terminalID(f.next)
	info := protocol.TerminalInfo{ID: id, Name: name, Command: append([]string(nil), command...), Size: size, State: "running"}
	f.storeTerminal(info)
	return &protocol.CreateResult{TerminalID: id, State: "running"}, nil
}

func (f *fakeClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	f.nextChannel++
	f.attachedIDs = append(f.attachedIDs, terminalID)
	return &protocol.AttachResult{Mode: mode, Channel: f.nextChannel}, nil
}

func (f *fakeClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	f.snapshotCalls++
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	if f.snapshotByID != nil {
		if snap, ok := f.snapshotByID[terminalID]; ok {
			cp := *snap
			return &cp, nil
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
	}, nil
}

func (f *fakeClient) List(ctx context.Context) (*protocol.ListResult, error) {
	items := make([]protocol.TerminalInfo, 0, len(f.terminalOrder))
	for _, info := range f.listResult {
		f.storeTerminal(info)
	}
	for _, id := range f.terminalOrder {
		info, ok := f.terminalByID[id]
		if ok {
			items = append(items, info)
		}
	}
	return &protocol.ListResult{Terminals: items}, nil
}

func (f *fakeClient) Input(ctx context.Context, channel uint16, data []byte) error {
	f.inputs = append(f.inputs, append([]byte(nil), data...))
	return nil
}
func (f *fakeClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	f.resizeCalls++
	f.resizeChannel = channel
	f.resizeCols = cols
	f.resizeRows = rows
	return nil
}
func (f *fakeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}
func (f *fakeClient) Kill(ctx context.Context, terminalID string) error {
	f.kills++
	f.killedIDs = append(f.killedIDs, terminalID)
	delete(f.terminalByID, terminalID)
	for i, id := range f.terminalOrder {
		if id == terminalID {
			f.terminalOrder = append(f.terminalOrder[:i], f.terminalOrder[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeClient) storeTerminal(info protocol.TerminalInfo) {
	if f.terminalByID == nil {
		f.terminalByID = make(map[string]protocol.TerminalInfo)
	}
	if _, ok := f.terminalByID[info.ID]; !ok {
		f.terminalOrder = append(f.terminalOrder, info.ID)
	}
	if info.Size.Cols == 0 {
		info.Size.Cols = 80
	}
	if info.Size.Rows == 0 {
		info.Size.Rows = 24
	}
	if info.Command == nil {
		info.Command = []string{}
	}
	f.terminalByID[info.ID] = info
}

func terminalID(i int) string {
	return fmt.Sprintf("pane-%03d", i)
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func firstCellPtr(grid [][]drawCell) *drawCell {
	for _, row := range grid {
		if len(row) > 0 {
			return &row[0]
		}
	}
	return nil
}
