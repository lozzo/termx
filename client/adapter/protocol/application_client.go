package protocol

import (
	"errors"

	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
)

// ApplicationClient 组合单条 ready connection 的 framing client 与 Proto application session。
// terminal/path command 必须通过 ApplicationSession；history/file/storage 等尚未迁移切片仍由嵌入的 framing client 承载。
type ApplicationClient struct {
	*internalprotocol.Client
	*clientruntime.ApplicationSession
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
