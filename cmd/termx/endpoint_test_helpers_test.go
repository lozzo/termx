package main

import (
	"os"
	"testing"

	"github.com/lozzow/termx/shared/connection"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "termx-cmd-tests-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", root+"/config")
	_ = os.Setenv("XDG_STATE_HOME", root+"/state")
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func testLocalEndpoint(id connection.EndpointID, label, socket string, mode connection.ConnectMode, enabled bool) connection.Endpoint {
	endpoint := connection.NewLocalEndpoint(id, label, socket, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func testSSHEndpoint(id connection.EndpointID, label, host, credentialRef, remoteSocket string, mode connection.ConnectMode, enabled bool) connection.Endpoint {
	endpoint := connection.NewSSHEndpoint(id, label, host, credentialRef, remoteSocket, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func testManagedEndpoint(id connection.EndpointID, label, deviceID, fingerprint, credentialRef string, relayMode connection.RelayMode, mode connection.ConnectMode, enabled bool) connection.Endpoint {
	endpoint := connection.NewManagedEndpoint(id, label, connection.DaemonIdentity{DeviceID: deviceID, DeviceFingerprint: fingerprint}, deviceID, credentialRef, relayMode, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func testOnlyRoute(endpoint connection.Endpoint) connection.AccessRoute {
	for _, route := range endpoint.Routes {
		return route
	}
	return connection.AccessRoute{}
}
