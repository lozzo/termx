package sessionstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	termx "github.com/lozzow/termx/termx-core"
)

type inMemoryStorageClient struct {
	mu              sync.Mutex
	storage         *termx.Storage
	conflictOnceKey string
	conflicted      bool
}

func newInMemoryStorageClient() *inMemoryStorageClient {
	return &inMemoryStorageClient{storage: termx.NewStorage()}
}

func (c *inMemoryStorageClient) StorageGet(_ context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error) {
	entry, err := c.storage.Get(termx.StorageGetRequest{
		AppID:   params.AppID,
		Scope:   termx.StorageScope(params.Scope),
		OwnerID: params.OwnerID,
		Key:     params.Key,
	})
	if err != nil {
		return nil, protocolError(err)
	}
	return protocolEntry(entry), nil
}

func (c *inMemoryStorageClient) StoragePut(_ context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error) {
	c.mu.Lock()
	if params.Key == c.conflictOnceKey && !c.conflicted {
		c.conflicted = true
		_, _ = c.storage.Put(termx.StoragePutRequest{
			AppID:   params.AppID,
			Scope:   termx.StorageScope(params.Scope),
			OwnerID: params.OwnerID,
			Key:     params.Key,
			Value:   params.Value,
		})
	}
	c.mu.Unlock()

	entry, err := c.storage.Put(termx.StoragePutRequest{
		AppID:           params.AppID,
		Scope:           termx.StorageScope(params.Scope),
		OwnerID:         params.OwnerID,
		Key:             params.Key,
		Value:           params.Value,
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	})
	if err != nil {
		return nil, protocolError(err)
	}
	return protocolEntry(entry), nil
}

func (c *inMemoryStorageClient) StorageDelete(_ context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error) {
	result, err := c.storage.Delete(termx.StorageDeleteRequest{
		AppID:           params.AppID,
		Scope:           termx.StorageScope(params.Scope),
		OwnerID:         params.OwnerID,
		Key:             params.Key,
		CheckVersion:    params.CheckVersion,
		ExpectedVersion: params.ExpectedVersion,
	})
	if err != nil {
		return nil, protocolError(err)
	}
	return &protocol.StorageDeleteResult{
		AppID:   result.AppID,
		Scope:   protocol.StorageScope(result.Scope),
		OwnerID: result.OwnerID,
		Key:     result.Key,
		Deleted: result.Deleted,
		Version: result.Version,
	}, nil
}

func (c *inMemoryStorageClient) StorageList(_ context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error) {
	entries, err := c.storage.List(termx.StorageListRequest{
		AppID:   params.AppID,
		Scope:   termx.StorageScope(params.Scope),
		OwnerID: params.OwnerID,
		Prefix:  params.Prefix,
	})
	if err != nil {
		return nil, protocolError(err)
	}
	out := make([]protocol.StorageEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, *protocolEntry(entry))
	}
	return &protocol.StorageListResult{Entries: out}, nil
}

func (c *inMemoryStorageClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func protocolEntry(entry termx.StorageEntry) *protocol.StorageEntry {
	return &protocol.StorageEntry{
		AppID:     entry.AppID,
		Scope:     protocol.StorageScope(entry.Scope),
		OwnerID:   entry.OwnerID,
		Key:       entry.Key,
		Value:     append([]byte(nil), entry.Value...),
		Version:   entry.Version,
		UpdatedAt: entry.UpdatedAt,
	}
}

func protocolError(err error) error {
	switch {
	case errors.Is(err, termx.ErrNotFound):
		return fmt.Errorf("protocol error 404: %v", err)
	case errors.Is(err, termx.ErrConflict):
		return fmt.Errorf("protocol error 409: %v", err)
	default:
		return err
	}
}

func TestUpdateViewRetriesRecoverableStorageVersionConflict(t *testing.T) {
	client := newInMemoryStorageClient()
	store := New(client)
	snapshot, err := store.Create(context.Background(), CreateParams{SessionID: "main"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	attached, err := store.Attach(context.Background(), AttachParams{SessionID: snapshot.Session.ID, WindowCols: 80, WindowRows: 24})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	client.conflictOnceKey = viewKey(attached.Session.ID, attached.View.ViewID)

	view, err := store.UpdateView(context.Background(), UpdateViewParams{
		SessionID: attached.Session.ID,
		ViewID:    attached.View.ViewID,
		View: UpdateViewPatch{
			FocusedPaneID: "pane-2",
			WindowCols:    120,
			WindowRows:    40,
		},
	})
	if err != nil {
		t.Fatalf("UpdateView should recover from one storage CAS conflict: %v", err)
	}
	if view.FocusedPaneID != "pane-2" || view.WindowCols != 120 || view.WindowRows != 40 {
		t.Fatalf("unexpected updated view: %#v", view)
	}

	_, entry, err := store.getViewEntry(context.Background(), attached.Session.ID, attached.View.ViewID)
	if err != nil {
		t.Fatalf("getViewEntry: %v", err)
	}
	if entry.Version != 3 {
		t.Fatalf("expected original view, raced write, and retry versions, got %d", entry.Version)
	}
}

func TestReplaceWrapsStorageVersionMismatchAsSessionConflict(t *testing.T) {
	client := newInMemoryStorageClient()
	store := New(client)
	snapshot, err := store.Create(context.Background(), CreateParams{SessionID: "main"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	client.conflictOnceKey = stateKey(snapshot.Session.ID)
	snapshot.Session.Revision++
	snapshot.Session.UpdatedAt = time.Now().UTC()

	_, err = store.Replace(context.Background(), ReplaceParams{
		SessionID:    snapshot.Session.ID,
		BaseRevision: 1,
		Workbench:    snapshot.Workbench,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected session conflict wrapper, got %v", err)
	}
	if !strings.Contains(err.Error(), "storage version mismatch") {
		t.Fatalf("expected wrapped storage mismatch details, got %v", err)
	}
}
