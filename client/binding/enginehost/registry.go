package enginehost

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"google.golang.org/protobuf/proto"
)

const (
	endpointShareURIPrefix   = "termx://share?payload="
	maxPendingEndpointShares = 16
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

// ReceiveEndpointShare 接收并验证一次性 TLS share，只返回当前 registry 上的 Route/policy diff。
// bundle 保存在当前 Host generation 内；Android WebView 冻结或 engine 重建后 token 自动失效。
func (host *Host) ReceiveEndpointShare(ctx context.Context, request *bindingpb.EndpointShareReceiveRequest) (*bindingpb.EndpointShareReceiveResult, error) {
	offer, err := decodeEndpointShareOffer(request.GetPortableOffer())
	if err != nil {
		return nil, err
	}
	bundle, err := host.options.ShareReceive(ctx, offer)
	if err != nil {
		return nil, err
	}
	candidate, err := endpoint.EndpointCandidateFromShareBundle(bundle)
	if err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate endpoint share import token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	registry, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return nil, err
	}
	diff, err := endpoint.PreviewShare(registry, candidate)
	if err != nil {
		return nil, err
	}
	for pendingToken, pending := range host.pendingShares {
		if pending.GetExpiresAtUnixNano() <= host.options.Now().UnixNano() {
			delete(host.pendingShares, pendingToken)
		}
	}
	if len(host.pendingShares) >= maxPendingEndpointShares {
		return nil, fmt.Errorf("too many pending endpoint share previews")
	}
	host.pendingShares[token] = proto.Clone(bundle).(*remoteauthpb.ClientEndpointShareBundleV1)
	preview := &bindingpb.EndpointSharePreview{
		ImportToken: token, EndpointId: string(diff.EndpointID), Label: diff.Label,
		Identity:           &remoteauthpb.EndpointDaemonIdentity{DeviceId: diff.Identity.DeviceID, DeviceFingerprint: diff.Identity.DeviceFingerprint},
		ConnectModeChanged: diff.ConnectModeChanged, SelectionPolicyChanged: diff.SelectionPolicyChanged,
		ExpiresAtUnixNano: bundle.GetExpiresAtUnixNano(),
	}
	for _, route := range diff.Routes {
		preview.RouteDiffs = append(preview.RouteDiffs, &bindingpb.EndpointShareRouteDiff{RouteId: string(route.RouteID), RouteKind: string(route.Kind), Action: route.Action})
	}
	for _, descriptor := range bundle.GetCredentialDescriptors() {
		preview.CredentialDescriptors = append(preview.CredentialDescriptors, proto.Clone(descriptor).(*remoteauthpb.EndpointCredentialDescriptor))
	}
	return &bindingpb.EndpointShareReceiveResult{Preview: preview}, nil
}

// CommitEndpointShare 原子提交当前 generation 内的 config-only share token。
// 提交会针对最新 registry 重新执行 assembler；只有持久化成功或 token 过期时才消费，平台写失败可在当前 generation 内重试。
func (host *Host) CommitEndpointShare(ctx context.Context, request *bindingpb.EndpointShareCommitRequest) (*bindingpb.EndpointShareCommitResult, error) {
	token := strings.TrimSpace(request.GetImportToken())
	host.registryMu.Lock()
	defer host.registryMu.Unlock()
	bundle := host.pendingShares[token]
	if bundle == nil {
		return nil, fmt.Errorf("endpoint share import token is invalid or expired")
	}
	if bundle.GetExpiresAtUnixNano() <= host.options.Now().UnixNano() {
		delete(host.pendingShares, token)
		return nil, fmt.Errorf("endpoint share import token expired")
	}
	candidate, err := endpoint.EndpointCandidateFromShareBundle(bundle)
	if err != nil {
		return nil, err
	}
	current, err := host.loadRegistryLocked(ctx)
	if err != nil {
		return nil, err
	}
	assembled, err := endpoint.AssembleEndpoints(endpoint.EndpointAssemblerInput{Registry: current, Candidates: []endpoint.EndpointCandidate{candidate}})
	if err != nil {
		return nil, err
	}
	resolvedID := assembled.ResolvedEndpointIDs[0]
	wireRegistry, err := host.storeRegistryLocked(ctx, assembled.Registry, nil)
	if err != nil {
		return nil, err
	}
	delete(host.pendingShares, token)
	wireEndpoint, err := endpoint.EndpointToProto(assembled.Registry.Endpoints[resolvedID])
	if err != nil {
		return nil, err
	}
	return &bindingpb.EndpointShareCommitResult{Endpoint: wireEndpoint, Registry: wireRegistry, AuthorizationRequired: true}, nil
}

func decodeEndpointShareOffer(value string) (*remoteauthpb.ShareSessionOffer, error) {
	encoded := strings.TrimSpace(value)
	if strings.HasPrefix(encoded, endpointShareURIPrefix) {
		encoded = strings.TrimPrefix(encoded, endpointShareURIPrefix)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return nil, fmt.Errorf("endpoint share offer payload is invalid")
	}
	return endpoint.ParseShareSessionOffer(payload)
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
	// grant 属于已验证 daemon Endpoint，不属于某个低优先级 bootstrap candidate。
	// 必须在 assembler 完成后绑定全部远程 Route，否则 share/manual Route 会保留配置却丢失 terminal capability。
	pairedEndpoint := assembled.Registry.Endpoints[resolvedID]
	for routeID, route := range pairedEndpoint.Routes {
		if route.Kind == endpoint.RouteDirectWebRTCTCP || route.Kind == endpoint.RouteSSHWebRTCTCP || route.Kind == endpoint.RouteManagedWebRTC {
			route.CredentialRef = credentialRef
			pairedEndpoint.Routes[routeID] = route
		}
	}
	assembled.Registry.Endpoints[resolvedID] = pairedEndpoint
	assembled.Registry, err = assembled.Registry.Normalize()
	if err != nil {
		return nil, nil, err
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
var _ binding.EndpointShareHost = (*Host)(nil)
