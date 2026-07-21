// Package relaycontrol 实现 Controller 与 Edge 内 Relay runtime 的独立控制通道。
package relaycontrol

import (
	"errors"
	"sync"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// ErrPublisherBackpressure 表示 attached Relay 尚未消费前一条 command。
var ErrPublisherBackpressure = errors.New("Relay control publisher backpressure")

// Publisher 只发布 Relay command，不保存 allocation 或 usage 真值。
type Publisher struct {
	mu          sync.RWMutex
	subscribers map[string]map[uint64]chan *cloudpb.RelayControlCommand
	nextID      uint64
}

// NewPublisher 创建空 Relay command publisher。
func NewPublisher() *Publisher {
	return &Publisher{subscribers: make(map[string]map[uint64]chan *cloudpb.RelayControlCommand)}
}

// PublishCommand 向当前 Relay attachment 有界投递命令。
func (publisher *Publisher) PublishCommand(relayID string, command *cloudpb.RelayControlCommand) error {
	if publisher == nil || relayID == "" || command == nil || command.GetCommandId() == "" || command.GetTarget() == nil || command.GetTarget().GetRelayId() != relayID {
		return errors.New("invalid Relay control command")
	}
	publisher.mu.RLock()
	defer publisher.mu.RUnlock()
	for _, subscriber := range publisher.subscribers[relayID] {
		select {
		case subscriber <- proto.Clone(command).(*cloudpb.RelayControlCommand):
		default:
			return ErrPublisherBackpressure
		}
	}
	return nil
}

// Subscribe 创建单 attachment 的有界 command channel。
func (publisher *Publisher) Subscribe(relayID string) (<-chan *cloudpb.RelayControlCommand, func()) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.nextID++
	id := publisher.nextID
	channel := make(chan *cloudpb.RelayControlCommand, 1)
	if publisher.subscribers[relayID] == nil {
		publisher.subscribers[relayID] = make(map[uint64]chan *cloudpb.RelayControlCommand)
	}
	publisher.subscribers[relayID][id] = channel
	return channel, func() {
		publisher.mu.Lock()
		delete(publisher.subscribers[relayID], id)
		if len(publisher.subscribers[relayID]) == 0 {
			delete(publisher.subscribers, relayID)
		}
		publisher.mu.Unlock()
	}
}
