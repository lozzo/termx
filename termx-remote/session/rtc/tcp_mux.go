package rtc

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

type ICETCPMuxEndpoint struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
}

func (e ICETCPMuxEndpoint) PortString() string {
	return strconv.Itoa(e.Port)
}

type LocalICETCPMux struct {
	mu       sync.Mutex
	listener net.Listener
	mux      ice.TCPMux
	endpoint ICETCPMuxEndpoint
	listenIP net.IP
}

func StartLocalICETCPMux(ctx context.Context, addr string) (*LocalICETCPMux, error) {
	_ = ctx
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("local ICE TCP address is required")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	tcpMux := webrtc.NewICETCPMux(nil, listener, 20)
	endpoint := ICETCPMuxEndpointFromAddr(listener.Addr())
	listenIP := net.IP(nil)
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok && tcpAddr.IP != nil {
		listenIP = append(net.IP(nil), tcpAddr.IP...)
	}
	return &LocalICETCPMux{
		listener: listener,
		mux:      tcpMux,
		endpoint: endpoint,
		listenIP: listenIP,
	}, nil
}

func ICETCPMuxEndpointFromAddr(addr net.Addr) ICETCPMuxEndpoint {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return ICETCPMuxEndpoint{}
	}
	host := tcpAddr.IP.String()
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return ICETCPMuxEndpoint{
		Enabled: true,
		Host:    host,
		Port:    tcpAddr.Port,
	}
}

func (m *LocalICETCPMux) Endpoint() ICETCPMuxEndpoint {
	if m == nil {
		return ICETCPMuxEndpoint{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.endpoint
}

func (m *LocalICETCPMux) Apply(setting *webrtc.SettingEngine) {
	if m == nil || setting == nil || m.mux == nil {
		return
	}
	listenIP := append(net.IP(nil), m.listenIP...)
	setting.SetICETCPMux(m.mux)
	setting.SetIncludeLoopbackCandidate(true)
	if len(listenIP) > 0 && !listenIP.IsUnspecified() {
		setting.SetIPFilter(func(ip net.IP) bool {
			return ip.Equal(listenIP)
		})
	}
	setting.SetNetworkTypes([]webrtc.NetworkType{
		webrtc.NetworkTypeUDP4,
		webrtc.NetworkTypeUDP6,
		webrtc.NetworkTypeTCP4,
		webrtc.NetworkTypeTCP6,
	})
}

func (m *LocalICETCPMux) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mux != nil {
		err := m.mux.Close()
		m.mux = nil
		m.listener = nil
		m.endpoint = ICETCPMuxEndpoint{}
		m.listenIP = nil
		return err
	}
	if m.listener != nil {
		err := m.listener.Close()
		m.listener = nil
		m.endpoint = ICETCPMuxEndpoint{}
		m.listenIP = nil
		return err
	}
	return nil
}
