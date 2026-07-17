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
	if client == nil {
		return nil, errors.New("protocol client is required")
	}
	application, err := clientruntime.NewApplicationSession(stamp, client)
	if err != nil {
		return nil, err
	}
	return &ApplicationClient{Client: client, ApplicationSession: application}, nil
}
