package app

import (
	"testing"

	"github.com/lozzow/termx/tui/input"
	"github.com/lozzow/termx/tui/render"
	"github.com/lozzow/termx/tui/state"
)

func TestHostRenderFuncUsesANSIOnlyWhenSinkDoesNotNeedCompleteFrame(t *testing.T) {
	host := renderPreferenceHost{sink: ansiOnlyPreferenceSink{}}
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())

	frame := hostRenderFunc(host, builder, renderer)(state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 20, Rows: 6},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Cols:       18,
			Rows:       4,
			Lines:      []string{"hello"},
		},
	})

	if len(frame.ANSILines) == 0 {
		t.Fatalf("ANSI-only render must still produce ANSI lines, got %#v", frame)
	}
	if len(frame.Lines) != 0 || len(frame.StyledLines) != 0 {
		t.Fatalf("ANSI-only render should skip plain/styled snapshots, got %#v", frame)
	}
}

func TestHostRenderFuncKeepsCompleteFrameByDefault(t *testing.T) {
	host := renderPreferenceHost{sink: completePreferenceSink{}}
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())

	frame := hostRenderFunc(host, builder, renderer)(state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 20, Rows: 6},
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Cols:       18,
			Rows:       4,
			Lines:      []string{"hello"},
		},
	})

	if len(frame.Lines) == 0 || len(frame.StyledLines) == 0 || len(frame.ANSILines) == 0 {
		t.Fatalf("complete render should keep plain/styled/ANSI snapshots, got %#v", frame)
	}
}

type renderPreferenceHost struct {
	sink render.FrameSink
}

func (host renderPreferenceHost) Size() (int, int, error) {
	return 20, 6, nil
}

func (host renderPreferenceHost) InputEvents() <-chan input.InputEvent {
	return nil
}

func (host renderPreferenceHost) EventsReady() <-chan struct{} {
	return nil
}

func (host renderPreferenceHost) FrameSink() render.FrameSink {
	return host.sink
}

type ansiOnlyPreferenceSink struct{}

func (sink ansiOnlyPreferenceSink) WriteFrame(render.Frame) error {
	return nil
}

func (sink ansiOnlyPreferenceSink) NeedsCompleteFrame() bool {
	return false
}

type completePreferenceSink struct{}

func (sink completePreferenceSink) WriteFrame(render.Frame) error {
	return nil
}
