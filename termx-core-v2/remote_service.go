package termxcorev2

import (
	"context"
	"errors"

	"github.com/lozzow/termx/internal/protocol"
)

var ErrRemoteServiceUnavailable = errors.New("remote service is not configured")

// RemoteService 是 core-v2 daemon 对 remote runtime 的 typed hook。
// 它只接受 internal/protocol 的 core-v2 domain 类型，不承接旧 bytes handler。
type RemoteService interface {
	Status(ctx context.Context) (protocol.RemoteStatus, error)
	PairStart(ctx context.Context, params protocol.RemotePairStartParams) (protocol.RemotePairStartResult, error)
	LocalEnable(ctx context.Context, params protocol.RemoteLocalEnableParams) (protocol.RemoteLocalStatus, error)
	LocalStatus(ctx context.Context) (protocol.RemoteLocalStatus, error)
	LocalDisable(ctx context.Context) (protocol.RemoteLocalStatus, error)
}
