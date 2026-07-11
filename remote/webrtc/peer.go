package webrtc

import (
	"net"
	"net/url"
	"strings"

	pion "github.com/pion/webrtc/v4"
)

// NewPeerConnection 按受信 ICE 配置创建公开 WebRTC primitive。
// 只有显式 loopback TURN URL 会启用 loopback candidate，用于本地开发云或自托管 harness；普通公网配置保持 Pion 默认网络边界。
func NewPeerConnection(configuration pion.Configuration) (*pion.PeerConnection, error) {
	if !containsLoopbackTURN(configuration.ICEServers) {
		return pion.NewPeerConnection(configuration)
	}
	settingEngine := pion.SettingEngine{}
	settingEngine.SetIncludeLoopbackCandidate(true)
	return pion.NewAPI(pion.WithSettingEngine(settingEngine)).NewPeerConnection(configuration)
}

func containsLoopbackTURN(servers []pion.ICEServer) bool {
	for _, server := range servers {
		for _, rawURL := range server.URLs {
			parsed, err := url.Parse(strings.TrimSpace(rawURL))
			if err != nil || !strings.EqualFold(parsed.Scheme, "turn") && !strings.EqualFold(parsed.Scheme, "turns") {
				continue
			}
			authority := parsed.Host
			if authority == "" {
				authority = parsed.Opaque
			}
			authority, _, _ = strings.Cut(authority, "?")
			host, _, err := net.SplitHostPort(authority)
			if err == nil {
				if strings.EqualFold(host, "localhost") {
					return true
				}
				if address := net.ParseIP(host); address != nil && address.IsLoopback() {
					return true
				}
			}
		}
	}
	return false
}
