package render

// Frame is the renderer output contract consumed by FrameSink implementations.
type Frame struct {
	Lines []string
}

// FrameSink writes rendered frames to a host, recorder, or test sink.
type FrameSink interface {
	WriteFrame(Frame) error
}
