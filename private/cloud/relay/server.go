package relay

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	pionturn "github.com/pion/turn/v4"
)

// ServerConfig 配置单节点 UDP TURN Relay。
// Authority 是唯一 credential/limit owner；Server 不接受静态 username/password 或 24h shared credential。
type ServerConfig struct {
	Authority  *Authority
	ListenAddr string
	PublicIP   string
}

// Server 是 Pion TURN packet forwarder 与 lease meter 的部署单元。
// 它只观察 packet 长度和 allocation metadata，不能访问 WebRTC DTLS/DataChannel 明文。
type Server struct {
	authority *Authority
	packet    net.PacketConn
	turn      *pionturn.Server
	generator *meteredRelayGenerator
	publicIP  string
	closeOnce sync.Once
	closeErr  error
}

// NewServer 在 ListenAddr 启动 UDP TURN server，并把 AuthHandler 与 traffic meter 绑定到 Authority。
// 监听失败、无 Authority 或 unspecified bind 缺 PublicIP 时返回错误，不回退共享 TURN secret。
func NewServer(config ServerConfig) (*Server, error) {
	if config.Authority == nil {
		return nil, fmt.Errorf("Relay authority is required")
	}
	listenAddr := strings.TrimSpace(config.ListenAddr)
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	publicIP := strings.TrimSpace(config.PublicIP)
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid Relay listen address: %w", err)
	}
	if ip := net.ParseIP(host); (host == "" || ip != nil && ip.IsUnspecified()) && net.ParseIP(publicIP) == nil {
		return nil, fmt.Errorf("Relay public IP is required for unspecified listen address")
	}
	packet, err := net.ListenPacket("udp4", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen Relay UDP: %w", err)
	}
	base := relayAddressGenerator(packet.LocalAddr(), publicIP)
	generator := newMeteredRelayGenerator(base, config.Authority)
	server := &Server{authority: config.Authority, packet: packet, generator: generator, publicIP: publicIP}
	turnServer, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm:       config.Authority.realm,
		AuthHandler: server.authHandler,
		EventHandler: pionturn.EventHandler{
			OnAllocationCreated: server.onAllocationCreated,
			OnAllocationDeleted: server.onAllocationDeleted,
		},
		PacketConnConfigs: []pionturn.PacketConnConfig{{PacketConn: packet, RelayAddressGenerator: generator}},
	})
	if err != nil {
		_ = packet.Close()
		return nil, fmt.Errorf("start Relay TURN server: %w", err)
	}
	server.turn = turnServer
	return server, nil
}

// URL 返回 endpoint ICE config 使用的 UDP TURN URL。
func (server *Server) URL() string {
	if server == nil || server.packet == nil {
		return ""
	}
	host, port, err := net.SplitHostPort(server.packet.LocalAddr().String())
	if err != nil {
		return ""
	}
	if server.publicIP != "" {
		host = server.publicIP
	}
	return "turn:" + net.JoinHostPort(host, port) + "?transport=udp"
}

// Addr 返回 TURN listener 的实际本地地址，供部署 health check 和 integration harness 使用。
func (server *Server) Addr() net.Addr {
	if server == nil || server.packet == nil {
		return nil
	}
	return server.packet.LocalAddr()
}

// ActivateLease 让 Edge HTTP adapter 把 Controller 签名 lease 交给 Relay authority，
// 并取得 principal-specific TURN credential；Server 不自行签发或扩大 quota。
func (server *Server) ActivateLease(request ActivationRequest) (Activation, error) {
	if server == nil || server.authority == nil {
		return Activation{}, ErrLeaseRejected
	}
	return server.authority.ActivateLease(request)
}

// DrainUsageRecords 从 Relay authority 取得已签名增量记录；调用方必须先写 durable outbox 再上报。
func (server *Server) DrainUsageRecords(terminationReason string) ([]UsageRecord, error) {
	if server == nil || server.authority == nil {
		return nil, ErrLeaseRejected
	}
	return server.authority.DrainUsageRecords(terminationReason)
}

// FlushUsageOutbox 让 Relay authority 以落盘成功为计量提交点。
func (server *Server) FlushUsageOutbox(outbox *UsageOutbox, terminationReason string) error {
	if server == nil || server.authority == nil {
		return ErrLeaseRejected
	}
	return server.authority.FlushUsageOutbox(outbox, terminationReason)
}

// Close 幂等关闭 TURN server 和所有 active allocations。
func (server *Server) Close() error {
	if server == nil {
		return nil
	}
	server.closeOnce.Do(func() {
		if server.turn != nil {
			server.closeErr = server.turn.Close()
		} else if server.packet != nil {
			server.closeErr = server.packet.Close()
		}
	})
	return server.closeErr
}

func (server *Server) authHandler(username, realm string, source net.Addr) ([]byte, bool) {
	if source == nil {
		return nil, false
	}
	return server.authority.AuthenticateTURN(username, realm, source.String())
}

func (server *Server) onAllocationCreated(source, destination net.Addr, protocol, username, _ string, relayAddr net.Addr, _ int) {
	if source == nil || relayAddr == nil {
		return
	}
	allocationID := allocationKey(source, destination, protocol)
	if allocationID == "" {
		return
	}
	if err := server.authority.ConfirmAllocation(source.String(), allocationID, username); err != nil {
		return
	}
	server.generator.bind(relayAddr, allocationID)
}

func (server *Server) onAllocationDeleted(source, destination net.Addr, protocol, _, _ string) {
	allocationID := allocationKey(source, destination, protocol)
	if allocationID == "" {
		return
	}
	server.generator.forget(allocationID)
	server.authority.ReleaseAllocation(allocationID)
}

func relayAddressGenerator(listener net.Addr, publicIP string) pionturn.RelayAddressGenerator {
	host, _, _ := net.SplitHostPort(listener.String())
	address := host
	if address == "" {
		address = "0.0.0.0"
	}
	if ip := net.ParseIP(publicIP); ip != nil {
		return &pionturn.RelayAddressGeneratorStatic{RelayAddress: ip, Address: address}
	}
	return &pionturn.RelayAddressGeneratorNone{Address: address}
}

func allocationKey(source, destination net.Addr, protocol string) string {
	if source == nil {
		return ""
	}
	destinationText := ""
	if destination != nil {
		destinationText = destination.String()
	}
	return source.String() + "|" + destinationText + "|" + strings.TrimSpace(protocol)
}

var errRelayQuota = errors.New("Relay allocation quota exhausted")
