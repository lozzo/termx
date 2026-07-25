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

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	cloudentitlement "github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestOperatorAPIEnforcesRoleCSRFRecentAuthAndPersistsSubscriptionAudit(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog, err := webcontroller.LoadCatalog("config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	catalogService, _ := cloudcatalog.New(store, func() time.Time { return now })
	if err := catalogService.Bootstrap(context.Background(), catalog.Contract()); err != nil {
		t.Fatal(err)
	}
	commerceService, err := commerce.New(commerce.Config{Store: store, Catalog: catalogService, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := commerceService.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "operator-target@example.com", Password: "secure-password"})
	if err != nil {
		t.Fatal(err)
	}
	accountID := registered.GetSession().GetAccount().GetAccountId()
	overrides, _ := cloudentitlement.NewOverrideService(cloudentitlement.OverrideServiceConfig{Store: store, Plans: catalogService, Now: func() time.Time { return now }})
	topology := &managementTargetSource{}
	outbox, _ := commandoutbox.New(store)
	planner, _ := commandoutbox.NewPlanner(outbox, topology, nil, bytes.NewReader(bytes.Repeat([]byte{7}, 64)), nil)
	adminToken := bytes.Repeat([]byte{0x41}, 32)
	admin, err := webcontroller.OperatorAPIHandler(webcontroller.OperatorAPIConfig{
		AccessToken: adminToken, OperatorID: "operator-admin", ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN,
		Commerce: commerceService, Catalog: catalogService, Overrides: overrides, Topology: topology, Quota: store, Outbox: outbox, Planner: planner, Fleet: staticFleet{}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	adminCookies := operatorLogin(t, admin, adminToken)

	list := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/list", &cloudpb.ListOperatorAccountsRequest{Query: "operator-target"}, adminCookies)
	listResponse := httptest.NewRecorder()
	admin.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), accountID) {
		t.Fatalf("operator account list = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	catalogList := operatorRequest(t, http.MethodPost, "/api/v1/operator/catalog/list", &cloudpb.ListPlanCatalogReleasesRequest{}, adminCookies)
	catalogListResponse := httptest.NewRecorder()
	admin.ServeHTTP(catalogListResponse, catalogList)
	if catalogListResponse.Code != http.StatusOK || !strings.Contains(catalogListResponse.Body.String(), "catalog_version\":\"1") {
		t.Fatalf("operator catalog list = %d: %s", catalogListResponse.Code, catalogListResponse.Body.String())
	}
	nextCatalog := proto.Clone(catalog.Contract()).(*cloudpb.PlanCatalogContract)
	nextCatalog.CatalogVersion = 2
	for _, plan := range nextCatalog.Plans {
		plan.PlanVersion++
	}
	publish := operatorRequest(t, http.MethodPost, "/api/v1/operator/catalog/publish", &cloudpb.PublishPlanCatalogRequest{Catalog: nextCatalog, Reason: "publish next catalog", RequestId: "catalog-request-2"}, adminCookies)
	publishResponse := httptest.NewRecorder()
	admin.ServeHTTP(publishResponse, publish)
	if publishResponse.Code != http.StatusOK || !strings.Contains(publishResponse.Body.String(), "catalog_version\":\"2") {
		t.Fatalf("operator catalog publish = %d: %s", publishResponse.Code, publishResponse.Body.String())
	}
	putOverride := operatorRequest(t, http.MethodPost, "/api/v1/operator/entitlement-overrides/put", &cloudpb.PutEntitlementOverrideRequest{Override: &cloudpb.EntitlementOverrideProjection{AccountId: accountID, CapabilityMask: &fieldmaskpb.FieldMask{Paths: []string{"cloud_device_limit"}}, Capability: &cloudpb.PlanCapability{CloudDeviceLimit: 9}, EffectiveFromUnixMillis: now.Add(-time.Minute).UnixMilli(), EffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), Reason: "support grant"}, RequestId: "override-request-1"}, adminCookies)
	putOverrideResponse := httptest.NewRecorder()
	admin.ServeHTTP(putOverrideResponse, putOverride)
	if putOverrideResponse.Code != http.StatusOK || !strings.Contains(putOverrideResponse.Body.String(), "cloud_device_limit\":9") {
		t.Fatalf("operator entitlement override = %d: %s", putOverrideResponse.Code, putOverrideResponse.Body.String())
	}
	missing := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/get", &cloudpb.GetOperatorAccountRequest{AccountId: "missing-account"}, adminCookies)
	missingResponse := httptest.NewRecorder()
	admin.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound || !strings.Contains(missingResponse.Body.String(), "MANAGEMENT_ERROR_CODE_NOT_FOUND") {
		t.Fatalf("missing operator account = %d: %s", missingResponse.Code, missingResponse.Body.String())
	}

	transitionContract := &cloudpb.OperatorTransitionSubscriptionRequest{AccountId: accountID, Transition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND}
	withoutCSRF := operatorRequest(t, http.MethodPost, "/api/v1/operator/subscription/transition", transitionContract, adminCookies)
	withoutCSRF.Header.Del("X-Muxvia-CSRF")
	withoutCSRFResponse := httptest.NewRecorder()
	admin.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusUnauthorized {
		t.Fatalf("operator transition without CSRF = %d: %s", withoutCSRFResponse.Code, withoutCSRFResponse.Body.String())
	}

	transition := operatorRequest(t, http.MethodPost, "/api/v1/operator/subscription/transition", transitionContract, adminCookies)
	transitionResponse := httptest.NewRecorder()
	admin.ServeHTTP(transitionResponse, transition)
	if transitionResponse.Code != http.StatusOK || !strings.Contains(transitionResponse.Body.String(), "SUBSCRIPTION_STATUS_SUSPENDED") {
		t.Fatalf("operator suspend = %d: %s", transitionResponse.Code, transitionResponse.Body.String())
	}
	detail := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/get", &cloudpb.GetOperatorAccountRequest{AccountId: accountID}, adminCookies)
	detailResponse := httptest.NewRecorder()
	admin.ServeHTTP(detailResponse, detail)
	if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), "subscription_transition_kind_suspend") {
		t.Fatalf("operator account detail audit = %d: %s", detailResponse.Code, detailResponse.Body.String())
	}

	readonlyToken := bytes.Repeat([]byte{0x52}, 32)
	readonly, err := webcontroller.OperatorAPIHandler(webcontroller.OperatorAPIConfig{
		AccessToken: readonlyToken, OperatorID: "operator-readonly", ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_READONLY,
		Commerce: commerceService, Catalog: catalogService, Overrides: overrides, Topology: topology, Quota: store, Outbox: outbox, Planner: planner, Fleet: staticFleet{}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	readonlyCookies := operatorLogin(t, readonly, readonlyToken)
	forbidden := operatorRequest(t, http.MethodPost, "/api/v1/operator/subscription/transition", &cloudpb.OperatorTransitionSubscriptionRequest{AccountId: accountID, Transition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE}, readonlyCookies)
	forbiddenResponse := httptest.NewRecorder()
	readonly.ServeHTTP(forbiddenResponse, forbidden)
	if forbiddenResponse.Code != http.StatusForbidden || !strings.Contains(forbiddenResponse.Body.String(), "MANAGEMENT_ERROR_CODE_FORBIDDEN") {
		t.Fatalf("readonly mutation = %d: %s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}

	now = now.Add(6 * time.Minute)
	expiredRecent := operatorRequest(t, http.MethodPost, "/api/v1/operator/subscription/transition", &cloudpb.OperatorTransitionSubscriptionRequest{AccountId: accountID, Transition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE}, adminCookies)
	expiredRecentResponse := httptest.NewRecorder()
	admin.ServeHTTP(expiredRecentResponse, expiredRecent)
	if expiredRecentResponse.Code != http.StatusForbidden || !strings.Contains(expiredRecentResponse.Body.String(), "MANAGEMENT_ERROR_CODE_RECENT_AUTH_REQUIRED") {
		t.Fatalf("expired operator recent auth = %d: %s", expiredRecentResponse.Code, expiredRecentResponse.Body.String())
	}
}

type staticFleet struct{}

func (staticFleet) ListHubFleet(context.Context, *cloudpb.ListHubFleetRequest) (*cloudpb.ListHubFleetResponse, error) {
	return &cloudpb.ListHubFleetResponse{Page: &cloudpb.PageResponse{}}, nil
}

func (staticFleet) GetHubStatus(context.Context, string) (*cloudpb.GetHubStatusResponse, error) {
	return &cloudpb.GetHubStatusResponse{}, nil
}

func operatorLogin(t *testing.T, handler http.Handler, token []byte) map[string]*http.Cookie {
	t.Helper()
	login := operatorRequest(t, http.MethodPost, "/api/v1/operator/login", &cloudpb.OperatorLoginRequest{AccessToken: token}, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("operator login = %d: %s", response.Code, response.Body.String())
	}
	return cookieMap(response.Result().Cookies())
}

func operatorRequest(t *testing.T, method, path string, body proto.Message, cookies map[string]*http.Cookie) *http.Request {
	t.Helper()
	payload, err := protojson.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "http://operator.test"+path, strings.NewReader(string(payload)))
	request.Header.Set("Origin", "http://operator.test")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if csrf := cookies["muxvia_cloud_operator_csrf"]; csrf != nil {
		request.Header.Set("X-Muxvia-CSRF", csrf.Value)
	}
	return request
}
