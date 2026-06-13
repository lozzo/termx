package terminalhost

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-tui-v3/render"
)

const (
	cursorHome              = "\x1b[H"
	clearScreen             = "\x1b[2J"
	clearLine               = "\x1b[2K"
	scrollUpOne             = "\x1b[S"
	scrollDownOne           = "\x1b[T"
	resetScrollRegion       = "\x1b[r"
	cursorShapeBlock        = "\x1b[2 q"
	cursorShapeBar          = "\x1b[6 q"
	synchronizedOutputBegin = "\x1b[?2026h"
	synchronizedOutputEnd   = "\x1b[?2026l"
)

// FrameSink 把 Renderer 生成的 frame 直接写到宿主输出。
type FrameSink struct {
	mu           sync.Mutex
	writer       io.Writer
	lastLines    []string
	lastWidth    int
	lastHeight   int
	lastCursor   string
	hasLastFrame bool
}

func NewFrameSink(writer io.Writer) *FrameSink {
	return &FrameSink{writer: writer}
}

func (sink *FrameSink) NeedsCompleteFrame() bool {
	return false
}

func (sink *FrameSink) WriteFrame(frame render.Frame) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	lines := frame.ANSILines
	if len(lines) == 0 {
		lines = frame.Lines
	}
	width := frame.Metadata.Width
	height := frame.Metadata.Height
	if height == 0 {
		height = len(lines)
	}
	fullRepaint := !sink.hasLastFrame || width != sink.lastWidth || height != sink.lastHeight || len(lines) != len(sink.lastLines)
	cursorSequence := frameCursorSequence(frame)
	dirtyRows := fullRepaint
	if !dirtyRows {
		for i, line := range lines {
			if sink.lastLines[i] != line {
				dirtyRows = true
				break
			}
		}
	}
	if !dirtyRows && sink.lastCursor == cursorSequence {
		return nil
	}

	var builder strings.Builder
	builder.WriteString(synchronizedOutputBegin)
	builder.WriteString(hideCursor)
	if !fullRepaint && sink.writeScrollShift(&builder, lines) {
		// 一行滚动时让终端移动已有行，只补新露出的一行；避免滚轮每次重写整块 copy history。
	} else {
		if fullRepaint {
			builder.WriteString(cursorHome)
			builder.WriteString(clearScreen)
		}
		for i, line := range lines {
			if !fullRepaint && i < len(sink.lastLines) && sink.lastLines[i] == line {
				continue
			}
			// 满宽 frame 写到最后一列后继续输出换行，会在部分终端触发额外自动换行；
			// 每行绝对定位可以保持 pane 竖向边框连续。
			builder.WriteString(cursorPosition(i+1, 1))
			builder.WriteString(clearLine)
			builder.WriteString(line)
			builder.WriteString(render.ANSIReset)
		}
	}
	builder.WriteString(render.ANSIReset)
	builder.WriteString(cursorSequence)
	builder.WriteString(synchronizedOutputEnd)
	_, err := io.WriteString(sink.writer, builder.String())
	if err != nil {
		return err
	}
	sink.lastLines = append(sink.lastLines[:0], lines...)
	sink.lastWidth = width
	sink.lastHeight = height
	sink.lastCursor = cursorSequence
	sink.hasLastFrame = true
	return nil
}

func (sink *FrameSink) writeScrollShift(builder *strings.Builder, lines []string) bool {
	if len(lines) != len(sink.lastLines) || len(lines) < 3 {
		return false
	}
	if start, end, ok := shiftedUpWindow(sink.lastLines, lines); ok {
		builder.WriteString(scrollRegion(start+1, end+1))
		builder.WriteString(cursorPosition(end+1, 1))
		builder.WriteString(scrollUpOne)
		builder.WriteString(resetScrollRegion)
		builder.WriteString(cursorPosition(end+1, 1))
		builder.WriteString(clearLine)
		builder.WriteString(lines[end])
		builder.WriteString(render.ANSIReset)
		return true
	}
	if start, end, ok := shiftedDownWindow(sink.lastLines, lines); ok {
		builder.WriteString(scrollRegion(start+1, end+1))
		builder.WriteString(cursorPosition(start+1, 1))
		builder.WriteString(scrollDownOne)
		builder.WriteString(resetScrollRegion)
		builder.WriteString(cursorPosition(start+1, 1))
		builder.WriteString(clearLine)
		builder.WriteString(lines[start])
		builder.WriteString(render.ANSIReset)
		return true
	}
	return false
}

func shiftedUpWindow(previous []string, next []string) (int, int, bool) {
	start := 0
	for start < len(previous) && previous[start] == next[start] {
		start++
	}
	end := len(previous) - 1
	for end >= start && previous[end] == next[end] {
		end--
	}
	if end-start < 2 {
		return 0, 0, false
	}
	for i := start; i < end; i++ {
		if previous[i+1] != next[i] {
			return 0, 0, false
		}
	}
	return start, end, true
}

func shiftedDownWindow(previous []string, next []string) (int, int, bool) {
	start := 0
	for start < len(previous) && previous[start] == next[start] {
		start++
	}
	end := len(previous) - 1
	for end >= start && previous[end] == next[end] {
		end--
	}
	if end-start < 2 {
		return 0, 0, false
	}
	for i := start + 1; i <= end; i++ {
		if previous[i-1] != next[i] {
			return 0, 0, false
		}
	}
	return start, end, true
}

func frameCursorSequence(frame render.Frame) string {
	if (!frame.Cursor.Visible && !frame.Cursor.Anchor) || frame.CursorRect.W <= 0 || frame.CursorRect.H <= 0 {
		return hideCursor
	}
	// 中文说明：参考 tuiv2 的最终光标复投经验。v3 已经有全局 CursorRect，
	// 因此只在真实 virtual cursor 可见时显示宿主光标；anchor-only 仍隐藏但停靠。
	sequence := cursorShapeSequence(frame.Cursor.Shape) + cursorPosition(frame.CursorRect.Y+1, frame.CursorRect.X+1)
	if frame.Cursor.Visible {
		return sequence + showCursor
	}
	return hideCursor + sequence
}

func cursorShapeSequence(shape render.CursorShape) string {
	switch shape {
	case render.CursorShapeBar:
		return cursorShapeBar
	default:
		return cursorShapeBlock
	}
}

func cursorPosition(row int, col int) string {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	return fmt.Sprintf("\x1b[%d;%dH", row, col)
}

func scrollRegion(top int, bottom int) string {
	if top < 1 {
		top = 1
	}
	if bottom < top {
		bottom = top
	}
	return fmt.Sprintf("\x1b[%d;%dr", top, bottom)
}
