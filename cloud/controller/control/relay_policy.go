package control

import (
	"context"
	"errors"

	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

// RelayLimits 是 Controller 商业策略在签发时冻结到 RelayLease 的执行值。
// R6 composition 使用显式部署配置；R7 的 Entitlement owner 将实现同一窄接口。
type RelayLimits struct {
	MaxBytes                 uint64
	MaxRateBytesPerSecond    uint64
	MaxConcurrentAllocations uint32
}

// RelayPolicy 只根据持久账号/订阅策略决定 Relay 上限，不读取或持有 Edge 在线 topology。
type RelayPolicy interface {
	Limits(context.Context, *cloudv1.ClientSessionSummary) (RelayLimits, error)
}

// ConfiguredRelayPolicy 是 R6 development 部署的版本化技术策略，不在业务分支中写死额度。
// R7 接入 Subscription/Entitlement 后由持久策略实现替换。
type ConfiguredRelayPolicy struct {
	Value RelayLimits
}

// Limits 返回部署时显式配置的正限制；零值拒绝收费 Relay 准入。
func (policy ConfiguredRelayPolicy) Limits(context.Context, *cloudv1.ClientSessionSummary) (RelayLimits, error) {
	if policy.Value.MaxBytes == 0 || policy.Value.MaxRateBytesPerSecond == 0 || policy.Value.MaxConcurrentAllocations == 0 {
		return RelayLimits{}, errors.New("Relay policy is not configured")
	}
	return policy.Value, nil
}

// UsageStore 是 Controller 对 Edge 用量批次的持久事务边界。
type UsageStore interface {
	CommitRelayUsage(context.Context, string, []*cloudv1.UsageEvent) ([]string, error)
}
