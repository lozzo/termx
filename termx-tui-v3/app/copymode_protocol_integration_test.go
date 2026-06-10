package app

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyModeUsesProtocolHistoryWindowClient(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = serverTransport.Close() }()
		errCh <- runCopyModeHistoryProtocolServer(serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-copy"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		state.Root{
			Shell:         state.DefaultShell(),
			Session:       state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
			Surface:       state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 80, Rows: 24, Lines: []string{"live"}},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send older page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain older: %v", err)
	}
	if got := historyRowTexts(runtime.State().History.Rows); len(got) != 2 || got[0] != "old" || got[1] != "new" {
		t.Fatalf("unexpected protocol history rows %v", got)
	}
	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func runCopyModeHistoryProtocolServer(tr *memory.Transport) error {
	if err := expectCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := sendCopyModeProtocolHello(tr); err != nil {
		return err
	}
	req, err := expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.Token != "" || params.Cols != 78 || params.Limit != 20 {
		return fmt.Errorf("unexpected latest params %#v", params)
	}
	if err := sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID:   "term-1",
		Token:        "tok-1",
		Op:           protocol.HistoryWindowReplace,
		Size:         protocol.Size{Cols: 78, Rows: 20},
		Rows:         []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "new"}})},
		Lines:        []protocol.HistoryLineSpan{{LogicalLineID: 20, StartRow: 0, EndRow: 0}},
		RowLineIDs:   []uint64{20},
		RowInLine:    []int{0},
		CursorValid:  true,
		CursorLineID: 20,
		Generation:   7,
		FirstLineID:  20,
		LastLineID:   20,
	}); err != nil {
		return err
	}
	req, err = expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err = copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.Token != "tok-1" || params.Generation != 7 || !params.CursorValid || params.BeforeLineID != 20 || params.BoundaryLastLineID != 20 {
		return fmt.Errorf("unexpected older params %#v", params)
	}
	return sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID:   "term-1",
		Token:        "tok-1",
		Op:           protocol.HistoryWindowPrepend,
		Size:         protocol.Size{Cols: 78, Rows: 20},
		Rows:         []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "old"}})},
		Lines:        []protocol.HistoryLineSpan{{LogicalLineID: 10, StartRow: 0, EndRow: 0}},
		RowLineIDs:   []uint64{10},
		RowInLine:    []int{0},
		CursorValid:  true,
		CursorLineID: 10,
		Generation:   7,
		FirstLineID:  10,
		LastLineID:   20,
	})
}

func sendCopyModeHistoryWindow(tr *memory.Transport, req protocol.Request, window protocol.HistoryWindow) error {
	payload, err := protocol.EncodeHistoryWindowPayload(&window)
	if err != nil {
		return err
	}
	binaryPayload, err := protocol.EncodeBinaryResponsePayload(req.ID, payload)
	if err != nil {
		return err
	}
	return sendCopyModeProtocolFrame(tr, 0, wire.TypeResponseBinary, binaryPayload)
}

func expectCopyModeProtocolHello(tr *memory.Transport) error {
	channel, typ, payload, err := recvCopyModeProtocolFrame(tr)
	if err != nil {
		return err
	}
	if channel != 0 || typ != wire.TypeHello {
		return fmt.Errorf("unexpected hello frame channel=%d type=%d", channel, typ)
	}
	_, err = protocol.DecodeHelloPayload(payload)
	return err
}

func sendCopyModeProtocolHello(tr *memory.Transport) error {
	payload, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Server: "fake"})
	if err != nil {
		return err
	}
	return sendCopyModeProtocolFrame(tr, 0, wire.TypeHello, payload)
}

func expectCopyModeProtocolRequest(tr *memory.Transport, method string) (protocol.Request, error) {
	channel, typ, payload, err := recvCopyModeProtocolFrame(tr)
	if err != nil {
		return protocol.Request{}, err
	}
	if channel != 0 || typ != wire.TypeRequest {
		return protocol.Request{}, fmt.Errorf("unexpected request frame channel=%d type=%d", channel, typ)
	}
	req, err := protocol.DecodeRequestPayload(payload)
	if err != nil {
		return protocol.Request{}, err
	}
	if req.Method != method {
		return protocol.Request{}, fmt.Errorf("unexpected method %s", req.Method)
	}
	return req, nil
}

func copyModeProtocolRequestParams[T any](req protocol.Request) (T, error) {
	var zero T
	decoded, err := protocol.DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return zero, err
	}
	params, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected params type %T", decoded)
	}
	return params, nil
}

func sendCopyModeProtocolFrame(tr *memory.Transport, channel uint16, typ uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

func recvCopyModeProtocolFrame(tr *memory.Transport) (uint16, uint8, []byte, error) {
	frame, err := tr.Recv()
	if err != nil {
		return 0, 0, nil, err
	}
	return wire.DecodeFrame(frame)
}
