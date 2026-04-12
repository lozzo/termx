package app

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	xterm "github.com/charmbracelet/x/term"
)

func (w *outputCursorWriter) fitFrameToTTY(frame string) string {
	if w == nil || w.tty == nil || frame == "" {
		return frame
	}
	width, _, err := xterm.GetSize(w.tty.Fd())
	if err != nil || width <= 0 {
		return frame
	}
	// 如果宽度未变（coordinator 已经按该宽度渲染），跳过逐行截断
	if width == w.lastTTYWidth {
		return frame
	}
	w.lastTTYWidth = width
	return truncateFrameToWidth(frame, width)
}

func (w *outputCursorWriter) fitLinesToTTY(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	if w == nil || w.tty == nil {
		return lines
	}
	width, _, err := xterm.GetSize(w.tty.Fd())
	if err != nil || width <= 0 {
		return lines
	}
	if width == w.lastTTYWidth {
		return lines
	}
	out := make([]string, len(lines))
	w.lastTTYWidth = width
	for i := range lines {
		out[i] = xansi.Truncate(lines[i], width, "")
	}
	return out
}

func stripTrailingEraseLineRight(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	var out []string
	for i := range lines {
		line := lines[i]
		if strings.HasSuffix(line, xansi.EraseLineRight) {
			if out == nil {
				out = append([]string(nil), lines...)
			}
			out[i] = strings.TrimSuffix(line, xansi.EraseLineRight)
		}
	}
	if out == nil {
		return lines
	}
	return out
}

func truncateFrameToWidth(frame string, width int) string {
	if frame == "" || width <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i := range lines {
		lines[i] = xansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func normalizeFrameForTTY(frame string) string {
	if frame == "" {
		return frame
	}
	return strings.ReplaceAll(frame, "\n", "\r\n")
}

func normalizedFrameLen(frame string) int {
	if frame == "" {
		return 0
	}
	return len(frame) + strings.Count(frame, "\n")
}

func normalizedLinesLen(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	total := len(lines) - 1
	for _, line := range lines {
		total += len(line)
	}
	return total
}

func writeNormalizedFrame(out *strings.Builder, frame string) {
	if out == nil || frame == "" {
		return
	}
	start := 0
	for i := 0; i < len(frame); i++ {
		if frame[i] != '\n' {
			continue
		}
		if start < i {
			out.WriteString(frame[start:i])
		}
		out.WriteString("\r\n")
		start = i + 1
	}
	if start < len(frame) {
		out.WriteString(frame[start:])
	}
}

func frameLikeWritePayload(p []byte) bool {
	return strings.Trim(xansi.Strip(string(p)), "\r\n") != ""
}

func bubbleTeaRestoreSequence(p []byte) string {
	if len(p) == 0 {
		return ""
	}
	i := len(p)
	for i > 0 {
		if p[i-1] == '\r' {
			i--
			continue
		}
		start := trailingCSISequenceStart(p[:i])
		if start < 0 {
			break
		}
		i = start
	}
	return string(p[i:])
}

func trailingCSISequenceStart(p []byte) int {
	if len(p) < 3 {
		return -1
	}
	final := p[len(p)-1]
	if final < '@' || final > '~' {
		return -1
	}
	i := len(p) - 1
	for i > 0 {
		b := p[i-1]
		if b < ' ' || b > '/' {
			break
		}
		i--
	}
	for i > 0 {
		b := p[i-1]
		if (b >= '0' && b <= '9') || b == ';' || b == '?' {
			i--
			continue
		}
		break
	}
	if i >= 2 && p[i-2] == '\x1b' && p[i-1] == '[' {
		return i - 2
	}
	return -1
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
