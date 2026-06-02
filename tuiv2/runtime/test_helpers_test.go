package runtime

import (
	"context"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
)

type fakeBridgeClient struct {
	attachResult       *protocol.AttachResult
	snapshotByTerminal map[string]*protocol.Snapshot
	resizeCalls        []fakeResizeCall
}

type fakeResizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

func newFakeBridgeClient() *fakeBridgeClient {
	return &fakeBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
}

func (c *fakeBridgeClient) Close() error { return nil }
func (c *fakeBridgeClient) Create(context.Context, protocol.CreateParams) (*protocol.CreateResult, error) {
	return &protocol.CreateResult{}, nil
}
func (c *fakeBridgeClient) SetTags(context.Context, string, map[string]string) error { return nil }
func (c *fakeBridgeClient) SetMetadata(context.Context, string, string, map[string]string) error { return nil }
func (c *fakeBridgeClient) List(context.Context) (*protocol.ListResult, error) { return &protocol.ListResult{}, nil }
func (c *fakeBridgeClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return make(chan protocol.Event), nil
}
func (c *fakeBridgeClient) Attach(context.Context, protocol.AttachParams) (*protocol.AttachResult, error) {
	if c.attachResult != nil {
		return c.attachResult, nil
	}
	return &protocol.AttachResult{Channel: 1, Mode: "collaborator"}, nil
}
func (c *fakeBridgeClient) EnsureResize(_ context.Context, params protocol.EnsureResizeParams) (*protocol.EnsureResizeResult, error) {
	c.resizeCalls = append(c.resizeCalls, fakeResizeCall{channel: params.Channel, cols: params.Cols, rows: params.Rows})
	return &protocol.EnsureResizeResult{
		Resized: true,
		Size:    protocol.Size{Cols: params.Cols, Rows: params.Rows},
		ResizeControl: &protocol.ResizeControl{
			CanResize: true,
			Reason:    protocol.ResizeControlReasonOwner,
		},
	}, nil
}
func (c *fakeBridgeClient) Snapshot(_ context.Context, terminalID string, _, _ int) (*protocol.Snapshot, error) {
	return cloneProtocolSnapshot(c.snapshotByTerminal[terminalID]), nil
}
func (c *fakeBridgeClient) GridViewport(context.Context, string, int, int, int) (*protocol.GridViewport, error) {
	return &protocol.GridViewport{}, nil
}
func (c *fakeBridgeClient) Input(context.Context, uint16, []byte) error { return nil }
func (c *fakeBridgeClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	return nil
}
func (c *fakeBridgeClient) StreamReady(context.Context, uint16, uint64) error { return nil }
func (c *fakeBridgeClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	return make(chan protocol.StreamFrame), func() {}
}
func (c *fakeBridgeClient) Kill(context.Context, string) error { return nil }
func (c *fakeBridgeClient) Remove(context.Context, string) error { return nil }
func (c *fakeBridgeClient) Restart(context.Context, string) error { return nil }

func snapshotWithLines(terminalID string, cols, rows uint16, lines []string) *protocol.Snapshot {
	screen := make([][]protocol.Cell, 0, len(lines))
	for _, line := range lines {
		screen = append(screen, protocolRowFromText(line, int(cols)))
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: cols, Rows: rows},
		Screen:     protocol.ScreenData{Cells: screen},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func protocolRowFromText(text string, cols int) []protocol.Cell {
	if cols <= 0 {
		cols = len([]rune(text))
	}
	cells := make([]protocol.Cell, 0, cols)
	for _, r := range []rune(text) {
		cells = append(cells, protocol.Cell{Content: string(r), Width: 1})
	}
	for len(cells) < cols {
		cells = append(cells, protocol.Cell{Content: " ", Width: 1})
	}
	return cells
}

func rowText(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}
