// Package client owns the public client-side managed WebRTC dialer.
package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	remotev2webrtc "github.com/lozzow/termx/termx-remote-v2/webrtc"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/lozzow/termx/termx-shared/transport/datachannel"
	pion "github.com/pion/webrtc/v4"
)

const protocolChannelLabel = "termx-protocol"

// SessionAuthentication 是 client 在 DTLS DataChannel 内验证目标设备并提交 capability 时使用的本地输入。
// CapabilityGrant 只能交给目标 daemon；Authenticator 不得把它转发给 Cloud Companion、Hub、日志或 endpoint registry。
type SessionAuthentication struct {
	TargetDeviceID    string
	DeviceFingerprint string
	CapabilityGrant   string
	Now               time.Time
}

// SessionAuthenticator 是 managed WebRTC transport 返回给 termx protocol 前的端到端安全门。
// 实现必须通过 DataChannel 完成 daemon DeviceIdentity proof 和 CapabilityGrant open；失败时 Dial 关闭 PeerConnection 且不返回 transport。
type SessionAuthenticator interface {
	Authenticate(context.Context, transport.Transport, SessionAuthentication) error
}

// DialOptions 是一个 managed cloud endpoint 的公开连接输入。
// Companion 只接收 endpoint、device、signaling 和 route preference；fingerprint 与 grant 始终留在当前公开进程。
type DialOptions struct {
	Companion         cloudcompanion.Client
	EndpointID        string
	TargetDeviceID    string
	DeviceFingerprint string
	CapabilityGrant   string
	RoutePreference   cloudpb.RoutePreference
	Authenticator     SessionAuthenticator
	Now               time.Time
}

// Dial 先本地验证 capability 绑定，再通过 Companion 协商 WebRTC，并在 DataChannel 内完成端到端授权。
// Companion、WebRTC 或授权失败只返回当前 endpoint 错误，不 fallback 到 local、SSH、旧 Hub API、remote UI 或原始 shell。
func Dial(ctx context.Context, options DialOptions) (transport.Transport, error) {
	endpointID := strings.TrimSpace(options.EndpointID)
	targetDeviceID := strings.TrimSpace(options.TargetDeviceID)
	if endpointID == "" || targetDeviceID == "" {
		return nil, fmt.Errorf("managed WebRTC endpoint and target device are required")
	}
	claims, err := remoteauth.Verify(options.CapabilityGrant, options.DeviceFingerprint, options.Now, nil)
	if err != nil {
		return nil, fmt.Errorf("verify managed endpoint capability grant: %w", err)
	}
	if claims.DeviceID != targetDeviceID {
		return nil, fmt.Errorf("managed endpoint device mismatch: grant %q registry %q", claims.DeviceID, targetDeviceID)
	}
	if options.Authenticator == nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "DataChannel end-to-end authenticator is not available")
	}
	if options.Companion == nil {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion is not configured")
	}
	if options.RoutePreference == cloudpb.RoutePreference_ROUTE_PREFERENCE_UNSPECIFIED {
		return nil, fmt.Errorf("managed WebRTC route preference is required")
	}

	resolved, err := options.Companion.ResolveEndpoint(ctx, &cloudpb.ResolveEndpointRequest{
		EndpointId: endpointID, TargetDeviceId: targetDeviceID,
	})
	if err != nil {
		return nil, err
	}
	if resolved == nil || strings.TrimSpace(resolved.GetManagedSessionId()) == "" {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an invalid endpoint resolution")
	}
	if resolved.GetTargetDeviceId() != "" && strings.TrimSpace(resolved.GetTargetDeviceId()) != targetDeviceID {
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion resolved a different target device")
	}
	configuration, err := peerConfiguration(resolved.GetIceServers(), options.RoutePreference)
	if err != nil {
		return nil, err
	}
	peer, err := pion.NewPeerConnection(configuration)
	if err != nil {
		return nil, fmt.Errorf("create managed endpoint peer connection: %w", err)
	}
	channel, err := peer.CreateDataChannel(protocolChannelLabel, nil)
	if err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("create managed endpoint protocol data channel: %w", err)
	}
	opened := make(chan struct{})
	channel.OnOpen(func() { close(opened) })
	channel.OnClose(func() { _ = peer.Close() })
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("create managed endpoint offer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("set managed endpoint local offer: %w", err)
	}
	select {
	case <-ctx.Done():
		_ = peer.Close()
		return nil, ctx.Err()
	case <-gatherComplete:
	}
	localDescription := peer.LocalDescription()
	if localDescription == nil || strings.TrimSpace(localDescription.SDP) == "" {
		_ = peer.Close()
		return nil, fmt.Errorf("managed endpoint offer has no local description")
	}
	signaling, err := options.Companion.CreateSignalingSession(ctx, &cloudpb.CreateSignalingSessionRequest{
		EndpointId:       endpointID,
		ManagedSessionId: resolved.GetManagedSessionId(),
		TargetDeviceId:   targetDeviceID,
		OfferSdp:         localDescription.SDP,
		RoutePreference:  options.RoutePreference,
	})
	if err != nil {
		_ = peer.Close()
		return nil, err
	}
	if signaling == nil {
		_ = peer.Close()
		return nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling stream")
	}
	defer signaling.Close()
	answer, candidates, err := receiveAnswer(signaling)
	if err != nil {
		_ = peer.Close()
		return nil, err
	}
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.GetSdp()}); err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("set managed endpoint remote answer: %w", err)
	}
	for _, candidate := range append(candidates, answer.GetCandidates()...) {
		if err := peer.AddICECandidate(toPionCandidate(candidate)); err != nil {
			_ = peer.Close()
			return nil, fmt.Errorf("add managed endpoint ICE candidate: %w", err)
		}
	}
	select {
	case <-ctx.Done():
		_ = peer.Close()
		return nil, ctx.Err()
	case <-opened:
	}
	secured := &peerTransport{Transport: datachannel.New(remotev2webrtc.NewChannel(channel)), peer: peer}
	authentication := SessionAuthentication{
		TargetDeviceID: targetDeviceID, DeviceFingerprint: options.DeviceFingerprint,
		CapabilityGrant: options.CapabilityGrant, Now: options.Now,
	}
	if err := options.Authenticator.Authenticate(ctx, secured, authentication); err != nil {
		_ = secured.Close()
		return nil, fmt.Errorf("authenticate managed endpoint DataChannel: %w", err)
	}
	return secured, nil
}

func receiveAnswer(stream cloudcompanion.SignalingStream) (*cloudpb.SignalingAnswer, []*cloudpb.IceCandidate, error) {
	var candidates []*cloudpb.IceCandidate
	for {
		event, err := stream.Receive()
		if err != nil {
			return nil, nil, err
		}
		if event == nil {
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling event")
		}
		switch payload := event.GetPayload().(type) {
		case *cloudpb.SignalingEvent_Answer:
			if payload.Answer == nil || strings.TrimSpace(payload.Answer.GetSdp()) == "" {
				return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an empty signaling answer")
			}
			return payload.Answer, candidates, nil
		case *cloudpb.SignalingEvent_Candidate:
			if payload.Candidate != nil {
				candidates = append(candidates, payload.Candidate)
			}
		case *cloudpb.SignalingEvent_Error:
			return nil, nil, cloudcompanion.ErrorFromWire(payload.Error)
		case *cloudpb.SignalingEvent_Closed:
			reason := "signaling session closed"
			if payload.Closed != nil && strings.TrimSpace(payload.Closed.GetReason()) != "" {
				reason = payload.Closed.GetReason()
			}
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, reason)
		default:
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned an unknown signaling event")
		}
	}
}

func peerConfiguration(servers []*cloudpb.IceServer, preference cloudpb.RoutePreference) (pion.Configuration, error) {
	switch preference {
	case cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
		cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR:
	default:
		return pion.Configuration{}, fmt.Errorf("unsupported managed WebRTC route preference %s", preference)
	}
	configuration := pion.Configuration{}
	for _, server := range servers {
		if server == nil {
			continue
		}
		urls := append([]string(nil), server.GetUrls()...)
		if preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
			filtered := urls[:0]
			for _, candidateURL := range urls {
				lower := strings.ToLower(strings.TrimSpace(candidateURL))
				if !strings.HasPrefix(lower, "turn:") && !strings.HasPrefix(lower, "turns:") {
					filtered = append(filtered, candidateURL)
				}
			}
			urls = filtered
		}
		if len(urls) == 0 {
			continue
		}
		configuration.ICEServers = append(configuration.ICEServers, pion.ICEServer{
			URLs: urls, Username: server.GetUsername(), Credential: server.GetCredential(),
		})
	}
	return configuration, nil
}

func toPionCandidate(candidate *cloudpb.IceCandidate) pion.ICECandidateInit {
	if candidate == nil {
		return pion.ICECandidateInit{}
	}
	mid := candidate.GetSdpMid()
	lineIndex := uint16(candidate.GetSdpMlineIndex())
	usernameFragment := candidate.GetUsernameFragment()
	result := pion.ICECandidateInit{Candidate: candidate.GetCandidate(), SDPMLineIndex: &lineIndex}
	if mid != "" {
		result.SDPMid = &mid
	}
	if usernameFragment != "" {
		result.UsernameFragment = &usernameFragment
	}
	return result
}

type peerTransport struct {
	transport.Transport
	peer *pion.PeerConnection
}

func (connection *peerTransport) Close() error {
	err := connection.Transport.Close()
	peerErr := connection.peer.Close()
	if err != nil {
		return err
	}
	return peerErr
}
