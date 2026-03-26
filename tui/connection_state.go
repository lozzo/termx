package tui

import (
	"slices"
	"strings"
)

type terminalConnectionSnapshot struct {
	TerminalID  string
	paneIDs     []string
	panes       map[string]*Pane
	ownerPaneID string
}

func buildTerminalConnectionSnapshot(tabs []*Tab, terminalID string) (terminalConnectionSnapshot, bool) {
	terminalID = strings.TrimSpace(terminalID)
	if terminalID == "" {
		return terminalConnectionSnapshot{}, false
	}
	snapshot := terminalConnectionSnapshot{
		TerminalID: terminalID,
		panes:      make(map[string]*Pane),
	}
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		ids := make([]string, 0, len(tab.Panes))
		for id, pane := range tab.Panes {
			if pane == nil || strings.TrimSpace(pane.TerminalID) != terminalID {
				continue
			}
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			pane := tab.Panes[id]
			snapshot.paneIDs = append(snapshot.paneIDs, id)
			snapshot.panes[id] = pane
			if snapshot.ownerPaneID == "" && pane.IsResizeAcquired() {
				snapshot.ownerPaneID = id
			}
		}
	}
	if len(snapshot.paneIDs) == 0 {
		return terminalConnectionSnapshot{}, false
	}
	return snapshot, true
}

func (s terminalConnectionSnapshot) PaneCount() int {
	return len(s.paneIDs)
}

func (s terminalConnectionSnapshot) ResolvedOwnerID() string {
	if len(s.paneIDs) == 0 {
		return ""
	}
	if s.ownerPaneID != "" {
		return s.ownerPaneID
	}
	return s.paneIDs[0]
}

func (s terminalConnectionSnapshot) PreferredOwnerID(excludePaneID string) string {
	ownerID := s.ResolvedOwnerID()
	if ownerID != "" && ownerID != excludePaneID {
		return ownerID
	}
	for _, paneID := range s.paneIDs {
		if paneID != excludePaneID {
			return paneID
		}
	}
	return ""
}

func (s terminalConnectionSnapshot) PaneShouldSubmitResize(paneID string) bool {
	if _, ok := s.panes[paneID]; !ok {
		return false
	}
	if s.PaneCount() <= 1 {
		return true
	}
	return s.ResolvedOwnerID() == paneID
}

func (s terminalConnectionSnapshot) StatusForPane(paneID string) string {
	if _, ok := s.panes[paneID]; !ok {
		return ""
	}
	if s.PaneShouldSubmitResize(paneID) {
		return "owner"
	}
	return "follower"
}

func (s terminalConnectionSnapshot) ApplyOwner(preferredPaneID string) {
	targetID := preferredPaneID
	if _, ok := s.panes[targetID]; !ok {
		targetID = s.ResolvedOwnerID()
	}
	if targetID == "" {
		return
	}
	for _, paneID := range s.paneIDs {
		s.panes[paneID].SetResizeAcquired(paneID == targetID)
	}
}
