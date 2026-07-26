// Package relay 把 Pion STUN/TURN 数据面装配进同一个 Edge 进程。
// 本包只转发字节并执行 Runtime 已冻结的租约上限，不解析 terminal 或 DataChannel payload。
package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/muxvia/muxvia/cloud/edge/policy"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"github.com/pion/transport/v4/stdnet"
	turn "github.com/pion/turn/v4"
)

// Runtime 是 TURN callback 提交到 Edge 唯一 State actor 的窄边界。
type Runtime interface {
	RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayLeaseClaims, string, bool, error)
	ReserveRelayAllocation(context.Context, string, string, time.Time) (policy.RelayAdmission, error)
	ActivateRelayAllocation(context.Context, string, string, cloudv1.RelayTransport, time.Time) error
	CancelRelayAllocationReservation(context.Context, string) error
	CloseRelayAllocation(context.Context, string, uint64, uint64, time.Time) (*cloudv1.UsageEvent, error)
}

// UsageOutbox 是 allocation 关闭后的先落盘边界。
type UsageOutbox interface {
	Put(*cloudv1.UsageEvent) error
}

// Config 提供由部署生成的 TURN listener/public endpoint、realm、Runtime 和 durable outbox。
type Config struct {
	ListenAddress  string
	PublicEndpoint string
	Realm          string
	Runtime        Runtime
	Outbox         UsageOutbox
	Now            func() time.Time
}

// Server 拥有 Edge 内置 Pion TURN server、relay socket 计数器和 callback correlation。
type Server struct {
	turn       *turn.Server
	packetConn net.PacketConn
	listener   net.Listener
	generator  *trackedGenerator
	runtime    Runtime
	outbox     UsageOutbox
	realm      string
	now        func() time.Time
	errors     chan error
	degraded   atomic.Bool
	mu         sync.Mutex
	pending    map[string]pendingReservation
	active     map[string]activeAllocation
}

type pendingReservation struct {
	id        string
	admission policy.RelayAdmission
	created   time.Time
}

type activeAllocation struct {
	id   string
	conn *trackedPacketConn
}

// Start 在同一端口启动 UDP/TCP STUN/TURN listener；公网域名可以与 gRPC 相同。
func Start(config Config) (*Server, error) {
	config.ListenAddress, config.PublicEndpoint, config.Realm = strings.TrimSpace(config.ListenAddress), strings.TrimSpace(config.PublicEndpoint), strings.TrimSpace(config.Realm)
	if config.ListenAddress == "" || config.PublicEndpoint == "" || config.Realm == "" || config.Runtime == nil || config.Outbox == nil {
		return nil, errors.New("TURN listener, public endpoint, realm, Runtime, and usage outbox are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	publicIP, err := resolvePublicIP(config.PublicEndpoint)
	if err != nil {
		return nil, err
	}
	listenHost, _, err := net.SplitHostPort(config.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("parse TURN listen address: %w", err)
	}
	packetConn, err := net.ListenPacket("udp", config.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen TURN UDP: %w", err)
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		_ = packetConn.Close()
		return nil, fmt.Errorf("listen TURN TCP: %w", err)
	}
	generator := newTrackedGenerator(publicIP, listenHost)
	server := &Server{
		packetConn: packetConn, listener: listener, generator: generator, runtime: config.Runtime, outbox: config.Outbox, realm: config.Realm, now: config.Now,
		errors: make(chan error, 1), pending: make(map[string]pendingReservation), active: make(map[string]activeAllocation),
	}
	turnServer, err := turn.NewServer(turn.ServerConfig{
		Realm: config.Realm,
		AuthHandler: func(username, realm string, _ net.Addr) ([]byte, bool) {
			if realm != server.realm || server.degraded.Load() {
				return nil, false
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, password, ok, authErr := server.runtime.RelayAuth(ctx, username, server.now().UTC())
			if authErr != nil || !ok {
				return nil, false
			}
			return turn.GenerateAuthKey(username, realm, password), true
		},
		QuotaHandler: func(username, realm string, source net.Addr) bool {
			return realm == server.realm && server.reserve(username, source)
		},
		EventHandler: turn.EventHandler{
			OnAllocationCreated: server.allocationCreated,
			OnAllocationDeleted: server.allocationDeleted,
		},
		PacketConnConfigs: []turn.PacketConnConfig{{PacketConn: packetConn, RelayAddressGenerator: generator}},
		ListenerConfigs:   []turn.ListenerConfig{{Listener: listener, RelayAddressGenerator: generator}},
	})
	if err != nil {
		_ = packetConn.Close()
		_ = listener.Close()
		return nil, fmt.Errorf("start Pion TURN: %w", err)
	}
	server.turn = turnServer
	return server, nil
}

// Address 返回实际绑定的 TURN UDP/TCP 共用地址。
func (server *Server) Address() string { return server.packetConn.LocalAddr().String() }

// Errors 报告 outbox 或 allocation lifecycle 的致命计费失败。
func (server *Server) Errors() <-chan error { return server.errors }

// Degraded 表示 Relay 已因 allocation/usage 失败停止接受新分配；同进程 P2P 不受影响。
func (server *Server) Degraded() bool { return server != nil && server.degraded.Load() }

// Close 关闭 TURN allocation 和 listener；删除 callback 会先冻结并持久化 usage。
func (server *Server) Close() error {
	if server == nil {
		return nil
	}
	if server.turn != nil {
		return server.turn.Close()
	}
	if server.packetConn != nil {
		_ = server.packetConn.Close()
	}
	if server.listener != nil {
		return server.listener.Close()
	}
	return nil
}

func (server *Server) reserve(username string, source net.Addr) bool {
	if server.degraded.Load() || source == nil {
		return false
	}
	reservationID := reservationKey(username, source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	admission, err := server.runtime.ReserveRelayAllocation(ctx, reservationID, username, server.now().UTC())
	if err != nil {
		return false
	}
	server.mu.Lock()
	created := server.now().UTC()
	server.pending[reservationID] = pendingReservation{id: reservationID, admission: admission, created: created}
	server.mu.Unlock()
	go server.expireReservation(reservationID, created, 10*time.Second)
	return true
}

func (server *Server) expireReservation(reservationID string, created time.Time, after time.Duration) {
	timer := time.NewTimer(after)
	defer timer.Stop()
	<-timer.C
	server.mu.Lock()
	reservation, exists := server.pending[reservationID]
	if exists && reservation.created.Equal(created) {
		delete(server.pending, reservationID)
	} else {
		exists = false
	}
	server.mu.Unlock()
	if exists {
		_ = server.runtime.CancelRelayAllocationReservation(context.Background(), reservation.id)
	}
}

func (server *Server) allocationCreated(source, destination net.Addr, protocol, username, _ string, relayAddress net.Addr, _ int) {
	reservationID := reservationKey(username, source)
	server.mu.Lock()
	reservation, reserved := server.pending[reservationID]
	delete(server.pending, reservationID)
	connection := server.generator.take(relayAddress)
	allocationID := uuid.NewString()
	key := allocationKey(source, destination, protocol)
	if reserved && connection != nil {
		server.active[key] = activeAllocation{id: allocationID, conn: connection}
	}
	server.mu.Unlock()
	if !reserved || connection == nil {
		server.fail(errors.New("TURN allocation callback has no Runtime reservation or relay socket"))
		return
	}
	connection.bind(reservation.admission)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.runtime.ActivateRelayAllocation(ctx, reservation.id, allocationID, relayTransport(protocol), server.now().UTC()); err != nil {
		server.mu.Lock()
		delete(server.active, key)
		server.mu.Unlock()
		_ = connection.Close()
		_ = server.runtime.CancelRelayAllocationReservation(context.Background(), reservation.id)
		server.fail(fmt.Errorf("activate Relay allocation: %w", err))
	}
}

func (server *Server) allocationDeleted(source, destination net.Addr, protocol, _, _ string) {
	key := allocationKey(source, destination, protocol)
	server.mu.Lock()
	allocation, exists := server.active[key]
	delete(server.active, key)
	server.mu.Unlock()
	if !exists {
		return
	}
	ingress, egress := allocation.conn.counts()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	event, err := server.runtime.CloseRelayAllocation(ctx, allocation.id, ingress, egress, server.now().UTC())
	if err == nil {
		err = server.outbox.Put(event)
	}
	if err != nil {
		server.fail(fmt.Errorf("settle Relay allocation: %w", err))
	}
}

func (server *Server) fail(err error) {
	server.degraded.Store(true)
	select {
	case server.errors <- err:
	default:
	}
}

func reservationKey(username string, source net.Addr) string {
	return strings.TrimSpace(username) + "|" + source.String()
}

func allocationKey(source, destination net.Addr, protocol string) string {
	return source.String() + "|" + destination.String() + "|" + strings.ToLower(strings.TrimSpace(protocol))
}

func relayTransport(protocol string) cloudv1.RelayTransport {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "tcp":
		return cloudv1.RelayTransport_RELAY_TRANSPORT_TCP
	case "tls":
		return cloudv1.RelayTransport_RELAY_TRANSPORT_TLS
	default:
		return cloudv1.RelayTransport_RELAY_TRANSPORT_UDP
	}
}

func resolvePublicIP(endpoint string) (net.IP, error) {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse TURN public endpoint: %w", err)
	}
	if parsed := net.ParseIP(strings.Trim(host, "[]")); parsed != nil {
		return parsed, nil
	}
	addresses, err := net.LookupIP(strings.Trim(host, "[]"))
	if err != nil {
		return nil, fmt.Errorf("resolve TURN public endpoint: %w", err)
	}
	for _, address := range addresses {
		if ipv4 := address.To4(); ipv4 != nil {
			return ipv4, nil
		}
	}
	return nil, errors.New("TURN public endpoint has no IPv4 address")
}

type trackedGenerator struct {
	base  *turn.RelayAddressGeneratorStatic
	mu    sync.Mutex
	conns map[string]*trackedPacketConn
}

func newTrackedGenerator(publicIP net.IP, listenHost string) *trackedGenerator {
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}
	return &trackedGenerator{base: &turn.RelayAddressGeneratorStatic{
		RelayAddress: publicIP, Address: listenHost,
		// TURN allocation 只需要标准 UDP socket；跳过接口枚举，保持 Edge systemd 无需开放 AF_NETLINK。
		Net: &stdnet.Net{},
	}, conns: make(map[string]*trackedPacketConn)}
}

func (generator *trackedGenerator) Validate() error { return generator.base.Validate() }

func (generator *trackedGenerator) AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error) {
	connection, address, err := generator.base.AllocatePacketConn(network, requestedPort)
	if err != nil {
		return nil, nil, err
	}
	key := address.String()
	tracked := &trackedPacketConn{PacketConn: connection}
	tracked.onClose = func() {
		generator.mu.Lock()
		if generator.conns[key] == tracked {
			delete(generator.conns, key)
		}
		generator.mu.Unlock()
	}
	generator.mu.Lock()
	generator.conns[key] = tracked
	generator.mu.Unlock()
	return tracked, address, nil
}

func (generator *trackedGenerator) AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error) {
	return generator.base.AllocateConn(network, requestedPort)
}

func (generator *trackedGenerator) take(address net.Addr) *trackedPacketConn {
	if address == nil {
		return nil
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	connection := generator.conns[address.String()]
	delete(generator.conns, address.String())
	return connection
}

type trackedPacketConn struct {
	net.PacketConn
	mu       sync.Mutex
	limiter  *policy.AdmissionLimiter
	ingress  uint64
	egress   uint64
	onClose  func()
	close    sync.Once
	closeErr error
}

// Close 同时从 generator correlation 中移除 socket，避免 allocation 失败留下内存引用。
func (connection *trackedPacketConn) Close() error {
	connection.close.Do(func() {
		if connection.onClose != nil {
			connection.onClose()
		}
		connection.closeErr = connection.PacketConn.Close()
	})
	return connection.closeErr
}

func (connection *trackedPacketConn) bind(admission policy.RelayAdmission) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.limiter = admission.Limiter
}

func (connection *trackedPacketConn) ReadFrom(payload []byte) (int, net.Addr, error) {
	count, address, err := connection.PacketConn.ReadFrom(payload)
	if err != nil {
		return count, address, err
	}
	if !connection.allow(uint64(count), true, time.Now()) {
		return 0, address, errors.New("Relay ingress exceeded lease byte or rate limit")
	}
	return count, address, nil
}

func (connection *trackedPacketConn) WriteTo(payload []byte, address net.Addr) (int, error) {
	requested := uint64(len(payload))
	connection.mu.Lock()
	limiter := connection.limiter
	connection.mu.Unlock()
	if limiter == nil || !limiter.Reserve(requested, time.Now()) {
		return 0, errors.New("Relay egress exceeded lease byte or rate limit")
	}
	written, err := connection.PacketConn.WriteTo(payload, address)
	actual := uint64(written)
	if actual < requested {
		limiter.Refund(requested - actual)
	}
	connection.mu.Lock()
	connection.egress += actual
	connection.mu.Unlock()
	return written, err
}

func (connection *trackedPacketConn) allow(count uint64, ingress bool, now time.Time) bool {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.limiter == nil || !connection.limiter.Reserve(count, now) {
		return false
	}
	if ingress {
		connection.ingress += count
	} else {
		connection.egress += count
	}
	return true
}

func (connection *trackedPacketConn) counts() (uint64, uint64) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.ingress, connection.egress
}
