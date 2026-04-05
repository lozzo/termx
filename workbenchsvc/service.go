package workbenchsvc

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/workbenchdoc"
	"github.com/lozzow/termx/workbenchops"
)

type Service struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
	views    map[string]*ViewState
	leases   map[string]*LeaseState
	nextView atomic.Uint64
}

type SessionState struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  uint64
	Doc       *workbenchdoc.Doc
}

type ViewState struct {
	ViewID              string
	SessionID           string
	ClientID            string
	ActiveWorkspaceName string
	ActiveTabID         string
	FocusedPaneID       string
	WindowCols          uint16
	WindowRows          uint16
	AttachedAt          time.Time
	UpdatedAt           time.Time
}

type LeaseState struct {
	TerminalID string
	SessionID  string
	ViewID     string
	PaneID     string
	AcquiredAt time.Time
}

type CreateSessionOptions struct {
	ID   string
	Name string
}

type AttachSessionOptions struct {
	ClientID   string
	WindowCols uint16
	WindowRows uint16
}

type UpdateViewRequest struct {
	ActiveWorkspaceName string
	ActiveTabID         string
	FocusedPaneID       string
	WindowCols          uint16
	WindowRows          uint16
}

type ApplyRequest struct {
	ViewID       string
	BaseRevision uint64
	Ops          []workbenchops.Op
}

type ReplaceRequest struct {
	ViewID       string
	BaseRevision uint64
	Doc          *workbenchdoc.Doc
}

type AcquireLeaseRequest struct {
	PaneID     string
	TerminalID string
}

type ReleaseLeaseRequest struct {
	TerminalID string
}

type SessionSnapshot struct {
	Session *SessionState
	View    *ViewState
	Leases  []*LeaseState
}

func New() *Service {
	return &Service{
		sessions: make(map[string]*SessionState),
		views:    make(map[string]*ViewState),
		leases:   make(map[string]*LeaseState),
	}
}

func (s *Service) CreateSession(opts CreateSessionOptions) (*SessionState, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	now := time.Now().UTC()
	sessionID := opts.ID
	if sessionID == "" {
		sessionID = "main"
	}
	name := opts.Name
	if name == "" {
		name = sessionID
	}
	doc := workbenchdoc.New()
	doc.CurrentWorkspace = "main"
	doc.WorkspaceOrder = []string{"main"}
	doc.Workspaces["main"] = &workbenchdoc.Workspace{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbenchdoc.Tab{{
			ID:           "1",
			Name:         "1",
			Root:         workbenchdoc.NewLeaf("1"),
			Panes:        map[string]*workbenchdoc.Pane{"1": {ID: "1"}},
			ActivePaneID: "1",
		}},
	}
	state := &SessionState{
		ID:        sessionID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
		Doc:       doc,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[sessionID] != nil {
		return nil, fmt.Errorf("workbenchsvc: session %q already exists", sessionID)
	}
	s.sessions[sessionID] = state
	return cloneSessionState(state), nil
}

func (s *Service) ListSessions() []*SessionState {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SessionState, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, cloneSessionState(session))
	}
	return out
}

func (s *Service) GetSession(sessionID string) (*SessionSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return nil, fmt.Errorf("workbenchsvc: session %q not found", sessionID)
	}
	return &SessionSnapshot{
		Session: cloneSessionState(session),
		Leases:  s.cloneSessionLeasesLocked(sessionID),
	}, nil
}

func (s *Service) DeleteSession(sessionID string) error {
	if s == nil {
		return fmt.Errorf("workbenchsvc: nil service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[sessionID] == nil {
		return fmt.Errorf("workbenchsvc: session %q not found", sessionID)
	}
	delete(s.sessions, sessionID)
	for viewID, view := range s.views {
		if view != nil && view.SessionID == sessionID {
			delete(s.views, viewID)
		}
	}
	for terminalID, lease := range s.leases {
		if lease != nil && lease.SessionID == sessionID {
			delete(s.leases, terminalID)
		}
	}
	return nil
}

func (s *Service) AttachSession(sessionID string, opts AttachSessionOptions) (*SessionSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		return nil, fmt.Errorf("workbenchsvc: session %q not found", sessionID)
	}
	now := time.Now().UTC()
	viewID := "view-" + strconv.FormatUint(s.nextView.Add(1), 10)
	view := &ViewState{
		ViewID:              viewID,
		SessionID:           sessionID,
		ClientID:            opts.ClientID,
		ActiveWorkspaceName: session.Doc.CurrentWorkspace,
		WindowCols:          opts.WindowCols,
		WindowRows:          opts.WindowRows,
		AttachedAt:          now,
		UpdatedAt:           now,
	}
	if ws := currentWorkspace(session.Doc); ws != nil {
		if tab := currentTab(ws); tab != nil {
			view.ActiveTabID = tab.ID
			view.FocusedPaneID = activePaneID(tab)
		}
	}
	s.views[viewID] = view
	return &SessionSnapshot{
		Session: cloneSessionState(session),
		View:    cloneViewState(view),
		Leases:  s.cloneSessionLeasesLocked(sessionID),
	}, nil
}

func (s *Service) DetachSession(sessionID, viewID string) error {
	if s == nil {
		return fmt.Errorf("workbenchsvc: nil service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.views[viewID]
	if view == nil || view.SessionID != sessionID {
		return fmt.Errorf("workbenchsvc: view %q not found in session %q", viewID, sessionID)
	}
	delete(s.views, viewID)
	for terminalID, lease := range s.leases {
		if lease != nil && lease.SessionID == sessionID && lease.ViewID == viewID {
			delete(s.leases, terminalID)
		}
	}
	return nil
}

func (s *Service) Apply(sessionID string, req ApplyRequest) (*SessionSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		return nil, fmt.Errorf("workbenchsvc: session %q not found", sessionID)
	}
	if session.Revision != req.BaseRevision {
		return nil, fmt.Errorf("workbenchsvc: session revision conflict: expected %d, got %d", req.BaseRevision, session.Revision)
	}
	nextDoc, err := workbenchops.Apply(session.Doc, req.Ops)
	if err != nil {
		return nil, err
	}
	session.Doc = nextDoc
	session.Revision++
	session.UpdatedAt = time.Now().UTC()
	return &SessionSnapshot{
		Session: cloneSessionState(session),
		Leases:  s.cloneSessionLeasesLocked(sessionID),
	}, nil
}

func (s *Service) Replace(sessionID string, req ReplaceRequest) (*SessionSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	if req.Doc == nil {
		return nil, fmt.Errorf("workbenchsvc: replacement document is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		return nil, fmt.Errorf("workbenchsvc: session %q not found", sessionID)
	}
	if session.Revision != req.BaseRevision {
		return nil, fmt.Errorf("workbenchsvc: session revision conflict: expected %d, got %d", req.BaseRevision, session.Revision)
	}
	session.Doc = req.Doc.Clone()
	session.Revision++
	session.UpdatedAt = time.Now().UTC()
	return &SessionSnapshot{
		Session: cloneSessionState(session),
		Leases:  s.cloneSessionLeasesLocked(sessionID),
	}, nil
}

func (s *Service) UpdateView(sessionID, viewID string, req UpdateViewRequest) (*ViewState, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.views[viewID]
	if view == nil || view.SessionID != sessionID {
		return nil, fmt.Errorf("workbenchsvc: view %q not found in session %q", viewID, sessionID)
	}
	if req.ActiveWorkspaceName != "" {
		view.ActiveWorkspaceName = req.ActiveWorkspaceName
	}
	if req.ActiveTabID != "" {
		view.ActiveTabID = req.ActiveTabID
	}
	if req.FocusedPaneID != "" {
		view.FocusedPaneID = req.FocusedPaneID
	}
	if req.WindowCols > 0 {
		view.WindowCols = req.WindowCols
	}
	if req.WindowRows > 0 {
		view.WindowRows = req.WindowRows
	}
	view.UpdatedAt = time.Now().UTC()
	return cloneViewState(view), nil
}

func (s *Service) AcquireLease(sessionID, viewID string, req AcquireLeaseRequest) (*LeaseState, error) {
	if s == nil {
		return nil, fmt.Errorf("workbenchsvc: nil service")
	}
	if req.TerminalID == "" {
		return nil, fmt.Errorf("workbenchsvc: terminal id is required")
	}
	if req.PaneID == "" {
		return nil, fmt.Errorf("workbenchsvc: pane id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.views[viewID]
	if view == nil || view.SessionID != sessionID {
		return nil, fmt.Errorf("workbenchsvc: view %q not found in session %q", viewID, sessionID)
	}
	if s.sessions[sessionID] == nil {
		return nil, fmt.Errorf("workbenchsvc: session %q not found", sessionID)
	}
	lease := &LeaseState{
		TerminalID: req.TerminalID,
		SessionID:  sessionID,
		ViewID:     viewID,
		PaneID:     req.PaneID,
		AcquiredAt: time.Now().UTC(),
	}
	s.leases[req.TerminalID] = lease
	return cloneLeaseState(lease), nil
}

func (s *Service) ReleaseLease(sessionID, viewID string, req ReleaseLeaseRequest) error {
	if s == nil {
		return fmt.Errorf("workbenchsvc: nil service")
	}
	if req.TerminalID == "" {
		return fmt.Errorf("workbenchsvc: terminal id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.views[viewID]
	if view == nil || view.SessionID != sessionID {
		return fmt.Errorf("workbenchsvc: view %q not found in session %q", viewID, sessionID)
	}
	lease := s.leases[req.TerminalID]
	if lease == nil {
		return nil
	}
	if lease.SessionID != sessionID {
		return fmt.Errorf("workbenchsvc: terminal %q lease is not in session %q", req.TerminalID, sessionID)
	}
	if lease.ViewID != viewID {
		return nil
	}
	delete(s.leases, req.TerminalID)
	return nil
}

func cloneSessionState(session *SessionState) *SessionState {
	if session == nil {
		return nil
	}
	return &SessionState{
		ID:        session.ID,
		Name:      session.Name,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Revision:  session.Revision,
		Doc:       session.Doc.Clone(),
	}
}

func cloneViewState(view *ViewState) *ViewState {
	if view == nil {
		return nil
	}
	return &ViewState{
		ViewID:              view.ViewID,
		SessionID:           view.SessionID,
		ClientID:            view.ClientID,
		ActiveWorkspaceName: view.ActiveWorkspaceName,
		ActiveTabID:         view.ActiveTabID,
		FocusedPaneID:       view.FocusedPaneID,
		WindowCols:          view.WindowCols,
		WindowRows:          view.WindowRows,
		AttachedAt:          view.AttachedAt,
		UpdatedAt:           view.UpdatedAt,
	}
}

func cloneLeaseState(lease *LeaseState) *LeaseState {
	if lease == nil {
		return nil
	}
	return &LeaseState{
		TerminalID: lease.TerminalID,
		SessionID:  lease.SessionID,
		ViewID:     lease.ViewID,
		PaneID:     lease.PaneID,
		AcquiredAt: lease.AcquiredAt,
	}
}

func (s *Service) cloneSessionLeasesLocked(sessionID string) []*LeaseState {
	out := make([]*LeaseState, 0, len(s.leases))
	for _, lease := range s.leases {
		if lease != nil && lease.SessionID == sessionID {
			out = append(out, cloneLeaseState(lease))
		}
	}
	return out
}

func currentWorkspace(doc *workbenchdoc.Doc) *workbenchdoc.Workspace {
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

func currentTab(ws *workbenchdoc.Workspace) *workbenchdoc.Tab {
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

func activePaneID(tab *workbenchdoc.Tab) string {
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
