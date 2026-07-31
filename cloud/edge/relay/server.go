// Package relay 把 Pion STUN/TURN 数据面装配进同一个 Edge 进程。
// 本包只转发字节并执行 Controller grant 的本地上限，不解析 terminal 或 DataChannel payload。
package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"github.com/pion/transport/v4/stdnet"
	turn "github.com/pion/turn/v4"
)

// Runtime 是 TURN callback 提交到 Edge 唯一 State actor 的窄边界。
type Runtime interface {
	RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayGrant, string, bool, error)
	ReserveRelayAllocation(context.Context, string, string, time.Time) (policy.RelayAdmission, error)
	ActivateRelayAllocation(context.Context, string, string, cloudv1.RelayTransport, time.Time) error
	CancelRelayAllocationReservation(context.Context, string) error
	BeginRelayAllocationClose(context.Context, string) error
	CloseRelayAllocation(context.Context, string, uint64, uint64) error
}

// Config 提供由部署生成的 TURN listener/public endpoint、realm 和 Runtime。
type Config struct {
	ListenAddress  string
	PublicEndpoint string
	Realm          string
	Runtime        Runtime
	Now            func() time.Time
}

// Server 拥有 Edge 内置 Pion TURN server、relay socket 计数器和 callback correlation。
type Server struct {
	turn              *turn.Server
	packetConn        net.PacketConn
	listener          net.Listener
	generator         *trackedGenerator
	runtime           Runtime
	realm             string
	now               func() time.Time
	errors            chan error
	degraded          atomic.Bool
	mu                sync.Mutex
	closing           bool
	closed            chan struct{}
	work              sync.WaitGroup
	closeOnce         sync.Once
	closeMu           sync.Mutex
	closeErr          error
	settlementTimeout time.Duration
	pending           map[string]pendingReservation
	active            map[string]activeAllocation
	callbackFIFO      map[string][]string
}

type pendingReservation struct {
	id        string
	admission policy.RelayAdmission
	created   time.Time
}

type activeAllocation struct {
	id        string
	key       string
	sessionID string
	conn      *trackedPacketConn
	settling  bool
}

type claimedAllocation struct {
	allocation activeAllocation
}

// Start 在同一端口启动 UDP/TCP STUN/TURN listener；公网域名可以与 gRPC 相同。
func Start(config Config) (*Server, error) {
	config.ListenAddress, config.PublicEndpoint, config.Realm = strings.TrimSpace(config.ListenAddress), strings.TrimSpace(config.PublicEndpoint), strings.TrimSpace(config.Realm)
	if config.ListenAddress == "" || config.PublicEndpoint == "" || config.Realm == "" || config.Runtime == nil {
		return nil, errors.New("TURN listener, public endpoint, realm, and Runtime are required")
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
		packetConn: packetConn, listener: listener, generator: generator, runtime: config.Runtime, realm: config.Realm, now: config.Now,
		errors: make(chan error, 1), closed: make(chan struct{}), pending: make(map[string]pendingReservation), active: make(map[string]activeAllocation), callbackFIFO: make(map[string][]string),
	}
	turnServer, err := turn.NewServer(turn.ServerConfig{
		Realm: config.Realm,
		AuthHandler: func(username, realm string, _ net.Addr) ([]byte, bool) {
			return server.authenticate(username, realm)
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

// Errors 报告 allocation lifecycle 的致命失败。
func (server *Server) Errors() <-chan error { return server.errors }

// Degraded 表示 Relay 已因 allocation lifecycle 失败停止接受新分配；同进程 P2P 不受影响。
func (server *Server) Degraded() bool { return server != nil && server.degraded.Load() }

// Close 关闭 TURN allocation 和 listener，并在 socket 静止后累加 group counters。
func (server *Server) Close() error {
	if server == nil {
		return nil
	}
	server.closeOnce.Do(func() { server.closeErr = server.stop() })
	server.closeMu.Lock()
	defer server.closeMu.Unlock()
	err := errors.Join(server.closeErr, server.drain())
	if server.degraded.Load() {
		err = errors.Join(err, errors.New("Relay is degraded during shutdown"))
	}
	return err
}

func (server *Server) stop() error {
	server.mu.Lock()
	server.closing = true
	if server.closed != nil {
		close(server.closed)
	}
	server.mu.Unlock()

	var stopErrors []error
	if server.turn != nil {
		if err := server.turn.Close(); err != nil {
			stopErrors = append(stopErrors, err)
		}
	} else {
		if server.packetConn != nil {
			if err := server.packetConn.Close(); err != nil {
				stopErrors = append(stopErrors, err)
			}
		}
		if server.listener != nil {
			if err := server.listener.Close(); err != nil {
				stopErrors = append(stopErrors, err)
			}
		}
	}

	server.work.Wait()
	return errors.Join(stopErrors...)
}

func (server *Server) drain() error {
	var drainErrors []error
	server.mu.Lock()
	pending := make([]pendingReservation, 0, len(server.pending))
	for key, reservation := range server.pending {
		pending = append(pending, reservation)
		delete(server.pending, key)
	}
	server.mu.Unlock()
	active := server.claimAllocations(func(activeAllocation) bool { return true })

	for _, reservation := range pending {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := server.runtime.CancelRelayAllocationReservation(ctx, reservation.id)
		cancel()
		if err != nil {
			drainErrors = append(drainErrors, fmt.Errorf("cancel Relay reservation %s during shutdown: %w", reservation.id, err))
		}
	}
	for _, claimed := range active {
		allocation := claimed.allocation
		ctx, cancel := context.WithTimeout(context.Background(), server.allocationSettlementTimeout())
		err := server.settleClaimedAllocation(ctx, claimed)
		cancel()
		if err != nil {
			settlementErr := fmt.Errorf("settle Relay allocation %s during shutdown: %w", allocation.id, err)
			drainErrors = append(drainErrors, settlementErr)
			server.fail(settlementErr)
		}
	}
	return errors.Join(drainErrors...)
}

// StateCloseSafe reports whether no pending or active allocation remains in the data plane.
func (server *Server) StateCloseSafe() bool {
	if server == nil {
		return true
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	return len(server.pending) == 0 && len(server.active) == 0
}

// CloseSessionAllocations 释放 ClientGateway session 申请中的 reservation 和已激活 allocation。
// TURN Refresh(lifetime=0) 仍由标准客户端路径处理；本方法保证客户端进程突然退出时，
// Edge 的并发配额和 usage truth 不必等待 TURN allocation 自然超时。
func (server *Server) CloseSessionAllocations(ctx context.Context, sessionID string) error {
	if server == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if ctx == nil || sessionID == "" {
		return errors.New("Relay session cleanup requires context and session ID")
	}
	if !server.beginWork(false) {
		return nil
	}
	defer server.work.Done()
	server.mu.Lock()
	pending := make([]pendingReservation, 0)
	for key, reservation := range server.pending {
		if reservation.admission.SessionID == sessionID {
			pending = append(pending, reservation)
			delete(server.pending, key)
		}
	}
	server.mu.Unlock()
	active := server.claimAllocations(func(allocation activeAllocation) bool { return allocation.sessionID == sessionID })

	var cleanupErrors []error
	recordFailure := func(err error) {
		cleanupErrors = append(cleanupErrors, err)
		server.fail(fmt.Errorf("clean up Relay session %s: %w", sessionID, err))
	}
	for _, reservation := range pending {
		if err := server.runtime.CancelRelayAllocationReservation(ctx, reservation.id); err != nil {
			recordFailure(fmt.Errorf("cancel Relay reservation %s: %w", reservation.id, err))
		}
	}
	for _, claimed := range active {
		if err := server.settleClaimedAllocation(ctx, claimed); err != nil {
			recordFailure(err)
		}
	}
	return errors.Join(cleanupErrors...)
}

func (server *Server) reserve(username string, source net.Addr) bool {
	if source == nil || !server.beginWork(true) {
		return false
	}
	defer server.work.Done()
	reservationID := reservationKey(username, source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	admission, err := server.runtime.ReserveRelayAllocation(ctx, reservationID, username, server.now().UTC())
	if err != nil {
		return false
	}
	server.mu.Lock()
	if server.closing || server.degraded.Load() {
		server.mu.Unlock()
		_ = server.runtime.CancelRelayAllocationReservation(context.Background(), reservationID)
		return false
	}
	created := server.now().UTC()
	server.pending[reservationID] = pendingReservation{id: reservationID, admission: admission, created: created}
	server.mu.Unlock()
	go server.expireReservation(reservationID, created, 10*time.Second)
	return true
}

func (server *Server) expireReservation(reservationID string, created time.Time, after time.Duration) {
	timer := time.NewTimer(after)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-server.closed:
		return
	}
	if !server.beginWork(false) {
		return
	}
	defer server.work.Done()
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
	if !server.beginWork(false) {
		return
	}
	defer server.work.Done()
	reservationID := reservationKey(username, source)
	server.mu.Lock()
	reservation, reserved := server.pending[reservationID]
	delete(server.pending, reservationID)
	connection := server.generator.take(relayAddress)
	allocationID := uuid.NewString()
	key := allocationKey(source, destination, protocol)
	if !reserved || connection == nil {
		server.degraded.Store(true)
		server.mu.Unlock()
		if connection != nil {
			_ = connection.Close()
		}
		server.reportFailure(errors.New("TURN allocation callback has no Runtime reservation or relay socket"))
		return
	}
	if server.closing || server.degraded.Load() {
		server.mu.Unlock()
		_ = connection.Close()
		_ = server.runtime.CancelRelayAllocationReservation(context.Background(), reservation.id)
		return
	}
	connection.bind(reservation.admission)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := server.runtime.ActivateRelayAllocation(ctx, reservation.id, allocationID, relayTransport(protocol), server.now().UTC())
	cancel()
	if err == nil {
		server.active[allocationID] = activeAllocation{id: allocationID, key: key, sessionID: reservation.admission.SessionID, conn: connection}
		if server.callbackFIFO == nil {
			server.callbackFIFO = make(map[string][]string)
		}
		server.callbackFIFO[key] = append(server.callbackFIFO[key], allocationID)
	} else {
		server.degraded.Store(true)
	}
	server.mu.Unlock()
	if err != nil {
		_ = connection.Close()
		_ = server.runtime.CancelRelayAllocationReservation(context.Background(), reservation.id)
		server.reportFailure(fmt.Errorf("activate Relay allocation: %w", err))
	}
}

func (server *Server) allocationDeleted(source, destination net.Addr, protocol, _, _ string) {
	if !server.beginWork(false) {
		return
	}
	defer server.work.Done()
	key := allocationKey(source, destination, protocol)
	claimed, exists := server.claimDeletedAllocation(key)
	if !exists {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), server.allocationSettlementTimeout())
	defer cancel()
	if err := server.settleClaimedAllocation(ctx, claimed); err != nil {
		server.fail(fmt.Errorf("settle Relay allocation: %w", err))
	}
}

func (server *Server) settleClaimedAllocation(ctx context.Context, claimed claimedAllocation) error {
	allocation := claimed.allocation
	if allocation.conn == nil {
		server.releaseAllocationClaim(claimed)
		return fmt.Errorf("close Relay allocation %s: missing relay socket", allocation.id)
	}
	if err := server.runtime.BeginRelayAllocationClose(ctx, allocation.id); err != nil {
		server.releaseAllocationClaim(claimed)
		return fmt.Errorf("begin Relay allocation %s close: %w", allocation.id, err)
	}
	if err := allocation.conn.Close(); err != nil {
		server.releaseAllocationClaim(claimed)
		return fmt.Errorf("close Relay allocation %s socket: %w", allocation.id, err)
	}
	ingress, egress := allocation.conn.counts()
	if err := server.runtime.CloseRelayAllocation(ctx, allocation.id, ingress, egress); err != nil {
		server.releaseAllocationClaim(claimed)
		return fmt.Errorf("record Relay allocation %s counters: %w", allocation.id, err)
	}
	server.mu.Lock()
	current, exists := server.active[allocation.id]
	if !exists || !current.settling {
		server.mu.Unlock()
		return fmt.Errorf("Relay allocation %s lost its active ownership", allocation.id)
	}
	delete(server.active, allocation.id)
	server.mu.Unlock()
	return nil
}

func (server *Server) claimDeletedAllocation(key string) (claimedAllocation, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	queue := server.callbackFIFO[key]
	if len(queue) == 0 {
		return claimedAllocation{}, false
	}
	allocationID := queue[0]
	if len(queue) == 1 {
		delete(server.callbackFIFO, key)
	} else {
		server.callbackFIFO[key] = queue[1:]
	}
	allocation, exists := server.active[allocationID]
	if !exists || allocation.settling {
		return claimedAllocation{}, false
	}
	allocation.settling = true
	server.active[allocationID] = allocation
	return claimedAllocation{allocation: allocation}, true
}

func (server *Server) claimAllocations(match func(activeAllocation) bool) []claimedAllocation {
	server.mu.Lock()
	defer server.mu.Unlock()
	allocationIDs := make([]string, 0, len(server.active))
	for allocationID, allocation := range server.active {
		if !allocation.settling && match(allocation) {
			allocationIDs = append(allocationIDs, allocationID)
		}
	}
	sort.Strings(allocationIDs)
	claimed := make([]claimedAllocation, 0, len(allocationIDs))
	for _, allocationID := range allocationIDs {
		allocation := server.active[allocationID]
		allocation.settling = true
		server.active[allocationID] = allocation
		claimed = append(claimed, claimedAllocation{allocation: allocation})
	}
	return claimed
}

func (server *Server) releaseAllocationClaim(claimed claimedAllocation) {
	server.mu.Lock()
	defer server.mu.Unlock()
	allocation, exists := server.active[claimed.allocation.id]
	if exists && allocation.id == claimed.allocation.id && allocation.settling {
		allocation.settling = false
		server.active[claimed.allocation.id] = allocation
	}
}

func (server *Server) allocationSettlementTimeout() time.Duration {
	if server.settlementTimeout > 0 {
		return server.settlementTimeout
	}
	return 2 * time.Second
}

func (server *Server) fail(err error) {
	server.mu.Lock()
	server.degraded.Store(true)
	server.mu.Unlock()
	server.reportFailure(err)
}

func (server *Server) reportFailure(err error) {
	select {
	case server.errors <- err:
	default:
	}
}

func (server *Server) authenticate(username, realm string) ([]byte, bool) {
	if realm != server.realm || !server.beginWork(true) {
		return nil, false
	}
	defer server.work.Done()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, password, ok, err := server.runtime.RelayAuth(ctx, username, server.now().UTC())
	if err != nil || !ok {
		return nil, false
	}
	server.mu.Lock()
	available := !server.closing && !server.degraded.Load()
	server.mu.Unlock()
	if !available {
		return nil, false
	}
	return turn.GenerateAuthKey(username, realm, password), true
}

func (server *Server) beginWork(admission bool) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing || admission && server.degraded.Load() {
		return false
	}
	server.work.Add(1)
	return true
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
	limiter  *policy.GroupLimiter
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
		return 0, address, errors.New("Relay ingress exceeded grant byte or rate limit")
	}
	return count, address, nil
}

func (connection *trackedPacketConn) WriteTo(payload []byte, address net.Addr) (int, error) {
	requested := uint64(len(payload))
	connection.mu.Lock()
	limiter := connection.limiter
	connection.mu.Unlock()
	if limiter == nil || !limiter.Reserve(requested, time.Now()) {
		return 0, errors.New("Relay egress exceeded grant byte or rate limit")
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
