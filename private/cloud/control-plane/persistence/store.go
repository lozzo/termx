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
	usage.Store
	Close() error
}
