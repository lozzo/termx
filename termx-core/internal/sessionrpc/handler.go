package sessionrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/workbenchsvc"
)

type PublishHooks struct {
	SessionCreated func(sessionID string, revision uint64)
	SessionUpdated func(sessionID string, revision uint64, viewID string)
	SessionDeleted func(sessionID string)
}

type Handler struct {
	workbench *workbenchsvc.Service
	publish   PublishHooks
}

func New(workbench *workbenchsvc.Service, hooks PublishHooks) *Handler {
	return &Handler{
		workbench: workbench,
		publish:   hooks,
	}
}

func (h *Handler) Handle(ctx context.Context, remote string, req protocol.Request) (json.RawMessage, int, error) {
	if h == nil || h.workbench == nil {
		return nil, 500, fmt.Errorf("workbench service unavailable")
	}
	switch req.Method {
	case "session.create":
		var params protocol.CreateSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		session, err := h.workbench.CreateSession(workbenchsvc.CreateSessionOptions{
			ID:   params.SessionID,
			Name: params.Name,
		})
		if err != nil {
			return nil, 409, err
		}
		result, err := json.Marshal(protocol.SessionSnapshot{
			Session:   sessionInfoFromState(session),
			Workbench: session.Doc.Clone(),
		})
		if err != nil {
			return nil, 500, err
		}
		if h.publish.SessionCreated != nil {
			h.publish.SessionCreated(session.ID, session.Revision)
		}
		return result, 0, nil
	case "session.list":
		sessions := h.workbench.ListSessions()
		items := make([]protocol.SessionInfo, 0, len(sessions))
		for _, session := range sessions {
			items = append(items, sessionInfoFromState(session))
		}
		result, err := json.Marshal(protocol.ListSessionsResult{Sessions: items})
		if err != nil {
			return nil, 500, err
		}
		return result, 0, nil
	case "session.get":
		var params protocol.GetSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		snapshot, err := h.workbench.GetSession(params.SessionID)
		if err != nil {
			return nil, 404, err
		}
		result, err := json.Marshal(sessionSnapshotFromState(snapshot))
		if err != nil {
			return nil, 500, err
		}
		return result, 0, nil
	case "session.delete":
		var params protocol.GetSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := h.workbench.DeleteSession(params.SessionID); err != nil {
			return nil, 404, err
		}
		if h.publish.SessionDeleted != nil {
			h.publish.SessionDeleted(params.SessionID)
		}
		return json.RawMessage(`{}`), 0, nil
	case "session.attach":
		var params protocol.AttachSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		clientID := params.ClientID
		if clientID == "" {
			clientID = remote
		}
		snapshot, err := h.workbench.AttachSession(params.SessionID, workbenchsvc.AttachSessionOptions{
			ClientID:   clientID,
			WindowCols: params.WindowCols,
			WindowRows: params.WindowRows,
		})
		if err != nil {
			return nil, 404, err
		}
		result, err := json.Marshal(sessionSnapshotFromState(snapshot))
		if err != nil {
			return nil, 500, err
		}
		return result, 0, nil
	case "session.detach":
		var params protocol.DetachSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := h.workbench.DetachSession(params.SessionID, params.ViewID); err != nil {
			return nil, 404, err
		}
		return json.RawMessage(`{}`), 0, nil
	case "session.apply":
		var params protocol.ApplySessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		snapshot, err := h.workbench.Apply(params.SessionID, workbenchsvc.ApplyRequest{
			ViewID:       params.ViewID,
			BaseRevision: params.BaseRevision,
			Ops:          params.Ops,
		})
		if err != nil {
			if strings.Contains(err.Error(), "revision conflict") {
				return nil, 409, err
			}
			return nil, 400, err
		}
		result, err := json.Marshal(sessionSnapshotFromState(snapshot))
		if err != nil {
			return nil, 500, err
		}
		if h.publish.SessionUpdated != nil {
			h.publish.SessionUpdated(snapshot.Session.ID, snapshot.Session.Revision, params.ViewID)
		}
		return result, 0, nil
	case "session.replace":
		var params protocol.ReplaceSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		snapshot, err := h.workbench.Replace(params.SessionID, workbenchsvc.ReplaceRequest{
			ViewID:       params.ViewID,
			BaseRevision: params.BaseRevision,
			Doc:          params.Workbench,
		})
		if err != nil {
			if strings.Contains(err.Error(), "revision conflict") {
				return nil, 409, err
			}
			return nil, 400, err
		}
		result, err := json.Marshal(sessionSnapshotFromState(snapshot))
		if err != nil {
			return nil, 500, err
		}
		if h.publish.SessionUpdated != nil {
			h.publish.SessionUpdated(snapshot.Session.ID, snapshot.Session.Revision, params.ViewID)
		}
		return result, 0, nil
	case "session.view_update":
		var params protocol.UpdateSessionViewParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		view, err := h.workbench.UpdateView(params.SessionID, params.ViewID, workbenchsvc.UpdateViewRequest{
			ActiveWorkspaceName: params.View.ActiveWorkspaceName,
			ActiveTabID:         params.View.ActiveTabID,
			FocusedPaneID:       params.View.FocusedPaneID,
			WindowCols:          params.View.WindowCols,
			WindowRows:          params.View.WindowRows,
		})
		if err != nil {
			return nil, 404, err
		}
		result, err := json.Marshal(viewInfoFromState(view))
		if err != nil {
			return nil, 500, err
		}
		return result, 0, nil
	case "session.acquire_lease":
		var params protocol.AcquireSessionLeaseParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		lease, err := h.workbench.AcquireLease(params.SessionID, params.ViewID, workbenchsvc.AcquireLeaseRequest{
			PaneID:     params.PaneID,
			TerminalID: params.TerminalID,
		})
		if err != nil {
			return nil, 404, err
		}
		snapshot, err := h.workbench.GetSession(params.SessionID)
		if err != nil {
			return nil, 404, err
		}
		result, err := json.Marshal(protocol.LeaseInfo{
			TerminalID: lease.TerminalID,
			SessionID:  lease.SessionID,
			ViewID:     lease.ViewID,
			PaneID:     lease.PaneID,
			AcquiredAt: lease.AcquiredAt,
		})
		if err != nil {
			return nil, 500, err
		}
		if h.publish.SessionUpdated != nil {
			h.publish.SessionUpdated(params.SessionID, snapshot.Session.Revision, params.ViewID)
		}
		return result, 0, nil
	case "session.release_lease":
		var params protocol.ReleaseSessionLeaseParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, 400, err
		}
		if err := h.workbench.ReleaseLease(params.SessionID, params.ViewID, workbenchsvc.ReleaseLeaseRequest{
			TerminalID: params.TerminalID,
		}); err != nil {
			return nil, 404, err
		}
		snapshot, err := h.workbench.GetSession(params.SessionID)
		if err != nil {
			return nil, 404, err
		}
		if h.publish.SessionUpdated != nil {
			h.publish.SessionUpdated(params.SessionID, snapshot.Session.Revision, params.ViewID)
		}
		return json.RawMessage(`{}`), 0, nil
	default:
		return nil, 400, fmt.Errorf("unknown session method: %s", req.Method)
	}
}

func sessionSnapshotFromState(snapshot *workbenchsvc.SessionSnapshot) protocol.SessionSnapshot {
	out := protocol.SessionSnapshot{}
	if snapshot == nil {
		return out
	}
	if snapshot.Session != nil {
		out.Session = sessionInfoFromState(snapshot.Session)
		out.Workbench = snapshot.Session.Doc.Clone()
	}
	if snapshot.View != nil {
		view := viewInfoFromState(snapshot.View)
		out.View = &view
	}
	if len(snapshot.Leases) > 0 {
		out.Leases = make([]protocol.LeaseInfo, 0, len(snapshot.Leases))
		for _, lease := range snapshot.Leases {
			if lease == nil {
				continue
			}
			out.Leases = append(out.Leases, protocol.LeaseInfo{
				TerminalID: lease.TerminalID,
				SessionID:  lease.SessionID,
				ViewID:     lease.ViewID,
				PaneID:     lease.PaneID,
				AcquiredAt: lease.AcquiredAt,
			})
		}
	}
	return out
}

func sessionInfoFromState(session *workbenchsvc.SessionState) protocol.SessionInfo {
	if session == nil {
		return protocol.SessionInfo{}
	}
	return protocol.SessionInfo{
		ID:        session.ID,
		Name:      session.Name,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Revision:  session.Revision,
	}
}

func viewInfoFromState(view *workbenchsvc.ViewState) protocol.ViewInfo {
	if view == nil {
		return protocol.ViewInfo{}
	}
	return protocol.ViewInfo{
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
