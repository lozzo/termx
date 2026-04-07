package app

import (
	"io"
	"regexp"
	"strings"
	"sync"

	xansi "github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"
)

type cursorSequenceWriter interface {
	SetCursorSequence(seq string)
	WriteControlSequence(seq string) error
	// QueueControlSequenceAfterWrite defers a control sequence until after the
	// next Bubble Tea frame write. Startup probes rely on this so the host sees
	// the probe only after alt-screen entry and the first frame are live.
	QueueControlSequenceAfterWrite(seq string)
}

type outputCursorWriter struct {
	out io.Writer
	tty xterm.File

	mu         sync.Mutex
	cursor     string
	afterWrite []string

	bubbleTeaRestore string
	cursorProjected  bool
}

var (
	synchronizedOutputBegin = xansi.DECSET(xansi.ModeSynchronizedOutput)
	synchronizedOutputEnd   = xansi.DECRST(xansi.ModeSynchronizedOutput)
	trailingControlSuffixRE = regexp.MustCompile(`(?:\r|\x1b\[[0-9;?]*[ -/]*[@-~])+$`)
)

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

func (w *outputCursorWriter) QueueControlSequenceAfterWrite(seq string) {
	if w == nil || seq == "" {
		return
	}
	w.mu.Lock()
	w.afterWrite = append(w.afterWrite, seq)
	w.mu.Unlock()
}

func (w *outputCursorWriter) Write(p []byte) (int, error) {
	if w == nil || w.out == nil {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	frameLike := frameLikeWritePayload(p)
	syncOutput := w.tty != nil
	if syncOutput {
		if _, err := io.WriteString(w.out, synchronizedOutputBegin); err != nil {
			return 0, err
		}
	}
	if w.cursorProjected && w.bubbleTeaRestore != "" {
		if _, err := io.WriteString(w.out, w.bubbleTeaRestore); err != nil {
			if syncOutput {
				_, _ = io.WriteString(w.out, synchronizedOutputEnd)
			}
			return 0, err
		}
		w.cursorProjected = false
	}
	n, err := w.out.Write(p)
	if err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return n, err
	}
	cursor := w.cursor
	afterWrite := append([]string(nil), w.afterWrite...)
	w.afterWrite = nil
	if frameLike {
		w.bubbleTeaRestore = bubbleTeaRestoreSequence(p)
	}
	for _, seq := range afterWrite {
		if seq == "" {
			continue
		}
		if _, err := io.WriteString(w.out, seq); err != nil {
			return n, err
		}
	}
	if cursor == "" {
		if syncOutput {
			if _, err := io.WriteString(w.out, synchronizedOutputEnd); err != nil {
				return n, err
			}
		}
		return n, nil
	}
	// Only append the projected host cursor after frame-like writes. Bubble Tea
	// may emit setup/control writes before the actual frame bytes; injecting the
	// cursor after those intermediate writes can reposition the host cursor in
	// the middle of a redraw and corrupt the visible layout.
	if frameLike {
		if _, err := io.WriteString(w.out, cursor); err != nil {
			return n, err
		}
		w.cursorProjected = true
	}
	if syncOutput {
		if _, err := io.WriteString(w.out, synchronizedOutputEnd); err != nil {
			return n, err
		}
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

func frameLikeWritePayload(p []byte) bool {
	return strings.Trim(xansi.Strip(string(p)), "\r\n") != ""
}

func bubbleTeaRestoreSequence(p []byte) string {
	if len(p) == 0 {
		return ""
	}
	return trailingControlSuffixRE.FindString(string(p))
}
