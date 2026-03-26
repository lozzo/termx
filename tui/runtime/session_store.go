package runtime

import (
	"sync"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/core/types"
)

type Session struct {
	TerminalID types.TerminalID
	Channel    uint16
	ReadOnly   bool
	Preview    bool
	Snapshot   *protocol.Snapshot
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[types.TerminalID]Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[types.TerminalID]Session)}
}

func (s *SessionStore) Upsert(session Session) {
	if s == nil || session.TerminalID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.TerminalID] = session
}

func (s *SessionStore) ApplySnapshot(terminalID types.TerminalID, snapshot *protocol.Snapshot) {
	if s == nil || terminalID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[terminalID]
	session.TerminalID = terminalID
	session.Snapshot = snapshot
	s.sessions[terminalID] = session
}

func (s *SessionStore) Session(terminalID types.TerminalID) (Session, bool) {
	if s == nil {
		return Session{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[terminalID]
	return session, ok
}

func (s *SessionStore) Remove(terminalID types.TerminalID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, terminalID)
}
