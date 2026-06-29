package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// ProtocolStorageClient 是 TUI storage adapter 需要的最小 core-v2 protocol 客户端。
// 它只覆盖 opaque storage get/put/watch 消息链路，不暴露 terminal history contract。
type ProtocolStorageClient interface {
	StorageGet(context.Context, protocol.StorageGetParams) (*protocol.StorageEntry, error)
	StoragePut(context.Context, protocol.StoragePutParams) (*protocol.StorageEntry, error)
	Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error)
}

// ProtocolWorkbenchStorageAdapter 只把 TUI workbench opaque snapshot 读写到 core-v2 storage。
// 它属于 workbench 布局持久化链路，不持有 terminal lifecycle 或无限历史 truth。
type ProtocolWorkbenchStorageAdapter struct {
	Client ProtocolStorageClient
}

// ProtocolClipboardStorageAdapter 只把 TUI clipboard opaque snapshot 读写到 core-v2 storage。
// 它保留剪贴板列表持久化能力，不恢复旧无限历史入口。
type ProtocolClipboardStorageAdapter struct {
	Client ProtocolStorageClient
}

// LoadWorkbench 从 core-v2 opaque storage 读取 workbench snapshot。
// storage entry 不存在时返回 Found=false；解码失败必须显式返回错误。
func (adapter ProtocolWorkbenchStorageAdapter) LoadWorkbench(ctx context.Context, ref state.WorkbenchStorageRef) (WorkbenchStorageLoadResult, error) {
	entry, err := adapter.Client.StorageGet(ctx, protocol.StorageGetParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
	})
	if err != nil {
		if isStorageNotFound(err) {
			return WorkbenchStorageLoadResult{Found: false}, nil
		}
		return WorkbenchStorageLoadResult{}, err
	}
	if entry == nil || len(entry.Value) == 0 {
		return WorkbenchStorageLoadResult{Found: false}, nil
	}
	snapshot, err := state.DecodeWorkbenchStorageSnapshot(entry.Value)
	if err != nil {
		return WorkbenchStorageLoadResult{}, err
	}
	return WorkbenchStorageLoadResult{
		Snapshot: snapshot,
		Version:  entry.Version,
		Found:    true,
	}, nil
}

// LoadClipboard 从 core-v2 opaque storage 读取剪贴板列表 snapshot。
// 该数据属于 TUI schema；core-v2 只负责保存 opaque bytes 和版本号。
func (adapter ProtocolClipboardStorageAdapter) LoadClipboard(ctx context.Context, ref state.ClipboardStorageRef) (ClipboardStorageLoadResult, error) {
	entry, err := adapter.Client.StorageGet(ctx, protocol.StorageGetParams{
		AppID:   ref.AppID,
		Scope:   protocol.StorageScope(ref.Scope),
		OwnerID: ref.OwnerID,
		Key:     ref.Key,
	})
	if err != nil {
		if isStorageNotFound(err) {
			return ClipboardStorageLoadResult{Found: false}, nil
		}
		return ClipboardStorageLoadResult{}, err
	}
	if entry == nil || len(entry.Value) == 0 {
		return ClipboardStorageLoadResult{Found: false}, nil
	}
	snapshot, err := state.DecodeClipboardStorageSnapshot(entry.Value)
	if err != nil {
		return ClipboardStorageLoadResult{}, err
	}
	return ClipboardStorageLoadResult{
		Snapshot: snapshot,
		Version:  entry.Version,
		Found:    true,
	}, nil
}

// SaveWorkbench 把 reducer 生成的 workbench snapshot 写回 core-v2 storage。
// 版本冲突会映射为 ErrWorkbenchStorageConflict，调用方负责重新加载或提示。
func (adapter ProtocolWorkbenchStorageAdapter) SaveWorkbench(ctx context.Context, req WorkbenchStorageSaveRequest) (WorkbenchStorageSaveResult, error) {
	value, err := state.EncodeWorkbenchStorageSnapshotValue(req.Snapshot)
	if err != nil {
		return WorkbenchStorageSaveResult{}, err
	}
	entry, err := adapter.Client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           req.Ref.AppID,
		Scope:           protocol.StorageScope(req.Ref.Scope),
		OwnerID:         req.Ref.OwnerID,
		Key:             req.Ref.Key,
		Value:           value,
		CheckVersion:    req.CheckVersion,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		if isStorageVersionConflict(err) {
			return WorkbenchStorageSaveResult{}, fmt.Errorf("%w: %v", ErrWorkbenchStorageConflict, err)
		}
		return WorkbenchStorageSaveResult{}, err
	}
	return WorkbenchStorageSaveResult{
		Ref:     req.Ref.WithVersion(entry.Version),
		Version: entry.Version,
	}, nil
}

// SaveClipboard 把剪贴板列表 snapshot 写回 core-v2 storage。
// 版本冲突会映射为 ErrClipboardStorageConflict，避免静默覆盖其他客户端更新。
func (adapter ProtocolClipboardStorageAdapter) SaveClipboard(ctx context.Context, req ClipboardStorageSaveRequest) (ClipboardStorageSaveResult, error) {
	value, err := state.EncodeClipboardStorageSnapshotValue(req.Snapshot)
	if err != nil {
		return ClipboardStorageSaveResult{}, err
	}
	entry, err := adapter.Client.StoragePut(ctx, protocol.StoragePutParams{
		AppID:           req.Ref.AppID,
		Scope:           protocol.StorageScope(req.Ref.Scope),
		OwnerID:         req.Ref.OwnerID,
		Key:             req.Ref.Key,
		Value:           value,
		CheckVersion:    req.CheckVersion,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		if isStorageVersionConflict(err) {
			return ClipboardStorageSaveResult{}, fmt.Errorf("%w: %v", ErrClipboardStorageConflict, err)
		}
		return ClipboardStorageSaveResult{}, err
	}
	return ClipboardStorageSaveResult{
		Ref:     req.Ref.WithVersion(entry.Version),
		Version: entry.Version,
	}, nil
}

// WatchWorkbench 订阅 core-v2 storage.changed 并投影为 workbench storage event。
// 事件只按 storage ref 过滤，不携带或推断 terminal runtime 状态。
func (adapter ProtocolWorkbenchStorageAdapter) WatchWorkbench(ctx context.Context, ref state.WorkbenchStorageRef) (<-chan WorkbenchStorageEvent, error) {
	events, err := adapter.Client.Events(ctx, protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     ref.AppID,
		StorageScope:     protocol.StorageScope(ref.Scope),
		StorageOwnerID:   ref.OwnerID,
		StorageKeyPrefix: ref.KeyPrefix(),
	})
	if err != nil {
		return nil, err
	}
	out := make(chan WorkbenchStorageEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Storage == nil {
					continue
				}
				changed := WorkbenchStorageEvent{
					Ref: state.WorkbenchStorageRef{
						AppID:   event.Storage.AppID,
						Scope:   string(event.Storage.Scope),
						OwnerID: event.Storage.OwnerID,
						Key:     event.Storage.Key,
						Version: event.Storage.Version,
					},
					Version: event.Storage.Version,
					Op:      event.Storage.Op,
				}
				select {
				case out <- changed:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// WatchClipboard 订阅 core-v2 storage.changed 并投影为 clipboard storage event。
// ctx 取消或底层事件流关闭时，返回通道会关闭。
func (adapter ProtocolClipboardStorageAdapter) WatchClipboard(ctx context.Context, ref state.ClipboardStorageRef) (<-chan ClipboardStorageEvent, error) {
	events, err := adapter.Client.Events(ctx, protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     ref.AppID,
		StorageScope:     protocol.StorageScope(ref.Scope),
		StorageOwnerID:   ref.OwnerID,
		StorageKeyPrefix: ref.KeyPrefix(),
	})
	if err != nil {
		return nil, err
	}
	out := make(chan ClipboardStorageEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Storage == nil {
					continue
				}
				changed := ClipboardStorageEvent{
					Ref: state.ClipboardStorageRef{
						AppID:   event.Storage.AppID,
						Scope:   string(event.Storage.Scope),
						OwnerID: event.Storage.OwnerID,
						Key:     event.Storage.Key,
						Version: event.Storage.Version,
					},
					Version: event.Storage.Version,
					Op:      event.Storage.Op,
				}
				select {
				case out <- changed:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func isStorageVersionConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "storage version conflict")
}

func isStorageNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "storage entry not found")
}
