package termx

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
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

	if _, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
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

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     41,
		Method: "session.acquire_lease",
		Params: mustJSONRaw(t, protocol.AcquireSessionLeaseParams{
			SessionID:  "main",
			ViewID:     attached.View.ViewID,
			PaneID:     "2",
			TerminalID: "term-2",
		}),
	}, sendFrame)

	var lease protocol.LeaseInfo
	if err := json.Unmarshal(result, &lease); err != nil {
		t.Fatalf("unmarshal acquire lease result: %v", err)
	}
	if lease.ViewID != attached.View.ViewID || lease.PaneID != "2" || lease.TerminalID != "term-2" {
		t.Fatalf("unexpected lease payload: %#v", lease)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     42,
		Method: "session.get",
		Params: mustJSONRaw(t, protocol.GetSessionParams{SessionID: "main"}),
	}, sendFrame)

	var leased protocol.SessionSnapshot
	if err := json.Unmarshal(result, &leased); err != nil {
		t.Fatalf("unmarshal leased snapshot: %v", err)
	}
	if len(leased.Leases) != 1 || leased.Leases[0].TerminalID != "term-2" {
		t.Fatalf("expected lease in session snapshot, got %#v", leased.Leases)
	}

	_ = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     43,
		Method: "session.release_lease",
		Params: mustJSONRaw(t, protocol.ReleaseSessionLeaseParams{
			SessionID:  "main",
			ViewID:     attached.View.ViewID,
			TerminalID: "term-2",
		}),
	}, sendFrame)

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

func TestHandleRequestSessionAttachDefaultsClientIDToRemote(t *testing.T) {
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
		Params: mustJSONRaw(t, protocol.AttachSessionParams{SessionID: "main"}),
	}, sendFrame)

	var attached protocol.SessionSnapshot
	if err := json.Unmarshal(result, &attached); err != nil {
		t.Fatalf("unmarshal attach result: %v", err)
	}
	if attached.View == nil {
		t.Fatalf("expected attached view, got %#v", attached)
	}
	if attached.View.ClientID != "memory" {
		t.Fatalf("expected remote client id fallback, got %#v", attached.View)
	}
}

func TestHandleRequestSessionMutationsPublishEvents(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	eventsCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := srv.Events(eventsCtx, WithSessionFilter("main"))

	result := mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "session.create",
		Params: mustJSONRaw(t, protocol.CreateSessionParams{SessionID: "main"}),
	}, sendFrame)
	var created protocol.SessionSnapshot
	if err := json.Unmarshal(result, &created); err != nil {
		t.Fatalf("unmarshal create result: %v", err)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     2,
		Method: "session.attach",
		Params: mustJSONRaw(t, protocol.AttachSessionParams{SessionID: "main"}),
	}, sendFrame)
	var attached protocol.SessionSnapshot
	if err := json.Unmarshal(result, &attached); err != nil {
		t.Fatalf("unmarshal attach result: %v", err)
	}

	result = mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     3,
		Method: "session.apply",
		Params: mustJSONRaw(t, protocol.ApplySessionParams{
			SessionID:    "main",
			ViewID:       attached.View.ViewID,
			BaseRevision: created.Session.Revision,
			Ops: []protocol.SessionOp{
				{Kind: "split-pane", TabID: "1", PaneID: "1", NewPaneID: "2", Direction: "vertical"},
			},
		}),
	}, sendFrame)
	var applied protocol.SessionSnapshot
	if err := json.Unmarshal(result, &applied); err != nil {
		t.Fatalf("unmarshal apply result: %v", err)
	}

	mustHandleSessionRequest(t, srv, ctx, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     4,
		Method: "session.delete",
		Params: mustJSONRaw(t, protocol.GetSessionParams{SessionID: "main"}),
	}, sendFrame)

	var collected []Event
	deadline := time.After(3 * time.Second)
	for len(collected) < 3 {
		select {
		case evt := <-events:
			collected = append(collected, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for session events: %#v", collected)
		}
	}

	if collected[0].Type != EventSessionCreated || collected[0].Session == nil || collected[0].Session.Revision != 1 {
		t.Fatalf("unexpected create event: %#v", collected[0])
	}
	if collected[1].Type != EventSessionUpdated || collected[1].Session == nil || collected[1].Session.Revision != applied.Session.Revision || collected[1].Session.ViewID != attached.View.ViewID {
		t.Fatalf("unexpected update event: %#v", collected[1])
	}
	if collected[2].Type != EventSessionDeleted || collected[2].Session == nil || collected[2].Session.Revision != 0 {
		t.Fatalf("unexpected delete event: %#v", collected[2])
	}
}

func TestHandleRequestSessionUnknownMethodReturns400(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	_, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "session.unknown",
		Params: json.RawMessage(`{}`),
	}, sendFrame)
	if err == nil {
		t.Fatal("expected unknown session method to fail")
	}
	if code != 400 {
		t.Fatalf("expected code 400, got %d (err=%v)", code, err)
	}
}

func TestHandleRequestSessionWithoutWorkbenchReturns500(t *testing.T) {
	ctx := context.Background()
	srv := &Server{}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	_, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "session.list",
		Params: json.RawMessage(`{}`),
	}, sendFrame)
	if err == nil {
		t.Fatal("expected missing workbench to fail")
	}
	if code != 500 {
		t.Fatalf("expected code 500, got %d (err=%v)", code, err)
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
	result, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, attachmentsMu, req, sendFrame)
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
