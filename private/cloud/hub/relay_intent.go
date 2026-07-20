package hub

import (
	"fmt"
	"time"
)

const relayLeaseRefreshWindow = 30 * time.Second

type relayIntentState struct {
	accountID       string
	clientDeviceID  string
	targetDeviceID  string
	expiresAt       time.Time
	leaseID         string
	leaseExpiresAt  time.Time
	leaseGeneration uint64
}

// RelayIntent 是 Hub 对一次 endpoint resolution 的短期账号/client/target binding。
// 它只让 client 与 daemon 为同一 managed session 取得同一 Relay lease，不授予 terminal 权限。
type RelayIntent struct {
	AccountID      string
	ClientDeviceID string
	TargetDeviceID string
	ExpiresAt      time.Time
	LeaseID        string
}

// CreateRelayIntent 在 endpoint resolution 后保存短 TTL binding。
// Hub 重启会丢失该状态；调用方必须重新 resolve，不能从磁盘恢复或 fallback。
func (service *Service) CreateRelayIntent(managedSessionID, accountID, clientDeviceID, targetDeviceID string) error {
	if service == nil || managedSessionID == "" || accountID == "" || clientDeviceID == "" || targetDeviceID == "" || clientDeviceID == targetDeviceID {
		return ErrAdmission
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	value := relayIntentState{accountID: accountID, clientDeviceID: clientDeviceID, targetDeviceID: targetDeviceID, expiresAt: now.Add(service.maxSignalingTTL), leaseID: "lease-" + managedSessionID + "-1", leaseGeneration: 1}
	if current, ok := service.relayIntents[managedSessionID]; ok {
		if current != value {
			return ErrSessionConflict
		}
		return nil
	}
	service.relayIntents[managedSessionID] = value
	return nil
}

// RelayIntent 返回 requester 可以使用的短期 binding；只有原 client 或目标 daemon 可读取。
func (service *Service) RelayIntent(managedSessionID, requesterDeviceID string) (RelayIntent, error) {
	if service == nil || managedSessionID == "" || requesterDeviceID == "" {
		return RelayIntent{}, ErrAdmission
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	value, ok := service.relayIntents[managedSessionID]
	if !ok || requesterDeviceID != value.clientDeviceID && requesterDeviceID != value.targetDeviceID {
		return RelayIntent{}, ErrAdmission
	}
	if !value.leaseExpiresAt.IsZero() && !now.Before(value.leaseExpiresAt.Add(-relayLeaseRefreshWindow)) {
		value.leaseGeneration++
		value.leaseID = "lease-" + managedSessionID + "-" + fmt.Sprint(value.leaseGeneration)
		value.leaseExpiresAt = time.Time{}
		service.relayIntents[managedSessionID] = value
	}
	return RelayIntent{AccountID: value.accountID, ClientDeviceID: value.clientDeviceID, TargetDeviceID: value.targetDeviceID, ExpiresAt: value.expiresAt, LeaseID: value.leaseID}, nil
}

// BindRelayIntentLease 记录 Controller 返回的精确 lease expiry，供临近到期时单调轮换下一 lease ID。
func (service *Service) BindRelayIntentLease(managedSessionID, leaseID string, expiresAt time.Time) error {
	if service == nil || managedSessionID == "" || leaseID == "" || expiresAt.IsZero() {
		return ErrAdmission
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	value, ok := service.relayIntents[managedSessionID]
	if !ok || value.leaseID != leaseID || !expiresAt.After(now) {
		return ErrAdmission
	}
	if value.leaseExpiresAt.IsZero() {
		value.leaseExpiresAt = expiresAt.UTC()
		if extended := expiresAt.UTC().Add(service.maxSignalingTTL); extended.After(value.expiresAt) {
			value.expiresAt = extended
		}
		service.relayIntents[managedSessionID] = value
		return nil
	}
	if !value.leaseExpiresAt.Equal(expiresAt.UTC()) {
		return ErrSessionConflict
	}
	return nil
}
