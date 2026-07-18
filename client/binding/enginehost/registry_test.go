package enginehost

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"google.golang.org/protobuf/proto"
)

func TestEndpointRegistryPersistsAcrossEngineRecreation(t *testing.T) {
	platform := newRegistryPlatform(t)
	first := platform.host()
	if _, err := first.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, "studio", "daemon-studio", "grant-studio"), MakeDefault: true}); err != nil {
		t.Fatal(err)
	}
	second := platform.host()
	result, err := second.GetEndpointRegistry(context.Background(), &bindingpb.EndpointRegistryGetRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetRegistry().GetDefaultEndpointId() != "studio" || len(result.GetRegistry().GetEndpoints()) != 1 {
		t.Fatalf("recreated registry = %#v", result.GetRegistry())
	}
}

func TestEndpointRegistryRejectsIdentityReplacementWithoutPublishing(t *testing.T) {
	platform := newRegistryPlatform(t)
	host := platform.host()
	if _, err := host.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, "studio", "daemon-one", "grant-one")}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, "studio", "daemon-two", "grant-two")}); err == nil {
		t.Fatal("identity replacement unexpectedly succeeded")
	}
	result, err := host.GetEndpointRegistry(context.Background(), &bindingpb.EndpointRegistryGetRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.GetRegistry().GetEndpoints()[0].GetIdentity().GetDeviceId(); got != "daemon-one" {
		t.Fatalf("identity conflict published %q", got)
	}
}

func TestEndpointRegistryConcurrentUpsertsDoNotLoseEndpoints(t *testing.T) {
	platform := newRegistryPlatform(t)
	host := platform.host()
	const count = 32
	var wait sync.WaitGroup
	errors := make(chan error, count)
	for index := 0; index < count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			id := fmt.Sprintf("endpoint-%02d", index)
			_, err := host.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, id, "daemon-"+id, "grant-"+id)})
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := host.GetEndpointRegistry(context.Background(), &bindingpb.EndpointRegistryGetRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(result.GetRegistry().GetEndpoints()); got != count {
		t.Fatalf("registry contains %d endpoints, want %d", got, count)
	}
}

func TestEndpointRegistryStoreFailureKeepsPreviousSnapshot(t *testing.T) {
	platform := newRegistryPlatform(t)
	host := platform.host()
	if _, err := host.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, "one", "daemon-one", "grant-one")}); err != nil {
		t.Fatal(err)
	}
	platform.failNextStore()
	if _, err := host.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, "two", "daemon-two", "grant-two")}); err == nil {
		t.Fatal("failed platform store unexpectedly succeeded")
	}
	result, err := host.GetEndpointRegistry(context.Background(), &bindingpb.EndpointRegistryGetRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(result.GetRegistry().GetEndpoints()); got != 1 || result.GetRegistry().GetEndpoints()[0].GetEndpointId() != "one" {
		t.Fatalf("failed store published registry %#v", result.GetRegistry())
	}
}

func TestEndpointDeleteCommitsRegistryAndCredentialCleanupTogether(t *testing.T) {
	platform := newRegistryPlatform(t)
	platform.credentials["grant-studio"] = "bound"
	host := platform.host()
	if _, err := host.UpsertEndpoint(context.Background(), &bindingpb.EndpointUpsertRequest{Endpoint: testEndpointProto(t, "studio", "daemon-studio", "grant-studio")}); err != nil {
		t.Fatal(err)
	}
	deleted, err := host.DeleteEndpoint(context.Background(), &bindingpb.EndpointDeleteRequest{EndpointId: "studio"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.GetRegistry().GetEndpoints()) != 0 {
		t.Fatalf("deleted registry = %#v", deleted.GetRegistry())
	}
	platform.mu.Lock()
	_, credentialExists := platform.credentials["grant-studio"]
	platform.mu.Unlock()
	if credentialExists {
		t.Fatal("unreferenced credential was not deleted in registry transaction")
	}
}

func TestPairingCredentialRollbackRestoresPreparedState(t *testing.T) {
	platform := newRegistryPlatform(t)
	host := platform.host()
	platform.credentials["grant-new"] = "bound-new"
	if err := host.rollbackPreparedCredential(context.Background(), &bindingpb.CredentialRecord{
		EndpointId: "new", CredentialRef: "grant-new", NewlyCreated: true,
	}, "bound-new"); err != nil {
		t.Fatal(err)
	}
	platform.credentials["grant-existing"] = "bound-new"
	if err := host.rollbackPreparedCredential(context.Background(), &bindingpb.CredentialRecord{
		EndpointId: "existing", CredentialRef: "grant-existing", CapabilityGrant: "bound-old",
	}, "bound-new"); err != nil {
		t.Fatal(err)
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if _, exists := platform.credentials["grant-new"]; exists {
		t.Fatal("newly created credential survived rollback")
	}
	if got := platform.credentials["grant-existing"]; got != "bound-old" {
		t.Fatalf("existing credential grant after rollback = %q", got)
	}
}

func TestEndpointSharePreviewCommitsAtomicallyAndTokenIsSingleUse(t *testing.T) {
	platform := newRegistryPlatform(t)
	host := platform.host()
	now := time.Now().UTC()
	bundle, err := endpoint.NewClientEndpointShareBundle(endpoint.Endpoint{
		ID: "source", Label: "Shared Studio", LabelSource: endpoint.SourceUser,
		DaemonIdentity: endpoint.DaemonIdentity{DeviceID: "daemon-shared", DeviceFingerprint: "SHA256:shared"},
		ConnectMode:    endpoint.ConnectOnDemand, Enabled: true,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"direct": {ID: "direct", Kind: endpoint.RouteDirectWebRTCTCP, Enabled: true, Source: endpoint.SourceManual, PolicySource: endpoint.SourceManual, SignalingAddresses: []string{"shared:41120"}, ICETCPAddresses: []string{"shared:41121"}},
		},
	}, "share-binding", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	host.options.Now = func() time.Time { return now }
	host.options.ShareReceive = func(context.Context, *remoteauthpb.ShareSessionOffer) (*remoteauthpb.ClientEndpointShareBundleV1, error) {
		return proto.Clone(bundle).(*remoteauthpb.ClientEndpointShareBundleV1), nil
	}
	host.pendingShares = make(map[string]*remoteauthpb.ClientEndpointShareBundleV1)
	offer := &remoteauthpb.ShareSessionOffer{
		SchemaVersion: endpoint.ShareSessionOfferVersion, TransferId: bundle.GetTransferId(), ListenerAddresses: []string{"127.0.0.1:41130"},
		EphemeralCertificateSha256: "sha256:" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), OneTimeSessionSecret: make([]byte, 32), ExpiresAtUnixNano: now.Add(time.Minute).UnixNano(),
	}
	offerPayload, err := endpoint.MarshalShareSessionOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	received, err := host.ReceiveEndpointShare(context.Background(), &bindingpb.EndpointShareReceiveRequest{PortableOffer: endpointShareURIPrefix + base64.RawURLEncoding.EncodeToString(offerPayload)})
	if err != nil {
		t.Fatal(err)
	}
	preview := received.GetPreview()
	if preview.GetImportToken() == "" || len(preview.GetRouteDiffs()) != 1 || preview.GetRouteDiffs()[0].GetAction() != "add" {
		t.Fatalf("share preview=%#v", preview)
	}
	committed, err := host.CommitEndpointShare(context.Background(), &bindingpb.EndpointShareCommitRequest{ImportToken: preview.GetImportToken()})
	if err != nil {
		t.Fatal(err)
	}
	if !committed.GetAuthorizationRequired() || committed.GetEndpoint().GetRoutes()[0].GetCredentialRef() != "" {
		t.Fatalf("share commit was not config-only: %#v", committed)
	}
	if _, err := host.CommitEndpointShare(context.Background(), &bindingpb.EndpointShareCommitRequest{ImportToken: preview.GetImportToken()}); err == nil {
		t.Fatal("share import token unexpectedly committed twice")
	}
	retryPreview, err := host.ReceiveEndpointShare(context.Background(), &bindingpb.EndpointShareReceiveRequest{PortableOffer: endpointShareURIPrefix + base64.RawURLEncoding.EncodeToString(offerPayload)})
	if err != nil {
		t.Fatal(err)
	}
	platform.failNextStore()
	retryToken := retryPreview.GetPreview().GetImportToken()
	if _, err := host.CommitEndpointShare(context.Background(), &bindingpb.EndpointShareCommitRequest{ImportToken: retryToken}); err == nil {
		t.Fatal("share commit unexpectedly ignored platform store failure")
	}
	if _, err := host.CommitEndpointShare(context.Background(), &bindingpb.EndpointShareCommitRequest{ImportToken: retryToken}); err != nil {
		t.Fatalf("share token did not survive unpublished store failure: %v", err)
	}
}

func testEndpointProto(t *testing.T, id, deviceID, credentialRef string) *remoteauthpb.EndpointConfigV1 {
	t.Helper()
	digest := sha256.Sum256([]byte(deviceID))
	model := endpoint.Endpoint{
		ID: endpoint.EndpointID(id), Label: id, LabelSource: endpoint.SourceUser,
		DaemonIdentity: endpoint.DaemonIdentity{DeviceID: deviceID, DeviceFingerprint: "ed25519-sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])},
		ConnectMode:    endpoint.ConnectOnDemand, Enabled: true,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"direct": {
				ID: "direct", Kind: endpoint.RouteDirectWebRTCTCP, Enabled: true, Source: endpoint.SourceBootstrap, PolicySource: endpoint.SourceBootstrap,
				CredentialRef: credentialRef, SignalingAddresses: []string{"127.0.0.1:41120"}, ICETCPAddresses: []string{"127.0.0.1:41121"},
			},
		},
	}
	wire, err := endpoint.EndpointToProto(model)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

type registryPlatform struct {
	t           *testing.T
	mu          sync.Mutex
	registry    []byte
	credentials map[string]string
	failStore   bool
}

func newRegistryPlatform(t *testing.T) *registryPlatform {
	return &registryPlatform{t: t, credentials: make(map[string]string)}
}

func (platform *registryPlatform) host() *Host {
	broker := binding.NewPlatformBroker()
	platform.t.Cleanup(func() { _ = broker.Close() })
	go platform.pump(broker)
	return &Host{options: Options{Broker: broker}}
}

func (platform *registryPlatform) failNextStore() {
	platform.mu.Lock()
	platform.failStore = true
	platform.mu.Unlock()
}

func (platform *registryPlatform) pump(broker *binding.PlatformBroker) {
	for {
		payload, err := broker.NextRequest(context.Background())
		if err != nil {
			return
		}
		request := &bindingpb.PlatformRequest{}
		if err := proto.Unmarshal(payload, request); err != nil {
			platform.t.Errorf("decode platform request: %v", err)
			return
		}
		response := &bindingpb.PlatformResponse{RequestId: request.GetRequestId()}
		platform.mu.Lock()
		switch value := request.GetRequest().(type) {
		case *bindingpb.PlatformRequest_EndpointRegistryLoad:
			response.Response = &bindingpb.PlatformResponse_EndpointRegistry{EndpointRegistry: &bindingpb.EndpointRegistryLoaded{RegistryProto: append([]byte(nil), platform.registry...)}}
		case *bindingpb.PlatformRequest_EndpointRegistryStore:
			if platform.failStore {
				platform.failStore = false
				response.Error = &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE, Message: "registry store failed"}
				break
			}
			platform.registry = append([]byte(nil), value.EndpointRegistryStore.GetRegistryProto()...)
			for _, ref := range value.EndpointRegistryStore.GetDeleteCredentialRefs() {
				delete(platform.credentials, ref)
			}
		case *bindingpb.PlatformRequest_CredentialDelete:
			delete(platform.credentials, value.CredentialDelete.GetCredentialRef())
		case *bindingpb.PlatformRequest_CredentialBind:
			platform.credentials[value.CredentialBind.GetCredentialRef()] = value.CredentialBind.GetCapabilityGrant()
			response.Response = &bindingpb.PlatformResponse_Credential{Credential: &bindingpb.CredentialRecord{
				EndpointId: value.CredentialBind.GetEndpointId(), CredentialRef: value.CredentialBind.GetCredentialRef(), CapabilityGrant: value.CredentialBind.GetCapabilityGrant(),
			}}
		default:
			response.Error = &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST, Message: "unexpected platform request"}
		}
		platform.mu.Unlock()
		encoded, err := proto.Marshal(response)
		if err != nil {
			platform.t.Errorf("encode platform response: %v", err)
			return
		}
		if err := broker.Complete(encoded); err != nil {
			return
		}
	}
}
