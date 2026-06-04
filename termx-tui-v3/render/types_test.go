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

func TestFrameFromRenderResultUsesSingleResultPath(t *testing.T) {
	result := RenderResult{
		Content:    []Line{NewLine("hello"), NewLine("世界")},
		HitRegions: []HitRegion{{Kind: HitRegionStatus, Rect: Rect{W: 5, H: 1}}},
		Metadata:   RenderMetadata{Width: 5, Height: 2},
	}

	frame := FrameFromRenderResult(result)
	if len(frame.Lines) != 2 || frame.Lines[0] != "hello" || frame.Lines[1] != "世界" {
		t.Fatalf("unexpected frame %#v", frame)
	}
}

func TestFrameCloneDetachesLines(t *testing.T) {
	frame := Frame{Lines: []string{"one"}}
	cloned := frame.Clone()
	frame.Lines[0] = "mutated"
	if cloned.Lines[0] != "one" {
		t.Fatalf("expected detached clone, got %#v", cloned)
	}
}
