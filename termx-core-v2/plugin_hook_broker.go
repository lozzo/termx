package termxcorev2

import (
	"context"
	"sync"

	"github.com/lozzow/termx/termx-shared/plugin"
)

// PluginHookFilter 描述 daemon plugin hook stream 的订阅过滤条件。
// 过滤只使用 daemon/core 拥有的事件字段；EndpointID/TerminalRef 必须由 client 侧补充，不能在 daemon filter 中出现。
type PluginHookFilter struct {
	Types            []plugin.EventType
	DaemonTerminalID plugin.TerminalID
}

type pluginHookBroker struct {
	mu          sync.RWMutex
	buffer      int
	subscribers map[uint64]pluginHookSubscription
	nextID      uint64
	closed      bool
}

type pluginHookSubscription struct {
	filter PluginHookFilter
	ch     chan plugin.HookEvent
}

func newPluginHookBroker(buffer int) *pluginHookBroker {
	if buffer <= 0 {
		buffer = 1
	}
	return &pluginHookBroker{
		buffer:      buffer,
		subscribers: make(map[uint64]pluginHookSubscription),
	}
}

func (broker *pluginHookBroker) subscribe(ctx context.Context, filter PluginHookFilter) <-chan plugin.HookEvent {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	ch := make(chan plugin.HookEvent, broker.buffer)
	if broker.closed {
		close(ch)
		return ch
	}
	broker.nextID++
	id := broker.nextID
	broker.subscribers[id] = pluginHookSubscription{filter: filter, ch: ch}
	go func() {
		<-ctx.Done()
		broker.unsubscribe(id)
	}()
	return ch
}

func (broker *pluginHookBroker) publish(event plugin.HookEvent) {
	broker.mu.RLock()
	defer broker.mu.RUnlock()
	if broker.closed {
		return
	}
	for _, sub := range broker.subscribers {
		if !pluginHookMatchesFilter(event, sub.filter) {
			continue
		}
		select {
		case sub.ch <- event.Clone():
		default:
			// plugin hook stream 是通知边界；router/delivery 策略会在下一层处理慢订阅。
		}
	}
}

func (broker *pluginHookBroker) unsubscribe(id uint64) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	sub, ok := broker.subscribers[id]
	if !ok {
		return
	}
	delete(broker.subscribers, id)
	close(sub.ch)
}

func (broker *pluginHookBroker) close() {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.closed = true
	for id, sub := range broker.subscribers {
		delete(broker.subscribers, id)
		close(sub.ch)
	}
}

func pluginHookMatchesFilter(event plugin.HookEvent, filter PluginHookFilter) bool {
	if filter.DaemonTerminalID != "" && filter.DaemonTerminalID != event.DaemonTerminalID {
		return false
	}
	if len(filter.Types) == 0 {
		return true
	}
	for _, typ := range filter.Types {
		if typ == event.Type {
			return true
		}
	}
	return false
}
