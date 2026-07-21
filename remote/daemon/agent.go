package daemon

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/remoteauth"
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
// Companion 只拥有账号会话、设备目录和 signaling；Agent 仅发布 AccessStore 生成的 opaque
// 管理投影，不发布 CapabilityGrant、scope、terminal identity 或 terminal payload。
type Agent struct {
	Companion cloudcompanion.Client
	// Identity 是 presence proof 与 DataChannel DeviceHello 共用的 daemon-local 身份真值。
	// 私钥只能留在当前公开 daemon 进程，不能传给 Companion、Control Plane 或 Hub。
	Identity remoteauth.Identity
	// Metadata 是允许云设备目录看到的非秘密展示信息；不能包含 terminal inventory、capability 或本机凭据。
	Metadata *cloudpb.DeviceMetadata
	Answerer OfferAnswerer
	// Runtime 是 daemon process 级 managed session owner；Presence 续约不得重建该对象。
	Runtime *ManagedRuntime
	// AccessStore 是 terminal grant 与脱敏 access inventory 的 daemon-local 持久真值。
	AccessStore *remoteauth.AccessStore
	// ControlReceipts 持久拥有 enrollment control key binding 与 command replay receipt。
	ControlReceipts *ControlReceiptStore
	// Now 只用于 deterministic harness；零值使用 UTC 当前时间。
	Now func() time.Time
	// RuntimeReportRetryDelay 是 runtime report 失败后的有界重试间隔；零值使用一秒。
	// 它只供确定性 harness 缩短等待，不改变 revision、Presence 或 assignment fencing。
	RuntimeReportRetryDelay time.Duration
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
	if agent.Runtime == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon managed runtime is not configured")
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
	var stopReporter func()
	defer func() {
		if stopReporter != nil {
			stopReporter()
		}
	}()

	var presenceSessionID string
	var controlOwnerHubID string
	var assignmentEpoch uint64
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
			controlOwnerHubID = strings.TrimSpace(payload.Ready.GetHubId())
			assignmentEpoch = payload.Ready.GetAssignmentEpoch()
			if controlOwnerHubID == "" || assignmentEpoch == 0 {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an incomplete Presence assignment")
			}
			observedAt := time.Now().UTC()
			if agent.Now != nil {
				observedAt = agent.Now().UTC()
			}
			if err := agent.Runtime.BindPresence(controlOwnerHubID, assignmentEpoch, presenceSessionID, observedAt); err != nil {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon could not bind managed Presence")
			}
			if stopReporter != nil {
				stopReporter()
			}
			registry := agent.Runtime.Registry()
			if registry == nil {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon managed registry is unavailable")
			}
			reporterContext, cancelReporter := context.WithCancel(ctx)
			reporterDone := make(chan struct{})
			go func() {
				defer close(reporterDone)
				agent.runRuntimeReporter(reporterContext, registry, controlOwnerHubID, assignmentEpoch, presenceSessionID)
			}()
			stopReporter = func() {
				cancelReporter()
				<-reporterDone
			}
			iceServers = cloneIceServers(payload.Ready.GetIceServers())
		case *cloudpb.PresenceEvent_Offer:
			if payload.Offer == nil || strings.TrimSpace(payload.Offer.GetSignalingSessionId()) == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an invalid signaling offer")
			}
			if presenceSessionID == "" || strings.TrimSpace(payload.Offer.GetManagedSessionId()) == "" || payload.Offer.GetSessionIncarnation() == 0 || payload.Offer.GetPresenceSessionId() != presenceSessionID || payload.Offer.GetAssignmentEpoch() != assignmentEpoch || payload.Offer.GetTargetDeviceId() != agent.Identity.DeviceID {
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
		case *cloudpb.PresenceEvent_DaemonCommand:
			if payload.DaemonCommand == nil || presenceSessionID == "" {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an invalid daemon command")
			}
			if agent.ControlReceipts == nil {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon control receipt store is not configured")
			}
			now := time.Now().UTC()
			if agent.Now != nil {
				now = agent.Now().UTC()
			}
			result, err := agent.Runtime.ExecuteControlCommand(ctx, payload.DaemonCommand, agent.ControlReceipts, agent.AccessStore, now)
			if err != nil {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon rejected control command")
			}
			response, err := agent.Companion.ReportDaemonCommandResult(ctx, &cloudpb.ReportDaemonCommandResultRequest{Result: result})
			if err != nil {
				return err
			}
			if response == nil || response.GetAcceptedCommandId() != result.GetCommandId() {
				return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion did not acknowledge daemon command result")
			}
		default:
			return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud companion returned an unknown presence event")
		}
	}
}

func (agent Agent) runRuntimeReporter(ctx context.Context, registry *ManagedSessionRegistry, hubID string, assignmentEpoch uint64, presenceSessionID string) {
	retryDelay := agent.RuntimeReportRetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}
	var pending *cloudpb.ReportDaemonRuntimeRequest
	var acceptedRevision uint64
	var acceptedAccessRevision uint64
	var hasAcceptedRevision bool
	var accessChanges <-chan struct{}
	if agent.AccessStore != nil {
		accessChanges = agent.AccessStore.AccessChanges()
	}
	for {
		if pending == nil {
			observedAt := time.Now().UTC()
			if agent.Now != nil {
				observedAt = agent.Now().UTC()
			}
			inventory, err := registry.Inventory("runtime-report", observedAt)
			if err != nil {
				return
			}
			accessRevision := uint64(0)
			if agent.AccessStore != nil {
				accessRevision = agent.AccessStore.AccessProjectionRevision()
			}
			if hasAcceptedRevision && inventory.GetRegistryRevision() == acceptedRevision && accessRevision == acceptedAccessRevision {
				select {
				case <-ctx.Done():
					return
				case <-registry.Changes():
					continue
				case <-accessChanges:
					continue
				}
			}
			reportID := fmt.Sprintf("%s:%d:%d", agent.Runtime.RuntimeGeneration(), inventory.GetRegistryRevision(), accessRevision)
			inventory.ReportId = reportID
			pending = &cloudpb.ReportDaemonRuntimeRequest{
				ReportId: reportID, HubId: hubID, AssignmentEpoch: assignmentEpoch,
				PresenceSessionId: presenceSessionID, DaemonRuntimeGeneration: agent.Runtime.RuntimeGeneration(),
				RegistryRevision: inventory.GetRegistryRevision(), PeerSessions: inventory,
			}
			pending.TerminalAccesses = BuildTerminalAccessInventory(agent.AccessStore, reportID, agent.Identity.DeviceID, hubID, assignmentEpoch, presenceSessionID, agent.Runtime.RuntimeGeneration(), inventory.GetRegistryRevision(), observedAt)
		}
		response, reportErr := agent.Companion.ReportDaemonRuntime(ctx, pending)
		if reportErr == nil && response != nil && response.GetReportId() == pending.GetReportId() && response.GetDaemonRuntimeGeneration() == pending.GetDaemonRuntimeGeneration() && response.GetAcceptedRegistryRevision() == pending.GetRegistryRevision() && (pending.GetTerminalAccesses() == nil || response.GetAcceptedAccessProjectionRevision() == pending.GetTerminalAccesses().GetAccessProjectionRevision()) {
			acceptedRevision = pending.GetRegistryRevision()
			if pending.GetTerminalAccesses() != nil {
				acceptedAccessRevision = pending.GetTerminalAccesses().GetAccessProjectionRevision()
			}
			hasAcceptedRevision = true
			pending = nil
			select {
			case <-ctx.Done():
				return
			case <-registry.Changes():
				continue
			case <-accessChanges:
				continue
			}
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-registry.Changes():
			if !timer.Stop() {
				<-timer.C
			}
			pending = nil
		case <-accessChanges:
			if !timer.Stop() {
				<-timer.C
			}
			pending = nil
		case <-timer.C:
		}
	}
}

// RunContinuously 维护 owning daemon 的长期 managed presence。
// 每个 Hub TTL 周期仍由 Run 重新执行 BeginPresence -> fresh DeviceIdentity proof -> OpenPresence；旧 admission、
// PresenceSession 和 challenge 绝不复用。只有流 EOF 或明确 retryable 的云错误会续约，显式关闭、鉴权和协议失败直接返回。
func (agent Agent) RunContinuously(ctx context.Context, retryDelay time.Duration) error {
	if ctx == nil {
		return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "remote daemon presence context is required")
	}
	if retryDelay <= 0 {
		retryDelay = time.Second
	}
	for {
		err := agent.Run(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !presenceRenewable(err) {
			return err
		}

		delay := retryDelay
		var cloudErr *cloudcompanion.Error
		if errors.As(err, &cloudErr) && cloudErr.RetryAfter > delay {
			delay = cloudErr.RetryAfter
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func presenceRenewable(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	var cloudErr *cloudcompanion.Error
	return errors.As(err, &cloudErr) && cloudErr.Retryable
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
	offerIceServers, answerErr := agent.iceServersForOffer(ctx, offer, iceServers)
	var answer *cloudpb.SignalingAnswer
	if answerErr == nil {
		answer, answerErr = agent.Answerer.Answer(ctx, offer, offerIceServers)
	}
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

// iceServersForOffer 只为显式 relay-only offer 获取当前 daemon principal 的短期 TURN material。
// 普通 direct/auto offer 继续使用 presence ICE；租约失败直接回传当前 signaling failure，不能改走 direct 或共享 credential。
func (agent Agent) iceServersForOffer(ctx context.Context, offer *cloudpb.SignalingOffer, presenceServers []*cloudpb.IceServer) ([]*cloudpb.IceServer, error) {
	if offer == nil || !offer.GetRelayOnly() {
		return cloneIceServers(presenceServers), nil
	}
	if offer.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || offer.GetManagedSessionId() == "" || offer.GetTargetDeviceId() != agent.Identity.DeviceID {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "relay-only offer binding is invalid")
	}
	request := &cloudpb.AcquireRelayLeaseRequest{
		ManagedSessionId: offer.GetManagedSessionId(), TargetDeviceId: agent.Identity.DeviceID,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
	}
	lease, err := agent.Companion.AcquireRelayLease(ctx, request)
	if err != nil {
		return nil, err
	}
	now := time.Time{}
	if agent.Now != nil {
		now = agent.Now().UTC()
	}
	if err := cloudcompanion.ValidateSingleRelayLease(request, lease, now); err != nil {
		return nil, err
	}
	return cloneIceServers(lease.GetIceServers()), nil
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
		MuxviaVersion: metadata.GetMuxviaVersion(), SignalingVersions: append([]uint32(nil), metadata.GetSignalingVersions()...),
	}
}
