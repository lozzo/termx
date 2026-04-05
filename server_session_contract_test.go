package termx

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestHandleRequestSessionCreateListGetDelete(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	result := mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "session.create",
		Params: mustJSONRaw(t, protocol.CreateSessionParams{SessionID: "main", Name: "Main"}),
	}, sendFrame)

	var created protocol.SessionSnapshot
	if err := json.Unmarshal(result, &created); err != nil {
		t.Fatalf("unmarshal create result: %v", err)
	}
	if created.Session.ID != "main" || created.Session.Revision != 1 {
		t.Fatalf("unexpected created session: %#v", created)
	}
	if created.Workbench == nil || created.Workbench.Workspaces["main"] == nil {
		t.Fatalf("expected default workbench snapshot, got %#v", created.Workbench)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     2,
		Method: "session.list",
		Params: json.RawMessage(`{}`),
	}, sendFrame)

	var listed protocol.ListSessionsResult
	if err := json.Unmarshal(result, &listed); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != "main" {
		t.Fatalf("unexpected sessions: %#v", listed.Sessions)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     3,
		Method: "session.get",
		Params: mustJSONRaw(t, protocol.GetSessionParams{SessionID: "main"}),
	}, sendFrame)

	var got protocol.SessionSnapshot
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal get result: %v", err)
	}
	if got.Session.ID != "main" || got.Workbench == nil {
		t.Fatalf("unexpected get snapshot: %#v", got)
	}

	mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     4,
		Method: "session.delete",
		Params: mustJSONRaw(t, protocol.GetSessionParams{SessionID: "main"}),
	}, sendFrame)

	if _, code, err := srv.handleRequest(ctx, "memory", allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     5,
		Method: "session.get",
		Params: mustJSONRaw(t, protocol.GetSessionParams{SessionID: "main"}),
	}, sendFrame); err == nil || code != 404 {
		t.Fatalf("expected deleted session lookup to fail with 404, code=%d err=%v", code, err)
	}
}

func TestHandleRequestSessionAttachApplyAndViewUpdate(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "session.create",
		Params: mustJSONRaw(t, protocol.CreateSessionParams{SessionID: "main"}),
	}, sendFrame)

	result := mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     2,
		Method: "session.attach",
		Params: mustJSONRaw(t, protocol.AttachSessionParams{SessionID: "main", WindowCols: 180, WindowRows: 50}),
	}, sendFrame)

	var attached protocol.SessionSnapshot
	if err := json.Unmarshal(result, &attached); err != nil {
		t.Fatalf("unmarshal attach result: %v", err)
	}
	if attached.View == nil || attached.View.ViewID == "" {
		t.Fatalf("expected attached view, got %#v", attached)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     3,
		Method: "session.apply",
		Params: mustJSONRaw(t, protocol.ApplySessionParams{
			SessionID:    "main",
			ViewID:       attached.View.ViewID,
			BaseRevision: attached.Session.Revision,
			Ops: []protocol.SessionOp{
				{Kind: "split-pane", TabID: "1", PaneID: "1", NewPaneID: "2", Direction: "vertical"},
				{Kind: "bind-terminal", TabID: "1", PaneID: "2", TerminalID: "term-2"},
			},
		}),
	}, sendFrame)

	var applied protocol.SessionSnapshot
	if err := json.Unmarshal(result, &applied); err != nil {
		t.Fatalf("unmarshal apply result: %v", err)
	}
	if applied.Session.Revision != 2 {
		t.Fatalf("expected revision 2, got %#v", applied.Session)
	}
	if got := applied.Workbench.Workspaces["main"].Tabs[0].Panes["2"].TerminalID; got != "term-2" {
		t.Fatalf("expected terminal binding term-2, got %q", got)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     4,
		Method: "session.view_update",
		Params: mustJSONRaw(t, protocol.UpdateSessionViewParams{
			SessionID: "main",
			ViewID:    attached.View.ViewID,
			View: protocol.UpdateSessionViewPatch{
				ActiveTabID:   "1",
				FocusedPaneID: "2",
			},
		}),
	}, sendFrame)

	var updated protocol.ViewInfo
	if err := json.Unmarshal(result, &updated); err != nil {
		t.Fatalf("unmarshal view update result: %v", err)
	}
	if updated.FocusedPaneID != "2" {
		t.Fatalf("expected focused pane 2, got %#v", updated)
	}

	replacement := applied.Workbench.Clone()
	replacement.Workspaces["main"].Tabs[0].Name = "replaced"
	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     5,
		Method: "session.replace",
		Params: mustJSONRaw(t, protocol.ReplaceSessionParams{
			SessionID:    "main",
			ViewID:       attached.View.ViewID,
			BaseRevision: applied.Session.Revision,
			Workbench:    replacement,
		}),
	}, sendFrame)

	var replaced protocol.SessionSnapshot
	if err := json.Unmarshal(result, &replaced); err != nil {
		t.Fatalf("unmarshal replace result: %v", err)
	}
	if replaced.Session.Revision != 3 {
		t.Fatalf("expected revision 3 after replace, got %#v", replaced.Session)
	}
	if got := replaced.Workbench.Workspaces["main"].Tabs[0].Name; got != "replaced" {
		t.Fatalf("expected replaced tab name, got %q", got)
	}
}

func mustHandleSessionRequest(
	t *testing.T,
	srv *Server,
	ctx context.Context,
	allocator *protocol.ChannelAllocator,
	attachments map[uint16]*sessionAttachment,
	attachmentsMu *sync.RWMutex,
	req protocol.Request,
	sendFrame func(uint16, uint8, []byte) error,
) json.RawMessage {
	t.Helper()
	result, code, err := srv.handleRequest(ctx, "memory", allocator, attachments, attachmentsMu, req, sendFrame)
	if err != nil {
		t.Fatalf("handleRequest %s failed: code=%d err=%v", req.Method, code, err)
	}
	return result
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return data
}
