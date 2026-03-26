package tui

import (
	"context"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	charmvt "github.com/charmbracelet/x/vt"
	localvterm "github.com/lozzow/termx/vterm"
	"github.com/muesli/cancelreader"
	xterm "golang.org/x/term"
)

func nextTabName(tabs []*Tab) string {
	return itoa(len(tabs) + 1)
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
				program.Send(inputEventMsg{event: event})
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

func encodeKeyForPane(pane *Pane, event uv.KeyPressEvent) []byte {
	if pane == nil {
		return nil
	}
	if data, ok := encodePrintableKey(event); ok {
		return data
	}
	emu := charmvt.NewSafeEmulator(1, 1)
	applyPaneModesToEncoder(emu, pane)
	return withEncoderBytes(emu, func() {
		emu.SendKey(event)
	})
}

func encodePrintableKey(event uv.KeyPressEvent) ([]byte, bool) {
	if event.Text == "" {
		return nil, false
	}
	if event.Mod&(uv.ModCtrl|uv.ModMeta|uv.ModHyper|uv.ModSuper) != 0 {
		return nil, false
	}

	size := len(event.Text)
	if event.Mod&uv.ModAlt != 0 {
		size++
	}
	data := make([]byte, 0, size)
	if event.Mod&uv.ModAlt != 0 {
		data = append(data, 0x1b)
	}
	data = append(data, event.Text...)
	return data, true
}

func encodePasteForPane(pane *Pane, text string) []byte {
	if pane == nil || text == "" {
		return nil
	}
	emu := charmvt.NewSafeEmulator(1, 1)
	applyPaneModesToEncoder(emu, pane)
	return withEncoderBytes(emu, func() {
		emu.Paste(text)
	})
}

func applyPaneModesToEncoder(emu *charmvt.SafeEmulator, pane *Pane) {
	modes := localModes(pane)
	if modes.ApplicationCursor {
		_, _ = emu.Write([]byte("\x1b[?1h"))
	}
	if modes.BracketedPaste {
		_, _ = emu.Write([]byte("\x1b[?2004h"))
	}
}

func localModes(pane *Pane) localvterm.TerminalModes {
	if pane == nil {
		return localvterm.TerminalModes{}
	}
	if pane.live && pane.VTerm != nil {
		return pane.VTerm.Modes()
	}
	if pane.Snapshot != nil {
		return localvterm.TerminalModes{
			AlternateScreen:   pane.Snapshot.Modes.AlternateScreen,
			MouseTracking:     pane.Snapshot.Modes.MouseTracking,
			BracketedPaste:    pane.Snapshot.Modes.BracketedPaste,
			ApplicationCursor: pane.Snapshot.Modes.ApplicationCursor,
			AutoWrap:          pane.Snapshot.Modes.AutoWrap,
		}
	}
	return localvterm.TerminalModes{}
}

func withEncoderBytes(emu *charmvt.SafeEmulator, fn func()) []byte {
	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(emu)
		done <- data
	}()
	fn()
	_ = emu.Close()
	data := <-done
	if len(data) == 0 {
		return nil
	}
	return copyBytes(data)
}
