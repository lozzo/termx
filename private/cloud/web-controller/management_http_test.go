package webcontroller_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/commandoutbox"
	"github.com/lozzow/termx/private/cloud/control-plane/commerce"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestManagementAPIUsesAccountCSRFAndDurableCommandProjection(t *testing.T) {
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog, _ := webcontroller.LoadCatalog("config/plans.json")
	commerceService, _ := commerce.New(commerce.Config{Store: store, Catalog: catalog.Contract(), Now: func() time.Time { return now }})
	productHandler, _ := webcontroller.ProductAPIHandler(webcontroller.ProductAPIConfig{Commerce: commerceService})
	register := productRequest(http.MethodPost, "/api/v1/account/register", `{"email":"management@example.com","password":"secure-password"}`, nil)
	registerResponse := httptest.NewRecorder()
	productHandler.ServeHTTP(registerResponse, register)
	registered := &cloudpb.RegisterAccountResponse{}
	if err := protojson.Unmarshal(registerResponse.Body.Bytes(), registered); err != nil {
		t.Fatal(err)
	}
	account := registered.GetSession().GetAccount()
	cookies := cookieMap(registerResponse.Result().Cookies())
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1"}
	source := &managementTargetSource{session: cloudtopology.StoredPeerSession{AccountID: account.GetAccountId(), HubID: "hub-1", Value: &cloudpb.ManagedPeerSessionProjection{Target: target, ClientDeviceId: "client-1", State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY}}}
	outbox, _ := commandoutbox.New(store)
	randomBytes := make([]byte, 36)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 1)
	}
	planner, _ := commandoutbox.NewPlanner(outbox, source, bytes.NewReader(randomBytes), nil)
	managementHandler, _ := webcontroller.ManagementAPIHandler(webcontroller.ManagementAPIConfig{Commerce: commerceService, Planner: planner, Outbox: outbox, Now: func() time.Time { return now }})
	body, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&cloudpb.CreateManagementCommandRequest{AccountId: "other-account", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_PeerSession{PeerSession: target}}, IdempotencyKey: "idem-1"})
	create := productRequest(http.MethodPost, "/api/v1/management/commands", string(body), cookies)
	createResponse := httptest.NewRecorder()
	managementHandler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusAccepted {
		t.Fatalf("create = %d: %s", createResponse.Code, createResponse.Body.String())
	}
	created := &cloudpb.CreateManagementCommandResponse{}
	if err := protojson.Unmarshal(createResponse.Body.Bytes(), created); err != nil {
		t.Fatal(err)
	}
	if created.GetCommand().GetAccountId() != account.GetAccountId() || len(created.GetCommand().GetChildren()) != 1 {
		t.Fatalf("created command = %v", created.GetCommand())
	}
	getBody, _ := protojson.Marshal(&cloudpb.GetManagementCommandRequest{AccountId: "other-account", CommandId: created.GetCommand().GetCommandId()})
	get := productRequest(http.MethodPost, "/api/v1/management/commands/get", string(getBody), cookies)
	getResponse := httptest.NewRecorder()
	managementHandler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), created.GetCommand().GetCommandId()) {
		t.Fatalf("get = %d: %s", getResponse.Code, getResponse.Body.String())
	}
	withoutCSRF := productRequest(http.MethodPost, "/api/v1/management/commands", string(body), cookies)
	withoutCSRF.Header.Del("X-TermX-CSRF")
	withoutCSRFResponse := httptest.NewRecorder()
	managementHandler.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusUnauthorized {
		t.Fatalf("create without CSRF = %d: %s", withoutCSRFResponse.Code, withoutCSRFResponse.Body.String())
	}
}

type managementTargetSource struct {
	session cloudtopology.StoredPeerSession
}

func (source *managementTargetSource) Device(context.Context, string) (cloudtopology.DeviceOwnership, error) {
	return cloudtopology.DeviceOwnership{}, cloudtopology.ErrOwnershipNotFound
}
func (source *managementTargetSource) Presence(context.Context, string) (string, *cloudpb.PresenceProjection, error) {
	return "", nil, cloudtopology.ErrTopologyRejected
}
func (source *managementTargetSource) PeerSession(context.Context, *cloudpb.ManagedPeerSessionTarget) (cloudtopology.StoredPeerSession, error) {
	return source.session, nil
}
func (source *managementTargetSource) PeerSessionsForClient(context.Context, string) ([]cloudtopology.StoredPeerSession, error) {
	return nil, nil
}
