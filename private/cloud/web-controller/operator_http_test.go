package webcontroller_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
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
	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
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
	operatorCatalog := proto.Clone(catalog.Contract()).(*cloudpb.PlanCatalogContract)
	for _, plan := range operatorCatalog.GetPlans() {
		if plan.GetPlanId() == "pro" {
			plan.Price = &cloudpb.PlanPriceDefinition{Mode: cloudpb.CatalogPriceMode_CATALOG_PRICE_MODE_CONFIGURED, Currency: "USD", MonthlyMinor: 1000, YearlyMinor: 10000, Label: "$10 / month"}
			plan.Creem = &cloudpb.CreemProductMapping{MonthlyProductId: "prod_operator_monthly", YearlyProductId: "prod_operator_yearly"}
		}
	}
	if err := catalogService.Bootstrap(context.Background(), operatorCatalog); err != nil {
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
	adminAccount, err := commerceService.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "operator-admin@example.com", Password: "admin-password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := commerceService.SetOperatorRole(context.Background(), adminAccount.GetSession().GetAccount().GetAccountId(), commerce.OperatorRoleAdmin); err != nil {
		t.Fatal(err)
	}
	readonlyAccount, err := commerceService.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "operator-readonly@example.com", Password: "readonly-password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := commerceService.SetOperatorRole(context.Background(), readonlyAccount.GetSession().GetAccount().GetAccountId(), commerce.OperatorRoleReadonly); err != nil {
		t.Fatal(err)
	}
	_, err = commerceService.Register(context.Background(), &cloudpb.RegisterAccountRequest{Email: "ordinary@example.com", Password: "ordinary-password"})
	if err != nil {
		t.Fatal(err)
	}
	presence := &cloudpb.PresenceProjection{DaemonDeviceId: "daemon-operator", ControlOwnerHubId: "hub-operator", AssignmentEpoch: 3, PresenceSessionId: "presence-operator", Availability: cloudpb.Availability_AVAILABILITY_ONLINE, Freshness: cloudpb.Freshness_FRESHNESS_FRESH, ObservedAtUnixMillis: now.UnixMilli(), FreshUntilUnixMillis: now.Add(time.Minute).UnixMilli(), DaemonRuntimeGeneration: "runtime-operator", RegistryRevision: 4}
	device := &cloudpb.AccountDeviceProjection{AccountId: accountID, DeviceId: "daemon-operator", DisplayName: "Operator workstation", Platform: "linux", DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 1, AssignedHubId: "hub-operator", AssignmentEpoch: 3, Presence: presence}
	peer := &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-operator", ManagedSessionId: "managed-operator", SessionIncarnation: 1, AssignmentEpoch: 3, ControlPresenceSessionId: "presence-operator", DaemonRuntimeGeneration: "runtime-operator"}, ClientDeviceId: "client-operator", ControlOwnerHubId: "hub-operator", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
	checkout, err := commerceService.CreateCheckout(context.Background(), accountID, registered.GetSession().GetAccount().GetUserId(), &cloudpb.CreateCheckoutRequest{PlanId: "pro", RequestedTransition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_UPGRADE, BillingCadence: cloudpb.BillingCadence_BILLING_CADENCE_MONTHLY})
	if err != nil {
		t.Fatal(err)
	}
	creemAttempt, err := commerceService.CreatePaymentAttempt(context.Background(), accountID, registered.GetSession().GetAccount().GetUserId(), &cloudpb.CreatePaymentAttemptRequest{OrderId: checkout.GetOrder().GetOrderId(), Provider: "creem"})
	if err != nil {
		t.Fatal(err)
	}
	reconciler := &operatorPaymentReconciler{commerce: commerceService}
	overrides, _ := cloudentitlement.NewOverrideService(cloudentitlement.OverrideServiceConfig{Store: store, Plans: catalogService, Now: func() time.Time { return now }})
	promotions, _ := promotion.New(store, func() time.Time { return now }, nil, nil)
	releasePrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	releases, _ := releasecatalog.New(store, map[string]ed25519.PublicKey{"operator-release-key": releasePrivate.Public().(ed25519.PublicKey)}, []string{"https://releases.muxvia.test"}, func() time.Time { return now })
	topology := &managementTargetSource{presenceAccountID: accountID, presence: presence, devices: []*cloudpb.AccountDeviceProjection{device}, presences: []*cloudpb.PresenceProjection{presence}, peerSessions: []*cloudpb.ManagedPeerSessionProjection{peer}}
	outbox, _ := commandoutbox.New(store)
	planner, _ := commandoutbox.NewPlanner(outbox, topology, nil, bytes.NewReader(bytes.Repeat([]byte{7}, 64)), nil)
	admin, err := webcontroller.OperatorAPIHandler(webcontroller.OperatorAPIConfig{
		Commerce: commerceService, Catalog: catalogService, Overrides: overrides, Promotions: promotions, Topology: topology, Quota: store, Outbox: outbox, Planner: planner, Fleet: staticFleet{}, PaymentReconciler: reconciler, Releases: releases, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := webcontroller.ProductAPIHandler(webcontroller.ProductAPIConfig{Commerce: commerceService})
	if err != nil {
		t.Fatal(err)
	}
	ordinaryCookies := accountLogin(t, product, "ordinary@example.com", "ordinary-password")
	ordinaryWorkspace := operatorRequest(t, http.MethodGet, "/api/v1/operator/workspace", nil, ordinaryCookies)
	ordinaryWorkspaceResponse := httptest.NewRecorder()
	admin.ServeHTTP(ordinaryWorkspaceResponse, ordinaryWorkspace)
	if ordinaryWorkspaceResponse.Code != http.StatusForbidden {
		t.Fatalf("ordinary workspace = %d: %s", ordinaryWorkspaceResponse.Code, ordinaryWorkspaceResponse.Body.String())
	}
	adminCookies := accountLogin(t, product, "operator-admin@example.com", "admin-password")
	operatorReauth(t, admin, adminCookies, "admin-password")
	workspace := operatorRequest(t, http.MethodGet, "/api/v1/operator/workspace", nil, adminCookies)
	workspaceResponse := httptest.NewRecorder()
	admin.ServeHTTP(workspaceResponse, workspace)
	if workspaceResponse.Code != http.StatusOK || !strings.Contains(workspaceResponse.Body.String(), "OPERATOR_WORKSPACE_MODULE_USERS") || strings.Contains(workspaceResponse.Body.String(), "admin") {
		t.Fatalf("admin workspace = %d: %s", workspaceResponse.Code, workspaceResponse.Body.String())
	}

	list := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/list", &cloudpb.ListOperatorAccountsRequest{Query: "operator-target"}, adminCookies)
	listResponse := httptest.NewRecorder()
	admin.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), accountID) {
		t.Fatalf("operator account list = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	releaseDigest := sha256.Sum256([]byte("operator-release"))
	releaseArtifact := &cloudpb.ReleaseArtifactProjection{ReleaseId: "operator-android-1", Product: cloudpb.ReleaseProduct_RELEASE_PRODUCT_ANDROID, Channel: cloudpb.ReleaseChannel_RELEASE_CHANNEL_STABLE, Version: "v1.0.0", VersionCode: 100, Os: "android", Arch: "arm64", DownloadUrl: "https://releases.muxvia.test/operator.apk", ArtifactSize: 4096, Sha256: releaseDigest[:], SigningKeyId: "operator-release-key", MinCompatibleVersionCode: 50, RolloutBasisPoints: 1000}
	releasePayload, _ := releasecatalog.SigningPayload(releaseArtifact)
	releaseArtifact.Signature = ed25519.Sign(releasePrivate, releasePayload)
	publishRelease := operatorRequest(t, http.MethodPost, "/api/v1/operator/releases/publish", &cloudpb.PublishReleaseArtifactRequest{Artifact: releaseArtifact, Reason: "operator release", RequestId: "operator-release-publish"}, adminCookies)
	publishReleaseResponse := httptest.NewRecorder()
	admin.ServeHTTP(publishReleaseResponse, publishRelease)
	if publishReleaseResponse.Code != http.StatusCreated || !strings.Contains(publishReleaseResponse.Body.String(), releaseArtifact.GetReleaseId()) {
		t.Fatalf("operator release publish = %d: %s", publishReleaseResponse.Code, publishReleaseResponse.Body.String())
	}
	activateRelease := operatorRequest(t, http.MethodPost, "/api/v1/operator/releases/channel", &cloudpb.SetReleaseChannelRequest{ReleaseId: releaseArtifact.GetReleaseId(), Reason: "operator activation", RequestId: "operator-release-activate"}, adminCookies)
	activateReleaseResponse := httptest.NewRecorder()
	admin.ServeHTTP(activateReleaseResponse, activateRelease)
	if activateReleaseResponse.Code != http.StatusOK || !strings.Contains(activateReleaseResponse.Body.String(), "revision\":\"1") {
		t.Fatalf("operator release activation = %d: %s", activateReleaseResponse.Code, activateReleaseResponse.Body.String())
	}
	releaseAPI, err := webcontroller.ReleaseAPIHandler(releases)
	if err != nil {
		t.Fatal(err)
	}
	resolveRelease := operatorRequest(t, http.MethodPost, "/api/v1/releases/resolve", &cloudpb.ResolveClientReleaseRequest{Product: cloudpb.ReleaseProduct_RELEASE_PRODUCT_ANDROID, Channel: cloudpb.ReleaseChannel_RELEASE_CHANNEL_STABLE, Os: "android", Arch: "arm64", CurrentVersion: "v0.2.5", CurrentVersionCode: 25, StableClientId: "android-device-1"}, nil)
	resolveReleaseResponse := httptest.NewRecorder()
	releaseAPI.ServeHTTP(resolveReleaseResponse, resolveRelease)
	if resolveReleaseResponse.Code != http.StatusOK || !strings.Contains(resolveReleaseResponse.Body.String(), "forced\":true") || !strings.Contains(resolveReleaseResponse.Body.String(), releaseArtifact.GetReleaseId()) {
		t.Fatalf("client release resolve = %d: %s", resolveReleaseResponse.Code, resolveReleaseResponse.Body.String())
	}
	agents := operatorRequest(t, http.MethodPost, "/api/v1/operator/agents/list", &cloudpb.ListOperatorAgentsRequest{Query: "workstation", Freshness: cloudpb.Freshness_FRESHNESS_FRESH, Page: &cloudpb.PageRequest{PageSize: 20}}, adminCookies)
	agentsResponse := httptest.NewRecorder()
	admin.ServeHTTP(agentsResponse, agents)
	if agentsResponse.Code != http.StatusOK || !strings.Contains(agentsResponse.Body.String(), "presence-operator") || !strings.Contains(agentsResponse.Body.String(), "active_peer_session_count\":\"1") {
		t.Fatalf("operator agent list = %d: %s", agentsResponse.Code, agentsResponse.Body.String())
	}
	kick := operatorRequest(t, http.MethodPost, "/api/v1/operator/commands", &cloudpb.CreateManagementCommandRequest{AccountId: accountID, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_KICK_PRESENCE, Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_Presence{Presence: &cloudpb.KickPresenceTarget{DaemonDeviceId: presence.GetDaemonDeviceId(), AssignmentEpoch: presence.GetAssignmentEpoch(), PresenceSessionId: presence.GetPresenceSessionId()}}}, IdempotencyKey: "operator-kick-presence"}, adminCookies)
	kickResponse := httptest.NewRecorder()
	admin.ServeHTTP(kickResponse, kick)
	if kickResponse.Code != http.StatusAccepted || !strings.Contains(kickResponse.Body.String(), "presence-operator") {
		t.Fatalf("operator agent kick = %d: %s", kickResponse.Code, kickResponse.Body.String())
	}
	revokeSession := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/sessions/revoke", &cloudpb.RevokeOperatorAccountSessionRequest{AccountId: accountID, SessionId: registered.GetSession().GetSessionId(), ExpectedRevision: registered.GetSession().GetSessionRevision(), Reason: "credential reported lost", RequestId: "operator-session-revoke"}, adminCookies)
	revokeSessionResponse := httptest.NewRecorder()
	admin.ServeHTTP(revokeSessionResponse, revokeSession)
	if revokeSessionResponse.Code != http.StatusOK || !strings.Contains(revokeSessionResponse.Body.String(), "revoked\":true") {
		t.Fatalf("operator session revoke = %d: %s", revokeSessionResponse.Code, revokeSessionResponse.Body.String())
	}
	sessionAudit := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/get", &cloudpb.GetOperatorAccountRequest{AccountId: accountID}, adminCookies)
	sessionAuditResponse := httptest.NewRecorder()
	admin.ServeHTTP(sessionAuditResponse, sessionAudit)
	if sessionAuditResponse.Code != http.StatusOK || !strings.Contains(sessionAuditResponse.Body.String(), "account.session.revoke") || !strings.Contains(sessionAuditResponse.Body.String(), "credential reported lost") {
		t.Fatalf("operator session audit projection = %d: %s", sessionAuditResponse.Code, sessionAuditResponse.Body.String())
	}
	catalogList := operatorRequest(t, http.MethodPost, "/api/v1/operator/catalog/list", &cloudpb.ListPlanCatalogReleasesRequest{}, adminCookies)
	catalogListResponse := httptest.NewRecorder()
	admin.ServeHTTP(catalogListResponse, catalogList)
	if catalogListResponse.Code != http.StatusOK || !strings.Contains(catalogListResponse.Body.String(), "catalog_version\":\"1") {
		t.Fatalf("operator catalog list = %d: %s", catalogListResponse.Code, catalogListResponse.Body.String())
	}
	nextCatalog := proto.Clone(operatorCatalog).(*cloudpb.PlanCatalogContract)
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
	promotionCreate := operatorRequest(t, http.MethodPost, "/api/v1/operator/promotions/create", &cloudpb.CreatePromotionRequest{Promotion: &cloudpb.PromotionProjection{Code: "OPS20", DiscountKind: cloudpb.PromotionDiscountKind_PROMOTION_DISCOUNT_KIND_PERCENT, PercentBasisPoints: 2000, PlanIds: []string{"pro"}, EffectiveFromUnixMillis: now.Add(-time.Minute).UnixMilli(), EffectiveUntilUnixMillis: now.Add(time.Hour).UnixMilli(), MaxRedemptions: 10, MaxRedemptionsPerAccount: 1, CreemDiscountCode: "disc_ops20", Reason: "operator test"}, RequestId: "promotion-request-1"}, adminCookies)
	promotionCreateResponse := httptest.NewRecorder()
	admin.ServeHTTP(promotionCreateResponse, promotionCreate)
	if promotionCreateResponse.Code != http.StatusOK || !strings.Contains(promotionCreateResponse.Body.String(), "OPS20") {
		t.Fatalf("operator promotion create = %d: %s", promotionCreateResponse.Code, promotionCreateResponse.Body.String())
	}
	promotionList := operatorRequest(t, http.MethodPost, "/api/v1/operator/promotions/list", &cloudpb.ListPromotionsRequest{IncludeDisabled: true, Page: &cloudpb.PageRequest{PageSize: 20}}, adminCookies)
	promotionListResponse := httptest.NewRecorder()
	admin.ServeHTTP(promotionListResponse, promotionList)
	if promotionListResponse.Code != http.StatusOK || !strings.Contains(promotionListResponse.Body.String(), "disc_ops20") {
		t.Fatalf("operator promotion list = %d: %s", promotionListResponse.Code, promotionListResponse.Body.String())
	}
	adjust := operatorRequest(t, http.MethodPost, "/api/v1/operator/subscriptions/adjust", &cloudpb.CreateSubscriptionAdjustmentRequest{AccountId: accountID, AdjustmentKind: cloudpb.SubscriptionAdjustmentKind_SUBSCRIPTION_ADJUSTMENT_KIND_EXTEND, DurationDays: 7, ExpectedSubscriptionRevision: 1, Reason: "support extension", RequestId: "adjust-request-1"}, adminCookies)
	adjustResponse := httptest.NewRecorder()
	admin.ServeHTTP(adjustResponse, adjust)
	if adjustResponse.Code != http.StatusOK || !strings.Contains(adjustResponse.Body.String(), "support extension") {
		t.Fatalf("operator subscription adjust = %d: %s", adjustResponse.Code, adjustResponse.Body.String())
	}
	ordersList := operatorRequest(t, http.MethodPost, "/api/v1/operator/orders/list", &cloudpb.ListOperatorOrdersRequest{Page: &cloudpb.PageRequest{PageSize: 20}}, adminCookies)
	ordersListResponse := httptest.NewRecorder()
	admin.ServeHTTP(ordersListResponse, ordersList)
	if ordersListResponse.Code != http.StatusOK {
		t.Fatalf("operator orders list = %d: %s", ordersListResponse.Code, ordersListResponse.Body.String())
	}
	reconcile := operatorRequest(t, http.MethodPost, "/api/v1/operator/orders/reconcile-creem", &cloudpb.ReconcileCreemPaymentAttemptRequest{PaymentAttemptId: creemAttempt.GetPaymentAttempt().GetPaymentAttemptId(), Reason: "webhook delivery delayed", RequestId: "reconcile-request-1"}, adminCookies)
	reconcileResponse := httptest.NewRecorder()
	admin.ServeHTTP(reconcileResponse, reconcile)
	if reconcileResponse.Code != http.StatusOK || reconciler.calls != 1 || !strings.Contains(reconcileResponse.Body.String(), "last_provider_status\":\"operator_reconciled") {
		t.Fatalf("operator Creem reconciliation = %d calls=%d: %s", reconcileResponse.Code, reconciler.calls, reconcileResponse.Body.String())
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

	readonlyCookies := accountLogin(t, product, "operator-readonly@example.com", "readonly-password")
	readonlyPublish := operatorRequest(t, http.MethodPost, "/api/v1/operator/releases/publish", &cloudpb.PublishReleaseArtifactRequest{Artifact: releaseArtifact, Reason: "readonly", RequestId: "readonly-release"}, readonlyCookies)
	readonlyPublishResponse := httptest.NewRecorder()
	admin.ServeHTTP(readonlyPublishResponse, readonlyPublish)
	if readonlyPublishResponse.Code != http.StatusForbidden {
		t.Fatalf("readonly release publish = %d: %s", readonlyPublishResponse.Code, readonlyPublishResponse.Body.String())
	}
	readonlyRevoke := operatorRequest(t, http.MethodPost, "/api/v1/operator/accounts/sessions/revoke", &cloudpb.RevokeOperatorAccountSessionRequest{AccountId: accountID, AllAccountSessions: true, Reason: "readonly must fail", RequestId: "readonly-session-revoke"}, readonlyCookies)
	readonlyRevokeResponse := httptest.NewRecorder()
	admin.ServeHTTP(readonlyRevokeResponse, readonlyRevoke)
	if readonlyRevokeResponse.Code != http.StatusForbidden {
		t.Fatalf("readonly session revoke = %d: %s", readonlyRevokeResponse.Code, readonlyRevokeResponse.Body.String())
	}
	forbidden := operatorRequest(t, http.MethodPost, "/api/v1/operator/subscription/transition", &cloudpb.OperatorTransitionSubscriptionRequest{AccountId: accountID, Transition: cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE}, readonlyCookies)
	forbiddenResponse := httptest.NewRecorder()
	admin.ServeHTTP(forbiddenResponse, forbidden)
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
	if err := commerceService.SetOperatorRole(context.Background(), adminAccount.GetSession().GetAccount().GetAccountId(), commerce.OperatorRoleNone); err != nil {
		t.Fatal(err)
	}
	downgraded := operatorRequest(t, http.MethodGet, "/api/v1/operator/workspace", nil, adminCookies)
	downgradedResponse := httptest.NewRecorder()
	admin.ServeHTTP(downgradedResponse, downgraded)
	if downgradedResponse.Code != http.StatusForbidden {
		t.Fatalf("downgraded existing session = %d: %s", downgradedResponse.Code, downgradedResponse.Body.String())
	}
}

type staticFleet struct{}

type operatorPaymentReconciler struct {
	commerce *commerce.Service
	calls    int
}

func (reconciler *operatorPaymentReconciler) ReconcilePaymentAttempt(ctx context.Context, attemptID string) (*cloudpb.PaymentAttemptProjection, error) {
	_, _, current, err := reconciler.commerce.ProviderPaymentContext(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	next := proto.Clone(current).(*cloudpb.PaymentAttemptProjection)
	next.Revision++
	next.LastProviderStatus = "operator_reconciled"
	next.UpdatedAtUnixMillis++
	if err := reconciler.commerce.UpdateProviderPaymentAttempt(ctx, next, current.GetRevision()); err != nil {
		return nil, err
	}
	reconciler.calls++
	return next, nil
}

func (staticFleet) ListHubFleet(context.Context, *cloudpb.ListHubFleetRequest) (*cloudpb.ListHubFleetResponse, error) {
	return &cloudpb.ListHubFleetResponse{Page: &cloudpb.PageResponse{}}, nil
}

func (staticFleet) GetHubStatus(context.Context, string) (*cloudpb.GetHubStatusResponse, error) {
	return &cloudpb.GetHubStatusResponse{}, nil
}

func (staticFleet) CreateHubDeployment(context.Context, *cloudpb.CreateHubDeploymentRequest, string, time.Time) (*cloudpb.CreateHubDeploymentResponse, error) {
	return &cloudpb.CreateHubDeploymentResponse{}, nil
}

func (staticFleet) UpdateHubDeployment(context.Context, *cloudpb.UpdateHubDeploymentRequest, string, time.Time) (*cloudpb.UpdateHubDeploymentResponse, error) {
	return &cloudpb.UpdateHubDeploymentResponse{}, nil
}

func (staticFleet) ApproveHubDeploymentIdentity(context.Context, *cloudpb.ApproveHubDeploymentIdentityRequest, string, time.Time) (*cloudpb.ApproveHubDeploymentIdentityResponse, error) {
	return &cloudpb.ApproveHubDeploymentIdentityResponse{}, nil
}

func (staticFleet) SetHubDeploymentDrain(context.Context, *cloudpb.SetHubDeploymentDrainRequest, string, time.Time) (*cloudpb.SetHubDeploymentDrainResponse, error) {
	return &cloudpb.SetHubDeploymentDrainResponse{}, nil
}

func (staticFleet) DisableHubDeployment(context.Context, *cloudpb.DisableHubDeploymentRequest, string, time.Time) (*cloudpb.DisableHubDeploymentResponse, error) {
	return &cloudpb.DisableHubDeploymentResponse{}, nil
}

func accountLogin(t *testing.T, handler http.Handler, email, password string) map[string]*http.Cookie {
	t.Helper()
	login := operatorRequest(t, http.MethodPost, "/api/v1/account/login", &cloudpb.PasswordLoginRequest{Email: email, Password: password}, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("account login = %d: %s", response.Code, response.Body.String())
	}
	return cookieMap(response.Result().Cookies())
}

func operatorReauth(t *testing.T, handler http.Handler, cookies map[string]*http.Cookie, password string) {
	t.Helper()
	request := operatorRequest(t, http.MethodPost, "/api/v1/operator/reauth", &cloudpb.RecentAuthenticationRequest{Password: password}, cookies)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("operator reauth = %d: %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
}

func operatorRequest(t *testing.T, method, path string, body proto.Message, cookies map[string]*http.Cookie) *http.Request {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = protojson.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, "http://operator.test"+path, strings.NewReader(string(payload)))
	request.Header.Set("Origin", "http://operator.test")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if csrf := cookies["muxvia_cloud_csrf"]; csrf != nil {
		request.Header.Set("X-Muxvia-CSRF", csrf.Value)
	}
	return request
}
