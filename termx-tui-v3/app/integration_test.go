package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type enterCopyModeMsg struct {
	TerminalID string
	Cols       int
}

func (enterCopyModeMsg) isMsg() {}

type historyResultMsg struct {
	Result services.HistoryResult
	Err    error
}

func (historyResultMsg) isMsg() {}

func TestRuntimeServiceHistoryLatestE2E(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{
			Window: state.HistoryWindow{
				TerminalID: "term-1",
				Token:      "tok-1",
				Op:         state.HistoryWindowReplace,
				Cols:       80,
				Rows:       []state.HistoryRow{{Text: "history-row", LineID: 42}},
				Lines:      []state.HistoryLineSpan{{LineID: 42, StartRow: 0, EndRow: 0}},
				Generation: 9,
				Boundary:   state.HistoryBoundary{FirstLineID: 42, LastLineID: 42},
			},
		}},
	}
	host := NewFakeTerminalHost(4)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())

	runtime := NewAppRuntime(
		state.Root{},
		func(root state.Root, msg Msg) (state.Root, []Effect) {
			switch msg := msg.(type) {
			case enterCopyModeMsg:
				requestID := state.RequestID(1)
				var err error
				root.History, err = root.History.BeginLatest(state.HistoryPendingRequest{
					ID:         requestID,
					TerminalID: msg.TerminalID,
					Cols:       msg.Cols,
				})
				if err != nil {
					t.Fatalf("begin latest: %v", err)
				}
				root.CopyMode = root.CopyMode.BindLatest(msg.TerminalID, requestID, msg.Cols)
				return root, []Effect{FuncEffect{
					Run: func(ctx context.Context) Msg {
						result, err := core.HistoryLatest(ctx, services.HistoryLatestRequest{
							RequestID:  services.RequestID(requestID),
							TerminalID: msg.TerminalID,
							Cols:       msg.Cols,
							Rows:       20,
						})
						return historyResultMsg{Result: result, Err: err}
					},
				}}
			case historyResultMsg:
				if msg.Err != nil {
					t.Fatalf("history result: %v", msg.Err)
				}
				nextHistory, _, err := root.History.ApplyWindow(state.RequestID(msg.Result.RequestID), msg.Result.Window)
				if err != nil {
					t.Fatalf("apply history: %v", err)
				}
				root.History = nextHistory
				root.CopyMode = root.CopyMode.AcceptLatest(msg.Result.Window)
				return root, nil
			default:
				return root, nil
			}
		},
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := runtime.Post(enterCopyModeMsg{TerminalID: "term-1", Cols: 80}); err != nil {
		t.Fatalf("post enter copy mode: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(core.LatestRequests) != 1 || core.LatestRequests[0].TerminalID != "term-1" {
		t.Fatalf("expected latest service request, got %#v", core.LatestRequests)
	}
	if runtime.State().History.Token != "tok-1" || runtime.State().CopyMode.BoundToken != "tok-1" {
		t.Fatalf("state did not accept history response: %#v", runtime.State())
	}
	if got := firstLines(host.Frames()); !reflect.DeepEqual(got, []string{"live surface pending", "history-row"}) {
		t.Fatalf("expected pending frame then copy frame, got %v", got)
	}
}

func firstLines(frames []render.Frame) []string {
	lines := make([]string, 0, len(frames))
	for _, frame := range frames {
		if len(frame.Lines) > 0 {
			lines = append(lines, frame.Lines[0])
		}
	}
	return lines
}
