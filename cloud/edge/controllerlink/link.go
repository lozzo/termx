// Package controllerlink 实现 Edge 到 Controller 的唯一 mTLS EdgeControl 客户端连接。
package controllerlink

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/runtimesnapshot"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const snapshotChunkSize = 256

// RuntimeFeed 是在同一 Edge actor 事务中取得的快照和后续增量。
type RuntimeFeed struct {
	Snapshot *cloudv1.RuntimeSnapshot
	Deltas   <-chan *cloudv1.RuntimeDelta
	Close    func()
}

// Config 是一次 EdgeControl 连接尝试的完整输入。
// OpenRuntimeFeed 必须从 Edge Runtime actor 原子取得快照与后续增量订阅。
type Config struct {
	ControllerAddress    string
	TLSConfig            *tls.Config
	EdgeID               string
	BootID               string
	SoftwareVersion      string
	DesiredConfigVersion uint64
	CertificateProfileID string
	CertificateVersion   uint64
	WriterQueueSize      int
	OpenRuntimeFeed      func(context.Context) (*RuntimeFeed, error)
	ApplyDesiredConfig   func(context.Context, *cloudv1.SignedEdgeDesiredConfig) (uint64, error)
	ApplyCertificate     func(context.Context, *cloudv1.EdgeCertificateBundle) error
	CloseDaemon          func(context.Context, string, uint64) error
	CloseSession         func(context.Context, string, uint64) error
	Capabilities         []cloudv1.EdgeCapability
}

// Session 拥有一个 EdgeControl generation、唯一 reader、唯一 writer 与同步 coordinator。
type Session struct {
	connectionID string
	welcome      *cloudv1.EdgeWelcome
	stream       cloudv1.EdgeControl_ConnectClient
	connection   *grpc.ClientConn
	cancel       context.CancelFunc
	done         chan struct{}
	doneOnce     sync.Once
	resultMu     sync.Mutex
	resultErr    error
	ready        chan struct{}
	readyOnce    sync.Once
	closeOnce    sync.Once
	outbound     chan any
	usageAcks    chan *cloudv1.UsageAck
	usageMu      sync.Mutex
}

// Open 建立 mTLS 流并完成 Hello/Welcome；SnapshotAccepted 由 WaitReady 单独等待。
func Open(parent context.Context, config Config) (*Session, error) {
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.BootID = strings.TrimSpace(config.BootID)
	config.SoftwareVersion = strings.TrimSpace(config.SoftwareVersion)
	if config.WriterQueueSize == 0 {
		config.WriterQueueSize = 1024
	}
	if len(config.Capabilities) == 0 {
		config.Capabilities = []cloudv1.EdgeCapability{cloudv1.EdgeCapability_EDGE_CAPABILITY_CONTROL_STREAM}
	}
	if config.ControllerAddress == "" || config.EdgeID == "" || config.BootID == "" || config.SoftwareVersion == "" || config.TLSConfig == nil || config.OpenRuntimeFeed == nil || config.WriterQueueSize <= 0 {
		return nil, errors.New("controller address, TLS, Edge identity, runtime feed, and positive writer queue are required")
	}
	ctx, cancel := context.WithCancel(parent)
	connection, err := grpc.NewClient(config.ControllerAddress, grpc.WithTransportCredentials(credentials.NewTLS(config.TLSConfig.Clone())))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create EdgeControl client: %w", err)
	}
	stream, err := cloudv1.NewEdgeControlClient(connection).Connect(ctx)
	if err != nil {
		cancel()
		_ = connection.Close()
		return nil, fmt.Errorf("open EdgeControl stream: %w", err)
	}
	connectionID := uuid.NewString()
	if err := stream.Send(edgeEvent(config, connectionID, 1, &cloudv1.EdgeEvent_Hello{Hello: &cloudv1.EdgeHello{
		EdgeId: config.EdgeID, SoftwareVersion: config.SoftwareVersion,
		Capabilities:         append([]cloudv1.EdgeCapability(nil), config.Capabilities...),
		DesiredConfigVersion: config.DesiredConfigVersion, CertificateVersion: config.CertificateVersion, CertificateProfileId: config.CertificateProfileID,
	}})); err != nil {
		cancel()
		_ = connection.Close()
		return nil, fmt.Errorf("send EdgeHello: %w", err)
	}
	command, err := stream.Recv()
	if err != nil {
		cancel()
		_ = connection.Close()
		return nil, fmt.Errorf("receive EdgeWelcome: %w", err)
	}
	welcome, err := validateWelcome(command, connectionID)
	if err != nil {
		cancel()
		_ = connection.Close()
		return nil, err
	}
	session := &Session{
		connectionID: connectionID, welcome: welcome, stream: stream, connection: connection, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}),
		outbound: make(chan any, config.WriterQueueSize), usageAcks: make(chan *cloudv1.UsageAck, 1),
	}
	go session.run(ctx, config, command.GetSenderId(), command.GetBootId(), config.WriterQueueSize)
	return session, nil
}

// ConnectionID 返回当前 Edge connection generation，只在进程内存中生效。
func (session *Session) ConnectionID() string { return session.connectionID }

// Welcome 返回 Controller 接受的心跳和公开验签策略投影。
func (session *Session) Welcome() *cloudv1.EdgeWelcome { return session.welcome }

// WaitReady 等待 Controller 原子接受当前 generation 的完整快照。
func (session *Session) WaitReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
		if session.err() == nil {
			return errors.New("EdgeControl closed before snapshot was accepted")
		}
		return session.err()
	case <-session.ready:
		return nil
	}
}

// Wait 等待当前 generation 结束；调用方据此撤销 ready 并重连。
func (session *Session) Wait() error {
	<-session.done
	return session.err()
}

// Done 在当前 EdgeControl generation 结束时关闭，供 usage pump 停止重试。
func (session *Session) Done() <-chan struct{} { return session.done }

// Close 取消 reader/writer/coordinator 并关闭底层连接；重复调用安全。
func (session *Session) Close() error {
	var err error
	session.closeOnce.Do(func() {
		session.cancel()
		err = session.connection.Close()
	})
	return err
}

// CommitUsageBatch 发送一个 outbox 批次并等待数据库提交后的精确 ACK。
// 调用方同一时间只能有一个未完成批次，避免 UsageAck 与本地读取窗口错配。
func (session *Session) CommitUsageBatch(ctx context.Context, batch *cloudv1.UsageBatch) (*cloudv1.UsageAck, error) {
	if session == nil || batch == nil || strings.TrimSpace(batch.GetBatchId()) == "" || len(batch.GetEvents()) == 0 {
		return nil, errors.New("non-empty UsageBatch is required")
	}
	session.usageMu.Lock()
	defer session.usageMu.Unlock()
	select {
	case stale := <-session.usageAcks:
		_ = stale
	default:
	}
	if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_UsageBatch{UsageBatch: batch}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.done:
		return nil, errors.New("EdgeControl closed before UsageAck")
	case ack := <-session.usageAcks:
		return ack, nil
	}
}

func (session *Session) run(ctx context.Context, config Config, controllerID, controllerBootID string, writerQueueSize int) {
	commands := make(chan *cloudv1.ControllerCommand, writerQueueSize)
	receiveErrors := make(chan error, 1)
	go session.readCommands(ctx, controllerID, controllerBootID, commands, receiveErrors)
	writeErrors := make(chan error, 1)
	go session.writeEvents(ctx, config, session.outbound, writeErrors)

	var feed *RuntimeFeed
	closeFeed := func() {
		if feed != nil && feed.Close != nil {
			feed.Close()
		}
		feed = nil
	}
	defer closeFeed()
	startSnapshot := func() error {
		closeFeed()
		var err error
		feed, err = config.OpenRuntimeFeed(ctx)
		if err != nil {
			return err
		}
		return queueSnapshot(ctx, session.outbound, feed.Snapshot)
	}
	if err := startSnapshot(); err != nil {
		session.finish(err)
		return
	}
	latestRevision := feed.Snapshot.GetRevision()
	snapshotAccepted := false
	desiredApplied := config.ApplyDesiredConfig == nil
	markReady := func() {
		if snapshotAccepted && desiredApplied {
			session.readyOnce.Do(func() { close(session.ready) })
		}
	}
	heartbeat := time.NewTicker(session.welcome.GetHeartbeat().GetInterval().AsDuration())
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			session.finish(context.Cause(ctx))
			return
		case err := <-receiveErrors:
			session.finish(err)
			return
		case err := <-writeErrors:
			session.finish(err)
			return
		case command := <-commands:
			switch payload := command.GetPayload().(type) {
			case *cloudv1.ControllerCommand_SnapshotAccepted:
				if feed != nil && payload.SnapshotAccepted.GetRevision() == feed.Snapshot.GetRevision() {
					snapshotAccepted = true
					markReady()
				}
			case *cloudv1.ControllerCommand_ResyncRequired:
				if err := startSnapshot(); err != nil {
					session.finish(err)
					return
				}
				latestRevision = feed.Snapshot.GetRevision()
				snapshotAccepted = false
			case *cloudv1.ControllerCommand_DesiredConfig:
				if config.ApplyDesiredConfig == nil {
					session.finish(errors.New("Controller sent desired config to an Edge without a config verifier"))
					return
				}
				version, applyErr := config.ApplyDesiredConfig(ctx, payload.DesiredConfig)
				result := &cloudv1.ConfigApplied{Version: version, Applied: applyErr == nil}
				if applyErr != nil {
					result.ErrorCode = "CONFIG_INVALID"
				}
				if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_ConfigApplied{ConfigApplied: result}); err != nil {
					session.finish(err)
					return
				}
				if applyErr != nil {
					session.finish(applyErr)
					return
				}
				desiredApplied = true
				markReady()
			case *cloudv1.ControllerCommand_CertificateBundle:
				result := &cloudv1.CertificateApplied{CertificateProfileId: payload.CertificateBundle.GetCertificateProfileId(), Revision: payload.CertificateBundle.GetRevision()}
				if config.ApplyCertificate == nil {
					result.ErrorCode = "CERTIFICATE_APPLY_UNAVAILABLE"
					result.ErrorMessage = "Edge certificate manager is unavailable"
				} else if applyErr := config.ApplyCertificate(ctx, payload.CertificateBundle); applyErr != nil {
					result.ErrorCode = "CERTIFICATE_APPLY_FAILED"
					result.ErrorMessage = applyErr.Error()
				} else {
					result.Applied = true
				}
				if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_CertificateApplied{CertificateApplied: result}); err != nil {
					session.finish(err)
					return
				}
			case *cloudv1.ControllerCommand_UsageAck:
				select {
				case session.usageAcks <- payload.UsageAck:
				default:
					session.finish(errors.New("unexpected or duplicate UsageAck"))
					return
				}
			case *cloudv1.ControllerCommand_CloseDaemon:
				result := executeRuntimeCommand(ctx, payload.CloseDaemon.GetCommandId(), payload.CloseDaemon.GetCorrelationId(), payload.CloseDaemon.GetDeadline(), func(commandContext context.Context) error {
					if config.CloseDaemon == nil {
						return errors.New("daemon close handler is unavailable")
					}
					return config.CloseDaemon(commandContext, payload.CloseDaemon.GetDaemonId(), payload.CloseDaemon.GetGeneration())
				})
				if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_CommandResult{CommandResult: result}); err != nil {
					session.finish(err)
					return
				}
			case *cloudv1.ControllerCommand_CloseSession:
				result := executeRuntimeCommand(ctx, payload.CloseSession.GetCommandId(), payload.CloseSession.GetCorrelationId(), payload.CloseSession.GetDeadline(), func(commandContext context.Context) error {
					if config.CloseSession == nil {
						return errors.New("session close handler is unavailable")
					}
					return config.CloseSession(commandContext, payload.CloseSession.GetSessionId(), payload.CloseSession.GetGeneration())
				})
				if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_CommandResult{CommandResult: result}); err != nil {
					session.finish(err)
					return
				}
			default:
				session.finish(fmt.Errorf("unsupported Controller command payload %T", command.GetPayload()))
				return
			}
		case delta, ok := <-feed.Deltas:
			if !ok {
				session.finish(errors.New("runtime delta buffer overflowed"))
				return
			}
			if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_RuntimeDelta{RuntimeDelta: delta}); err != nil {
				session.finish(err)
				return
			}
			latestRevision = delta.GetRevision()
		case <-heartbeat.C:
			if err := queueEvent(ctx, session.outbound, &cloudv1.EdgeEvent_Heartbeat{Heartbeat: &cloudv1.EdgeHeartbeat{RuntimeRevision: latestRevision}}); err != nil {
				session.finish(err)
				return
			}
		}
	}
}

func executeRuntimeCommand(parent context.Context, commandID, correlationID string, deadline *timestamppb.Timestamp, execute func(context.Context) error) *cloudv1.EdgeCommandResult {
	result := &cloudv1.EdgeCommandResult{CommandId: commandID, CorrelationId: correlationID, CompletedAt: timestamppb.Now()}
	if commandID == "" || correlationID == "" || deadline == nil || deadline.CheckValid() != nil || !time.Now().UTC().Before(deadline.AsTime()) {
		result.Code = cloudv1.CommandResultCode_COMMAND_RESULT_CODE_REJECTED
		result.Message = "command deadline is invalid or expired"
		return result
	}
	ctx, cancel := context.WithDeadline(parent, deadline.AsTime())
	defer cancel()
	err := execute(ctx)
	result.CompletedAt = timestamppb.Now()
	if err == nil {
		result.Code = cloudv1.CommandResultCode_COMMAND_RESULT_CODE_APPLIED
		return result
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		result.Code = cloudv1.CommandResultCode_COMMAND_RESULT_CODE_REJECTED
		result.Message = "command deadline exceeded"
		return result
	}
	result.Code = cloudv1.CommandResultCode_COMMAND_RESULT_CODE_STALE
	result.Message = "target generation is no longer current"
	return result
}

func (session *Session) readCommands(ctx context.Context, controllerID, controllerBootID string, output chan<- *cloudv1.ControllerCommand, errorsOut chan<- error) {
	expected := uint64(2)
	for {
		command, err := session.stream.Recv()
		if err != nil {
			errorsOut <- err
			return
		}
		if err := validateCommand(command, session.connectionID, controllerID, controllerBootID, expected); err != nil {
			errorsOut <- err
			return
		}
		expected++
		select {
		case <-ctx.Done():
			return
		case output <- command:
		}
	}
}

func (session *Session) writeEvents(ctx context.Context, config Config, input <-chan any, errorsOut chan<- error) {
	sequence := uint64(1)
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-input:
			sequence++
			if err := session.stream.Send(edgeEvent(config, session.connectionID, sequence, payload)); err != nil {
				errorsOut <- err
				return
			}
		}
	}
}

func (session *Session) finish(err error) {
	session.doneOnce.Do(func() {
		session.resultMu.Lock()
		session.resultErr = err
		session.resultMu.Unlock()
		close(session.done)
	})
}

func (session *Session) err() error {
	session.resultMu.Lock()
	defer session.resultMu.Unlock()
	return session.resultErr
}

func queueSnapshot(ctx context.Context, output chan<- any, snapshot *cloudv1.RuntimeSnapshot) error {
	normalized, err := runtimesnapshot.NormalizeClone(snapshot)
	if err != nil {
		return err
	}
	digest, err := runtimesnapshot.Digest(normalized)
	if err != nil {
		return err
	}
	snapshotID := uuid.NewString()
	if err := queueEvent(ctx, output, &cloudv1.EdgeEvent_SnapshotBegin{SnapshotBegin: &cloudv1.SnapshotBegin{SnapshotId: snapshotID, Revision: normalized.GetRevision()}}); err != nil {
		return err
	}
	chunkIndex := uint32(0)
	for agentIndex, sessionIndex, allocationIndex := 0, 0, 0; agentIndex < len(normalized.Agents) || sessionIndex < len(normalized.Sessions) || allocationIndex < len(normalized.Allocations); chunkIndex++ {
		chunk := &cloudv1.SnapshotChunk{SnapshotId: snapshotID, ChunkIndex: chunkIndex}
		remaining := snapshotChunkSize
		for agentIndex < len(normalized.Agents) && remaining > 0 {
			chunk.Agents = append(chunk.Agents, normalized.Agents[agentIndex])
			agentIndex++
			remaining--
		}
		for sessionIndex < len(normalized.Sessions) && remaining > 0 {
			chunk.Sessions = append(chunk.Sessions, normalized.Sessions[sessionIndex])
			sessionIndex++
			remaining--
		}
		for allocationIndex < len(normalized.Allocations) && remaining > 0 {
			chunk.Allocations = append(chunk.Allocations, normalized.Allocations[allocationIndex])
			allocationIndex++
			remaining--
		}
		if err := queueEvent(ctx, output, &cloudv1.EdgeEvent_SnapshotChunk{SnapshotChunk: chunk}); err != nil {
			return err
		}
	}
	return queueEvent(ctx, output, &cloudv1.EdgeEvent_SnapshotEnd{SnapshotEnd: &cloudv1.SnapshotEnd{SnapshotId: snapshotID, Revision: normalized.GetRevision(), ChunkCount: chunkIndex, Digest: digest}})
}

func queueEvent(ctx context.Context, output chan<- any, payload any) error {
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case output <- payload:
		return nil
	default:
		return errors.New("EdgeControl writer queue is full")
	}
}

func edgeEvent(config Config, connectionID string, sequence uint64, payload any) *cloudv1.EdgeEvent {
	event := &cloudv1.EdgeEvent{ProtocolVersion: control.ProtocolVersion, MessageId: uuid.NewString(), SenderId: config.EdgeID, BootId: config.BootID, ConnectionId: connectionID, StreamSeq: sequence, SentAt: timestamppb.Now()}
	switch typed := payload.(type) {
	case *cloudv1.EdgeEvent_Hello:
		event.Payload = typed
	case *cloudv1.EdgeEvent_SnapshotBegin:
		event.Payload = typed
	case *cloudv1.EdgeEvent_SnapshotChunk:
		event.Payload = typed
	case *cloudv1.EdgeEvent_SnapshotEnd:
		event.Payload = typed
	case *cloudv1.EdgeEvent_RuntimeDelta:
		event.Payload = typed
	case *cloudv1.EdgeEvent_Heartbeat:
		event.Payload = typed
	case *cloudv1.EdgeEvent_ConfigApplied:
		event.Payload = typed
	case *cloudv1.EdgeEvent_UsageBatch:
		event.Payload = typed
	case *cloudv1.EdgeEvent_CommandResult:
		event.Payload = typed
	case *cloudv1.EdgeEvent_CertificateApplied:
		event.Payload = typed
	default:
		panic("unsupported EdgeEvent payload")
	}
	return event
}

func validateWelcome(command *cloudv1.ControllerCommand, connectionID string) (*cloudv1.EdgeWelcome, error) {
	if command == nil || command.GetWelcome() == nil {
		return nil, errors.New("first Controller payload must be EdgeWelcome")
	}
	if command.GetProtocolVersion() != control.ProtocolVersion || command.GetWelcome().GetAcceptedProtocolVersion() != control.ProtocolVersion {
		return nil, fmt.Errorf("Controller accepted unsupported protocol version %d", command.GetWelcome().GetAcceptedProtocolVersion())
	}
	if err := validateCommand(command, connectionID, command.GetSenderId(), command.GetBootId(), 1); err != nil {
		return nil, err
	}
	heartbeat := command.GetWelcome().GetHeartbeat()
	if heartbeat == nil || heartbeat.GetInterval() == nil || heartbeat.GetTimeout() == nil || heartbeat.GetInterval().AsDuration() <= 0 || heartbeat.GetTimeout().AsDuration() < heartbeat.GetInterval().AsDuration() {
		return nil, errors.New("EdgeWelcome heartbeat policy is invalid")
	}
	return command.GetWelcome(), nil
}

func validateCommand(command *cloudv1.ControllerCommand, connectionID, controllerID, controllerBootID string, sequence uint64) error {
	if command == nil || command.GetProtocolVersion() != control.ProtocolVersion || strings.TrimSpace(command.GetMessageId()) == "" || strings.TrimSpace(controllerID) == "" || strings.TrimSpace(controllerBootID) == "" {
		return errors.New("Controller command envelope is invalid")
	}
	if command.GetSenderId() != controllerID || command.GetBootId() != controllerBootID || command.GetConnectionId() != connectionID || command.GetStreamSeq() != sequence {
		return errors.New("Controller command identity, generation, or sequence does not match")
	}
	if command.GetSentAt() == nil || command.GetSentAt().CheckValid() != nil {
		return errors.New("Controller command sent_at is invalid")
	}
	return nil
}

// IsExpectedClosure 区分 shutdown/cancel 与需要重连的 Controller 故障。
func IsExpectedClosure(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	code := status.Code(err)
	return code == codes.Canceled || code == codes.DeadlineExceeded
}
