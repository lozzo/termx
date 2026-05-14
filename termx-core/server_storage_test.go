package termx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
)

func TestServerStorageRPCAndEvents(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()
	events := srv.Events(ctx, WithTypeFilter(EventStorageChanged), WithStorageFilter("tui", StorageScopePublic, "", "locks/"))

	req := protocol.Request{
		ID:     1,
		Method: "storage.put",
		Params: protocol.MustEncodeMethodParams("storage.put", protocol.StoragePutParams{
			AppID: "tui",
			Scope: protocol.StorageScopePublic,
			Key:   "locks/copy-mode-owner",
			Value: []byte("view-a"),
		}),
	}
	result, code, err := srv.handleRequest(ctx, "client-a", nil, protocol.NewChannelAllocator(), map[uint16]*sessionAttachment{}, &sync.RWMutex{}, transportScope{}, req, discardSendFrame)
	if err != nil {
		t.Fatalf("storage.put failed code=%d err=%v", code, err)
	}
	var entry protocol.StorageEntry
	if err := protocol.DecodeMethodResult("storage.put", result, &entry); err != nil {
		t.Fatalf("decode storage.put result: %v", err)
	}
	if entry.AppID != "tui" || entry.Scope != protocol.StorageScopePublic || entry.Key != "locks/copy-mode-owner" || string(entry.Value) != "view-a" || entry.Version != 1 {
		t.Fatalf("unexpected storage entry: %#v", entry)
	}

	select {
	case evt := <-events:
		if evt.Type != EventStorageChanged || evt.Storage == nil {
			t.Fatalf("unexpected storage event: %#v", evt)
		}
		if evt.Storage.AppID != "tui" || evt.Storage.Scope != StorageScopePublic || evt.Storage.Key != "locks/copy-mode-owner" || evt.Storage.Op != StorageOpPut {
			t.Fatalf("unexpected storage event payload: %#v", evt.Storage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for storage event")
	}

	getReq := protocol.Request{
		ID:     2,
		Method: "storage.get",
		Params: protocol.MustEncodeMethodParams("storage.get", protocol.StorageGetParams{
			AppID: "tui",
			Scope: protocol.StorageScopePublic,
			Key:   "locks/copy-mode-owner",
		}),
	}
	getResult, code, err := srv.handleRequest(ctx, "client-b", nil, protocol.NewChannelAllocator(), map[uint16]*sessionAttachment{}, &sync.RWMutex{}, transportScope{}, getReq, discardSendFrame)
	if err != nil {
		t.Fatalf("storage.get failed code=%d err=%v", code, err)
	}
	var got protocol.StorageEntry
	if err := protocol.DecodeMethodResult("storage.get", getResult, &got); err != nil {
		t.Fatalf("decode storage.get result: %v", err)
	}
	if string(got.Value) != "view-a" || got.Version != 1 {
		t.Fatalf("unexpected get result: %#v", got)
	}
}

func TestServerStoragePrivateDefaultsToRemoteOwner(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()
	attachments := map[uint16]*sessionAttachment{}
	var attachmentsMu sync.RWMutex

	putReq := protocol.Request{
		ID:     1,
		Method: "storage.put",
		Params: protocol.MustEncodeMethodParams("storage.put", protocol.StoragePutParams{
			AppID: "tui",
			Scope: protocol.StorageScopePrivate,
			Key:   "viewport",
			Value: []byte("top=20"),
		}),
	}
	if _, _, err := srv.handleRequest(ctx, "client-a", nil, protocol.NewChannelAllocator(), attachments, &attachmentsMu, transportScope{}, putReq, discardSendFrame); err != nil {
		t.Fatalf("storage.put private: %v", err)
	}

	listReq := protocol.Request{
		ID:     2,
		Method: "storage.list",
		Params: protocol.MustEncodeMethodParams("storage.list", protocol.StorageListParams{
			AppID: "tui",
			Scope: protocol.StorageScopePrivate,
		}),
	}
	result, code, err := srv.handleRequest(ctx, "client-a", nil, protocol.NewChannelAllocator(), attachments, &attachmentsMu, transportScope{}, listReq, discardSendFrame)
	if err != nil {
		t.Fatalf("storage.list private failed code=%d err=%v", code, err)
	}
	var listed protocol.StorageListResult
	if err := protocol.DecodeMethodResult("storage.list", result, &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Entries) != 1 || listed.Entries[0].OwnerID != "client-a" || string(listed.Entries[0].Value) != "top=20" {
		t.Fatalf("unexpected private list: %#v", listed)
	}

	result, code, err = srv.handleRequest(ctx, "client-b", nil, protocol.NewChannelAllocator(), attachments, &attachmentsMu, transportScope{}, listReq, discardSendFrame)
	if err != nil {
		t.Fatalf("storage.list other private failed code=%d err=%v", code, err)
	}
	listed = protocol.StorageListResult{}
	if err := protocol.DecodeMethodResult("storage.list", result, &listed); err != nil {
		t.Fatalf("decode other list: %v", err)
	}
	if len(listed.Entries) != 0 {
		t.Fatalf("expected private state to be isolated from other remote, got %#v", listed)
	}
}

func TestServerStorageEventsDoNotLeakOtherPrivateOwners(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()
	attachments := map[uint16]*sessionAttachment{}
	var attachmentsMu sync.RWMutex

	clientAEvents := make(chan protocol.Event, 8)
	if _, code, err := srv.handleEventsRequest(ctx, "client-a", protocol.Request{
		ID:     1,
		Method: "events",
		Params: protocol.MustEncodeMethodParams("events", protocol.EventsParams{
			Types: []protocol.EventType{protocol.EventStorageChanged},
		}),
	}, func() {}, &sync.Mutex{}, new(context.CancelFunc), func(_ uint16, typ uint8, payload []byte) error {
		if typ != protocol.TypeEvent {
			return nil
		}
		evt, err := protocol.DecodeEventPayload(payload)
		if err != nil {
			return err
		}
		clientAEvents <- evt
		return nil
	}); err != nil {
		t.Fatalf("events subscribe failed code=%d err=%v", code, err)
	}

	for _, remote := range []string{"client-b", "client-a"} {
		req := protocol.Request{
			ID:     2,
			Method: "storage.put",
			Params: protocol.MustEncodeMethodParams("storage.put", protocol.StoragePutParams{
				AppID: "tui",
				Scope: protocol.StorageScopePrivate,
				Key:   "viewport",
				Value: []byte(remote),
			}),
		}
		if _, _, err := srv.handleRequest(ctx, remote, nil, protocol.NewChannelAllocator(), attachments, &attachmentsMu, transportScope{}, req, discardSendFrame); err != nil {
			t.Fatalf("storage.put private for %s: %v", remote, err)
		}
	}

	select {
	case evt := <-clientAEvents:
		if evt.Storage == nil || evt.Storage.OwnerID != "client-a" || string(evt.Storage.Scope) != string(protocol.StorageScopePrivate) {
			t.Fatalf("unexpected event visible to client-a: %#v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for own private event")
	}
	select {
	case evt := <-clientAEvents:
		t.Fatalf("unexpected leaked private event: %#v", evt)
	case <-time.After(100 * time.Millisecond):
	}
}

func discardSendFrame(uint16, uint8, []byte) error {
	return nil
}
