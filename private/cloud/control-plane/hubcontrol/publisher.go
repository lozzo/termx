// Package hubcontrol 实现 Controller 到 Hub 的真实 Proto HTTP control transport。
//
// 本包拥有 challenge、connection attachment、sender sequence 和 reconciliation 协调；
// deployment/generation/assignment 真值仍属于 hubregistry，projection 内容由上游 provider 生成。
package hubcontrol

import (
	"errors"
	"sync"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var ErrPublisherBackpressure = errors.New("Hub control publisher backpressure")

// ProjectionHead 是 Controller 当前 per-Hub projection 的 revision/digest 摘要。
type ProjectionHead struct {
	Revision uint64
	Digest   []byte
}

// Publisher 保存每个 Hub 当前 full projection，并向当前 attachment 有界发布后续 full/delta。
// 它不是持久真值；Controller 重启后应从数据库重新生成 full projection。
type Publisher struct {
	mu          sync.RWMutex
	currentFull map[string]*cloudpb.FullProjectionSnapshot
	heads       map[string]ProjectionHead
	subscribers map[string]map[uint64]chan proto.Message
	nextID      uint64
}

// NewPublisher 创建空 projection publisher。
func NewPublisher() *Publisher {
	return &Publisher{currentFull: make(map[string]*cloudpb.FullProjectionSnapshot), heads: make(map[string]ProjectionHead), subscribers: make(map[string]map[uint64]chan proto.Message)}
}

// PublishFull 设置当前完整投影并通知 attached Hub。
func (publisher *Publisher) PublishFull(snapshot *cloudpb.FullProjectionSnapshot) error {
	if snapshot == nil || snapshot.GetHubId() == "" || snapshot.GetProjectionRevision() == 0 || len(snapshot.GetSnapshotDigest()) == 0 {
		return errors.New("invalid full Hub projection")
	}
	clone := proto.Clone(snapshot).(*cloudpb.FullProjectionSnapshot)
	return publisher.publish(snapshot.GetHubId(), clone, ProjectionHead{Revision: snapshot.GetProjectionRevision(), Digest: append([]byte(nil), snapshot.GetSnapshotDigest()...)}, clone)
}

// PublishDelta 原子发布严格递增 delta 与对应的结果 full projection。
//
// Controller 是 projection 真值来源。结果 full 必须与 delta 的 Hub、revision 和 digest
// 完全一致，确保当前连接收到 delta 后，新连接仍从同一个最新状态完成 full sync。
func (publisher *Publisher) PublishDelta(delta *cloudpb.PolicyDelta, resultingFull *cloudpb.FullProjectionSnapshot) error {
	if delta == nil || delta.GetHubId() == "" || delta.GetProjectionRevision() == 0 || len(delta.GetResultingDigest()) == 0 {
		return errors.New("invalid Hub policy delta")
	}
	if resultingFull == nil || resultingFull.GetHubId() != delta.GetHubId() || resultingFull.GetProjectionRevision() != delta.GetProjectionRevision() || !equalDigest(resultingFull.GetSnapshotDigest(), delta.GetResultingDigest()) {
		return errors.New("Hub policy delta resulting full projection mismatch")
	}
	clone := proto.Clone(delta).(*cloudpb.PolicyDelta)
	fullClone := proto.Clone(resultingFull).(*cloudpb.FullProjectionSnapshot)
	return publisher.publish(delta.GetHubId(), clone, ProjectionHead{Revision: delta.GetProjectionRevision(), Digest: append([]byte(nil), delta.GetResultingDigest()...)}, fullClone)
}

// PublishCommand 向当前 Hub attachment 投递一个持久 CommandOutbox child。
// command 不修改 projection head；断线或背压由 dispatcher 保留 pending 后重试。
func (publisher *Publisher) PublishCommand(hubID string, command *cloudpb.HubCommand) error {
	if hubID == "" || command == nil || command.GetCommandId() == "" || command.GetCommandKind() == cloudpb.HubCommandKind_HUB_COMMAND_KIND_UNSPECIFIED || command.GetExpiresAtUnixMillis() <= command.GetIssuedAtUnixMillis() {
		return errors.New("invalid Hub command")
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.enqueueLocked(hubID, proto.Clone(command).(*cloudpb.HubCommand))
}

func equalDigest(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (publisher *Publisher) publish(hubID string, message proto.Message, head ProjectionHead, full *cloudpb.FullProjectionSnapshot) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	current := publisher.heads[hubID]
	if current.Revision != 0 && head.Revision <= current.Revision {
		return errors.New("Hub projection revision did not advance")
	}
	if err := publisher.enqueueLocked(hubID, message); err != nil {
		return err
	}
	publisher.heads[hubID] = ProjectionHead{Revision: head.Revision, Digest: append([]byte(nil), head.Digest...)}
	if full != nil {
		publisher.currentFull[hubID] = proto.Clone(full).(*cloudpb.FullProjectionSnapshot)
	}
	return nil
}

func (publisher *Publisher) enqueueLocked(hubID string, message proto.Message) error {
	// 先检查全部 attachment，避免只向部分 Hub control stream 投递后才发现背压。
	// 这样 caller 可以安全重试同一 projection 或持久 command，不制造部分发布真值。
	for _, subscriber := range publisher.subscribers[hubID] {
		if len(subscriber) == cap(subscriber) {
			return ErrPublisherBackpressure
		}
	}
	for _, subscriber := range publisher.subscribers[hubID] {
		subscriber <- proto.Clone(message)
	}
	return nil
}

// CurrentFull 返回新 attachment 必须接收的完整投影。
func (publisher *Publisher) CurrentFull(hubID string) (*cloudpb.FullProjectionSnapshot, bool) {
	publisher.mu.RLock()
	defer publisher.mu.RUnlock()
	value := publisher.currentFull[hubID]
	if value == nil {
		return nil, false
	}
	return proto.Clone(value).(*cloudpb.FullProjectionSnapshot), true
}

// Head 返回 reconciliation 使用的当前 revision/digest。
func (publisher *Publisher) Head(hubID string) (ProjectionHead, bool) {
	publisher.mu.RLock()
	defer publisher.mu.RUnlock()
	value, ok := publisher.heads[hubID]
	value.Digest = append([]byte(nil), value.Digest...)
	return value, ok
}

// Subscribe 创建当前 attachment 的有界更新队列。
func (publisher *Publisher) Subscribe(hubID string) (<-chan proto.Message, func()) {
	publisher.mu.Lock()
	publisher.nextID++
	id := publisher.nextID
	channel := make(chan proto.Message, 16)
	if publisher.subscribers[hubID] == nil {
		publisher.subscribers[hubID] = make(map[uint64]chan proto.Message)
	}
	publisher.subscribers[hubID][id] = channel
	publisher.mu.Unlock()
	return channel, func() {
		publisher.mu.Lock()
		if subscribers := publisher.subscribers[hubID]; subscribers != nil {
			delete(subscribers, id)
			if len(subscribers) == 0 {
				delete(publisher.subscribers, hubID)
			}
		}
		publisher.mu.Unlock()
	}
}
