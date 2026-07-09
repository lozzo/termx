package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	pionturn "github.com/pion/turn/v4"
)

const credentialTTL = 24 * time.Hour

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type Config struct {
	ListenAddr string
	PublicIP   string
	Secret     string
	Realm      string
	Clock      Clock
}

type Server struct {
	cfg         Config
	clock       Clock
	traffic     *TrafficMeter
	udpConn     net.PacketConn
	tcpListener net.Listener
	server      *pionturn.Server
	closeOnce   sync.Once
	closeErr    error
}

func NewServer(cfg Config) (*Server, error) {
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:3478"
	}
	cfg.PublicIP = strings.TrimSpace(cfg.PublicIP)
	cfg.Secret = strings.TrimSpace(cfg.Secret)
	cfg.Realm = strings.TrimSpace(cfg.Realm)
	if cfg.Realm == "" {
		cfg.Realm = "termx"
	}
	if cfg.Secret == "" {
		return nil, errors.New("turn secret is required")
	}
	if requiresPublicAdvertiseHost(cfg.ListenAddr) && cfg.PublicIP == "" {
		return nil, errors.New("turn public ip is required when listen address is unspecified")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}

	udpConn, err := net.ListenPacket("udp4", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen turn udp: %w", err)
	}
	tcpAddr := cfg.ListenAddr
	if configuredPort(cfg.ListenAddr) == "0" {
		tcpAddr = replacePort(cfg.ListenAddr, localPort(udpConn.LocalAddr()))
	}
	tcpListener, err := net.Listen("tcp4", tcpAddr)
	if err != nil {
		_ = udpConn.Close()
		return nil, fmt.Errorf("listen turn tcp: %w", err)
	}

	traffic := NewTrafficMeter()
	relayGenerator := newMeteredRelayAddressGenerator(relayAddressGenerator(cfg, udpConn.LocalAddr()), traffic)
	s := &Server{
		cfg:         cfg,
		clock:       clock,
		traffic:     traffic,
		udpConn:     udpConn,
		tcpListener: tcpListener,
	}
	server, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm:        cfg.Realm,
		AuthHandler:  s.AuthHandler(),
		EventHandler: relayTrafficEventHandler(relayGenerator),
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn:            udpConn,
			RelayAddressGenerator: relayGenerator,
		}},
		ListenerConfigs: []pionturn.ListenerConfig{{
			Listener:              tcpListener,
			RelayAddressGenerator: relayGenerator,
		}},
	})
	if err != nil {
		_ = tcpListener.Close()
		_ = udpConn.Close()
		return nil, fmt.Errorf("start turn server: %w", err)
	}
	s.server = server
	return s, nil
}

func (s *Server) GenerateCredentials() (username, credential string) {
	expiresAt := s.clock.Now().UTC().Add(credentialTTL).Unix()
	username = strconv.FormatInt(expiresAt, 10)
	return username, computeCredential(username, s.cfg.Secret)
}

func (s *Server) GenerateCredentialsForAgent(agentID string) (username, credential string) {
	username, credential = s.GenerateCredentials()
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return username, credential
	}
	username = agentID + ":" + username
	return username, computeCredential(username, s.cfg.Secret)
}

func (s *Server) AuthHandler() pionturn.AuthHandler {
	return func(username, realm string, _ net.Addr) ([]byte, bool) {
		if strings.TrimSpace(realm) != s.cfg.Realm {
			return nil, false
		}
		expiryPart := strings.TrimSpace(username)
		if _, rest, ok := strings.Cut(expiryPart, ":"); ok {
			expiryPart = rest
		}
		expiry, err := strconv.ParseInt(strings.TrimSpace(expiryPart), 10, 64)
		if err != nil || s.clock.Now().UTC().Unix() > expiry {
			return nil, false
		}
		credential := computeCredential(username, s.cfg.Secret)
		return pionturn.GenerateAuthKey(username, s.cfg.Realm, credential), true
	}
}

func (s *Server) URLs() []string {
	host := s.advertiseHost()
	port := s.advertisePort()
	return []string{
		"turn:" + net.JoinHostPort(host, port) + "?transport=udp",
		"turn:" + net.JoinHostPort(host, port) + "?transport=tcp",
	}
}

func (s *Server) UDPAddr() net.Addr {
	return s.udpConn.LocalAddr()
}

func (s *Server) TCPAddr() net.Addr {
	return s.tcpListener.Addr()
}

func (s *Server) DrainTraffic() []TrafficDelta {
	if s == nil || s.traffic == nil {
		return nil
	}
	return s.traffic.DrainTraffic()
}

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		if s.server != nil {
			s.closeErr = s.server.Close()
		}
	})
	return s.closeErr
}

func relayTrafficEventHandler(generator *meteredRelayAddressGenerator) pionturn.EventHandler {
	return pionturn.EventHandler{
		OnAllocationCreated: func(srcAddr, dstAddr net.Addr, protocol, username, _ string, relayAddr net.Addr, _ int) {
			generator.rememberAllocation(srcAddr, dstAddr, protocol, relayAddr)
			generator.bindAgent(relayAddr, username)
		},
		OnAllocationDeleted: func(srcAddr, dstAddr net.Addr, protocol, _, _ string) {
			generator.forgetAllocation(srcAddr, dstAddr, protocol)
		},
	}
}

func (s *Server) advertiseHost() string {
	if s.cfg.PublicIP != "" {
		return s.cfg.PublicIP
	}
	if host, _, err := net.SplitHostPort(s.udpConn.LocalAddr().String()); err == nil && host != "" {
		return host
	}
	return "127.0.0.1"
}

func (s *Server) advertisePort() string {
	_, port, err := net.SplitHostPort(s.udpConn.LocalAddr().String())
	if err != nil || port == "" {
		return "3478"
	}
	return port
}

func relayAddressGenerator(cfg Config, listenAddr net.Addr) pionturn.RelayAddressGenerator {
	address := "0.0.0.0"
	if host, _, err := net.SplitHostPort(listenAddr.String()); err == nil && host != "" {
		address = host
	}
	if publicIP := net.ParseIP(cfg.PublicIP); publicIP != nil {
		return &pionturn.RelayAddressGeneratorStatic{
			RelayAddress: publicIP,
			Address:      address,
		}
	}
	return &pionturn.RelayAddressGeneratorNone{Address: address}
}

func requiresPublicAdvertiseHost(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func computeCredential(username, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func configuredPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}

func localPort(addr net.Addr) string {
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return port
}

func replacePort(addr, port string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return net.JoinHostPort(host, port)
}
