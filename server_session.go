package termx

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lozzow/termx/internal/sessionrpc"
	"github.com/lozzow/termx/protocol"
)

type sessionRequestHandler interface {
	Handle(ctx context.Context, remote string, req protocol.Request) (json.RawMessage, int, error)
}

func (s *Server) handleSessionRequest(ctx context.Context, remote string, req protocol.Request) (json.RawMessage, int, error) {
	if s == nil || s.sessionHandler == nil {
		return nil, 500, fmt.Errorf("workbench service unavailable")
	}
	return s.sessionHandler.Handle(ctx, remote, req)
}

func (s *Server) publishSessionEvent(eventType EventType, sessionID string, revision uint64, viewID string) {
	if s == nil || s.events == nil || sessionID == "" {
		return
	}
	s.events.Publish(Event{
		Type:      eventType,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Session: &SessionEventData{
			Revision: revision,
			ViewID:   viewID,
		},
	})
}

func newSessionHandler(s *Server) *sessionrpc.Handler {
	if s == nil {
		return nil
	}
	return sessionrpc.New(s.workbench, sessionrpc.PublishHooks{
		SessionCreated: func(sessionID string, revision uint64) {
			s.publishSessionEvent(EventSessionCreated, sessionID, revision, "")
		},
		SessionUpdated: func(sessionID string, revision uint64, viewID string) {
			s.publishSessionEvent(EventSessionUpdated, sessionID, revision, viewID)
		},
		SessionDeleted: func(sessionID string) {
			s.publishSessionEvent(EventSessionDeleted, sessionID, 0, "")
		},
	})
}
