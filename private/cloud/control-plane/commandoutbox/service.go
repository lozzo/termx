package commandoutbox

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	resultKindHub    = "hub"
	resultKindDaemon = "daemon"
)

// Store 是 CommandOutbox 的持久事务边界。
// result journal 与 projection CAS 必须同事务提交；相同 child 的冲突 digest 必须 fail closed。
type Store interface {
	CreateCommand(context.Context, Record) (Record, bool, error)
	CreateDeviceRevokeCommand(context.Context, Record, string, uint64) (Record, bool, error)
	Command(context.Context, string, string) (Record, error)
	CommandByIdempotency(context.Context, string, string) (Record, error)
	CommandByChild(context.Context, string) (Record, error)
	OpenCommands(context.Context, int) ([]Record, error)
	ListCommands(context.Context, string, cloudpb.ManagementCommandKind, cloudpb.CommandExecutionState, int) ([]Record, error)
	ApplyCommandResult(context.Context, Record, uint64, string, string, [sha256.Size]byte, []byte) (Record, bool, error)
	ReplaceCommand(context.Context, Record, uint64) (Record, error)
}

// ByIdempotency 返回账号隔离的首次 operation，供 planner 在重新解析易变 target 前处理 HTTP replay。
func (service *Service) ByIdempotency(ctx context.Context, accountID, idempotencyKey string) (*cloudpb.ManagementCommandProjection, error) {
	if service == nil || accountID == "" || idempotencyKey == "" {
		return nil, ErrCommandConflict
	}
	record, err := service.store.CommandByIdempotency(ctx, accountID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	return proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection), nil
}

// CreateDeviceRevoke 在同一持久事务中提交 auth epoch/revoked authority 与 CommandOutbox。
// 相同 idempotency key 返回首次 operation，不重复递增 auth epoch。
func (service *Service) CreateDeviceRevoke(ctx context.Context, projection *cloudpb.ManagementCommandProjection, idempotencyKey, deviceID string, expectedAuthEpoch uint64, now time.Time) (*cloudpb.ManagementCommandProjection, bool, error) {
	validated, err := ValidateCreate(projection, idempotencyKey, now)
	if err != nil || validated.GetCommandKind() != cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_CLOUD_DEVICE || deviceID == "" || expectedAuthEpoch == 0 {
		return nil, false, ErrCommandConflict
	}
	record, created, err := service.store.CreateDeviceRevokeCommand(ctx, Record{Projection: validated, IdempotencyKey: idempotencyKey, Version: 1}, deviceID, expectedAuthEpoch)
	if err != nil {
		return nil, false, err
	}
	return proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection), created, nil
}

// Service 编排 CommandOutbox 创建、结果 journal、CAS 更新与 expiry。
// 它不负责 Web 授权、target topology 解析或实际 Hub transport。
type Service struct{ store Store }

// New 创建 CommandOutbox service；缺少持久 Store 时 fail closed。
func New(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("CommandOutbox store is required")
	}
	return &Service{store: store}, nil
}

// Create 原子保存一个已经完成账号与 target 校验的 generated command。
// 相同 account+idempotency key 返回首次持久结果，不创建第二个 operation。
func (service *Service) Create(ctx context.Context, projection *cloudpb.ManagementCommandProjection, idempotencyKey string, now time.Time) (*cloudpb.ManagementCommandProjection, bool, error) {
	validated, err := ValidateCreate(projection, idempotencyKey, now)
	if err != nil {
		return nil, false, err
	}
	record, created, err := service.store.CreateCommand(ctx, Record{Projection: validated, IdempotencyKey: idempotencyKey, Version: 1})
	if err != nil {
		return nil, false, err
	}
	return proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection), created, nil
}

// Get 返回账号隔离的 command projection 深拷贝。
func (service *Service) Get(ctx context.Context, accountID, commandID string) (*cloudpb.ManagementCommandProjection, error) {
	record, err := service.store.Command(ctx, accountID, commandID)
	if err != nil {
		return nil, err
	}
	return proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection), nil
}

// Pending 返回 dispatcher 可尝试投递的未过期持久 command。
func (service *Service) Pending(ctx context.Context, now time.Time, limit int) ([]*cloudpb.ManagementCommandProjection, error) {
	records, err := service.store.OpenCommands(ctx, limit)
	if err != nil {
		return nil, err
	}
	result := make([]*cloudpb.ManagementCommandProjection, 0, len(records))
	for _, record := range records {
		if now.UnixMilli() < record.Projection.GetExpiresAtUnixMillis() {
			result = append(result, proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection))
		}
	}
	return result, nil
}

// List 返回账号隔离、按稳定枚举过滤的有界 command projection。
func (service *Service) List(ctx context.Context, accountID string, kind cloudpb.ManagementCommandKind, execution cloudpb.CommandExecutionState, limit int) ([]*cloudpb.ManagementCommandProjection, error) {
	records, err := service.store.ListCommands(ctx, accountID, kind, execution, limit)
	if err != nil {
		return nil, err
	}
	result := make([]*cloudpb.ManagementCommandProjection, 0, len(records))
	for _, record := range records {
		result = append(result, proto.Clone(record.Projection).(*cloudpb.ManagementCommandProjection))
	}
	return result, nil
}

// ApplyHubResult 持久 journal Hub receipt，并用版本 CAS 推进对应 child。
// exact replay 返回首次结果；相同 child 的不同 digest 返回 ErrCommandConflict。
func (service *Service) ApplyHubResult(ctx context.Context, result *cloudpb.HubCommandResult, now time.Time) (*cloudpb.ManagementCommandProjection, bool, error) {
	stable := proto.Clone(result).(*cloudpb.HubCommandResult)
	stable.ControlGeneration = 0
	stable.CompletedAtUnixMillis = 0
	return service.applyResult(ctx, result.GetCommandId(), resultKindHub, result, stable, now, func(current *cloudpb.ManagementCommandProjection) (*cloudpb.ManagementCommandProjection, error) {
		return ApplyHubResult(current, result, now)
	})
}

// IngestHubResult 实现 HubControl runtime result sink；返回前 result journal 已持久提交。
func (service *Service) IngestHubResult(ctx context.Context, result *cloudpb.HubCommandResult, now time.Time) error {
	_, _, err := service.ApplyHubResult(ctx, result, now)
	return err
}

// ApplyDaemonResult 持久 journal daemon 的独立 execution receipt，并推进精确 session child。
func (service *Service) ApplyDaemonResult(ctx context.Context, result *cloudpb.DaemonCommandResult, now time.Time) (*cloudpb.ManagementCommandProjection, bool, error) {
	return service.applyResult(ctx, result.GetCommandId(), resultKindDaemon, result, result, now, func(current *cloudpb.ManagementCommandProjection) (*cloudpb.ManagementCommandProjection, error) {
		return ApplyDaemonResult(current, result, now)
	})
}

// IngestDaemonResult 实现 HubControl runtime result sink；只有独立 daemon receipt 才推进 execution。
func (service *Service) IngestDaemonResult(ctx context.Context, result *cloudpb.DaemonCommandResult, now time.Time) error {
	_, _, err := service.ApplyDaemonResult(ctx, result, now)
	return err
}

func (service *Service) applyResult(ctx context.Context, childID, kind string, result, digestInput proto.Message, now time.Time, transition func(*cloudpb.ManagementCommandProjection) (*cloudpb.ManagementCommandProjection, error)) (*cloudpb.ManagementCommandProjection, bool, error) {
	if childID == "" || result == nil || digestInput == nil || now.IsZero() {
		return nil, false, ErrCommandConflict
	}
	digestPayload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(digestInput)
	if err != nil {
		return nil, false, err
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(result)
	if err != nil {
		return nil, false, err
	}
	digest := sha256.Sum256(digestPayload)
	for attempts := 0; attempts < 3; attempts++ {
		current, err := service.store.CommandByChild(ctx, childID)
		if err != nil {
			return nil, false, err
		}
		nextProjection, err := transition(current.Projection)
		if err != nil {
			return nil, false, err
		}
		next := Record{Projection: nextProjection, IdempotencyKey: current.IdempotencyKey, Version: current.Version + 1}
		stored, replay, err := service.store.ApplyCommandResult(ctx, next, current.Version, childID, kind, digest, payload)
		if err == nil {
			return proto.Clone(stored.Projection).(*cloudpb.ManagementCommandProjection), replay, nil
		}
		if err != ErrCommandConflict {
			return nil, false, err
		}
	}
	return nil, false, ErrCommandConflict
}

// ExpireDue 原子推进当前已到期 command；已完成 child 和 authority 不回滚。
func (service *Service) ExpireDue(ctx context.Context, now time.Time, limit int) ([]*cloudpb.ManagementCommandProjection, error) {
	records, err := service.store.OpenCommands(ctx, limit)
	if err != nil {
		return nil, err
	}
	var result []*cloudpb.ManagementCommandProjection
	for _, candidate := range records {
		if now.UnixMilli() < candidate.Projection.GetExpiresAtUnixMillis() {
			continue
		}
		for attempts := 0; attempts < 3; attempts++ {
			current, err := service.store.Command(ctx, candidate.Projection.GetAccountId(), candidate.Projection.GetCommandId())
			if err != nil {
				return nil, err
			}
			if current.Projection.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
				break
			}
			nextProjection, err := Expire(current.Projection, now)
			if err != nil {
				return nil, err
			}
			stored, err := service.store.ReplaceCommand(ctx, Record{Projection: nextProjection, IdempotencyKey: current.IdempotencyKey, Version: current.Version + 1}, current.Version)
			if err == nil {
				result = append(result, proto.Clone(stored.Projection).(*cloudpb.ManagementCommandProjection))
				break
			}
			if err != ErrCommandConflict || attempts == 2 {
				return nil, err
			}
		}
	}
	return result, nil
}
