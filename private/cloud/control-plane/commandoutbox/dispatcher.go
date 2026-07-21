package commandoutbox

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// CommandPublisher 是 Controller HubControl transport 的瞬时投递端口。
// 成功返回不代表 Hub 已接收；delivery 只能由独立 HubCommandResult 推进。
type CommandPublisher interface {
	PublishCommand(string, *cloudpb.HubCommand) error
}

// RelayCommandPublisher 使用独立 Relay control generation 投递 close command。
type RelayCommandPublisher interface {
	PublishCommand(string, *cloudpb.RelayControlCommand) error
}

// DeviceSource 提供 daemon control command 所需的持久 auth epoch。
type DeviceSource interface {
	Device(context.Context, string) (cloudtopology.DeviceOwnership, error)
}

// Dispatcher 重试未完成 child 并收敛到 expiry。
// 它不持有命令状态，重启后始终从 durable CommandOutbox 重新读取。
type Dispatcher struct {
	outbox         *Service
	publisher      CommandPublisher
	relayPublisher RelayCommandPublisher
	devices        DeviceSource
	keyID          string
	privateKey     ed25519.PrivateKey
}

// NewDispatcher 创建 Hub command dispatcher；daemon control key 必须与 projection/edge/relay key 分离配置。
func NewDispatcher(outbox *Service, publisher CommandPublisher, relayPublisher RelayCommandPublisher, devices DeviceSource, keyID string, privateKey ed25519.PrivateKey) (*Dispatcher, error) {
	if outbox == nil || publisher == nil || devices == nil || keyID == "" || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("CommandOutbox dispatcher dependencies are invalid")
	}
	return &Dispatcher{outbox: outbox, publisher: publisher, relayPublisher: relayPublisher, devices: devices, keyID: keyID, privateKey: append(ed25519.PrivateKey(nil), privateKey...)}, nil
}

// DispatchOnce 投递一批未过期 child，并把到期项收敛为 UNKNOWN/PARTIAL。
// 同一 child 的 wire bytes 只依赖持久 projection，因此重试不会制造 replay digest 冲突。
func (dispatcher *Dispatcher) DispatchOnce(ctx context.Context, now time.Time, limit int) error {
	if dispatcher == nil || now.IsZero() {
		return ErrCommandConflict
	}
	if _, err := dispatcher.outbox.ExpireDue(ctx, now, limit); err != nil {
		return err
	}
	commands, err := dispatcher.outbox.Pending(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, parent := range commands {
		for _, child := range parent.GetChildren() {
			if child.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
				continue
			}
			if target := child.GetTarget().GetRelayAllocations(); target != nil {
				if dispatcher.relayPublisher == nil {
					return ErrCommandConflict
				}
				kind := cloudpb.RelayControlCommandKind_RELAY_CONTROL_COMMAND_KIND_CLOSE_LEASE_ALLOCATIONS
				if target.GetManagedSessionId() != "" {
					kind = cloudpb.RelayControlCommandKind_RELAY_CONTROL_COMMAND_KIND_CLOSE_SESSION_ALLOCATIONS
				}
				command := &cloudpb.RelayControlCommand{CommandId: child.GetChildCommandId(), CommandKind: kind, Target: proto.Clone(target).(*cloudpb.RelayControlTarget), IssuedAtUnixMillis: parent.GetCreatedAtUnixMillis(), ExpiresAtUnixMillis: parent.GetExpiresAtUnixMillis()}
				if err := dispatcher.relayPublisher.PublishCommand(target.GetRelayId(), command); err != nil {
					return err
				}
				continue
			}
			command, err := dispatcher.hubCommand(ctx, parent, child)
			if err != nil {
				return err
			}
			if err := dispatcher.publisher.PublishCommand(child.GetTargetHubId(), command); err != nil {
				return err
			}
		}
	}
	return nil
}

func (dispatcher *Dispatcher) hubCommand(ctx context.Context, parent *cloudpb.ManagementCommandProjection, child *cloudpb.ManagementCommandChildProjection) (*cloudpb.HubCommand, error) {
	command := &cloudpb.HubCommand{CommandId: child.GetChildCommandId(), IssuedAtUnixMillis: parent.GetCreatedAtUnixMillis(), ExpiresAtUnixMillis: parent.GetExpiresAtUnixMillis()}
	if target := child.GetTarget().GetAssignmentMigration(); target != nil {
		if target.GetSourceHubId() != child.GetTargetHubId() || target.GetSourceControlGeneration() == 0 || target.GetSourceAssignmentEpoch() == 0 || target.GetMigrationId() == "" {
			return nil, ErrCommandConflict
		}
		command.CommandKind = cloudpb.HubCommandKind_HUB_COMMAND_KIND_FENCE_ASSIGNMENT
		command.Target = &cloudpb.HubCommand_FenceAssignment{FenceAssignment: &cloudpb.FenceAssignment{MigrationId: target.GetMigrationId(), FenceCommandId: child.GetChildCommandId(), DaemonDeviceId: target.GetDaemonDeviceId(), SourceHubId: target.GetSourceHubId(), SourceAssignmentEpoch: target.GetSourceAssignmentEpoch(), SourceControlGeneration: target.GetSourceControlGeneration(), ExpiresAtUnixMillis: parent.GetExpiresAtUnixMillis()}}
		return command, nil
	}
	if target := child.GetTarget().GetPresence(); target != nil {
		command.CommandKind = cloudpb.HubCommandKind_HUB_COMMAND_KIND_KICK_PRESENCE
		command.Target = &cloudpb.HubCommand_KickPresence{KickPresence: target}
		return command, nil
	}
	peerTarget := child.GetTarget().GetPeerSession()
	accessTarget := child.GetTarget().GetTerminalAccess()
	if peerTarget == nil && accessTarget == nil {
		return nil, ErrCommandConflict
	}
	daemonDeviceID := ""
	if peerTarget != nil {
		daemonDeviceID = peerTarget.GetDaemonDeviceId()
	} else {
		daemonDeviceID = accessTarget.GetDaemonDeviceId()
	}
	device, err := dispatcher.devices.Device(ctx, daemonDeviceID)
	if err != nil || device.AccountID != parent.GetAccountId() || device.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || device.AuthEpoch == 0 {
		return nil, ErrCommandConflict
	}
	unsigned := &cloudpb.DaemonControlCommand{CommandId: child.GetChildCommandId(), AccountId: parent.GetAccountId(), TargetDeviceId: daemonDeviceID, HubId: child.GetTargetHubId(), AuthEpoch: device.AuthEpoch, IssuedAtUnixMillis: parent.GetCreatedAtUnixMillis(), ExpiresAtUnixMillis: parent.GetExpiresAtUnixMillis()}
	if peerTarget != nil {
		unsigned.CommandKind = cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION
		unsigned.AssignmentEpoch, unsigned.PresenceSessionId, unsigned.DaemonRuntimeGeneration = peerTarget.GetAssignmentEpoch(), peerTarget.GetControlPresenceSessionId(), peerTarget.GetDaemonRuntimeGeneration()
		unsigned.Target = &cloudpb.DaemonControlCommand_ManagedPeerSession{ManagedPeerSession: peerTarget}
	} else {
		unsigned.CommandKind = cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_REVOKE_TERMINAL_ACCESS
		unsigned.AssignmentEpoch, unsigned.PresenceSessionId, unsigned.DaemonRuntimeGeneration = accessTarget.GetAssignmentEpoch(), accessTarget.GetPresenceSessionId(), accessTarget.GetDaemonRuntimeGeneration()
		unsigned.Target = &cloudpb.DaemonControlCommand_TerminalAccess{TerminalAccess: accessTarget}
	}
	signed, err := cloudpb.SignDaemonControlCommand(unsigned, dispatcher.keyID, dispatcher.privateKey)
	if err != nil {
		return nil, err
	}
	command.CommandKind = cloudpb.HubCommandKind_HUB_COMMAND_KIND_FORWARD_DAEMON_COMMAND
	command.Target = &cloudpb.HubCommand_DaemonCommand{DaemonCommand: signed}
	return command, nil
}
