// Package relaylease 编排 managed session、entitlement 和短期 Relay credential 签发。
package relaylease

import (
	"context"
	"fmt"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/domain"
	"github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relayquota"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
)

// InternalReservePath 是 Edge 向 Controller 提交 Proto reservation 的专用内部路径。
// 它不属于公开 Companion API，也不能被浏览器账号 session 直接调用。
const InternalReservePath = "/v1/internal/relay/leases/reserve"

// SessionSource 是 Relay lease service 读取 managed session 真值的最小接口。
// 实现必须按 AccountID 检查 ownership，且不得返回 terminal session 或 inventory。
type SessionSource interface {
	ManagedSession(context.Context, string, string, string, string, string, string, time.Time) (domain.ManagedSession, error)
}

// EntitlementSource 是 Relay lease service 读取账号服务能力的最小接口。
// 该接口与 Hub、daemon lifecycle 和 terminal scope 完全隔离。
type EntitlementSource interface {
	Entitlement(context.Context, string) (entitlement.Entitlement, error)
}

// Command 是签发 session-specific Relay lease 的服务命令。
// Route 字段由后续 route planner 或可信 Control Plane policy 提供，不能由未认证 endpoint 任意覆盖。
type Command struct {
	LeaseID             string
	AccountID           string
	ManagedSessionID    string
	AudienceRelayPool   string
	Region              string
	HubID               string
	RelayID             string
	PathKind            servicecredential.RelayPathKind
	RouteID             string
	RouteVersion        uint32
	ClientEdgeRelayID   string
	DaemonEdgeRelayID   string
	MaxInternalTransit  uint32
	RequestedTTL        time.Duration
	CredentialBindingID string
	ClientDeviceID      string
	TargetDeviceID      string
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
	reservations relayquota.Store
	issuer       servicecredential.RelayLeaseIssuer
	reportGrace  time.Duration
}

// NewService 创建 Relay lease service。
// 任一依赖缺失都会返回错误，避免开发期静默 fallback 成无 entitlement 的 lease issuer。
func NewService(sessions SessionSource, entitlements EntitlementSource, reservations relayquota.Store, issuer servicecredential.RelayLeaseIssuer, reportGrace time.Duration) (*Service, error) {
	if sessions == nil || entitlements == nil || reservations == nil || reportGrace < 0 {
		return nil, fmt.Errorf("relay lease service dependencies are required")
	}
	return &Service{sessions: sessions, entitlements: entitlements, reservations: reservations, issuer: issuer, reportGrace: reportGrace}, nil
}

// Issue 验证 managed session ownership，并把 entitlement clamp 的 quota 写入短期 lease。
// 失败只拒绝本次 paid Relay allocation；该方法没有 daemon kick、grant revoke 或 terminal authorization side effect。
func (service *Service) Issue(ctx context.Context, command Command, now time.Time) (servicecredential.RelayLease, servicecredential.RelayLeaseClaims, error) {
	now = now.UTC()
	session, err := service.sessions.ManagedSession(ctx, command.AccountID, command.ManagedSessionID, command.ClientDeviceID, command.TargetDeviceID, command.HubID, command.Region, now)
	if err != nil {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, fmt.Errorf("load managed session: %w", err)
	}
	if session.ID != command.ManagedSessionID || session.AccountID != command.AccountID {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	if session.Hub.HubID != command.HubID || session.Hub.Region != command.Region {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	accountEntitlement, err := service.entitlements.Entitlement(ctx, command.AccountID)
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
	leaseTTL := allocation.TTL
	if sessionRemaining := session.ExpiresAt.Sub(now); sessionRemaining < leaseTTL {
		leaseTTL = sessionRemaining
	}
	if leaseTTL <= 0 {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialExpired
	}
	policy := accountEntitlement.Capability.GetRelay()
	reservation, _, created, err := service.reservations.Reserve(ctx, relayquota.ReserveRequest{
		LeaseID: command.LeaseID, AccountID: command.AccountID, ManagedSessionID: command.ManagedSessionID,
		ClientDeviceID: session.ClientDeviceID, TargetDeviceID: session.TargetDeviceID, Region: command.Region,
		HubID: command.HubID, RelayID: command.RelayID, RouteID: command.RouteID,
		PeriodStart: accountEntitlement.EffectiveFrom, PeriodEnd: accountEntitlement.EffectiveUntil,
		PeriodLimitBytes: policy.GetMaxBytesPerPeriod(), MaxBytesPerLease: allocation.MaxBytes, MaxConcurrency: allocation.MaxConcurrency,
		ExpiresAt: now.Add(leaseTTL), ReleaseAfter: now.Add(leaseTTL).Add(service.reportGrace),
	}, now)
	if err != nil {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, err
	}
	issuedAt := time.UnixMilli(reservation.GetIssuedAtUnixMillis()).UTC()
	expiresAt := time.UnixMilli(reservation.GetExpiresAtUnixMillis()).UTC()
	lease, claims, err := service.issuer.Issue(servicecredential.RelayLeaseRequest{
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
		TTL:                 expiresAt.Sub(issuedAt),
		MaxBytes:            reservation.GetReservedBytes(),
		MaxBitrateKbps:      allocation.MaxBitrateKbps,
		MaxConcurrency:      allocation.MaxConcurrency,
		CredentialBindingID: command.CredentialBindingID,
	}, issuedAt)
	if err != nil {
		if created {
			_, _, _ = service.reservations.Release(ctx, command.AccountID, command.LeaseID, now)
		}
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, err
	}
	return lease, claims, nil
}

// Refresh 重新执行 session ownership、entitlement 和 quota clamp 后签发全新的短期 lease。
// 套餐在旧 lease 有效期间失效时，旧 lease 只按自身 TTL 收敛，本次 refresh 会被拒绝。
func (service *Service) Refresh(ctx context.Context, command RefreshCommand, now time.Time) (servicecredential.RelayLease, servicecredential.RelayLeaseClaims, error) {
	if command.PreviousLeaseID == "" || command.Next.LeaseID == "" || command.PreviousLeaseID == command.Next.LeaseID {
		return servicecredential.RelayLease{}, servicecredential.RelayLeaseClaims{}, servicecredential.ErrCredentialBinding
	}
	return service.Issue(ctx, command.Next, now)
}

// Cancel 只释放确认没有 allocation 和待 drain usage 的 reservation。
// 它不关闭 Relay 数据面；数据面关闭与 final usage drain 由 HUB006/CLOUDP005 接入。
func (service *Service) Cancel(ctx context.Context, accountID, leaseID string, now time.Time) error {
	_, _, err := service.reservations.Release(ctx, accountID, leaseID, now.UTC())
	return err
}
