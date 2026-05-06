package turn

import (
	"net"
	"sort"
	"strings"
	"sync"
)

type TrafficDelta struct {
	AgentID  string `json:"agent_id"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

type TrafficReader interface {
	DrainTraffic() []TrafficDelta
}

type TrafficMeter struct {
	mu     sync.Mutex
	totals map[string]TrafficDelta
}

func NewTrafficMeter() *TrafficMeter {
	return &TrafficMeter{totals: make(map[string]TrafficDelta)}
}

func (m *TrafficMeter) Add(agentID string, bytesIn, bytesOut int64) {
	if m == nil {
		return
	}
	agentID = trafficAgentID(agentID)
	if agentID == "" || (bytesIn <= 0 && bytesOut <= 0) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delta := m.totals[agentID]
	delta.AgentID = agentID
	if bytesIn > 0 {
		delta.BytesIn += bytesIn
	}
	if bytesOut > 0 {
		delta.BytesOut += bytesOut
	}
	m.totals[agentID] = delta
}

func (m *TrafficMeter) DrainTraffic() []TrafficDelta {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.totals) == 0 {
		return nil
	}
	out := make([]TrafficDelta, 0, len(m.totals))
	for _, delta := range m.totals {
		if delta.AgentID == "" || (delta.BytesIn <= 0 && delta.BytesOut <= 0) {
			continue
		}
		out = append(out, delta)
	}
	m.totals = make(map[string]TrafficDelta)
	sort.Slice(out, func(i, j int) bool {
		return out[i].AgentID < out[j].AgentID
	})
	return out
}

type meteredRelayAddressGenerator struct {
	base        relayAddressGeneratorInterface
	meter       *TrafficMeter
	mu          sync.Mutex
	relays      map[string]trafficAgentSetter
	allocations map[string]string
}

type trafficAgentSetter interface {
	setAgent(agentID string)
}

func newMeteredRelayAddressGenerator(base relayAddressGeneratorInterface, meter *TrafficMeter) *meteredRelayAddressGenerator {
	return &meteredRelayAddressGenerator{
		base:        base,
		meter:       meter,
		relays:      make(map[string]trafficAgentSetter),
		allocations: make(map[string]string),
	}
}

type relayAddressGeneratorInterface interface {
	Validate() error
	AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error)
	AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error)
}

func (g *meteredRelayAddressGenerator) Validate() error {
	return g.base.Validate()
}

func (g *meteredRelayAddressGenerator) AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error) {
	conn, addr, err := g.base.AllocatePacketConn(network, requestedPort)
	if err != nil || conn == nil {
		return conn, addr, err
	}
	wrapped := &meteredPacketConn{PacketConn: conn, meter: g.meter}
	g.remember(addr, wrapped)
	return wrapped, addr, nil
}

func (g *meteredRelayAddressGenerator) AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error) {
	conn, addr, err := g.base.AllocateConn(network, requestedPort)
	if err != nil || conn == nil {
		return conn, addr, err
	}
	wrapped := &meteredConn{Conn: conn, meter: g.meter}
	g.remember(addr, wrapped)
	return wrapped, addr, nil
}

func (g *meteredRelayAddressGenerator) remember(addr net.Addr, relay trafficAgentSetter) {
	if g == nil || addr == nil || relay == nil {
		return
	}
	g.mu.Lock()
	g.relays[addr.String()] = relay
	g.mu.Unlock()
}

func (g *meteredRelayAddressGenerator) bindAgent(relayAddr net.Addr, username string) {
	if g == nil || relayAddr == nil {
		return
	}
	agentID := trafficAgentID(username)
	if agentID == "" {
		return
	}
	g.mu.Lock()
	relay := g.relays[relayAddr.String()]
	g.mu.Unlock()
	if relay != nil {
		relay.setAgent(agentID)
	}
}

func (g *meteredRelayAddressGenerator) rememberAllocation(srcAddr, dstAddr net.Addr, protocol string, relayAddr net.Addr) {
	if g == nil || relayAddr == nil {
		return
	}
	key := allocationKey(srcAddr, dstAddr, protocol)
	if key == "" {
		return
	}
	g.mu.Lock()
	g.allocations[key] = relayAddr.String()
	g.mu.Unlock()
}

func (g *meteredRelayAddressGenerator) forgetAllocation(srcAddr, dstAddr net.Addr, protocol string) {
	if g == nil {
		return
	}
	key := allocationKey(srcAddr, dstAddr, protocol)
	if key == "" {
		return
	}
	g.mu.Lock()
	if relayAddr, ok := g.allocations[key]; ok {
		delete(g.relays, relayAddr)
		delete(g.allocations, key)
	}
	g.mu.Unlock()
}

type meteredPacketConn struct {
	net.PacketConn
	meter  *TrafficMeter
	agent  string
	agentM sync.RWMutex
}

func (c *meteredPacketConn) setAgent(agentID string) {
	c.agentM.Lock()
	c.agent = trafficAgentID(agentID)
	c.agentM.Unlock()
}

func (c *meteredPacketConn) agentID() string {
	c.agentM.RLock()
	defer c.agentM.RUnlock()
	return c.agent
}

func (c *meteredPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if err == nil && n > 0 {
		c.meter.Add(c.agentID(), int64(n), 0)
	}
	return n, addr, err
}

func (c *meteredPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if n > 0 {
		c.meter.Add(c.agentID(), 0, int64(n))
	}
	return n, err
}

type meteredConn struct {
	net.Conn
	meter  *TrafficMeter
	agent  string
	agentM sync.RWMutex
}

func (c *meteredConn) setAgent(agentID string) {
	c.agentM.Lock()
	c.agent = trafficAgentID(agentID)
	c.agentM.Unlock()
}

func (c *meteredConn) agentID() string {
	c.agentM.RLock()
	defer c.agentM.RUnlock()
	return c.agent
}

func (c *meteredConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err == nil && n > 0 {
		c.meter.Add(c.agentID(), int64(n), 0)
	}
	return n, err
}

func (c *meteredConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.meter.Add(c.agentID(), 0, int64(n))
	}
	return n, err
}

func trafficAgentID(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	if agentID, _, ok := strings.Cut(username, ":"); ok {
		return strings.TrimSpace(agentID)
	}
	return username
}

func allocationKey(srcAddr, dstAddr net.Addr, protocol string) string {
	protocol = strings.TrimSpace(protocol)
	if srcAddr == nil || dstAddr == nil || protocol == "" {
		return ""
	}
	return protocol + "|" + srcAddr.String() + "|" + dstAddr.String()
}
