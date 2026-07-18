package protocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/shared/remoteauth"
)

// ApplicationClient 组合单条 ready connection 的 framing client 与 Proto application session。
// terminal/path command 必须通过 ApplicationSession；history/file/storage 等尚未迁移切片仍由嵌入的 framing client 承载。
type ApplicationClient struct {
	*internalprotocol.Client
	*clientruntime.ApplicationSession
	ready    clientruntime.ApplicationReadySession
	path     string
	evidence clientruntime.ReadySessionEvidence
}

// MarkReady 在 adapter 按 route 类型完成身份边界、authorization 与 Hello 后冻结 readiness evidence。
// 该方法只能在 session 发布给 SessionOwner 前调用一次；缺失或重复 evidence 都直接失败。
func (client *ApplicationClient) MarkReady(evidence clientruntime.ReadySessionEvidence) error {
	if client == nil {
		return errors.New("protocol application client is required")
	}
	if client.evidence.IdentityVerified || client.evidence.AuthorizationVerified || client.evidence.ProtocolVersion != 0 {
		return errors.New("protocol application client readiness is already frozen")
	}
	if err := evidence.Validate(endpoint.DaemonIdentity{}); err != nil {
		return err
	}
	client.evidence = evidence
	return nil
}

// VerifyDaemonIdentity 通过当前已经完成 Hello 的 authenticated application session读取 daemon public identity，
// 校验 public key fingerprint，并在 Endpoint 已有 pin 时要求 DeviceID/Fingerprint 精确匹配。
func VerifyDaemonIdentity(ctx context.Context, session *clientruntime.ApplicationSession, expected endpoint.DaemonIdentity) (endpoint.DaemonIdentity, error) {
	result, err := VerifyDaemonIdentityResult(ctx, session, expected)
	if err != nil {
		return endpoint.DaemonIdentity{}, err
	}
	identity := result.GetIdentity()
	return endpoint.DaemonIdentity{DeviceID: identity.GetDeviceId(), DeviceFingerprint: identity.GetDeviceFingerprint()}, nil
}

// VerifyDaemonIdentityResult 执行 fresh challenge 并返回已验签的完整 Proto identity result，供需要展示 public key 的 application consumer 使用。
// 它不授予 capability；调用方仍只能在 local/SSH 已授权 session 或 managed remote-auth 后调用。
func VerifyDaemonIdentityResult(ctx context.Context, session *clientruntime.ApplicationSession, expected endpoint.DaemonIdentity) (*apipb.ClientAccessIdentityResult, error) {
	if session == nil {
		return nil, errors.New("application session is required for daemon identity proof")
	}
	challenge := make([]byte, remoteauth.DeviceIdentityChallengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("generate daemon identity challenge: %w", err)
	}
	result, err := session.ClientAccessIdentity(ctx, &apipb.ClientAccessIdentityCommand{Challenge: challenge})
	if err != nil {
		return nil, fmt.Errorf("read daemon identity proof: %w", err)
	}
	identity := result.GetIdentity()
	if identity == nil || !bytes.Equal(result.GetChallenge(), challenge) {
		return nil, errors.New("daemon identity proof is incomplete")
	}
	verified := endpoint.DaemonIdentity{DeviceID: identity.GetDeviceId(), DeviceFingerprint: identity.GetDeviceFingerprint()}
	if err := verified.Validate(true); err != nil {
		return nil, fmt.Errorf("daemon identity proof is invalid: %w", err)
	}
	if err := remoteauth.VerifyDeviceIdentityProof(challenge, identity.GetDeviceId(), identity.GetDeviceFingerprint(), identity.GetDevicePublicKey(), result.GetProof()); err != nil {
		return nil, err
	}
	if !expected.Empty() && verified != expected {
		return nil, errors.New("daemon identity proof does not match endpoint pin")
	}
	return result, nil
}

// HistoryWindow 显式选择 Proto application API，消除 framing client 旧同名方法的嵌入歧义。
func (client *ApplicationClient) HistoryWindow(ctx context.Context, command *apipb.HistoryWindowCommand) (*apipb.HistoryWindowResult, error) {
	return client.ApplicationSession.HistoryWindow(ctx, command)
}

// HistoryCopy 从 frozen Proto history window 复制 authoritative text。
func (client *ApplicationClient) HistoryCopy(ctx context.Context, command *apipb.HistoryCopyCommand) (*apipb.HistoryCopyResult, error) {
	return client.ApplicationSession.HistoryCopy(ctx, command)
}

// HistoryRelease 释放 Proto history token。
func (client *ApplicationClient) HistoryRelease(ctx context.Context, command *apipb.HistoryReleaseCommand) error {
	return client.ApplicationSession.HistoryRelease(ctx, command)
}

// LiveScreen 读取 Proto native screen projection。
func (client *ApplicationClient) LiveScreen(ctx context.Context, command *apipb.LiveScreenGetCommand) (*apipb.NativeScreenResult, error) {
	return client.ApplicationSession.LiveScreen(ctx, command)
}

// NewApplicationClient 把已完成 Hello/auth 的 framing client 绑定到 runtime winner stamp。
// 构造失败时不得返回部分可用客户端，也不得 fallback 到旧 terminal method。
func NewApplicationClient(client *internalprotocol.Client, stamp clientruntime.EndpointSessionStamp) (*ApplicationClient, error) {
	return NewApplicationClientWithObservedPath(client, stamp, "protocol")
}

// NewApplicationClientWithObservedPath 把已完成 Hello/auth 的 framing client 绑定到 attempt stamp 与观测路径。
// observedPath 仅用于诊断，不参与 route 选择或 session identity。
func NewApplicationClientWithObservedPath(client *internalprotocol.Client, stamp clientruntime.EndpointSessionStamp, observedPath string) (*ApplicationClient, error) {
	if client == nil {
		return nil, errors.New("protocol client is required")
	}
	application, err := clientruntime.NewApplicationSession(stamp, client)
	if err != nil {
		return nil, err
	}
	return &ApplicationClient{Client: client, ApplicationSession: application, path: observedPath}, nil
}

// NewOwnedApplicationClient 把 concrete protocol stream primitives 与 SessionOwner-fenced application session 组合。
// terminal/file stream 仍由同一 raw connection 提供，但所有 Proto command、event 与 Close 都先经过 owner generation 校验。
func NewOwnedApplicationClient(client *internalprotocol.Client, ready clientruntime.ApplicationReadySession) (*ApplicationClient, error) {
	if client == nil || ready == nil {
		return nil, errors.New("protocol client and owned ready session are required")
	}
	application, err := clientruntime.NewApplicationSession(ready.Stamp(), ready)
	if err != nil {
		return nil, err
	}
	return &ApplicationClient{Client: client, ApplicationSession: application, ready: ready, path: ready.ObservedPath(), evidence: ready.Readiness()}, nil
}

// ObservedPath 返回 route adapter 观测到的实际连接路径。
func (client *ApplicationClient) ObservedPath() string {
	if client == nil {
		return ""
	}
	return client.path
}

// Readiness 返回 adapter 在 session 发布前冻结的 identity、authorization 与 Hello 证据。
func (client *ApplicationClient) Readiness() clientruntime.ReadySessionEvidence {
	if client == nil {
		return clientruntime.ReadySessionEvidence{}
	}
	if client.ready != nil {
		return client.ready.Readiness()
	}
	return client.evidence
}

// ExecuteApplication 运输完整 generated Proto command；owned client 会先通过 SessionOwner generation fence。
func (client *ApplicationClient) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	if client.ready != nil {
		return client.ready.ExecuteApplication(ctx, command)
	}
	return client.Client.ExecuteApplication(ctx, command)
}

// ExecuteApplicationTerminal 为 resource-producing binding operation 保留有界 terminal response。
// 该选择来自上层 owner；protocol adapter 只转交通用能力，不解释 command oneof。
func (client *ApplicationClient) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	if executor, ok := client.ready.(clientruntime.TerminalResponseApplicationExecutor); ok {
		return executor.ExecuteApplicationTerminal(ctx, command)
	}
	return client.Client.ExecuteApplicationTerminal(ctx, command)
}

// ApplicationEvents 返回同一 ready connection 的 generated Proto event stream。
func (client *ApplicationClient) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	if client.ready != nil {
		return client.ready.ApplicationEvents(ctx)
	}
	return client.Client.ApplicationEvents(ctx)
}

// Done 返回 ready connection 的终止信号。
func (client *ApplicationClient) Done() <-chan struct{} {
	if client.ready != nil {
		return client.ready.Done()
	}
	return client.Client.Done()
}

// Err 返回 ready connection 的终止原因。
func (client *ApplicationClient) Err() error {
	if client.ready != nil {
		return client.ready.Err()
	}
	return client.Client.Err()
}

// Close 释放 owner 中精确匹配的 generation；底层 protocol connection 由 owner session 一并关闭。
func (client *ApplicationClient) Close() error {
	if client.ready != nil {
		return client.ready.Close()
	}
	return client.Client.Close()
}

var _ clientruntime.ApplicationReadySession = (*ApplicationClient)(nil)
