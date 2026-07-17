package main

import (
	"os"
	"testing"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
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

func testLocalEndpoint(id endpointdomain.EndpointID, label, socket string, mode endpointdomain.ConnectMode, enabled bool) endpointdomain.Endpoint {
	endpoint := endpointdomain.NewLocalEndpoint(id, label, socket, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func testSSHEndpoint(id endpointdomain.EndpointID, label, host, credentialRef, remoteSocket string, mode endpointdomain.ConnectMode, enabled bool) endpointdomain.Endpoint {
	endpoint := endpointdomain.NewSSHEndpoint(id, label, host, credentialRef, remoteSocket, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func testManagedEndpoint(id endpointdomain.EndpointID, label, deviceID, fingerprint, credentialRef string, relayMode endpointdomain.RelayMode, mode endpointdomain.ConnectMode, enabled bool) endpointdomain.Endpoint {
	endpoint := endpointdomain.NewManagedEndpoint(id, label, endpointdomain.DaemonIdentity{DeviceID: deviceID, DeviceFingerprint: fingerprint}, deviceID, credentialRef, relayMode, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func testOnlyRoute(endpoint endpointdomain.Endpoint) endpointdomain.AccessRoute {
	for _, route := range endpoint.Routes {
		return route
	}
	return endpointdomain.AccessRoute{}
}
