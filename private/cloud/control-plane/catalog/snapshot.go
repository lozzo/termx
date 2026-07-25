package catalog

import (
	"context"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// SnapshotSource 是显式 development/test harness 使用的不可变目录来源。
// 生产 Controller 必须使用数据库 Service；该类型不持久化 active head，也不接受发布 mutation。
type SnapshotSource struct{ catalog *cloudpb.PlanCatalogContract }

// NewSnapshotSource 校验并复制固定目录，供单进程测试和 bootstrap 工具装配 Commerce。
func NewSnapshotSource(contract *cloudpb.PlanCatalogContract) (*SnapshotSource, error) {
	if contract == nil || contract.GetCatalogVersion() == 0 || len(contract.GetPlans()) == 0 {
		return nil, ErrInvalid
	}
	return &SnapshotSource{catalog: proto.Clone(contract).(*cloudpb.PlanCatalogContract)}, nil
}

// Active 返回固定快照的深拷贝。
func (source *SnapshotSource) Active(context.Context) (*cloudpb.PlanCatalogContract, error) {
	return proto.Clone(source.catalog).(*cloudpb.PlanCatalogContract), nil
}

// Plan 返回固定快照中的精确历史版本。
func (source *SnapshotSource) Plan(_ context.Context, planID string, planVersion uint64) (*cloudpb.PlanDefinition, error) {
	for _, plan := range source.catalog.GetPlans() {
		if plan.GetPlanId() == planID && plan.GetPlanVersion() == planVersion {
			return proto.Clone(plan).(*cloudpb.PlanDefinition), nil
		}
	}
	return nil, ErrNotFound
}

// CatalogPlan 满足目录 Store 的精确版本读取语义；SnapshotSource 不提供 release mutation。
func (source *SnapshotSource) CatalogPlan(ctx context.Context, planID string, planVersion uint64) (*cloudpb.PlanDefinition, error) {
	return source.Plan(ctx, planID, planVersion)
}
