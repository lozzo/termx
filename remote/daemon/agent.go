package daemon

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
)

// ErrPresenceClosed 表示 Cloud Companion 已结束当前 daemon presence。
// 它只结束当前 managed cloud presence，不修改 core-v2 terminal lifecycle、本地 listener 或其他 endpoint。
var ErrPresenceClosed = errors.New("cloud companion closed daemon presence")

// OfferAnswerer 把 companion 中继的 WebRTC offer 协商为公开进程拥有的 answer。
// 实现只能接收 signaling DTO 和 ICE 配置；CapabilityGrant 必须在 DTLS DataChannel 内由目标 daemon 验证。
type OfferAnswerer interface {
	// Answer 协商单个 signaling offer；iceServers 是当前 PresenceReady 固定的服务准入配置。
	Answer(context.Context, *cloudpb.SignalingOffer, []*cloudpb.IceServer) (*cloudpb.SignalingAnswer, error)
}

// Agent 管理 daemon 通过本机 Cloud Companion 建立的一条 presence stream。
// Companion 只拥有账号会话、设备目录和 signaling；Agent 不发布 terminal inventory，也不把 terminal capability 交给云侧。
type Agent struct {
	Companion cloudcompanion.Client
	// Identity 是 presence proof 与 DataChannel DeviceHello 共用的 daemon-local 身份真值。
	// 私钥只能留在当前公开 daemon 进程，不能传给 Companion、Control Plane 或 Hub。
	Identity remoteauth.Identity
	// Metadata 是允许云设备目录看到的非秘密展示信息；不能包含 terminal inventory、capability 或本机凭据。
	Metadata *cloudpb.DeviceMetadata
	Answerer OfferAnswerer
	// Now 只用于 deterministic harness；零值使用 UTC 当前时间。
	Now func() time.Time
}

// Run 持续消费当前 daemon presence，直到 context、stream 或 companion 明确结束。
// 单个 offer 的协商失败通过 CompleteSignalingOffer 回传稳定错误，并继续处理同一 presence 上的其他 session。
func (agent Agent) Run(ctx context.Context) error {
	if agent.Companion == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "remote daemon cloud companion is not configured")
	}
	if agent.Answerer == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon offer answerer is not configured")
	}
	if err := agent.Identity.Validate(); err != nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon DeviceIdentity is invalid")
	}
	presence, err := agent.createPresenceRequest(ctx)
	if err != nil {
		return err
	}
	stream, err := agent.Companion.OpenPresence(ctx, presence)
	if err != nil {
		return err
	}
	if stream == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an empty presence stream")
	}
	defer stream.Close()

	var presenceSessionID string
	var iceServers []*cloudpb.IceServer
	for {
		event, receiveErr := stream.Receive()
		if receiveErr != nil {
			return receiveErr
		}
		if event == nil {
			return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an empty presence event")
		}
		switch payload := event.GetPayload().(type) {
		case *cloudpb.PresenceEvent_Ready:
			if payload.Ready == nil {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an empty presence ready event")
			}
			presenceSessionID = strings.TrimSpace(payload.Ready.GetPresenceSessionId())
			if presenceSessionID == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an empty presence session")
			}
			iceServers = cloneIceServers(payload.Ready.GetIceServers())
		case *cloudpb.PresenceEvent_Offer:
			if payload.Offer == nil || strings.TrimSpace(payload.Offer.GetSignalingSessionId()) == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an invalid signaling offer")
			}
			if presenceSessionID == "" || strings.TrimSpace(payload.Offer.GetManagedSessionId()) == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion routed an offer without an active presence or managed session")
			}
			if err := agent.completeOffer(ctx, payload.Offer, iceServers); err != nil {
				return err
			}
		case *cloudpb.PresenceEvent_Error:
			return cloudcompanion.ErrorFromWire(payload.Error)
		case *cloudpb.PresenceEvent_Closed:
			reason := ""
			if payload.Closed != nil {
				reason = strings.TrimSpace(payload.Closed.GetReason())
			}
			if reason == "" {
				return ErrPresenceClosed
			}
			return fmt.Errorf("%w: %s", ErrPresenceClosed, reason)
		case *cloudpb.PresenceEvent_Candidate:
			return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "trickle ICE is not enabled for the current public answerer")
		default:
			return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an unknown presence event")
		}
	}
}

// createPresenceRequest 获取一次性 challenge 并由当前公开 daemon 的 DeviceIdentity 签名。
// challenge、proof 与 metadata 可以经过 Companion；PrivateKey、CapabilityGrant 和 terminal 数据不得离开本进程。
func (agent Agent) createPresenceRequest(ctx context.Context) (*cloudpb.OpenPresenceRequest, error) {
	challenge, err := agent.Companion.BeginPresence(ctx, &cloudpb.BeginPresenceRequest{DeviceId: agent.Identity.DeviceID})
	if err != nil {
		return nil, err
	}
	if challenge == nil || strings.TrimSpace(challenge.GetPresenceSessionId()) == "" || strings.TrimSpace(challenge.GetChallengeId()) == "" || len(challenge.GetChallenge()) == 0 {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an invalid presence challenge")
	}
	now := time.Now().UTC()
	if agent.Now != nil {
		now = agent.Now().UTC()
	}
	input := &cloudpb.PresenceProofInput{
		PresenceSessionId: challenge.GetPresenceSessionId(), ChallengeId: challenge.GetChallengeId(),
		Challenge: append([]byte(nil), challenge.GetChallenge()...), DeviceId: agent.Identity.DeviceID,
		DevicePublicKey: append([]byte(nil), agent.Identity.PublicKey...), SignedAtUnixNano: now.UnixNano(),
	}
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(input)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(agent.Identity.PrivateKey, signingBytes)
	return &cloudpb.OpenPresenceRequest{
		PresenceSessionId: challenge.GetPresenceSessionId(),
		Proof: &cloudpb.DeviceProof{
			DeviceId: agent.Identity.DeviceID, DevicePublicKey: append([]byte(nil), agent.Identity.PublicKey...),
			ChallengeId: challenge.GetChallengeId(), Signature: signature, SignedAtUnixNano: now.UnixNano(),
		},
		Metadata: cloneDeviceMetadata(agent.Metadata),
	}, nil
}

func (agent Agent) completeOffer(ctx context.Context, offer *cloudpb.SignalingOffer, iceServers []*cloudpb.IceServer) error {
	answer, answerErr := agent.Answerer.Answer(ctx, offer, iceServers)
	request := &cloudpb.CompleteSignalingOfferRequest{SignalingSessionId: offer.GetSignalingSessionId()}
	if answerErr != nil {
		request.Result = &cloudpb.CompleteSignalingOfferRequest_Error{Error: cloudcompanion.ErrorToWire(answerErr)}
	} else if answer == nil || strings.TrimSpace(answer.GetSdp()) == "" {
		request.Result = &cloudpb.CompleteSignalingOfferRequest_Error{Error: &cloudpb.CloudError{
			Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, Message: "remote daemon answerer returned an empty answer",
		}}
	} else {
		answer.SignalingSessionId = offer.GetSignalingSessionId()
		request.Result = &cloudpb.CompleteSignalingOfferRequest_Answer{Answer: answer}
	}
	_, err := agent.Companion.CompleteSignalingOffer(ctx, request)
	return err
}

func cloneIceServers(servers []*cloudpb.IceServer) []*cloudpb.IceServer {
	cloned := make([]*cloudpb.IceServer, 0, len(servers))
	for _, server := range servers {
		if server == nil {
			continue
		}
		cloned = append(cloned, &cloudpb.IceServer{
			Urls: append([]string(nil), server.GetUrls()...), Username: server.GetUsername(), Credential: server.GetCredential(),
		})
	}
	return cloned
}

func cloneDeviceMetadata(metadata *cloudpb.DeviceMetadata) *cloudpb.DeviceMetadata {
	if metadata == nil {
		return nil
	}
	return &cloudpb.DeviceMetadata{
		DisplayName: metadata.GetDisplayName(), Hostname: metadata.GetHostname(), Platform: metadata.GetPlatform(),
		TermxVersion: metadata.GetTermxVersion(), SignalingVersions: append([]uint32(nil), metadata.GetSignalingVersions()...),
	}
}
