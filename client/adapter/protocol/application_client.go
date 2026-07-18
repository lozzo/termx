package protocol

import (
	"context"
	"errors"

	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
)

// ApplicationClient 组合单条 ready connection 的 framing client 与 Proto application session。
// terminal/path command 必须通过 ApplicationSession；history/file/storage 等尚未迁移切片仍由嵌入的 framing client 承载。
type ApplicationClient struct {
	*internalprotocol.Client
	*clientruntime.ApplicationSession
	ready clientruntime.ApplicationReadySession
	path  string
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
	return &ApplicationClient{Client: client, ApplicationSession: application, ready: ready, path: ready.ObservedPath()}, nil
}

// ObservedPath 返回 route adapter 观测到的实际连接路径。
func (client *ApplicationClient) ObservedPath() string {
	if client == nil {
		return ""
	}
	return client.path
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
