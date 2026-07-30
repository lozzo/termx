package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/shared/transport"
)

var ErrGrantTransportInactive = errors.New("grant transport is not active")

type transportCloseReason uint8

const (
	transportCloseNormal transportCloseReason = iota + 1
	transportCloseRevoked
	transportCloseExpired
	transportCloseShutdown
)

type trackedTransport struct {
	transport.Transport
	closeOnce sync.Once
	closeErr  error
	reason    transportCloseReason
}

func (tracked *trackedTransport) Close() error {
	return tracked.closeWithReason(transportCloseNormal)
}

func (tracked *trackedTransport) closeWithReason(reason transportCloseReason) error {
	if tracked == nil {
		return nil
	}
	tracked.closeOnce.Do(func() {
		tracked.reason = reason
		tracked.closeErr = tracked.Transport.Close()
	})
	return tracked.closeErr
}

type grantTimer interface {
	Stop() bool
}

type grantTimerEntry struct {
	expiresAt time.Time
	timer     grantTimer
}

func (server *Server) admitTransport(ctx context.Context, connection transport.Transport, scope TransportScope) (*trackedTransport, error) {
	tracked := &trackedTransport{Transport: connection}
	if scope.LocalOwner {
		server.grantMu.Lock()
		server.trackTransportLocked(tracked)
		server.grantMu.Unlock()
		return tracked, nil
	}

	now := server.grantNow().UTC()
	service := server.ClientAccessService()
	server.grantMu.Lock()
	if _, denied := server.grantTombstones[scope.GrantID]; denied || !now.Before(scope.GrantExpiresAt) {
		if !now.Before(scope.GrantExpiresAt) {
			server.grantTombstones[scope.GrantID] = transportCloseExpired
		}
		server.grantMu.Unlock()
		_ = tracked.closeWithReason(transportCloseExpired)
		return nil, ErrGrantTransportInactive
	}
	if service == nil || !service.GrantActive(ctx, scope.GrantID, scope.GrantExpiresAt, now) {
		server.grantMu.Unlock()
		_ = tracked.closeWithReason(transportCloseRevoked)
		return nil, ErrGrantTransportInactive
	}
	server.trackTransportLocked(tracked)
	transports := server.grantTransports[scope.GrantID]
	if transports == nil {
		transports = make(map[*trackedTransport]struct{})
		server.grantTransports[scope.GrantID] = transports
	}
	transports[tracked] = struct{}{}
	server.transportGrants[tracked] = scope.GrantID
	server.scheduleGrantExpiryLocked(scope.GrantID, scope.GrantExpiresAt, now)
	server.grantMu.Unlock()
	return tracked, nil
}

func (server *Server) trackTransportLocked(tracked *trackedTransport) {
	server.mu.Lock()
	server.transports[tracked] = struct{}{}
	server.mu.Unlock()
}

func (server *Server) untrackTransport(tracked *trackedTransport) {
	server.grantMu.Lock()
	if grantID, ok := server.transportGrants[tracked]; ok {
		delete(server.transportGrants, tracked)
		transports := server.grantTransports[grantID]
		delete(transports, tracked)
		if len(transports) == 0 {
			delete(server.grantTransports, grantID)
			if entry := server.grantTimers[grantID]; entry != nil {
				entry.timer.Stop()
				delete(server.grantTimers, grantID)
			}
		}
	}
	server.mu.Lock()
	delete(server.transports, tracked)
	server.mu.Unlock()
	server.grantMu.Unlock()
}

func (server *Server) scheduleGrantExpiryLocked(grantID string, expiresAt, now time.Time) {
	if entry := server.grantTimers[grantID]; entry != nil {
		return
	}
	delay := expiresAt.Sub(now)
	server.grantTimers[grantID] = &grantTimerEntry{
		expiresAt: expiresAt,
		timer: server.grantAfterFunc(delay, func() {
			server.expireGrant(grantID, expiresAt)
		}),
	}
}

func (server *Server) expireGrant(grantID string, expiresAt time.Time) {
	now := server.grantNow().UTC()
	server.grantMu.Lock()
	entry := server.grantTimers[grantID]
	if entry == nil || !entry.expiresAt.Equal(expiresAt) {
		server.grantMu.Unlock()
		return
	}
	if now.Before(expiresAt) {
		entry.timer = server.grantAfterFunc(expiresAt.Sub(now), func() {
			server.expireGrant(grantID, expiresAt)
		})
		server.grantMu.Unlock()
		return
	}
	server.grantTombstones[grantID] = transportCloseExpired
	transports := server.detachGrantTransportsLocked(grantID)
	server.grantMu.Unlock()
	closeTrackedTransports(transports, transportCloseExpired)
}

func (server *Server) revokeClientAccess(ctx context.Context, grantID string) (ClientAccessRecord, error) {
	service := server.ClientAccessService()
	if service == nil {
		return ClientAccessRecord{}, ErrClientAccessServiceUnavailable
	}
	grantID = strings.TrimSpace(grantID)
	server.grantMu.Lock()
	record, err := service.Revoke(ctx, grantID)
	if err != nil {
		server.grantMu.Unlock()
		return ClientAccessRecord{}, err
	}
	server.grantTombstones[record.GrantID] = transportCloseRevoked
	transports := server.detachGrantTransportsLocked(record.GrantID)
	server.grantMu.Unlock()
	closeTrackedTransports(transports, transportCloseRevoked)
	return record, nil
}

func (server *Server) detachGrantTransportsLocked(grantID string) []*trackedTransport {
	indexed := server.grantTransports[grantID]
	transports := make([]*trackedTransport, 0, len(indexed))
	for tracked := range indexed {
		transports = append(transports, tracked)
		delete(server.transportGrants, tracked)
	}
	delete(server.grantTransports, grantID)
	if entry := server.grantTimers[grantID]; entry != nil {
		entry.timer.Stop()
		delete(server.grantTimers, grantID)
	}
	return transports
}

func (server *Server) stopGrantTimers() {
	server.grantMu.Lock()
	for grantID, entry := range server.grantTimers {
		entry.timer.Stop()
		delete(server.grantTimers, grantID)
	}
	clear(server.grantTransports)
	clear(server.transportGrants)
	server.grantMu.Unlock()
}

func closeTrackedTransports(transports []*trackedTransport, reason transportCloseReason) {
	for _, tracked := range transports {
		_ = tracked.closeWithReason(reason)
	}
}
