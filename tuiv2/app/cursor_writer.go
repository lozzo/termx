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

type frameSequenceWriter interface {
	WriteFrame(frame, cursor string) error
}

type outputCursorWriter struct {
	out io.Writer
	tty xterm.File

	mu         sync.Mutex
	cursor     string
	afterWrite []string

	bubbleTeaRestore string
	cursorProjected  bool

	directAltScreen      bool
	directMouseCell      bool
	directBracketedPaste bool
}

var (
	synchronizedOutputBegin = xansi.DECSET(xansi.ModeSynchronizedOutput)
	synchronizedOutputEnd   = xansi.DECRST(xansi.ModeSynchronizedOutput)
	trailingControlSuffixRE = regexp.MustCompile(`(?:\r|\x1b\[[0-9;?]*[ -/]*[@-~])+$`)
)

const hideHostCursorSequence = "\x1b[?25l"

func (w *outputCursorWriter) enterDirectTerminal() error {
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.directAltScreen {
		return nil
	}
	if _, err := io.WriteString(w.out, xansi.HideCursor); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EnableAltScreenBuffer); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EraseEntireDisplay+xansi.MoveCursorOrigin); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.HideCursor); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EnableBracketedPaste); err != nil {
		return err
	}
	if _, err := io.WriteString(w.out, xansi.EnableMouseCellMotion+xansi.EnableMouseSgrExt); err != nil {
		return err
	}
	w.directAltScreen = true
	w.directMouseCell = true
	w.directBracketedPaste = true
	return nil
}

func (w *outputCursorWriter) exitDirectTerminal() error {
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.directBracketedPaste {
		if _, err := io.WriteString(w.out, xansi.DisableBracketedPaste); err != nil {
			return err
		}
		w.directBracketedPaste = false
	}
	if _, err := io.WriteString(w.out, xansi.ShowCursor); err != nil {
		return err
	}
	if w.directMouseCell {
		if _, err := io.WriteString(w.out, xansi.DisableMouseCellMotion+xansi.DisableMouseSgrExt); err != nil {
			return err
		}
		w.directMouseCell = false
	}
	if w.directAltScreen {
		if _, err := io.WriteString(w.out, xansi.DisableAltScreenBuffer); err != nil {
			return err
		}
		w.directAltScreen = false
	}
	return nil
}

func (w *outputCursorWriter) WriteFrame(frame, cursor string) error {
	if w == nil || w.out == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	syncOutput := w.tty != nil
	if syncOutput {
		if _, err := io.WriteString(w.out, synchronizedOutputBegin); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w.out, hideHostCursorSequence); err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return err
	}
	if _, err := io.WriteString(w.out, xansi.MoveCursorOrigin); err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return err
	}
	if _, err := io.WriteString(w.out, frame); err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return err
	}
	afterWrite := append([]string(nil), w.afterWrite...)
	w.afterWrite = nil
	for _, seq := range afterWrite {
		if seq == "" {
			continue
		}
		if _, err := io.WriteString(w.out, seq); err != nil {
			if syncOutput {
				_, _ = io.WriteString(w.out, synchronizedOutputEnd)
			}
			return err
		}
	}
	if cursor == "" {
		cursor = hideHostCursorSequence
	}
	if _, err := io.WriteString(w.out, cursor); err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return err
	}
	w.bubbleTeaRestore = ""
	w.cursorProjected = false
	if syncOutput {
		if _, err := io.WriteString(w.out, synchronizedOutputEnd); err != nil {
			return err
		}
	}
	return nil
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
	cursor := w.cursor
	payload := string(p)
	if cursor != "" {
		payload = stripEmbeddedCursorSequence(payload, cursor)
	}
	if cursor != "" {
		if _, err := io.WriteString(w.out, hideHostCursorSequence); err != nil {
			if syncOutput {
				_, _ = io.WriteString(w.out, synchronizedOutputEnd)
			}
			return 0, err
		}
	}
	n, err := io.WriteString(w.out, payload)
	if err != nil {
		if syncOutput {
			_, _ = io.WriteString(w.out, synchronizedOutputEnd)
		}
		return n, err
	}
	afterWrite := append([]string(nil), w.afterWrite...)
	w.afterWrite = nil
	if frameLike {
		w.bubbleTeaRestore = bubbleTeaRestoreSequence([]byte(payload))
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
	// 中文说明：tmux/zellij 都会在一次输出结束后把真实终端光标留在 pane/
	// 输入框的最终位置。这里即使 Bubble Tea 这次只写了控制序列，也要把 host
	// cursor 重新投回去，否则输入法候选框会跟着框架内部的临时光标跑偏。
	if _, err := io.WriteString(w.out, cursor); err != nil {
		return n, err
	}
	w.cursorProjected = w.bubbleTeaRestore != ""
	if syncOutput {
		if _, err := io.WriteString(w.out, synchronizedOutputEnd); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
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

func stripEmbeddedCursorSequence(payload, cursor string) string {
	if payload == "" || cursor == "" {
		return payload
	}
	trailing := bubbleTeaRestoreSequence([]byte(payload))
	body := strings.TrimSuffix(payload, trailing)
	if !strings.HasSuffix(body, cursor) {
		return payload
	}
	return strings.TrimSuffix(body, cursor) + trailing
}
