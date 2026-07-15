package services

import "github.com/lozzow/termx/shared/connection"

func serviceTestLocalEndpoint(id connection.EndpointID, label, socket string, mode connection.ConnectMode, enabled bool) connection.Endpoint {
	endpoint := connection.NewLocalEndpoint(id, label, socket, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func serviceTestSSHEndpoint(id connection.EndpointID, label, host, credentialRef, remoteSocket string, mode connection.ConnectMode, enabled bool) connection.Endpoint {
	endpoint := connection.NewSSHEndpoint(id, label, host, credentialRef, remoteSocket, mode)
	endpoint.Enabled = enabled
	return endpoint
}

func serviceTestManagedEndpoint(id connection.EndpointID, label, deviceID, fingerprint, credentialRef string, relayMode connection.RelayMode, mode connection.ConnectMode, enabled bool) connection.Endpoint {
	endpoint := connection.NewManagedEndpoint(id, label, connection.DaemonIdentity{DeviceID: deviceID, DeviceFingerprint: fingerprint}, deviceID, credentialRef, relayMode, mode)
	endpoint.Enabled = enabled
	return endpoint
}
