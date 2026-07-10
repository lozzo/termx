// Package client owns the Hub/P2P client-side WebRTC dialer.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	hubclient "github.com/lozzow/termx/termx-hub/client"
	remotev2webrtc "github.com/lozzow/termx/termx-remote-v2/webrtc"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/lozzow/termx/termx-shared/transport/datachannel"
	pion "github.com/pion/webrtc/v4"
)

// RelayMode 描述客户端对 Hub 返回 relay capability 的使用限制。
type RelayMode string

const (
	// RelayAuto 允许 ICE 先直连并在 Hub policy 允许时使用 TURN relay。
	RelayAuto RelayMode = "auto"
	// RelayDirect 禁止使用 Hub 返回的 TURN server。
	RelayDirect RelayMode = "direct"
	// RelayOnly 要求 Hub 明确允许 relay；Pion 只使用 relay candidate。
	RelayOnly RelayMode = "relay_only"
)

// DialOptions 是一个 hub/P2P endpoint 的完整连接身份。
// HubDeviceID 只用于发现，DeviceFingerprint 是 trust anchor，CapabilityGrant 是从 grant_ref 凭据存储解析出的 bearer secret。
type DialOptions struct {
	HubURL            string
	HubDeviceID       string
	DeviceFingerprint string
	CapabilityGrant   string
	RelayMode         RelayMode
	HTTPClient        *http.Client
	Now               time.Time
}

// Dial 验证本地 capability grant，通过 Hub 中继 WebRTC offer/answer，并返回 termx protocol transport。
// Hub 或 WebRTC 失败只返回当前 endpoint 错误，不 fallback 到 local、SSH、旧 remote UI 或原始 shell。
func Dial(ctx context.Context, options DialOptions) (transport.Transport, error) {
	claims, err := remoteauth.Verify(options.CapabilityGrant, options.DeviceFingerprint, options.Now, nil)
	if err != nil {
		return nil, fmt.Errorf("verify hub endpoint capability grant: %w", err)
	}
	if claims.DeviceID != strings.TrimSpace(options.HubDeviceID) {
		return nil, fmt.Errorf("hub endpoint device mismatch: grant %q registry %q", claims.DeviceID, options.HubDeviceID)
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	preflight, err := sessionPreflight(ctx, httpClient, options)
	if err != nil {
		return nil, err
	}
	configuration, err := peerConfiguration(preflight, options.RelayMode)
	if err != nil {
		return nil, err
	}
	peer, err := pion.NewPeerConnection(configuration)
	if err != nil {
		return nil, fmt.Errorf("create hub endpoint peer connection: %w", err)
	}
	channel, err := peer.CreateDataChannel("termx-protocol", nil)
	if err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("create hub endpoint protocol data channel: %w", err)
	}
	opened := make(chan struct{})
	channel.OnOpen(func() { close(opened) })
	channel.OnClose(func() { _ = peer.Close() })
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("create hub endpoint offer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("set hub endpoint local offer: %w", err)
	}
	select {
	case <-ctx.Done():
		_ = peer.Close()
		return nil, ctx.Err()
	case <-gatherComplete:
	}
	localDescription := peer.LocalDescription()
	if localDescription == nil {
		_ = peer.Close()
		return nil, fmt.Errorf("hub endpoint offer has no local description")
	}
	answer, err := submitSession(ctx, httpClient, options, localDescription.SDP)
	if err != nil {
		_ = peer.Close()
		return nil, err
	}
	if err := peer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.SDP}); err != nil {
		_ = peer.Close()
		return nil, fmt.Errorf("set hub endpoint remote answer: %w", err)
	}
	select {
	case <-ctx.Done():
		_ = peer.Close()
		return nil, ctx.Err()
	case <-opened:
	}
	return &peerTransport{Transport: datachannel.New(remotev2webrtc.NewChannel(channel)), peer: peer}, nil
}

type sessionPreflightResponse struct {
	ICEServers  []hubclient.ICEServer `json:"ice_servers"`
	RelayPolicy hubclient.RelayPolicy `json:"relay_policy"`
}

type sessionAnswerResponse struct {
	Pending bool `json:"pending"`
	Answer  struct {
		SDP string `json:"sdp"`
	} `json:"answer"`
}

func sessionPreflight(ctx context.Context, httpClient *http.Client, options DialOptions) (sessionPreflightResponse, error) {
	var response sessionPreflightResponse
	err := postJSON(ctx, httpClient, options.HubURL, "/api/v1/sessions/ice", map[string]any{
		"machine_id": options.HubDeviceID, "session_token": options.CapabilityGrant,
	}, &response)
	if err != nil {
		return sessionPreflightResponse{}, fmt.Errorf("hub endpoint ICE preflight: %w", err)
	}
	return response, nil
}

func submitSession(ctx context.Context, httpClient *http.Client, options DialOptions, offerSDP string) (hubclient.Answer, error) {
	request := map[string]any{
		"machine_id":    options.HubDeviceID,
		"session_token": options.CapabilityGrant,
		"offer":         map[string]any{"session_id": fmt.Sprintf("termx-%d", time.Now().UnixNano()), "sdp": offerSDP},
	}
	var response sessionAnswerResponse
	if err := postJSON(ctx, httpClient, options.HubURL, "/api/v1/sessions", request, &response); err != nil {
		return hubclient.Answer{}, fmt.Errorf("submit hub endpoint offer: %w", err)
	}
	if response.Pending {
		return hubclient.Answer{}, fmt.Errorf("hub endpoint answer is pending; asynchronous polling is not yet available")
	}
	if response.Answer.SDP == "" {
		return hubclient.Answer{}, fmt.Errorf("hub endpoint returned empty answer SDP")
	}
	return hubclient.Answer{SDP: response.Answer.SDP}, nil
}

func peerConfiguration(preflight sessionPreflightResponse, mode RelayMode) (pion.Configuration, error) {
	configuration := pion.Configuration{}
	if mode == RelayOnly && !preflight.RelayPolicy.AllowRelay {
		return pion.Configuration{}, fmt.Errorf("hub endpoint relay_only requested but Hub relay is unavailable")
	}
	for _, server := range preflight.ICEServers {
		urls := append([]string(nil), server.URLs...)
		if mode == RelayDirect {
			filtered := urls[:0]
			for _, candidateURL := range urls {
				if !strings.HasPrefix(candidateURL, "turn:") && !strings.HasPrefix(candidateURL, "turns:") {
					filtered = append(filtered, candidateURL)
				}
			}
			urls = filtered
		}
		if len(urls) == 0 {
			continue
		}
		configuration.ICEServers = append(configuration.ICEServers, pion.ICEServer{URLs: urls, Username: server.Username, Credential: server.Credential})
	}
	if mode == RelayOnly {
		configuration.ICETransportPolicy = pion.ICETransportPolicyRelay
	}
	return configuration, nil
}

func postJSON(ctx context.Context, httpClient *http.Client, baseURL string, path string, input any, output any) error {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid Hub URL %q", baseURL)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Hub returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return err
	}
	return nil
}

type peerTransport struct {
	transport.Transport
	peer *pion.PeerConnection
}

func (transport *peerTransport) Close() error {
	err := transport.Transport.Close()
	peerErr := transport.peer.Close()
	if err != nil {
		return err
	}
	return peerErr
}
