package render

import "testing"

type recordingSink struct {
	frames []Frame
}

func (s *recordingSink) WriteFrame(frame Frame) error {
	s.frames = append(s.frames, frame)
	return nil
}

func TestFrameSinkContract(t *testing.T) {
	sink := &recordingSink{}
	if err := sink.WriteFrame(Frame{Lines: []string{"ok"}}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	if len(sink.frames) != 1 || sink.frames[0].Lines[0] != "ok" {
		t.Fatalf("unexpected frames %#v", sink.frames)
	}
}
