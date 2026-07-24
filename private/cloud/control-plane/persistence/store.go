// Package persistence 定义 muxvia-cloud-controller composition root 所需的持久化能力集合。
//
// 各领域 service 继续依赖 commerce、hubregistry、topology 等包内的最小 Store port；
// 本包只允许 composition root 聚合这些端口，不能成为通用 repository 或业务状态机。
package persistence

import (
	"context"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubcontrol"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relayquota"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// ProjectionStore 持久化每个 Hub 的 policy projection revision 与 digest。
// revision 分配和 digest 提交必须由数据库原子串行化，Controller 内存不能维护第二份 head。
type ProjectionStore interface {
	AllocateProjectionRevision(context.Context, string, time.Time) (uint64, error)
	SetProjectionDigest(context.Context, string, uint64, []byte) error
}

// RelayQueryStore 提供 Relay 管理面和 CommandOutbox planner 所需的持久查询。
// 返回值只包含 generated projection，不暴露数据库 row 或 SQL transaction。
type RelayQueryStore interface {
	SnapshotForPeriod(context.Context, string, time.Time, time.Time, uint64, time.Time) (*cloudpb.GetAccountRelayQuotaResponse, error)
	RelayReservation(context.Context, string) (*cloudpb.RelayLeaseReservation, error)
	RelayReservationsForSession(context.Context, string) ([]*cloudpb.RelayLeaseReservation, error)
}

// DaemonEnrollmentCommit 描述 proof、Hub 选择和 credential 生成完成后的唯一持久化提交。
// ExpectedOwnership 与 ExpectedAssignment 是事务 CAS 真值；nil 表示数据库中必须尚不存在对应记录。
// NextOwnership、NextAssignment、Session 与 Audit 必须在同一事务中全部生效或全部回滚。
type DaemonEnrollmentCommit struct {
	ExpectedOwnership  *cloudtopology.DeviceOwnership
	ExpectedAssignment *cloudpb.HubAssignment
	NextOwnership      cloudtopology.DeviceOwnership
	NextAssignment     *cloudpb.HubAssignment
	Session            commerce.SessionRecord
	Audit              *cloudpb.CommerceAuditProjection
}

// DaemonEnrollmentStore 提供 daemon enrollment 的最终单事务提交。
// Web 批准、DeviceIdentity proof、Hub 探测和明文 credential 都只存在于内存；实现必须在一个事务中
// CAS ownership/assignment、撤销该设备旧 session、清理跨账号旧 topology 并写入新 session 与审计。
type DaemonEnrollmentStore interface {
	CommitDaemonEnrollment(context.Context, DaemonEnrollmentCommit, time.Time) (hubregistry.Assignment, error)
}

// MobileActivationCommit 描述 Web 已明确批准后的手机授权最终事务。
// ExpectedOwnership 是读取到的设备归属 CAS 真值；nil 表示设备必须尚不存在。NextOwnership
// 只能是当前账号下未撤销的 client。Session 与 Audit 由 commerce 在内存准备，事务提交前
// 不能把 Credential 交付给手机。
type MobileActivationCommit struct {
	ExpectedOwnership *cloudtopology.DeviceOwnership
	NextOwnership     cloudtopology.DeviceOwnership
	Session           commerce.SessionRecord
	Audit             *cloudpb.CommerceAuditProjection
}

// MobileActivationStore 原子提交同一手机的重新授权和 refresh session 轮换。
// 实现必须锁定账号与设备归属，拒绝跨账号或设备类型替换，并在任一步失败时完整回滚。
type MobileActivationStore interface {
	CommitMobileActivation(context.Context, MobileActivationCommit, time.Time) error
}

// Store 是 Controller composition root 的完整持久化依赖。
//
// 生产实现必须保证各领域接口注释声明的事务、CAS、幂等和 ack-after-commit 语义。
// Close 只管理 adapter 生命周期；领域 service 不得调用它或据此推断业务状态。
type Store interface {
	commerce.Store
	hubregistry.Store
	cloudtopology.Store
	commandoutbox.Store
	relayquota.Store
	hubcontrol.CursorStore
	ProjectionStore
	RelayQueryStore
	DaemonEnrollmentStore
	MobileActivationStore
	usage.Store
	Close() error
}
