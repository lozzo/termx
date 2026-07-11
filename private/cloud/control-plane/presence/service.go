// Package presence 拥有 daemon device-scoped presence challenge 与 Hub admission 事务。
//
// PresenceSession 只表达设备在线注册，不是 client ManagedSession、termx ProtocolSession 或 terminal capability。
package presence

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

var (
	// ErrChallengeNotFound 表示 challenge 不存在、已过期或已被一次性消费。
	ErrChallengeNotFound = errors.New("control plane presence challenge not found")
	// ErrProofRejected 表示 proof 与注册设备、challenge 或签名时间不匹配。
	ErrProofRejected = errors.New("control plane presence proof rejected")
	// ErrCapacity 表示尚未过期的一次性 challenge 已达到显式容量上限。
	ErrCapacity = errors.New("control plane presence challenge capacity exhausted")
)

// DeviceSource 是 presence service 读取 daemon 注册公钥与账号 ownership 的唯一真值边界。
// 实现不得从 proof 自带公钥自动注册设备，也不得读取 terminal 或 capability 数据。
type DeviceSource interface {
	Device(accountID, deviceID string) (domain.DeviceRegistration, error)
}

// Config 固定 fresh challenge、Hub 目标、签名 issuer、有界生命周期和在途 challenge 硬上限。
// Now 与 Random 只用于确定性 harness；缺失时使用 UTC clock 与 crypto/rand.Reader。
type Config struct {
	Devices       DeviceSource
	Issuer        servicecredential.HubAdmissionIssuer
	HubID         string
	ChallengeTTL  time.Duration
	AdmissionTTL  time.Duration
	MaxChallenges int
	Now           func() time.Time
	Random        io.Reader
}

// Challenge 是可以返回公开 daemon 签名的非秘密一次性 presence challenge。
// PresenceSessionID 与 ChallengeID 都由 Control Plane 生成，不能由 caller 自选。
type Challenge struct {
	PresenceSessionID string
	ChallengeID       string
	Value             []byte
	ExpiresAt         time.Time
}

// Proof 是公开 daemon 对 Challenge 的 DeviceIdentity 签名结果。
// Private key 不进入该对象；PublicKey 只用于与已注册目录真值做常量时间匹配。
type Proof struct {
	PresenceSessionID string
	ChallengeID       string
	DeviceID          string
	PublicKey         []byte
	Signature         []byte
	SignedAt          time.Time
}

// Admission 是通过 fresh proof 后签发的 device-scoped Hub presence 凭据。
// Ticket 是短期 secret；PresenceSessionID 可以用于 Companion/Hub stream correlation。
type Admission struct {
	PresenceSessionID string
	Ticket            servicecredential.HubAdmissionTicket
	ExpiresAt         time.Time
}

type challengeState struct {
	accountID string
	device    domain.DeviceRegistration
	challenge Challenge
}

// Service 是 Control Plane 的 fresh presence proof owner。
// 消息链路固定为 registered daemon -> one-time challenge -> DeviceIdentity proof -> presence-only admission。
type Service struct {
	devices       DeviceSource
	issuer        servicecredential.HubAdmissionIssuer
	hubID         string
	challengeTTL  time.Duration
	admissionTTL  time.Duration
	maxChallenges int
	now           func() time.Time
	random        io.Reader

	mu         sync.Mutex
	challenges map[string]challengeState
}

// NewService 创建有界 presence challenge service。
// 缺少目录、Hub、issuer 或非法 TTL 时直接失败，不允许退化为 enrollment challenge 或长期 daemon token。
func NewService(config Config) (*Service, error) {
	if config.Devices == nil || config.HubID == "" || config.ChallengeTTL <= 0 || config.ChallengeTTL > 2*time.Minute || config.AdmissionTTL <= 0 || config.AdmissionTTL > 5*time.Minute || config.MaxChallenges < 1 {
		return nil, fmt.Errorf("invalid presence service configuration")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &Service{
		devices: config.Devices, issuer: config.Issuer, hubID: config.HubID,
		challengeTTL: config.ChallengeTTL, admissionTTL: config.AdmissionTTL,
		maxChallenges: config.MaxChallenges, now: config.Now, random: config.Random, challenges: make(map[string]challengeState),
	}, nil
}

// Begin 为已注册且未撤销的 daemon DeviceID 创建一个新的 PresenceSession challenge。
// 同一设备可以在旧 presence 到期后再次 Begin；每个 challenge 仍只能成功消费一次。
func (service *Service) Begin(ctx context.Context, accountID, deviceID string) (Challenge, error) {
	if service == nil || ctx == nil || accountID == "" || deviceID == "" {
		return Challenge{}, ErrProofRejected
	}
	if err := ctx.Err(); err != nil {
		return Challenge{}, err
	}
	device, err := service.devices.Device(accountID, deviceID)
	if err != nil || device.Kind != domain.DeviceKindDaemon || device.RevokedAt != nil || len(device.PublicKey) != ed25519.PublicKeySize {
		return Challenge{}, ErrProofRejected
	}
	presenceSessionID, err := service.randomID("presence")
	if err != nil {
		return Challenge{}, err
	}
	challengeID, err := service.randomID("challenge")
	if err != nil {
		return Challenge{}, err
	}
	value := make([]byte, 32)
	if _, err := io.ReadFull(service.random, value); err != nil {
		return Challenge{}, fmt.Errorf("generate presence challenge: %w", err)
	}
	now := service.now().UTC()
	challenge := Challenge{PresenceSessionID: presenceSessionID, ChallengeID: challengeID, Value: value, ExpiresAt: now.Add(service.challengeTTL)}
	service.mu.Lock()
	service.cleanupLocked(now)
	if len(service.challenges) >= service.maxChallenges {
		service.mu.Unlock()
		clear(value)
		return Challenge{}, ErrCapacity
	}
	service.challenges[presenceSessionID] = challengeState{accountID: accountID, device: device, challenge: cloneChallenge(challenge)}
	service.mu.Unlock()
	return cloneChallenge(challenge), nil
}

// Issue 一次性消费 challenge、验证目录公钥与 deterministic signature，并签发 presence-only Hub admission。
// 任一失败都不会创建 ManagedSession、Hub presence 或 terminal authorization。
func (service *Service) Issue(ctx context.Context, accountID string, proof Proof) (Admission, error) {
	if service == nil || ctx == nil || accountID == "" || proof.PresenceSessionID == "" || proof.ChallengeID == "" || proof.DeviceID == "" || proof.SignedAt.IsZero() {
		return Admission{}, ErrProofRejected
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, err
	}
	now := service.now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	state, ok := service.challenges[proof.PresenceSessionID]
	if ok {
		delete(service.challenges, proof.PresenceSessionID)
	}
	service.mu.Unlock()
	if !ok || state.accountID != accountID || state.challenge.ChallengeID != proof.ChallengeID || state.device.ID != proof.DeviceID || !now.Before(state.challenge.ExpiresAt) {
		return Admission{}, ErrChallengeNotFound
	}
	if len(proof.PublicKey) != ed25519.PublicKeySize || subtle.ConstantTimeCompare(proof.PublicKey, state.device.PublicKey) != 1 || len(proof.Signature) != ed25519.SignatureSize {
		return Admission{}, ErrProofRejected
	}
	signedAt := proof.SignedAt.UTC()
	if signedAt.Before(now.Add(-service.challengeTTL)) || signedAt.After(now.Add(30*time.Second)) {
		return Admission{}, ErrProofRejected
	}
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{
		PresenceSessionId: proof.PresenceSessionID, ChallengeId: proof.ChallengeID,
		Challenge: append([]byte(nil), state.challenge.Value...), DeviceId: proof.DeviceID,
		DevicePublicKey: append([]byte(nil), proof.PublicKey...), SignedAtUnixNano: signedAt.UnixNano(),
	})
	if err != nil || !ed25519.Verify(ed25519.PublicKey(state.device.PublicKey), signingBytes, proof.Signature) {
		return Admission{}, ErrProofRejected
	}
	ticketID, err := service.randomID("ticket")
	if err != nil {
		return Admission{}, err
	}
	ticket, err := service.issuer.Issue(servicecredential.HubAdmissionRequest{
		TicketID: ticketID, AudienceHubID: service.hubID, PrincipalKind: servicecredential.PrincipalDaemon,
		AccountID: accountID, DeviceID: proof.DeviceID, SessionKind: servicecredential.HubSessionPresence,
		SessionID: proof.PresenceSessionID, AllowedOperations: []servicecredential.HubOperation{servicecredential.HubOperationPresence},
		TTL: service.admissionTTL,
	}, now)
	if err != nil {
		return Admission{}, err
	}
	return Admission{PresenceSessionID: proof.PresenceSessionID, Ticket: ticket, ExpiresAt: now.Add(service.admissionTTL)}, nil
}

func (service *Service) cleanupLocked(now time.Time) {
	for id, state := range service.challenges {
		if !now.Before(state.challenge.ExpiresAt) {
			delete(service.challenges, id)
		}
	}
}

func (service *Service) randomID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(service.random, buffer); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func cloneChallenge(challenge Challenge) Challenge {
	challenge.Value = append([]byte(nil), challenge.Value...)
	return challenge
}
