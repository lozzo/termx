package main

import (
	"io"
	"net"
	"sync"
)

var hub007FaultProxies sync.Map

type controlFaultProxy struct {
	listener net.Listener
	target   string

	mu          sync.Mutex
	blocked     bool
	connections map[net.Conn]struct{}
}

func newControlFaultProxy(target string) (*controlFaultProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	proxy := &controlFaultProxy{listener: listener, target: target, connections: make(map[net.Conn]struct{})}
	go proxy.serve()
	return proxy, nil
}

func (proxy *controlFaultProxy) URL() string { return "http://" + proxy.listener.Addr().String() }

func (proxy *controlFaultProxy) SetBlocked(blocked bool) {
	proxy.mu.Lock()
	proxy.blocked = blocked
	if blocked {
		for connection := range proxy.connections {
			_ = connection.Close()
		}
	}
	proxy.mu.Unlock()
}

func (proxy *controlFaultProxy) Close() error {
	proxy.SetBlocked(true)
	return proxy.listener.Close()
}

func (proxy *controlFaultProxy) serve() {
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		go proxy.forward(client)
	}
}

func (proxy *controlFaultProxy) forward(client net.Conn) {
	proxy.mu.Lock()
	if proxy.blocked {
		proxy.mu.Unlock()
		_ = client.Close()
		return
	}
	upstream, err := net.Dial("tcp", proxy.target)
	if err != nil {
		proxy.mu.Unlock()
		_ = client.Close()
		return
	}
	proxy.connections[client] = struct{}{}
	proxy.connections[upstream] = struct{}{}
	proxy.mu.Unlock()
	defer func() {
		_ = client.Close()
		_ = upstream.Close()
		proxy.mu.Lock()
		delete(proxy.connections, client)
		delete(proxy.connections, upstream)
		proxy.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}
