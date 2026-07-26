// Package ticket 实现 Controller 短期票据和 DeviceIdentity stream proof 的唯一签名边界。
package ticket

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const (
	agentTicketDomain = "muxvia.cloud.agent-ticket.v1\x00"
	agentProofDomain  = "muxvia.cloud.agent-hello-proof.v1\x00"
)

// KeySet 是 Edge 当前从 EdgeWelcome 获得的只读 Controller 票据公钥集合。
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
	if !now.IsZero() {
		now = now.UTC()
		if claims.GetIssuedAt().AsTime().After(now.Add(skew)) || !claims.GetExpiresAt().AsTime().After(now.Add(-skew)) {
			return fmt.Errorf("AgentTicket is outside its validity window")
		}
	}
	return nil
}

func signingBytes(domain string, payload []byte) []byte {
	result := make([]byte, 0, len(domain)+len(payload))
	result = append(result, domain...)
	return append(result, payload...)
}
