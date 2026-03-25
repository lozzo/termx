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
	live     map[types.TerminalID]LiveBinding
	preview  PreviewBinding
	messages chan tea.Msg
}

type PreviewBinding struct {
	TerminalID types.TerminalID
	Channel    uint16
	Revision   int
	stream     <-chan protocol.StreamFrame
	cancel     func()
}

type LiveBinding struct {
	TerminalID types.TerminalID
	Channel    uint16
	stream     <-chan protocol.StreamFrame
	cancel     func()
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[types.TerminalID]app.TerminalSession),
		live:     make(map[types.TerminalID]LiveBinding),
		messages: make(chan tea.Msg, 256),
	}
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

func (s *SessionStore) BindLive(terminalID types.TerminalID, channel uint16, snapshot *protocol.Snapshot, stream <-chan protocol.StreamFrame, cancel func()) LiveBinding {
	s.mu.Lock()
	if existing, ok := s.live[terminalID]; ok && existing.cancel != nil {
		existing.cancel()
	}
	binding := LiveBinding{
		TerminalID: terminalID,
		Channel:    channel,
		stream:     stream,
		cancel:     cancel,
	}
	s.live[terminalID] = binding
	s.sessions[terminalID] = app.TerminalSession{
		TerminalID: terminalID,
		Channel:    channel,
		Attached:   true,
		Snapshot:   snapshot,
	}
	s.mu.Unlock()

	if stream != nil {
		go s.forwardLive(binding)
	}
	return binding
}

// BindPreview 专门记录 pool preview 的只读订阅。
// revision 递增用来明确“切换选中项后已经触发了一次新的 preview 订阅”。
func (s *SessionStore) BindPreview(terminalID types.TerminalID, channel uint16, snapshot *protocol.Snapshot, stream <-chan protocol.StreamFrame, cancel func()) PreviewBinding {
	return s.bindPreview(terminalID, channel, snapshot, stream, cancel, 0, false)
}

// BindPreviewAtRevision 给恢复路径使用，确保磁盘里的 preview revision 与 live stream binding 对齐。
func (s *SessionStore) BindPreviewAtRevision(terminalID types.TerminalID, channel uint16, snapshot *protocol.Snapshot, stream <-chan protocol.StreamFrame, cancel func(), revision int) PreviewBinding {
	return s.bindPreview(terminalID, channel, snapshot, stream, cancel, revision, true)
}

func (s *SessionStore) bindPreview(terminalID types.TerminalID, channel uint16, snapshot *protocol.Snapshot, stream <-chan protocol.StreamFrame, cancel func(), revision int, fixedRevision bool) PreviewBinding {
	s.mu.Lock()
	if s.preview.cancel != nil {
		s.preview.cancel()
	}
	if !fixedRevision {
		revision = s.preview.Revision + 1
	}
	s.preview = PreviewBinding{
		TerminalID: terminalID,
		Channel:    channel,
		Revision:   revision,
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
	binding := s.preview
	s.mu.Unlock()

	if stream != nil {
		go s.forwardPreview(binding)
	}
	return binding
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

func (s *SessionStore) HasActiveStreams() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preview.stream != nil || len(s.live) > 0
}

func (s *SessionStore) NextStreamMessageCmd() tea.Cmd {
	s.mu.RLock()
	active := s.preview.stream != nil || len(s.live) > 0
	s.mu.RUnlock()
	if !active {
		return nil
	}
	return func() tea.Msg {
		return <-s.messages
	}
}

func (s *SessionStore) forwardPreview(binding PreviewBinding) {
	for frame := range binding.stream {
		s.messages <- app.PreviewStreamMessage{
			TerminalID: binding.TerminalID,
			Revision:   binding.Revision,
			Frame:      frame,
		}
	}
	s.mu.Lock()
	if s.preview.TerminalID == binding.TerminalID && s.preview.Channel == binding.Channel && s.preview.Revision == binding.Revision {
		s.preview = PreviewBinding{}
	}
	s.mu.Unlock()
	s.messages <- app.PreviewStreamClosedMessage{
		TerminalID: binding.TerminalID,
		Revision:   binding.Revision,
	}
}

func (s *SessionStore) forwardLive(binding LiveBinding) {
	for frame := range binding.stream {
		s.messages <- app.LiveStreamMessage{
			TerminalID: binding.TerminalID,
			Frame:      frame,
		}
	}
	s.mu.Lock()
	if current, ok := s.live[binding.TerminalID]; ok && current.Channel == binding.Channel {
		delete(s.live, binding.TerminalID)
	}
	s.mu.Unlock()
	s.messages <- app.LiveStreamClosedMessage{
		TerminalID: binding.TerminalID,
	}
}
