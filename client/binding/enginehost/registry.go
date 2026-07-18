package enginehost

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"google.golang.org/protobuf/proto"
)

// GetEndpointRegistry 返回 Go 校验后的完整 registry projection。
// 平台持久层的空 payload 表示尚未创建 registry，不表示读取失败或允许平台提供默认 Endpoint。
func (host *Host) GetEndpointRegistry(ctx context.Context, _ *bindingpb.EndpointRegistryGetRequest) (*bindingpb.EndpointRegistryGetResult, error) {
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	registry, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return nil, err
	}
	wire, err := endpoint.RegistryToProto(registry)
	if err != nil {
		return nil, err
	}
	return &bindingpb.EndpointRegistryGetResult{Registry: wire}, nil
}

// UpsertEndpoint 用 generated EndpointConfigV1 替换同 ID 配置，但禁止更换已有 daemon identity pin。
// 新 snapshot 只有在平台确认 opaque Proto 持久化成功后才发布到当前 generation。
func (host *Host) UpsertEndpoint(ctx context.Context, request *bindingpb.EndpointUpsertRequest) (*bindingpb.EndpointUpsertResult, error) {
	incoming, err := endpoint.EndpointFromProto(request.GetEndpoint())
	if err != nil {
		return nil, err
	}
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	current, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return nil, err
	}
	if existing, ok := current.Endpoints[incoming.ID]; ok && !existing.DaemonIdentity.Empty() && existing.DaemonIdentity != incoming.DaemonIdentity {
		return nil, fmt.Errorf("endpoint %q is pinned to a different daemon identity", incoming.ID)
	}
	next, err := cloneRegistry(current)
	if err != nil {
		return nil, err
	}
	next.Endpoints[incoming.ID] = incoming
	if request.GetMakeDefault() || next.Default == "" {
		next.Default = incoming.ID
	}
	next, err = next.Normalize()
	if err != nil {
		return nil, err
	}
	wireRegistry, err := host.storeRegistryLocked(ctx, next, nil)
	if err != nil {
		return nil, err
	}
	wireEndpoint, err := endpoint.EndpointToProto(next.Endpoints[incoming.ID])
	if err != nil {
		return nil, err
	}
	return &bindingpb.EndpointUpsertResult{Endpoint: wireEndpoint, Registry: wireRegistry}, nil
}

// DeleteEndpoint 提交移除后的 registry，并把不再被任何 Route 引用的 credential ref 交给同一平台事务清理。
func (host *Host) DeleteEndpoint(ctx context.Context, request *bindingpb.EndpointDeleteRequest) (*bindingpb.EndpointDeleteResult, error) {
	id := endpoint.EndpointID(strings.TrimSpace(request.GetEndpointId()))
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	current, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return nil, err
	}
	removed, ok := current.Endpoints[id]
	if !ok {
		return nil, fmt.Errorf("endpoint %q does not exist", id)
	}
	next, err := cloneRegistry(current)
	if err != nil {
		return nil, err
	}
	delete(next.Endpoints, id)
	if next.Default == id {
		next.Default = ""
	}
	next, err = next.Normalize()
	if err != nil {
		return nil, err
	}
	deleteRefs := unreferencedCredentials(removed, next)
	wireRegistry, err := host.storeRegistryLocked(ctx, next, deleteRefs)
	if err != nil {
		return nil, err
	}
	return &bindingpb.EndpointDeleteResult{EndpointId: string(id), Registry: wireRegistry}, nil
}

func (host *Host) registryEndpoint(ctx context.Context, id endpoint.EndpointID) (endpoint.Endpoint, error) {
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	registry, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return endpoint.Endpoint{}, err
	}
	target, ok := registry.Endpoints[id]
	if !ok {
		return endpoint.Endpoint{}, fmt.Errorf("endpoint %q does not exist", id)
	}
	return cloneEndpoint(target)
}

func (host *Host) loadRegistryLocked(ctx context.Context) (endpoint.Registry, error) {
	if host.registryLoaded {
		return host.registry, nil
	}
	response, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_EndpointRegistryLoad{
		EndpointRegistryLoad: &bindingpb.EndpointRegistryLoadRequest{},
	}})
	if err != nil {
		return endpoint.Registry{}, err
	}
	if err := platformResponseError(response); err != nil {
		return endpoint.Registry{}, err
	}
	loaded := response.GetEndpointRegistry()
	if loaded == nil {
		return endpoint.Registry{}, fmt.Errorf("platform endpoint registry load returned no payload")
	}
	payload := loaded.GetRegistryProto()
	registry := endpoint.Registry{Version: endpoint.RegistryVersion, Endpoints: map[endpoint.EndpointID]endpoint.Endpoint{}}
	if len(payload) != 0 {
		if len(payload) > endpoint.MaxRegistryBytes {
			return endpoint.Registry{}, fmt.Errorf("endpoint registry payload exceeds size limit")
		}
		wire := &remoteauthpb.EndpointRegistryV1{}
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, wire); err != nil {
			return endpoint.Registry{}, fmt.Errorf("decode endpoint registry: %w", err)
		}
		registry, err = endpoint.RegistryFromProto(wire)
		if err != nil {
			return endpoint.Registry{}, err
		}
	}
	host.registry = registry
	host.registryLoaded = true
	return host.registry, nil
}

func (host *Host) storeRegistryLocked(ctx context.Context, registry endpoint.Registry, deleteCredentialRefs []string) (*remoteauthpb.EndpointRegistryV1, error) {
	wire, err := endpoint.RegistryToProto(registry)
	if err != nil {
		return nil, err
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode endpoint registry: %w", err)
	}
	response, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_EndpointRegistryStore{
		EndpointRegistryStore: &bindingpb.EndpointRegistryStoreRequest{RegistryProto: payload, DeleteCredentialRefs: append([]string(nil), deleteCredentialRefs...)},
	}})
	if err != nil {
		return nil, err
	}
	if err := platformResponseError(response); err != nil {
		return nil, err
	}
	host.registry = registry
	host.registryLoaded = true
	return wire, nil
}

func (host *Host) commitPairingEndpoint(ctx context.Context, preferredID endpoint.EndpointID, candidate endpoint.EndpointCandidate, credentialRef string) (*remoteauthpb.EndpointConfigV1, *remoteauthpb.EndpointRegistryV1, error) {
	for index := range candidate.Routes {
		if candidate.Routes[index].Kind == endpoint.RouteDirectWebRTCTCP || candidate.Routes[index].Kind == endpoint.RouteSSHWebRTCTCP || candidate.Routes[index].Kind == endpoint.RouteManagedWebRTC {
			candidate.Routes[index].CredentialRef = credentialRef
		}
	}
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	current, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return nil, nil, err
	}
	input := endpoint.EndpointAssemblerInput{Registry: current, Candidates: []endpoint.EndpointCandidate{candidate}}
	if existing, ok := current.Endpoints[preferredID]; ok {
		if !existing.DaemonIdentity.Empty() && existing.DaemonIdentity != candidate.Identity {
			return nil, nil, fmt.Errorf("endpoint %q is pinned to a different daemon identity", preferredID)
		}
		if existing.DaemonIdentity.Empty() {
			input.ConfirmedIdentityBindings = []endpoint.ConfirmedIdentityBinding{{EndpointID: preferredID, Identity: candidate.Identity}}
		}
	}
	assembled, err := endpoint.AssembleEndpoints(input)
	if err != nil {
		return nil, nil, err
	}
	resolvedID := assembled.ResolvedEndpointIDs[0]
	if resolvedID != preferredID {
		if _, occupied := assembled.Registry.Endpoints[preferredID]; occupied {
			return nil, nil, fmt.Errorf("endpoint id %q is already used by another daemon", preferredID)
		}
		value := assembled.Registry.Endpoints[resolvedID]
		delete(assembled.Registry.Endpoints, resolvedID)
		value.ID = preferredID
		assembled.Registry.Endpoints[preferredID] = value
		if assembled.Registry.Default == resolvedID {
			assembled.Registry.Default = preferredID
		}
		assembled.Registry, err = assembled.Registry.Normalize()
		if err != nil {
			return nil, nil, err
		}
		resolvedID = preferredID
	}
	wireRegistry, err := host.storeRegistryLocked(ctx, assembled.Registry, nil)
	if err != nil {
		return nil, nil, err
	}
	wireEndpoint, err := endpoint.EndpointToProto(assembled.Registry.Endpoints[resolvedID])
	if err != nil {
		return nil, nil, err
	}
	return wireEndpoint, wireRegistry, nil
}

func (host *Host) rollbackPreparedCredential(ctx context.Context, prepared *bindingpb.CredentialRecord, boundGrant string) error {
	if prepared == nil || strings.TrimSpace(prepared.GetCredentialRef()) == "" {
		return nil
	}
	if prepared.GetNewlyCreated() || strings.TrimSpace(prepared.GetCapabilityGrant()) == "" {
		response, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialDelete{
			CredentialDelete: &bindingpb.CredentialDeleteRequest{CredentialRef: prepared.GetCredentialRef()},
		}})
		if err != nil {
			return err
		}
		return platformResponseError(response)
	}
	if prepared.GetCapabilityGrant() == boundGrant {
		return nil
	}
	response, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialBind{
		CredentialBind: &bindingpb.CredentialBindRequest{EndpointId: prepared.GetEndpointId(), CredentialRef: prepared.GetCredentialRef(), CapabilityGrant: prepared.GetCapabilityGrant()},
	}})
	if err != nil {
		return err
	}
	return platformResponseError(response)
}

func cloneRegistry(value endpoint.Registry) (endpoint.Registry, error) {
	wire, err := endpoint.RegistryToProto(value)
	if err != nil {
		return endpoint.Registry{}, err
	}
	return endpoint.RegistryFromProto(proto.Clone(wire).(*remoteauthpb.EndpointRegistryV1))
}

func cloneEndpoint(value endpoint.Endpoint) (endpoint.Endpoint, error) {
	wire, err := endpoint.EndpointToProto(value)
	if err != nil {
		return endpoint.Endpoint{}, err
	}
	return endpoint.EndpointFromProto(proto.Clone(wire).(*remoteauthpb.EndpointConfigV1))
}

func unreferencedCredentials(removed endpoint.Endpoint, remaining endpoint.Registry) []string {
	used := make(map[string]struct{})
	for _, item := range remaining.Endpoints {
		for _, route := range item.Routes {
			if ref := strings.TrimSpace(route.CredentialRef); ref != "" {
				used[ref] = struct{}{}
			}
		}
	}
	seen := make(map[string]struct{})
	var refs []string
	for _, route := range removed.Routes {
		ref := strings.TrimSpace(route.CredentialRef)
		if ref == "" {
			continue
		}
		if _, stillUsed := used[ref]; stillUsed {
			continue
		}
		if _, duplicate := seen[ref]; duplicate {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

var _ binding.EndpointRegistryHost = (*Host)(nil)
