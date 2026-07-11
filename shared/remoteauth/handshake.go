package remoteauth

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/shared/transport"
)

const (
	authSessionBytes = 18
	authNonceBytes   = 32
	defaultClockSkew = 2 * time.Minute
)

// ClientHandshakeRequest 是 client 端到端验证 daemon 并打开 capability 的本地输入。
// ExpectedDeviceFingerprint 来自 endpoint pin，CapabilityGrant 来自安全凭据存储，DaemonDTLSCertificateFingerprint 来自实际 Pion peer certificate。
type ClientHandshakeRequest struct {
	ExpectedDeviceID                 string
	ExpectedDeviceFingerprint        string
	CapabilityGrant                  string
	DaemonDTLSCertificateFingerprint string
}

// ClientHandshake 是公开 client 在 termx protocol 前执行的 DeviceHello/CapabilityOpen 状态机。
// Random 和 Now 仅用于 deterministic harness；零值使用 crypto/rand 与 UTC 当前时间，不能注入云侧授权结果。
type ClientHandshake struct {
	Random io.Reader
	Now    func() time.Time
}

// Authenticate 完成设备 pin、DTLS binding、grant challenge 和 accepted scope 的双向验证。
// 成功后 transport 已切换到 termx protocol；失败时调用方必须关闭当前 DataChannel，不能覆盖 fingerprint 或 fallback。
func (handshake ClientHandshake) Authenticate(ctx context.Context, connection transport.Transport, request ClientHandshakeRequest) (Claims, error) {
	if connection == nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "remote auth transport is nil", nil)
	}
	now := handshake.now()
	claims, err := Verify(request.CapabilityGrant, request.ExpectedDeviceFingerprint, now, nil)
	if err != nil {
		return Claims{}, mapGrantError(err)
	}
	if strings.TrimSpace(claims.IssuerDeviceID) != strings.TrimSpace(request.ExpectedDeviceID) {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "capability issuer device does not match endpoint", nil)
	}
	dtlsFingerprint, err := NormalizeDTLSCertificateFingerprint(request.DaemonDTLSCertificateFingerprint)
	if err != nil {
		return Claims{}, err
	}

	helloEnvelope, err := receiveAuthEnvelope(ctx, connection)
	if err != nil {
		return Claims{}, err
	}
	hello := helloEnvelope.GetDeviceHello()
	if hello == nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "daemon did not start with DeviceHello", nil)
	}
	if err := verifyDeviceHello(helloEnvelope.GetAuthSessionId(), hello, request, dtlsFingerprint, now); err != nil {
		return Claims{}, err
	}
	clientNonce, err := randomBytes(handshake.random(), authNonceBytes)
	if err != nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_INTERNAL, "generate client nonce", err)
	}
	proof, err := CalculateCapabilityProof(request.CapabilityGrant, helloEnvelope.GetAuthSessionId(), hello.GetServerNonce(), clientNonce, dtlsFingerprint)
	if err != nil {
		return Claims{}, err
	}
	open := &remoteauthpb.AuthEnvelope{
		Protocol: AuthProtocol, Version: AuthVersion, AuthSessionId: helloEnvelope.GetAuthSessionId(),
		Payload: &remoteauthpb.AuthEnvelope_CapabilityOpen{CapabilityOpen: &remoteauthpb.CapabilityOpen{
			Grant: strings.TrimSpace(request.CapabilityGrant), ClientNonce: clientNonce, Proof: proof,
		}},
	}
	if err := sendAuthEnvelope(connection, open); err != nil {
		return Claims{}, err
	}
	result, err := receiveAuthEnvelope(ctx, connection)
	if err != nil {
		return Claims{}, err
	}
	if subtle.ConstantTimeCompare([]byte(result.GetAuthSessionId()), []byte(helloEnvelope.GetAuthSessionId())) != 1 {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_REPLAYED, "daemon changed auth session", nil)
	}
	if rejected := result.GetCapabilityRejected(); rejected != nil {
		code := rejected.GetCode()
		if code == remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_UNSPECIFIED {
			code = remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL
		}
		return Claims{}, newHandshakeError(code, rejected.GetMessage(), nil)
	}
	accepted := result.GetCapabilityAccepted()
	if accepted == nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "daemon did not complete capability authorization", nil)
	}
	if accepted.GetGrantId() != claims.GrantID || !scopeSummaryMatches(accepted.GetScope(), claims.Scope) {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_SCOPE_INVALID, "daemon accepted a different capability scope", nil)
	}
	return claims, nil
}

// ServerHandshake 是 daemon 在同一 DataChannel 上证明 DeviceIdentity 并验证 capability challenge 的状态机。
// Identity、revocation truth 和 scope mapping 全部属于 daemon；Random/Now 的注入只服务 deterministic harness。
type ServerHandshake struct {
	Identity    Identity
	Revocations RevocationChecker
	Random      io.Reader
	Now         func() time.Time
}

// Accept 发送绑定当前 daemon DTLS certificate 的 DeviceHello，验证一次性 CapabilityOpen，并发送协议切换点 CapabilityAccepted。
// 返回前不会调用 core-v2；失败会尽力发送脱敏 CapabilityRejected，调用方随后必须关闭当前 DataChannel。
func (handshake ServerHandshake) Accept(ctx context.Context, connection transport.Transport, daemonDTLSFingerprint string) (Claims, error) {
	if connection == nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "remote auth transport is nil", nil)
	}
	if err := handshake.Identity.Validate(); err != nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_INTERNAL, "daemon DeviceIdentity is invalid", err)
	}
	dtlsFingerprint, err := NormalizeDTLSCertificateFingerprint(daemonDTLSFingerprint)
	if err != nil {
		return Claims{}, err
	}
	authSessionRaw, err := randomBytes(handshake.random(), authSessionBytes)
	if err != nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_INTERNAL, "generate auth session id", err)
	}
	authSessionID := base64.RawURLEncoding.EncodeToString(authSessionRaw)
	serverNonce, err := randomBytes(handshake.random(), authNonceBytes)
	if err != nil {
		return Claims{}, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_INTERNAL, "generate server nonce", err)
	}
	now := handshake.now()
	hello := &remoteauthpb.DeviceHello{
		DeviceId: handshake.Identity.DeviceID, DevicePublicKey: append([]byte(nil), handshake.Identity.PublicKey...),
		DeviceFingerprint: handshake.Identity.Fingerprint, ServerNonce: serverNonce,
		DaemonDtlsCertificateFingerprint: dtlsFingerprint, IssuedAtUnixNano: now.UnixNano(),
	}
	signingBytes, err := DeviceHelloSigningBytes(authSessionID, hello)
	if err != nil {
		return Claims{}, err
	}
	hello.Signature = ed25519.Sign(handshake.Identity.PrivateKey, signingBytes)
	if err := sendAuthEnvelope(connection, &remoteauthpb.AuthEnvelope{
		Protocol: AuthProtocol, Version: AuthVersion, AuthSessionId: authSessionID,
		Payload: &remoteauthpb.AuthEnvelope_DeviceHello{DeviceHello: hello},
	}); err != nil {
		return Claims{}, err
	}

	openEnvelope, err := receiveAuthEnvelope(ctx, connection)
	if err != nil {
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	if subtle.ConstantTimeCompare([]byte(openEnvelope.GetAuthSessionId()), []byte(authSessionID)) != 1 {
		err := newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_REPLAYED, "capability open used a different auth session", nil)
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	open := openEnvelope.GetCapabilityOpen()
	if open == nil || len(open.GetClientNonce()) != authNonceBytes || len(open.GetProof()) != sha256Size {
		err := newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "client did not send a valid CapabilityOpen", nil)
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	claims, err := Verify(open.GetGrant(), handshake.Identity.Fingerprint, now, handshake.Revocations)
	if err != nil {
		mapped := mapGrantError(err)
		return Claims{}, handshake.reject(connection, authSessionID, mapped)
	}
	if claims.IssuerDeviceID != handshake.Identity.DeviceID || claims.IssuerDeviceFingerprint != handshake.Identity.Fingerprint {
		err := newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_INVALID, "capability issuer does not match daemon identity", nil)
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	expectedProof, err := CalculateCapabilityProof(open.GetGrant(), authSessionID, serverNonce, open.GetClientNonce(), dtlsFingerprint)
	if err != nil {
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	if !hmac.Equal(expectedProof, open.GetProof()) {
		err := newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_PROOF_INVALID, "capability proof does not match current challenge", nil)
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	summary, err := scopeSummary(claims.Scope)
	if err != nil {
		return Claims{}, handshake.reject(connection, authSessionID, err)
	}
	accepted := &remoteauthpb.AuthEnvelope{
		Protocol: AuthProtocol, Version: AuthVersion, AuthSessionId: authSessionID,
		Payload: &remoteauthpb.AuthEnvelope_CapabilityAccepted{CapabilityAccepted: &remoteauthpb.CapabilityAccepted{
			GrantId: claims.GrantID, Scope: summary,
		}},
	}
	if err := sendAuthEnvelope(connection, accepted); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

const sha256Size = 32

func verifyDeviceHello(authSessionID string, hello *remoteauthpb.DeviceHello, request ClientHandshakeRequest, dtlsFingerprint string, now time.Time) error {
	issuedAt := time.Unix(0, hello.GetIssuedAtUnixNano()).UTC()
	if hello.GetIssuedAtUnixNano() <= 0 || issuedAt.Before(now.Add(-defaultClockSkew)) || issuedAt.After(now.Add(defaultClockSkew)) {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "DeviceHello is outside the accepted time window", nil)
	}
	publicKey := ed25519.PublicKey(hello.GetDevicePublicKey())
	if len(publicKey) != ed25519.PublicKeySize || len(hello.GetServerNonce()) != authNonceBytes || len(hello.GetSignature()) != ed25519.SignatureSize {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "DeviceHello key, nonce or signature is malformed", nil)
	}
	calculatedFingerprint := Fingerprint(publicKey)
	if subtle.ConstantTimeCompare([]byte(calculatedFingerprint), []byte(hello.GetDeviceFingerprint())) != 1 ||
		subtle.ConstantTimeCompare([]byte(calculatedFingerprint), []byte(strings.TrimSpace(request.ExpectedDeviceFingerprint))) != 1 {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "DeviceHello fingerprint does not match endpoint pin", nil)
	}
	if strings.TrimSpace(hello.GetDeviceId()) != strings.TrimSpace(request.ExpectedDeviceID) {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "DeviceHello device id does not match endpoint", nil)
	}
	helloDTLSFingerprint, err := NormalizeDTLSCertificateFingerprint(hello.GetDaemonDtlsCertificateFingerprint())
	if err != nil || subtle.ConstantTimeCompare([]byte(helloDTLSFingerprint), []byte(dtlsFingerprint)) != 1 {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "DeviceHello is not bound to the current DTLS certificate", err)
	}
	signingBytes, err := DeviceHelloSigningBytes(authSessionID, hello)
	if err != nil || !ed25519.Verify(publicKey, signingBytes, hello.GetSignature()) {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "DeviceHello signature is invalid", err)
	}
	return nil
}

func (handshake ServerHandshake) reject(connection transport.Transport, authSessionID string, cause error) error {
	code := HandshakeCodeOf(cause)
	rejected := &remoteauthpb.AuthEnvelope{
		Protocol: AuthProtocol, Version: AuthVersion, AuthSessionId: authSessionID,
		Payload: &remoteauthpb.AuthEnvelope_CapabilityRejected{CapabilityRejected: &remoteauthpb.CapabilityRejected{
			Code: code, Message: rejectionMessage(code),
		}},
	}
	if err := sendAuthEnvelope(connection, rejected); err != nil {
		return newHandshakeError(code, "authorization failed and rejection could not be sent", errors.Join(cause, err))
	}
	return cause
}

func sendAuthEnvelope(connection transport.Transport, envelope *remoteauthpb.AuthEnvelope) error {
	frame, err := MarshalAuthEnvelope(envelope)
	if err != nil {
		return err
	}
	if err := connection.Send(frame); err != nil {
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "send remote auth frame", err)
	}
	return nil
}

func receiveAuthEnvelope(ctx context.Context, connection transport.Transport) (*remoteauthpb.AuthEnvelope, error) {
	type result struct {
		frame []byte
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		frame, err := connection.Recv()
		resultCh <- result{frame: frame, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = connection.Close()
		return nil, ctx.Err()
	case received := <-resultCh:
		if received.err != nil {
			return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_PROTOCOL, "receive remote auth frame", received.err)
		}
		return UnmarshalAuthEnvelope(received.frame)
	}
}

func scopeSummary(scope Scope) (*remoteauthpb.ScopeSummary, error) {
	switch {
	case scope.AllowDaemon && scope.TerminalID == "" && !scope.MachineEventsOnly:
		return &remoteauthpb.ScopeSummary{Kind: remoteauthpb.ScopeKind_SCOPE_KIND_DAEMON}, nil
	case !scope.AllowDaemon && scope.TerminalID != "" && !scope.MachineEventsOnly:
		return &remoteauthpb.ScopeSummary{Kind: remoteauthpb.ScopeKind_SCOPE_KIND_TERMINAL, TerminalId: scope.TerminalID}, nil
	case !scope.AllowDaemon && scope.TerminalID == "" && scope.MachineEventsOnly:
		return &remoteauthpb.ScopeSummary{Kind: remoteauthpb.ScopeKind_SCOPE_KIND_MACHINE_EVENTS}, nil
	default:
		return nil, newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_SCOPE_INVALID, "capability scope cannot map to core-v2", ErrGrantScopeInvalid)
	}
}

func scopeSummaryMatches(summary *remoteauthpb.ScopeSummary, scope Scope) bool {
	want, err := scopeSummary(scope)
	return err == nil && summary != nil && summary.GetKind() == want.GetKind() && summary.GetTerminalId() == want.GetTerminalId()
}

func mapGrantError(err error) *HandshakeError {
	switch {
	case errors.Is(err, ErrGrantExpired), errors.Is(err, ErrGrantNotActive):
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_EXPIRED, "capability time window is invalid", err)
	case errors.Is(err, ErrGrantRevoked):
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_REVOKED, "capability is revoked", err)
	case errors.Is(err, ErrGrantScopeInvalid):
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_SCOPE_INVALID, "capability scope is invalid", err)
	default:
		return newHandshakeError(remoteauthpb.AuthErrorCode_AUTH_ERROR_CODE_CAPABILITY_INVALID, "capability verification failed", err)
	}
}

func randomBytes(reader io.Reader, size int) ([]byte, error) {
	if reader == nil {
		reader = rand.Reader
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (handshake ClientHandshake) random() io.Reader {
	if handshake.Random != nil {
		return handshake.Random
	}
	return rand.Reader
}

func (handshake ClientHandshake) now() time.Time {
	if handshake.Now != nil {
		return handshake.Now().UTC()
	}
	return time.Now().UTC()
}

func (handshake ServerHandshake) random() io.Reader {
	if handshake.Random != nil {
		return handshake.Random
	}
	return rand.Reader
}

func (handshake ServerHandshake) now() time.Time {
	if handshake.Now != nil {
		return handshake.Now().UTC()
	}
	return time.Now().UTC()
}
