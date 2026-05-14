package sessionstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/sessiondoc"
)

const (
	AppID           = "tui"
	storageScope    = protocol.StorageScopePublic
	sessionPrefix   = "sessions/"
	stateKeySuffix  = "/state"
	viewKeyPart     = "/views/"
	leaseKeyPart    = "/leases/"
	storageOpPut    = "put"
	storageOpDelete = "delete"
)

var (
	ErrNotFound = errors.New("sessionstore: not found")
	ErrConflict = errors.New("sessionstore: revision conflict")
)

type Client interface {
	StorageGet(ctx context.Context, params protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(ctx context.Context, params protocol.StoragePutParams) (*protocol.StorageEntry, error)
	StorageDelete(ctx context.Context, params protocol.StorageDeleteParams) (*protocol.StorageDeleteResult, error)
	StorageList(ctx context.Context, params protocol.StorageListParams) (*protocol.StorageListResult, error)
	Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error)
}

type Store struct {
	client Client
}

func New(client Client) *Store {
	return &Store{client: client}
}

func (s *Store) Create(ctx context.Context, params CreateParams) (*Snapshot, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("sessionstore: client unavailable")
	}
	now := time.Now().UTC()
	sessionID := strings.TrimSpace(params.SessionID)
	if sessionID == "" {
		sessionID = "main"
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = sessionID
	}
	info := SessionInfo{
		ID:        sessionID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	doc := defaultDoc()
	value, err := encodeSessionRecord(info, doc)
	if err != nil {
		return nil, err
	}
	_, err = s.client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           AppID,
		Scope:           storageScope,
		Key:             stateKey(sessionID),
		Value:           value,
		CheckVersion:    true,
		ExpectedVersion: 0,
	})
	if err != nil {
		if isProtocolConflict(err) {
			return nil, fmt.Errorf("%w: session %q already exists", ErrConflict, sessionID)
		}
		return nil, err
	}
	return &Snapshot{Session: info, Workbench: doc.Clone()}, nil
}

func (s *Store) Get(ctx context.Context, sessionID string) (*Snapshot, error) {
	info, doc, _, err := s.getSessionEntry(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	leases, err := s.listLeases(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Session: info, Workbench: doc, Leases: leases}, nil
}

func (s *Store) Attach(ctx context.Context, params AttachParams) (*Snapshot, error) {
	info, doc, _, err := s.getSessionEntry(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	view := ViewInfo{
		ViewID:              newViewID(),
		SessionID:           info.ID,
		ClientID:            strings.TrimSpace(params.ClientID),
		ActiveWorkspaceName: doc.CurrentWorkspace,
		WindowCols:          params.WindowCols,
		WindowRows:          params.WindowRows,
		AttachedAt:          now,
		UpdatedAt:           now,
	}
	if ws := currentWorkspace(doc); ws != nil {
		if tab := currentTab(ws); tab != nil {
			view.ActiveTabID = tab.ID
			view.FocusedPaneID = activePaneID(tab)
		}
	}
	value, err := encodeView(view)
	if err != nil {
		return nil, err
	}
	_, err = s.client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           AppID,
		Scope:           storageScope,
		Key:             viewKey(info.ID, view.ViewID),
		Value:           value,
		CheckVersion:    true,
		ExpectedVersion: 0,
	})
	if err != nil {
		return nil, err
	}
	leases, err := s.listLeases(ctx, info.ID)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Session: info, Workbench: doc, View: &view, Leases: leases}, nil
}

func (s *Store) Replace(ctx context.Context, params ReplaceParams) (*Snapshot, error) {
	if params.Workbench == nil {
		return nil, fmt.Errorf("sessionstore: replacement workbench is required")
	}
	info, _, entry, err := s.getSessionEntry(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	if info.Revision != params.BaseRevision {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrConflict, params.BaseRevision, info.Revision)
	}
	info.Revision++
	info.UpdatedAt = time.Now().UTC()
	doc := params.Workbench.Clone()
	value, err := encodeSessionRecord(info, doc)
	if err != nil {
		return nil, err
	}
	_, err = s.client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           AppID,
		Scope:           storageScope,
		Key:             stateKey(info.ID),
		Value:           value,
		CheckVersion:    true,
		ExpectedVersion: entry.Version,
	})
	if err != nil {
		if isProtocolConflict(err) {
			return nil, fmt.Errorf("%w: expected %d, got %d", ErrConflict, params.BaseRevision, params.BaseRevision+1)
		}
		return nil, err
	}
	leases, err := s.listLeases(ctx, info.ID)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Session: info, Workbench: doc, Leases: leases}, nil
}

func (s *Store) UpdateView(ctx context.Context, params UpdateViewParams) (*ViewInfo, error) {
	view, entry, err := s.getViewEntry(ctx, params.SessionID, params.ViewID)
	if err != nil {
		return nil, err
	}
	if params.View.ActiveWorkspaceName != "" {
		view.ActiveWorkspaceName = params.View.ActiveWorkspaceName
	}
	if params.View.ActiveTabID != "" {
		view.ActiveTabID = params.View.ActiveTabID
	}
	if params.View.FocusedPaneID != "" {
		view.FocusedPaneID = params.View.FocusedPaneID
	}
	if params.View.WindowCols > 0 {
		view.WindowCols = params.View.WindowCols
	}
	if params.View.WindowRows > 0 {
		view.WindowRows = params.View.WindowRows
	}
	view.UpdatedAt = time.Now().UTC()
	value, err := encodeView(view)
	if err != nil {
		return nil, err
	}
	_, err = s.client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           AppID,
		Scope:           storageScope,
		Key:             viewKey(params.SessionID, params.ViewID),
		Value:           value,
		CheckVersion:    true,
		ExpectedVersion: entry.Version,
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Store) AcquireLease(ctx context.Context, params AcquireLeaseParams) (*LeaseInfo, error) {
	if strings.TrimSpace(params.TerminalID) == "" {
		return nil, fmt.Errorf("sessionstore: terminal id is required")
	}
	if strings.TrimSpace(params.PaneID) == "" {
		return nil, fmt.Errorf("sessionstore: pane id is required")
	}
	if _, _, _, err := s.getSessionEntry(ctx, params.SessionID); err != nil {
		return nil, err
	}
	if _, _, err := s.getViewEntry(ctx, params.SessionID, params.ViewID); err != nil {
		return nil, err
	}
	lease := LeaseInfo{
		TerminalID: params.TerminalID,
		SessionID:  params.SessionID,
		ViewID:     params.ViewID,
		PaneID:     params.PaneID,
		AcquiredAt: time.Now().UTC(),
	}
	value, err := encodeLease(lease)
	if err != nil {
		return nil, err
	}
	_, err = s.client.StoragePut(ctx, protocol.StoragePutParams{
		AppID: AppID,
		Scope: storageScope,
		Key:   leaseKey(params.SessionID, params.TerminalID),
		Value: value,
	})
	if err != nil {
		return nil, err
	}
	return &lease, nil
}

func (s *Store) ReleaseLease(ctx context.Context, params ReleaseLeaseParams) error {
	if strings.TrimSpace(params.TerminalID) == "" {
		return fmt.Errorf("sessionstore: terminal id is required")
	}
	if _, _, err := s.getViewEntry(ctx, params.SessionID, params.ViewID); err != nil {
		return err
	}
	entry, err := s.client.StorageGet(ctx, protocol.StorageGetParams{
		AppID: AppID,
		Scope: storageScope,
		Key:   leaseKey(params.SessionID, params.TerminalID),
	})
	if isProtocolNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lease, err := decodeLease(entry.Value)
	if err != nil {
		return err
	}
	if lease.SessionID != params.SessionID {
		return fmt.Errorf("sessionstore: terminal %q lease is not in session %q", params.TerminalID, params.SessionID)
	}
	if lease.ViewID != params.ViewID {
		return nil
	}
	_, err = s.client.StorageDelete(ctx, protocol.StorageDeleteParams{
		AppID: AppID,
		Scope: storageScope,
		Key:   leaseKey(params.SessionID, params.TerminalID),
	})
	return err
}

func (s *Store) Watch(ctx context.Context, sessionID string) (<-chan EventData, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("sessionstore: client unavailable")
	}
	events, err := s.client.Events(ctx, protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     AppID,
		StorageScope:     storageScope,
		StorageKeyPrefix: sessionPrefix + strings.TrimSpace(sessionID) + "/",
	})
	if err != nil {
		return nil, err
	}
	out := make(chan EventData, 32)
	go func() {
		defer close(out)
		for evt := range events {
			data, ok := EventFromProtocol(evt)
			if !ok || data.SessionID != sessionID {
				continue
			}
			select {
			case out <- data:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func EventFromProtocol(evt protocol.Event) (EventData, bool) {
	if evt.Type != protocol.EventStorageChanged || evt.Storage == nil {
		return EventData{}, false
	}
	return EventFromStorageChange(*evt.Storage)
}

func (s *Store) getSessionEntry(ctx context.Context, sessionID string) (SessionInfo, *sessiondoc.Doc, *protocol.StorageEntry, error) {
	sessionID = strings.TrimSpace(sessionID)
	entry, err := s.client.StorageGet(ctx, protocol.StorageGetParams{
		AppID: AppID,
		Scope: storageScope,
		Key:   stateKey(sessionID),
	})
	if isProtocolNotFound(err) {
		return SessionInfo{}, nil, nil, fmt.Errorf("%w: session %q", ErrNotFound, sessionID)
	}
	if err != nil {
		return SessionInfo{}, nil, nil, err
	}
	info, doc, err := decodeSessionRecord(entry.Value)
	if err != nil {
		return SessionInfo{}, nil, nil, err
	}
	if info.ID == "" {
		info.ID = sessionID
	}
	return info, doc, entry, nil
}

func (s *Store) getViewEntry(ctx context.Context, sessionID, viewID string) (ViewInfo, *protocol.StorageEntry, error) {
	entry, err := s.client.StorageGet(ctx, protocol.StorageGetParams{
		AppID: AppID,
		Scope: storageScope,
		Key:   viewKey(sessionID, viewID),
	})
	if isProtocolNotFound(err) {
		return ViewInfo{}, nil, fmt.Errorf("%w: view %q in session %q", ErrNotFound, viewID, sessionID)
	}
	if err != nil {
		return ViewInfo{}, nil, err
	}
	view, err := decodeView(entry.Value)
	if err != nil {
		return ViewInfo{}, nil, err
	}
	return view, entry, nil
}

func (s *Store) listLeases(ctx context.Context, sessionID string) ([]LeaseInfo, error) {
	result, err := s.client.StorageList(ctx, protocol.StorageListParams{
		AppID:  AppID,
		Scope:  storageScope,
		Prefix: leasesPrefix(sessionID),
	})
	if err != nil {
		return nil, err
	}
	leases := make([]LeaseInfo, 0, len(result.Entries))
	for _, entry := range result.Entries {
		lease, err := decodeLease(entry.Value)
		if err != nil || lease.TerminalID == "" {
			continue
		}
		leases = append(leases, lease)
	}
	sort.Slice(leases, func(i, j int) bool {
		return leases[i].TerminalID < leases[j].TerminalID
	})
	return leases, nil
}

func defaultDoc() *sessiondoc.Doc {
	doc := sessiondoc.New()
	doc.CurrentWorkspace = "main"
	doc.WorkspaceOrder = []string{"main"}
	doc.Workspaces["main"] = &sessiondoc.Workspace{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*sessiondoc.Tab{{
			ID:           "1",
			Name:         "1",
			Root:         sessiondoc.NewLeaf("1"),
			Panes:        map[string]*sessiondoc.Pane{"1": {ID: "1"}},
			ActivePaneID: "1",
		}},
	}
	return doc
}

func currentWorkspace(doc *sessiondoc.Doc) *sessiondoc.Workspace {
	if doc == nil {
		return nil
	}
	if doc.CurrentWorkspace != "" && doc.Workspaces[doc.CurrentWorkspace] != nil {
		return doc.Workspaces[doc.CurrentWorkspace]
	}
	for _, name := range doc.WorkspaceOrder {
		if doc.Workspaces[name] != nil {
			return doc.Workspaces[name]
		}
	}
	for _, ws := range doc.Workspaces {
		return ws
	}
	return nil
}

func currentTab(ws *sessiondoc.Workspace) *sessiondoc.Tab {
	if ws == nil || len(ws.Tabs) == 0 {
		return nil
	}
	if ws.ActiveTab >= 0 && ws.ActiveTab < len(ws.Tabs) && ws.Tabs[ws.ActiveTab] != nil {
		return ws.Tabs[ws.ActiveTab]
	}
	for _, tab := range ws.Tabs {
		if tab != nil {
			return tab
		}
	}
	return nil
}

func activePaneID(tab *sessiondoc.Tab) string {
	if tab == nil {
		return ""
	}
	if tab.ActivePaneID != "" && tab.Panes[tab.ActivePaneID] != nil {
		return tab.ActivePaneID
	}
	if tab.Root != nil {
		for _, paneID := range tab.Root.LeafIDs() {
			if tab.Panes[paneID] != nil {
				return paneID
			}
		}
	}
	for paneID := range tab.Panes {
		return paneID
	}
	return ""
}

func stateKey(sessionID string) string {
	return sessionKey(sessionID) + stateKeySuffix
}

func viewKey(sessionID, viewID string) string {
	return sessionKey(sessionID) + viewKeyPart + strings.TrimSpace(viewID)
}

func leaseKey(sessionID, terminalID string) string {
	return leasesPrefix(sessionID) + strings.TrimSpace(terminalID)
}

func leasesPrefix(sessionID string) string {
	return sessionKey(sessionID) + leaseKeyPart
}

func sessionKey(sessionID string) string {
	return sessionPrefix + strings.TrimSpace(sessionID)
}

func parseSessionKey(key string) (sessionID string, kind string, ok bool) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, sessionPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, sessionPrefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" {
		return "", "", false
	}
	switch parts[1] {
	case "state", "views", "leases":
		return parts[0], parts[1], true
	default:
		return "", "", false
	}
}

func viewIDFromKey(key string) string {
	needle := viewKeyPart
	idx := strings.Index(key, needle)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(key[idx+len(needle):])
}

func newViewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "view-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("view-%d", time.Now().UTC().UnixNano())
}

func isProtocolNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "protocol error 404")
}

func isProtocolConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "protocol error 409")
}
