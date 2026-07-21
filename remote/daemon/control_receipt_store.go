package daemon

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/filelock"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const (
	controlReceiptVersion   = 1
	controlReceiptFile      = "remote_cloud_control.json"
	controlReceiptLockFile  = "remote_cloud_control.lock"
	controlReceiptSignature = "muxvia.remote-daemon-control-state.v1\x00"
)

var (
	// ErrControlEnrollmentMissing 表示 daemon 尚未持久绑定 Cloud control key ring。
	ErrControlEnrollmentMissing = errors.New("daemon control enrollment is missing")
	// ErrControlReceiptConflict 表示相同 command ID 被不同签名命令重放。
	ErrControlReceiptConflict = errors.New("daemon control receipt conflicts with persisted command")
)

type controlReceiptRecord struct {
	CommandDigest []byte `json:"command_digest"`
	Result        []byte `json:"result"`
	ExpiresAt     int64  `json:"expires_at_unix_millis"`
}

type controlReceiptState struct {
	Version        uint32                          `json:"version"`
	Enrollment     []byte                          `json:"enrollment"`
	Receipts       map[string]controlReceiptRecord `json:"receipts"`
	StateSignature string                          `json:"state_signature"`
}

// ControlReceiptStore 持久拥有 daemon control enrollment 与未过期 command receipt。
// 它不拥有 Cloud session、Hub Presence、AccessStore grant 或 managed session lifecycle。
type ControlReceiptStore struct {
	mu       sync.Mutex
	path     string
	identity remoteauth.Identity
	owner    *filelock.Lock
	state    controlReceiptState
	closed   bool
}

// LoadControlReceiptStore 加载并验签 daemon control state，或创建空的 enrollment 容器。
func LoadControlReceiptStore(dir string, identity remoteauth.Identity) (*ControlReceiptStore, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("daemon control state directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	owner, err := filelock.Acquire(filepath.Join(dir, controlReceiptLockFile), true)
	if err != nil {
		return nil, err
	}
	store := &ControlReceiptStore{path: filepath.Join(dir, controlReceiptFile), identity: identity, owner: owner, state: controlReceiptState{Version: controlReceiptVersion, Receipts: make(map[string]controlReceiptRecord)}}
	payload, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store.state); err != nil {
		_ = owner.Close()
		return nil, fmt.Errorf("decode daemon control state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		_ = owner.Close()
		return nil, fmt.Errorf("decode daemon control state: trailing data")
	}
	if err := store.validateState(); err != nil {
		_ = owner.Close()
		return nil, err
	}
	if err := os.Chmod(store.path, 0o600); err != nil {
		_ = owner.Close()
		return nil, err
	}
	return store, nil
}

// Close 释放 control state 的唯一 mutation owner lock。
func (store *ControlReceiptStore) Close() error {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	if store.closed {
		store.mu.Unlock()
		return nil
	}
	store.closed = true
	owner := store.owner
	store.owner = nil
	store.mu.Unlock()
	return owner.Close()
}

// InstallEnrollment 原子替换 daemon control binding；auth epoch 回滚会被拒绝。
// 新 enrollment 会清除旧 command receipts，避免跨账号或 key ring 复用结果。
func (store *ControlReceiptStore) InstallEnrollment(enrollment *cloudpb.DaemonControlEnrollment) error {
	if store == nil || enrollment == nil || enrollment.GetDaemonDeviceId() != store.identity.DeviceID {
		return cloudpb.ErrInvalidDaemonControlCommand
	}
	if _, err := cloudpb.NewDaemonControlEnrollmentVerifier(enrollment); err != nil {
		return err
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(enrollment)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	if current, _ := store.enrollmentLocked(); current != nil && enrollment.GetAuthEpoch() < current.GetAuthEpoch() {
		return ErrControlReceiptConflict
	}
	previous := cloneControlReceiptState(store.state)
	store.state.Enrollment = payload
	store.state.Receipts = make(map[string]controlReceiptRecord)
	if err := store.persistLocked(); err != nil {
		store.state = previous
		return err
	}
	return nil
}

// Enrollment 返回当前持久 enrollment 深拷贝。
func (store *ControlReceiptStore) Enrollment() (*cloudpb.DaemonControlEnrollment, error) {
	if store == nil {
		return nil, ErrControlEnrollmentMissing
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return nil, err
	}
	return store.enrollmentLocked()
}

// VerifyOrReplay 验证 enrollment binding、签名、expiry 与 command digest。
// exact replay 返回首次持久 result；新 command 返回 nil result 和 canonical digest。
func (store *ControlReceiptStore) VerifyOrReplay(command *cloudpb.DaemonControlCommand, now time.Time) (*cloudpb.DaemonCommandResult, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if store == nil || command == nil || now.IsZero() {
		return nil, zero, cloudpb.ErrInvalidDaemonControlCommand
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(command)
	if err != nil {
		return nil, zero, err
	}
	digest := sha256.Sum256(payload)
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return nil, zero, err
	}
	enrollment, err := store.enrollmentLocked()
	if err != nil {
		return nil, zero, err
	}
	if command.GetAccountId() != enrollment.GetAccountId() || command.GetTargetDeviceId() != enrollment.GetDaemonDeviceId() || command.GetAuthEpoch() != enrollment.GetAuthEpoch() {
		return nil, zero, cloudpb.ErrInvalidDaemonControlCommand
	}
	verifier, err := cloudpb.NewDaemonControlEnrollmentVerifier(enrollment)
	if err != nil || verifier.Verify(command, now) != nil {
		return nil, zero, cloudpb.ErrInvalidDaemonControlSignature
	}
	changed := false
	previousReceipts := cloneControlReceipts(store.state.Receipts)
	for commandID, receipt := range store.state.Receipts {
		if receipt.ExpiresAt <= now.UnixMilli() {
			delete(store.state.Receipts, commandID)
			changed = true
		}
	}
	if changed {
		if err := store.persistLocked(); err != nil {
			store.state.Receipts = previousReceipts
			return nil, zero, err
		}
	}
	if receipt, ok := store.state.Receipts[command.GetCommandId()]; ok {
		if !bytes.Equal(receipt.CommandDigest, digest[:]) {
			return nil, zero, ErrControlReceiptConflict
		}
		result := &cloudpb.DaemonCommandResult{}
		if proto.Unmarshal(receipt.Result, result) != nil {
			return nil, zero, ErrControlReceiptConflict
		}
		return result, digest, nil
	}
	return nil, digest, nil
}

// CommitReceipt 在向 Companion 回报前持久保存 deterministic daemon result。
func (store *ControlReceiptStore) CommitReceipt(command *cloudpb.DaemonControlCommand, digest [sha256.Size]byte, result *cloudpb.DaemonCommandResult) error {
	if store == nil || command == nil || result == nil || command.GetCommandId() == "" || result.GetCommandId() != command.GetCommandId() || command.GetExpiresAtUnixMillis() <= 0 {
		return ErrControlReceiptConflict
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(result)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	if current, ok := store.state.Receipts[command.GetCommandId()]; ok {
		if !bytes.Equal(current.CommandDigest, digest[:]) || !bytes.Equal(current.Result, payload) {
			return ErrControlReceiptConflict
		}
		return nil
	}
	store.state.Receipts[command.GetCommandId()] = controlReceiptRecord{CommandDigest: append([]byte(nil), digest[:]...), Result: payload, ExpiresAt: command.GetExpiresAtUnixMillis()}
	if err := store.persistLocked(); err != nil {
		delete(store.state.Receipts, command.GetCommandId())
		return err
	}
	return nil
}

func (store *ControlReceiptStore) enrollmentLocked() (*cloudpb.DaemonControlEnrollment, error) {
	if len(store.state.Enrollment) == 0 {
		return nil, ErrControlEnrollmentMissing
	}
	enrollment := &cloudpb.DaemonControlEnrollment{}
	if proto.Unmarshal(store.state.Enrollment, enrollment) != nil {
		return nil, ErrControlReceiptConflict
	}
	return enrollment, nil
}

func (store *ControlReceiptStore) ensureOpenLocked() error {
	if store.closed || store.owner == nil {
		return fmt.Errorf("daemon control receipt store is closed")
	}
	return nil
}

func (store *ControlReceiptStore) validateState() error {
	if store.state.Version != controlReceiptVersion || store.state.Receipts == nil {
		return ErrControlReceiptConflict
	}
	signature, err := base64.RawURLEncoding.DecodeString(store.state.StateSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrControlReceiptConflict
	}
	canonical, err := controlReceiptSigningBytes(store.state)
	if err != nil || !ed25519.Verify(store.identity.PublicKey, canonical, signature) {
		return ErrControlReceiptConflict
	}
	if len(store.state.Enrollment) != 0 {
		enrollment, err := store.enrollmentLocked()
		if err != nil || enrollment.GetDaemonDeviceId() != store.identity.DeviceID {
			return ErrControlReceiptConflict
		}
		if _, err := cloudpb.NewDaemonControlEnrollmentVerifier(enrollment); err != nil {
			return err
		}
	}
	for commandID, receipt := range store.state.Receipts {
		result := &cloudpb.DaemonCommandResult{}
		if commandID == "" || len(receipt.CommandDigest) != sha256.Size || receipt.ExpiresAt <= 0 || proto.Unmarshal(receipt.Result, result) != nil || result.GetCommandId() != commandID {
			return ErrControlReceiptConflict
		}
	}
	return nil
}

func (store *ControlReceiptStore) persistLocked() error {
	store.state.Version = controlReceiptVersion
	canonical, err := controlReceiptSigningBytes(store.state)
	if err != nil {
		return err
	}
	store.state.StateSignature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(store.identity.PrivateKey, canonical))
	payload, err := json.MarshalIndent(store.state, "", "  ")
	if err != nil {
		return err
	}
	temporary := store.path + ".tmp"
	if err := os.WriteFile(temporary, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, store.path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func controlReceiptSigningBytes(state controlReceiptState) ([]byte, error) {
	state.StateSignature = ""
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return append([]byte(controlReceiptSignature), payload...), nil
}

func cloneControlReceiptState(state controlReceiptState) controlReceiptState {
	state.Enrollment = append([]byte(nil), state.Enrollment...)
	state.Receipts = cloneControlReceipts(state.Receipts)
	return state
}

func cloneControlReceipts(source map[string]controlReceiptRecord) map[string]controlReceiptRecord {
	result := make(map[string]controlReceiptRecord, len(source))
	for commandID, receipt := range source {
		receipt.CommandDigest = append([]byte(nil), receipt.CommandDigest...)
		receipt.Result = append([]byte(nil), receipt.Result...)
		result[commandID] = receipt
	}
	return result
}
