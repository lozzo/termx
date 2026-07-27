// Package usage 持有 Edge 唯一允许落盘的 runtime 数据：尚未被 Controller 确认的 Relay 用量事件。
package usage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
)

var eventsBucket = []byte("usage-events-v1")

// Outbox 使用单个 bbolt bucket 原子保存、读取和确认 versioned UsageEvent。
// Presence、信令、票据、credential 和在线 topology 不得写入这个数据库。
type Outbox struct {
	database *bolt.DB
}

// Open 创建或打开 usage outbox；文件和父目录仅允许当前用户访问。
func Open(path string, timeout time.Duration) (*Outbox, error) {
	path = strings.TrimSpace(path)
	if path == "" || timeout <= 0 {
		return nil, errors.New("usage outbox path and positive open timeout are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create usage outbox directory: %w", err)
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("open usage outbox: %w", err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(eventsBucket)
		return err
	}); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize usage outbox: %w", err)
	}
	return &Outbox{database: database}, nil
}

// Put 在 Relay allocation 关闭后先持久化不可变 UsageEvent，再允许控制流发送。
func (outbox *Outbox) Put(event *cloudv1.UsageEvent) error {
	if outbox == nil || outbox.database == nil {
		return errors.New("usage outbox is closed")
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		return err
	}
	return outbox.database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(eventsBucket).Put([]byte(event.GetEventId()), payload)
	})
}

// Batch 按稳定 key 顺序读取至多 limit 条事件；读取不删除，断线后可用原 event_id 重发。
func (outbox *Outbox) Batch(limit int) ([]*cloudv1.UsageEvent, error) {
	if outbox == nil || outbox.database == nil {
		return nil, errors.New("usage outbox is closed")
	}
	if limit <= 0 {
		return nil, errors.New("usage batch limit must be positive")
	}
	result := make([]*cloudv1.UsageEvent, 0, limit)
	err := outbox.database.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(eventsBucket).Cursor()
		for key, value := cursor.First(); key != nil && len(result) < limit; key, value = cursor.Next() {
			event := &cloudv1.UsageEvent{}
			if err := proto.Unmarshal(value, event); err != nil {
				return fmt.Errorf("decode usage event %q: %w", string(key), err)
			}
			if event.GetEventId() != string(key) {
				return fmt.Errorf("usage event key %q does not match payload", string(key))
			}
			if err := validateEvent(event); err != nil {
				return fmt.Errorf("validate usage event %q: %w", string(key), err)
			}
			result = append(result, event)
		}
		return nil
	})
	return result, err
}

// Ack 在单个事务中只删除 Controller 已确认提交的精确 event ID。
func (outbox *Outbox) Ack(eventIDs []string) error {
	if outbox == nil || outbox.database == nil {
		return errors.New("usage outbox is closed")
	}
	return outbox.database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(eventsBucket)
		for _, eventID := range eventIDs {
			eventID = strings.TrimSpace(eventID)
			if eventID == "" {
				return errors.New("usage ACK contains an empty event ID")
			}
			if err := bucket.Delete([]byte(eventID)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Len 返回当前未确认事件数量，供健康状态和测试检查。
func (outbox *Outbox) Len() (int, error) {
	if outbox == nil || outbox.database == nil {
		return 0, errors.New("usage outbox is closed")
	}
	count := 0
	err := outbox.database.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(eventsBucket).Stats().KeyN
		return nil
	})
	return count, err
}

// Close 刷新并关闭 bbolt 文件；未确认事件保留到下次进程启动。
func (outbox *Outbox) Close() error {
	if outbox == nil || outbox.database == nil {
		return nil
	}
	err := outbox.database.Close()
	outbox.database = nil
	return err
}

func validateEvent(event *cloudv1.UsageEvent) error {
	if event == nil || event.GetSchemaVersion() != 1 || strings.TrimSpace(event.GetEventId()) == "" || strings.TrimSpace(event.GetEdgeId()) == "" ||
		strings.TrimSpace(event.GetLeaseId()) == "" || strings.TrimSpace(event.GetAccountId()) == "" || strings.TrimSpace(event.GetDaemonId()) == "" ||
		strings.TrimSpace(event.GetClientId()) == "" || strings.TrimSpace(event.GetSessionId()) == "" || strings.TrimSpace(event.GetAllocationId()) == "" ||
		event.GetTransport() == cloudv1.RelayTransport_RELAY_TRANSPORT_UNSPECIFIED || event.GetStartedAt() == nil || event.GetEndedAt() == nil ||
		event.GetStartedAt().CheckValid() != nil || event.GetEndedAt().CheckValid() != nil || event.GetEndedAt().AsTime().Before(event.GetStartedAt().AsTime()) {
		return errors.New("UsageEvent is incomplete")
	}
	return nil
}
