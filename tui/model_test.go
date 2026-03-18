package tui

import (
	"context"
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

func TestClosingLastPaneQuitsTUI(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	quitMsg := mustRunCmd(t, cmd)

	_, cmd = model.Update(quitMsg)
	if !model.quitting {
		t.Fatal("expected closing the last pane to quit the TUI")
	}
	if _, ok := mustRunCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("expected quit command after closing the last pane")
	}
	if client.kills != 1 {
		t.Fatalf("expected 1 kill request, got %d", client.kills)
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

func mustRunCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

type fakeClient struct {
	next   int
	inputs [][]byte
	kills  int
}

func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	f.next++
	return &protocol.CreateResult{TerminalID: terminalID(f.next), State: "running"}, nil
}

func (f *fakeClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return &protocol.AttachResult{Mode: mode, Channel: uint16(f.next)}, nil
}

func (f *fakeClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
	}, nil
}

func (f *fakeClient) Input(ctx context.Context, channel uint16, data []byte) error {
	f.inputs = append(f.inputs, append([]byte(nil), data...))
	return nil
}
func (f *fakeClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return nil
}
func (f *fakeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}
func (f *fakeClient) Kill(ctx context.Context, terminalID string) error {
	f.kills++
	return nil
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
