package runtime

import (
	"sync"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/types"
)

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[types.TerminalID]app.TerminalSession
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[types.TerminalID]app.TerminalSession)}
}

func (s *SessionStore) Bind(terminalID types.TerminalID, channel uint16, snapshot *protocol.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[terminalID] = app.TerminalSession{
		TerminalID: terminalID,
		Channel:    channel,
		Attached:   true,
		Snapshot:   snapshot,
	}
}

func (s *SessionStore) Session(terminalID types.TerminalID) (app.TerminalSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[terminalID]
	return session, ok
}
