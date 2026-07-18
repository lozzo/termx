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

	localadapter "github.com/lozzow/termx/client/adapter/local"
	managedadapter "github.com/lozzow/termx/client/adapter/managed"
	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	protocoladapter "github.com/lozzow/termx/client/adapter/protocol"
	sshadapter "github.com/lozzow/termx/client/adapter/ssh"
	systemadapter "github.com/lozzow/termx/client/adapter/system"
	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/remoteauth"
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
	localDialer := localadapter.NewDialer(localOptions)
	dialers, err := clientruntime.NewPeerConnectorMap(map[clientendpoint.RouteKind]clientruntime.PeerConnector{
		clientendpoint.RouteLocalUnix:    localDialer,
		clientendpoint.RouteSSHWebRTCTCP: sshadapter.NewDialer(sshadapter.Options{ClientName: "termx-cli"}),
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
			Peers: pionadapter.Factory{}, ClientName: "termx-cli",
			Authorization: managedadapter.CapabilityAuthorizer{Credentials: credentials}, Now: time.Now,
		},
	})
	if err != nil {
		return nil, err
	}
	environment := cliRoutePlanEnvironment(ctx, target, credentials)
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

func cliRoutePlanEnvironment(ctx context.Context, target clientendpoint.Endpoint, credentials cliCredentialSource) clientruntime.RoutePlanEnvironment {
	environment := clientruntime.RoutePlanEnvironment{SupportedRouteKinds: []clientendpoint.RouteKind{
		clientendpoint.RouteLocalUnix, clientendpoint.RouteSSHWebRTCTCP, clientendpoint.RouteManagedWebRTC,
	}}
	for _, route := range target.RouteList() {
		reference := strings.TrimSpace(route.CredentialRef)
		if reference == "" {
			continue
		}
		switch route.Kind {
		case clientendpoint.RouteSSHWebRTCTCP:
			if strings.HasPrefix(reference, "ssh:") {
				environment.AvailableCredentialRefs = append(environment.AvailableCredentialRefs, reference)
			}
		case clientendpoint.RouteManagedWebRTC:
			if credential, err := credentials.store.ResolveContext(ctx, reference); err == nil && credential.EndpointID == string(target.ID) {
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
