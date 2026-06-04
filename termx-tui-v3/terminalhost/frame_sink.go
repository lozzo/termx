package terminalhost

import (
	"io"
	"strings"
	"sync"

	"github.com/lozzow/termx/termx-tui-v3/render"
)

const (
	cursorHome  = "\x1b[H"
	clearScreen = "\x1b[2J"
	clearLine   = "\x1b[2K"
)

// FrameSink 把 Renderer 生成的 frame 直接写到宿主输出。
type FrameSink struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewFrameSink(writer io.Writer) *FrameSink {
	return &FrameSink{writer: writer}
}

func (sink *FrameSink) WriteFrame(frame render.Frame) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var builder strings.Builder
	builder.WriteString(cursorHome)
	builder.WriteString(clearScreen)
	lines := frame.ANSILines
	if len(lines) == 0 {
		lines = frame.Lines
	}
	for i, line := range lines {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(clearLine)
		builder.WriteString(line)
		builder.WriteString(render.ANSIReset)
	}
	builder.WriteString(render.ANSIReset)
	_, err := io.WriteString(sink.writer, builder.String())
	return err
}
