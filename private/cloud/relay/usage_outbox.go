package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lozzow/termx/private/cloud/control-plane/usage"
)

// UsageOutbox 是 Relay 签名用量事件的 durable at-least-once 队列。
// 事件先落盘再允许上传；只有 Control Plane 明确 ack 后才删除，重启会加载同一 event_id/sequence。
type UsageOutbox struct {
	mu   sync.Mutex
	path string
}

// UsageRecord 把签名 usage event 与签发它的原始 regional signed lease 一起持久化。
// Control Plane 可在 Relay 重启后重新验 lease 并恢复 quota/session binding。
type UsageRecord struct {
	SignedLease []byte      `json:"signed_lease"`
	Event       usage.Event `json:"event"`
}

// NewUsageOutbox 创建固定路径 outbox；父目录由首次写入以 0700 创建。
func NewUsageOutbox(path string) (*UsageOutbox, error) {
	if path == "" || filepath.Base(path) == "." {
		return nil, fmt.Errorf("Relay usage outbox path is required")
	}
	return &UsageOutbox{path: path}, nil
}

// Enqueue 原子追加签名事件；重复 event_id/sequence/body 会保持一份，不同 body 冲突 fail closed。
func (outbox *UsageOutbox) Enqueue(records ...UsageRecord) error {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	current, err := outbox.loadLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, record := range records {
		event := record.Event
		if event.EventID == "" || event.Sequence == 0 || len(event.Signature) == 0 || len(record.SignedLease) == 0 {
			return fmt.Errorf("invalid Relay usage event")
		}
		encoded, _ := json.Marshal(record)
		duplicate := false
		for _, existing := range current {
			if existing.Event.EventID != event.EventID || existing.Event.Sequence != event.Sequence {
				continue
			}
			existingEncoded, _ := json.Marshal(existing)
			if string(existingEncoded) != string(encoded) {
				return fmt.Errorf("Relay usage outbox idempotency conflict")
			}
			duplicate = true
			break
		}
		if !duplicate {
			current = append(current, record)
		}
	}
	return outbox.saveLocked(current)
}

// Pending 返回当前待确认事件的深拷贝，顺序与持久化队列一致。
func (outbox *UsageOutbox) Pending() ([]UsageRecord, error) {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	events, err := outbox.loadLocked()
	if os.IsNotExist(err) {
		return nil, nil
	}
	return events, err
}

// Ack 删除 Control Plane 已确认的 event_id/sequence；未知 ack 不改变队列并返回错误。
func (outbox *UsageOutbox) Ack(eventID string, sequence uint64) error {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	events, err := outbox.loadLocked()
	if err != nil {
		return err
	}
	index := -1
	for candidate, record := range events {
		if record.Event.EventID == eventID && record.Event.Sequence == sequence {
			index = candidate
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("Relay usage ack is unknown")
	}
	events = append(events[:index], events[index+1:]...)
	return outbox.saveLocked(events)
}

func (outbox *UsageOutbox) loadLocked() ([]UsageRecord, error) {
	data, err := os.ReadFile(outbox.path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 64<<20 {
		return nil, fmt.Errorf("Relay usage outbox is invalid")
	}
	var events []UsageRecord
	decoderErr := json.Unmarshal(data, &events)
	if decoderErr != nil {
		return nil, fmt.Errorf("decode Relay usage outbox: %w", decoderErr)
	}
	return events, nil
}

func (outbox *UsageOutbox) saveLocked(events []UsageRecord) error {
	directory := filepath.Dir(outbox.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(events)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".relay-usage-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, outbox.path); err != nil {
		return err
	}
	committed = true
	return nil
}
