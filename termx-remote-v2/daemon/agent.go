package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
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
	Presence  *cloudpb.OpenPresenceRequest
	Answerer  OfferAnswerer
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
	if agent.Presence == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon presence request is not configured")
	}
	stream, err := agent.Companion.OpenPresence(ctx, agent.Presence)
	if err != nil {
		return err
	}
	if stream == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an empty presence stream")
	}
	defer stream.Close()

	var managedSessionID string
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
			managedSessionID = strings.TrimSpace(payload.Ready.GetManagedSessionId())
			if managedSessionID == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an empty managed session")
			}
			iceServers = cloneIceServers(payload.Ready.GetIceServers())
		case *cloudpb.PresenceEvent_Offer:
			if payload.Offer == nil || strings.TrimSpace(payload.Offer.GetSignalingSessionId()) == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an invalid signaling offer")
			}
			if managedSessionID == "" || strings.TrimSpace(payload.Offer.GetManagedSessionId()) != managedSessionID {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion routed an offer outside the active managed session")
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
