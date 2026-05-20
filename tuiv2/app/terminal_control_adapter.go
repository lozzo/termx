package app

import (
	"github.com/lozzow/termx/tuiv2/sessionstore"
	"github.com/lozzow/termx/tuiv2/terminalcontrol"
)

func (m *Model) terminalControlManager() *terminalcontrol.Manager {
	if m == nil || m.runtime == nil {
		return nil
	}
	hooks := terminalcontrol.SessionLeaseHooks{
		SessionID:    m.sessionID,
		ViewID:       m.sessionViewID,
		NeedsAcquire: m.implicitSessionLeaseNeedsAcquire,
		Store: func(lease sessionstore.LeaseInfo) {
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
	}
	if m.sessionStore != nil {
		hooks.Acquire = m.sessionStore.AcquireSessionLease
		hooks.Release = m.sessionStore.ReleaseSessionLease
	}
	return terminalcontrol.NewManager(m.runtime, hooks)
}
