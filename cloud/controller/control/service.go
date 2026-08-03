// Package control 实现 Controller 侧 EdgeControl gRPC admission、同步状态机和 writer 边界。
package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/securetransport"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtocolVersion 9 combines authenticated EdgeIdentity certificate rotation
// with global per-account daemon connection admission.
const ProtocolVersion uint32 = 9

// Config 是 EdgeControl service 的 Controller 身份、Directory 和下发策略。
type Config struct {
	ControllerID               string
	ControllerBootID           string
	HeartbeatInterval          time.Duration
	HeartbeatTimeout           time.Duration
	BindingKeyBundle           func(context.Context) (*cloudv1.KeyBundle, error)
	Directory                  *directory.Directory
	EdgeEnabled                func(context.Context, string) (bool, error)
	DesiredConfig              func(context.Context, string) (*cloudv1.SignedEdgeDesiredConfig, error)
	DesiredCertificate         func(context.Context, string) (*cloudv1.EdgeCertificateBundle, error)
	CertificateApplied         func(context.Context, string, *cloudv1.CertificateApplied) error
	RenewIdentityCertificate   func(context.Context, string, *cloudv1.EdgeIdentityRenewRequest) (*cloudv1.EdgeIdentityRenewResponse, error)
	IdentityCertificateApplied func(context.Context, string, *cloudv1.EdgeIdentityApplied) error
	DaemonStateSnapshot        func(context.Context) (*cloudv1.DaemonStateSnapshot, error)
	ResolveDaemonState         func(context.Context, string) (*cloudv1.DaemonStateRecord, bool, error)
	DaemonConnectionLimit      func(context.Context, string, string) (uint32, error)
	RelayStore                 RelayStore
}

// RelayStore is the Controller transaction boundary for durable Relay authority.
type RelayStore interface {
	ReserveRelay(context.Context, string, *cloudv1.RelayReserveRequest) (*cloudv1.RelayReserveResponse, error)
	RenewRelay(context.Context, string, *cloudv1.RelayRenewRequest) (*cloudv1.RelayRenewResponse, error)
	SettleRelay(context.Context, string, *cloudv1.RelaySettlement) (*cloudv1.RelaySettlementAck, error)
	QueryRelay(context.Context, string, *cloudv1.RelayQueryRequest) (*cloudv1.RelayQueryResponse, error)
}

// Service 只拥有 EdgeControl admission 和 wire 状态机；实时拓扑全部提交给 Directory actor。
type Service struct {
	cloudv1.UnimplementedEdgeControlServer
	config          Config
	draining        atomic.Bool
	drain           chan struct{}
	drainOnce       sync.Once
	connectionsMu   sync.RWMutex
	connections     map[string]*connectionGeneration
	edgeConnections map[string]string
}

type externalCommand struct {
	payload            any
	certificateRefresh bool
	result             chan error
}

type connectionGeneration struct {
	edgeID       string
	external     chan externalCommand
	invalidated  chan struct{}
	invalidateMu sync.Once
}

func (generation *connectionGeneration) invalidate() {
	generation.invalidateMu.Do(func() { close(generation.invalidated) })
}

// NewService 校验 Controller 身份、心跳和 Directory，失败时不创建部分可用 service。
func NewService(config Config) (*Service, error) {
	config.ControllerID = strings.TrimSpace(config.ControllerID)
	config.ControllerBootID = strings.TrimSpace(config.ControllerBootID)
	if config.ControllerID == "" || config.ControllerBootID == "" || config.Directory == nil || config.BindingKeyBundle == nil || config.EdgeEnabled == nil || config.DaemonStateSnapshot == nil || config.ResolveDaemonState == nil || config.DaemonConnectionLimit == nil {
		return nil, errors.New("controller ID, boot ID, Directory, Edge admission, binding keys, and daemon state providers are required")
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatTimeout < config.HeartbeatInterval {
		return nil, errors.New("heartbeat timeout must be greater than or equal to a positive interval")
	}
	return &Service{config: config, drain: make(chan struct{}), connections: make(map[string]*connectionGeneration), edgeConnections: make(map[string]string)}, nil
}

// BeginShutdown 拒绝新控制流并通知现有 Connect handler 主动结束。
func (service *Service) BeginShutdown() {
	service.draining.Store(true)
	service.drainOnce.Do(func() { close(service.drain) })
}

// Connect 验证 mTLS/Hello，然后把快照和增量逐条提交给 Directory。
// 半份快照、序号缺口和摘要错误只触发当前连接重同步，不发布陈旧或不完整投影。
func (service *Service) Connect(stream cloudv1.EdgeControl_ConnectServer) error {
	if service.draining.Load() {
		return status.Error(codes.Unavailable, "Controller is draining")
	}
	certificateEdgeID, peerCertificateSHA256, err := authenticatedEdgeIdentity(stream)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	outbound := make(chan *cloudv1.ControllerCommand, 64)
	writerErrors := make(chan error, 1)
	go func() {
		for command := range outbound {
			if err := stream.Send(command); err != nil {
				writerErrors <- err
				return
			}
		}
	}()
	defer close(outbound)
	send := func(command *cloudv1.ControllerCommand) error {
		select {
		case err := <-writerErrors:
			return err
		case <-stream.Context().Done():
			return context.Cause(stream.Context())
		case outbound <- command:
			return nil
		default:
			return errors.New("Controller writer queue is full")
		}
	}

	event, err := service.receive(stream, writerErrors)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "receive EdgeHello: %v", err)
	}
	hello, err := validateHello(event, certificateEdgeID)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if err := service.config.Directory.Attach(stream.Context(), directory.Attachment{
		EdgeID: certificateEdgeID, BootID: event.GetBootId(), ConnectionID: event.GetConnectionId(), SoftwareVersion: hello.GetSoftwareVersion(), Capabilities: hello.GetCapabilities(), ConnectedAt: time.Now().UTC(),
	}); err != nil {
		return status.Errorf(codes.Aborted, "attach Edge generation: %v", err)
	}
	defer service.config.Directory.Detach(event.GetConnectionId())
	bootID := event.GetBootId()
	connectionID := event.GetConnectionId()
	generation := &connectionGeneration{edgeID: certificateEdgeID, external: make(chan externalCommand, 64), invalidated: make(chan struct{})}
	service.connectionsMu.Lock()
	service.connections[connectionID] = generation
	service.edgeConnections[certificateEdgeID] = connectionID
	service.connectionsMu.Unlock()
	defer func() {
		service.connectionsMu.Lock()
		delete(service.connections, connectionID)
		if service.edgeConnections[certificateEdgeID] == connectionID {
			delete(service.edgeConnections, certificateEdgeID)
		}
		service.connectionsMu.Unlock()
	}()
	enabled, err := service.config.EdgeEnabled(stream.Context(), certificateEdgeID)
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "load Edge admission state: %v", err)
	}
	if !enabled {
		return status.Error(codes.PermissionDenied, "Edge is disabled")
	}

	commandSeq := uint64(1)
	bindingBundle, err := service.config.BindingKeyBundle(stream.Context())
	if err == nil {
		bindingBundle, err = validPublishedBundle(bindingBundle, time.Now().UTC())
	}
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "load binding key bundle: %v", err)
	}
	daemonStates, err := service.config.DaemonStateSnapshot(stream.Context())
	if err != nil || !validDaemonStateSnapshot(daemonStates) {
		return status.Errorf(codes.FailedPrecondition, "load daemon state snapshot: %v", err)
	}
	if err := send(service.command(connectionID, commandSeq, &cloudv1.ControllerCommand_Welcome{Welcome: &cloudv1.EdgeWelcome{
		AcceptedProtocolVersion: ProtocolVersion,
		Heartbeat:               &cloudv1.HeartbeatPolicy{Interval: durationpb.New(service.config.HeartbeatInterval), Timeout: durationpb.New(service.config.HeartbeatTimeout)},
		BindingKeyBundle:        bindingBundle,
		DaemonStates:            daemonStates,
	}})); err != nil {
		return status.Errorf(codes.Unavailable, "send EdgeWelcome: %v", err)
	}
	if service.config.DesiredConfig != nil {
		desired, err := service.config.DesiredConfig(stream.Context(), certificateEdgeID)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "load Edge desired config: %v", err)
		}
		commandSeq++
		if err := send(service.command(connectionID, commandSeq, &cloudv1.ControllerCommand_DesiredConfig{DesiredConfig: desired})); err != nil {
			return status.Errorf(codes.Unavailable, "send Edge desired config: %v", err)
		}
	}
	if service.config.DesiredCertificate != nil {
		desired, err := service.reconcileDesiredCertificate(stream.Context(), certificateEdgeID, hello)
		if err != nil {
			return status.Errorf(codes.Unavailable, "reconcile Edge applied certificate: %v", err)
		}
		if desired != nil {
			commandSeq++
			if err := send(service.command(connectionID, commandSeq, &cloudv1.ControllerCommand_CertificateBundle{CertificateBundle: desired})); err != nil {
				return status.Errorf(codes.Unavailable, "send Edge desired certificate: %v", err)
			}
		}
	}
	inbound := make(chan receiveResult, 1)
	go func() {
		for {
			received, receiveErr := stream.Recv()
			select {
			case inbound <- receiveResult{event: received, err: receiveErr}:
			case <-stream.Context().Done():
				return
			}
			if receiveErr != nil {
				return
			}
		}
	}()
	expectedEventSeq := uint64(2)
	bundleRefresh := time.NewTimer(bindingBundleRefreshDelay(bindingBundle, time.Now().UTC()))
	defer bundleRefresh.Stop()
	for {
		select {
		case <-service.drain:
			return status.Error(codes.Unavailable, "Controller is draining")
		case <-generation.invalidated:
			return status.Error(codes.Unavailable, "EdgeControl state delivery was invalidated")
		case <-stream.Context().Done():
			return context.Cause(stream.Context())
		case err = <-writerErrors:
			return status.Errorf(codes.Unavailable, "send Controller command: %v", err)
		case <-bundleRefresh.C:
			bindingBundle, err = service.config.BindingKeyBundle(stream.Context())
			if err == nil {
				bindingBundle, err = validPublishedBundle(bindingBundle, time.Now().UTC())
			}
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "refresh binding key bundle: %v", err)
			}
			commandSeq++
			if err := send(service.command(connectionID, commandSeq, &cloudv1.ControllerCommand_BindingKeyBundle{BindingKeyBundle: bindingBundle})); err != nil {
				return status.Errorf(codes.Unavailable, "send binding key bundle: %v", err)
			}
			bundleRefresh.Reset(bindingBundleRefreshDelay(bindingBundle, time.Now().UTC()))
		case request := <-generation.external:
			payload, shouldSend, resolveErr := service.resolveExternalCommand(stream.Context(), certificateEdgeID, request)
			if resolveErr != nil {
				if request.result != nil {
					request.result <- resolveErr
				}
				continue
			}
			if !shouldSend {
				if request.result != nil {
					request.result <- nil
				}
				continue
			}
			commandSeq++
			sendErr := send(service.command(connectionID, commandSeq, payload))
			if request.result != nil {
				request.result <- sendErr
			}
			if sendErr != nil {
				return status.Errorf(codes.Unavailable, "send Controller state: %v", sendErr)
			}
			continue
		case received := <-inbound:
			event, err = received.event, received.err
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := validateEventEnvelope(event, certificateEdgeID, hello.GetEdgeId(), bootID, connectionID); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		if event.GetStreamSeq() != expectedEventSeq {
			expectedEventSeq = event.GetStreamSeq() + 1
			commandSeq++
			if err := send(service.resyncCommand(event.GetConnectionId(), commandSeq, 0, "Edge event stream sequence has a gap")); err != nil {
				return status.Errorf(codes.Unavailable, "send ResyncRequired: %v", err)
			}
			continue
		}
		expectedEventSeq++
		response, syncErr := service.applyEvent(stream.Context(), event, peerCertificateSHA256)
		if syncErr != nil {
			var resync *directory.SyncError
			if !errors.As(syncErr, &resync) {
				return status.Error(codes.InvalidArgument, syncErr.Error())
			}
			commandSeq++
			if err := send(service.resyncCommand(event.GetConnectionId(), commandSeq, resync.ExpectedRevision, resync.Reason)); err != nil {
				return status.Errorf(codes.Unavailable, "send ResyncRequired: %v", err)
			}
			continue
		}
		if response != nil {
			commandSeq++
			if err := send(service.command(event.GetConnectionId(), commandSeq, response)); err != nil {
				return status.Errorf(codes.Unavailable, "send Controller response: %v", err)
			}
		}
	}
}

// reconcileDesiredCertificate 使用认证后的 EdgeHello 修复可能丢失的 applied 回执。
// Hello 与 desired 不同才返回待下发证书；幂等落库失败会关闭当前流，由 Edge 重连重试。
func (service *Service) reconcileDesiredCertificate(ctx context.Context, edgeID string, hello *cloudv1.EdgeHello) (*cloudv1.EdgeCertificateBundle, error) {
	desired, err := service.config.DesiredCertificate(ctx, edgeID)
	// Controller secret 暂时不可读不能中断 Edge 的现有 P2P/Relay；绑定保持 pending，重连后继续收敛。
	if err != nil || desired == nil {
		return nil, nil
	}
	if hello.GetCertificateProfileId() != desired.GetCertificateProfileId() || hello.GetCertificateVersion() != desired.GetRevision() {
		return desired, nil
	}
	if service.config.CertificateApplied == nil {
		return nil, nil
	}
	applied := &cloudv1.CertificateApplied{CertificateProfileId: desired.GetCertificateProfileId(), Revision: desired.GetRevision(), Applied: true}
	if err := service.config.CertificateApplied(ctx, edgeID, applied); err != nil {
		return nil, fmt.Errorf("persist Hello certificate reconciliation: %w", err)
	}
	return nil, nil
}

func (service *Service) applyEvent(ctx context.Context, event *cloudv1.EdgeEvent, peerCertificateSHA256 []byte) (any, error) {
	switch payload := event.GetPayload().(type) {
	case *cloudv1.EdgeEvent_SnapshotBegin:
		return nil, service.config.Directory.BeginSnapshot(ctx, event.GetConnectionId(), payload.SnapshotBegin)
	case *cloudv1.EdgeEvent_SnapshotChunk:
		return nil, service.config.Directory.AppendSnapshot(ctx, event.GetConnectionId(), payload.SnapshotChunk)
	case *cloudv1.EdgeEvent_SnapshotEnd:
		if err := service.config.Directory.CommitSnapshot(ctx, event.GetConnectionId(), payload.SnapshotEnd); err != nil {
			return nil, err
		}
		return &cloudv1.ControllerCommand_SnapshotAccepted{SnapshotAccepted: &cloudv1.SnapshotAccepted{SnapshotId: payload.SnapshotEnd.GetSnapshotId(), Revision: payload.SnapshotEnd.GetRevision()}}, nil
	case *cloudv1.EdgeEvent_RuntimeDelta:
		return nil, service.config.Directory.ApplyDelta(ctx, event.GetConnectionId(), payload.RuntimeDelta)
	case *cloudv1.EdgeEvent_Heartbeat:
		return nil, service.config.Directory.Heartbeat(ctx, event.GetConnectionId(), payload.Heartbeat)
	case *cloudv1.EdgeEvent_ConfigApplied:
		if payload.ConfigApplied == nil || payload.ConfigApplied.GetVersion() == 0 {
			return nil, errors.New("ConfigApplied version is required")
		}
		return nil, nil
	case *cloudv1.EdgeEvent_CertificateApplied:
		if service.config.CertificateApplied == nil || payload.CertificateApplied == nil || payload.CertificateApplied.GetCertificateProfileId() == "" || payload.CertificateApplied.GetRevision() == 0 {
			return nil, errors.New("CertificateApplied is invalid or unavailable")
		}
		return nil, service.config.CertificateApplied(ctx, event.GetSenderId(), payload.CertificateApplied)
	case *cloudv1.EdgeEvent_IdentityRenew:
		request := payload.IdentityRenew
		if service.config.RenewIdentityCertificate == nil || request == nil || strings.TrimSpace(request.GetRequestId()) == "" || len(request.GetCurrentCertificateSha256()) != sha256.Size || !bytes.Equal(request.GetCurrentCertificateSha256(), peerCertificateSHA256) {
			return nil, errors.New("EdgeIdentity renewal request is invalid or does not match the mTLS peer")
		}
		response, err := service.config.RenewIdentityCertificate(ctx, event.GetSenderId(), request)
		return &cloudv1.ControllerCommand_IdentityRenew{IdentityRenew: response}, err
	case *cloudv1.EdgeEvent_IdentityApplied:
		if service.config.IdentityCertificateApplied == nil || payload.IdentityApplied == nil {
			return nil, errors.New("EdgeIdentity applied receipt is invalid or unavailable")
		}
		return nil, service.config.IdentityCertificateApplied(ctx, event.GetSenderId(), payload.IdentityApplied)
	case *cloudv1.EdgeEvent_RelayReserve:
		if service.config.RelayStore == nil || payload.RelayReserve == nil {
			return nil, errors.New("Relay reserve is invalid or unavailable")
		}
		response, err := service.config.RelayStore.ReserveRelay(ctx, event.GetSenderId(), payload.RelayReserve)
		return &cloudv1.ControllerCommand_RelayReserve{RelayReserve: response}, err
	case *cloudv1.EdgeEvent_RelayRenew:
		if service.config.RelayStore == nil || payload.RelayRenew == nil {
			return nil, errors.New("Relay renewal is invalid or unavailable")
		}
		response, err := service.config.RelayStore.RenewRelay(ctx, event.GetSenderId(), payload.RelayRenew)
		return &cloudv1.ControllerCommand_RelayRenew{RelayRenew: response}, err
	case *cloudv1.EdgeEvent_RelaySettle:
		if service.config.RelayStore == nil || payload.RelaySettle == nil {
			return nil, errors.New("Relay settlement is invalid or unavailable")
		}
		response, err := service.config.RelayStore.SettleRelay(ctx, event.GetSenderId(), payload.RelaySettle)
		return &cloudv1.ControllerCommand_RelaySettle{RelaySettle: response}, err
	case *cloudv1.EdgeEvent_RelayQuery:
		if service.config.RelayStore == nil || payload.RelayQuery == nil {
			return nil, errors.New("Relay query is invalid or unavailable")
		}
		response, err := service.config.RelayStore.QueryRelay(ctx, event.GetSenderId(), payload.RelayQuery)
		return &cloudv1.ControllerCommand_RelayQuery{RelayQuery: response}, err
	case *cloudv1.EdgeEvent_DaemonStateQuery:
		query := payload.DaemonStateQuery
		if query == nil || strings.TrimSpace(query.GetRequestId()) == "" || strings.TrimSpace(query.GetDaemonId()) == "" {
			return nil, errors.New("daemon state query is invalid")
		}
		record, found, err := service.config.ResolveDaemonState(ctx, strings.TrimSpace(query.GetDaemonId()))
		return &cloudv1.ControllerCommand_DaemonStateQueryResult{DaemonStateQueryResult: &cloudv1.DaemonStateQueryResult{RequestId: query.GetRequestId(), DaemonId: query.GetDaemonId(), Found: found, Daemon: record}}, err
	case *cloudv1.EdgeEvent_DaemonConnectionAdmission:
		request := payload.DaemonConnectionAdmission
		if request == nil || strings.TrimSpace(request.GetRequestId()) == "" || strings.TrimSpace(request.GetAdmissionId()) == "" || strings.TrimSpace(request.GetDaemonId()) == "" || strings.TrimSpace(request.GetAccountId()) == "" || strings.TrimSpace(request.GetAgentConnectionId()) == "" {
			return nil, errors.New("daemon connection admission request is invalid")
		}
		response := &cloudv1.DaemonConnectionAdmissionResponse{RequestId: request.GetRequestId(), AdmissionId: request.GetAdmissionId()}
		if request.GetRelease() {
			if err := service.config.Directory.ReleaseDaemonConnection(ctx, event.GetConnectionId(), directory.DaemonConnectionAdmission{
				AdmissionID: request.GetAdmissionId(), DaemonID: request.GetDaemonId(), AccountID: request.GetAccountId(), AgentConnectionID: request.GetAgentConnectionId(),
			}); err != nil {
				response.Result = cloudv1.DaemonConnectionAdmissionResult_DAEMON_CONNECTION_ADMISSION_RESULT_UNAVAILABLE
				response.Message = "daemon connection admission release is unavailable"
			} else {
				response.Result = cloudv1.DaemonConnectionAdmissionResult_DAEMON_CONNECTION_ADMISSION_RESULT_RELEASED
			}
			return &cloudv1.ControllerCommand_DaemonConnectionAdmission{DaemonConnectionAdmission: response}, nil
		}
		limit, err := service.config.DaemonConnectionLimit(ctx, strings.TrimSpace(request.GetDaemonId()), strings.TrimSpace(request.GetAccountId()))
		response.Limit = limit
		if err != nil || limit == 0 {
			response.Result = cloudv1.DaemonConnectionAdmissionResult_DAEMON_CONNECTION_ADMISSION_RESULT_REJECTED
			response.Message = "daemon connection is not allowed by the current entitlement"
			return &cloudv1.ControllerCommand_DaemonConnectionAdmission{DaemonConnectionAdmission: response}, nil
		}
		err = service.config.Directory.AdmitDaemonConnection(ctx, event.GetConnectionId(), directory.DaemonConnectionAdmission{
			AdmissionID: request.GetAdmissionId(), DaemonID: request.GetDaemonId(), AccountID: request.GetAccountId(), AgentConnectionID: request.GetAgentConnectionId(),
		}, limit)
		switch {
		case err == nil:
			response.Result = cloudv1.DaemonConnectionAdmissionResult_DAEMON_CONNECTION_ADMISSION_RESULT_ADMITTED
		case errors.Is(err, directory.ErrDaemonConnectionLimit):
			response.Result = cloudv1.DaemonConnectionAdmissionResult_DAEMON_CONNECTION_ADMISSION_RESULT_LIMIT_REACHED
			response.Message = "cloud daemon connection limit reached"
		default:
			response.Result = cloudv1.DaemonConnectionAdmissionResult_DAEMON_CONNECTION_ADMISSION_RESULT_UNAVAILABLE
			response.Message = "daemon connection admission is unavailable"
		}
		return &cloudv1.ControllerCommand_DaemonConnectionAdmission{DaemonConnectionAdmission: response}, nil
	case *cloudv1.EdgeEvent_CommandResult:
		return nil, service.config.Directory.CompleteCommand(ctx, event.GetConnectionId(), payload.CommandResult)
	default:
		return nil, errors.New("EdgeHello is only valid as the first EdgeControl payload")
	}
}

func (service *Service) command(connectionID string, sequence uint64, payload any) *cloudv1.ControllerCommand {
	command := &cloudv1.ControllerCommand{ProtocolVersion: ProtocolVersion, MessageId: uuid.NewString(), SenderId: service.config.ControllerID, BootId: service.config.ControllerBootID, ConnectionId: connectionID, StreamSeq: sequence, SentAt: timestamppb.Now()}
	switch typed := payload.(type) {
	case *cloudv1.ControllerCommand_Welcome:
		command.Payload = typed
	case *cloudv1.ControllerCommand_SnapshotAccepted:
		command.Payload = typed
	case *cloudv1.ControllerCommand_ResyncRequired:
		command.Payload = typed
	case *cloudv1.ControllerCommand_DesiredConfig:
		command.Payload = typed
	case *cloudv1.ControllerCommand_BindingKeyBundle:
		command.Payload = typed
	case *cloudv1.ControllerCommand_RelayReserve:
		command.Payload = typed
	case *cloudv1.ControllerCommand_RelayRenew:
		command.Payload = typed
	case *cloudv1.ControllerCommand_RelaySettle:
		command.Payload = typed
	case *cloudv1.ControllerCommand_RelayQuery:
		command.Payload = typed
	case *cloudv1.ControllerCommand_CloseDaemon:
		command.Payload = typed
	case *cloudv1.ControllerCommand_CloseSession:
		command.Payload = typed
	case *cloudv1.ControllerCommand_CertificateBundle:
		command.Payload = typed
	case *cloudv1.ControllerCommand_DaemonStateDelta:
		command.Payload = typed
	case *cloudv1.ControllerCommand_DaemonStateQueryResult:
		command.Payload = typed
	case *cloudv1.ControllerCommand_ReselectDaemonEdge:
		command.Payload = typed
	case *cloudv1.ControllerCommand_IdentityRenew:
		command.Payload = typed
	case *cloudv1.ControllerCommand_DaemonConnectionAdmission:
		command.Payload = typed
	default:
		panic("unsupported ControllerCommand payload")
	}
	return command
}

// ReselectDaemonEdge asks the exact online daemon generation to probe and refresh its Edge binding.
func (service *Service) ReselectDaemonEdge(ctx context.Context, daemonID string, generation, preferenceRevision uint64) cloudv1.RuntimeCommandResult {
	commandID, correlationID := uuid.NewString(), uuid.NewString()
	location, waiter, err := service.config.Directory.BeginCommand(ctx, correlationID, daemonID, generation, true)
	if err != nil {
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_STALE
	}
	defer service.config.Directory.CancelCommand(correlationID)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().UTC().Add(10 * time.Second)
	}
	payload := &cloudv1.ControllerCommand_ReselectDaemonEdge{ReselectDaemonEdge: &cloudv1.ReselectDaemonEdge{CommandId: commandID, CorrelationId: correlationID, Deadline: timestamppb.New(deadline), DaemonId: daemonID, Generation: generation, PreferenceRevision: preferenceRevision}}
	if err := service.sendExternal(ctx, location.ConnectionID, payload); err != nil {
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_UNAVAILABLE
	}
	return waitRuntimeCommand(ctx, waiter)
}

// DisconnectDaemon 发送精确 generation 实时命令并等待 Edge 结果；命令不写数据库。
func (service *Service) DisconnectDaemon(ctx context.Context, daemonID string, generation uint64, reason string) cloudv1.RuntimeCommandResult {
	commandID, correlationID := uuid.NewString(), uuid.NewString()
	location, waiter, err := service.config.Directory.BeginCommand(ctx, correlationID, daemonID, generation, true)
	if err != nil {
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_STALE
	}
	defer service.config.Directory.CancelCommand(correlationID)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().UTC().Add(10 * time.Second)
	}
	payload := &cloudv1.ControllerCommand_CloseDaemon{CloseDaemon: &cloudv1.CloseDaemonConnection{CommandId: commandID, CorrelationId: correlationID, Deadline: timestamppb.New(deadline), DaemonId: daemonID, Generation: generation, Reason: reason}}
	if err := service.sendExternal(ctx, location.ConnectionID, payload); err != nil {
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_UNAVAILABLE
	}
	return waitRuntimeCommand(ctx, waiter)
}

// DisconnectSession 发送精确 generation 客户端断开命令并等待 Edge 结果。
func (service *Service) DisconnectSession(ctx context.Context, sessionID string, generation uint64, reason string) cloudv1.RuntimeCommandResult {
	commandID, correlationID := uuid.NewString(), uuid.NewString()
	location, waiter, err := service.config.Directory.BeginCommand(ctx, correlationID, sessionID, generation, false)
	if err != nil {
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_STALE
	}
	defer service.config.Directory.CancelCommand(correlationID)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().UTC().Add(10 * time.Second)
	}
	payload := &cloudv1.ControllerCommand_CloseSession{CloseSession: &cloudv1.CloseClientSession{CommandId: commandID, CorrelationId: correlationID, Deadline: timestamppb.New(deadline), SessionId: sessionID, Generation: generation, Reason: reason}}
	if err := service.sendExternal(ctx, location.ConnectionID, payload); err != nil {
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_UNAVAILABLE
	}
	return waitRuntimeCommand(ctx, waiter)
}

func (service *Service) sendExternal(ctx context.Context, connectionID string, payload any) error {
	return service.sendExternalRequest(ctx, connectionID, externalCommand{payload: payload})
}

func (service *Service) sendExternalRequest(ctx context.Context, connectionID string, request externalCommand) error {
	service.connectionsMu.RLock()
	generation := service.connections[connectionID]
	service.connectionsMu.RUnlock()
	if generation == nil {
		return directory.ErrStaleConnection
	}
	request.result = make(chan error, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case generation.external <- request:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-request.result:
		return err
	}
}

// BroadcastDaemonState queues one durable state replacement for every online Edge.
// A full queue invalidates that connection so reconnect will reload the snapshot.
func (service *Service) BroadcastDaemonState(record *cloudv1.DaemonStateRecord) {
	if !validDaemonStateRecord(record) {
		return
	}
	service.connectionsMu.RLock()
	generations := make([]*connectionGeneration, 0, len(service.connections))
	for _, generation := range service.connections {
		generations = append(generations, generation)
	}
	service.connectionsMu.RUnlock()
	request := externalCommand{payload: &cloudv1.ControllerCommand_DaemonStateDelta{DaemonStateDelta: &cloudv1.DaemonStateDelta{Daemon: record}}}
	for _, generation := range generations {
		select {
		case generation.external <- request:
		default:
			generation.invalidate()
		}
	}
}

// InvalidateEdge removes every admitted generation before asking its handler to stop.
// A later connection must pass the durable Edge enabled check before receiving state.
func (service *Service) InvalidateEdge(edgeID string) {
	edgeID = strings.TrimSpace(edgeID)
	if edgeID == "" {
		return
	}
	service.connectionsMu.Lock()
	generations := make([]*connectionGeneration, 0)
	for connectionID, generation := range service.connections {
		if generation.edgeID != edgeID {
			continue
		}
		delete(service.connections, connectionID)
		generations = append(generations, generation)
	}
	delete(service.edgeConnections, edgeID)
	service.connectionsMu.Unlock()
	for _, generation := range generations {
		generation.invalidate()
	}
}

// RefreshCertificate 通知当前在线的 Edge writer 在分配 command sequence 时读取最新持久 desired。
// 通知本身不携带私钥或旧 bundle；离线时返回 stale，持久 desired 等待重连收敛。
func (service *Service) RefreshCertificate(ctx context.Context, edgeID string) error {
	edgeID = strings.TrimSpace(edgeID)
	if edgeID == "" || service.config.DesiredCertificate == nil {
		return errors.New("Edge ID and desired certificate loader are required")
	}
	service.connectionsMu.RLock()
	connectionID := service.edgeConnections[edgeID]
	service.connectionsMu.RUnlock()
	if connectionID == "" {
		return directory.ErrStaleConnection
	}
	return service.sendExternalRequest(ctx, connectionID, externalCommand{certificateRefresh: true})
}

func (service *Service) resolveExternalCommand(ctx context.Context, edgeID string, request externalCommand) (any, bool, error) {
	if !request.certificateRefresh {
		return request.payload, true, nil
	}
	desired, err := service.config.DesiredCertificate(ctx, edgeID)
	if err != nil {
		return nil, false, err
	}
	if desired == nil {
		return nil, false, nil
	}
	if desired.GetTargetEdgeId() != edgeID || desired.GetCertificateProfileId() == "" || desired.GetRevision() == 0 || len(desired.GetCertificateChainPem()) == 0 || len(desired.GetPrivateKeyPem()) == 0 {
		return nil, false, errors.New("current targeted Edge certificate bundle is invalid")
	}
	return &cloudv1.ControllerCommand_CertificateBundle{CertificateBundle: desired}, true, nil
}

func waitRuntimeCommand(ctx context.Context, waiter <-chan *cloudv1.EdgeCommandResult) cloudv1.RuntimeCommandResult {
	select {
	case <-ctx.Done():
		return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_TIMEOUT
	case result, ok := <-waiter:
		if !ok || result == nil {
			return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_UNAVAILABLE
		}
		switch result.GetCode() {
		case cloudv1.CommandResultCode_COMMAND_RESULT_CODE_APPLIED:
			return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_APPLIED
		case cloudv1.CommandResultCode_COMMAND_RESULT_CODE_STALE:
			return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_STALE
		default:
			return cloudv1.RuntimeCommandResult_RUNTIME_COMMAND_RESULT_REJECTED
		}
	}
}

func (service *Service) resyncCommand(connectionID string, sequence, expected uint64, reason string) *cloudv1.ControllerCommand {
	return service.command(connectionID, sequence, &cloudv1.ControllerCommand_ResyncRequired{ResyncRequired: &cloudv1.ResyncRequired{ExpectedRevision: expected, Reason: reason}})
}

func validDaemonStateSnapshot(snapshot *cloudv1.DaemonStateSnapshot) bool {
	if snapshot == nil {
		return false
	}
	seen := make(map[string]struct{}, len(snapshot.GetDaemons()))
	for _, record := range snapshot.GetDaemons() {
		if !validDaemonStateRecord(record) {
			return false
		}
		if _, exists := seen[record.GetDaemonId()]; exists {
			return false
		}
		seen[record.GetDaemonId()] = struct{}{}
	}
	return true
}

func validDaemonStateRecord(record *cloudv1.DaemonStateRecord) bool {
	return record != nil && strings.TrimSpace(record.GetDaemonId()) != "" && record.GetStateRevision() > 0 &&
		(record.GetState() == cloudv1.DaemonState_DAEMON_STATE_ACTIVE || record.GetState() == cloudv1.DaemonState_DAEMON_STATE_BLOCKED || record.GetState() == cloudv1.DaemonState_DAEMON_STATE_DELETED)
}

type receiveResult struct {
	event *cloudv1.EdgeEvent
	err   error
}

func (service *Service) receive(stream cloudv1.EdgeControl_ConnectServer, writerErrors <-chan error) (*cloudv1.EdgeEvent, error) {
	result := make(chan receiveResult, 1)
	go func() {
		event, err := stream.Recv()
		result <- receiveResult{event: event, err: err}
	}()
	select {
	case err := <-writerErrors:
		return nil, status.Errorf(codes.Unavailable, "send Controller command: %v", err)
	case <-service.drain:
		return nil, status.Error(codes.Unavailable, "Controller is draining")
	case <-stream.Context().Done():
		return nil, context.Cause(stream.Context())
	case received := <-result:
		return received.event, received.err
	}
}

func authenticatedEdgeIdentity(stream cloudv1.EdgeControl_ConnectServer) (string, []byte, error) {
	remotePeer, ok := peer.FromContext(stream.Context())
	if !ok {
		return "", nil, errors.New("mTLS peer is missing")
	}
	tlsInfo, ok := remotePeer.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return "", nil, errors.New("verified mTLS client certificate is missing")
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	edgeID, err := securetransport.EdgeIDFromCertificate(certificate)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(certificate.Raw)
	return edgeID, digest[:], nil
}

func validateHello(event *cloudv1.EdgeEvent, certificateEdgeID string) (*cloudv1.EdgeHello, error) {
	if event == nil || event.GetHello() == nil {
		return nil, errors.New("first EdgeControl payload must be EdgeHello")
	}
	if err := validateEventEnvelope(event, certificateEdgeID, certificateEdgeID, event.GetBootId(), event.GetConnectionId()); err != nil {
		return nil, err
	}
	if event.GetStreamSeq() != 1 {
		return nil, errors.New("EdgeHello stream_seq must be 1")
	}
	hello := event.GetHello()
	if hello.GetEdgeId() != certificateEdgeID {
		return nil, errors.New("EdgeHello identity does not match the mTLS Edge URI SAN")
	}
	if strings.TrimSpace(hello.GetSoftwareVersion()) == "" {
		return nil, errors.New("EdgeHello software version is required")
	}
	if !slices.Contains(hello.GetCapabilities(), cloudv1.EdgeCapability_EDGE_CAPABILITY_CONTROL_STREAM) {
		return nil, errors.New("EdgeHello does not advertise the control stream capability")
	}
	if !slices.Contains(hello.GetCapabilities(), cloudv1.EdgeCapability_EDGE_CAPABILITY_DAEMON_CONNECTION_ADMISSION) {
		return nil, errors.New("EdgeHello does not advertise daemon connection admission")
	}
	return hello, nil
}

func validateEventEnvelope(event *cloudv1.EdgeEvent, certificateEdgeID, senderID, bootID, connectionID string) error {
	if event == nil || event.GetProtocolVersion() != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d", event.GetProtocolVersion())
	}
	if strings.TrimSpace(event.GetMessageId()) == "" || strings.TrimSpace(event.GetBootId()) == "" || strings.TrimSpace(event.GetConnectionId()) == "" {
		return errors.New("Edge event envelope IDs are required")
	}
	if event.GetSenderId() != senderID || senderID != certificateEdgeID || event.GetBootId() != bootID || event.GetConnectionId() != connectionID {
		return errors.New("Edge event identity or generation does not match EdgeHello")
	}
	if event.GetSentAt() == nil || event.GetSentAt().CheckValid() != nil {
		return errors.New("Edge event sent_at is invalid")
	}
	return nil
}

func validPublishedBundle(bundle *cloudv1.KeyBundle, now time.Time) (*cloudv1.KeyBundle, error) {
	canonical, _, err := ticket.CanonicalKeyBundle(bundle)
	if err != nil {
		return nil, err
	}
	if !ticket.KeyBundleUsableAt(canonical, now) {
		return nil, errors.New("binding key bundle is not currently usable")
	}
	return canonical, nil
}

func bindingBundleRefreshDelay(bundle *cloudv1.KeyBundle, now time.Time) time.Duration {
	remaining := bundle.GetExpiresAt().AsTime().Sub(now)
	if remaining <= 0 {
		return time.Nanosecond
	}
	return remaining / 2
}
