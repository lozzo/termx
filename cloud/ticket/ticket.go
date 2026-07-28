// Package ticket 实现 Controller 短期票据和 DeviceIdentity stream proof 的唯一签名边界。
package ticket

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const (
	agentTicketDomain          = "anytty.cloud.agent-ticket.v1\x00"
	agentProofDomain           = "anytty.cloud.agent-hello-proof.v1\x00"
	cloudRouteGrantDomain      = "anytty.cloud.route-grant.v1\x00"
	pairingRouteGrantDomain    = "anytty.cloud.pairing-route-grant.v1\x00"
	clientRouteProofDomain     = "anytty.cloud.client-route-proof.v1\x00"
	pairingRouteProofDomain    = "anytty.cloud.pairing-route-proof.v1\x00"
	cloudRouteHelloProofDomain = "anytty.cloud.route-hello-proof.v1\x00"
	relayLeaseDomain           = "anytty.cloud.relay-lease.v1\x00"
)

// KeySet 是 Edge 当前从 EdgeWelcome 获得的 AgentTicket/RelayLease 公钥集合。
type KeySet map[string]ed25519.PublicKey

// FromVerificationKeys 严格解析 Ed25519 公钥集合；重复 key_id 和未知算法都会失败。
func FromVerificationKeys(keys []*cloudv1.VerificationKey) (KeySet, error) {
	result := make(KeySet, len(keys))
	for _, key := range keys {
		id := strings.TrimSpace(key.GetKeyId())
		if id == "" || key.GetAlgorithm() != "Ed25519" || len(key.GetPublicKey()) != ed25519.PublicKeySize {
			return nil, errors.New("ticket verification key is invalid")
		}
		if _, exists := result[id]; exists {
			return nil, errors.New("ticket verification key ID is duplicated")
		}
		result[id] = append(ed25519.PublicKey(nil), key.GetPublicKey()...)
	}
	if len(result) == 0 {
		return nil, errors.New("ticket verification key set is empty")
	}
	return result, nil
}

// SignAgentTicket 对确定性 AgentTicketClaims 做 domain-separated 签名。
func SignAgentTicket(keyID string, privateKey ed25519.PrivateKey, claims *cloudv1.AgentTicketClaims) (*cloudv1.SignedEnvelope, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("AgentTicket signer is invalid")
	}
	if err := validateAgentClaims(claims, time.Time{}, 0); err != nil {
		return nil, err
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(claims)
	if err != nil {
		return nil, err
	}
	return &cloudv1.SignedEnvelope{KeyId: keyID, Payload: payload, Signature: ed25519.Sign(privateKey, signingBytes(agentTicketDomain, payload))}, nil
}

// VerifyAgentTicket 在 Edge 本地验签并校验 target、期限和 30 秒时钟偏差。
func VerifyAgentTicket(envelope *cloudv1.SignedEnvelope, keys KeySet, edgeID string, now time.Time, skew time.Duration) (*cloudv1.AgentTicketClaims, error) {
	if envelope == nil || len(envelope.GetSignature()) != ed25519.SignatureSize {
		return nil, errors.New("AgentTicket envelope is invalid")
	}
	publicKey := keys[strings.TrimSpace(envelope.GetKeyId())]
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, signingBytes(agentTicketDomain, envelope.GetPayload()), envelope.GetSignature()) {
		return nil, errors.New("AgentTicket signature is invalid")
	}
	claims := &cloudv1.AgentTicketClaims{}
	if err := proto.Unmarshal(envelope.GetPayload(), claims); err != nil {
		return nil, errors.New("AgentTicket payload is invalid")
	}
	if err := validateAgentClaims(claims, now, skew); err != nil {
		return nil, err
	}
	if strings.TrimSpace(claims.GetEdgeId()) != strings.TrimSpace(edgeID) {
		return nil, errors.New("AgentTicket targets another Edge")
	}
	return claims, nil
}

// SignAgentHelloProof 把当前 daemon stream generation 和 AgentTicket digest 绑定到 DeviceIdentity。
func SignAgentHelloProof(identity remoteauth.Identity, ticketEnvelope *cloudv1.SignedEnvelope, daemonID, bootID, connectionID string) ([]byte, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	canonical, err := agentHelloProofBytes(ticketEnvelope, daemonID, bootID, connectionID)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(identity.PrivateKey, canonical), nil
}

// VerifyAgentHelloProof 证明 AgentHello 持有 AgentTicket 内 DeviceIdentity 私钥。
func VerifyAgentHelloProof(publicKey []byte, proof []byte, ticketEnvelope *cloudv1.SignedEnvelope, daemonID, bootID, connectionID string) error {
	if len(publicKey) != ed25519.PublicKeySize || len(proof) != ed25519.SignatureSize {
		return errors.New("AgentHello proof key or signature is invalid")
	}
	canonical, err := agentHelloProofBytes(ticketEnvelope, daemonID, bootID, connectionID)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), canonical, proof) {
		return errors.New("AgentHello DeviceIdentity proof is invalid")
	}
	return nil
}

// SignCloudRouteGrant 使用 daemon DeviceIdentity 对只含发现信息的 grant 做 domain-separated 签名。
func SignCloudRouteGrant(identity remoteauth.Identity, claims *cloudv1.CloudRouteGrantClaims) (*cloudv1.SignedEnvelope, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	if err := validateCloudRouteGrant(claims, time.Time{}); err != nil {
		return nil, err
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(claims)
	if err != nil {
		return nil, err
	}
	return &cloudv1.SignedEnvelope{KeyId: identity.Fingerprint, Payload: payload, Signature: ed25519.Sign(identity.PrivateKey, signingBytes(cloudRouteGrantDomain, payload))}, nil
}

// VerifyCloudRouteGrant 使用 Controller 持久化的 daemon 公钥验签，不读取或保存在线拓扑。
func VerifyCloudRouteGrant(envelope *cloudv1.SignedEnvelope, daemonPublicKey ed25519.PublicKey, expectedDaemonID string, now time.Time) (*cloudv1.CloudRouteGrantClaims, error) {
	if envelope == nil || len(daemonPublicKey) != ed25519.PublicKeySize || len(envelope.GetSignature()) != ed25519.SignatureSize ||
		strings.TrimSpace(envelope.GetKeyId()) != remoteauth.Fingerprint(daemonPublicKey) {
		return nil, errors.New("CloudRouteGrant signature is invalid")
	}
	if !ed25519.Verify(daemonPublicKey, signingBytes(cloudRouteGrantDomain, envelope.GetPayload()), envelope.GetSignature()) {
		return nil, errors.New("CloudRouteGrant signature is invalid")
	}
	claims := &cloudv1.CloudRouteGrantClaims{}
	if err := proto.Unmarshal(envelope.GetPayload(), claims); err != nil {
		return nil, errors.New("CloudRouteGrant payload is invalid")
	}
	if err := validateCloudRouteGrant(claims, now); err != nil {
		return nil, err
	}
	if claims.GetDaemonId() != strings.TrimSpace(expectedDaemonID) {
		return nil, errors.New("CloudRouteGrant targets another daemon")
	}
	return claims, nil
}

// SignPairingRouteGrant 使用 daemon DeviceIdentity 签发不包含 claim 本体的 Cloud bootstrap grant。
func SignPairingRouteGrant(identity remoteauth.Identity, claims *cloudv1.PairingRouteGrantClaims) (*cloudv1.SignedEnvelope, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	if err := validatePairingRouteGrant(claims, time.Time{}); err != nil {
		return nil, err
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(claims)
	if err != nil {
		return nil, err
	}
	return &cloudv1.SignedEnvelope{KeyId: identity.Fingerprint, Payload: payload, Signature: ed25519.Sign(identity.PrivateKey, signingBytes(pairingRouteGrantDomain, payload))}, nil
}

// VerifyPairingRouteGrant 使用 Controller 持久化的 daemon identity 验证 bootstrap grant。
func VerifyPairingRouteGrant(envelope *cloudv1.SignedEnvelope, daemonPublicKey ed25519.PublicKey, expectedDaemonID string, now time.Time) (*cloudv1.PairingRouteGrantClaims, error) {
	if envelope == nil || len(daemonPublicKey) != ed25519.PublicKeySize || len(envelope.GetSignature()) != ed25519.SignatureSize ||
		strings.TrimSpace(envelope.GetKeyId()) != remoteauth.Fingerprint(daemonPublicKey) ||
		!ed25519.Verify(daemonPublicKey, signingBytes(pairingRouteGrantDomain, envelope.GetPayload()), envelope.GetSignature()) {
		return nil, errors.New("PairingRouteGrant signature is invalid")
	}
	claims := &cloudv1.PairingRouteGrantClaims{}
	if err := proto.Unmarshal(envelope.GetPayload(), claims); err != nil {
		return nil, errors.New("PairingRouteGrant payload is invalid")
	}
	if err := validatePairingRouteGrant(claims, now); err != nil {
		return nil, err
	}
	if claims.GetDaemonId() != strings.TrimSpace(expectedDaemonID) {
		return nil, errors.New("PairingRouteGrant targets another daemon")
	}
	return claims, nil
}

// ClientRouteProofBytes 返回 ClientAccessIdentity 对 Controller challenge 的 canonical 签名输入。
func ClientRouteProofBytes(challengeID string, challenge []byte, grant *cloudv1.SignedEnvelope, requestID string) ([]byte, error) {
	if strings.TrimSpace(challengeID) == "" || len(challenge) == 0 || grant == nil || strings.TrimSpace(requestID) == "" {
		return nil, errors.New("client route proof input is incomplete")
	}
	digest := sha256.Sum256(grant.GetPayload())
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&cloudv1.ClientRouteProofInput{
		ChallengeId: strings.TrimSpace(challengeID), Challenge: append([]byte(nil), challenge...), GrantPayloadSha256: digest[:], RequestId: strings.TrimSpace(requestID),
	})
	if err != nil {
		return nil, err
	}
	return signingBytes(clientRouteProofDomain, payload), nil
}

// VerifyClientRouteProof 证明发起解析的客户端持有 grant 内 ClientAccessIdentity 私钥。
func VerifyClientRouteProof(publicKey []byte, signature []byte, canonical []byte) error {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || len(canonical) == 0 || !ed25519.Verify(ed25519.PublicKey(publicKey), canonical, signature) {
		return errors.New("client route proof is invalid")
	}
	return nil
}

// PairingRouteProofBytes 把客户端 proof 绑定到短期 pairing offer 与本次 Controller challenge。
func PairingRouteProofBytes(challengeID string, challenge []byte, grant *cloudv1.SignedEnvelope, requestID string) ([]byte, error) {
	if strings.TrimSpace(challengeID) == "" || len(challenge) == 0 || grant == nil || strings.TrimSpace(requestID) == "" {
		return nil, errors.New("pairing route proof input is incomplete")
	}
	digest := sha256.Sum256(grant.GetPayload())
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&cloudv1.PairingRouteProofInput{
		ChallengeId: strings.TrimSpace(challengeID), Challenge: append([]byte(nil), challenge...), PairingRouteGrantPayloadSha256: digest[:], RequestId: strings.TrimSpace(requestID),
	})
	if err != nil {
		return nil, err
	}
	return signingBytes(pairingRouteProofDomain, payload), nil
}

// SignRelayLease 使用 Controller TicketSigner 签发绑定精确 managed session 的短租约。
// max bytes、rate、并发和期限必须已经由 Controller 的商业策略冻结。
func SignRelayLease(keyID string, privateKey ed25519.PrivateKey, claims *cloudv1.RelayLeaseClaims) (*cloudv1.SignedEnvelope, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("RelayLease signer is invalid")
	}
	if err := validateRelayLease(claims, time.Time{}, 0); err != nil {
		return nil, err
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(claims)
	if err != nil {
		return nil, err
	}
	return &cloudv1.SignedEnvelope{KeyId: keyID, Payload: payload, Signature: ed25519.Sign(privateKey, signingBytes(relayLeaseDomain, payload))}, nil
}

// VerifyRelayLease 由目标 Edge 离线验证签名、identity/session 绑定、上限和有效期。
func VerifyRelayLease(envelope *cloudv1.SignedEnvelope, keys KeySet, edgeID, sessionID string, now time.Time, skew time.Duration) (*cloudv1.RelayLeaseClaims, error) {
	if envelope == nil || len(envelope.GetSignature()) != ed25519.SignatureSize {
		return nil, errors.New("RelayLease envelope is invalid")
	}
	publicKey := keys[strings.TrimSpace(envelope.GetKeyId())]
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, signingBytes(relayLeaseDomain, envelope.GetPayload()), envelope.GetSignature()) {
		return nil, errors.New("RelayLease signature is invalid")
	}
	claims := &cloudv1.RelayLeaseClaims{}
	if err := proto.Unmarshal(envelope.GetPayload(), claims); err != nil {
		return nil, errors.New("RelayLease payload is invalid")
	}
	if err := validateRelayLease(claims, now, skew); err != nil {
		return nil, err
	}
	if claims.GetEdgeId() != strings.TrimSpace(edgeID) || claims.GetSessionId() != strings.TrimSpace(sessionID) {
		return nil, errors.New("RelayLease targets another Edge or session")
	}
	return claims, nil
}

// CloudRouteHelloProofBytes 把可复用 Route grant 绑定到目标 Edge 和当前单次连接。
func CloudRouteHelloProofBytes(routeGrant *cloudv1.SignedEnvelope, edgeID, sessionID string, attemptGeneration uint64) ([]byte, error) {
	if routeGrant == nil || strings.TrimSpace(edgeID) == "" || strings.TrimSpace(sessionID) == "" || attemptGeneration == 0 {
		return nil, errors.New("Cloud Route hello proof input is incomplete")
	}
	digest := sha256.Sum256(routeGrant.GetPayload())
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&cloudv1.CloudRouteHelloProofInput{
		RouteGrantPayloadSha256: digest[:], EdgeId: strings.TrimSpace(edgeID), SessionId: strings.TrimSpace(sessionID), AttemptGeneration: attemptGeneration,
	})
	if err != nil {
		return nil, err
	}
	return signingBytes(cloudRouteHelloProofDomain, payload), nil
}

// VerifyCloudRouteHelloProof 证明直连 Edge 的客户端持有 Route grant 绑定的 ClientAccessIdentity 私钥。
func VerifyCloudRouteHelloProof(publicKey, proof []byte, routeGrant *cloudv1.SignedEnvelope, edgeID, sessionID string, attemptGeneration uint64) error {
	canonical, err := CloudRouteHelloProofBytes(routeGrant, edgeID, sessionID, attemptGeneration)
	if err != nil {
		return err
	}
	if len(publicKey) != ed25519.PublicKeySize || len(proof) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(publicKey), canonical, proof) {
		return errors.New("Cloud Route hello proof is invalid")
	}
	return nil
}

func agentHelloProofBytes(envelope *cloudv1.SignedEnvelope, daemonID, bootID, connectionID string) ([]byte, error) {
	if envelope == nil || strings.TrimSpace(daemonID) == "" || strings.TrimSpace(bootID) == "" || strings.TrimSpace(connectionID) == "" {
		return nil, errors.New("AgentHello proof input is incomplete")
	}
	digest := sha256.Sum256(envelope.GetPayload())
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&cloudv1.AgentHelloProofInput{
		TicketPayloadSha256: digest[:], DaemonId: strings.TrimSpace(daemonID), BootId: strings.TrimSpace(bootID), ConnectionId: strings.TrimSpace(connectionID),
	})
	if err != nil {
		return nil, err
	}
	return signingBytes(agentProofDomain, payload), nil
}

func validateAgentClaims(claims *cloudv1.AgentTicketClaims, now time.Time, skew time.Duration) error {
	if claims == nil || strings.TrimSpace(claims.GetTicketId()) == "" || strings.TrimSpace(claims.GetDaemonId()) == "" ||
		strings.TrimSpace(claims.GetAccountId()) == "" || strings.TrimSpace(claims.GetEdgeId()) == "" || strings.TrimSpace(claims.GetDeviceId()) == "" ||
		len(claims.GetDevicePublicKey()) != ed25519.PublicKeySize || claims.GetIssuedAt() == nil || claims.GetExpiresAt() == nil ||
		claims.GetIssuedAt().CheckValid() != nil || claims.GetExpiresAt().CheckValid() != nil || !claims.GetExpiresAt().AsTime().After(claims.GetIssuedAt().AsTime()) {
		return errors.New("AgentTicket claims are incomplete")
	}
	if len(claims.GetCapabilities()) == 0 {
		return errors.New("AgentTicket has no capability")
	}
	if relay := claims.GetRelayDelegation(); relay != nil && (relay.GetMaxBytesPerLease() == 0 || relay.GetMaxRateBytesPerSecond() == 0 || relay.GetMaxConcurrentAllocations() == 0) {
		return errors.New("AgentTicket Relay delegation is incomplete")
	}
	if !now.IsZero() {
		now = now.UTC()
		if claims.GetIssuedAt().AsTime().After(now.Add(skew)) || !claims.GetExpiresAt().AsTime().After(now.Add(-skew)) {
			return fmt.Errorf("AgentTicket is outside its validity window")
		}
	}
	return nil
}

func validateCloudRouteGrant(claims *cloudv1.CloudRouteGrantClaims, now time.Time) error {
	if claims == nil || strings.TrimSpace(claims.GetGrantId()) == "" || strings.TrimSpace(claims.GetDaemonId()) == "" || len(claims.GetClientPublicKey()) != ed25519.PublicKeySize ||
		claims.GetProduct() < cloudv1.ClientProduct_CLIENT_PRODUCT_TUI || claims.GetProduct() > cloudv1.ClientProduct_CLIENT_PRODUCT_DESKTOP_GUI || claims.GetIssuedAt() == nil || claims.GetExpiresAt() == nil || claims.GetIssuedAt().CheckValid() != nil || claims.GetExpiresAt().CheckValid() != nil ||
		!claims.GetExpiresAt().AsTime().After(claims.GetIssuedAt().AsTime()) || claims.GetExpiresAt().AsTime().Sub(claims.GetIssuedAt().AsTime()) > 365*24*time.Hour {
		return errors.New("CloudRouteGrant claims are incomplete")
	}
	if !now.IsZero() && (claims.GetIssuedAt().AsTime().After(now.UTC().Add(30*time.Second)) || !claims.GetExpiresAt().AsTime().After(now.UTC().Add(-30*time.Second))) {
		return errors.New("CloudRouteGrant is outside its validity window")
	}
	return nil
}

func validatePairingRouteGrant(claims *cloudv1.PairingRouteGrantClaims, now time.Time) error {
	if claims == nil || strings.TrimSpace(claims.GetGrantId()) == "" || strings.TrimSpace(claims.GetDaemonId()) == "" || strings.TrimSpace(claims.GetDeviceId()) == "" || len(claims.GetPairingClaimSha256()) != sha256.Size ||
		claims.GetIssuedAt() == nil || claims.GetExpiresAt() == nil || claims.GetIssuedAt().CheckValid() != nil || claims.GetExpiresAt().CheckValid() != nil || !claims.GetExpiresAt().AsTime().After(claims.GetIssuedAt().AsTime()) || claims.GetExpiresAt().AsTime().Sub(claims.GetIssuedAt().AsTime()) > 24*time.Hour {
		return errors.New("PairingRouteGrant claims are incomplete")
	}
	if !now.IsZero() && (claims.GetIssuedAt().AsTime().After(now.UTC().Add(30*time.Second)) || !claims.GetExpiresAt().AsTime().After(now.UTC().Add(-30*time.Second))) {
		return errors.New("PairingRouteGrant is outside its validity window")
	}
	return nil
}

func validateRelayLease(claims *cloudv1.RelayLeaseClaims, now time.Time, skew time.Duration) error {
	if claims == nil || strings.TrimSpace(claims.GetLeaseId()) == "" || strings.TrimSpace(claims.GetAccountId()) == "" || strings.TrimSpace(claims.GetEdgeId()) == "" ||
		strings.TrimSpace(claims.GetDaemonId()) == "" || strings.TrimSpace(claims.GetClientId()) == "" || strings.TrimSpace(claims.GetSessionId()) == "" ||
		claims.GetMaxBytes() == 0 || claims.GetMaxRateBytesPerSecond() == 0 || claims.GetMaxConcurrentAllocations() == 0 || claims.GetIssuedAt() == nil || claims.GetExpiresAt() == nil ||
		claims.GetIssuedAt().CheckValid() != nil || claims.GetExpiresAt().CheckValid() != nil || !claims.GetExpiresAt().AsTime().After(claims.GetIssuedAt().AsTime()) ||
		claims.GetExpiresAt().AsTime().Sub(claims.GetIssuedAt().AsTime()) > 5*time.Minute {
		return errors.New("RelayLease claims are incomplete")
	}
	if !now.IsZero() && (claims.GetIssuedAt().AsTime().After(now.UTC().Add(skew)) || !claims.GetExpiresAt().AsTime().After(now.UTC().Add(-skew))) {
		return errors.New("RelayLease is outside its validity window")
	}
	return nil
}

func signingBytes(domain string, payload []byte) []byte {
	result := make([]byte, 0, len(domain)+len(payload))
	result = append(result, domain...)
	return append(result, payload...)
}
