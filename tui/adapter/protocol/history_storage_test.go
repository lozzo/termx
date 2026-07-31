package protocoladapter

import (
	"context"
	"errors"
	"testing"

	clientruntime "github.com/anytty/anytty/client/runtime"
	"github.com/anytty/anytty/proto/apipb"
	"github.com/anytty/anytty/tui/port"
	"github.com/anytty/anytty/tui/state"
)

func TestProtocolCoreClientAdapterUsesGeneratedHistoryWindow(t *testing.T) {
	executor := &recordingProtoExecutor{}
	executor.handle = func(command *apipb.CommandEnvelope) *apipb.ResultEnvelope {
		return protoTestResult(command, &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_HistoryWindow{HistoryWindow: &apipb.HistoryWindowResult{
			Terminal: &apipb.TerminalRef{EndpointId: "local", TerminalId: "term-1"}, Token: "token-1",
			Operation: apipb.HistoryWindowOperation_HISTORY_WINDOW_OPERATION_REPLACE,
			Size:      &apipb.TerminalSize{Cols: 80, Rows: 24}, HistoryGeneration: 7, FirstLineId: 41, LastLineId: 41,
			Rows: []*apipb.HistoryRow{{LogicalLineId: 41, Row: &apipb.ScreenRow{Cells: []*apipb.ScreenCell{{Content: "hello", Width: 5}}}}},
		}}})
	}
	adapter := ProtocolCoreClientAdapter{Application: newProtoTestApplication(t, executor)}

	result, err := adapter.HistoryLatest(context.Background(), port.HistoryLatestRequest{RequestID: 9, EndpointID: "local", TerminalID: "term-1", Rows: 20, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != 9 || result.Window.Token != "token-1" || len(result.Window.Rows) != 1 || result.Window.Rows[0].Text != "hello" {
		t.Fatalf("unexpected history result %#v", result)
	}
	if got := executor.commands[0].GetHistoryWindow(); got.GetTerminal().GetTerminalId() != "term-1" || got.GetLimit() != 20 {
		t.Fatalf("unexpected history command %#v", got)
	}
}

func TestHistoryCursorUsesLogicalCoordinates(t *testing.T) {
	want := state.HistoryCursor{
		Valid:           true,
		BeforeLineID:    42,
		BeforeRowInLine: 3,
		Segment:         state.HistoryCursorSegmentArchivedPrimaryFrame,
	}
	wire := historyCursorToProto(want)
	window := historyWindowFromProto(&apipb.HistoryWindowResult{
		Terminal: &apipb.TerminalRef{TerminalId: "term-1"},
		Size:     &apipb.TerminalSize{Cols: 80},
		Cursor:   wire,
	}, 80)
	if window.Cursor != want {
		t.Fatalf("history cursor round trip = %#v, want %#v", window.Cursor, want)
	}
}

func TestProtocolCoreClientAdapterUsesGeneratedCopyAndRelease(t *testing.T) {
	executor := &recordingProtoExecutor{}
	executor.handle = func(command *apipb.CommandEnvelope) *apipb.ResultEnvelope {
		switch command.Command.(type) {
		case *apipb.CommandEnvelope_HistoryCopy:
			return protoTestResult(command, &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_HistoryCopy{HistoryCopy: &apipb.HistoryCopyResult{Text: "copied"}}})
		default:
			return protoTestResult(command, &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_Acknowledge{Acknowledge: &apipb.AcknowledgeResult{}}})
		}
	}
	adapter := ProtocolCoreClientAdapter{Application: newProtoTestApplication(t, executor)}
	copyResult, err := adapter.HistoryCopyRange(context.Background(), port.HistoryCopyRangeRequest{
		EndpointID: "local", TerminalID: "term-1", Token: "token-1", Generation: 7, Cols: 80,
		Start: state.CopyLogicalPosition{Valid: true, LineID: 41, Col: 1}, End: state.CopyLogicalPosition{Valid: true, LineID: 42, Col: 2},
	})
	if err != nil || copyResult.Text != "copied" {
		t.Fatalf("copy=%#v err=%v", copyResult, err)
	}
	if err := adapter.ReleaseHistory(context.Background(), port.HistoryReleaseRequest{EndpointID: "local", TerminalID: "term-1", Token: "token-1"}); err != nil {
		t.Fatal(err)
	}
	if len(executor.commands) != 2 || executor.commands[0].GetHistoryCopy() == nil || executor.commands[1].GetHistoryRelease() == nil {
		t.Fatalf("unexpected commands %#v", executor.commands)
	}
}

func TestProtocolCoreClientAdapterNormalizesStaleHistoryError(t *testing.T) {
	err := normalizeProtocolHistoryError(errors.New("stale history window: generation changed"))
	if !errors.Is(err, port.ErrStaleHistoryWindow) {
		t.Fatalf("stale error = %v", err)
	}
}

type recordingProtoExecutor struct {
	commands []*apipb.CommandEnvelope
	handle   func(*apipb.CommandEnvelope) *apipb.ResultEnvelope
}

func (executor *recordingProtoExecutor) ExecuteApplication(_ context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	executor.commands = append(executor.commands, command)
	return executor.handle(command), nil
}

func newProtoTestApplication(t *testing.T, executor *recordingProtoExecutor) *clientruntime.ApplicationSession {
	t.Helper()
	session, err := clientruntime.NewApplicationSession(clientruntime.EndpointSessionStamp{EndpointID: "local", RouteID: "test", Generation: 1}, executor)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func protoTestResult(command *apipb.CommandEnvelope, result *apipb.ResultEnvelope) *apipb.ResultEnvelope {
	result.RequestId = command.GetContext().GetRequestId()
	result.OriginSession = command.GetContext().GetSession()
	return result
}
