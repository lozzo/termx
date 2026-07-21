// Package policy 把账号与 Entitlement 持久真值映射为 Hub 最小准入投影。
package policy

import (
	"context"
	"fmt"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// Source 是 policy mapper 读取账号 auth revision 与 Entitlement 的持久边界。
type Source interface {
	Account(context.Context, string) (commerce.AccountRecord, error)
	Entitlement(context.Context, string) (*cloudpb.EntitlementProjection, error)
}

// Service 是 Controller 内 Entitlement 到 HubAccountPolicy 的唯一映射 owner。
type Service struct{ source Source }

// New 创建 policy mapper；缺少持久来源时 fail closed。
func New(source Source) (*Service, error) {
	if source == nil {
		return nil, fmt.Errorf("Hub policy source is required")
	}
	return &Service{source: source}, nil
}

// HubAccountPolicy 返回账号当前 auth epoch 与 Entitlement 的最小 generated 投影。
// 它不读取套餐名、价格、terminal、CapabilityGrant 或 Hub runtime 状态。
func (service *Service) HubAccountPolicy(ctx context.Context, accountID string) (*cloudpb.HubAccountPolicy, error) {
	account, err := service.source.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	entitlement, err := service.source.Entitlement(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Projection == nil || account.Projection.GetAccountId() != accountID || account.Projection.GetAuthRevision() == 0 || entitlement.GetAccountId() != accountID || entitlement.GetStatus() == cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_UNSPECIFIED || entitlement.GetCapability() == nil {
		return nil, fmt.Errorf("invalid Hub account policy source")
	}
	return &cloudpb.HubAccountPolicy{AccountId: accountID, AuthEpoch: account.Projection.GetAuthRevision(), EntitlementStatus: entitlement.GetStatus(), EntitlementEffectiveUntilUnixMillis: entitlement.GetEffectiveUntilUnixMillis(), Capability: proto.Clone(entitlement.GetCapability()).(*cloudpb.PlanCapability)}, nil
}
