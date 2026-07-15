package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

// EdgePresenceChallenge 是 Hub 为已离线验证 daemon 创建的一次性 DeviceIdentity challenge。
// Value 可返回公开 daemon 签名；Hub 不接收或持有 DeviceIdentity private key。
type EdgePresenceChallenge struct {
	PresenceSessionID string
	ChallengeID       string
	Value             []byte
	ExpiresAt         time.Time
}

// EdgePresenceProof 是 daemon 对 Hub challenge 的公开签名证明。
// PublicKey 必须与 Hub 当前 signed policy projection 完全一致。
type EdgePresenceProof struct {
	PresenceSessionID string
	ChallengeID       string
	DeviceID          string
	PublicKey         []byte
	Signature         []byte
	SignedAt          time.Time
}

type edgePresenceChallengeState struct {
	accountID string
	deviceID  string
	publicKey []byte
	challenge EdgePresenceChallenge
}

// BeginEdgePresence 使用 daemon edge token 和本地 policy 创建 fresh Presence challenge。
// cache miss、stale policy、撤销或错误 principal 直接失败，绝不回查 Control Plane。
func (service *Service) BeginEdgePresence(ctx context.Context, edgeToken []byte, accountID, deviceID string) (EdgePresenceChallenge, error) {
	if service == nil || service.edgeAuthorizer == nil || ctx == nil || accountID == "" || deviceID == "" {
		return EdgePresenceChallenge{}, ErrAdmission
	}
	if err := ctx.Err(); err != nil {
		return EdgePresenceChallenge{}, err
	}
	_, device, err := service.edgeAuthorizer.AuthorizeDaemonDevice(edgeToken, accountID, deviceID)
	if err != nil || len(device.PublicKey) != ed25519.PublicKeySize {
		return EdgePresenceChallenge{}, ErrAdmission
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	if len(service.presenceChallenges) >= service.maxPresenceChallenges {
		return EdgePresenceChallenge{}, ErrCapacity
	}
	presenceSessionID, err := service.randomIDLocked("presence")
	if err != nil {
		return EdgePresenceChallenge{}, err
	}
	challengeID, err := service.randomIDLocked("challenge")
	if err != nil {
		return EdgePresenceChallenge{}, err
	}
	value := make([]byte, 32)
	if _, err := io.ReadFull(service.random, value); err != nil {
		clear(value)
		return EdgePresenceChallenge{}, fmt.Errorf("generate Hub presence challenge: %w", err)
	}
	challenge := EdgePresenceChallenge{PresenceSessionID: presenceSessionID, ChallengeID: challengeID, Value: value, ExpiresAt: now.Add(service.presenceChallengeTTL)}
	service.presenceChallenges[presenceSessionID] = edgePresenceChallengeState{accountID: accountID, deviceID: deviceID, publicKey: append([]byte(nil), device.PublicKey...), challenge: cloneEdgePresenceChallenge(challenge)}
	return cloneEdgePresenceChallenge(challenge), nil
}

// OpenEdgePresence 一次性消费 Hub challenge、验证 DeviceIdentity proof 并注册 Presence。
// edge token 和 proof 必须绑定同一 account/device；验证失败不会创建 Presence 或 signaling 状态。
func (service *Service) OpenEdgePresence(ctx context.Context, edgeToken []byte, accountID string, proof EdgePresenceProof) (*Presence, error) {
	if service == nil || service.edgeAuthorizer == nil || ctx == nil || accountID == "" || proof.PresenceSessionID == "" || proof.ChallengeID == "" || proof.DeviceID == "" || proof.SignedAt.IsZero() {
		return nil, ErrAdmission
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, device, err := service.edgeAuthorizer.AuthorizeDaemonDevice(edgeToken, accountID, proof.DeviceID)
	if err != nil {
		return nil, ErrAdmission
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	state, ok := service.presenceChallenges[proof.PresenceSessionID]
	if ok {
		delete(service.presenceChallenges, proof.PresenceSessionID)
	}
	service.mu.Unlock()
	if !ok || state.accountID != accountID || state.deviceID != proof.DeviceID || state.challenge.ChallengeID != proof.ChallengeID || !now.Before(state.challenge.ExpiresAt) {
		return nil, ErrAdmission
	}
	defer clear(state.challenge.Value)
	defer clear(state.publicKey)
	if len(proof.PublicKey) != ed25519.PublicKeySize || len(proof.Signature) != ed25519.SignatureSize || subtle.ConstantTimeCompare(proof.PublicKey, state.publicKey) != 1 || subtle.ConstantTimeCompare(device.PublicKey, state.publicKey) != 1 {
		return nil, ErrAdmission
	}
	signedAt := proof.SignedAt.UTC()
	if signedAt.Before(now.Add(-service.presenceChallengeTTL)) || signedAt.After(now.Add(30*time.Second)) {
		return nil, ErrAdmission
	}
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{
		PresenceSessionId: proof.PresenceSessionID, ChallengeId: proof.ChallengeID,
		Challenge: append([]byte(nil), state.challenge.Value...), DeviceId: proof.DeviceID,
		DevicePublicKey: append([]byte(nil), proof.PublicKey...), SignedAtUnixNano: signedAt.UnixNano(),
	})
	if err != nil || !ed25519.Verify(ed25519.PublicKey(state.publicKey), signingBytes, proof.Signature) {
		return nil, ErrAdmission
	}
	return service.registerPresence(ctx, accountID, proof.DeviceID, proof.PresenceSessionID, now.Add(service.maxPresenceTTL))
}

func (service *Service) registerPresence(ctx context.Context, accountID, deviceID, presenceSessionID string, expiresAt time.Time) (*Presence, error) {
	now := service.clock.Now().UTC()
	state := &presenceState{deviceID: deviceID, accountID: accountID, sessionID: presenceSessionID, expiresAt: expiresAt.UTC(), events: make(chan PresenceEvent, service.presenceQueue), done: make(chan struct{})}
	presence := &Presence{service: service, state: state}
	service.mu.Lock()
	service.cleanupLocked(now)
	if current := service.presences[deviceID]; current != nil && !current.closed {
		service.mu.Unlock()
		return nil, ErrPresenceConflict
	}
	if len(service.presences) >= service.maxPresences {
		service.mu.Unlock()
		return nil, ErrCapacity
	}
	service.presences[deviceID] = state
	service.mu.Unlock()
	go func() {
		timer := time.NewTimer(max(expiresAt.Sub(now), 0))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			_ = presence.Close()
		case <-timer.C:
			_ = presence.Close()
		case <-state.done:
		}
	}()
	return presence, nil
}

func (service *Service) randomIDLocked(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(service.random, buffer); err != nil {
		return "", fmt.Errorf("generate Hub %s id: %w", prefix, err)
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func cloneEdgePresenceChallenge(challenge EdgePresenceChallenge) EdgePresenceChallenge {
	challenge.Value = append([]byte(nil), challenge.Value...)
	return challenge
}
