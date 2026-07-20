package daemon

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"
)

// ManagedRuntime 是 daemon process 对 Cloud managed session registry 与当前控制 Presence 的 owner。
// runtime generation 在进程内固定；Presence 续约只能替换 control Presence，不能复制 session map。
type ManagedRuntime struct {
	mu                sync.RWMutex
	daemonDeviceID    string
	runtimeGeneration string
	registry          *ManagedSessionRegistry
}

// NewManagedRuntime 创建进程级 runtime generation；random 为空时使用 crypto/rand。
// 生成值只用于 correlation/fencing，不进入 credential、terminal payload 或持久配置。
func NewManagedRuntime(daemonDeviceID string, random io.Reader) (*ManagedRuntime, error) {
	if daemonDeviceID == "" {
		return nil, fmt.Errorf("create managed runtime: %w", ErrManagedSessionRegistryTarget)
	}
	if random == nil {
		random = rand.Reader
	}
	value := make([]byte, 18)
	if _, err := io.ReadFull(random, value); err != nil {
		return nil, fmt.Errorf("generate daemon runtime generation: %w", err)
	}
	return &ManagedRuntime{daemonDeviceID: daemonDeviceID, runtimeGeneration: "runtime-" + base64.RawURLEncoding.EncodeToString(value)}, nil
}

// RuntimeGeneration 返回当前 daemon 进程固定的 generation。
func (runtime *ManagedRuntime) RuntimeGeneration() string {
	if runtime == nil {
		return ""
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.runtimeGeneration
}

// BindPresence 绑定经过 Hub 验证的当前 control Presence。
// 同 Hub/assignment 的续约保留 active session；跨 assignment 只有空 inventory 时才能建立新 registry。
func (runtime *ManagedRuntime) BindPresence(hubID string, assignmentEpoch uint64, presenceSessionID string, observedAt time.Time) error {
	if runtime == nil || hubID == "" || assignmentEpoch == 0 || presenceSessionID == "" || observedAt.IsZero() {
		return fmt.Errorf("bind managed runtime presence: %w", ErrManagedSessionRegistryTarget)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.registry == nil {
		registry, err := NewManagedSessionRegistry(runtime.daemonDeviceID, runtime.runtimeGeneration, hubID, assignmentEpoch, presenceSessionID)
		if err != nil {
			return err
		}
		runtime.registry = registry
		return nil
	}
	if runtime.registry.controlOwnerHubID == hubID && runtime.registry.assignmentEpoch == assignmentEpoch {
		_, err := runtime.registry.ReplaceControlPresence("presence-bind", hubID, assignmentEpoch, presenceSessionID, observedAt)
		return err
	}
	inventory, err := runtime.registry.Inventory("assignment-rebind", observedAt)
	if err != nil || len(inventory.GetSessions()) != 0 {
		return fmt.Errorf("bind managed runtime across active assignment: %w", ErrManagedSessionRegistryTransition)
	}
	registry, err := NewManagedSessionRegistry(runtime.daemonDeviceID, runtime.runtimeGeneration, hubID, assignmentEpoch, presenceSessionID)
	if err != nil {
		return err
	}
	runtime.registry = registry
	return nil
}

// Registry 返回当前 Presence 已绑定的 registry；未完成 PresenceReady 时返回 nil。
func (runtime *ManagedRuntime) Registry() *ManagedSessionRegistry {
	if runtime == nil {
		return nil
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.registry
}
