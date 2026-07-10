package remoteauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-proto/remoteauthpb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	// AuthProtocol 是 DataChannel 切换到 termx protocol 前的独立协议标识。
	AuthProtocol = "termx-remote-auth"
	// AuthVersion 是当前 DeviceHello/CapabilityOpen canonical contract 的版本。
	AuthVersion uint32 = 1
	// MaxAuthFrameSize 限制单条授权 envelope，避免 bearer grant 或恶意 protobuf 无界占用内存。
	MaxAuthFrameSize = 64 * 1024
)

var authFrameMagic = [4]byte{'T', 'X', 'R', 'A'}

// HandshakeError 是端到端 remote auth 状态机的稳定错误。
// Code 可用于 endpoint 局部状态；Detail 只留在当前公开进程，发送给对端时会替换为固定脱敏消息。
type HandshakeError struct {
	Code   remoteauthpb.AuthErrorCode
	Detail string
	Cause  error
}

// Error 返回稳定错误码和本地诊断，不包含原始 grant body。
func (err *HandshakeError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Detail == "" {
		return err.Code.String()
	}
	return fmt.Sprintf("%s: %s", err.Code.String(), err.Detail)
}

// Unwrap 返回本地底层错误，供调用方用 errors.Is 区分 expiry、revoke 等 daemon-local truth。
func (err *HandshakeError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// HandshakeCodeOf 返回错误链中的稳定 remote auth code。
// 非状态机错误映射为 INTERNAL；调用方不得据此 fallback 到其他 endpoint 或 transport。
func HandshakeCodeOf(err error) remoteauthpb.AuthErrorCode {
	var handshakeErr *HandshakeError
	if errors.As(err, &handshakeErr) {
		return handshakeErr.Code
	}
	return remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_INTERNAL
}

// MarshalAuthEnvelope 使用 `TXRA` magic 和 deterministic protobuf 编码一条 pre-protocol 授权帧。
// 输入必须是当前版本、单一 payload 且无 unknown field；成功后的 bytes 可以作为一条完整 DataChannel message 发送。
func MarshalAuthEnvelope(envelope *remoteauthpb.AuthEnvelope) ([]byte, error) {
	if err := validateAuthEnvelope(envelope); err != nil {
		return nil, err
	}
	payload, err := deterministicMarshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal remote auth envelope: %w", err)
	}
	if len(payload) > MaxAuthFrameSize-len(authFrameMagic) {
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "remote auth frame exceeds limit", nil)
	}
	frame := make([]byte, len(authFrameMagic)+len(payload))
	copy(frame, authFrameMagic[:])
	copy(frame[len(authFrameMagic):], payload)
	return frame, nil
}

// UnmarshalAuthEnvelope 解码并严格校验一条 pre-protocol 授权帧。
// termx protocol frame、错误 magic、错误版本、unknown field 或多义 payload 都会返回 PROTOCOL，不会尝试旧格式。
func UnmarshalAuthEnvelope(frame []byte) (*remoteauthpb.AuthEnvelope, error) {
	if len(frame) <= len(authFrameMagic) || len(frame) > MaxAuthFrameSize || subtle.ConstantTimeCompare(frame[:min(len(frame), len(authFrameMagic))], authFrameMagic[:]) != 1 {
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "invalid remote auth frame", nil)
	}
	envelope := &remoteauthpb.AuthEnvelope{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(frame[len(authFrameMagic):], envelope); err != nil {
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "decode remote auth envelope", err)
	}
	if err := validateAuthEnvelope(envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}

// DeviceHelloSigningBytes 返回 DeviceIdentity Ed25519 签名使用的跨平台 canonical bytes。
// signature 字段不会进入输入；字段顺序由 remoteauthpb.DeviceHelloSignatureInput 的 deterministic protobuf 固定。
func DeviceHelloSigningBytes(authSessionID string, hello *remoteauthpb.DeviceHello) ([]byte, error) {
	if hello == nil {
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "device hello is nil", nil)
	}
	dtlsFingerprint, err := NormalizeDTLSCertificateFingerprint(hello.GetDaemonDtlsCertificateFingerprint())
	if err != nil {
		return nil, err
	}
	input := &remoteauthpb.DeviceHelloSignatureInput{
		Protocol:                         AuthProtocol,
		Version:                          AuthVersion,
		AuthSessionId:                    strings.TrimSpace(authSessionID),
		DeviceId:                         strings.TrimSpace(hello.GetDeviceId()),
		DevicePublicKey:                  append([]byte(nil), hello.GetDevicePublicKey()...),
		DeviceFingerprint:                strings.TrimSpace(hello.GetDeviceFingerprint()),
		ServerNonce:                      append([]byte(nil), hello.GetServerNonce()...),
		DaemonDtlsCertificateFingerprint: dtlsFingerprint,
		IssuedAtUnixNano:                 hello.GetIssuedAtUnixNano(),
	}
	if input.AuthSessionId == "" || input.DeviceId == "" || input.DeviceFingerprint == "" || len(input.ServerNonce) == 0 {
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "device hello canonical input is incomplete", nil)
	}
	return deterministicMarshal(input)
}

// CalculateCapabilityProof 计算 CapabilityOpen 使用的 HMAC-SHA-256 proof。
// HMAC key 与 grant hash 都来自同一去除首尾空白后的原始 grant；nonce 和 daemon DTLS fingerprint 绑定当前 channel。
func CalculateCapabilityProof(grant string, authSessionID string, serverNonce []byte, clientNonce []byte, daemonDTLSFingerprint string) ([]byte, error) {
	grant = strings.TrimSpace(grant)
	if grant == "" || strings.TrimSpace(authSessionID) == "" || len(serverNonce) == 0 || len(clientNonce) == 0 {
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "capability proof input is incomplete", nil)
	}
	dtlsFingerprint, err := NormalizeDTLSCertificateFingerprint(daemonDTLSFingerprint)
	if err != nil {
		return nil, err
	}
	grantHash := sha256.Sum256([]byte(grant))
	input := &remoteauthpb.CapabilityProofInput{
		Protocol:                         AuthProtocol,
		Version:                          AuthVersion,
		AuthSessionId:                    strings.TrimSpace(authSessionID),
		ServerNonce:                      append([]byte(nil), serverNonce...),
		ClientNonce:                      append([]byte(nil), clientNonce...),
		DaemonDtlsCertificateFingerprint: dtlsFingerprint,
		GrantSha256:                      grantHash[:],
	}
	canonical, err := deterministicMarshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal capability proof input: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(grant))
	_, _ = mac.Write(canonical)
	return mac.Sum(nil), nil
}

// NormalizeDTLSCertificateFingerprint 校验并规范化 daemon SHA-256 DTLS certificate fingerprint。
// 输入格式固定为 `sha-256:aa:bb:...`；不接受 SDP 字段替代实际 adapter 读取的 certificate bytes。
func NormalizeDTLSCertificateFingerprint(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	const prefix = "sha-256:"
	if !strings.HasPrefix(value, prefix) {
		return "", newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "daemon DTLS fingerprint must use sha-256", nil)
	}
	rawHex := strings.ReplaceAll(strings.TrimPrefix(value, prefix), ":", "")
	decoded, err := hex.DecodeString(rawHex)
	if err != nil || len(decoded) != sha256.Size {
		return "", newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "daemon DTLS fingerprint is malformed", err)
	}
	parts := make([]string, len(decoded))
	for index, octet := range decoded {
		parts[index] = fmt.Sprintf("%02x", octet)
	}
	return prefix + strings.Join(parts, ":"), nil
}

func validateAuthEnvelope(envelope *remoteauthpb.AuthEnvelope) error {
	if envelope == nil || envelope.GetProtocol() != AuthProtocol || envelope.GetVersion() != AuthVersion {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "unsupported remote auth envelope", nil)
	}
	sessionID := strings.TrimSpace(envelope.GetAuthSessionId())
	if len(sessionID) < 16 || len(sessionID) > 128 {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "invalid remote auth session id", nil)
	}
	for _, char := range sessionID {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "invalid remote auth session id", nil)
		}
	}
	if envelope.GetPayload() == nil || messageHasUnknown(envelope.ProtoReflect()) {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "ambiguous remote auth envelope", nil)
	}
	return nil
}

func deterministicMarshal(message proto.Message) ([]byte, error) {
	return (proto.MarshalOptions{Deterministic: true}).Marshal(message)
}

func messageHasUnknown(message protoreflect.Message) bool {
	if len(message.GetUnknown()) != 0 {
		return true
	}
	found := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				if messageHasUnknown(list.Get(index).Message()) {
					found = true
					return false
				}
			}
			return !found
		}
		found = messageHasUnknown(value.Message())
		return !found
	})
	return found
}

func newHandshakeError(code remoteauthpb.AuthErrorCode, detail string, cause error) *HandshakeError {
	if code == remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_UNSPECIFIED {
		code = remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_INTERNAL
	}
	return &HandshakeError{Code: code, Detail: detail, Cause: cause}
}

func rejectionMessage(code remoteauthpb.AuthErrorCode) string {
	switch code {
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH:
		return "device identity verification failed"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_EXPIRED:
		return "capability is expired or not active"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_REVOKED:
		return "capability has been revoked"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_PROOF_INVALID:
		return "capability proof verification failed"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_SCOPE_INVALID:
		return "capability scope is invalid"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_REPLAYED:
		return "authorization challenge is no longer valid"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_INVALID:
		return "capability verification failed"
	case remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL:
		return "remote authorization protocol error"
	default:
		return "remote authorization failed"
	}
}
