package registry

import (
	"strings"
	"sync"
	"time"
)

type signalQueue[T any] struct {
	mu      sync.Mutex
	keyFn   func(T) string
	items   map[string]T
	queues  map[string][]T
	waiters map[string][]chan struct{}
}

func newSignalQueue[T any](keyFn func(T) string) signalQueue[T] {
	if keyFn == nil {
		panic("registry signalQueue keyFn is required")
	}
	return signalQueue[T]{
		keyFn:   keyFn,
		items:   make(map[string]T),
		queues:  make(map[string][]T),
		waiters: make(map[string][]chan struct{}),
	}
}

func (q *signalQueue[T]) enqueue(machineID string, item T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	itemID := q.key(item)
	if itemID != "" {
		q.items[itemID] = item
	}
	machineID = strings.TrimSpace(machineID)
	if machineID != "" {
		q.queues[machineID] = append(q.queues[machineID], item)
	}
}

func (q *signalQueue[T]) dequeue(machineID string) (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	var zero T
	machineID = strings.TrimSpace(machineID)
	queue := q.queues[machineID]
	if len(queue) == 0 {
		return zero, false
	}
	item := queue[0]
	queue = queue[1:]
	if len(queue) == 0 {
		delete(q.queues, machineID)
	} else {
		q.queues[machineID] = queue
	}
	return item, true
}

func (q *signalQueue[T]) notify(machineID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	q.notifyLocked(strings.TrimSpace(machineID))
}

func (q *signalQueue[T]) wait(machineID string, timeout time.Duration) bool {
	waiter := q.addWaiter(machineID)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-waiter:
		return true
	case <-timer.C:
		q.removeWaiter(machineID, waiter)
		return false
	}
}

func (q *signalQueue[T]) get(itemID string) (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	item, ok := q.items[strings.TrimSpace(itemID)]
	return item, ok
}

func (q *signalQueue[T]) set(item T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	if itemID := q.key(item); itemID != "" {
		q.items[itemID] = item
	}
}

func (q *signalQueue[T]) delete(itemID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	delete(q.items, strings.TrimSpace(itemID))
}

func (q *signalQueue[T]) pruneExpired(isExpired func(T) bool) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	removed := 0
	for id, item := range q.items {
		if isExpired(item) {
			delete(q.items, id)
			removed++
		}
	}
	q.pruneQueuesLocked(func(queued T) (T, bool) {
		item, ok := q.items[q.key(queued)]
		return item, ok
	})
	return removed
}

func (q *signalQueue[T]) addWaiter(machineID string) chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	waiter := make(chan struct{}, 1)
	machineID = strings.TrimSpace(machineID)
	q.waiters[machineID] = append(q.waiters[machineID], waiter)
	return waiter
}

func (q *signalQueue[T]) removeWaiter(machineID string, target chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	machineID = strings.TrimSpace(machineID)
	waiters := q.waiters[machineID]
	for i, waiter := range waiters {
		if waiter == target {
			waiters = append(waiters[:i], waiters[i+1:]...)
			if len(waiters) == 0 {
				delete(q.waiters, machineID)
			} else {
				q.waiters[machineID] = waiters
			}
			return
		}
	}
}

func (q *signalQueue[T]) pruneQueues(keep func(T) (T, bool)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	q.pruneQueuesLocked(keep)
}

func (q *signalQueue[T]) deleteQueue(machineID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	delete(q.queues, strings.TrimSpace(machineID))
}

func (q *signalQueue[T]) itemsSnapshot() []T {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	out := make([]T, 0, len(q.items))
	for _, item := range q.items {
		out = append(out, item)
	}
	return out
}

func (q *signalQueue[T]) waiterCount(machineID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLocked()
	return len(q.waiters[strings.TrimSpace(machineID)])
}

func (q *signalQueue[T]) notifyLocked(machineID string) {
	waiters := q.waiters[machineID]
	delete(q.waiters, machineID)
	for _, waiter := range waiters {
		select {
		case waiter <- struct{}{}:
		default:
		}
	}
}

func (q *signalQueue[T]) pruneQueuesLocked(keep func(T) (T, bool)) {
	for machineID, queue := range q.queues {
		kept := queue[:0]
		for _, queued := range queue {
			item, ok := q.items[q.key(queued)]
			if !ok {
				continue
			}
			item, ok = keep(item)
			if !ok {
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) == 0 {
			delete(q.queues, machineID)
			continue
		}
		q.queues[machineID] = kept
	}
}

func (q *signalQueue[T]) ensureLocked() {
	if q.items == nil {
		q.items = make(map[string]T)
	}
	if q.queues == nil {
		q.queues = make(map[string][]T)
	}
	if q.waiters == nil {
		q.waiters = make(map[string][]chan struct{})
	}
}

func (q *signalQueue[T]) key(item T) string {
	if q.keyFn == nil {
		panic("registry signalQueue keyFn is required")
	}
	return strings.TrimSpace(q.keyFn(item))
}
