// Package webcontroller 提供私有 Control Plane 的管理 API facade。
//
// 该 facade 只编排账号设备、entitlement、配对审批、usage 和审计 metadata；
// 它不是 client 与 daemon 之间的 signaling、DataChannel 或 terminal protocol gateway。
package webcontroller

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/domain"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/usage"
)

// DeviceDirectory 是 Web Controller 注册设备所需的最小 Control Plane 接口。
// 实现负责 ownership 校验，Controller 不绕过目录直接写数据库。
type DeviceDirectory interface {
	RegisterDevice(domain.DeviceRegistration) error
}

// EntitlementAdmin 是 Web Controller 更新归一化服务能力的最小接口。
// 更新只影响后续付费服务准入，不允许带 daemon disconnect side effect。
type EntitlementAdmin interface {
	Put(entitlement.Entitlement) error
}

// AuditLog 是 Web Controller 写入配对和管理审计 metadata 的最小接口。
// 实现必须拒绝原始 grant 和 credential body。
type AuditLog interface {
	RecordPairing(domain.PairingApproval) error
	Append(domain.AuditEvent) error
}

// UsageReader 是 Web Controller 展示 Relay 结算结果的只读接口。
type UsageReader interface {
	Aggregate(managedSessionID, routeID string) usage.SessionUsage
}

// Controller 是 Web 管理面到 Control Plane domain service 的薄 facade。
// 所有写操作都追加审计事件；它不拥有 Hub presence 或 Relay data-plane 状态。
type Controller struct {
	devices      DeviceDirectory
	entitlements EntitlementAdmin
	audit        AuditLog
	usage        UsageReader
}

// New 创建 Web Controller facade。
// 缺少任一领域依赖都会失败，避免管理请求 fallback 成旧 Web Controller schema。
func New(devices DeviceDirectory, entitlements EntitlementAdmin, auditLog AuditLog, usageReader UsageReader) (*Controller, error) {
	if devices == nil || entitlements == nil || auditLog == nil || usageReader == nil {
		return nil, fmt.Errorf("web controller domain dependencies are required")
	}
	return &Controller{devices: devices, entitlements: entitlements, audit: auditLog, usage: usageReader}, nil
}

// RegisterDevice 通过 Control Plane directory 建立设备所有权并记录审计 metadata。
// 该操作不创建长期 agent token，也不注册 terminal inventory。
func (controller *Controller) RegisterDevice(actorID string, device domain.DeviceRegistration, auditEventID string, now time.Time) error {
	if actorID == "" || auditEventID == "" {
		return fmt.Errorf("device registration audit identity is required")
	}
	if err := controller.devices.RegisterDevice(device); err != nil {
		return err
	}
	return controller.audit.Append(domain.AuditEvent{
		ID:         auditEventID,
		AccountID:  device.AccountID,
		ActorID:    actorID,
		Action:     "device.register",
		ResourceID: device.ID,
		OccurredAt: now,
	})
}

// SetEntitlement 更新账号归一化套餐能力并记录审计 metadata。
// 该方法没有 heartbeat 或 daemon kick callback，过期套餐只影响后续 lease issuance。
func (controller *Controller) SetEntitlement(actorID string, value entitlement.Entitlement, auditEventID string, now time.Time) error {
	if actorID == "" || auditEventID == "" {
		return fmt.Errorf("entitlement audit identity is required")
	}
	if err := controller.entitlements.Put(value); err != nil {
		return err
	}
	return controller.audit.Append(domain.AuditEvent{
		ID:         auditEventID,
		AccountID:  value.AccountID,
		ActorID:    actorID,
		Action:     "entitlement.update",
		ResourceID: value.AccountID,
		OccurredAt: now,
	})
}

// RecordPairingApproval 保存不可逆 grant reference hash 和审批 metadata。
// Controller 不接收、解析或返回原始 CapabilityGrant。
func (controller *Controller) RecordPairingApproval(approval domain.PairingApproval, auditEventID string) error {
	if err := controller.audit.RecordPairing(approval); err != nil {
		return err
	}
	return controller.audit.Append(domain.AuditEvent{
		ID:         auditEventID,
		AccountID:  approval.AccountID,
		ActorID:    approval.ApproverUserID,
		Action:     "pairing." + string(approval.Decision),
		ResourceID: approval.ID,
		OccurredAt: approval.DecidedAt,
	})
}

// RelayUsage 返回按 managed session/route 聚合一次的 Relay usage。
// 该查询不读取 packet payload、terminal metadata 或每 hop 重复账单。
func (controller *Controller) RelayUsage(managedSessionID, routeID string) usage.SessionUsage {
	return controller.usage.Aggregate(managedSessionID, routeID)
}
