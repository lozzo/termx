package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice/httpapi"
	"github.com/muxvia/muxvia/private/cloud/companion/session"
	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/persistence"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestMobileActivationRequiresWebApprovalAndIsSingleUse(t *testing.T) {
	service, commerce, topology, now := newMobileActivationTestService(t)
	registered, err := commerce.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "mobile@muxvia.invalid", Password: "password-123"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	activation, err := service.CreateMobileActivation(context.Background(), account.GetAccountId(), account.GetUserId())
	if err != nil {
		t.Fatal(err)
	}
	if activation.GetState() != cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_DEVICE || activation.GetQrPayload() != "muxvia-cloud-activate:v1:"+activation.GetUserCode() {
		t.Fatalf("unexpected activation: %+v", activation)
	}
	if len(strings.ReplaceAll(activation.GetUserCode(), "-", "")) != 29 || !strings.HasPrefix(activation.GetUserCode(), "MXA-") {
		t.Fatalf("activation code does not contain a 128-bit App locator: %q", activation.GetUserCode())
	}
	for _, value := range strings.TrimPrefix(strings.ReplaceAll(activation.GetUserCode(), "-", ""), "MXA") {
		if !strings.ContainsRune(oneTimeCodeAlphabet, value) {
			t.Fatalf("activation code contains an ambiguous App-rejected character: %q", activation.GetUserCode())
		}
	}
	if _, err := service.ApproveMobileActivation(context.Background(), account.GetAccountId(), activation.GetUserCode()); !errors.Is(err, cloudcommerce.ErrNotFound) {
		t.Fatalf("approve before claim = %v", err)
	}
	const clientDeviceID = "client-12345678-1234-1234-1234-123456789abc"
	flow, err := service.claim(&cloudpb.ClaimMobileActivationRequest{UserCode: activation.GetUserCode(), ClientDeviceId: clientDeviceID, ClientMetadata: &cloudpb.DeviceMetadata{DisplayName: "Android phone", Platform: "android/arm64", MuxviaVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()}); !errors.Is(err, errMobileActivationPending) {
		t.Fatalf("complete before Web approval = %v", err)
	}
	inspected, err := service.InspectMobileActivation(context.Background(), account.GetAccountId(), activation.GetUserCode())
	if err != nil || inspected.GetState() != cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_APPROVAL || inspected.GetClientMetadata().GetDisplayName() != "Android phone" {
		t.Fatalf("inspect after claim = %+v, %v", inspected, err)
	}
	if _, err := service.InspectMobileActivation(context.Background(), "other-account", activation.GetUserCode()); !errors.Is(err, cloudcommerce.ErrNotFound) {
		t.Fatalf("cross-account inspect = %v", err)
	}
	approved, err := service.ApproveMobileActivation(context.Background(), account.GetAccountId(), activation.GetUserCode())
	if err != nil || !approved.GetApproved() {
		t.Fatalf("approve = %+v, %v", approved, err)
	}
	commitFailure := errors.New("temporary mobile activation commit failure")
	service.activationStore = &failOnceMobileActivationStore{delegate: service.activationStore, failure: commitFailure}
	if _, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()}); !errors.Is(err, commitFailure) {
		t.Fatalf("complete commit failure = %v", err)
	}
	afterFailure, err := service.InspectMobileActivation(context.Background(), account.GetAccountId(), activation.GetUserCode())
	if err != nil || afterFailure.GetState() != cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_APPROVED {
		t.Fatalf("activation was consumed before final commit: %+v, %v", afterFailure, err)
	}
	wire, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
	if err != nil {
		t.Fatal(err)
	}
	if wire.AccountID != account.GetAccountId() || wire.DeviceID != clientDeviceID || wire.HubID != "hub-1" || wire.HubURL != "http://127.0.0.1:41002" || wire.HubRegion != "local-1" || len(wire.AccessToken) == 0 || len(wire.RefreshToken) < 32 || wire.PlanID != "managed-free" || wire.PlanName != "Managed Free" || wire.SubscriptionStatus != cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE.String() || wire.SubscriptionRevision != 1 {
		t.Fatalf("unexpected session: %+v", wire)
	}
	if _, err := topology.Device(context.Background(), wire.DeviceID); err != nil {
		t.Fatal(err)
	}
	replayed, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
	if err != nil || replayed.AccountID != wire.AccountID || string(replayed.RefreshToken) != string(wire.RefreshToken) {
		t.Fatalf("activation delivery retry = %+v, %v", replayed, err)
	}

	// 注册码是一次性事务，不是设备实体。同一 Official App 安装再次扫码只轮换同一个
	// client_device_id 的 session，账号设备投影仍必须只有一台手机。
	secondActivation, err := service.CreateMobileActivation(context.Background(), account.GetAccountId(), account.GetUserId())
	if err != nil {
		t.Fatal(err)
	}
	secondFlow, err := service.claim(&cloudpb.ClaimMobileActivationRequest{UserCode: secondActivation.GetUserCode(), ClientDeviceId: clientDeviceID, ClientMetadata: &cloudpb.DeviceMetadata{DisplayName: "Android phone renamed", Platform: "android/arm64", MuxviaVersion: "test-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveMobileActivation(context.Background(), account.GetAccountId(), secondActivation.GetUserCode()); err != nil {
		t.Fatal(err)
	}
	if err := topology.PutDeviceOwnership(context.Background(), &cloudpb.CloudDevicePolicy{AccountId: account.GetAccountId(), DeviceId: clientDeviceID, DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: account.GetAuthRevision(), Revoked: true}); err != nil {
		t.Fatal(err)
	}
	secondWire, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: secondFlow.GetFlowId()})
	if err != nil || secondWire.DeviceID != clientDeviceID {
		t.Fatalf("second activation = %+v, %v", secondWire, err)
	}
	devices, err := topology.ListAccountDevices(context.Background(), account.GetAccountId(), cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, false, 10)
	if err != nil || len(devices) != 1 || devices[0].GetDeviceId() != clientDeviceID {
		t.Fatalf("stable client device projection = %+v, %v", devices, err)
	}
	reactivated, err := topology.Device(context.Background(), clientDeviceID)
	if err != nil || reactivated.Revoked || reactivated.AuthEpoch != account.GetAuthRevision() {
		t.Fatalf("reactivated ownership = %+v, %v", reactivated, err)
	}

	refreshed, err := service.refreshSession(context.Background(), refreshWire(secondWire.RefreshToken))
	if err != nil || refreshed.DeviceID != wire.DeviceID || len(refreshed.RefreshToken) < 32 || refreshed.PlanID != wire.PlanID || refreshed.SubscriptionStatus != wire.SubscriptionStatus || refreshed.SubscriptionRevision != wire.SubscriptionRevision {
		t.Fatalf("refresh = %+v, %v", refreshed, err)
	}
	if _, err := service.refreshSession(context.Background(), refreshWire(wire.RefreshToken)); err == nil {
		t.Fatal("refresh token was replayed")
	}
	*now = now.Add(31 * 24 * time.Hour)
	if _, err := service.refreshSession(context.Background(), refreshWire(refreshed.RefreshToken)); err == nil {
		t.Fatal("expired refresh token succeeded")
	}
}

type failOnceMobileActivationStore struct {
	delegate persistence.MobileActivationStore
	failure  error
}

func (store *failOnceMobileActivationStore) CommitMobileActivation(ctx context.Context, input persistence.MobileActivationCommit, now time.Time) error {
	if store.failure != nil {
		failure := store.failure
		store.failure = nil
		return failure
	}
	return store.delegate.CommitMobileActivation(ctx, input, now)
}

func TestDaemonSessionRefreshRevalidatesOwnershipAndAssignment(t *testing.T) {
	service, commerce, topology, now := newMobileActivationTestService(t)
	registered, err := commerce.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "daemon-refresh@muxvia.invalid", Password: "password-123"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	const deviceID = "daemon-refresh-1"
	if err := topology.PutDeviceOwnership(context.Background(), &cloudpb.CloudDevicePolicy{
		AccountId: account.GetAccountId(), DeviceId: deviceID, DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision(), PublicKey: make([]byte, ed25519.PublicKeySize),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.registry.Assign(context.Background(), &cloudpb.HubAssignment{
		DaemonDeviceId: deviceID, AccountId: account.GetAccountId(), HubId: "hub-2", AssignmentEpoch: 1,
		NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(24 * time.Hour).UnixMilli(),
	}, *now); err != nil {
		t.Fatal(err)
	}
	credential, err := commerce.IssueDeviceSession(context.Background(), account.GetAccountId(), deviceID)
	if err != nil {
		t.Fatal(err)
	}
	originalRefresh := append([]byte(nil), credential.GetRefreshToken()...)
	refreshed, err := service.refreshSession(context.Background(), httpapi.RefreshSessionWire{Kind: session.KindDevice, RefreshToken: originalRefresh})
	if err != nil || refreshed.Kind != session.KindDevice || refreshed.DeviceID != deviceID || refreshed.HubID != "hub-2" || refreshed.HubURL != "http://127.0.0.1:42002" || refreshed.HubRegion != "remote-1" || len(refreshed.AccessToken) == 0 || len(refreshed.RefreshToken) < 32 {
		t.Fatalf("daemon refresh = %+v, %v", refreshed, err)
	}
	if _, err := service.refreshSession(context.Background(), httpapi.RefreshSessionWire{Kind: session.KindDevice, RefreshToken: originalRefresh}); err == nil {
		t.Fatal("daemon refresh token was replayed")
	}
}

func newMobileActivationTestService(t *testing.T) (*mobileActivationService, *cloudcommerce.Service, *cloudtopology.Service, *time.Time) {
	t.Helper()
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	catalog, err := webcontroller.LoadCatalog("../web-controller/config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	catalogSource, _ := cloudcatalog.NewSnapshotSource(catalog.Contract())
	commerce, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalogSource, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := hubregistry.New(store)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := cloudtopology.New(registry, store)
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := servicecredential.NewSigner("test-key", privateKey, now.Add(-time.Hour), now.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgeAccessIssuer("test-controller", signer)
	if err != nil {
		t.Fatal(err)
	}
	service, err := newMobileActivationService(commerce, store, topology, registry, issuer, "hub-1", "http://127.0.0.1:41002", "local-1", func(hubID string) (string, string, bool) {
		switch hubID {
		case "hub-1":
			return "http://127.0.0.1:41002", "local-1", true
		case "hub-2":
			return "http://127.0.0.1:42002", "remote-1", true
		default:
			return "", "", false
		}
	}, now.Add(48*time.Hour), func() time.Time { return now }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return service, commerce, topology, &now
}

func refreshWire(token []byte) httpapi.RefreshSessionWire {
	return httpapi.RefreshSessionWire{Kind: session.KindAccount, RefreshToken: append([]byte(nil), token...)}
}
