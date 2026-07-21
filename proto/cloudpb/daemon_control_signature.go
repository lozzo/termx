package cloudpb

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
)

const daemonControlSignatureDomain = "MUXVIA_DAEMON_CONTROL_V1\x00"

var (
	// ErrInvalidDaemonControlCommand 表示命令缺少 deny-only 类型、精确目标或时效字段。
	ErrInvalidDaemonControlCommand = errors.New("invalid daemon control command")
	// ErrUnknownDaemonControlKey 表示 daemon enrollment 没有信任命令引用的 control key。
	ErrUnknownDaemonControlKey = errors.New("unknown daemon control key")
	// ErrInvalidDaemonControlSignature 表示命令内容与 Controller 签名不匹配。
	ErrInvalidDaemonControlSignature = errors.New("invalid daemon control signature")
	// ErrExpiredDaemonControlCommand 表示命令尚未生效、已经过期或时间窗口非法。
	ErrExpiredDaemonControlCommand = errors.New("daemon control command expired")
)

// DaemonControlSigningBytes 返回不含 signature 字段的确定性 Proto 签名输入。
// 该辅助契约与 generated schema 同包，Controller 与 daemon 不需要跨越彼此领域边界。
func DaemonControlSigningBytes(command *DaemonControlCommand) ([]byte, error) {
	if err := validateDaemonControlCommand(command); err != nil {
		return nil, err
	}
	unsigned := proto.Clone(command).(*DaemonControlCommand)
	unsigned.Signature = nil
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(unsigned)
	if err != nil {
		return nil, fmt.Errorf("marshal daemon control command: %w", err)
	}
	result := make([]byte, 0, len(daemonControlSignatureDomain)+len(payload))
	result = append(result, daemonControlSignatureDomain...)
	result = append(result, payload...)
	return result, nil
}

// SignDaemonControlCommand 使用专用 Ed25519 key 签发 generated command 的深拷贝。
func SignDaemonControlCommand(command *DaemonControlCommand, keyID string, privateKey ed25519.PrivateKey) (*DaemonControlCommand, error) {
	if len(privateKey) != ed25519.PrivateKeySize || keyID == "" || command == nil {
		return nil, ErrInvalidDaemonControlCommand
	}
	owned := proto.Clone(command).(*DaemonControlCommand)
	owned.ControlKeyId = keyID
	owned.Signature = nil
	canonical, err := DaemonControlSigningBytes(owned)
	if err != nil {
		return nil, err
	}
	owned.Signature = ed25519.Sign(privateKey, canonical)
	return owned, nil
}

type daemonControlVerificationMaterial struct {
	publicKey ed25519.PublicKey
	notBefore time.Time
	notAfter  time.Time
}

// DaemonControlVerifier 是 daemon enrollment 装配的 control public key 集。
// 它只拥有验签材料，不拥有账号、assignment、Presence 或 session 真值。
type DaemonControlVerifier struct {
	keys map[string]daemonControlVerificationMaterial
}

// NewDaemonControlVerifier 复制可信 control public key；空集合或非法 key 会 fail closed。
func NewDaemonControlVerifier(keys map[string]ed25519.PublicKey) (*DaemonControlVerifier, error) {
	if len(keys) == 0 {
		return nil, ErrUnknownDaemonControlKey
	}
	owned := make(map[string]daemonControlVerificationMaterial, len(keys))
	for keyID, publicKey := range keys {
		if keyID == "" || len(publicKey) != ed25519.PublicKeySize {
			return nil, ErrUnknownDaemonControlKey
		}
		owned[keyID] = daemonControlVerificationMaterial{publicKey: append(ed25519.PublicKey(nil), publicKey...)}
	}
	return &DaemonControlVerifier{keys: owned}, nil
}

// NewDaemonControlEnrollmentVerifier 从 Proto enrollment 构造带 key window 的 verifier。
// enrollment 的账号、设备和 auth epoch 仍由 daemon ControlReceiptStore 作为本地 binding 校验。
func NewDaemonControlEnrollmentVerifier(enrollment *DaemonControlEnrollment) (*DaemonControlVerifier, error) {
	if enrollment == nil || enrollment.GetAccountId() == "" || enrollment.GetDaemonDeviceId() == "" || enrollment.GetAuthEpoch() == 0 || enrollment.GetEnrolledAtUnixMillis() <= 0 || len(enrollment.GetVerificationKeys()) == 0 {
		return nil, ErrInvalidDaemonControlCommand
	}
	owned := make(map[string]daemonControlVerificationMaterial, len(enrollment.GetVerificationKeys()))
	for _, key := range enrollment.GetVerificationKeys() {
		notBefore := time.UnixMilli(key.GetNotBeforeUnixMillis()).UTC()
		notAfter := time.UnixMilli(key.GetNotAfterUnixMillis()).UTC()
		if key == nil || key.GetKeyId() == "" || len(key.GetPublicKey()) != ed25519.PublicKeySize || key.GetNotBeforeUnixMillis() <= 0 || !notAfter.After(notBefore) {
			return nil, ErrUnknownDaemonControlKey
		}
		if _, duplicate := owned[key.GetKeyId()]; duplicate {
			return nil, ErrUnknownDaemonControlKey
		}
		owned[key.GetKeyId()] = daemonControlVerificationMaterial{publicKey: append(ed25519.PublicKey(nil), key.GetPublicKey()...), notBefore: notBefore, notAfter: notAfter}
	}
	return &DaemonControlVerifier{keys: owned}, nil
}

// Verify 验证命令结构、有效期和 Controller 签名。
// daemon runtime 仍必须另外校验 Hub、assignment、Presence、generation 和精确目标。
func (verifier *DaemonControlVerifier) Verify(command *DaemonControlCommand, now time.Time) error {
	if verifier == nil || command == nil || now.IsZero() {
		return ErrInvalidDaemonControlCommand
	}
	if command.GetIssuedAtUnixMillis() > now.UnixMilli() || command.GetExpiresAtUnixMillis() <= now.UnixMilli() {
		return ErrExpiredDaemonControlCommand
	}
	key, ok := verifier.keys[command.GetControlKeyId()]
	if !ok || len(key.publicKey) != ed25519.PublicKeySize {
		return ErrUnknownDaemonControlKey
	}
	issuedAt := time.UnixMilli(command.GetIssuedAtUnixMillis()).UTC()
	if !key.notBefore.IsZero() && (issuedAt.Before(key.notBefore) || !issuedAt.Before(key.notAfter)) {
		return ErrExpiredDaemonControlCommand
	}
	canonical, err := DaemonControlSigningBytes(command)
	if err != nil {
		return err
	}
	if len(command.GetSignature()) != ed25519.SignatureSize || !ed25519.Verify(key.publicKey, canonical, command.GetSignature()) {
		return ErrInvalidDaemonControlSignature
	}
	return nil
}

func validateDaemonControlCommand(command *DaemonControlCommand) error {
	if command == nil || command.GetCommandId() == "" || command.GetAccountId() == "" || command.GetTargetDeviceId() == "" || command.GetHubId() == "" || command.GetAssignmentEpoch() == 0 || command.GetAuthEpoch() == 0 || command.GetPresenceSessionId() == "" || command.GetDaemonRuntimeGeneration() == "" || command.GetIssuedAtUnixMillis() <= 0 || command.GetExpiresAtUnixMillis() <= command.GetIssuedAtUnixMillis() || command.GetControlKeyId() == "" {
		return ErrInvalidDaemonControlCommand
	}
	switch command.GetCommandKind() {
	case DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION:
		target := command.GetManagedPeerSession()
		if target == nil || target.GetDaemonDeviceId() != command.GetTargetDeviceId() || target.GetManagedSessionId() == "" || target.GetSessionIncarnation() == 0 || target.GetAssignmentEpoch() != command.GetAssignmentEpoch() || target.GetControlPresenceSessionId() != command.GetPresenceSessionId() || target.GetDaemonRuntimeGeneration() != command.GetDaemonRuntimeGeneration() {
			return ErrInvalidDaemonControlCommand
		}
	case DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_REVOKE_TERMINAL_ACCESS:
		target := command.GetTerminalAccess()
		if target == nil || target.GetDaemonDeviceId() != command.GetTargetDeviceId() || target.GetOpaqueAccessReference() == "" || target.GetAssignmentEpoch() != command.GetAssignmentEpoch() || target.GetPresenceSessionId() != command.GetPresenceSessionId() || target.GetDaemonRuntimeGeneration() != command.GetDaemonRuntimeGeneration() || target.GetAccessProjectionRevision() == 0 {
			return ErrInvalidDaemonControlCommand
		}
	default:
		return ErrInvalidDaemonControlCommand
	}
	return nil
}
