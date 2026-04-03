package app

import (
	"context"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/cancelreader"
	xterm "golang.org/x/term"
)

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
