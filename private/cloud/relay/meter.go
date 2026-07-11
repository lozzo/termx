package relay

import (
	"net"
	"sync"

	pionturn "github.com/pion/turn/v4"
)

type relayGenerator interface {
	Validate() error
	AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error)
	AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error)
}

type allocationBinder interface {
	setAllocation(string)
	closeForQuota()
}

type meteredRelayGenerator struct {
	base      relayGenerator
	authority *Authority
	mu        sync.Mutex
	relays    map[string]allocationBinder
	byAlloc   map[string]string
}

func newMeteredRelayGenerator(base pionturn.RelayAddressGenerator, authority *Authority) *meteredRelayGenerator {
	return &meteredRelayGenerator{base: base, authority: authority, relays: make(map[string]allocationBinder), byAlloc: make(map[string]string)}
}

func (generator *meteredRelayGenerator) Validate() error { return generator.base.Validate() }

func (generator *meteredRelayGenerator) AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error) {
	connection, address, err := generator.base.AllocatePacketConn(network, requestedPort)
	if err != nil || connection == nil || address == nil {
		return connection, address, err
	}
	wrapped := &meteredPacketConn{PacketConn: connection, authority: generator.authority}
	generator.mu.Lock()
	generator.relays[address.String()] = wrapped
	generator.mu.Unlock()
	return wrapped, address, nil
}

func (generator *meteredRelayGenerator) AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error) {
	connection, address, err := generator.base.AllocateConn(network, requestedPort)
	if err != nil || connection == nil || address == nil {
		return connection, address, err
	}
	wrapped := &meteredConn{Conn: connection, authority: generator.authority}
	generator.mu.Lock()
	generator.relays[address.String()] = wrapped
	generator.mu.Unlock()
	return wrapped, address, nil
}

func (generator *meteredRelayGenerator) bind(relayAddr net.Addr, allocationID string) {
	if relayAddr == nil || allocationID == "" {
		return
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	binder := generator.relays[relayAddr.String()]
	if binder == nil {
		return
	}
	binder.setAllocation(allocationID)
	generator.byAlloc[allocationID] = relayAddr.String()
}

func (generator *meteredRelayGenerator) forget(allocationID string) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	relayAddress := generator.byAlloc[allocationID]
	delete(generator.byAlloc, allocationID)
	if relayAddress != "" {
		delete(generator.relays, relayAddress)
	}
}

type meteredPacketConn struct {
	net.PacketConn
	authority  *Authority
	mu         sync.RWMutex
	allocation string
	closeOnce  sync.Once
}

func (connection *meteredPacketConn) setAllocation(allocationID string) {
	connection.mu.Lock()
	connection.allocation = allocationID
	connection.mu.Unlock()
}

func (connection *meteredPacketConn) allocationID() string {
	connection.mu.RLock()
	defer connection.mu.RUnlock()
	return connection.allocation
}

func (connection *meteredPacketConn) closeForQuota() {
	connection.closeOnce.Do(func() { _ = connection.PacketConn.Close() })
}

func (connection *meteredPacketConn) ReadFrom(buffer []byte) (int, net.Addr, error) {
	count, address, err := connection.PacketConn.ReadFrom(buffer)
	if err == nil && count > 0 {
		if meterErr := connection.authority.RecordTraffic(connection.allocationID(), 0, uint64(count)); meterErr != nil {
			connection.closeForQuota()
			return count, address, errRelayQuota
		}
	}
	return count, address, err
}

func (connection *meteredPacketConn) WriteTo(buffer []byte, address net.Addr) (int, error) {
	count, err := connection.PacketConn.WriteTo(buffer, address)
	if err == nil && count > 0 {
		if meterErr := connection.authority.RecordTraffic(connection.allocationID(), uint64(count), 0); meterErr != nil {
			connection.closeForQuota()
			return count, errRelayQuota
		}
	}
	return count, err
}

type meteredConn struct {
	net.Conn
	authority  *Authority
	mu         sync.RWMutex
	allocation string
	closeOnce  sync.Once
}

func (connection *meteredConn) setAllocation(allocationID string) {
	connection.mu.Lock()
	connection.allocation = allocationID
	connection.mu.Unlock()
}

func (connection *meteredConn) allocationID() string {
	connection.mu.RLock()
	defer connection.mu.RUnlock()
	return connection.allocation
}

func (connection *meteredConn) closeForQuota() {
	connection.closeOnce.Do(func() { _ = connection.Conn.Close() })
}

func (connection *meteredConn) Read(buffer []byte) (int, error) {
	count, err := connection.Conn.Read(buffer)
	if err == nil && count > 0 {
		if meterErr := connection.authority.RecordTraffic(connection.allocationID(), 0, uint64(count)); meterErr != nil {
			connection.closeForQuota()
			return count, errRelayQuota
		}
	}
	return count, err
}

func (connection *meteredConn) Write(buffer []byte) (int, error) {
	count, err := connection.Conn.Write(buffer)
	if err == nil && count > 0 {
		if meterErr := connection.authority.RecordTraffic(connection.allocationID(), uint64(count), 0); meterErr != nil {
			connection.closeForQuota()
			return count, errRelayQuota
		}
	}
	return count, err
}
