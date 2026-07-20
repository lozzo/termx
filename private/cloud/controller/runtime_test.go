package controller

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestControllerKeepsListenersSeparateAndProjectionRevisionPersistent(t *testing.T) {
	now := time.Now().UTC()
	hubPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	projectionPublic, projectionPrivate, _ := ed25519.GenerateKey(rand.Reader)
	_ = projectionPublic
	metadata := &cloudpb.EdgeDeploymentMetadata{EdgeDeploymentId: "edge-1", Region: "local-1", HubId: "hub-1", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubPublic), RelayId: "relay-1", RelayControlIdentityFingerprint: "relay-fingerprint"}
	config := Config{DatabasePath: filepath.Join(t.TempDir(), "controller.db"), PublicListen: "127.0.0.1:0", InternalControlListen: "127.0.0.1:0", OperatorListen: "127.0.0.1:0", CatalogPath: "../web-controller/config/plans.json", ProjectionKeyID: "controller-key", ProjectionPrivateKeyBase64: base64.RawStdEncoding.EncodeToString(projectionPrivate), EnableTestPaymentProvider: true, Deployments: []DeploymentConfig{{Metadata: metadata, HubControlPublicKeyBase64: base64.RawStdEncoding.EncodeToString(hubPublic)}}, Accounts: []*cloudpb.HubAccountPolicy{{AccountId: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}}, Devices: []*cloudpb.CloudDevicePolicy{{AccountId: "account-1", DeviceId: "daemon-1", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1}}, Assignments: []*cloudpb.HubAssignment{{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-1", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}}}
	first, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	manifest := first.Manifest()
	if manifest.PublicURL == manifest.InternalControlURL || manifest.PublicURL == manifest.OperatorURL || manifest.InternalControlURL == manifest.OperatorURL {
		t.Fatalf("Controller listeners are not separated: %#v", manifest)
	}
	response, err := http.Get(manifest.PublicURL + "/api/v1/catalog")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", response.StatusCode)
	}
	registerBody, _ := protojson.Marshal(&cloudpb.RegisterAccountRequest{Email: "runtime@example.com", Password: "secure-password"})
	registerRequest, _ := http.NewRequest(http.MethodPost, manifest.PublicURL+"/api/v1/account/register", bytes.NewReader(registerBody))
	registerRequest.Header.Set("Origin", manifest.PublicURL)
	registerResponse, err := http.DefaultClient.Do(registerRequest)
	if err != nil {
		t.Fatal(err)
	}
	registerResponse.Body.Close()
	if registerResponse.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d", registerResponse.StatusCode)
	}
	if head, ok := first.publisher.Head("hub-1"); !ok || head.Revision != 1 {
		t.Fatalf("first projection head = %#v, %v", head, ok)
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := first.Close(closeContext); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	second, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	if head, ok := second.publisher.Head("hub-1"); !ok || head.Revision != 2 {
		t.Fatalf("restarted projection head = %#v, %v", head, ok)
	}
	loginBody, _ := protojson.Marshal(&cloudpb.PasswordLoginRequest{Email: "runtime@example.com", Password: "secure-password"})
	loginRequest, _ := http.NewRequest(http.MethodPost, second.Manifest().PublicURL+"/api/v1/account/login", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Origin", second.Manifest().PublicURL)
	loginResponse, err := http.DefaultClient.Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(loginResponse.Body)
	loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login after Controller restart = %d: %s", loginResponse.StatusCode, responseBody)
	}
}
