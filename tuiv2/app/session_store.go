package app

import (
	"context"

	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/sessionstore"
)

type sessionStore interface {
	CreateSession(ctx context.Context, params sessionstore.CreateParams) (*sessionstore.Snapshot, error)
	GetSession(ctx context.Context, sessionID string) (*sessionstore.Snapshot, error)
	AttachSession(ctx context.Context, params sessionstore.AttachParams) (*sessionstore.Snapshot, error)
	ReplaceSession(ctx context.Context, params sessionstore.ReplaceParams) (*sessionstore.Snapshot, error)
	UpdateSessionView(ctx context.Context, params sessionstore.UpdateViewParams) (*sessionstore.ViewInfo, error)
	AcquireSessionLease(ctx context.Context, params sessionstore.AcquireLeaseParams) (*sessionstore.LeaseInfo, error)
	ReleaseSessionLease(ctx context.Context, params sessionstore.ReleaseLeaseParams) error
}

type storageBackedSessionStore struct {
	store *sessionstore.Store
}

func newSessionStoreFromClient(client bridge.Client) sessionStore {
	if store, ok := client.(sessionStore); ok {
		return store
	}
	storageClient, ok := client.(sessionstore.Client)
	if !ok || storageClient == nil {
		return nil
	}
	return storageBackedSessionStore{store: sessionstore.New(storageClient)}
}

func (s storageBackedSessionStore) CreateSession(ctx context.Context, params sessionstore.CreateParams) (*sessionstore.Snapshot, error) {
	return s.store.Create(ctx, params)
}

func (s storageBackedSessionStore) GetSession(ctx context.Context, sessionID string) (*sessionstore.Snapshot, error) {
	return s.store.Get(ctx, sessionID)
}

func (s storageBackedSessionStore) AttachSession(ctx context.Context, params sessionstore.AttachParams) (*sessionstore.Snapshot, error) {
	return s.store.Attach(ctx, params)
}

func (s storageBackedSessionStore) ReplaceSession(ctx context.Context, params sessionstore.ReplaceParams) (*sessionstore.Snapshot, error) {
	return s.store.Replace(ctx, params)
}

func (s storageBackedSessionStore) UpdateSessionView(ctx context.Context, params sessionstore.UpdateViewParams) (*sessionstore.ViewInfo, error) {
	return s.store.UpdateView(ctx, params)
}

func (s storageBackedSessionStore) AcquireSessionLease(ctx context.Context, params sessionstore.AcquireLeaseParams) (*sessionstore.LeaseInfo, error) {
	return s.store.AcquireLease(ctx, params)
}

func (s storageBackedSessionStore) ReleaseSessionLease(ctx context.Context, params sessionstore.ReleaseLeaseParams) error {
	return s.store.ReleaseLease(ctx, params)
}
