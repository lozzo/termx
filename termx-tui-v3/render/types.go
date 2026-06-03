package render

// Frame 是 Renderer 输出给 FrameSink 的不可变帧契约。
type Frame struct {
	Lines []string
}

// Clone 返回 detached frame，防止测试或 host 共享修改 frame lines。
func (frame Frame) Clone() Frame {
	if len(frame.Lines) == 0 {
		return Frame{}
	}
	lines := make([]string, len(frame.Lines))
	copy(lines, frame.Lines)
	return Frame{Lines: lines}
}

// FrameSink 把渲染帧写入 host、recorder 或 test sink。
type FrameSink interface {
	WriteFrame(Frame) error
}
