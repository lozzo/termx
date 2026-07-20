// Package relayquota 定义 Relay billing period 与 lease reservation 的持久领域边界。
// 它只管理 Cloud Relay 额度，不读取 terminal、CapabilityGrant、Hub 内存状态或数据面 payload。
package relayquota

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
)

var (
	// ErrQuotaExhausted 表示当前账期没有足够字节额度或并发槽位。
	ErrQuotaExhausted = errors.New("relay period quota exhausted")
	// ErrReservationConflict 表示相同 lease ID 被不同 reservation 输入复用，或账期能力发生不兼容变化。
	ErrReservationConflict = errors.New("relay reservation conflict")
	// ErrReservationNotFound 表示目标 reservation 不存在或不属于指定账号。
	ErrReservationNotFound = errors.New("relay reservation not found")
)

// ReserveRequest 是 Relay lease 签发前必须原子提交的额度请求。
// Period 和 limit 来自当前 Entitlement；设备、session 和 region 来自已验证 managed session。
type ReserveRequest struct {
	LeaseID          string
	AccountID        string
	ManagedSessionID string
	ClientDeviceID   string
	TargetDeviceID   string
	Region           string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	PeriodLimitBytes uint64
	MaxBytesPerLease uint64
	MaxConcurrency   uint32
	ExpiresAt        time.Time
	ReleaseAfter     time.Time
}

// Validate 验证 reservation 输入完整且所有窗口使用 UTC 可比较时间。
func (request ReserveRequest) Validate(now time.Time) error {
	if request.LeaseID == "" || request.AccountID == "" || request.ManagedSessionID == "" || request.ClientDeviceID == "" || request.TargetDeviceID == "" || request.Region == "" ||
		request.PeriodStart.IsZero() || request.PeriodEnd.IsZero() || !request.PeriodEnd.After(request.PeriodStart) || request.PeriodLimitBytes == 0 || request.PeriodLimitBytes > math.MaxInt64 || request.MaxBytesPerLease == 0 || request.MaxBytesPerLease > request.PeriodLimitBytes || request.MaxBytesPerLease > math.MaxInt64 || request.MaxConcurrency == 0 ||
		request.ExpiresAt.IsZero() || !request.ExpiresAt.After(now.UTC()) || request.ExpiresAt.After(request.PeriodEnd) || request.ReleaseAfter.Before(request.ExpiresAt) {
		return fmt.Errorf("invalid Relay reservation request")
	}
	return nil
}

// Store 是 Relay quota 的唯一持久 owner。
// Reserve 必须在一个事务内完成过期清理、额度/并发判断和 reservation 写入。
type Store interface {
	Reserve(context.Context, ReserveRequest, time.Time) (*cloudpb.RelayLeaseReservation, *cloudpb.RelayQuotaPeriod, bool, error)
	Release(context.Context, string, string, time.Time) (*cloudpb.RelayLeaseReservation, *cloudpb.RelayQuotaPeriod, error)
	Snapshot(context.Context, string, time.Time, time.Time, time.Time) (*cloudpb.GetAccountRelayQuotaResponse, error)
}
