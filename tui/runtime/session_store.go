package runtime

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/state/types"
)

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[types.TerminalID]app.TerminalSession
	preview  PreviewBinding
}

type PreviewBinding struct {
	TerminalID types.TerminalID
	Channel    uint16
	Revision   int
	stream     <-chan protocol.StreamFrame
	cancel     func()
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

// BindPreview 专门记录 pool preview 的只读订阅。
// revision 递增用来明确“切换选中项后已经触发了一次新的 preview 订阅”。
func (s *SessionStore) BindPreview(terminalID types.TerminalID, channel uint16, snapshot *protocol.Snapshot, stream <-chan protocol.StreamFrame, cancel func()) PreviewBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview.cancel != nil {
		s.preview.cancel()
	}
	s.preview = PreviewBinding{
		TerminalID: terminalID,
		Channel:    channel,
		Revision:   s.preview.Revision + 1,
		stream:     stream,
		cancel:     cancel,
	}
	s.sessions[terminalID] = app.TerminalSession{
		TerminalID: terminalID,
		Channel:    channel,
		Attached:   true,
		ReadOnly:   true,
		Preview:    true,
		Snapshot:   snapshot,
	}
	return s.preview
}

func (s *SessionStore) Session(terminalID types.TerminalID) (app.TerminalSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[terminalID]
	return session, ok
}

func (s *SessionStore) ActivePreview() PreviewBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preview
}

func (s *SessionStore) CancelPreview() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview.cancel != nil {
		s.preview.cancel()
	}
	s.preview = PreviewBinding{}
}

func (s *SessionStore) NextPreviewMessageCmd() tea.Cmd {
	s.mu.RLock()
	binding := s.preview
	s.mu.RUnlock()
	if binding.stream == nil {
		return nil
	}
	return func() tea.Msg {
		frame, ok := <-binding.stream
		if !ok {
			return app.PreviewStreamClosedMessage{
				TerminalID: binding.TerminalID,
				Revision:   binding.Revision,
			}
		}
		return app.PreviewStreamMessage{
			TerminalID: binding.TerminalID,
			Revision:   binding.Revision,
			Frame:      frame,
		}
	}
}
