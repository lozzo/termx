package app

import (
	"io"
	"sync"

	xterm "github.com/charmbracelet/x/term"
)

type cursorSequenceWriter interface {
	SetCursorSequence(seq string)
	WriteControlSequence(seq string) error
}

type outputCursorWriter struct {
	out io.Writer
	tty xterm.File

	mu     sync.RWMutex
	cursor string
}

func newOutputCursorWriter(out io.Writer) *outputCursorWriter {
	if out == nil {
		return nil
	}
	writer := &outputCursorWriter{out: out}
	if tty, ok := out.(xterm.File); ok {
		writer.tty = tty
	}
	return writer
}

func (w *outputCursorWriter) SetCursorSequence(seq string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.cursor = seq
	w.mu.Unlock()
}

func (w *outputCursorWriter) WriteControlSequence(seq string) error {
	if w == nil || w.out == nil || seq == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := io.WriteString(w.out, seq)
	return err
}

func (w *outputCursorWriter) Write(p []byte) (int, error) {
	if w == nil || w.out == nil {
		return 0, nil
	}
	n, err := w.out.Write(p)
	if err != nil {
		return n, err
	}
	w.mu.RLock()
	cursor := w.cursor
	w.mu.RUnlock()
	if cursor == "" {
		return n, nil
	}
	if _, err := io.WriteString(w.out, cursor); err != nil {
		return n, err
	}
	return n, nil
}

func (w *outputCursorWriter) Read(p []byte) (int, error) {
	if w == nil || w.tty == nil {
		return 0, io.EOF
	}
	return w.tty.Read(p)
}

func (w *outputCursorWriter) Close() error {
	if w == nil || w.tty == nil {
		return nil
	}
	return w.tty.Close()
}

func (w *outputCursorWriter) Fd() uintptr {
	if w == nil || w.tty == nil {
		return 0
	}
	return w.tty.Fd()
}

var _ xterm.File = (*outputCursorWriter)(nil)
