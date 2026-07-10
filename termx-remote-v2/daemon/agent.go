package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	hubclient "github.com/lozzow/termx/termx-hub/client"
)

// ErrHubKick 表示 Hub 管理面显式要求当前 daemon agent 下线。
// 它只结束当前 Hub agent session，不修改 core-v2 terminal lifecycle 或本地 socket listener。
var ErrHubKick = errors.New("termx hub kicked daemon agent")

// HubStream 是 daemon agent 消费的 Hub 双向信令边界。
// Hub 只中继 discovery/offer/answer；capability grant 的验证和 protocol session 创建由 OfferAnswerer/SessionAcceptor 完成。
type HubStream interface {
	Receive() (hubclient.Message, error)
	Heartbeat(sessionID string, terminals []hubclient.Terminal) error
	SendAnswer(answer hubclient.Answer) error
	Close() error
}

// HubConnect 建立并注册一个 daemon agent stream。
// bearer token 只认证 agent 到 Hub，不授予客户端访问 daemon 的 capability。
type HubConnect func(context.Context, string, hubclient.Registration) (HubStream, hubclient.RegistrationAck, error)

// OfferAnswerer 消费 Hub 中继的 offer，校验 capability grant，并建立 WebRTC DataChannel session。
// 授权或协商失败返回局部 answer error，不得启动 core session 或 fallback 到本地/SSH transport。
type OfferAnswerer interface {
	Answer(context.Context, hubclient.Offer, hubclient.RegistrationAck) (hubclient.Answer, error)
}

// Agent 管理一个 daemon 到一个 Hub 的注册、心跳和 offer/answer stream。
// terminal inventory 是 core-v2 的只读投影；Agent 不拥有 terminal lifecycle、history truth 或 endpoint manager state。
type Agent struct {
	BearerToken       string
	Registration      hubclient.Registration
	Connect           HubConnect
	Answerer          OfferAnswerer
	Inventory         func(context.Context) []hubclient.Terminal
	HeartbeatInterval time.Duration
}

// Run 持续处理当前 Hub session，直到 context、stream 错误或显式 kick。
// 单个 offer 失败只发送该 session 的 answer error；不会让其他 endpoint、terminal 或本地 daemon listener 离线。
func (agent Agent) Run(ctx context.Context) error {
	if agent.Connect == nil {
		return fmt.Errorf("remote daemon hub connector is not configured")
	}
	if agent.Answerer == nil {
		return fmt.Errorf("remote daemon offer answerer is not configured")
	}
	stream, ack, err := agent.Connect(ctx, agent.BearerToken, agent.Registration)
	if err != nil {
		return err
	}
	defer stream.Close()
	heartbeatInterval := agent.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = time.Duration(ack.HeartbeatSeconds) * time.Second
	}
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 * time.Second
	}
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	heartbeatErr := make(chan error, 1)
	go agent.runHeartbeat(heartbeatCtx, stream, ack.SessionID, heartbeatInterval, heartbeatErr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-heartbeatErr:
			return err
		default:
		}
		message, err := stream.Receive()
		if err != nil {
			return err
		}
		if reason := strings.TrimSpace(message.Kick); reason != "" {
			return fmt.Errorf("%w: %s", ErrHubKick, reason)
		}
		if message.Offer == nil {
			continue
		}
		answer, answerErr := agent.Answerer.Answer(ctx, *message.Offer, ack)
		answer.SessionID = message.Offer.SessionID
		if answerErr != nil {
			answer = hubclient.Answer{SessionID: message.Offer.SessionID, Error: answerErr.Error()}
		}
		if err := stream.SendAnswer(answer); err != nil {
			return err
		}
	}
}

func (agent Agent) runHeartbeat(ctx context.Context, stream HubStream, sessionID string, interval time.Duration, errCh chan<- error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var sendMu sync.Mutex
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			terminals := []hubclient.Terminal(nil)
			if agent.Inventory != nil {
				terminals = agent.Inventory(ctx)
			}
			sendMu.Lock()
			err := stream.Heartbeat(sessionID, terminals)
			sendMu.Unlock()
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}
}
