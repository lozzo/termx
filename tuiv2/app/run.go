package app

import (
	"context"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/muesli/cancelreader"
	xterm "golang.org/x/term"
)

// Run creates a new Model with the given Config and starts the bubbletea
// program. stdin/stdout are wired via the provided readers/writers so that
// tests can inject fakes without touching os.Stdin / os.Stdout.
func Run(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
	return RunWithClient(cfg, nil, stdin, stdout)
}

func RunWithClient(cfg shared.Config, client bridge.Client, stdin io.Reader, stdout io.Writer) error {
	model := New(cfg, nil, runtime.New(client))
	opts := []tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(stdout),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	p := tea.NewProgram(model, opts...)
	model.SetSendFunc(p.Send)

	stopInput, restoreInput, err := startInputForwarder(p, stdin)
	if err != nil {
		return err
	}
	defer func() { _ = restoreInput() }()
	defer stopInput()

	if stdout != nil {
		_, _ = io.WriteString(stdout, xansi.RequestForegroundColor+xansi.RequestBackgroundColor+requestTerminalPaletteQueries())
	}

	_, err = p.Run()
	return err
}

func startInputForwarder(program *tea.Program, input io.Reader) (func(), func() error, error) {
	if input == nil {
		return func() {}, func() error { return nil }, nil
	}

	restore := func() error { return nil }
	if file, ok := input.(interface{ Fd() uintptr }); ok && xterm.IsTerminal(int(file.Fd())) {
		state, err := xterm.MakeRaw(int(file.Fd()))
		if err != nil {
			return nil, nil, err
		}
		restore = func() error {
			return xterm.Restore(int(file.Fd()), state)
		}
	}

	reader, err := cancelreader.NewReader(input)
	if err != nil {
		_ = restore()
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	eventc := make(chan uv.Event, 128)
	terminalReader := uv.NewTerminalReader(reader, os.Getenv("TERM"))
	go func() {
		defer close(done)
		streamDone := make(chan struct{})
		go func() {
			defer close(streamDone)
			_ = terminalReader.StreamEvents(ctx, eventc)
			close(eventc)
		}()
		for {
			select {
			case <-ctx.Done():
				<-streamDone
				return
			case event, ok := <-eventc:
				if !ok {
					<-streamDone
					return
				}
				switch event := event.(type) {
				case uv.ForegroundColorEvent:
					program.Send(hostDefaultColorsMsg{FG: event.Color})
				case uv.BackgroundColorEvent:
					program.Send(hostDefaultColorsMsg{BG: event.Color})
				case uv.UnknownOscEvent:
					if index, c, ok := parsePaletteColorEvent(string(event)); ok {
						program.Send(hostPaletteColorMsg{Index: index, Color: c})
					}
				case uv.PasteEvent:
					program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(event.Content), Paste: true})
				case uv.KeyPressEvent:
					if msg, ok := uvKeyToTeaKeyMsg(event); ok {
						program.Send(msg)
					}
				case uv.MouseClickEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionPress); ok {
						program.Send(msg)
					}
				case uv.MouseReleaseEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionRelease); ok {
						program.Send(msg)
					}
				case uv.MouseMotionEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionMotion); ok {
						program.Send(msg)
					}
				case uv.MouseWheelEvent:
					if msg, ok := uvMouseEventToTeaMouseMsg(event, tea.MouseActionPress); ok {
						program.Send(msg)
					}
				}
			}
		}
	}()

	stop := func() {
		cancel()
		if reader.Cancel() {
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
			}
		}
		_ = reader.Close()
	}

	return stop, restore, nil
}

func requestTerminalPaletteQueries() string {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b]4;")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(";?\x07")
	}
	return b.String()
}

func parsePaletteColorEvent(raw string) (int, color.Color, bool) {
	if raw == "" {
		return 0, nil, false
	}
	raw = strings.TrimPrefix(raw, "\x1b]")
	raw = strings.TrimSuffix(raw, "\x07")
	raw = strings.TrimSuffix(raw, "\x1b\\")
	parts := strings.Split(raw, ";")
	if len(parts) != 3 || parts[0] != "4" {
		return 0, nil, false
	}
	index, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || index < 0 || index > 255 {
		return 0, nil, false
	}
	c := xansi.XParseColor(strings.TrimSpace(parts[2]))
	if c == nil {
		return 0, nil, false
	}
	return index, c, true
}

func uvKeyToTeaKeyMsg(event uv.KeyPressEvent) (tea.KeyMsg, bool) {
	k := event.Key()
	msg := tea.KeyMsg{Alt: k.Mod.Contains(uv.ModAlt)}

	if k.Mod.Contains(uv.ModCtrl) {
		switch {
		case k.Code >= 'a' && k.Code <= 'z':
			msg.Type = tea.KeyType(int(tea.KeyCtrlA) + int(k.Code-'a'))
			return msg, true
		case k.Code >= 'A' && k.Code <= 'Z':
			msg.Type = tea.KeyType(int(tea.KeyCtrlA) + int(unicode.ToLower(k.Code)-'a'))
			return msg, true
		case k.Code == '\\':
			msg.Type = tea.KeyCtrlBackslash
			return msg, true
		case k.Code == ']':
			msg.Type = tea.KeyCtrlCloseBracket
			return msg, true
		case k.Code == '^':
			msg.Type = tea.KeyCtrlCaret
			return msg, true
		case k.Code == '_':
			msg.Type = tea.KeyCtrlUnderscore
			return msg, true
		case k.Code == ' ' || k.Code == '@':
			msg.Type = tea.KeyCtrlAt
			return msg, true
		case k.Code == '?':
			msg.Type = tea.KeyCtrlQuestionMark
			return msg, true
		}
	}

	switch k.Code {
	case uv.KeyUp:
		msg.Type = tea.KeyUp
	case uv.KeyDown:
		msg.Type = tea.KeyDown
	case uv.KeyRight:
		msg.Type = tea.KeyRight
	case uv.KeyLeft:
		msg.Type = tea.KeyLeft
	case uv.KeyTab:
		msg.Type = tea.KeyTab
	case uv.KeyEnter, uv.KeyKpEnter:
		msg.Type = tea.KeyEnter
	case uv.KeyEscape:
		msg.Type = tea.KeyEsc
	case uv.KeyBackspace:
		msg.Type = tea.KeyBackspace
	case uv.KeyDelete:
		msg.Type = tea.KeyDelete
	case uv.KeyInsert:
		msg.Type = tea.KeyInsert
	case uv.KeyHome:
		msg.Type = tea.KeyHome
	case uv.KeyEnd:
		msg.Type = tea.KeyEnd
	case uv.KeyPgUp:
		msg.Type = tea.KeyPgUp
	case uv.KeyPgDown:
		msg.Type = tea.KeyPgDown
	case uv.KeySpace:
		msg.Type = tea.KeySpace
	default:
		if k.Text != "" {
			msg.Type = tea.KeyRunes
			msg.Runes = []rune(k.Text)
			return msg, true
		}
		if unicode.IsPrint(k.Code) {
			msg.Type = tea.KeyRunes
			msg.Runes = []rune{rune(k.Code)}
			return msg, true
		}
		return tea.KeyMsg{}, false
	}
	return msg, true
}

func uvMouseEventToTeaMouseMsg(event uv.MouseEvent, action tea.MouseAction) (tea.MouseMsg, bool) {
	mouse := event.Mouse()
	button, ok := uvMouseButtonToTeaMouseButton(mouse.Button)
	if !ok {
		return tea.MouseMsg{}, false
	}
	return tea.MouseMsg{
		X:      mouse.X,
		Y:      mouse.Y,
		Shift:  mouse.Mod.Contains(uv.ModShift),
		Alt:    mouse.Mod.Contains(uv.ModAlt),
		Ctrl:   mouse.Mod.Contains(uv.ModCtrl),
		Action: action,
		Button: button,
	}, true
}

func uvMouseButtonToTeaMouseButton(button uv.MouseButton) (tea.MouseButton, bool) {
	switch button {
	case uv.MouseNone:
		return tea.MouseButtonNone, true
	case uv.MouseLeft:
		return tea.MouseButtonLeft, true
	case uv.MouseMiddle:
		return tea.MouseButtonMiddle, true
	case uv.MouseRight:
		return tea.MouseButtonRight, true
	case uv.MouseWheelUp:
		return tea.MouseButtonWheelUp, true
	case uv.MouseWheelDown:
		return tea.MouseButtonWheelDown, true
	case uv.MouseWheelLeft:
		return tea.MouseButtonWheelLeft, true
	case uv.MouseWheelRight:
		return tea.MouseButtonWheelRight, true
	case uv.MouseBackward:
		return tea.MouseButtonBackward, true
	case uv.MouseForward:
		return tea.MouseButtonForward, true
	case uv.MouseButton10:
		return tea.MouseButton10, true
	case uv.MouseButton11:
		return tea.MouseButton11, true
	default:
		return tea.MouseButtonNone, false
	}
}
