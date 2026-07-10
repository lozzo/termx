// Package relaylease 编排 managed session、entitlement 和短期 Relay credential 签发。
package relaylease

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/control-plane/domain"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/termx-cloud/control-plane/servicecredential"
)

// SessionSource 是 Relay lease service 读取 managed session 真值的最小接口。
// 实现必须按 AccountID 检查 ownership，且不得返回 terminal session 或 inventory。
type SessionSource interface {
	ManagedSession(accountID, sessionID string, now time.Time) (domain.ManagedSession, error)
}

// EntitlementSource 是 Relay lease service 读取账号服务能力的最小接口。
// 该接口与 Hub、daemon lifecycle 和 terminal scope 完全隔离。
type EntitlementSource interface {
	Entitlement(accountID string) (entitlement.Entitlement, error)
}

// Command 是签发 session-specific Relay lease 的服务命令。
// Route 字段由后续 route planner 或可信 Control Plane policy 提供，不能由未认证 endpoint 任意覆盖。
type Command struct {
	LeaseID             string
	AccountID           string
	ManagedSessionID    string
	AudienceRelayPool   string
	Region              string
	PathKind            servicecredential.RelayPathKind
	RouteID             string
	RouteVersion        uint32
	ClientEdgeRelayID   string
	DaemonEdgeRelayID   string
	MaxInternalTransit  uint32
	RequestedTTL        time.Duration
	CredentialBindingID string
}

// RefreshCommand 表示用新的 lease ID 和 credential binding 刷新已有 Relay lease。
// PreviousLeaseID 只用于审计关联；新 lease 必须重新读取当前 entitlement，不能延长旧 credential。
type RefreshCommand struct {
	PreviousLeaseID string
	Next            Command
}

// Service 是 Relay entitlement 和 credential issuance 的事务边界。
// 消息链路固定为 session ownership -> entitlement policy -> quota clamp -> signed short lease。
type Service struct {
	sessions     SessionSource
	entitlements EntitlementSource
	issuer       servicecredential.RelayLeaseIssuer
}

// NewService 创建 Relay lease service。
// 任一依赖缺失都会返回错误，避免开发期静默 fallback 成无 entitlement 的 lease issuer。
func NewService(sessions SessionSource, entitlements EntitlementSource, issuer servicecredential.RelayLeaseIssuer) (*Service, error) {
	if sessions == nil || entitlements == nil {
		return nil, fmt.Errorf("relay lease service dependencies are required")
	}
	return &Service{sessions: sessions, entitlements: entitlements, issuer: issuer}, nil
}

// Issue 验证 managed session ownership，并把 entitlement clamp 的 quota 写入短期 lease。
// 失败只拒绝本次 paid Relay allocation；该方法没有 daemon kick、grant revoke 或 terminal authorization side effect。
func (service *Service) Issue(command Command, now time.Time) (servicecredential.RelayLease, servicecredential.RelayLeaseClaims, error) {
	session, err := service.sessions.ManagedSession(command.AccountID, command.ManagedSessionID, now)
	if err != nil {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, fmt.Errorf("load managed session: %w", err)
	}
	if session.ID != command.ManagedSessionID || session.AccountID != command.AccountID {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	if session.Hub.Region != command.Region {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	accountEntitlement, err := service.entitlements.Entitlement(command.AccountID)
	if err != nil {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, fmt.Errorf("load entitlement: %w", err)
	}
	if accountEntitlement.AccountID != command.AccountID {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	allocation, err := accountEntitlement.AuthorizeRelay(entitlement.RelayRequest{
		Region:       command.Region,
		PathKind:     command.PathKind,
		RequestedTTL: command.RequestedTTL,
	}, now)
	if err != nil {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, err
	}
	return service.issuer.Issue(servicecredential.RelayLeaseRequest{
		LeaseID:             command.LeaseID,
		AudienceRelayPool:   command.AudienceRelayPool,
		AccountID:           command.AccountID,
		ManagedSessionID:    command.ManagedSessionID,
		ClientDeviceID:      session.ClientDeviceID,
		TargetDeviceID:      session.TargetDeviceID,
		Region:              command.Region,
		PathKind:            command.PathKind,
		RouteID:             command.RouteID,
		RouteVersion:        command.RouteVersion,
		ClientEdgeRelayID:   command.ClientEdgeRelayID,
		DaemonEdgeRelayID:   command.DaemonEdgeRelayID,
		MaxInternalTransit:  command.MaxInternalTransit,
		TTL:                 allocation.TTL,
		MaxBytes:            allocation.MaxBytes,
		MaxBitrateKbps:      allocation.MaxBitrateKbps,
		MaxConcurrency:      allocation.MaxConcurrency,
		CredentialBindingID: command.CredentialBindingID,
	}, now)
}

// Refresh 重新执行 session ownership、entitlement 和 quota clamp 后签发全新的短期 lease。
// 套餐在旧 lease 有效期间失效时，旧 lease 只按自身 TTL 收敛，本次 refresh 会被拒绝。
func (service *Service) Refresh(command RefreshCommand, now time.Time) (servicecredential.RelayLease, servicecredential.RelayLeaseClaims, error) {
	if command.PreviousLeaseID == "" || command.Next.LeaseID == "" || command.PreviousLeaseID == command.Next.LeaseID {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	return service.Issue(command.Next, now)
}
