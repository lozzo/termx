package app

import (
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/terminalcontrol"
)

func (m *Model) terminalControlManager() *terminalcontrol.Manager {
	if m == nil || m.runtime == nil {
		return nil
	}
	return terminalcontrol.NewManager(m.runtime, terminalcontrol.SessionLeaseHooks{
		SessionID:    m.sessionID,
		ViewID:       m.sessionViewID,
		NeedsAcquire: m.implicitSessionLeaseNeedsAcquire,
		Store: func(lease protocol.LeaseInfo) {
			if service := m.sessionRuntimeService(); service != nil {
				service.storeLease(lease)
			}
		},
		Remove: func(terminalID string) {
			if service := m.sessionRuntimeService(); service != nil {
				service.removeLease(terminalID)
			}
		},
		Apply: func() {
			if service := m.sessionRuntimeService(); service != nil {
				service.applyCurrentLeases()
			}
		},
	})
}
