package controller

import (
	"context"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// migrationResultSink 在 Hub receipt 持久提交后刷新迁移涉及的两个 Hub projection。
// assignment 真值仍只在 PostgreSQL CommandOutbox 事务中切换；这里不缓存 owner 或 epoch。
type migrationResultSink struct {
	outbox  *commandoutbox.Service
	refresh func(*cloudpb.ManagementCommandProjection, time.Time) error
}

// IngestHubResult 先提交 receipt 和 assignment，再发布源 Hub 删除与目标 Hub 新 lease。
func (sink *migrationResultSink) IngestHubResult(ctx context.Context, result *cloudpb.HubCommandResult, now time.Time) error {
	projection, _, err := sink.outbox.ApplyHubResult(ctx, result, now)
	if err != nil {
		return err
	}
	state := projection.GetExecutionState()
	if projection.GetCommandKind() != cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT || state != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED && state != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED {
		return nil
	}
	return sink.refresh(projection, now.UTC())
}

// IngestDaemonResult 保持原有 daemon 独立 receipt 链路。
func (sink *migrationResultSink) IngestDaemonResult(ctx context.Context, result *cloudpb.DaemonCommandResult, now time.Time) error {
	return sink.outbox.IngestDaemonResult(ctx, result, now)
}
