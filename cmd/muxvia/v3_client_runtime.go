package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	localadapter "github.com/muxvia/muxvia/client/adapter/local"
	managedadapter "github.com/muxvia/muxvia/client/adapter/managed"
	pionadapter "github.com/muxvia/muxvia/client/adapter/managed/pion"
	peeradapter "github.com/muxvia/muxvia/client/adapter/peer"
	protocoladapter "github.com/muxvia/muxvia/client/adapter/protocol"
	sshadapter "github.com/muxvia/muxvia/client/adapter/ssh"
	systemadapter "github.com/muxvia/muxvia/client/adapter/system"
	clientendpoint "github.com/muxvia/muxvia/client/endpoint"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/remoteauth"
)

type cliEndpointPlanSource struct {
	snapshot clientruntime.EndpointPlanSnapshot
}

// Snapshot 返回当前 CLI invocation 冻结的单 Endpoint planner 输入；请求其他 Endpoint 必须 fail closed。
func (source cliEndpointPlanSource) Snapshot(_ context.Context, endpointID clientendpoint.EndpointID) (clientruntime.EndpointPlanSnapshot, error) {
	if source.snapshot.Endpoint.ID != endpointID {
		return clientruntime.EndpointPlanSnapshot{}, &clientruntime.Error{Code: clientruntime.ErrorNotFound, Message: fmt.Sprintf("endpoint %q is not configured", endpointID)}
	}
	return source.snapshot, nil
}

type cliCredentialSource struct {
	store *remoteauth.CredentialStore
}

// ResolveClientCredential 从 desktop owner-only store 读取 endpoint-bound grant 与私钥，不访问 Cloud 或 registry fallback。
func (source cliCredentialSource) ResolveClientCredential(ctx context.Context, endpointID, reference string) (remoteauth.ClientAccessCredential, error) {
	credential, err := source.store.ResolveContext(ctx, reference)
	if err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	if credential.EndpointID != endpointID {
		return remoteauth.ClientAccessCredential{}, fmt.Errorf("credential endpoint does not match route endpoint")
	}
	return credential, nil
}

// ResolveClientSigner 从已验证 credential 构造进程内 signer；identity 不匹配 Endpoint 时禁止签名。
func (source cliCredentialSource) ResolveClientSigner(_ context.Context, endpointID, _ string, identity remoteauth.ClientAccessIdentity) (remoteauth.ClientAccessSigner, error) {
	if identity.EndpointID != endpointID {
		return nil, fmt.Errorf("credential signer endpoint does not match route endpoint")
	}
	return remoteauth.NewPrivateClientAccessSigner(identity)
}

func (source cliCredentialSource) Available(ctx context.Context, endpointID, reference string) bool {
	credential, err := source.store.ResolveContext(ctx, strings.TrimSpace(reference))
	return err == nil && credential.EndpointID == endpointID && credential.Ready()
}

func connectCLIEndpointApplication(ctx context.Context, owner *clientruntime.SessionOwner, target clientendpoint.Endpoint, requested clientendpoint.RouteID, intent clientruntime.ConnectIntent, localOptions localadapter.Options) (*protocoladapter.ApplicationClient, clientendpoint.AccessRoute, error) {
	runtime, err := newCLIEndpointRuntime(ctx, owner, target, localOptions)
	if err != nil {
		return nil, clientendpoint.AccessRoute{}, err
	}
	lease, err := runtime.EnsureSession(ctx, clientruntime.ConnectRequest{EndpointID: target.ID, RouteOverride: requested, Intent: intent})
	if err != nil {
		return nil, clientendpoint.AccessRoute{}, err
	}
	ready, err := owner.ApplicationSession(lease)
	if err != nil {
		return nil, clientendpoint.AccessRoute{}, err
	}
	client, err := protocoladapter.NewRuntimeApplicationClient(ready, runtime)
	if err != nil {
		return nil, clientendpoint.AccessRoute{}, err
	}
	route, ok := target.Route(lease.Stamp.RouteID)
	if !ok {
		_ = client.Close()
		return nil, clientendpoint.AccessRoute{}, fmt.Errorf("runtime winner route %q is absent from endpoint %q", lease.Stamp.RouteID, target.ID)
	}
	return client, route, nil
}

func newCLIEndpointRuntime(ctx context.Context, owner *clientruntime.SessionOwner, target clientendpoint.Endpoint, localOptions localadapter.Options) (*clientruntime.ClientRuntime, error) {
	if ctx == nil || owner == nil {
		return nil, fmt.Errorf("CLI endpoint runtime requires context and a session owner")
	}
	credentials := cliCredentialSource{store: remoteauth.NewCredentialStore(v3RemoteCredentialDir())}
	sshCredentials := sshadapter.AgentCredentialSource{}
	localDialer := localadapter.NewDialer(localOptions)
	dialers, err := clientruntime.NewPeerConnectorMap(map[clientendpoint.RouteKind]clientruntime.PeerConnector{
		clientendpoint.RouteLocalUnix: localDialer,
		clientendpoint.RouteSSHWebRTCTCP: sshadapter.NewDialer(sshadapter.Options{
			Peers: pionadapter.Factory{}, Authorization: peeradapter.CapabilityAuthorizer{Credentials: credentials},
			Credentials: sshCredentials, ClientName: "muxvia-cli",
		}),
		clientendpoint.RouteManagedWebRTC: managedadapter.LazyDialer{
			OpenCloud: func(ctx context.Context) (managedadapter.CloudClient, io.Closer, error) {
				cloud, err := openV3CloudLifecycleClient(ctx, cloudpb.CallerRole_CALLER_ROLE_TUI,
					cloudpb.CompanionCapability_COMPANION_CAPABILITY_SIGNALING,
					cloudpb.CompanionCapability_COMPANION_CAPABILITY_RELAY_LEASE,
					cloudpb.CompanionCapability_COMPANION_CAPABILITY_PATH_QUALITY,
					cloudpb.CompanionCapability_COMPANION_CAPABILITY_SMART_ROUTE,
				)
				return cloud, cloud, err
			},
			Peers: pionadapter.Factory{}, ClientName: "muxvia-cli",
			Authorization: peeradapter.CapabilityAuthorizer{Credentials: credentials}, Now: time.Now,
		},
	})
	if err != nil {
		return nil, err
	}
	environment := cliRoutePlanEnvironment(ctx, target, credentials, sshCredentials)
	configKey, err := cliEndpointConfigKey(target, localOptions, environment)
	if err != nil {
		return nil, err
	}
	runtime, err := clientruntime.NewClientRuntime(owner, cliEndpointPlanSource{snapshot: clientruntime.EndpointPlanSnapshot{
		Endpoint: target, Environment: environment, ConfigKey: configKey,
	}}, systemadapter.Clock{}, dialers)
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

type cliSSHCredentialAvailability interface {
	Available(string) bool
}

type cliCapabilityAvailability interface {
	Available(context.Context, string, string) bool
}

func cliRoutePlanEnvironment(ctx context.Context, target clientendpoint.Endpoint, credentials cliCapabilityAvailability, sshCredentials cliSSHCredentialAvailability) clientruntime.RoutePlanEnvironment {
	environment := clientruntime.RoutePlanEnvironment{SupportedRouteKinds: []clientendpoint.RouteKind{
		clientendpoint.RouteLocalUnix, clientendpoint.RouteSSHWebRTCTCP, clientendpoint.RouteManagedWebRTC,
	}}
	for _, route := range target.RouteList() {
		reference := strings.TrimSpace(route.CredentialRef)
		switch route.Kind {
		case clientendpoint.RouteSSHWebRTCTCP:
			if credentials != nil && credentials.Available(ctx, string(target.ID), reference) {
				environment.AvailableCredentialRefs = append(environment.AvailableCredentialRefs, reference)
				if sshCredentials != nil && sshCredentials.Available(route.SSHCredentialRef) {
					environment.AvailableCredentialRefs = append(environment.AvailableCredentialRefs, route.SSHCredentialRef)
				}
			}
		case clientendpoint.RouteManagedWebRTC:
			if credentials != nil && credentials.Available(ctx, string(target.ID), reference) {
				environment.AvailableCredentialRefs = append(environment.AvailableCredentialRefs, reference)
			}
		}
	}
	return environment
}

func cliEndpointConfigKey(target clientendpoint.Endpoint, localOptions localadapter.Options, environment clientruntime.RoutePlanEnvironment) (string, error) {
	payload, err := json.Marshal(struct {
		Endpoint       clientendpoint.Endpoint            `json:"endpoint"`
		SocketOverride string                             `json:"socket_override"`
		DefaultSocket  string                             `json:"default_socket"`
		ClientName     string                             `json:"client_name"`
		ReadyTimeout   time.Duration                      `json:"ready_timeout"`
		RetryInterval  time.Duration                      `json:"retry_interval"`
		Environment    clientruntime.RoutePlanEnvironment `json:"environment"`
	}{target, localOptions.SocketOverride, localOptions.DefaultSocket, localOptions.ClientName, localOptions.ReadyTimeout, localOptions.RetryInterval, environment})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
