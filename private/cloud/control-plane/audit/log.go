// Package audit 保存配对审批和 Control Plane 管理操作的最小审计 metadata。
package audit

import (
	"encoding/hex"
	"errors"
	"sync"

	"github.com/lozzow/termx/private/cloud/control-plane/domain"
)

var (
	// ErrInvalidPairingMetadata 表示审批记录缺字段或包含的 grant reference 不是不可逆 SHA-256 摘要。
	ErrInvalidPairingMetadata = errors.New("invalid pairing approval metadata")
	// ErrInvalidAuditEvent 表示管理审计事件缺少稳定 actor、action 或 resource reference。
	ErrInvalidAuditEvent = errors.New("invalid control plane audit event")
)

// Log 是配对审批与管理审计的并发安全内存 contract 实现。
// 它不接受原始 CapabilityGrant、credential body 或 terminal payload。
type Log struct {
	mu       sync.RWMutex
	pairings map[string]domain.PairingApproval
	events   []domain.AuditEvent
}

// NewLog 创建空审计日志。
func NewLog() *Log {
	return &Log{pairings: make(map[string]domain.PairingApproval)}
}

// RecordPairing 保存配对审批 metadata。
// GrantReferenceHash 必须是 32-byte SHA-256 的十六进制文本，避免 bearer grant 被误存为引用。
func (log *Log) RecordPairing(approval domain.PairingApproval) error {
	digest, err := hex.DecodeString(approval.GrantReferenceHash)
	if err != nil || len(digest) != 32 || approval.ID == "" || approval.AccountID == "" || approval.ClientDeviceID == "" || approval.TargetDeviceID == "" || approval.ApproverUserID == "" || approval.DecidedAt.IsZero() {
		return ErrInvalidPairingMetadata
	}
	if approval.Decision != domain.PairingDecisionApproved && approval.Decision != domain.PairingDecisionRejected {
		return ErrInvalidPairingMetadata
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if current, ok := log.pairings[approval.ID]; ok && current != approval {
		return ErrInvalidPairingMetadata
	}
	log.pairings[approval.ID] = approval
	return nil
}

// Append 保存不含 secret body 的管理审计事件。
func (log *Log) Append(event domain.AuditEvent) error {
	if event.ID == "" || event.AccountID == "" || event.ActorID == "" || event.Action == "" || event.ResourceID == "" || event.OccurredAt.IsZero() {
		return ErrInvalidAuditEvent
	}
	log.mu.Lock()
	log.events = append(log.events, event)
	log.mu.Unlock()
	return nil
}

// Events 返回审计事件快照。
// 返回切片是副本，调用方不能修改 Log 的持久真值。
func (log *Log) Events() []domain.AuditEvent {
	log.mu.RLock()
	defer log.mu.RUnlock()
	return append([]domain.AuditEvent(nil), log.events...)
}
