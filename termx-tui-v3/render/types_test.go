package render

import (
	"strings"
	"testing"
)

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

func TestFrameFromRenderResultPreservesStyledANSIAndMetadata(t *testing.T) {
	result := RenderResult{
		Content: []Line{{
			Cells: []Cell{
				{Text: "hot", Width: 3, Style: StyleAccent, Safe: true},
				{Text: "\x1b[31mraw\x1b[0m", Width: 3, Safe: true},
			},
		}},
		Cursor:   Cursor{Visible: true, Row: 2, Col: 3, Shape: CursorShapeBar},
		Blink:    true,
		Metadata: RenderMetadata{Width: 6, Height: 1},
	}

	frame := FrameFromRenderResult(result)
	if len(frame.Lines) != 1 || frame.Lines[0] != "hotraw" {
		t.Fatalf("plain snapshot must strip ANSI while preserving text, got %#v", frame.Lines)
	}
	if len(frame.StyledLines) != 1 || frame.StyledLines[0].Cells[0].Style != StyleAccent {
		t.Fatalf("styled lines not preserved: %#v", frame.StyledLines)
	}
	if len(frame.ANSILines) != 1 || !strings.Contains(frame.ANSILines[0], "\x1b[") || !strings.Contains(frame.ANSILines[0], "hot") || !strings.HasSuffix(frame.ANSILines[0], ANSIReset) {
		t.Fatalf("ANSI line must retain SGR and reset, got %#v", frame.ANSILines)
	}
	if !frame.Cursor.Visible || frame.Cursor.Row != 2 || frame.Cursor.Col != 3 || frame.Cursor.Shape != CursorShapeBar {
		t.Fatalf("cursor metadata lost: %#v", frame.Cursor)
	}
	if !frame.Blink || frame.Metadata.Width != 6 || frame.Metadata.Height != 1 {
		t.Fatalf("frame metadata lost: blink=%v metadata=%#v", frame.Blink, frame.Metadata)
	}
}

func TestFrameCloneDetachesLines(t *testing.T) {
	frame := Frame{
		Lines:       []string{"one"},
		ANSILines:   []string{"\x1b[31mone\x1b[0m"},
		StyledLines: []Line{{Cells: []Cell{{Text: "one", Width: 3, Style: StyleAccent, Safe: true}}}},
		Cursor:      Cursor{Visible: true, Row: 1, Col: 2},
		Metadata:    RenderMetadata{Width: 3, Height: 1},
	}
	cloned := frame.Clone()
	frame.Lines[0] = "mutated"
	frame.ANSILines[0] = "mutated"
	frame.StyledLines[0].Cells[0].Text = "mutated"
	if cloned.Lines[0] != "one" {
		t.Fatalf("expected detached plain clone, got %#v", cloned)
	}
	if cloned.ANSILines[0] != "\x1b[31mone\x1b[0m" {
		t.Fatalf("expected detached ANSI clone, got %#v", cloned)
	}
	if cloned.StyledLines[0].Cells[0].Text != "one" || cloned.Cursor.Row != 1 || cloned.Metadata.Width != 3 {
		t.Fatalf("expected detached styled clone with metadata, got %#v", cloned)
	}
}
