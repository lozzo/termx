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
	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/muxvia/muxvia/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestMobileActivationRequiresWebApprovalAndIsSingleUse(t *testing.T) {
	service, commerce, topology, now := newMobileActivationTestService(t)
	registered, err := commerce.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "mobile@termx.invalid", Password: "password-123"})
	if err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	activation, err := service.CreateMobileActivation(context.Background(), account.GetAccountId(), account.GetUserId())
	if err != nil {
		t.Fatal(err)
	}
	if activation.GetState() != cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_DEVICE || activation.GetQrPayload() != "termx-cloud-activate:v1:"+activation.GetUserCode() {
		t.Fatalf("unexpected activation: %+v", activation)
	}
	if len(activation.GetUserCode()) != 11 || activation.GetUserCode()[5] != '-' {
		t.Fatalf("activation code does not match the App 5-5 contract: %q", activation.GetUserCode())
	}
	for _, value := range strings.ReplaceAll(activation.GetUserCode(), "-", "") {
		if !strings.ContainsRune(mobileCodeAlphabet, value) {
			t.Fatalf("activation code contains an ambiguous App-rejected character: %q", activation.GetUserCode())
		}
	}
	if _, err := service.ApproveMobileActivation(context.Background(), account.GetAccountId(), activation.GetUserCode()); !errors.Is(err, cloudcommerce.ErrNotFound) {
		t.Fatalf("approve before claim = %v", err)
	}
	flow, err := service.claim(&cloudpb.ClaimMobileActivationRequest{UserCode: activation.GetUserCode(), ClientMetadata: &cloudpb.DeviceMetadata{DisplayName: "Android phone", Platform: "android/arm64", TermxVersion: "test"}})
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
	wire, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
	if err != nil {
		t.Fatal(err)
	}
	if wire.AccountID != account.GetAccountId() || wire.DeviceID == "" || wire.HubID != "hub-1" || wire.HubURL != "http://127.0.0.1:41002" || wire.HubRegion != "local-1" || len(wire.AccessToken) == 0 || len(wire.RefreshToken) < 32 {
		t.Fatalf("unexpected session: %+v", wire)
	}
	if _, err := topology.Device(context.Background(), wire.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.complete(context.Background(), &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()}); err == nil {
		t.Fatal("activation flow was replayed")
	}

	refreshed, err := service.refreshSession(context.Background(), refreshWire(wire.RefreshToken))
	if err != nil || refreshed.DeviceID != wire.DeviceID || len(refreshed.RefreshToken) < 32 {
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

func newMobileActivationTestService(t *testing.T) (*mobileActivationService, *cloudcommerce.Service, *cloudtopology.Service, *time.Time) {
	t.Helper()
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	catalog, err := webcontroller.LoadCatalog("../web-controller/config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	commerce, err := cloudcommerce.New(cloudcommerce.Config{Store: store, Catalog: catalog.Contract(), Now: func() time.Time { return now }})
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
	service, err := newMobileActivationService(commerce, topology, issuer, "hub-1", "http://127.0.0.1:41002", "local-1", func() time.Time { return now }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return service, commerce, topology, &now
}

func refreshWire(token []byte) httpapi.RefreshSessionWire {
	return httpapi.RefreshSessionWire{Kind: session.KindAccount, RefreshToken: append([]byte(nil), token...)}
}
