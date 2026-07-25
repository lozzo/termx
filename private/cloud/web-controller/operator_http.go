package webcontroller

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	cloudentitlement "github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/private/cloud/control-plane/promotion"
	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
)

// OperatorFleetQuery 返回不伪造在线状态的 Hub/Relay attachment 投影。
type OperatorFleetQuery interface {
	ListHubFleet(context.Context, *cloudpb.ListHubFleetRequest) (*cloudpb.ListHubFleetResponse, error)
	GetHubStatus(context.Context, string) (*cloudpb.GetHubStatusResponse, error)
	CreateHubDeployment(context.Context, *cloudpb.CreateHubDeploymentRequest, string, time.Time) (*cloudpb.CreateHubDeploymentResponse, error)
	UpdateHubDeployment(context.Context, *cloudpb.UpdateHubDeploymentRequest, string, time.Time) (*cloudpb.UpdateHubDeploymentResponse, error)
	ApproveHubDeploymentIdentity(context.Context, *cloudpb.ApproveHubDeploymentIdentityRequest, string, time.Time) (*cloudpb.ApproveHubDeploymentIdentityResponse, error)
	SetHubDeploymentDrain(context.Context, *cloudpb.SetHubDeploymentDrainRequest, string, time.Time) (*cloudpb.SetHubDeploymentDrainResponse, error)
	DisableHubDeployment(context.Context, *cloudpb.DisableHubDeploymentRequest, string, time.Time) (*cloudpb.DisableHubDeploymentResponse, error)
}

// OperatorPaymentReconciler 从正式 provider 服务端立即核对一个持久 payment attempt。
// 实现必须把结果送入 commerce normalized journal，不能直接修改订单或订阅。
type OperatorPaymentReconciler interface {
	ReconcilePaymentAttempt(context.Context, string) (*cloudpb.PaymentAttemptProjection, error)
}

// OperatorAPIConfig 装配普通账号产品面中的运营模块 API。
type OperatorAPIConfig struct {
	Commerce          *commerce.Service
	Catalog           *cloudcatalog.Service
	Overrides         *cloudentitlement.OverrideService
	Promotions        *promotion.Service
	Topology          ManagementTopologyQuery
	Quota             RelayQuotaQuery
	Outbox            *commandoutbox.Service
	Planner           *commandoutbox.Planner
	Fleet             OperatorFleetQuery
	PaymentReconciler OperatorPaymentReconciler
	Releases          *releasecatalog.Service
	Now               func() time.Time
	RecentProofs      *proofStore
	SecureCookie      bool
}

// OperatorAPIHandler 使用 HttpOnly session、CSRF 与五分钟近期认证保护管理操作。
func OperatorAPIHandler(config OperatorAPIConfig) (http.Handler, error) {
	if config.Commerce == nil || config.Catalog == nil || config.Overrides == nil || config.Promotions == nil || config.Topology == nil || config.Quota == nil || config.Outbox == nil || config.Planner == nil || config.Fleet == nil || config.Releases == nil {
		return nil, commandoutbox.ErrCommandConflict
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.RecentProofs == nil {
		config.RecentProofs = newProofStore(config.Now)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/operator/reauth", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil && principal.actorKind != cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN {
			err = errOperatorForbidden
		}
		if err == nil && !productMutationAllowed(r) {
			err = commerce.ErrUnauthorized
		}
		request := &cloudpb.RecentAuthenticationRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		if err == nil {
			err = config.Commerce.VerifyPassword(r.Context(), principal.account.GetAccountId(), request.GetPassword())
		}
		var response *cloudpb.RecentAuthenticationResponse
		if err == nil {
			token, record, issueErr := config.RecentProofs.issue(principal.account.GetAccountId(), principal.actorKind, 5*time.Minute)
			err = issueErr
			if issueErr == nil {
				http.SetCookie(w, &http.Cookie{Name: "muxvia_cloud_recent", Value: token, Path: "/", MaxAge: 300, HttpOnly: true, Secure: config.SecureCookie, SameSite: http.SameSiteStrictMode})
				response = &cloudpb.RecentAuthenticationResponse{ExpiresAtUnixMillis: record.expiresAt.UnixMilli()}
			}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("GET /api/v1/operator/workspace", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		response := &cloudpb.GetOperatorWorkspaceResponse{}
		if err == nil {
			response.Modules = []cloudpb.OperatorWorkspaceModule{
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_USERS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_ORDERS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_SUBSCRIPTIONS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_PLANS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_HUBS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_AGENTS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_RELEASES,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_PROMOTIONS,
				cloudpb.OperatorWorkspaceModule_OPERATOR_WORKSPACE_MODULE_PRIVILEGES,
			}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/releases/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListReleaseArtifactsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListReleaseArtifactsResponse
		if err == nil {
			response, err = config.Releases.List(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/releases/publish", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.PublishReleaseArtifactRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var artifact *cloudpb.ReleaseArtifactProjection
		if err == nil {
			artifact, err = config.Releases.Publish(r.Context(), request.GetArtifact(), principal.account.GetUserId(), request.GetReason(), request.GetRequestId())
		}
		writeManagementProto(w, http.StatusCreated, &cloudpb.PublishReleaseArtifactResponse{Artifact: artifact}, err)
	})
	mux.HandleFunc("POST /api/v1/operator/releases/channel", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.SetReleaseChannelRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var channel *cloudpb.ReleaseChannelProjection
		if err == nil {
			channel, err = config.Releases.SetChannel(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, &cloudpb.SetReleaseChannelResponse{Channel: channel}, err)
	})
	mux.HandleFunc("POST /api/v1/operator/accounts/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListOperatorAccountsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorAccountsResponse
		if err == nil {
			accounts, listErr := config.Commerce.Accounts(r.Context(), boundedPageSize(request.GetPage(), 50))
			err = listErr
			response = &cloudpb.ListOperatorAccountsResponse{Page: &cloudpb.PageResponse{}}
			for _, account := range accounts {
				if request.GetQuery() != "" && !strings.Contains(strings.ToLower(account.GetEmail()+" "+account.GetAccountId()), strings.ToLower(request.GetQuery())) {
					continue
				}
				summary, summaryErr := operatorAccountSummary(r.Context(), config, account)
				if summaryErr != nil {
					err = summaryErr
					break
				}
				if request.GetSubscriptionStatus() == cloudpb.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED || summary.GetSubscription().GetStatus() == request.GetSubscriptionStatus() {
					response.Accounts = append(response.Accounts, summary)
				}
			}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/accounts/get", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.GetOperatorAccountRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetOperatorAccountResponse
		if err == nil {
			response, err = operatorAccountDetail(r.Context(), config, request.GetAccountId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/agents/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListOperatorAgentsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorAgentsResponse
		if err == nil {
			response, err = operatorAgents(r.Context(), config, request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/accounts/sessions/revoke", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.RevokeOperatorAccountSessionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.RevokeOperatorAccountSessionResponse
		if err == nil {
			response, err = config.Commerce.RevokeOperatorSessions(r.Context(), principal.account.GetUserId(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/subscription/transition", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.OperatorTransitionSubscriptionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.OperatorTransitionSubscriptionResponse
		if err == nil && request.GetTransition() != cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_SUSPEND && request.GetTransition() != cloudpb.SubscriptionTransitionKind_SUBSCRIPTION_TRANSITION_KIND_RESTORE {
			err = commandoutbox.ErrCommandConflict
		}
		if err == nil {
			result, transitionErr := config.Commerce.Transition(r.Context(), &cloudpb.TransitionSubscriptionRequest{AccountId: request.GetAccountId(), Transition: request.GetTransition(), ActorId: principal.account.GetUserId()})
			err, response = transitionErr, &cloudpb.OperatorTransitionSubscriptionResponse{Result: result}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/orders/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListOperatorOrdersRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorOrdersResponse
		if err == nil {
			response, err = config.Commerce.OperatorOrders(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/subscriptions/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListOperatorSubscriptionsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListOperatorSubscriptionsResponse
		if err == nil {
			response, err = config.Commerce.OperatorSubscriptions(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/subscriptions/adjust", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.CreateSubscriptionAdjustmentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateSubscriptionAdjustmentResponse
		if err == nil {
			response, err = config.Commerce.AdjustSubscription(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/orders/payment-event", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.ApplyOperatorPaymentEventRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ApplyOperatorPaymentEventResponse
		if err == nil {
			response, err = config.Commerce.ApplyOperatorPaymentEvent(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/orders/reconcile-creem", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.ReconcileCreemPaymentAttemptRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		if err == nil && (config.PaymentReconciler == nil || request.GetPaymentAttemptId() == "" || strings.TrimSpace(request.GetReason()) == "" || request.GetRequestId() == "") {
			err = commerce.ErrConflict
		}
		var response *cloudpb.ReconcileCreemPaymentAttemptResponse
		if err == nil {
			_, _, before, loadErr := config.Commerce.ProviderPaymentContext(r.Context(), request.GetPaymentAttemptId())
			err = loadErr
			if loadErr == nil {
				var reconciled *cloudpb.PaymentAttemptProjection
				reconciled, err = config.PaymentReconciler.ReconcilePaymentAttempt(r.Context(), request.GetPaymentAttemptId())
				if err == nil {
					err = config.Commerce.RecordPaymentReconciliationAudit(r.Context(), request.GetPaymentAttemptId(), principal.account.GetUserId(), request.GetReason(), request.GetRequestId(), before.GetRevision(), reconciled.GetRevision())
					response = &cloudpb.ReconcileCreemPaymentAttemptResponse{PaymentAttempt: reconciled}
				} else if _, _, latest, latestErr := config.Commerce.ProviderPaymentContext(r.Context(), request.GetPaymentAttemptId()); latestErr == nil {
					// Provider 失败也必须留下请求原因；原始 provider 错误继续返回给运营端。
					_ = config.Commerce.RecordPaymentReconciliationAudit(r.Context(), request.GetPaymentAttemptId(), principal.account.GetUserId(), request.GetReason(), request.GetRequestId(), before.GetRevision(), latest.GetRevision())
				}
			}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListPromotionsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListPromotionsResponse
		if err == nil {
			values, listErr := config.Promotions.List(r.Context(), request.GetIncludeDisabled(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListPromotionsResponse{Promotions: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/redemptions", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListPromotionRedemptionsRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListPromotionRedemptionsResponse
		if err == nil {
			values, listErr := config.Promotions.Redemptions(r.Context(), request.GetPromotionId(), request.GetAccountId(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListPromotionRedemptionsResponse{Redemptions: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/create", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.CreatePromotionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreatePromotionResponse
		if err == nil {
			response, err = config.Promotions.Create(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/promotions/disable", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.DisablePromotionRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.DisablePromotionResponse
		if err == nil {
			response, err = config.Promotions.Disable(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/catalog/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListPlanCatalogReleasesRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListPlanCatalogReleasesResponse
		if err == nil {
			values, listErr := config.Catalog.Releases(r.Context(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListPlanCatalogReleasesResponse{Releases: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/catalog/get", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.GetPlanCatalogReleaseRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetPlanCatalogReleaseResponse
		if err == nil {
			value, getErr := config.Catalog.Release(r.Context(), request.GetCatalogVersion())
			err, response = getErr, &cloudpb.GetPlanCatalogReleaseResponse{Release: value}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/catalog/publish", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.PublishPlanCatalogRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.PublishPlanCatalogResponse
		if err == nil {
			value, publishErr := config.Catalog.Publish(r.Context(), request.GetCatalog(), principal.account.GetUserId(), request.GetReason(), request.GetRequestId())
			err, response = publishErr, &cloudpb.PublishPlanCatalogResponse{Release: value}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/entitlement-overrides/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListEntitlementOverridesRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListEntitlementOverridesResponse
		if err == nil {
			values, listErr := config.Overrides.List(r.Context(), request.GetAccountId(), request.GetIncludeRevoked(), boundedPageSize(request.GetPage(), 50))
			err, response = listErr, &cloudpb.ListEntitlementOverridesResponse{Overrides: values, Page: &cloudpb.PageResponse{}}
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/entitlement-overrides/put", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.PutEntitlementOverrideRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.PutEntitlementOverrideResponse
		if err == nil {
			response, err = config.Overrides.Put(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/entitlement-overrides/revoke", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.RevokeEntitlementOverrideRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.RevokeEntitlementOverrideResponse
		if err == nil {
			response, err = config.Overrides.Revoke(r.Context(), request, principal.account.GetUserId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/commands", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.CreateManagementCommandRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateManagementCommandResponse
		if err == nil {
			command, _, createErr := config.Planner.Create(r.Context(), request, &cloudpb.ManagementActorProjection{ActorKind: principal.actorKind, ActorId: principal.account.GetUserId(), DisplayLabel: principal.account.GetUserId()}, config.Now().UTC())
			err, response = createErr, &cloudpb.CreateManagementCommandResponse{Command: command}
		}
		writeManagementProto(w, http.StatusAccepted, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/list", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.ListHubFleetRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ListHubFleetResponse
		if err == nil {
			response, err = config.Fleet.ListHubFleet(r.Context(), request)
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/get", func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateOperator(r, config.Commerce)
		request := &cloudpb.GetHubStatusRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.GetHubStatusResponse
		if err == nil {
			response, err = config.Fleet.GetHubStatus(r.Context(), request.GetHubId())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/create", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.CreateHubDeploymentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.CreateHubDeploymentResponse
		if err == nil {
			response, err = config.Fleet.CreateHubDeployment(r.Context(), request, principal.account.GetUserId(), config.Now().UTC())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/update", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.UpdateHubDeploymentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.UpdateHubDeploymentResponse
		if err == nil {
			response, err = config.Fleet.UpdateHubDeployment(r.Context(), request, principal.account.GetUserId(), config.Now().UTC())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/approve", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.ApproveHubDeploymentIdentityRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.ApproveHubDeploymentIdentityResponse
		if err == nil {
			response, err = config.Fleet.ApproveHubDeploymentIdentity(r.Context(), request, principal.account.GetUserId(), config.Now().UTC())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/drain", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.SetHubDeploymentDrainRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.SetHubDeploymentDrainResponse
		if err == nil {
			response, err = config.Fleet.SetHubDeploymentDrain(r.Context(), request, principal.account.GetUserId(), config.Now().UTC())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	mux.HandleFunc("POST /api/v1/operator/fleet/disable", func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateOperator(r, config.Commerce)
		if err == nil {
			err = requireOperatorMutation(r, principal, config.RecentProofs)
		}
		request := &cloudpb.DisableHubDeploymentRequest{}
		if err == nil {
			err = decodeProductProto(r, request)
		}
		var response *cloudpb.DisableHubDeploymentResponse
		if err == nil {
			response, err = config.Fleet.DisableHubDeployment(r.Context(), request, principal.account.GetUserId(), config.Now().UTC())
		}
		writeManagementProto(w, http.StatusOK, response, err)
	})
	return mux, nil
}

type operatorPrincipal struct {
	account   *cloudpb.AccountProjection
	actorKind cloudpb.ManagementActorKind
}

// authenticateOperator 使用普通账号 HttpOnly access cookie，并在每个请求上读取数据库当前角色。
// 菜单可见性不是授权真值；角色降级后既有浏览器 Session 的下一次请求立即失败。
func authenticateOperator(r *http.Request, service *commerce.Service) (operatorPrincipal, error) {
	cookie, err := r.Cookie(productAccessCookie)
	if err != nil {
		return operatorPrincipal{}, commerce.ErrUnauthorized
	}
	token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return operatorPrincipal{}, commerce.ErrUnauthorized
	}
	account, _, actorKind, err := service.AuthenticateOperatorAccess(r.Context(), token)
	clear(token)
	if err != nil {
		return operatorPrincipal{}, err
	}
	return operatorPrincipal{account: account, actorKind: actorKind}, nil
}

func requireOperatorMutation(r *http.Request, principal operatorPrincipal, recentProofs *proofStore) error {
	if principal.actorKind != cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN {
		return errOperatorForbidden
	}
	if !productMutationAllowed(r) {
		return commerce.ErrUnauthorized
	}
	cookie, err := r.Cookie("muxvia_cloud_recent")
	if err != nil {
		return errRecentAuthenticationRequired
	}
	if _, ok := recentProofs.validate(cookie.Value, principal.account.GetAccountId()); !ok {
		return errRecentAuthenticationRequired
	}
	return nil
}

func operatorAccountSummary(ctx context.Context, config OperatorAPIConfig, account *cloudpb.AccountProjection) (*cloudpb.OperatorAccountSummary, error) {
	commerceValue, err := config.Commerce.AccountCommerce(ctx, account.GetAccountId())
	if err != nil {
		return nil, err
	}
	quota, err := config.Quota.SnapshotForPeriod(ctx, account.GetAccountId(), time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveFromUnixMillis()), time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveUntilUnixMillis()), commerceValue.GetEntitlement().GetCapability().GetRelay().GetMaxBytesPerPeriod(), config.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &cloudpb.OperatorAccountSummary{Account: account, Subscription: commerceValue.GetSubscription(), Entitlement: commerceValue.GetEntitlement(), RelayQuota: quota.GetPeriod()}, nil
}

func operatorAccountDetail(ctx context.Context, config OperatorAPIConfig, accountID string) (*cloudpb.GetOperatorAccountResponse, error) {
	commerceValue, err := config.Commerce.AccountCommerce(ctx, accountID)
	if err != nil {
		return nil, err
	}
	quota, err := config.Quota.SnapshotForPeriod(ctx, accountID, time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveFromUnixMillis()), time.UnixMilli(commerceValue.GetEntitlement().GetEffectiveUntilUnixMillis()), commerceValue.GetEntitlement().GetCapability().GetRelay().GetMaxBytesPerPeriod(), config.Now().UTC())
	if err != nil {
		return nil, err
	}
	devices, err := config.Topology.ListAccountDevices(ctx, accountID, cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_UNSPECIFIED, true, 100)
	if err != nil {
		return nil, err
	}
	presences, sessions, err := config.Topology.ListAccountTopology(ctx, accountID, "", "", cloudpb.Freshness_FRESHNESS_UNSPECIFIED, 100)
	if err != nil {
		return nil, err
	}
	commands, err := config.Outbox.List(ctx, accountID, cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_UNSPECIFIED, cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_UNSPECIFIED, 100)
	if err != nil {
		return nil, err
	}
	accountSessions, err := config.Commerce.Sessions(ctx, accountID, true, 100)
	if err != nil {
		return nil, err
	}
	operatorAudit, err := config.Commerce.OperatorAudits(ctx, accountID, 100)
	if err != nil {
		return nil, err
	}
	return &cloudpb.GetOperatorAccountResponse{Commerce: commerceValue, RelayQuota: quota, Devices: &cloudpb.ListAccountDevicesResponse{Devices: devices, Page: &cloudpb.PageResponse{}}, Topology: &cloudpb.ListAccountTopologyResponse{Presences: presences, PeerSessions: sessions, Page: &cloudpb.PageResponse{}}, Commands: commands, Sessions: accountSessions, OperatorAudit: operatorAudit}, nil
}

func operatorAgents(ctx context.Context, config OperatorAPIConfig, request *cloudpb.ListOperatorAgentsRequest) (*cloudpb.ListOperatorAgentsResponse, error) {
	if request == nil {
		return nil, commandoutbox.ErrCommandConflict
	}
	accounts, err := config.Commerce.Accounts(ctx, 200)
	if err != nil {
		return nil, err
	}
	response := &cloudpb.ListOperatorAgentsResponse{Page: &cloudpb.PageResponse{}}
	query := strings.ToLower(strings.TrimSpace(request.GetQuery()))
	limit := boundedPageSize(request.GetPage(), 100)
	for _, account := range accounts {
		devices, listErr := config.Topology.ListAccountDevices(ctx, account.GetAccountId(), cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, request.GetIncludeRevoked(), 200)
		if listErr != nil {
			return nil, listErr
		}
		presences, peerSessions, listErr := config.Topology.ListAccountTopology(ctx, account.GetAccountId(), "", "", cloudpb.Freshness_FRESHNESS_UNSPECIFIED, 200)
		if listErr != nil {
			return nil, listErr
		}
		for _, device := range devices {
			var presence *cloudpb.PresenceProjection
			for _, candidate := range presences {
				if candidate.GetDaemonDeviceId() == device.GetDeviceId() {
					presence = candidate
					break
				}
			}
			if request.GetFreshness() != cloudpb.Freshness_FRESHNESS_UNSPECIFIED && (presence == nil || presence.GetFreshness() != request.GetFreshness()) {
				continue
			}
			presenceHubID := ""
			if presence != nil {
				presenceHubID = presence.GetControlOwnerHubId()
			}
			if query != "" && !strings.Contains(strings.ToLower(account.GetEmail()+" "+device.GetDisplayName()+" "+device.GetDeviceId()+" "+device.GetPlatform()+" "+device.GetAssignedHubId()+" "+presenceHubID), query) {
				continue
			}
			var active uint64
			for _, peer := range peerSessions {
				if peer.GetTarget().GetDaemonDeviceId() == device.GetDeviceId() && peer.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED && peer.GetState() != cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_SUPERSEDED {
					active++
				}
			}
			response.Agents = append(response.Agents, &cloudpb.OperatorAgentProjection{Account: account, Device: device, Presence: presence, ActivePeerSessionCount: active})
			if len(response.Agents) >= limit {
				return response, nil
			}
		}
	}
	return response, nil
}

var errOperatorForbidden = errors.New("operator role is forbidden")
